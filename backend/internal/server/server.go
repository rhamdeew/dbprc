// Package server wires HTTP routes. Features register their routes in New.
package server

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/rhamdeew/dbprc/backend/internal/admin"
	"github.com/rhamdeew/dbprc/backend/internal/auth"
	"github.com/rhamdeew/dbprc/backend/internal/chat"
	"github.com/rhamdeew/dbprc/backend/internal/config"
	"github.com/rhamdeew/dbprc/backend/internal/db"
	"github.com/rhamdeew/dbprc/backend/internal/httpx"
	"github.com/rhamdeew/dbprc/backend/internal/profile"
	"github.com/rhamdeew/dbprc/backend/internal/realtime"
	"github.com/rhamdeew/dbprc/backend/internal/session"
	"github.com/rhamdeew/dbprc/backend/internal/upload"
	"github.com/rhamdeew/dbprc/backend/internal/wall"
)

// Deps are the process-wide collaborators the router needs.
type Deps struct {
	DB  *db.DB
	Log *slog.Logger
	// ChatNotifier receives chat events after they commit.
	// Nil uses the in-memory realtime hub, which also serves GET /ws.
	// A non-nil value replaces the hub (tests pass a recorder).
	ChatNotifier chat.Notifier
}

// Server is the root HTTP handler. Close stops the session cleanup goroutine;
// call it before closing the database.
type Server struct {
	handler  http.Handler
	sessions *session.Manager
	db       *db.DB
	hub      *realtime.Hub
}

// New builds the root handler. deps.DB is required.
// GET /healthz, GET /api/version, and GET /auth/google/* are public.
// GET /api/me, the profile routes (including colleague search), the wall routes,
// the chat routes, the admin routes, and GET /ws require a session.
// Admin routes also require an email in ADMIN_EMAILS; anyone else gets 404.
// When cfg.StaticDir is set, other GET and HEAD requests serve that directory.
func New(cfg config.Config, deps Deps) *Server {
	if deps.DB == nil {
		panic("server: nil database")
	}
	log := deps.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	sessions := session.New(deps.DB.Writer(), session.Settings{
		CookieName: cfg.SessionCookie,
		Secure:     cfg.Env != "dev",
	}, log)
	sessions.ErrorFunc = func(w http.ResponseWriter, _ *http.Request, err error) {
		log.Error("session", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": cfg.Version})
	})
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"version": cfg.Version, "env": cfg.Env})
	})
	uploads := upload.New(cfg.UploadDir)
	auth.Routes(mux, cfg, log, sessions, deps.DB, uploads)
	profile.Routes(mux, log, sessions, deps.DB, uploads)
	wall.Routes(mux, log, sessions, deps.DB, uploads)
	admin.Routes(mux, cfg, log, sessions, deps.DB, uploads)

	notifier := deps.ChatNotifier
	var hub *realtime.Hub
	if notifier == nil {
		hub = realtime.New(deps.DB, log, cfg.AppBaseURL)
		notifier = hub
	}
	chat.Routes(mux, log, sessions, deps.DB, notifier)
	realtime.Routes(mux, log, sessions, hub)

	var handler http.Handler = mux
	handler = sessions.LoadAndSave(handler)
	handler = httpx.CSRF(cfg.AppBaseURL)(handler)
	handler = logRequests(log, handler)
	// Outside the session middleware so assets do not touch the database.
	// No-op unless STATIC_DIR is set (dev and E2E).
	handler = serveDevStatic(handler, cfg)

	return &Server{handler: handler, sessions: sessions, db: deps.DB, hub: hub}
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

// Close stops WebSocket handlers, then the session store's cleanup goroutine.
// Call it before http.Server.Shutdown: Shutdown does not close hijacked sockets.
func (s *Server) Close() {
	if s == nil {
		return
	}
	if s.hub != nil {
		s.hub.Close()
	}
	s.sessions.Close()
}

// WSConnCount is the number of open WebSocket connections for userID.
// The HTTP 101 is written before the socket is registered, so tests wait on this.
func (s *Server) WSConnCount(userID int64) int {
	if s == nil || s.hub == nil {
		return 0
	}
	return s.hub.ConnCount(userID)
}

// DB is the database this server uses.
func (s *Server) DB() *db.DB { return s.db }

// MintSession inserts a session for userID and returns its cookie.
func (s *Server) MintSession(ctx context.Context, userID int64) (*http.Cookie, error) {
	return s.sessions.Mint(ctx, userID)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap lets websocket.Accept find the http.Hijacker under the logger
// and the session middleware. Without it, GET /ws cannot upgrade.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if r.URL.Path == "/healthz" {
			return
		}
		log.Info("http", "method", r.Method, "path", r.URL.Path, "status", rec.status)
	})
}
