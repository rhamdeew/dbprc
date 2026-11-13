package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rhamdeew/dbprc/backend/internal/config"
)

func TestDevStatic(t *testing.T) {
	parent := t.TempDir()
	staticDir := filepath.Join(parent, "static")
	uploadDir := filepath.Join(parent, "uploads")
	if err := os.MkdirAll(filepath.Join(staticDir, "assets"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(uploadDir, "photos"), 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(staticDir, "index.html"), "INDEX")
	writeFile(t, filepath.Join(staticDir, "assets", "app.js"), "JS")
	writeFile(t, filepath.Join(staticDir, "ping"), "PING")
	writeFile(t, filepath.Join(uploadDir, "photos", "a.jpg"), "PHOTO")
	writeFile(t, filepath.Join(parent, "secret.txt"), "SECRET")

	cfg := config.Config{
		Env:           "test",
		Version:       "abc123",
		AppBaseURL:    "http://arc.test",
		SessionCookie: "arc_sid",
		StaticDir:     staticDir,
		UploadDir:     uploadDir,
	}
	cfg.MessageKey = make([]byte, 32)
	app := New(cfg, Deps{DB: openDB(t), Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	srv := httptest.NewServer(app)
	t.Cleanup(func() {
		srv.Close()
		app.Close()
	})

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantBody   string
		wantType   string
		wantCache  string
	}{
		{name: "root", method: http.MethodGet, path: "/", wantStatus: http.StatusOK, wantBody: "INDEX", wantType: "text/html"},
		{name: "client route", method: http.MethodGet, path: "/login", wantStatus: http.StatusOK, wantBody: "INDEX", wantType: "text/html"},
		{name: "profile route", method: http.MethodGet, path: "/u/1", wantStatus: http.StatusOK, wantBody: "INDEX"},
		{name: "asset", method: http.MethodGet, path: "/assets/app.js", wantStatus: http.StatusOK, wantBody: "JS", wantType: "text/javascript"},
		{name: "extensionless file", method: http.MethodGet, path: "/ping", wantStatus: http.StatusOK, wantBody: "PING"},
		{name: "missing asset", method: http.MethodGet, path: "/assets/missing.js", wantStatus: http.StatusNotFound},
		{name: "healthz stays json", method: http.MethodGet, path: "/healthz", wantStatus: http.StatusOK, wantBody: `"status":"ok"`, wantType: "application/json"},
		{name: "version stays json", method: http.MethodGet, path: "/api/version", wantStatus: http.StatusOK, wantBody: `"version":"abc123"`, wantType: "application/json"},
		{name: "upload", method: http.MethodGet, path: "/uploads/photos/a.jpg", wantStatus: http.StatusOK, wantBody: "PHOTO", wantCache: "public, max-age=31536000, immutable"},
		{name: "upload dir", method: http.MethodGet, path: "/uploads/photos/", wantStatus: http.StatusNotFound},
		{name: "upload missing", method: http.MethodGet, path: "/uploads/nope.jpg", wantStatus: http.StatusNotFound},
		{name: "head asset", method: http.MethodHead, path: "/assets/app.js", wantStatus: http.StatusOK, wantBody: ""},
		{name: "post unknown", method: http.MethodPost, path: "/login", wantStatus: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, srv.URL+tt.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			if tt.method == http.MethodPost {
				req.Header.Set("Origin", "http://arc.test")
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status %d body %q", resp.StatusCode, body)
			}
			if tt.wantBody != "" && !strings.Contains(string(body), tt.wantBody) {
				t.Fatalf("body %q, want substring %q", body, tt.wantBody)
			}
			if tt.method == http.MethodHead && len(body) != 0 {
				t.Fatalf("head body %q", body)
			}
			if tt.wantType != "" && !strings.Contains(resp.Header.Get("Content-Type"), tt.wantType) {
				t.Fatalf("content-type %q", resp.Header.Get("Content-Type"))
			}
			if tt.wantCache != "" && resp.Header.Get("Cache-Control") != tt.wantCache {
				t.Fatalf("cache %q", resp.Header.Get("Cache-Control"))
			}
			served := (tt.method == http.MethodGet || tt.method == http.MethodHead) && tt.wantStatus == http.StatusOK &&
				tt.path != "/healthz" && !strings.HasPrefix(tt.path, "/api/")
			if served {
				csp := resp.Header.Get("Content-Security-Policy")
				wantCSP := "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self' wss://" + req.Host + "; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"
				if csp != wantCSP {
					t.Fatalf("csp %q, want %q", csp, wantCSP)
				}
				if resp.Header.Get("X-Content-Type-Options") != "nosniff" || resp.Header.Get("Referrer-Policy") != "same-origin" {
					t.Fatalf("headers %v", resp.Header)
				}
			}
			if tt.path == "/uploads/photos/a.jpg" && resp.Header.Get("Content-Disposition") != "inline" {
				t.Fatalf("disposition %q", resp.Header.Get("Content-Disposition"))
			}
			if tt.path == "/healthz" && resp.Header.Get("Content-Security-Policy") != "" {
				t.Fatalf("healthz csp %q", resp.Header.Get("Content-Security-Policy"))
			}
		})
	}

	for _, target := range []string{"/../secret.txt", "/uploads/../secret.txt"} {
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.URL.Path = target
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		if strings.Contains(rec.Body.String(), "SECRET") {
			t.Fatalf("%s escaped: %d %s", target, rec.Code, rec.Body.String())
		}
	}

	wsReq := httptest.NewRequest(http.MethodGet, "/ws", nil)
	wsRec := httptest.NewRecorder()
	app.ServeHTTP(wsRec, wsReq)
	if wsRec.Code != http.StatusUnauthorized {
		t.Fatalf("ws status %d body %s", wsRec.Code, wsRec.Body.String())
	}
	if strings.Contains(wsRec.Header().Get("Content-Type"), "text/html") {
		t.Fatal("ws served as html")
	}
}

func TestNoStaticDirLeavesUnknown404(t *testing.T) {
	srv := newTestServer(t)
	resp, err := http.Get(srv.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Production serves static HTML through Caddy, so its policy must match E2E's.
func TestCaddyMatchesDevCSP(t *testing.T) {
	caddy, err := os.ReadFile("../../../deploy/env/Caddyfile")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://arc.test/", nil)
	rec := httptest.NewRecorder()
	setSecurityHeaders(rec, req)
	policy := strings.ReplaceAll(rec.Header().Get("Content-Security-Policy"), "wss://arc.test", "wss://{host}")
	if !strings.Contains(string(caddy), `Content-Security-Policy "`+policy+`"`) {
		t.Fatalf("Caddy must use dev CSP %q", policy)
	}
}
