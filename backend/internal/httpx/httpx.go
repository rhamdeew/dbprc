// Package httpx holds HTTP helpers shared by feature handlers: JSON errors,
// the signed-in user in the request context, and middleware every
// authenticated route uses.
package httpx

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/rhamdeew/dbprc/backend/internal/db/sqlc"
)

type contextKey int

const userKey contextKey = 1

// Error is the JSON body of a failed request.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteJSON writes v as JSON with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes an API error body.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, Error{Code: code, Message: message})
}

// WithUser stores the signed-in user on ctx.
func WithUser(ctx context.Context, user sqlc.User) context.Context {
	return context.WithValue(ctx, userKey, user)
}

// UserFrom returns the user stored by RequireUser.
func UserFrom(ctx context.Context) (sqlc.User, bool) {
	user, ok := ctx.Value(userKey).(sqlc.User)
	return user, ok
}

// RequireUser loads the session user and stores them on the request context.
//
// userID, load, and destroy run only after session middleware has loaded the
// session into the request context. A missing session or a user who is no
// longer in the database is 401 {"code":"unauthenticated"}. A deleted user
// also has their session destroyed. load is typically (*sqlc.Queries).GetUser.
func RequireUser(
	log *slog.Logger,
	userID func(context.Context) (int64, bool),
	load func(context.Context, int64) (sqlc.User, error),
	destroy func(context.Context) error,
) func(http.Handler) http.Handler {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, ok := userID(r.Context())
			if !ok {
				WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in required.")
				return
			}
			user, err := load(r.Context(), id)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					if derr := destroy(r.Context()); derr != nil {
						log.Error("destroy session", "err", derr)
						WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
						return
					}
					WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in required.")
					return
				}
				log.Error("load user", "err", err)
				WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
		})
	}
}

// RequireAdmin allows the request only when the session user's email is an admin.
//
// Anyone else, including a request with no user in the context, gets 404
// {"code":"not_found"}. That is the same status as an unknown route, so the
// handler does not tell a non-admin that the endpoint exists. Call it inside
// RequireUser. isAdmin is typically config.Config.IsAdmin; a nil predicate
// allows nobody.
func RequireAdmin(isAdmin func(email string) bool) func(http.Handler) http.Handler {
	if isAdmin == nil {
		isAdmin = func(string) bool { return false }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := UserFrom(r.Context())
			if !ok || !isAdmin(user.Email) {
				WriteError(w, http.StatusNotFound, "not_found", "Not found.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
