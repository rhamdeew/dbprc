package testutil

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rhamdeew/dbprc/backend/internal/config"
	"github.com/rhamdeew/dbprc/backend/internal/db"
	"github.com/rhamdeew/dbprc/backend/internal/db/sqlc"
	"github.com/rhamdeew/dbprc/backend/internal/server"
)

// ServerOption changes the server started by NewServer.
type ServerOption func(*serverOptions)

type serverOptions struct {
	cfg config.Config
	log *slog.Logger
}

// WithConfig replaces the test config. The default is Env "test" and Version "test".
func WithConfig(cfg config.Config) ServerOption {
	return func(o *serverOptions) { o.cfg = cfg }
}

// WithLogger sets the server logger. The default discards logs.
func WithLogger(log *slog.Logger) ServerOption {
	return func(o *serverOptions) { o.log = log }
}

// NewServer starts an httptest.Server with the full application router and its
// own migrated database. When AppBaseURL is empty it is set to the server URL
// so LoginAs, which sends that URL as Origin, passes the CSRF check.
func NewServer(t *testing.T, opts ...ServerOption) *httptest.Server {
	t.Helper()
	o := serverOptions{
		cfg: config.Config{Env: "test", Version: "test"},
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	if o.log == nil {
		o.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	database := NewDB(t)
	unstarted := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	if o.cfg.AppBaseURL == "" {
		o.cfg.AppBaseURL = "http://" + unstarted.Listener.Addr().String()
	}
	app := server.New(o.cfg, server.Deps{DB: database, Log: o.log})
	unstarted.Config.Handler = app
	t.Cleanup(func() {
		unstarted.Close()
		app.Close()
	})
	unstarted.Start()
	return unstarted
}

// Database returns the database behind a server started by NewServer.
func Database(t *testing.T, srv *httptest.Server) *db.DB {
	t.Helper()
	if srv == nil || srv.Config == nil {
		t.Fatal("Database: nil server")
	}
	app, ok := srv.Config.Handler.(*server.Server)
	if !ok || app.DB() == nil {
		t.Fatal("Database: server has no database")
	}
	return app.DB()
}

// sessionTransport sends Origin on mutating requests and, once set, a session cookie.
//
// cookie is set by LoginAs when srv is an application server.
type sessionTransport struct {
	base   http.RoundTripper
	origin string
	cookie *http.Cookie
}

func (t *sessionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	if mutating(req.Method) {
		req.Header.Set("Origin", t.origin)
	}
	if t.cookie != nil {
		req.AddCookie(t.cookie)
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

func mutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// LoginAs returns a client that calls srv as user.
//
// The client sets Origin to srv.URL on POST, PUT, PATCH, and DELETE so a CSRF
// check against the test server succeeds when the server's APP_BASE_URL is that
// URL (NewServer does this when the config leaves AppBaseURL empty).
//
// When srv is an application server, LoginAs inserts a session for user and
// sends that cookie. A plain httptest.Server gets the Origin header and no cookie.
func LoginAs(t *testing.T, srv *httptest.Server, user sqlc.User) *http.Client {
	t.Helper()
	if srv == nil {
		t.Fatal("LoginAs: nil server")
	}
	if user.ID == 0 {
		t.Fatalf("LoginAs: user %q has no id", user.Email)
	}
	base := srv.Client()
	client := *base
	tr := &sessionTransport{base: base.Transport, origin: srv.URL}
	if srv.Config != nil {
		if app, ok := srv.Config.Handler.(*server.Server); ok {
			cookie, err := app.MintSession(t.Context(), user.ID)
			if err != nil {
				t.Fatalf("LoginAs: session: %v", err)
			}
			tr.cookie = cookie
		}
	}
	client.Transport = tr
	return &client
}

// SessionCookie returns the session cookie LoginAs attached, or nil.
func SessionCookie(client *http.Client) *http.Cookie {
	if client == nil {
		return nil
	}
	tr, ok := client.Transport.(*sessionTransport)
	if !ok || tr.cookie == nil {
		return nil
	}
	cookie := *tr.cookie
	return &cookie
}
