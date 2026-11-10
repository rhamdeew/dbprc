// Package server wires HTTP routes. Features register their routes in New.
package server

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/rhamdeew/dbprc/backend/internal/auth"
	"github.com/rhamdeew/dbprc/backend/internal/config"
	"github.com/rhamdeew/dbprc/backend/internal/db"
	"github.com/rhamdeew/dbprc/backend/internal/httpx"
	"github.com/rhamdeew/dbprc/backend/internal/session"
	"github.com/rhamdeew/dbprc/backend/internal/upload"
)

// Deps are the process-wide collaborators the router needs.
type Deps struct {
	DB  *db.DB
	Log *slog.Logger
}

// Server is the root HTTP handler. Close stops the session cleanup goroutine;
// call it before closing the database.
type Server struct {
	handler  http.Handler
	sessions *session.Manager
	db       *db.DB
}

// New builds the root handler. deps.DB is required.
// GET /healthz, GET /api/version, and GET /auth/google/* are public.
// GET /api/me requires a session.
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
	auth.Routes(mux, cfg, log, sessions, deps.DB, upload.New(cfg.UploadDir))

	var handler http.Handler = mux
	handler = sessions.LoadAndSave(handler)
	handler = httpx.CSRF(cfg.AppBaseURL)(handler)
	handler = logRequests(log, handler)

	return &Server{handler: handler, sessions: sessions, db: deps.DB}
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

// Close stops the session store's cleanup goroutine.
func (s *Server) Close() {
	if s == nil {
		return
	}
	s.sessions.Close()
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
