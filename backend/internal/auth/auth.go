// Package auth serves Google sign-in, the signed-in user, and logout.
package auth

import (
	"log/slog"
	"net/http"

	"github.com/rhamdeew/dbprc/backend/internal/config"
	"github.com/rhamdeew/dbprc/backend/internal/db"
	"github.com/rhamdeew/dbprc/backend/internal/db/sqlc"
	"github.com/rhamdeew/dbprc/backend/internal/httpx"
	"github.com/rhamdeew/dbprc/backend/internal/session"
	"github.com/rhamdeew/dbprc/backend/internal/upload"
)

type meResponse struct {
	ID        int64   `json:"id"`
	Email     string  `json:"email"`
	Name      string  `json:"name"`
	AvatarURL *string `json:"avatarUrl"`
	Available bool    `json:"available"`
	IsAdmin   bool    `json:"isAdmin"`
}

// Routes registers GET /api/me, POST /auth/logout, and the Google OIDC endpoints.
// The caller wraps the mux with session LoadAndSave and CSRF.
// database is used for the user upsert (writer) and for GET /api/me (reader).
// uploads stores the Google picture on first login; a nil or unconfigured store skips the avatar.
func Routes(mux *http.ServeMux, cfg config.Config, log *slog.Logger, sessions *session.Manager, database *db.DB, uploads *upload.Store) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	mux.Handle("GET /api/me", httpx.RequireUser(log, sessions.UserID, sqlc.New(database.Reader()).GetUser, sessions.Destroy)(Me(cfg)))
	mux.Handle("POST /auth/logout", httpx.LimitBody(httpx.SmallBodyLimit)(Logout(sessions)))
	google := newGoogleLogin(cfg, log, sessions, database, uploads)
	mux.HandleFunc("GET /auth/google/start", google.start)
	mux.HandleFunc("GET /auth/google/callback", google.callback)
}

// Me writes the signed-in user. RequireUser must have run first.
func Me(cfg config.Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := httpx.UserFrom(r.Context())
		if !ok {
			httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in required.")
			return
		}
		var avatar *string
		if user.AvatarPath != nil && *user.AvatarPath != "" {
			u := upload.URL(*user.AvatarPath)
			avatar = &u
		}
		httpx.WriteJSON(w, http.StatusOK, meResponse{
			ID:        user.ID,
			Email:     user.Email,
			Name:      user.Name,
			AvatarURL: avatar,
			Available: user.Available != 0,
			IsAdmin:   cfg.IsAdmin(user.Email),
		})
	})
}

// Logout destroys the session and returns 204. It is safe to call with no session.
func Logout(sessions *session.Manager) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := sessions.Destroy(r.Context()); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
