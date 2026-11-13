package session_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rhamdeew/dbprc/backend/internal/session"
	"github.com/rhamdeew/dbprc/backend/internal/testutil"
)

func TestSessionUserID(t *testing.T) {
	if _, ok := session.SessionUserID(nil); ok {
		t.Fatal("nil blob")
	}
	if _, ok := session.SessionUserID([]byte{0, 1, 2}); ok {
		t.Fatal("garbage blob")
	}

	database := testutil.NewDB(t)
	mgr := session.New(database.Writer(), session.Settings{Secure: false}, nil)
	t.Cleanup(mgr.Close)
	user := testutil.CreateUser(t, database)
	other := testutil.CreateUser(t, database)
	cookie, err := mgr.Mint(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	var data []byte
	if err := database.Writer().QueryRowContext(t.Context(), `SELECT data FROM sessions WHERE token = ?`, cookie.Value).Scan(&data); err != nil {
		t.Fatal(err)
	}
	id, ok := session.SessionUserID(data)
	if !ok || id != user.ID {
		t.Fatalf("id %d ok %v, want %d", id, ok, user.ID)
	}
	if id == other.ID {
		t.Fatal("session matched the other user")
	}
}

func TestManagerCookieAndIdleExpiry(t *testing.T) {
	database := testutil.NewDB(t)
	tests := []struct {
		name       string
		cookieName string
		secure     bool
		wantName   string
	}{
		{name: "defaults are secure", secure: true, wantName: "arc_sid"},
		{name: "dev cookie", cookieName: "dev_sid", secure: false, wantName: "dev_sid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := session.New(database.Writer(), session.Settings{CookieName: tt.cookieName, Secure: tt.secure}, nil)
			t.Cleanup(mgr.Close)
			if mgr.Lifetime != session.Lifetime || mgr.IdleTimeout != session.IdleTimeout {
				t.Fatalf("lifetime %s idle %s", mgr.Lifetime, mgr.IdleTimeout)
			}
			if session.Lifetime != 30*24*time.Hour || session.IdleTimeout != 7*24*time.Hour {
				t.Fatalf("constants %s %s", session.Lifetime, session.IdleTimeout)
			}

			var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mgr.PutUserID(r.Context(), 42)
				w.WriteHeader(http.StatusNoContent)
			})
			handler = mgr.LoadAndSave(handler)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
			res := rec.Result()
			defer func() { _ = res.Body.Close() }()
			if res.StatusCode != http.StatusNoContent {
				t.Fatalf("status %d", res.StatusCode)
			}
			cookies := res.Cookies()
			if len(cookies) != 1 {
				t.Fatalf("cookies %d", len(cookies))
			}
			cookie := cookies[0]
			if cookie.Name != tt.wantName || cookie.Value == "" || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Secure != tt.secure || cookie.Path != "/" {
				t.Fatalf("cookie %#v", cookie)
			}
			maxAge := cookie.MaxAge
			const day = 24 * 60 * 60
			if maxAge < 6*day || maxAge > 8*day {
				t.Fatalf("Max-Age %d, want about 7 days", maxAge)
			}

			var days float64
			if err := database.Writer().QueryRowContext(t.Context(), `SELECT expiry - julianday('now') FROM sessions WHERE token = ?`, cookie.Value).Scan(&days); err != nil {
				t.Fatal(err)
			}
			if days < 6.9 || days > 7.1 {
				t.Fatalf("store expiry %.3f days", days)
			}

			var got int64
			var ok bool
			read := mgr.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got, ok = mgr.UserID(r.Context())
				w.WriteHeader(http.StatusNoContent)
			}))
			rec = httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.AddCookie(cookie)
			read.ServeHTTP(rec, req)
			if !ok || got != 42 {
				t.Fatalf("user %d ok %v", got, ok)
			}

			if _, err := database.Writer().ExecContext(t.Context(), `UPDATE sessions SET expiry = julianday('now', '-1 day') WHERE token = ?`, cookie.Value); err != nil {
				t.Fatal(err)
			}
			got, ok = 0, true
			rec = httptest.NewRecorder()
			req = httptest.NewRequest(http.MethodGet, "/", nil)
			req.AddCookie(cookie)
			read.ServeHTTP(rec, req)
			if ok || got != 0 {
				t.Fatalf("expired user %d ok %v", got, ok)
			}
		})
	}
}

func TestMintRoundTrip(t *testing.T) {
	database := testutil.NewDB(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr := session.New(database.Writer(), session.Settings{Secure: true}, log)
	t.Cleanup(func() {
		mgr.Close()
		mgr.Close()
	})
	cookie, err := mgr.Mint(t.Context(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if cookie.Name != "arc_sid" || !cookie.HttpOnly || !cookie.Secure || cookie.Value == "" {
		t.Fatalf("cookie %#v", cookie)
	}
	ctx, err := mgr.Load(t.Context(), cookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	id, ok := mgr.UserID(ctx)
	if !ok || id != 7 {
		t.Fatalf("id %d ok %v", id, ok)
	}
	empty, err := mgr.Load(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := mgr.UserID(empty); ok {
		t.Fatal("empty session had a user")
	}
}

func TestCloseNil(t *testing.T) {
	var mgr *session.Manager
	mgr.Close()
}
