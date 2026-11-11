// Package admin serves aggregate stats and hard-delete of one profile.
//
// Every route requires a session and an email in ADMIN_EMAILS. Anyone else
// gets 404 from httpx.RequireAdmin, so the route is not advertised.
package admin

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rhamdeew/dbprc/backend/internal/config"
	"github.com/rhamdeew/dbprc/backend/internal/db"
	"github.com/rhamdeew/dbprc/backend/internal/db/sqlc"
	"github.com/rhamdeew/dbprc/backend/internal/httpx"
	"github.com/rhamdeew/dbprc/backend/internal/session"
	"github.com/rhamdeew/dbprc/backend/internal/upload"
)

const actionDeleteUser = "delete_user"

var (
	errNotFound = errors.New("profile not found")
	errSelf     = errors.New("self delete")
)

type statsJSON struct {
	Users          int64 `json:"users"`
	AvailableUsers int64 `json:"availableUsers"`
	Chats          int64 `json:"chats"`
	Messages       int64 `json:"messages"`
	WallPosts      int64 `json:"wallPosts"`
	Photos         int64 `json:"photos"`
}

type previewJSON struct {
	Exists        bool  `json:"exists"`
	WallPosts     int64 `json:"wallPosts"`
	AuthoredPosts int64 `json:"authoredPosts"`
	Photos        int64 `json:"photos"`
	Chats         int64 `json:"chats"`
	Messages      int64 `json:"messages"`
}

type deleteResult struct {
	targetID int64
	hash     string
	paths    []string
}

type handlers struct {
	log     *slog.Logger
	db      *db.DB
	uploads *upload.Store
}

// Routes registers the admin endpoints.
// The caller wraps the mux with session LoadAndSave and CSRF.
func Routes(mux *http.ServeMux, cfg config.Config, log *slog.Logger, sessions *session.Manager, database *db.DB, uploads *upload.Store) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	h := &handlers{log: log, db: database, uploads: uploads}
	user := httpx.RequireUser(log, sessions.UserID, sqlc.New(database.Reader()).GetUser, sessions.Destroy)
	adminOnly := httpx.RequireAdmin(cfg.IsAdmin)
	mux.Handle("GET /api/admin/stats", user(adminOnly(http.HandlerFunc(h.stats))))
	mux.Handle("GET /api/admin/users/preview", user(adminOnly(http.HandlerFunc(h.preview))))
	mux.Handle("DELETE /api/admin/users", user(adminOnly(httpx.LimitBody(httpx.JSONBodyLimit)(http.HandlerFunc(h.deleteUser)))))
}

func (h *handlers) stats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.loadStats(r.Context())
	if err != nil {
		h.log.Error("admin stats", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, stats)
}

func (h *handlers) loadStats(ctx context.Context) (statsJSON, error) {
	tx, err := h.db.Reader().BeginTx(ctx, nil)
	if err != nil {
		return statsJSON{}, err
	}
	defer func() { _ = tx.Rollback() }()

	q := sqlc.New(tx)
	var stats statsJSON
	if stats.Users, err = q.CountUsers(ctx); err != nil {
		return statsJSON{}, err
	}
	if stats.AvailableUsers, err = q.CountAvailableUsers(ctx); err != nil {
		return statsJSON{}, err
	}
	if stats.Chats, err = q.CountChats(ctx); err != nil {
		return statsJSON{}, err
	}
	if stats.Messages, err = q.CountMessages(ctx); err != nil {
		return statsJSON{}, err
	}
	if stats.WallPosts, err = q.CountWallPosts(ctx); err != nil {
		return statsJSON{}, err
	}
	if stats.Photos, err = q.CountPhotos(ctx); err != nil {
		return statsJSON{}, err
	}
	return stats, nil
}

func (h *handlers) preview(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.URL.Query().Get("email"))
	if email == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid", "Enter an email address.")
		return
	}
	body, err := h.loadPreview(r.Context(), email)
	if err != nil {
		h.log.Error("admin preview", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, body)
}

func (h *handlers) loadPreview(ctx context.Context, email string) (previewJSON, error) {
	tx, err := h.db.Reader().BeginTx(ctx, nil)
	if err != nil {
		return previewJSON{}, err
	}
	defer func() { _ = tx.Rollback() }()

	q := sqlc.New(tx)
	user, err := q.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return previewJSON{}, nil
		}
		return previewJSON{}, err
	}
	counts, err := userCounts(ctx, q, user.ID)
	if err != nil {
		return previewJSON{}, err
	}
	counts.Exists = true
	return counts, nil
}

func userCounts(ctx context.Context, q *sqlc.Queries, userID int64) (previewJSON, error) {
	var counts previewJSON
	var err error
	if counts.WallPosts, err = q.CountWallPostsByOwner(ctx, userID); err != nil {
		return previewJSON{}, err
	}
	if counts.AuthoredPosts, err = q.CountWallPostsByAuthor(ctx, userID); err != nil {
		return previewJSON{}, err
	}
	if counts.Photos, err = q.CountUserPhotos(ctx, userID); err != nil {
		return previewJSON{}, err
	}
	if counts.Chats, err = q.CountChatsByUser(ctx, userID); err != nil {
		return previewJSON{}, err
	}
	if counts.Messages, err = q.CountMessagesByUser(ctx, userID); err != nil {
		return previewJSON{}, err
	}
	return counts, nil
}

func (h *handlers) deleteUser(w http.ResponseWriter, r *http.Request) {
	me, ok := httpx.UserFrom(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}
	email, ok := readEmail(w, r)
	if !ok {
		return
	}
	result, err := h.removeUser(r.Context(), me, email)
	switch {
	case err == nil:
	case errors.Is(err, errNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "No profile with that email.")
		return
	case errors.Is(err, errSelf):
		httpx.WriteError(w, http.StatusConflict, "self_delete", "You can't delete your own account.")
		return
	default:
		h.log.Error("admin delete", "err", err, "admin_id", me.ID)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	for _, path := range result.paths {
		if err := h.removeStored(path); err != nil {
			h.log.Error("admin delete file", "err", err, "admin_id", me.ID, "target_id", result.targetID)
			httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
			return
		}
	}
	h.log.Info("admin delete", "admin_id", me.ID, "target_id", result.targetID, "target_email_sha256", result.hash)
	w.WriteHeader(http.StatusNoContent)
}

// removeUser deletes the account in one transaction. File paths are returned
// so the caller can remove them after commit.
func (h *handlers) removeUser(ctx context.Context, me sqlc.User, email string) (deleteResult, error) {
	tx, err := h.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return deleteResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	q := sqlc.New(tx)
	user, err := q.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return deleteResult{}, errNotFound
		}
		return deleteResult{}, err
	}
	if user.ID == me.ID {
		return deleteResult{}, errSelf
	}
	paths, err := uploadPaths(ctx, q, user.ID)
	if err != nil {
		return deleteResult{}, err
	}
	if err := deleteUserSessions(ctx, q, user.ID); err != nil {
		return deleteResult{}, err
	}
	if err := q.DeleteUser(ctx, user.ID); err != nil {
		return deleteResult{}, err
	}
	hash := emailSHA256(user.Email)
	action := actionDeleteUser
	adminEmail := me.Email
	if err := q.InsertAdminAudit(ctx, sqlc.InsertAdminAuditParams{
		AdminEmail:        &adminEmail,
		Action:            &action,
		TargetEmailSha256: &hash,
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		return deleteResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return deleteResult{}, err
	}
	return deleteResult{targetID: user.ID, hash: hash, paths: paths}, nil
}

func uploadPaths(ctx context.Context, q *sqlc.Queries, userID int64) ([]string, error) {
	avatar, err := q.ListAvatarPath(ctx, userID)
	if err != nil {
		return nil, err
	}
	photos, err := q.ListPhotoPathsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	drawings, err := q.ListDrawingPathsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, 1+len(photos)+len(drawings))
	if avatar != nil {
		paths = append(paths, *avatar)
	}
	paths = append(paths, photos...)
	for _, drawing := range drawings {
		if drawing != nil {
			paths = append(paths, *drawing)
		}
	}
	return uniquePaths(paths), nil
}

func uniquePaths(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, path := range in {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}

// deleteUserSessions removes session rows for userID. sessions has no user
// column and no foreign key, so the blob's uid is the only link.
func deleteUserSessions(ctx context.Context, q *sqlc.Queries, userID int64) error {
	rows, err := q.ListSessionRows(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		id, ok := session.SessionUserID(row.Data)
		if !ok || id != userID {
			continue
		}
		if err := q.DeleteSessionByToken(ctx, row.Token); err != nil {
			return err
		}
	}
	return nil
}

func (h *handlers) removeStored(rel string) error {
	if strings.TrimSpace(rel) == "" {
		return nil
	}
	if h.uploads == nil || h.uploads.Dir == "" {
		return errors.New("upload directory is not configured")
	}
	err := h.uploads.Delete(rel)
	// Delete wraps the os error, and os.IsNotExist does not unwrap that.
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func readEmail(w http.ResponseWriter, r *http.Request) (string, bool) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, httpx.JSONBodyLimit))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			httpx.WriteError(w, http.StatusBadRequest, "too_large", "Request body is too large.")
			return "", false
		}
		httpx.WriteError(w, http.StatusBadRequest, "invalid", "Check the request and try again.")
		return "", false
	}
	var body struct {
		Email *string `json:"email"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || body.Email == nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid", "Enter an email address.")
		return "", false
	}
	email := strings.TrimSpace(*body.Email)
	if email == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid", "Enter an email address.")
		return "", false
	}
	return email, true
}

// emailSHA256 is the hex SHA-256 of the trimmed, lower-cased email.
func emailSHA256(email string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(sum[:])
}
