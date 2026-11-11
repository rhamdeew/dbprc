package profile_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/rhamdeew/dbprc/backend/internal/config"
	"github.com/rhamdeew/dbprc/backend/internal/db"
	"github.com/rhamdeew/dbprc/backend/internal/httpx"
	"github.com/rhamdeew/dbprc/backend/internal/testutil"
	"github.com/rhamdeew/dbprc/backend/internal/upload"
)

func TestGetProfile(t *testing.T) {
	srv, dir := newProfileServer(t)
	database := testutil.Database(t, srv)
	me := testutil.CreateUser(t, database, testutil.WithName("Ada"), testutil.WithEmail("ada@example.com"))
	other := testutil.CreateUser(t, database, testutil.WithName("Bea"), testutil.WithEmail("bea@example.com"), testutil.WithAvatarPath("avatar/bea.jpg"))
	client := testutil.LoginAs(t, srv, me)

	status, body, raw := getProfile(t, client, srv.URL+"/api/users/"+itoa(me.ID))
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	if body.ID != me.ID || body.Name != "Ada" || body.Email != "ada@example.com" || !body.IsMe || body.AvatarURL != nil {
		t.Fatalf("self %+v", body)
	}
	if body.Interests != (interestsBody{}) || len(body.Photos) != 0 {
		t.Fatalf("empty profile %+v", body)
	}
	assertProfileKeys(t, raw)
	photos, ok := raw["photos"].([]any)
	if !ok || photos == nil {
		t.Fatalf("photos %#v", raw["photos"])
	}
	if raw["avatarUrl"] != nil {
		t.Fatalf("avatar %#v", raw["avatarUrl"])
	}

	res := putJSON(t, client, srv.URL+"/api/me/interests", interestsBody{Hobby: "chess", Music: "jazz", Movies: "noir", Other: "walks"})
	assertInterests(t, res, interestsBody{Hobby: "chess", Music: "jazz", Movies: "noir", Other: "walks"})
	photo := postPhoto(t, client, srv.URL+"/api/me/photos", tinyJPEG(t))
	if _, err := os.Stat(storedPath(t, dir, photo.URL)); err != nil {
		t.Fatal(err)
	}

	status, body, raw = getProfile(t, client, srv.URL+"/api/users/"+itoa(me.ID))
	if status != http.StatusOK || !body.IsMe || body.Interests.Hobby != "chess" || len(body.Photos) != 1 || body.Photos[0] != photo {
		t.Fatalf("after write %d %+v", status, body)
	}
	interestRaw, ok := raw["interests"].(map[string]any)
	if !ok || len(interestRaw) != 4 {
		t.Fatalf("interests %#v", raw["interests"])
	}

	status, body, _ = getProfile(t, client, srv.URL+"/api/users/"+itoa(other.ID))
	if status != http.StatusOK || body.IsMe || body.Name != "Bea" || body.Email != "bea@example.com" || body.AvatarURL == nil || *body.AvatarURL != "/uploads/avatar/bea.jpg" {
		t.Fatalf("other %d %+v", status, body)
	}
	if len(body.Photos) != 0 || body.Interests != (interestsBody{}) {
		t.Fatalf("other details %+v", body)
	}

	for _, id := range []string{"0", "-1", "nope", "999999"} {
		res, err := client.Get(srv.URL + "/api/users/" + id)
		if err != nil {
			t.Fatal(err)
		}
		assertAPIError(t, res, http.StatusNotFound, "not_found")
	}
}

func TestPutInterests(t *testing.T) {
	srv, _ := newProfileServer(t)
	database := testutil.Database(t, srv)
	user := testutil.CreateUser(t, database, testutil.WithName("Ada"), testutil.WithEmail("ada@example.com"))
	client := testutil.LoginAs(t, srv, user)
	before := loadUser(t, database, user.ID)

	res := putJSON(t, client, srv.URL+"/api/me/interests", map[string]string{
		"hobby":  "  chess  ",
		"music":  "\tjazz\n",
		"movies": "a  b",
		"other":  "  ",
		"name":   "Hacker",
		"email":  "hacker@example.com",
	})
	assertInterests(t, res, interestsBody{Hobby: "chess", Music: "jazz", Movies: "a  b", Other: ""})
	after := loadUser(t, database, user.ID)
	if after.name != before.name || after.email != before.email || after.available != before.available || after.lastLogin != before.lastLogin || after.avatar != before.avatar {
		t.Fatalf("user changed from %+v to %+v", before, after)
	}
	_, body, _ := getProfile(t, client, srv.URL+"/api/users/"+itoa(user.ID))
	if body.Name != "Ada" || body.Email != "ada@example.com" || body.Interests != (interestsBody{Hobby: "chess", Music: "jazz", Movies: "a  b"}) {
		t.Fatalf("profile %+v", body)
	}

	exactly := strings.Repeat("a", 500)
	res = putJSON(t, client, srv.URL+"/api/me/interests", interestsBody{Hobby: " " + exactly + " ", Music: strings.Repeat("é", 500)})
	assertInterests(t, res, interestsBody{Hobby: exactly, Music: strings.Repeat("é", 500)})

	res = putJSON(t, client, srv.URL+"/api/me/interests", map[string]string{"hobby": "only hobby"})
	assertInterests(t, res, interestsBody{Hobby: "only hobby"})
	_, body, _ = getProfile(t, client, srv.URL+"/api/users/"+itoa(user.ID))
	if body.Interests != (interestsBody{Hobby: "only hobby"}) {
		t.Fatalf("replace %+v", body.Interests)
	}

	cases := []struct {
		name string
		body any
		code string
		msg  string
	}{
		{name: "hobby bytes", body: interestsBody{Hobby: strings.Repeat("a", 501)}, code: "too_long", msg: "Hobby must be 500 characters or fewer."},
		{name: "music runes", body: interestsBody{Music: strings.Repeat("é", 501)}, code: "too_long", msg: "Music must be 500 characters or fewer."},
		{name: "movies", body: interestsBody{Movies: strings.Repeat("m", 501)}, code: "too_long", msg: "Movies must be 500 characters or fewer."},
		{name: "other", body: interestsBody{Other: strings.Repeat("o", 501)}, code: "too_long", msg: "Other must be 500 characters or fewer."},
		{name: "first field wins", body: interestsBody{Hobby: strings.Repeat("a", 501), Music: strings.Repeat("b", 501)}, code: "too_long", msg: "Hobby must be 500 characters or fewer."},
		{name: "broken json", body: json.RawMessage(`{"hobby":`), code: "invalid"},
		{name: "text", body: json.RawMessage(`hello`), code: "invalid"},
		{name: "array", body: json.RawMessage(`[]`), code: "invalid"},
		{name: "number field", body: json.RawMessage(`{"hobby":1}`), code: "invalid"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			res := putJSON(t, client, srv.URL+"/api/me/interests", tt.body)
			if tt.msg != "" {
				assertAPIError(t, res, http.StatusBadRequest, tt.code, tt.msg)
				return
			}
			assertAPIError(t, res, http.StatusBadRequest, tt.code)
		})
	}
	_, body, _ = getProfile(t, client, srv.URL+"/api/users/"+itoa(user.ID))
	if body.Interests.Hobby != "only hobby" || body.Name != "Ada" || body.Email != "ada@example.com" {
		t.Fatalf("rejected write changed profile %+v", body)
	}

	// No Origin: CSRF rejects the write before the handler.
	req, err := http.NewRequest(http.MethodPut, srv.URL+"/api/me/interests", strings.NewReader(`{"hobby":"nope"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(testutil.SessionCookie(client))
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, res, http.StatusForbidden, "csrf")
	_, body, _ = getProfile(t, client, srv.URL+"/api/users/"+itoa(user.ID))
	if body.Interests.Hobby != "only hobby" {
		t.Fatalf("csrf write changed hobby %q", body.Interests.Hobby)
	}
}

func TestAvatar(t *testing.T) {
	srv, dir := newProfileServer(t)
	database := testutil.Database(t, srv)
	user := testutil.CreateUser(t, database, testutil.WithName("Ada"), testutil.WithEmail("ada@example.com"))
	client := testutil.LoginAs(t, srv, user)
	before := loadUser(t, database, user.ID)

	first := postAvatar(t, client, srv.URL+"/api/me/avatar", tinyJPEG(t))
	firstPath := storedPath(t, dir, first.AvatarURL)
	if _, err := os.Stat(firstPath); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first.AvatarURL, "/uploads/avatar/") {
		t.Fatalf("url %s", first.AvatarURL)
	}
	row := loadUser(t, database, user.ID)
	if row.name != before.name || row.email != before.email || row.available != before.available || row.lastLogin != before.lastLogin || row.avatar == nil {
		t.Fatalf("user after avatar %+v", row)
	}

	second := postAvatar(t, client, srv.URL+"/api/me/avatar", tinyJPEG(t))
	secondPath := storedPath(t, dir, second.AvatarURL)
	if _, err := os.Stat(secondPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(firstPath); !os.IsNotExist(err) {
		t.Fatalf("old avatar %s: %v", firstPath, err)
	}
	if first.AvatarURL == second.AvatarURL {
		t.Fatalf("avatar was not replaced: %s", first.AvatarURL)
	}
	_, profile, _ := getProfile(t, client, srv.URL+"/api/users/"+itoa(user.ID))
	if profile.AvatarURL == nil || *profile.AvatarURL != second.AvatarURL || profile.Name != "Ada" || profile.Email != "ada@example.com" {
		t.Fatalf("profile %+v", profile)
	}

	// The stored path can point at a file that is already gone. Replacement
	// still succeeds and keeps the new file.
	if _, err := database.Writer().ExecContext(t.Context(), `UPDATE users SET avatar_path = ? WHERE id = ?`, "avatar/missing.jpg", user.ID); err != nil {
		t.Fatal(err)
	}
	third := postAvatar(t, client, srv.URL+"/api/me/avatar", tinyJPEG(t))
	if _, err := os.Stat(storedPath(t, dir, third.AvatarURL)); err != nil {
		t.Fatal(err)
	}
	row = loadUser(t, database, user.ID)
	if row.avatar == nil || "/uploads/"+*row.avatar != third.AvatarURL {
		t.Fatalf("stored %#v url %s", row.avatar, third.AvatarURL)
	}

	res := postFile(t, client, srv.URL+"/api/me/avatar", "note.txt", []byte("not an image"))
	assertAPIError(t, res, http.StatusBadRequest, "unsupported_type")
	res = postFile(t, client, srv.URL+"/api/me/avatar", "empty.jpg", nil)
	assertAPIError(t, res, http.StatusBadRequest, "unsupported_type")
	res = postFile(t, client, srv.URL+"/api/me/avatar", "huge.png", hugePNG())
	assertAPIError(t, res, http.StatusBadRequest, "invalid_image")
	res = postFile(t, client, srv.URL+"/api/me/avatar", "big.jpg", bytes.Repeat([]byte{0xff}, upload.MaxBytes+1))
	assertAPIError(t, res, http.StatusBadRequest, "too_large")
	res = postMultipart(t, client, srv.URL+"/api/me/avatar", "note", "hello")
	assertAPIError(t, res, http.StatusBadRequest, "invalid")
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/me/avatar", strings.NewReader("nope"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "text/plain")
	res, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, res, http.StatusBadRequest, "invalid")

	row = loadUser(t, database, user.ID)
	if row.avatar == nil || "/uploads/"+*row.avatar != third.AvatarURL || row.name != "Ada" {
		t.Fatalf("failed upload changed user %+v", row)
	}
	if _, err := os.Stat(storedPath(t, dir, third.AvatarURL)); err != nil {
		t.Fatal(err)
	}
}

func TestAvatarStoreUnconfigured(t *testing.T) {
	srv := testutil.NewServer(t, testutil.WithConfig(config.Config{Env: "test", Version: "test"}))
	user := testutil.CreateUser(t, testutil.Database(t, srv), testutil.WithName("Ada"), testutil.WithEmail("ada@example.com"))
	client := testutil.LoginAs(t, srv, user)
	res := postFile(t, client, srv.URL+"/api/me/avatar", "a.jpg", tinyJPEG(t))
	assertAPIError(t, res, http.StatusInternalServerError, "internal")
}

func TestPhotos(t *testing.T) {
	srv, dir := newProfileServer(t)
	database := testutil.Database(t, srv)
	owner := testutil.CreateUser(t, database, testutil.WithName("Ada"), testutil.WithEmail("ada@example.com"))
	other := testutil.CreateUser(t, database, testutil.WithName("Bea"), testutil.WithEmail("bea@example.com"))
	ownerClient := testutil.LoginAs(t, srv, owner)
	otherClient := testutil.LoginAs(t, srv, other)

	var photos []photoBody
	for i := range 5 {
		photos = append(photos, postPhoto(t, ownerClient, srv.URL+"/api/me/photos", tinyJPEG(t)))
		if photos[i].ID == 0 || !strings.HasPrefix(photos[i].URL, "/uploads/photo/") {
			t.Fatalf("photo %+v", photos[i])
		}
		if i > 0 && photos[i].ID <= photos[i-1].ID {
			t.Fatalf("ids not increasing: %+v", photos)
		}
	}
	if photoCount(t, database, owner.ID) != 5 {
		t.Fatalf("count %d", photoCount(t, database, owner.ID))
	}
	_, profile, raw := getProfile(t, ownerClient, srv.URL+"/api/users/"+itoa(owner.ID))
	if len(profile.Photos) != 5 {
		t.Fatalf("profile photos %d", len(profile.Photos))
	}
	for i, photo := range profile.Photos {
		if photo != photos[i] {
			t.Fatalf("photo %d %+v want %+v", i, photo, photos[i])
		}
		item, ok := raw["photos"].([]any)[i].(map[string]any)
		if !ok || len(item) != 2 {
			t.Fatalf("photo keys %#v", raw["photos"])
		}
	}

	before := fileSet(t, dir)
	res := postFile(t, ownerClient, srv.URL+"/api/me/photos", "sixth.jpg", tinyJPEG(t))
	assertAPIError(t, res, http.StatusConflict, "photo_limit")
	if photoCount(t, database, owner.ID) != 5 {
		t.Fatalf("count after limit %d", photoCount(t, database, owner.ID))
	}
	if after := fileSet(t, dir); !sameSet(before, after) {
		t.Fatalf("6th photo left a file\nbefore %v\nafter %v", before, after)
	}
	_, profile, _ = getProfile(t, ownerClient, srv.URL+"/api/users/"+itoa(owner.ID))
	if len(profile.Photos) != 5 || profile.Name != "Ada" || profile.Email != "ada@example.com" {
		t.Fatalf("profile after limit %+v", profile)
	}

	res = postFile(t, ownerClient, srv.URL+"/api/me/photos", "note.txt", []byte("not an image"))
	assertAPIError(t, res, http.StatusBadRequest, "unsupported_type")
	if photoCount(t, database, owner.ID) != 5 || !sameSet(before, fileSet(t, dir)) {
		t.Fatal("rejected photo changed storage")
	}

	// Someone else's photo is hidden as a missing resource, and the file stays.
	theirs := postPhoto(t, otherClient, srv.URL+"/api/me/photos", tinyJPEG(t))
	theirsPath := storedPath(t, dir, theirs.URL)
	res, err := ownerClient.Do(deleteReq(t, srv.URL+"/api/me/photos/"+itoa(theirs.ID)))
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, res, http.StatusNotFound, "not_found")
	if _, err := os.Stat(theirsPath); err != nil {
		t.Fatal(err)
	}
	if photoCount(t, database, other.ID) != 1 {
		t.Fatal("other photo was deleted")
	}

	for _, id := range []string{"0", "-2", "nope", "999999"} {
		res, err := ownerClient.Do(deleteReq(t, srv.URL+"/api/me/photos/"+id))
		if err != nil {
			t.Fatal(err)
		}
		assertAPIError(t, res, http.StatusNotFound, "not_found")
	}

	gone := photos[0]
	gonePath := storedPath(t, dir, gone.URL)
	res, err = ownerClient.Do(deleteReq(t, srv.URL+"/api/me/photos/"+itoa(gone.ID)))
	if err != nil {
		t.Fatal(err)
	}
	assertNoContent(t, res)
	if _, err := os.Stat(gonePath); !os.IsNotExist(err) {
		t.Fatalf("deleted photo file: %v", err)
	}
	if photoCount(t, database, owner.ID) != 4 {
		t.Fatalf("count %d", photoCount(t, database, owner.ID))
	}
	_, profile, _ = getProfile(t, ownerClient, srv.URL+"/api/users/"+itoa(owner.ID))
	if len(profile.Photos) != 4 || profile.Photos[0].ID == gone.ID {
		t.Fatalf("remaining %+v", profile.Photos)
	}

	res, err = ownerClient.Do(deleteReq(t, srv.URL+"/api/me/photos/"+itoa(gone.ID)))
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, res, http.StatusNotFound, "not_found")

	// The freed slot can be filled again.
	extra := postPhoto(t, ownerClient, srv.URL+"/api/me/photos", tinyJPEG(t))
	if extra.ID == 0 {
		t.Fatal("refill failed")
	}
	if photoCount(t, database, owner.ID) != 5 {
		t.Fatalf("count %d", photoCount(t, database, owner.ID))
	}
}

func TestPhotoLimitIsTransactional(t *testing.T) {
	srv, dir := newProfileServer(t)
	database := testutil.Database(t, srv)
	user := testutil.CreateUser(t, database, testutil.WithEmail("ada@example.com"))
	client := testutil.LoginAs(t, srv, user)
	for range 4 {
		postPhoto(t, client, srv.URL+"/api/me/photos", tinyJPEG(t))
	}
	other := testutil.LoginAs(t, srv, user)
	img := tinyJPEG(t)

	var wg sync.WaitGroup
	type result struct {
		code int
		err  string
	}
	results := make(chan result, 2)
	wg.Add(2)
	send := func(c *http.Client) {
		defer wg.Done()
		req, err := fileRequest(srv.URL+"/api/me/photos", "race.jpg", img)
		if err != nil {
			results <- result{err: err.Error()}
			return
		}
		res, err := c.Do(req)
		if err != nil {
			results <- result{err: err.Error()}
			return
		}
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
		results <- result{code: res.StatusCode}
	}
	go send(client)
	go send(other)
	wg.Wait()
	close(results)

	got := map[int]int{}
	for res := range results {
		if res.err != "" {
			t.Fatal(res.err)
		}
		got[res.code]++
	}
	if got[http.StatusCreated] != 1 || got[http.StatusConflict] != 1 {
		t.Fatalf("statuses %v", got)
	}
	if photoCount(t, database, user.ID) != 5 {
		t.Fatalf("count %d", photoCount(t, database, user.ID))
	}
	if n := len(fileSet(t, dir)); n != 5 {
		t.Fatalf("files %d", n)
	}
}

func TestProfileRequiresSession(t *testing.T) {
	srv, _ := newProfileServer(t)
	database := testutil.Database(t, srv)
	user := testutil.CreateUser(t, database, testutil.WithEmail("ada@example.com"))
	client := testutil.LoginAs(t, srv, user)
	photo := postPhoto(t, client, srv.URL+"/api/me/photos", tinyJPEG(t))

	calls := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/api/users/" + itoa(user.ID)},
		{method: http.MethodPut, path: "/api/me/interests", body: `{"hobby":"x"}`},
		{method: http.MethodPost, path: "/api/me/avatar"},
		{method: http.MethodPost, path: "/api/me/photos"},
		{method: http.MethodDelete, path: "/api/me/photos/" + itoa(photo.ID)},
	}
	for _, call := range calls {
		t.Run(call.method+" "+call.path, func(t *testing.T) {
			req, err := http.NewRequest(call.method, srv.URL+call.path, strings.NewReader(call.body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Origin", srv.URL)
			req.Header.Set("Content-Type", "application/json")
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			assertAPIError(t, res, http.StatusUnauthorized, "unauthenticated")
		})
	}
	if photoCount(t, database, user.ID) != 1 {
		t.Fatal("unauthenticated delete removed the photo")
	}
}

type interestsBody struct {
	Hobby  string `json:"hobby"`
	Music  string `json:"music"`
	Movies string `json:"movies"`
	Other  string `json:"other"`
}

type photoBody struct {
	ID  int64  `json:"id"`
	URL string `json:"url"`
}

type avatarBody struct {
	AvatarURL string `json:"avatarUrl"`
}

type profileBody struct {
	ID        int64         `json:"id"`
	Name      string        `json:"name"`
	Email     string        `json:"email"`
	AvatarURL *string       `json:"avatarUrl"`
	Interests interestsBody `json:"interests"`
	Photos    []photoBody   `json:"photos"`
	IsMe      bool          `json:"isMe"`
}

type storedUser struct {
	name, email, lastLogin string
	available              int64
	avatar                 *string
}

func newProfileServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	srv := testutil.NewServer(t, testutil.WithConfig(config.Config{
		Env:       "test",
		Version:   "test",
		UploadDir: dir,
	}))
	return srv, dir
}

func getProfile(t *testing.T, client *http.Client, url string) (int, profileBody, map[string]any) {
	t.Helper()
	res, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	rawBytes, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		return res.StatusCode, profileBody{}, nil
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type %q", ct)
	}
	var raw map[string]any
	if err := json.Unmarshal(rawBytes, &raw); err != nil {
		t.Fatal(err)
	}
	var body profileBody
	if err := json.Unmarshal(rawBytes, &body); err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, body, raw
}

func putJSON(t *testing.T, client *http.Client, url string, body any) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	switch raw := body.(type) {
	case json.RawMessage:
		buf.Write(raw)
	default:
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(http.MethodPut, url, &buf)
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

func postFile(t *testing.T, client *http.Client, url, filename string, body []byte) *http.Response {
	t.Helper()
	req, err := fileRequest(url, filename, body)
	if err != nil {
		t.Fatal(err)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func fileRequest(url, filename string, body []byte) (*http.Request, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(body); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, url, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req, nil
}

func postMultipart(t *testing.T, client *http.Client, url, field, value string) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField(field, value); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, url, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func postAvatar(t *testing.T, client *http.Client, url string, body []byte) avatarBody {
	t.Helper()
	res := postFile(t, client, url, "avatar.jpg", body)
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("status %d %s", res.StatusCode, b)
	}
	var avatar avatarBody
	if err := json.NewDecoder(res.Body).Decode(&avatar); err != nil {
		t.Fatal(err)
	}
	if avatar.AvatarURL == "" {
		t.Fatal("empty avatar url")
	}
	return avatar
}

func postPhoto(t *testing.T, client *http.Client, url string, body []byte) photoBody {
	t.Helper()
	res := postFile(t, client, url, "photo.jpg", body)
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("status %d %s", res.StatusCode, b)
	}
	var photo photoBody
	if err := json.NewDecoder(res.Body).Decode(&photo); err != nil {
		t.Fatal(err)
	}
	if photo.ID == 0 || photo.URL == "" {
		t.Fatalf("photo %+v", photo)
	}
	return photo
}

func deleteReq(t *testing.T, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func assertInterests(t *testing.T, res *http.Response, want interestsBody) {
	t.Helper()
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("status %d %s", res.StatusCode, b)
	}
	var raw map[string]any
	var body interestsBody
	dec := json.NewDecoder(res.Body)
	if err := dec.Decode(&raw); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatal(err)
	}
	if body != want || len(raw) != 4 {
		t.Fatalf("got %+v keys %v", body, raw)
	}
}

func assertAPIError(t *testing.T, res *http.Response, status int, code string, message ...string) {
	t.Helper()
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != status {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("status %d, want %d (%s)", res.StatusCode, status, b)
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type %q", ct)
	}
	var body httpx.Error
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Code != code || body.Message == "" {
		t.Fatalf("body %+v", body)
	}
	if len(message) > 0 && body.Message != message[0] {
		t.Fatalf("message %q", body.Message)
	}
}

func assertNoContent(t *testing.T, res *http.Response) {
	t.Helper()
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("status %d %s", res.StatusCode, b)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 0 {
		t.Fatalf("body %q", body)
	}
}

func assertProfileKeys(t *testing.T, raw map[string]any) {
	t.Helper()
	for _, key := range []string{"id", "name", "email", "avatarUrl", "interests", "photos", "isMe"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("missing %s in %v", key, raw)
		}
	}
	if len(raw) != 7 {
		t.Fatalf("keys %v", raw)
	}
}

func loadUser(t *testing.T, database *db.DB, id int64) storedUser {
	t.Helper()
	var row storedUser
	err := database.Writer().QueryRowContext(t.Context(), `
		SELECT name, email, available, avatar_path, last_login_at FROM users WHERE id = ?`, id).
		Scan(&row.name, &row.email, &row.available, &row.avatar, &row.lastLogin)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func TestUploadRateAndBodyLimits(t *testing.T) {
	srv, _ := newProfileServer(t)
	database := testutil.Database(t, srv)
	me := testutil.CreateUser(t, database, testutil.WithName("Ada"), testutil.WithEmail("ada@example.com"))
	other := testutil.CreateUser(t, database, testutil.WithName("Bea"), testutil.WithEmail("bea@example.com"))
	client := testutil.LoginAs(t, srv, me)
	otherClient := testutil.LoginAs(t, srv, other)

	big := bytes.Repeat([]byte("a"), int(upload.MaxBytes+(1<<20))+1)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/me/avatar", bytes.NewReader(big))
	if err != nil {
		t.Fatal(err)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, res, http.StatusBadRequest, "too_large", "Request body is too large.")

	interests := bytes.Repeat([]byte("b"), int(httpx.JSONBodyLimit)+1)
	req, err = http.NewRequest(http.MethodPut, srv.URL+"/api/me/interests", bytes.NewReader(interests))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, res, http.StatusBadRequest, "too_large", "Request body is too large.")

	plain := func(c *http.Client, path string) *http.Response {
		t.Helper()
		r, err := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader("x"))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Content-Type", "text/plain")
		got, err := c.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	for range 10 {
		assertAPIError(t, plain(client, "/api/me/avatar"), http.StatusBadRequest, "invalid")
		assertAPIError(t, plain(client, "/api/me/photos"), http.StatusBadRequest, "invalid")
	}
	res = plain(client, "/api/me/avatar")
	assertAPIError(t, res, http.StatusTooManyRequests, "rate_limited", "Too many requests. Try again in a minute.")
	if res.Header.Get("Retry-After") == "" {
		t.Fatal("missing Retry-After")
	}
	res = plain(otherClient, "/api/me/photos")
	if res.StatusCode == http.StatusTooManyRequests {
		t.Fatal("other user was limited")
	}
	assertAPIError(t, res, http.StatusBadRequest, "invalid")

	req, err = http.NewRequest(http.MethodDelete, srv.URL+"/api/me/photos/1", bytes.NewReader(bytes.Repeat([]byte("c"), int(httpx.SmallBodyLimit)+1)))
	if err != nil {
		t.Fatal(err)
	}
	res, err = otherClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, res, http.StatusBadRequest, "too_large", "Request body is too large.")
}

func photoCount(t *testing.T, database *db.DB, userID int64) int {
	t.Helper()
	var n int
	err := database.Writer().QueryRowContext(t.Context(), `SELECT count(*) FROM photos WHERE user_id = ?`, userID).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func storedPath(t *testing.T, dir, publicURL string) string {
	t.Helper()
	const prefix = "/uploads/"
	if !strings.HasPrefix(publicURL, prefix) {
		t.Fatalf("url %s", publicURL)
	}
	return filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(publicURL, prefix)))
}

func fileSet(t *testing.T, root string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[rel] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for key := range a {
		if !b[key] {
			return false
		}
	}
	return true
}

func tinyJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func hugePNG() []byte {
	data := make([]byte, 13)
	binary.BigEndian.PutUint32(data[0:4], 9000)
	binary.BigEndian.PutUint32(data[4:8], 1)
	data[8] = 8
	data[9] = 2
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	writeChunk(&buf, "IHDR", data)
	return buf.Bytes()
}

func writeChunk(buf *bytes.Buffer, typ string, data []byte) {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(data)))
	buf.Write(n[:])
	buf.WriteString(typ)
	buf.Write(data)
	sum := crc32.NewIEEE()
	_, _ = sum.Write([]byte(typ))
	_, _ = sum.Write(data)
	binary.BigEndian.PutUint32(n[:], sum.Sum32())
	buf.Write(n[:])
}

func itoa(id int64) string {
	return strconv.FormatInt(id, 10)
}
