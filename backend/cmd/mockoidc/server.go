package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oauth2-proxy/mockoidc"
)

const (
	nextUserPath = "/_mock/next-user"
	avatarPath   = "/_mock/avatar.png"
)

// server is a mockoidc provider plus POST /_mock/next-user.
type server struct {
	oidc       *mockoidc.MockOIDC
	http       *http.Server
	publicHost string
	log        *slog.Logger

	// interactive makes an authorize request with an empty queue show the
	// sign-in form. It is off for E2E, where the queue decides who signs in.
	interactive atomic.Bool

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
	mux.HandleFunc("GET "+avatarPath, handleAvatar)

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
//
// Interactive mode adds one step in front: with nothing queued, the first GET
// shows a form, and the form submits back here with mock_email (and
// mock_name). That identity is used for this request only.
func (s *server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	queued := len(s.queue) > 0
	if !queued && s.interactive.Load() && r.Method == http.MethodGet {
		picked, shown := s.pickIdentity(w, r)
		if shown {
			return
		}
		s.oidc.QueueUser(picked)
		s.oidc.Authorize(w, r)
		s.drainLibraryQueue()
		return
	}
	user := s.popLocked()
	s.oidc.QueueUser(user)
	s.oidc.Authorize(w, r)
	leftover := s.drainLibraryQueue()
	if queued && len(leftover) > 0 {
		s.queue = append(leftover, s.queue...)
	}
}

var pickTmpl = template.Must(template.New("pick").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Mock Google sign-in</title>
<style>body{font:13px Tahoma,Verdana,sans-serif;max-width:360px;margin:60px auto;padding:0 16px}
input,button{display:block;width:100%;box-sizing:border-box;margin:6px 0 12px;padding:6px}
.error{color:#b00020}.hint{color:#666}</style>
</head><body><h1>Mock Google sign-in</h1>
<p class="hint">Local development only. Any @{{.Domain}} address signs in; {{.Default}} is the default. Use a second browser profile or a private window to be two people at once.</p>
{{if .Error}}<p class="error" role="alert">{{.Error}}</p>{{end}}
<form method="get" action="">
{{range $k, $v := .Params}}{{range $v}}<input type="hidden" name="{{$k}}" value="{{.}}">{{end}}{{end}}
<label>Email<input name="mock_email" type="email" value="{{.Email}}" autofocus required></label>
<label>Name (optional)<input name="mock_name" value="{{.Name}}"></label>
<button type="submit">Sign in</button>
</form></body></html>`))

// pickIdentity returns the identity named by mock_email. When the parameter
// is missing or not an email it writes the form and reports shown.
// hd is the email's domain, the way Google Workspace sets it, so an address
// outside ALLOWED_DOMAIN exercises the app's domain error.
func (s *server) pickIdentity(w http.ResponseWriter, r *http.Request) (user *identity, shown bool) {
	q := r.URL.Query()
	email := strings.TrimSpace(q.Get("mock_email"))
	name := strings.TrimSpace(q.Get("mock_name"))
	if email != "" && validEmail(email) {
		at := strings.LastIndex(email, "@")
		if name == "" {
			name = email[:at]
		}
		user = newIdentity(email, name, strings.ToLower(email[at+1:]), true)
		user.Picture = s.origin() + avatarPath + "?seed=" + url.QueryEscape(email)
		return user, false
	}
	params := url.Values{}
	for key, values := range q {
		if key != "mock_email" && key != "mock_name" {
			params[key] = values
		}
	}
	data := map[string]any{
		"Params":  params,
		"Domain":  defaultHD,
		"Default": defaultEmail,
		"Email":   defaultEmail,
		"Name":    name,
		"Error":   "",
	}
	if email != "" {
		data["Email"] = email
		data["Error"] = "Enter a full email address."
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := pickTmpl.Execute(w, data); err != nil {
		s.log.Warn("sign-in form", "err", err.Error())
	}
	return nil, true
}

// handleAvatar is a checkerboard whose color depends on ?seed=, so two mock
// people are easy to tell apart.
func handleAvatar(w http.ResponseWriter, r *http.Request) {
	sum := sha256.Sum256([]byte(r.URL.Query().Get("seed")))
	fill := color.RGBA{R: sum[0]/2 + 64, G: sum[1]/2 + 64, B: sum[2]/2 + 64, A: 255}
	const size, cell = 96, 24
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			if (x/cell+y/cell)%2 == 0 {
				img.Set(x, y, fill)
			} else {
				img.Set(x, y, color.White)
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		http.Error(w, "avatar", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(buf.Bytes())
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
