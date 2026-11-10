package auth_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rhamdeew/dbprc/backend/internal/auth"
	"github.com/rhamdeew/dbprc/backend/internal/config"
	"github.com/rhamdeew/dbprc/backend/internal/db/sqlc"
	"github.com/rhamdeew/dbprc/backend/internal/httpx"
	"github.com/rhamdeew/dbprc/backend/internal/testutil"
)

func TestMe(t *testing.T) {
	srv := testutil.NewServer(t, testutil.WithConfig(config.Config{
		Env:         "test",
		Version:     "test",
		AdminEmails: []string{"Admin@Example.com"},
	}))
	database := testutil.Database(t, srv)
	admin := testutil.CreateUser(t, database,
		testutil.WithEmail("admin@example.com"),
		testutil.WithName("Ada"),
		testutil.WithAvatarPath("avatars/a.png"),
		testutil.WithAvailable(false),
	)
	member := testutil.CreateUser(t, database,
		testutil.WithEmail("bea@example.com"),
		testutil.WithName("Bea"),
	)

	adminClient := testutil.LoginAs(t, srv, admin)
	adminCookie := testutil.SessionCookie(adminClient)
	if adminCookie == nil || !adminCookie.HttpOnly || !adminCookie.Secure || adminCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie %#v", adminCookie)
	}
	if adminCookie.Expires.Before(time.Now().Add(6*24*time.Hour)) || adminCookie.Expires.After(time.Now().Add(8*24*time.Hour)) {
		t.Fatalf("cookie expiry %s", adminCookie.Expires)
	}
	status, body, raw := getMe(t, adminClient, srv.URL+"/api/me")
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	avatar := "/uploads/avatars/a.png"
	if body.ID != admin.ID || body.Email != admin.Email || body.Name != "Ada" || body.Available || !body.IsAdmin || body.AvatarURL == nil || *body.AvatarURL != avatar {
		t.Fatalf("body %+v", body)
	}
	for _, key := range []string{"id", "email", "name", "avatarUrl", "available", "isAdmin"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("missing %s in %v", key, raw)
		}
	}
	if len(raw) != 6 {
		t.Fatalf("keys %v", raw)
	}

	memberClient := testutil.LoginAs(t, srv, member)
	status, body, _ = getMe(t, memberClient, srv.URL+"/api/me")
	if status != http.StatusOK || body.ID != member.ID || body.Email != member.Email || !body.Available || body.IsAdmin || body.AvatarURL != nil || body.Name != "Bea" {
		t.Fatalf("member %d %+v", status, body)
	}

	res, err := http.Get(srv.URL + "/api/me")
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, res, http.StatusUnauthorized, "unauthenticated")

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: "arc_sid", Value: "not-a-session"})
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, res, http.StatusUnauthorized, "unauthenticated")

	// A bad Origin does not apply to GET.
	req, err = http.NewRequest(http.MethodGet, srv.URL+"/api/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://evil.example")
	req.AddCookie(testutil.SessionCookie(memberClient))
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("get with origin %d", res.StatusCode)
	}

	expireSession(t, srv, memberClient)
	status, _, _ = getMe(t, memberClient, srv.URL+"/api/me")
	if status != http.StatusUnauthorized {
		t.Fatalf("expired status %d", status)
	}

	victim := testutil.CreateUser(t, database, testutil.WithEmail("gone@example.com"))
	victimClient := testutil.LoginAs(t, srv, victim)
	if err := sqlc.New(database.Writer()).DeleteUser(t.Context(), victim.ID); err != nil {
		t.Fatal(err)
	}
	res, err = victimClient.Get(srv.URL + "/api/me")
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, res, http.StatusUnauthorized, "unauthenticated")
	if sessionCount(t, srv, testutil.SessionCookie(victimClient).Value) != 0 {
		t.Fatal("deleted user kept a session")
	}

	res, err = http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("healthz %d", res.StatusCode)
	}
	res, err = http.Get(srv.URL + "/api/version")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("version %d", res.StatusCode)
	}
}

func TestLogoutAndCSRF(t *testing.T) {
	srv := testutil.NewServer(t, testutil.WithConfig(config.Config{
		Env:        "test",
		Version:    "test",
		AppBaseURL: "https://arc.example/app",
	}))
	user := testutil.CreateUser(t, testutil.Database(t, srv), testutil.WithEmail("ada@example.com"))
	client := testutil.LoginAs(t, srv, user)
	cookie := testutil.SessionCookie(client)

	res := send(t, http.MethodPost, srv.URL+"/auth/logout", cookie, map[string]string{"Origin": "https://evil.example"})
	assertAPIError(t, res, http.StatusForbidden, "csrf")
	if sessionCount(t, srv, cookie.Value) != 1 {
		t.Fatal("wrong origin destroyed the session")
	}
	status, body, _ := getMe(t, client, srv.URL+"/api/me")
	if status != http.StatusOK || body.Email != user.Email {
		t.Fatalf("session after csrf %d %+v", status, body)
	}

	res = send(t, http.MethodPost, srv.URL+"/auth/logout", cookie, nil)
	assertAPIError(t, res, http.StatusForbidden, "csrf")
	res = send(t, http.MethodPost, srv.URL+"/auth/logout", cookie, map[string]string{"Sec-Fetch-Site": "cross-site"})
	assertAPIError(t, res, http.StatusForbidden, "csrf")
	res = send(t, http.MethodPut, srv.URL+"/auth/logout", cookie, map[string]string{"Sec-Fetch-Site": "same-site"})
	assertAPIError(t, res, http.StatusForbidden, "csrf")
	if sessionCount(t, srv, cookie.Value) != 1 {
		t.Fatal("rejected request destroyed the session")
	}

	res = send(t, http.MethodPost, srv.URL+"/auth/logout", cookie, map[string]string{"Origin": "https://ARC.EXAMPLE"})
	assertNoContent(t, res)
	if sessionCount(t, srv, cookie.Value) != 0 {
		t.Fatal("logout kept the session")
	}
	status, _, _ = getMe(t, client, srv.URL+"/api/me")
	if status != http.StatusUnauthorized {
		t.Fatalf("after logout %d", status)
	}

	// Logout is idempotent, including the Sec-Fetch-Site fallback.
	res = send(t, http.MethodPost, srv.URL+"/auth/logout", nil, map[string]string{"Sec-Fetch-Site": "same-origin"})
	assertNoContent(t, res)

	other := testutil.CreateUser(t, testutil.Database(t, srv), testutil.WithEmail("bea@example.com"))
	otherClient := testutil.LoginAs(t, srv, other)
	otherCookie := testutil.SessionCookie(otherClient)
	res = send(t, http.MethodDelete, srv.URL+"/auth/logout", otherCookie, map[string]string{
		"Origin":         "https://arc.example/",
		"Sec-Fetch-Site": "cross-site",
	})
	// DELETE is not the logout route. CSRF allows it, then the mux says 405.
	// The session must still be there: the method was rejected after the check.
	if res.StatusCode != http.StatusMethodNotAllowed {
		body, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		t.Fatalf("delete status %d %s", res.StatusCode, body)
	}
	_ = res.Body.Close()
	if sessionCount(t, srv, otherCookie.Value) != 1 {
		t.Fatal("rejected method destroyed the session")
	}
	res = send(t, http.MethodPost, srv.URL+"/auth/logout", otherCookie, map[string]string{"Sec-Fetch-Site": "same-origin"})
	assertNoContent(t, res)
	if sessionCount(t, srv, otherCookie.Value) != 0 {
		t.Fatal("same-origin fetch did not log out")
	}
}

func TestDevCookieIsNotSecure(t *testing.T) {
	srv := testutil.NewServer(t, testutil.WithConfig(config.Config{Env: "dev", Version: "test"}))
	user := testutil.CreateUser(t, testutil.Database(t, srv))
	cookie := testutil.SessionCookie(testutil.LoginAs(t, srv, user))
	if cookie == nil || cookie.Secure || !cookie.HttpOnly || cookie.Name != "arc_sid" {
		t.Fatalf("cookie %#v", cookie)
	}
}

func TestCustomCookieName(t *testing.T) {
	srv := testutil.NewServer(t, testutil.WithConfig(config.Config{
		Env:           "prod",
		Version:       "test",
		SessionCookie: "custom_sid",
	}))
	user := testutil.CreateUser(t, testutil.Database(t, srv), testutil.WithEmail("ada@example.com"))
	client := testutil.LoginAs(t, srv, user)
	cookie := testutil.SessionCookie(client)
	if cookie == nil || cookie.Name != "custom_sid" || !cookie.Secure {
		t.Fatalf("cookie %#v", cookie)
	}
	status, body, _ := getMe(t, client, srv.URL+"/api/me")
	if status != http.StatusOK || body.Email != user.Email {
		t.Fatalf("me %d %+v", status, body)
	}
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: "arc_sid", Value: cookie.Value})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, res, http.StatusUnauthorized, "unauthenticated")
}

func TestMeWithoutSessionMiddleware(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	rec := httptest.NewRecorder()
	auth.Me(config.Config{}).ServeHTTP(rec, req)
	res := rec.Result()
	assertAPIError(t, res, http.StatusUnauthorized, "unauthenticated")

	user := sqlc.User{ID: 4, Email: "ada@example.com", Name: "Ada", Available: 1}
	req = httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req = req.WithContext(httpx.WithUser(req.Context(), user))
	rec = httptest.NewRecorder()
	auth.Me(config.Config{AdminEmails: []string{"ada@example.com"}}).ServeHTTP(rec, req)
	res = rec.Result()
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	var body meBody
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.ID != 4 || !body.IsAdmin || body.AvatarURL != nil || !body.Available {
		t.Fatalf("body %+v", body)
	}
}

type meBody struct {
	ID        int64   `json:"id"`
	Email     string  `json:"email"`
	Name      string  `json:"name"`
	AvatarURL *string `json:"avatarUrl"`
	Available bool    `json:"available"`
	IsAdmin   bool    `json:"isAdmin"`
}

func getMe(t *testing.T, client *http.Client, url string) (int, meBody, map[string]any) {
	t.Helper()
	res, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var raw map[string]any
	var body meBody
	dec := json.NewDecoder(res.Body)
	if res.StatusCode == http.StatusOK {
		if ct := res.Header.Get("Content-Type"); ct != "application/json" {
			t.Fatalf("content-type %q", ct)
		}
		if err := dec.Decode(&raw); err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &body); err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, body, raw
	}
	var api httpx.Error
	if err := dec.Decode(&api); err != nil {
		t.Fatal(err)
	}
	if api.Code != "unauthenticated" || api.Message == "" {
		t.Fatalf("error %+v", api)
	}
	return res.StatusCode, meBody{}, nil
}

func send(t *testing.T, method, url string, cookie *http.Cookie, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func assertAPIError(t *testing.T, res *http.Response, status int, code string) {
	t.Helper()
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != status {
		t.Fatalf("status %d, want %d", res.StatusCode, status)
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
}

func assertNoContent(t *testing.T, res *http.Response) {
	t.Helper()
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d", res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 0 {
		t.Fatalf("body %q", body)
	}
	cleared := false
	for _, cookie := range res.Cookies() {
		if cookie.Name == "arc_sid" && cookie.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("logout did not clear the cookie")
	}
}

func expireSession(t *testing.T, srv *httptest.Server, client *http.Client) {
	t.Helper()
	cookie := testutil.SessionCookie(client)
	if cookie == nil {
		t.Fatal("no cookie")
	}
	res, err := testutil.Database(t, srv).Writer().ExecContext(t.Context(), `UPDATE sessions SET expiry = julianday('now', '-1 day') WHERE token = ?`, cookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expired %d rows", n)
	}
}

func sessionCount(t *testing.T, srv *httptest.Server, token string) int {
	t.Helper()
	var n int
	err := testutil.Database(t, srv).Writer().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM sessions WHERE token = ?`, token).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
