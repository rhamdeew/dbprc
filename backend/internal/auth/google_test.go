package auth_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/oauth2-proxy/mockoidc"

	"github.com/rhamdeew/dbprc/backend/internal/config"
	"github.com/rhamdeew/dbprc/backend/internal/db/sqlc"
	"github.com/rhamdeew/dbprc/backend/internal/testutil"
)

func TestGoogleLogin(t *testing.T) {
	oidcSrv := startMockOIDC(t)
	pictures, pngBody := startPictures(t)
	uploadDir := t.TempDir()
	srv := newAuthServer(t, oidcSrv, uploadDir)
	appHost := hostOf(t, srv.URL)

	t.Run("start parameters and cookie", func(t *testing.T) {
		resp := doNotFollow(t, srv.URL+"/auth/google/start?next=/messenger")
		if resp.StatusCode != http.StatusFound {
			t.Fatalf("status %d", resp.StatusCode)
		}
		loc := mustLocation(t, resp)
		if loc.Query().Get("client_secret") != "" {
			t.Fatal("client secret leaked onto the authorize URL")
		}
		if loc.Query().Get("hd") != "example.com" {
			t.Fatalf("hd %q", loc.Query().Get("hd"))
		}
		scope := loc.Query().Get("scope")
		for _, want := range []string{"openid", "email", "profile"} {
			if !strings.Contains(scope, want) {
				t.Fatalf("scope %q", scope)
			}
		}
		if loc.Query().Get("response_type") != "code" || loc.Query().Get("code_challenge_method") != "S256" {
			t.Fatalf("query %s", loc.RawQuery)
		}
		if loc.Query().Get("code_challenge") == "" || loc.Query().Get("state") == "" || loc.Query().Get("nonce") == "" {
			t.Fatalf("query %s", loc.RawQuery)
		}
		if loc.Query().Get("client_id") != oidcSrv.ClientID {
			t.Fatalf("client_id %s", loc.Query().Get("client_id"))
		}
		if loc.Query().Get("redirect_uri") != srv.URL+"/auth/google/callback" {
			t.Fatalf("redirect_uri %s", loc.Query().Get("redirect_uri"))
		}
		cookie := cookieNamed(t, resp, "arc_oidc")
		if !cookie.HttpOnly || cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/auth/google" || cookie.MaxAge != 600 {
			t.Fatalf("cookie %#v", cookie)
		}
		if cookie.Value == "" {
			t.Fatal("empty cookie")
		}
	})

	t.Run("secure cookie outside dev", func(t *testing.T) {
		secureSrv := newAuthServerEnv(t, oidcSrv, uploadDir, "test")
		resp := doNotFollow(t, secureSrv.URL+"/auth/google/start")
		cookie := cookieNamed(t, resp, "arc_oidc")
		if !cookie.Secure || !cookie.HttpOnly {
			t.Fatalf("cookie %#v", cookie)
		}
	})

	var first sqlc.User
	t.Run("first login creates user and avatar", func(t *testing.T) {
		oidcSrv.QueueUser(&googleAccount{
			Subject: "sub:ada", Email: "ada@example.com", Name: "Ada",
			HD: "example.com", EmailVerified: true, Picture: pictures.URL + "/ada.png",
		})
		resp, client := followLogin(t, srv, oidcSrv, "/messenger")
		assertLanded(t, resp, appHost, "/messenger", "")
		first = userBySub(t, srv, "sub:ada")
		if first.Email != "ada@example.com" || first.Name != "Ada" || first.Available != 1 || first.AvatarPath == nil {
			t.Fatalf("user %+v", first)
		}
		if !strings.HasPrefix(*first.AvatarPath, "avatar/") || !strings.HasSuffix(*first.AvatarPath, ".jpg") {
			t.Fatalf("avatar %s", *first.AvatarPath)
		}
		if _, err := os.Stat(filepath.Join(uploadDir, *first.AvatarPath)); err != nil {
			t.Fatal(err)
		}
		status, me, _ := getMe(t, client, srv.URL+"/api/me")
		if status != http.StatusOK || me.Email != "ada@example.com" || me.Name != "Ada" || !me.Available || me.AvatarURL == nil || *me.AvatarURL != "/uploads/"+*first.AvatarPath {
			t.Fatalf("me %d %+v", status, me)
		}
		if len(pngBody) == 0 {
			t.Fatal("png")
		}
	})

	t.Run("repeat login updates name and keeps avatar", func(t *testing.T) {
		past := "2020-01-01T00:00:00Z"
		res, err := testutil.Database(t, srv).Writer().ExecContext(t.Context(), `UPDATE users SET last_login_at = ? WHERE id = ?`, past, first.ID)
		if err != nil {
			t.Fatal(err)
		}
		if n, _ := res.RowsAffected(); n != 1 {
			t.Fatalf("updated %d rows", n)
		}
		first.LastLoginAt = past
		oidcSrv.QueueUser(&googleAccount{
			Subject: "sub:ada", Email: "Ada@Example.com", Name: "Ada Lovelace",
			HD: "Example.com", EmailVerified: true, Picture: pictures.URL + "/missing",
		})
		resp, client := followLogin(t, srv, oidcSrv, "/u/1")
		assertLanded(t, resp, appHost, "/u/1", "")
		again := userBySub(t, srv, "sub:ada")
		if again.ID != first.ID || again.Name != "Ada Lovelace" || again.Email != "Ada@Example.com" {
			t.Fatalf("updated %+v", again)
		}
		if again.AvatarPath == nil || *again.AvatarPath != *first.AvatarPath || again.Available != 1 || again.CreatedAt != first.CreatedAt {
			t.Fatalf("kept %+v first %+v", again, first)
		}
		if again.LastLoginAt == first.LastLoginAt {
			t.Fatalf("last login not updated: %s", again.LastLoginAt)
		}
		status, me, _ := getMe(t, client, srv.URL+"/api/me")
		if status != http.StatusOK || me.Name != "Ada Lovelace" {
			t.Fatalf("me %d %+v", status, me)
		}
	})

	t.Run("wrong domain", func(t *testing.T) {
		before := userCount(t, srv)
		oidcSrv.QueueUser(&googleAccount{
			Subject: "sub:other", Email: "a@other.test", Name: "Other",
			HD: "other.test", EmailVerified: true, Picture: pictures.URL + "/ada.png",
		})
		resp, _ := followLogin(t, srv, oidcSrv, "/messenger")
		assertLanded(t, resp, appHost, "/login", "error=domain")
		if userCount(t, srv) != before {
			t.Fatal("user was created")
		}
	})

	t.Run("unverified email", func(t *testing.T) {
		before := userCount(t, srv)
		oidcSrv.QueueUser(&googleAccount{
			Subject: "sub:unverified", Email: "new@example.com", Name: "New",
			HD: "example.com", EmailVerified: false,
		})
		resp, _ := followLogin(t, srv, oidcSrv, "/")
		assertLanded(t, resp, appHost, "/login", "error=unverified")
		if userCount(t, srv) != before {
			t.Fatal("user was created")
		}
	})

	t.Run("bad state", func(t *testing.T) {
		before := userCount(t, srv)
		oidcSrv.QueueUser(&googleAccount{
			Subject: "sub:state", Email: "state@example.com", Name: "State",
			HD: "example.com", EmailVerified: true,
		})
		jar, _ := cookiejar.New(nil)
		start := doNotFollowJar(t, jar, srv.URL+"/auth/google/start")
		authLoc := mustLocation(t, start)
		authResp := doNotFollowJar(t, jar, authLoc.String())
		cb := mustLocation(t, authResp)
		q := cb.Query()
		q.Set("state", "not-the-state")
		cb.RawQuery = q.Encode()
		done := doNotFollowJar(t, jar, cb.String())
		if done.StatusCode != http.StatusFound || done.Header.Get("Location") != "/login?error=state" {
			t.Fatalf("status %d location %s", done.StatusCode, done.Header.Get("Location"))
		}
		if userCount(t, srv) != before {
			t.Fatal("user was created")
		}
	})

	t.Run("bad nonce", func(t *testing.T) {
		before := userCount(t, srv)
		oidcSrv.QueueUser(&googleAccount{
			Subject: "sub:nonce", Email: "nonce@example.com", Name: "Nonce",
			HD: "example.com", EmailVerified: true,
		})
		jar, _ := cookiejar.New(nil)
		start := doNotFollowJar(t, jar, srv.URL+"/auth/google/start")
		authLoc := mustLocation(t, start)
		q := authLoc.Query()
		q.Set("nonce", "attacker-nonce")
		authLoc.RawQuery = q.Encode()
		authResp := doNotFollowJar(t, jar, authLoc.String())
		cb := mustLocation(t, authResp)
		done := doNotFollowJar(t, jar, cb.String())
		if done.StatusCode != http.StatusFound || done.Header.Get("Location") != "/login?error=state" {
			t.Fatalf("status %d location %s", done.StatusCode, done.Header.Get("Location"))
		}
		if userCount(t, srv) != before {
			t.Fatal("user was created")
		}
	})

	t.Run("open redirects", func(t *testing.T) {
		for _, next := range []string{"//evil.com", "https://evil.com"} {
			oidcSrv.QueueUser(&googleAccount{
				Subject: "sub:redir-" + next, Email: "redir-" + url.QueryEscape(next) + "@example.com",
				Name: "Redir", HD: "example.com", EmailVerified: true,
			})
			resp, _ := followLogin(t, srv, oidcSrv, next)
			assertLanded(t, resp, appHost, "/", "")
			if strings.Contains(resp.Request.URL.String(), "evil.com") {
				t.Fatalf("landed on %s", resp.Request.URL)
			}
		}
	})

	t.Run("avatar failure still signs in", func(t *testing.T) {
		oidcSrv.QueueUser(&googleAccount{
			Subject: "sub:no-avatar", Email: "plain@example.com", Name: "Plain",
			HD: "example.com", EmailVerified: true, Picture: pictures.URL + "/nope",
		})
		resp, client := followLogin(t, srv, oidcSrv, "/")
		assertLanded(t, resp, appHost, "/", "")
		user := userBySub(t, srv, "sub:no-avatar")
		if user.AvatarPath != nil || user.Name != "Plain" {
			t.Fatalf("user %+v", user)
		}
		status, me, _ := getMe(t, client, srv.URL+"/api/me")
		if status != http.StatusOK || me.AvatarURL != nil || me.Email != "plain@example.com" {
			t.Fatalf("me %d %+v", status, me)
		}
	})

	t.Run("bad client secret", func(t *testing.T) {
		wrong := newAuthServerConfig(t, config.Config{
			Env:              "dev",
			Version:          "test",
			UploadDir:        uploadDir,
			OIDCIssuer:       oidcSrv.Issuer(),
			OIDCClientID:     oidcSrv.ClientID,
			OIDCClientSecret: "wrong-secret",
			AllowedDomain:    "example.com",
			SessionCookie:    "arc_sid",
		})
		oidcSrv.QueueUser(&googleAccount{
			Subject: "sub:secret", Email: "secret@example.com", Name: "Secret",
			HD: "example.com", EmailVerified: true,
		})
		resp, _ := followLogin(t, wrong, oidcSrv, "/")
		assertLanded(t, resp, hostOf(t, wrong.URL), "/login", "error=provider")
		if _, err := sqlc.New(testutil.Database(t, wrong).Reader()).GetUserByGoogleSub(t.Context(), "sub:secret"); err == nil {
			t.Fatal("user created")
		}
	})

	t.Run("missing configuration", func(t *testing.T) {
		plain := testutil.NewServer(t, testutil.WithConfig(config.Config{Env: "dev", Version: "test"}))
		resp, _ := followLoginNoQueue(t, plain, nil, "/")
		assertLanded(t, resp, hostOf(t, plain.URL), "/login", "error=provider")
	})

	t.Run("provider discovery down", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := ln.Addr().String()
		if err := ln.Close(); err != nil {
			t.Fatal(err)
		}
		down := newAuthServerConfig(t, config.Config{
			Env:              "dev",
			Version:          "test",
			UploadDir:        t.TempDir(),
			OIDCIssuer:       "http://" + addr + "/oidc",
			OIDCClientID:     "id",
			OIDCClientSecret: "secret",
			AllowedDomain:    "example.com",
			SessionCookie:    "arc_sid",
		})
		resp, _ := followLoginNoQueue(t, down, nil, "/")
		assertLanded(t, resp, hostOf(t, down.URL), "/login", "error=provider")
	})

	t.Run("callback without state cookie", func(t *testing.T) {
		resp := doNotFollow(t, srv.URL+"/auth/google/callback?code=abc&state=abc")
		if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/login?error=state" {
			t.Fatalf("status %d location %s", resp.StatusCode, resp.Header.Get("Location"))
		}
	})

	t.Run("renews an existing session", func(t *testing.T) {
		database := testutil.Database(t, srv)
		previous := testutil.CreateUser(t, database, testutil.WithEmail("old@example.com"), testutil.WithName("Old"))
		oldClient := testutil.LoginAs(t, srv, previous)
		oldCookie := testutil.SessionCookie(oldClient)
		oidcSrv.QueueUser(&googleAccount{
			Subject: "sub:renew", Email: "renew@example.com", Name: "Renew",
			HD: "example.com", EmailVerified: true,
		})
		jar, _ := cookiejar.New(nil)
		start := doNotFollowJar(t, jar, srv.URL+"/auth/google/start?next=/")
		authResp := doNotFollowJar(t, jar, mustLocation(t, start).String())
		cb := mustLocation(t, authResp)
		req, err := http.NewRequest(http.MethodGet, cb.String(), nil)
		if err != nil {
			t.Fatal(err)
		}
		// Send the state cookie from the jar and the previous session cookie.
		for _, c := range cookiesFor(t, jar, cb) {
			req.AddCookie(c)
		}
		req.AddCookie(oldCookie)
		resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
			t.Fatalf("status %d location %s", resp.StatusCode, resp.Header.Get("Location"))
		}
		fresh := cookieNamed(t, resp, "arc_sid")
		if fresh.Value == "" || fresh.Value == oldCookie.Value {
			t.Fatal("session token was not renewed")
		}
		oldReq, err := http.NewRequest(http.MethodGet, srv.URL+"/api/me", nil)
		if err != nil {
			t.Fatal(err)
		}
		oldReq.AddCookie(oldCookie)
		oldResp, err := http.DefaultClient.Do(oldReq)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = oldResp.Body.Close() }()
		if oldResp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("old session %d", oldResp.StatusCode)
		}
		newReq, err := http.NewRequest(http.MethodGet, srv.URL+"/api/me", nil)
		if err != nil {
			t.Fatal(err)
		}
		newReq.AddCookie(fresh)
		newResp, err := http.DefaultClient.Do(newReq)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = newResp.Body.Close() }()
		if newResp.StatusCode != http.StatusOK {
			t.Fatalf("new session %d", newResp.StatusCode)
		}
		var me meBody
		if err := json.NewDecoder(newResp.Body).Decode(&me); err != nil {
			t.Fatal(err)
		}
		if me.Email != "renew@example.com" || me.Name != "Renew" {
			t.Fatalf("me %+v", me)
		}
	})

	t.Run("email taken", func(t *testing.T) {
		database := testutil.Database(t, srv)
		testutil.CreateUser(t, database, testutil.WithEmail("taken@example.com"), testutil.WithGoogleSub("sub:taken-owner"))
		before := userCount(t, srv)
		oidcSrv.QueueUser(&googleAccount{
			Subject: "sub:taken-new", Email: "taken@example.com", Name: "Taken",
			HD: "example.com", EmailVerified: true, Picture: pictures.URL + "/ada.png",
		})
		resp, _ := followLogin(t, srv, oidcSrv, "/")
		assertLanded(t, resp, appHost, "/login", "error=provider")
		if userCount(t, srv) != before {
			t.Fatal("user count changed")
		}
	})
}

type googleAccount struct {
	Subject       string
	Email         string
	Name          string
	HD            string
	EmailVerified bool
	Picture       string
}

func (u *googleAccount) ID() string { return u.Subject }

func (u *googleAccount) Userinfo([]string) ([]byte, error) {
	return json.Marshal(map[string]any{
		"sub":            u.Subject,
		"email":          u.Email,
		"email_verified": u.EmailVerified,
		"hd":             u.HD,
		"name":           u.Name,
		"picture":        u.Picture,
	})
}

type googleClaims struct {
	*mockoidc.IDTokenClaims
	Email         string `json:"email,omitempty"`
	EmailVerified bool   `json:"email_verified"`
	HD            string `json:"hd,omitempty"`
	Name          string `json:"name,omitempty"`
	Picture       string `json:"picture,omitempty"`
}

func (u *googleAccount) Claims(_ []string, base *mockoidc.IDTokenClaims) (jwt.Claims, error) {
	if base == nil {
		return nil, errors.New("missing base claims")
	}
	return &googleClaims{
		IDTokenClaims: base,
		Email:         u.Email,
		EmailVerified: u.EmailVerified,
		HD:            u.HD,
		Name:          u.Name,
		Picture:       u.Picture,
	}, nil
}

func startMockOIDC(t *testing.T) *mockoidc.MockOIDC {
	t.Helper()
	srv, err := mockoidc.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := srv.Shutdown(); err != nil {
			t.Errorf("shutdown oidc: %v", err)
		}
	})
	return srv
}

func startPictures(t *testing.T) (*httptest.Server, []byte) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	body := buf.Bytes()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/nope") {
			http.Error(w, "nope", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, body
}

func newAuthServer(t *testing.T, oidcSrv *mockoidc.MockOIDC, uploadDir string) *httptest.Server {
	t.Helper()
	return newAuthServerConfig(t, config.Config{
		Env:              "dev",
		Version:          "test",
		UploadDir:        uploadDir,
		OIDCIssuer:       oidcSrv.Issuer(),
		OIDCClientID:     oidcSrv.ClientID,
		OIDCClientSecret: oidcSrv.ClientSecret,
		AllowedDomain:    "example.com",
		SessionCookie:    "arc_sid",
	})
}

func newAuthServerEnv(t *testing.T, oidcSrv *mockoidc.MockOIDC, uploadDir, env string) *httptest.Server {
	t.Helper()
	return newAuthServerConfig(t, config.Config{
		Env:              env,
		Version:          "test",
		UploadDir:        uploadDir,
		OIDCIssuer:       oidcSrv.Issuer(),
		OIDCClientID:     oidcSrv.ClientID,
		OIDCClientSecret: oidcSrv.ClientSecret,
		AllowedDomain:    "example.com",
		SessionCookie:    "arc_sid",
	})
}

func newAuthServerConfig(t *testing.T, cfg config.Config) *httptest.Server {
	t.Helper()
	return testutil.NewServer(t, testutil.WithConfig(cfg))
}

func followLogin(t *testing.T, srv *httptest.Server, oidcSrv *mockoidc.MockOIDC, next string) (*http.Response, *http.Client) {
	t.Helper()
	return followLoginNoQueue(t, srv, oidcSrv, next)
}

func followLoginNoQueue(t *testing.T, srv *httptest.Server, oidcSrv *mockoidc.MockOIDC, next string) (*http.Response, *http.Client) {
	t.Helper()
	allowed := map[string]bool{hostOf(t, srv.URL): true}
	if oidcSrv != nil {
		allowed[hostOf(t, oidcSrv.Addr())] = true
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 8 {
				return errors.New("too many redirects")
			}
			if req.URL.Host != "" && !allowed[req.URL.Host] {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/auth/google/start?next="+url.QueryEscape(next), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp, client
}

func doNotFollow(t *testing.T, rawURL string) *http.Response {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return doNotFollowJar(t, jar, rawURL)
}

func doNotFollowJar(t *testing.T, jar http.CookieJar, rawURL string) *http.Response {
	t.Helper()
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp
}

func mustLocation(t *testing.T, resp *http.Response) *url.URL {
	t.Helper()
	loc, err := resp.Location()
	if err != nil {
		t.Fatalf("location: %v (status %d)", err, resp.StatusCode)
	}
	return loc
}

func cookieNamed(t *testing.T, resp *http.Response, name string) *http.Cookie {
	t.Helper()
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("missing cookie %s in %v", name, resp.Header.Values("Set-Cookie"))
	return nil
}

func cookiesFor(t *testing.T, jar http.CookieJar, u *url.URL) []*http.Cookie {
	t.Helper()
	return jar.Cookies(u)
}

func assertLanded(t *testing.T, resp *http.Response, host, path, rawQuery string) {
	t.Helper()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		t.Fatalf("stopped on redirect %d to %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	u := resp.Request.URL
	if u.Host != host || u.Path != path || u.RawQuery != rawQuery {
		t.Fatalf("landed on %s, want //%s%s?%s", u, host, path, rawQuery)
	}
}

func hostOf(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host
}

func userBySub(t *testing.T, srv *httptest.Server, sub string) sqlc.User {
	t.Helper()
	user, err := sqlc.New(testutil.Database(t, srv).Writer()).GetUserByGoogleSub(t.Context(), sub)
	if err != nil {
		t.Fatalf("user %s: %v", sub, err)
	}
	return user
}

func userCount(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	var n int
	err := testutil.Database(t, srv).Writer().QueryRowContext(t.Context(), `SELECT count(*) FROM users`).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
