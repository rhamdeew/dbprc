package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/oauth2-proxy/mockoidc"
)

const nextUserPath = "/_mock/next-user"

// server is a mockoidc provider plus POST /_mock/next-user.
type server struct {
	oidc       *mockoidc.MockOIDC
	http       *http.Server
	publicHost string
	log        *slog.Logger

	mu    sync.Mutex
	queue []*identity

	wg       sync.WaitGroup
	serveErr chan error

	// conns tracks live sockets so shutdown can close a StateNew dial
	// that never sent a request. http.Server will not.
	connMu sync.Mutex
	conns  map[net.Conn]http.ConnState
}

func newServer(ctx context.Context, listen, host string, log *slog.Logger) (*server, error) {
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", listen)
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}
	if host == "" {
		host = ln.Addr().String()
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	oidc, err := mockoidc.NewServer(nil)
	if err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("mock oidc: %w", err)
	}
	oidc.ClientID = devClientID
	oidc.ClientSecret = devClientSecret

	srv := &server{
		oidc:       oidc,
		publicHost: host,
		log:        log,
		serveErr:   make(chan error, 1),
		conns:      map[net.Conn]http.ConnState{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc(mockoidc.AuthorizationEndpoint, srv.handleAuthorize)
	mux.HandleFunc(mockoidc.TokenEndpoint, oidc.Token)
	mux.HandleFunc(mockoidc.UserinfoEndpoint, oidc.Userinfo)
	mux.HandleFunc(mockoidc.JWKSEndpoint, oidc.JWKS)
	mux.HandleFunc(mockoidc.DiscoveryEndpoint, oidc.Discovery)
	mux.HandleFunc("POST "+nextUserPath, srv.handleNextUser)

	srv.http = &http.Server{
		Addr:              host,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ConnState:         srv.trackConn,
	}
	// Issuer() and the discovery document read Server.Addr. It is the public
	// host, which may differ from the address we bound.
	oidc.Server = srv.http

	srv.wg.Add(1)
	go func() {
		defer srv.wg.Done()
		defer close(srv.serveErr)
		err := srv.http.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			srv.serveErr <- err
		}
	}()
	return srv, nil
}

func (s *server) origin() string { return "http://" + s.publicHost }

func (s *server) issuer() string { return s.origin() + mockoidc.IssuerBase }

func (s *server) shutdown(ctx context.Context) error {
	// Keep-alives and a Go Transport spare dial (accepted, no bytes yet)
	// sit in StateNew. Shutdown leaves those alone until they are 5s old,
	// which loses the race with this deadline. Close them first.
	s.http.SetKeepAlivesEnabled(false)
	s.closeNewConns()
	err := s.http.Shutdown(ctx)
	if err != nil {
		err = s.http.Close()
	}
	s.wg.Wait()
	return err
}

func (s *server) trackConn(c net.Conn, st http.ConnState) {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	if st == http.StateClosed || st == http.StateHijacked {
		delete(s.conns, c)
		return
	}
	s.conns[c] = st
}

func (s *server) closeNewConns() {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	for c, st := range s.conns {
		if st == http.StateNew {
			_ = c.Close()
		}
	}
}

// handleAuthorize pushes exactly one identity and then runs mockoidc.
// The library pops that identity only after the request validates, so a
// rejected authorize puts a queued identity back. A synthetic default user
// is dropped, so a stray 400 cannot block a later POST /_mock/next-user.
func (s *server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	queued := len(s.queue) > 0
	user := s.popLocked()
	s.oidc.QueueUser(user)
	s.oidc.Authorize(w, r)
	leftover := s.drainLibraryQueue()
	if queued && len(leftover) > 0 {
		s.queue = append(leftover, s.queue...)
	}
}

func (s *server) popLocked() *identity {
	if len(s.queue) == 0 {
		return defaultIdentity()
	}
	user := s.queue[0]
	s.queue[0] = nil
	s.queue = s.queue[1:]
	return user
}

func (s *server) drainLibraryQueue() []*identity {
	s.oidc.UserQueue.Lock()
	defer s.oidc.UserQueue.Unlock()
	out := make([]*identity, 0, len(s.oidc.UserQueue.Queue))
	for _, user := range s.oidc.UserQueue.Queue {
		if id, ok := user.(*identity); ok {
			out = append(out, id)
		}
	}
	s.oidc.UserQueue.Queue = nil
	return out
}

type nextUserResponse struct {
	Email         string `json:"email"`
	Name          string `json:"name"`
	HD            string `json:"hd"`
	EmailVerified bool   `json:"emailVerified"`
}

func (s *server) handleNextUser(w http.ResponseWriter, r *http.Request) {
	user, err := decodeNextUser(r)
	if err != nil {
		s.log.Warn("next user rejected", "err", err.Error())
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"code":    "invalid_body",
			"message": err.Error(),
		})
		return
	}
	s.mu.Lock()
	s.queue = append(s.queue, user)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, nextUserResponse{
		Email:         user.Email,
		Name:          user.Name,
		HD:            user.HD,
		EmailVerified: user.EmailVerified,
	})
}

func decodeNextUser(r *http.Request) (*identity, error) {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<16)
	defer func() { _ = r.Body.Close() }()

	var body struct {
		Email         string  `json:"email"`
		Name          *string `json:"name"`
		HD            *string `json:"hd"`
		EmailVerified *bool   `json:"emailVerified"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, errors.New("request body is too large")
		}
		return nil, errors.New("request body must be json")
	}
	email := strings.TrimSpace(body.Email)
	if email == "" {
		return nil, errors.New("email is required")
	}
	if !validEmail(email) {
		return nil, errors.New("email is invalid")
	}
	name := email[:strings.LastIndex(email, "@")]
	if body.Name != nil && strings.TrimSpace(*body.Name) != "" {
		name = strings.TrimSpace(*body.Name)
	}
	hd := defaultHD
	if body.HD != nil {
		hd = strings.TrimSpace(*body.HD)
	}
	verified := true
	if body.EmailVerified != nil {
		verified = *body.EmailVerified
	}
	return newIdentity(email, name, hd, verified), nil
}

func validEmail(email string) bool {
	if strings.ContainsAny(email, " \t\r\n") {
		return false
	}
	at := strings.LastIndex(email, "@")
	return at > 0 && at < len(email)-1
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
