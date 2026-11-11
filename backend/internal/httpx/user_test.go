package httpx_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rhamdeew/dbprc/backend/internal/db/sqlc"
	"github.com/rhamdeew/dbprc/backend/internal/httpx"
	"github.com/rhamdeew/dbprc/backend/internal/session"
	"github.com/rhamdeew/dbprc/backend/internal/testutil"
)

func TestRequireUser(t *testing.T) {
	database := testutil.NewDB(t)
	mgr := session.New(database.Writer(), session.Settings{Secure: false}, nil)
	t.Cleanup(mgr.Close)
	user := testutil.CreateUser(t, database, testutil.WithEmail("ada@example.com"), testutil.WithName("Ada"))

	var destroyed int
	destroyOK := func(ctx context.Context) error {
		destroyed++
		return mgr.Destroy(ctx)
	}
	loadOK := func(_ context.Context, id int64) (sqlc.User, error) {
		if id != user.ID {
			t.Fatalf("load id %d", id)
		}
		return user, nil
	}

	var seen sqlc.User
	okHandler := mgr.LoadAndSave(httpx.RequireUser(nil, mgr.UserID, loadOK, destroyOK)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := httpx.UserFrom(r.Context())
		if !ok {
			t.Fatal("missing user")
		}
		seen = got
		w.WriteHeader(http.StatusNoContent)
	})))

	cookie, err := mgr.Mint(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(cookie)
	okHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || seen.Email != user.Email || destroyed != 0 {
		t.Fatalf("status %d email %q destroyed %d", rec.Code, seen.Email, destroyed)
	}

	rec = httptest.NewRecorder()
	okHandler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/me", nil))
	assertUnauthenticated(t, rec)
	if destroyed != 0 {
		t.Fatal("anonymous request destroyed a session")
	}

	garbage := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(&http.Cookie{Name: cookie.Name, Value: "not-a-session"})
	okHandler.ServeHTTP(garbage, req)
	assertUnauthenticated(t, garbage)

	missing := func(context.Context, int64) (sqlc.User, error) {
		return sqlc.User{}, sql.ErrNoRows
	}
	gone := mgr.LoadAndSave(httpx.RequireUser(nil, mgr.UserID, missing, destroyOK)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler ran for a deleted user")
	})))
	cookie, err = mgr.Mint(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(cookie)
	gone.ServeHTTP(rec, req)
	assertUnauthenticated(t, rec)
	if destroyed != 1 {
		t.Fatalf("destroyed %d", destroyed)
	}
	var n int
	if err := database.Writer().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM sessions WHERE token = ?`, cookie.Value).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("session rows %d", n)
	}

	loadErr := func(context.Context, int64) (sqlc.User, error) {
		return sqlc.User{}, errors.New("db down")
	}
	broken := mgr.LoadAndSave(httpx.RequireUser(nil, mgr.UserID, loadErr, destroyOK)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler ran after a database error")
	})))
	cookie, err = mgr.Mint(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(cookie)
	broken.ServeHTTP(rec, req)
	assertCode(t, rec, http.StatusInternalServerError, "internal")
	if destroyed != 1 {
		t.Fatalf("destroyed %d", destroyed)
	}

	destroyFail := func(context.Context) error { return errors.New("store down") }
	fail := mgr.LoadAndSave(httpx.RequireUser(nil, mgr.UserID, missing, destroyFail)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler ran after destroy failed")
	})))
	cookie, err = mgr.Mint(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(cookie)
	fail.ServeHTTP(rec, req)
	assertCode(t, rec, http.StatusInternalServerError, "internal")
}

func TestUserFromEmpty(t *testing.T) {
	if _, ok := httpx.UserFrom(t.Context()); ok {
		t.Fatal("expected no user")
	}
	user := sqlc.User{ID: 3, Email: "a@b.c"}
	got, ok := httpx.UserFrom(httpx.WithUser(t.Context(), user))
	if !ok || got.ID != 3 {
		t.Fatalf("got %+v ok %v", got, ok)
	}
}

func assertUnauthenticated(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	assertCode(t, rec, http.StatusUnauthorized, "unauthenticated")
}

func assertCode(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	res := rec.Result()
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
