package server_test

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rhamdeew/dbprc/backend/internal/config"
	"github.com/rhamdeew/dbprc/backend/internal/testutil"
)

func TestStaticUploadsRequireSession(t *testing.T) {
	staticDir, uploadDir := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("INDEX"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "avatar.png"), []byte("PRIVATE IMAGE"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := testutil.NewServer(t, testutil.WithConfig(config.Config{Env: "test", StaticDir: staticDir, UploadDir: uploadDir}))
	database := testutil.Database(t, srv)
	user := testutil.CreateUser(t, database)
	signedIn := testutil.LoginAs(t, srv, user)
	for _, tt := range []struct {
		name, method, path string
		client             *http.Client
		status             int
	}{
		{"anonymous", "GET", "/uploads/avatar.png", srv.Client(), 401},
		{"anonymous head", "HEAD", "/uploads/avatar.png", srv.Client(), 401},
		{"signed in", "GET", "/uploads/avatar.png", signedIn, 200},
		{"signed in head", "HEAD", "/uploads/avatar.png", signedIn, 200},
		{"missing", "GET", "/uploads/missing.png", signedIn, 404},
		{"directory", "GET", "/uploads/", signedIn, 404},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, srv.URL+tt.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := tt.client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tt.status {
				t.Fatalf("status %d body %s", resp.StatusCode, body)
			}
			if tt.status != 200 && strings.Contains(string(body), "PRIVATE IMAGE") {
				t.Fatal("leaked upload")
			}
			if tt.status == 200 {
				if !strings.HasPrefix(resp.Header.Get("Cache-Control"), "private, max-age=31536000, immutable") {
					t.Fatalf("cache %v", resp.Header)
				}
				if resp.Header.Get("Content-Disposition") != "inline" {
					t.Fatalf("disposition %v", resp.Header)
				}
				if tt.method == "GET" && string(body) != "PRIVATE IMAGE" {
					t.Fatalf("body %q", body)
				}
			}
		})
	}
	// Expired and deleted-user sessions must not reveal a cached/conditional file.
	for _, state := range []string{"expired", "deleted"} {
		t.Run(state, func(t *testing.T) {
			u := testutil.CreateUser(t, database)
			client := testutil.LoginAs(t, srv, u)
			if state == "expired" {
				if _, err := database.Writer().ExecContext(t.Context(), "UPDATE sessions SET expiry = julianday('now', '-1 day')"); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := database.Writer().ExecContext(t.Context(), "DELETE FROM users WHERE id = ?", u.ID); err != nil {
					t.Fatal(err)
				}
			}
			req, err := http.NewRequest("GET", srv.URL+"/uploads/avatar.png", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("If-Modified-Since", "Wed, 31 Dec 2099 23:59:59 GMT")
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != 401 {
				t.Fatalf("status %d", resp.StatusCode)
			}
		})
	}
}
