package testutil

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rhamdeew/dbprc/backend/internal/config"
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

// NewServer starts an httptest.Server with the full application router.
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
	srv := httptest.NewServer(server.New(o.cfg, o.log))
	t.Cleanup(srv.Close)
	return srv
}

// sessionTransport sends Origin on mutating requests and, once set, a session cookie.
//
// cookie stays nil until ARC-16 inserts a session for the user into the session
// store and assigns the cookie here.
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
// check against the test server succeeds. It does not send a session cookie:
// the session store arrives in ARC-16, which should insert a session for user
// and set sessionTransport.cookie on the client returned here.
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
	client.Transport = &sessionTransport{base: base.Transport, origin: srv.URL}
	return &client
}
