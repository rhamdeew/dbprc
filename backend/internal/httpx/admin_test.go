package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rhamdeew/dbprc/backend/internal/config"
	"github.com/rhamdeew/dbprc/backend/internal/db/sqlc"
	"github.com/rhamdeew/dbprc/backend/internal/httpx"
)

func TestRequireAdmin(t *testing.T) {
	cfg := config.Config{AdminEmails: []string{"Ada@Example.com"}}
	var seen string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := httpx.UserFrom(r.Context())
		if !ok {
			t.Fatal("missing user")
		}
		seen = user.Email
		w.WriteHeader(http.StatusNoContent)
	})
	handler := httpx.RequireAdmin(cfg.IsAdmin)(next)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/stats", nil)
	req = req.WithContext(httpx.WithUser(req.Context(), sqlc.User{ID: 1, Email: "ada@example.com"}))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || seen != "ada@example.com" {
		t.Fatalf("status %d email %q", rec.Code, seen)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/admin/stats", nil)
	req = req.WithContext(httpx.WithUser(req.Context(), sqlc.User{ID: 2, Email: "bea@example.com"}))
	handler.ServeHTTP(rec, req)
	assertNotFoundHidden(t, rec)

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/admin/users", nil))
	assertNotFoundHidden(t, rec)

	blocked := httpx.RequireAdmin(nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("nil predicate allowed the request")
	}))
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/admin/stats", nil)
	req = req.WithContext(httpx.WithUser(req.Context(), sqlc.User{Email: "ada@example.com"}))
	blocked.ServeHTTP(rec, req)
	assertNotFoundHidden(t, rec)
}

func assertNotFoundHidden(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	assertCode(t, rec, http.StatusNotFound, "not_found")
	if strings.Contains(strings.ToLower(rec.Body.String()), "admin") {
		t.Fatalf("body reveals the endpoint: %s", rec.Body.String())
	}
}
