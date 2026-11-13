package admin_test

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rhamdeew/dbprc/backend/internal/config"
	"github.com/rhamdeew/dbprc/backend/internal/db"
	"github.com/rhamdeew/dbprc/backend/internal/httpx"
	"github.com/rhamdeew/dbprc/backend/internal/server"
	"github.com/rhamdeew/dbprc/backend/internal/testutil"
)

const (
	adminEmail   = "admin-clear@example.com"
	targetEmail  = "Target-Clear@Example.com"
	partnerEmail = "partner-keep@example.com"
)

func TestAdminHiddenFromNonAdmin(t *testing.T) {
	srv, _ := newAdminServer(t, t.TempDir(), nil)
	database := testutil.Database(t, srv)
	member := testutil.CreateUser(t, database, testutil.WithEmail("member@example.com"))
	target := testutil.CreateUser(t, database, testutil.WithEmail("someone@example.com"))
	client := testutil.LoginAs(t, srv, member)

	for _, rawURL := range []string{
		srv.URL + "/api/admin/stats",
		srv.URL + "/api/admin/users/preview?email=" + url.QueryEscape(target.Email),
	} {
		res, err := client.Get(rawURL)
		if err != nil {
			t.Fatal(err)
		}
		assertHidden(t, res)
	}
	res := deleteUser(t, client, srv, target.Email)
	assertHidden(t, res)
	if n := scalar(t, database, `SELECT count(*) FROM users WHERE id = ?`, target.ID); n != 1 {
		t.Fatalf("target rows %d", n)
	}
	if n := scalar(t, database, `SELECT count(*) FROM admin_audit`); n != 0 {
		t.Fatalf("audit rows %d", n)
	}
}

func TestAdminRequiresSession(t *testing.T) {
	srv, _ := newAdminServer(t, t.TempDir(), nil)
	res, err := http.Get(srv.URL + "/api/admin/stats")
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, res, http.StatusUnauthorized, "unauthenticated")

	res, err = http.Get(srv.URL + "/api/admin/users/preview?email=ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, res, http.StatusUnauthorized, "unauthenticated")

	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/admin/users", strings.NewReader(`{"email":"ada@example.com"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", srv.URL)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, res, http.StatusUnauthorized, "unauthenticated")
}

func TestAdminStats(t *testing.T) {
	srv, _ := newAdminServer(t, t.TempDir(), nil)
	database := testutil.Database(t, srv)
	admin := testutil.CreateUser(t, database, testutil.WithEmail(adminEmail))
	ada := testutil.CreateUser(t, database, testutil.WithEmail("ada@example.com"))
	bea := testutil.CreateUser(t, database, testutil.WithAvailable(false), testutil.WithEmail("bea@example.com"))
	chat := testutil.CreateChat(t, database, testutil.WithChatUsers(ada.ID, bea.ID))
	testutil.CreateMessage(t, database, testutil.WithChat(chat.ID), testutil.WithSender(ada.ID))
	testutil.CreateMessage(t, database, testutil.WithChat(chat.ID), testutil.WithKind("system"))
	testutil.CreateWallPost(t, database, testutil.WithWallOwner(ada.ID), testutil.WithAuthor(bea.ID))
	testutil.CreatePhoto(t, database, testutil.WithPhotoUser(ada.ID))

	var stats statsBody
	res, err := testutil.LoginAs(t, srv, admin).Get(srv.URL + "/api/admin/stats")
	if err != nil {
		t.Fatal(err)
	}
	assertJSONKeys(t, res, &stats, "users", "availableUsers", "chats", "messages", "wallPosts", "photos")
	if stats != (statsBody{Users: 3, AvailableUsers: 2, Chats: 1, Messages: 2, WallPosts: 1, Photos: 1}) {
		t.Fatalf("stats %+v", stats)
	}
}

func TestAdminPreview(t *testing.T) {
	srv, _ := newAdminServer(t, t.TempDir(), nil)
	database := testutil.Database(t, srv)
	admin := testutil.CreateUser(t, database, testutil.WithEmail(adminEmail))
	target := testutil.CreateUser(t, database, testutil.WithEmail("Target@Example.com"))
	partner := testutil.CreateUser(t, database, testutil.WithEmail("partner@example.com"))
	third := testutil.CreateUser(t, database)
	testutil.CreateWallPost(t, database, testutil.WithWallOwner(target.ID), testutil.WithAuthor(target.ID), testutil.WithBody("own"))
	testutil.CreateWallPost(t, database, testutil.WithWallOwner(target.ID), testutil.WithAuthor(partner.ID), testutil.WithBody("from partner"))
	testutil.CreateWallPost(t, database, testutil.WithWallOwner(partner.ID), testutil.WithAuthor(target.ID), testutil.WithAnonymous(), testutil.WithBody("secret words"))
	testutil.CreateWallPost(t, database, testutil.WithWallOwner(partner.ID), testutil.WithAuthor(partner.ID), testutil.WithBody("stays"))
	testutil.CreatePhoto(t, database, testutil.WithPhotoUser(target.ID))
	testutil.CreatePhoto(t, database, testutil.WithPhotoUser(partner.ID))
	chat := testutil.CreateChat(t, database, testutil.WithChatUsers(target.ID, partner.ID))
	testutil.CreateMessage(t, database, testutil.WithChat(chat.ID), testutil.WithSender(target.ID), testutil.WithMessageBody("hello there"))
	testutil.CreateMessage(t, database, testutil.WithChat(chat.ID), testutil.WithKind("system"), testutil.WithMessageBody("Matched again ☕"))
	other := testutil.CreateChat(t, database, testutil.WithChatUsers(partner.ID, third.ID))
	testutil.CreateMessage(t, database, testutil.WithChat(other.ID), testutil.WithSender(partner.ID))

	client := testutil.LoginAs(t, srv, admin)
	var preview previewBody
	res, err := client.Get(srv.URL + "/api/admin/users/preview?email=" + url.QueryEscape("  target@example.com  "))
	if err != nil {
		t.Fatal(err)
	}
	raw := assertJSONKeys(t, res, &preview, "exists", "wallPosts", "authoredPosts", "photos", "chats", "messages")
	if preview != (previewBody{Exists: true, WallPosts: 2, AuthoredPosts: 2, Photos: 1, Chats: 1, Messages: 2}) {
		t.Fatalf("preview %+v", preview)
	}
	if strings.Contains(raw, "secret") || strings.Contains(raw, "hello") || strings.Contains(raw, "Target") || strings.Contains(raw, target.Name) {
		t.Fatalf("preview leaked content: %s", raw)
	}

	res, err = client.Get(srv.URL + "/api/admin/users/preview?email=" + url.QueryEscape("missing@example.com"))
	if err != nil {
		t.Fatal(err)
	}
	assertJSONKeys(t, res, &preview, "exists", "wallPosts", "authoredPosts", "photos", "chats", "messages")
	if preview != (previewBody{}) {
		t.Fatalf("missing %+v", preview)
	}

	for _, rawURL := range []string{
		srv.URL + "/api/admin/users/preview",
		srv.URL + "/api/admin/users/preview?email=",
		srv.URL + "/api/admin/users/preview?email=" + url.QueryEscape("   "),
	} {
		res, err = client.Get(rawURL)
		if err != nil {
			t.Fatal(err)
		}
		assertAPIError(t, res, http.StatusBadRequest, "invalid")
	}
}

func TestAdminDeleteCascade(t *testing.T) {
	dir := t.TempDir()
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	srv, _ := newAdminServer(t, dir, log)
	database := testutil.Database(t, srv)
	admin := testutil.CreateUser(t, database, testutil.WithEmail(adminEmail), testutil.WithName("Admin"))
	target := testutil.CreateUser(t, database,
		testutil.WithEmail(targetEmail),
		testutil.WithName("Target Person"),
		testutil.WithAvatarPath("avatar/target.jpg"),
	)
	partner := testutil.CreateUser(t, database,
		testutil.WithEmail(partnerEmail),
		testutil.WithName("Partner Person"),
		testutil.WithAvatarPath("avatar/partner.jpg"),
	)
	third := testutil.CreateUser(t, database, testutil.WithEmail("third@example.com"))
	writeUpload(t, dir, "avatar/target.jpg")
	writeUpload(t, dir, "avatar/partner.jpg")
	testutil.CreatePhoto(t, database, testutil.WithPhotoUser(target.ID), testutil.WithPhotoPath("photo/target-1.jpg"))
	testutil.CreatePhoto(t, database, testutil.WithPhotoUser(target.ID), testutil.WithPhotoPath("photo/target-2.jpg"))
	testutil.CreatePhoto(t, database, testutil.WithPhotoUser(partner.ID), testutil.WithPhotoPath("photo/partner.jpg"))
	writeUpload(t, dir, "photo/target-1.jpg")
	writeUpload(t, dir, "photo/target-2.jpg")
	writeUpload(t, dir, "photo/partner.jpg")

	root := testutil.CreateWallPost(t, database, testutil.WithWallOwner(target.ID), testutil.WithAuthor(target.ID), testutil.WithBody("hello from target"))
	testutil.CreateWallPost(t, database, testutil.WithWallOwner(target.ID), testutil.WithAuthor(partner.ID), testutil.WithParent(root.ID), testutil.WithBody("reply"))
	testutil.CreateWallPost(t, database,
		testutil.WithWallOwner(target.ID),
		testutil.WithAuthor(partner.ID),
		testutil.WithDrawingPath("drawing/on-target.png"),
		testutil.WithBody("drawn on target"),
	)
	onPartnerWall := testutil.CreateWallPost(t, database,
		testutil.WithWallOwner(partner.ID),
		testutil.WithAuthor(target.ID),
		testutil.WithAnonymous(),
		testutil.WithDrawingPath("drawing/by-target.png"),
		testutil.WithBody("anonymous note"),
	)
	// Someone else's reply under the target's post on a third wall. The row
	// goes with its parent, so its drawing has to go too.
	testutil.CreateWallPost(t, database,
		testutil.WithWallOwner(partner.ID),
		testutil.WithAuthor(third.ID),
		testutil.WithParent(onPartnerWall.ID),
		testutil.WithDrawingPath("drawing/reply-under-target.png"),
		testutil.WithBody("reply with a drawing"),
	)
	testutil.CreateWallPost(t, database,
		testutil.WithWallOwner(partner.ID),
		testutil.WithAuthor(partner.ID),
		testutil.WithDrawingPath("drawing/partner-own.png"),
		testutil.WithBody("partner own"),
	)
	writeUpload(t, dir, "drawing/on-target.png")
	writeUpload(t, dir, "drawing/by-target.png")
	writeUpload(t, dir, "drawing/partner-own.png")
	writeUpload(t, dir, "drawing/reply-under-target.png")

	insertInterests(t, database, target.ID, "chess")
	insertInterests(t, database, partner.ID, "jazz")

	withPartner := testutil.CreateChat(t, database, testutil.WithChatUsers(target.ID, partner.ID))
	withThird := testutil.CreateChat(t, database, testutil.WithChatUsers(target.ID, third.ID))
	stays := testutil.CreateChat(t, database, testutil.WithChatUsers(partner.ID, third.ID))
	testutil.CreateMessage(t, database, testutil.WithChat(withPartner.ID), testutil.WithSender(target.ID), testutil.WithMessageBody("hey"))
	testutil.CreateMessage(t, database, testutil.WithChat(withPartner.ID), testutil.WithKind("system"), testutil.WithMessageBody("Matched again ☕"))
	testutil.CreateMessage(t, database, testutil.WithChat(withThird.ID), testutil.WithSender(third.ID), testutil.WithMessageBody("other"))
	kept := testutil.CreateMessage(t, database, testutil.WithChat(stays.ID), testutil.WithSender(partner.ID), testutil.WithMessageBody("still here"))
	insertRead(t, database, withPartner.ID, target.ID)
	insertRead(t, database, withPartner.ID, partner.ID)
	insertRead(t, database, stays.ID, partner.ID)

	exec(t, database, `INSERT INTO sessions (token, data, expiry) VALUES ('garbage-session', x'00', 1)`)

	adminClient := testutil.LoginAs(t, srv, admin)
	targetClient := testutil.LoginAs(t, srv, target)
	partnerClient := testutil.LoginAs(t, srv, partner)
	app, ok := srv.Config.Handler.(*server.Server)
	if !ok {
		t.Fatal("handler")
	}
	extra, err := app.MintSession(t.Context(), target.ID)
	if err != nil {
		t.Fatal(err)
	}
	targetCookie := testutil.SessionCookie(targetClient)
	partnerCookie := testutil.SessionCookie(partnerClient)
	adminCookie := testutil.SessionCookie(adminClient)

	var preview previewBody
	res, err := adminClient.Get(srv.URL + "/api/admin/users/preview?email=" + url.QueryEscape("target-clear@example.com"))
	if err != nil {
		t.Fatal(err)
	}
	decodeJSON(t, res, &preview)
	if preview != (previewBody{Exists: true, WallPosts: 3, AuthoredPosts: 2, Photos: 2, Chats: 2, Messages: 3}) {
		t.Fatalf("preview %+v", preview)
	}
	var before statsBody
	res, err = adminClient.Get(srv.URL + "/api/admin/stats")
	if err != nil {
		t.Fatal(err)
	}
	decodeJSON(t, res, &before)
	if before != (statsBody{Users: 4, AvailableUsers: 4, Chats: 3, Messages: 4, WallPosts: 6, Photos: 3}) {
		t.Fatalf("stats before %+v", before)
	}

	res = deleteUser(t, adminClient, srv, "  TARGET-CLEAR@example.com  ")
	if res.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		t.Fatalf("status %d body %s", res.StatusCode, body)
	}
	body, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(bytes.TrimSpace(body)) != 0 {
		t.Fatalf("body %s", body)
	}

	if scalar(t, database, `SELECT count(*) FROM users WHERE id = ?`, target.ID) != 0 {
		t.Fatal("user row left")
	}
	if scalar(t, database, `SELECT count(*) FROM users`) != 3 {
		t.Fatal("user count")
	}
	if scalar(t, database, `SELECT count(*) FROM interests WHERE user_id = ?`, target.ID) != 0 {
		t.Fatal("target interests left")
	}
	if scalar(t, database, `SELECT count(*) FROM interests WHERE user_id = ?`, partner.ID) != 1 {
		t.Fatal("partner interests missing")
	}
	if scalar(t, database, `SELECT count(*) FROM photos WHERE user_id = ?`, target.ID) != 0 {
		t.Fatal("target photos left")
	}
	if scalar(t, database, `SELECT count(*) FROM photos`) != 1 {
		t.Fatal("partner photo missing")
	}
	if scalar(t, database, `SELECT count(*) FROM wall_posts WHERE wall_owner_id = ? OR author_id = ?`, target.ID, target.ID) != 0 {
		t.Fatal("target posts left")
	}
	if scalar(t, database, `SELECT count(*) FROM wall_posts`) != 1 {
		t.Fatal("partner post missing")
	}
	if scalar(t, database, `SELECT count(*) FROM chats WHERE user_low = ? OR user_high = ?`, target.ID, target.ID) != 0 {
		t.Fatal("target chats left")
	}
	if scalar(t, database, `SELECT count(*) FROM chats WHERE id = ?`, stays.ID) != 1 {
		t.Fatal("partner chat missing")
	}
	if scalar(t, database, `SELECT count(*) FROM messages WHERE id = ?`, kept.ID) != 1 {
		t.Fatal("kept message missing")
	}
	if scalar(t, database, `SELECT count(*) FROM messages`) != 1 {
		t.Fatal("messages left behind")
	}
	if scalar(t, database, `SELECT count(*) FROM chat_reads WHERE chat_id = ?`, stays.ID) != 1 {
		t.Fatal("kept read cursor missing")
	}
	if scalar(t, database, `SELECT count(*) FROM chat_reads WHERE chat_id != ?`, stays.ID) != 0 {
		t.Fatal("deleted chat reads left")
	}
	for _, token := range []string{targetCookie.Value, extra.Value} {
		if scalar(t, database, `SELECT count(*) FROM sessions WHERE token = ?`, token) != 0 {
			t.Fatalf("target session %s left", token)
		}
	}
	for _, token := range []string{adminCookie.Value, partnerCookie.Value, "garbage-session"} {
		if scalar(t, database, `SELECT count(*) FROM sessions WHERE token = ?`, token) != 1 {
			t.Fatalf("session %s missing", token)
		}
	}
	for _, rel := range []string{"avatar/target.jpg", "photo/target-1.jpg", "photo/target-2.jpg", "drawing/on-target.png", "drawing/by-target.png", "drawing/reply-under-target.png"} {
		if fileExists(dir, rel) {
			t.Fatalf("file %s still on disk", rel)
		}
	}
	for _, rel := range []string{"avatar/partner.jpg", "photo/partner.jpg", "drawing/partner-own.png"} {
		if !fileExists(dir, rel) {
			t.Fatalf("file %s was removed", rel)
		}
	}

	var auditAdmin, action, hash, created sql.NullString
	if err := database.Writer().QueryRowContext(t.Context(), `SELECT admin_email, action, target_email_sha256, created_at FROM admin_audit`).Scan(&auditAdmin, &action, &hash, &created); err != nil {
		t.Fatal(err)
	}
	if auditAdmin.String != admin.Email || action.String != "delete_user" || hash.String != emailHash(target.Email) || !created.Valid {
		t.Fatalf("audit admin %q action %q hash %q created %q", auditAdmin.String, action.String, hash.String, created.String)
	}
	if _, err := time.Parse(time.RFC3339, created.String); err != nil {
		t.Fatal(err)
	}
	if scalar(t, database, `SELECT count(*) FROM admin_audit`) != 1 {
		t.Fatal("audit count")
	}
	if strings.Contains(hash.String, "@") {
		t.Fatal("hash looks like an email")
	}

	text := logs.String()
	for _, email := range []string{admin.Email, target.Email, partner.Email, strings.ToLower(target.Email)} {
		if strings.Contains(text, email) {
			t.Fatalf("log contains %s: %s", email, text)
		}
	}
	if !strings.Contains(text, hash.String) || !strings.Contains(text, "admin delete") {
		t.Fatalf("log missing the action: %s", text)
	}

	res, err = partnerClient.Get(srv.URL + "/api/chats")
	if err != nil {
		t.Fatal(err)
	}
	var chats []struct {
		ID int64 `json:"id"`
	}
	decodeJSON(t, res, &chats)
	if len(chats) != 1 || chats[0].ID != stays.ID {
		t.Fatalf("partner chats %+v", chats)
	}
	res, err = partnerClient.Get(srv.URL + "/api/users/" + itoa(target.ID))
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, res, http.StatusNotFound, "not_found")

	res, err = adminClient.Get(srv.URL + "/api/me")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("admin me %d", res.StatusCode)
	}
	_ = res.Body.Close()

	res = deleteUser(t, adminClient, srv, targetEmail)
	assertAPIError(t, res, http.StatusNotFound, "not_found")
	if scalar(t, database, `SELECT count(*) FROM admin_audit`) != 1 {
		t.Fatal("second delete wrote an audit row")
	}

	var after statsBody
	res, err = adminClient.Get(srv.URL + "/api/admin/stats")
	if err != nil {
		t.Fatal(err)
	}
	decodeJSON(t, res, &after)
	if after != (statsBody{Users: 3, AvailableUsers: 3, Chats: 1, Messages: 1, WallPosts: 1, Photos: 1}) {
		t.Fatalf("stats after %+v", after)
	}
}

func TestAdminDeleteSelf(t *testing.T) {
	srv, _ := newAdminServer(t, t.TempDir(), nil)
	database := testutil.Database(t, srv)
	admin := testutil.CreateUser(t, database, testutil.WithEmail(adminEmail))
	client := testutil.LoginAs(t, srv, admin)
	res := deleteUser(t, client, srv, "  Admin-Clear@Example.com ")
	assertAPIError(t, res, http.StatusConflict, "self_delete")
	if scalar(t, database, `SELECT count(*) FROM users WHERE id = ?`, admin.ID) != 1 {
		t.Fatal("admin was deleted")
	}
	if scalar(t, database, `SELECT count(*) FROM admin_audit`) != 0 {
		t.Fatal("self-delete wrote an audit row")
	}
}

func TestAdminDeleteInvalid(t *testing.T) {
	srv, _ := newAdminServer(t, t.TempDir(), nil)
	database := testutil.Database(t, srv)
	admin := testutil.CreateUser(t, database, testutil.WithEmail(adminEmail))
	client := testutil.LoginAs(t, srv, admin)

	cases := []struct {
		name   string
		body   string
		status int
		code   string
	}{
		{name: "json", body: "{", status: http.StatusBadRequest, code: "invalid"},
		{name: "empty object", body: "{}", status: http.StatusBadRequest, code: "invalid"},
		{name: "blank", body: `{"email":"  "}`, status: http.StatusBadRequest, code: "invalid"},
		{name: "null", body: `{"email":null}`, status: http.StatusBadRequest, code: "invalid"},
		{name: "missing user", body: `{"email":"ada' OR '1'='1@example.com"}`, status: http.StatusNotFound, code: "not_found"},
		{name: "unknown", body: `{"email":"nobody@example.com"}`, status: http.StatusNotFound, code: "not_found"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			res := deleteRaw(t, client, srv, strings.NewReader(tt.body))
			assertAPIError(t, res, tt.status, tt.code)
		})
	}
	if scalar(t, database, `SELECT count(*) FROM admin_audit`) != 0 {
		t.Fatal("invalid delete wrote an audit row")
	}

	big := bytes.NewReader(bytes.Repeat([]byte("b"), int(httpx.JSONBodyLimit)+1))
	res := deleteRaw(t, client, srv, big)
	assertAPIError(t, res, http.StatusBadRequest, "too_large")

	cookie := testutil.SessionCookie(client)
	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/admin/users", strings.NewReader(`{"email":"nobody@example.com"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(cookie)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, res, http.StatusForbidden, "csrf")
}

func TestAdminDeleteMissingFile(t *testing.T) {
	dir := t.TempDir()
	srv, _ := newAdminServer(t, dir, nil)
	database := testutil.Database(t, srv)
	admin := testutil.CreateUser(t, database, testutil.WithEmail(adminEmail))
	target := testutil.CreateUser(t, database, testutil.WithEmail("gone@example.com"), testutil.WithAvatarPath("avatar/missing.jpg"))
	res := deleteUser(t, testutil.LoginAs(t, srv, admin), srv, target.Email)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d", res.StatusCode)
	}
	_ = res.Body.Close()
	if scalar(t, database, `SELECT count(*) FROM users WHERE id = ?`, target.ID) != 0 {
		t.Fatal("user left behind")
	}
}

func TestAdminDeleteFileError(t *testing.T) {
	dir := t.TempDir()
	var logs bytes.Buffer
	srv, _ := newAdminServer(t, dir, slog.New(slog.NewTextHandler(&logs, nil)))
	database := testutil.Database(t, srv)
	admin := testutil.CreateUser(t, database, testutil.WithEmail(adminEmail))
	target := testutil.CreateUser(t, database, testutil.WithEmail("bad-path@example.com"), testutil.WithAvatarPath("../escape.jpg"))
	res := deleteUser(t, testutil.LoginAs(t, srv, admin), srv, "bad-path@example.com")
	assertAPIError(t, res, http.StatusInternalServerError, "internal")
	if scalar(t, database, `SELECT count(*) FROM users WHERE id = ?`, target.ID) != 0 {
		t.Fatal("user row rolled back")
	}
	if scalar(t, database, `SELECT count(*) FROM admin_audit`) != 1 {
		t.Fatal("audit missing after the row committed")
	}
	if strings.Contains(logs.String(), target.Email) || strings.Contains(logs.String(), admin.Email) {
		t.Fatalf("log contains an email: %s", logs.String())
	}
}

type statsBody struct {
	Users          int64 `json:"users"`
	AvailableUsers int64 `json:"availableUsers"`
	Chats          int64 `json:"chats"`
	Messages       int64 `json:"messages"`
	WallPosts      int64 `json:"wallPosts"`
	Photos         int64 `json:"photos"`
}

type previewBody struct {
	Exists        bool  `json:"exists"`
	WallPosts     int64 `json:"wallPosts"`
	AuthoredPosts int64 `json:"authoredPosts"`
	Photos        int64 `json:"photos"`
	Chats         int64 `json:"chats"`
	Messages      int64 `json:"messages"`
}

func newAdminServer(t *testing.T, uploadDir string, log *slog.Logger) (*httptest.Server, string) {
	t.Helper()
	cfg := config.Config{
		Env:         "test",
		Version:     "test",
		UploadDir:   uploadDir,
		AdminEmails: []string{"Admin-Clear@Example.com"},
	}
	opts := []testutil.ServerOption{testutil.WithConfig(cfg)}
	if log != nil {
		opts = append(opts, testutil.WithLogger(log))
	}
	return testutil.NewServer(t, opts...), uploadDir
}

func deleteUser(t *testing.T, client *http.Client, srv *httptest.Server, email string) *http.Response {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"email": email})
	if err != nil {
		t.Fatal(err)
	}
	return deleteRaw(t, client, srv, bytes.NewReader(raw))
}

func deleteRaw(t *testing.T, client *http.Client, srv *httptest.Server, body io.Reader) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/admin/users", body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func assertHidden(t *testing.T, res *http.Response) {
	t.Helper()
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status %d", res.StatusCode)
	}
	var body httpx.Error
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "not_found" || body.Message != "Not found." || strings.Contains(strings.ToLower(body.Message), "admin") {
		t.Fatalf("body %+v", body)
	}
}

func assertAPIError(t *testing.T, res *http.Response, status int, code string) {
	t.Helper()
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != status {
		t.Fatalf("status %d, want %d", res.StatusCode, status)
	}
	var body httpx.Error
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Code != code || body.Message == "" {
		t.Fatalf("body %+v", body)
	}
}

func assertJSONKeys(t *testing.T, res *http.Response, dest any, keys ...string) string {
	t.Helper()
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(keys) {
		t.Fatalf("keys %v", got)
	}
	for _, key := range keys {
		if _, ok := got[key]; !ok {
			t.Fatalf("missing %s in %s", key, raw)
		}
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func decodeJSON(t *testing.T, res *http.Response, dest any) {
	t.Helper()
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("status %d body %s", res.StatusCode, body)
	}
	if err := json.NewDecoder(res.Body).Decode(dest); err != nil {
		t.Fatal(err)
	}
}

func scalar(t *testing.T, database *db.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := database.Writer().QueryRowContext(t.Context(), query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func exec(t *testing.T, database *db.DB, query string, args ...any) {
	t.Helper()
	if _, err := database.Writer().ExecContext(t.Context(), query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func insertInterests(t *testing.T, database *db.DB, userID int64, hobby string) {
	t.Helper()
	exec(t, database, `INSERT INTO interests (user_id, hobby, music, movies, other, updated_at) VALUES (?, ?, '', '', '', ?)`,
		userID, hobby, time.Now().UTC().Format(time.RFC3339))
}

func insertRead(t *testing.T, database *db.DB, chatID, userID int64) {
	t.Helper()
	exec(t, database, `INSERT INTO chat_reads (chat_id, user_id, last_read_message_id) VALUES (?, ?, 0)`, chatID, userID)
}

func writeUpload(t *testing.T, dir, rel string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fileExists(dir, rel string) bool {
	_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
	return err == nil
}

func emailHash(email string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(sum[:])
}

func itoa(id int64) string {
	return strconv.FormatInt(id, 10)
}
