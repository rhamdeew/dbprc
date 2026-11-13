// Package profile serves a colleague's profile, colleague search, and the
// signed-in user's avatar, interests, and photos.
package profile

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rhamdeew/dbprc/backend/internal/db"
	"github.com/rhamdeew/dbprc/backend/internal/db/sqlc"
	"github.com/rhamdeew/dbprc/backend/internal/httpx"
	"github.com/rhamdeew/dbprc/backend/internal/session"
	"github.com/rhamdeew/dbprc/backend/internal/upload"
)

const (
	// maxInterestRunes is the limit after trimming. "500 chars" counts characters, not bytes.
	maxInterestRunes = 500
	maxPhotos        = 5
	// multipartSlack leaves room for boundaries around an image of upload.MaxBytes.
	multipartSlack = 1 << 20
)

var errPhotoLimit = errors.New("photo limit")

type interestsJSON struct {
	Hobby  string `json:"hobby"`
	Music  string `json:"music"`
	Movies string `json:"movies"`
	Other  string `json:"other"`
}

type photoJSON struct {
	ID  int64  `json:"id"`
	URL string `json:"url"`
}

type profileJSON struct {
	ID        int64         `json:"id"`
	Name      string        `json:"name"`
	Email     string        `json:"email"`
	AvatarURL *string       `json:"avatarUrl"`
	Interests interestsJSON `json:"interests"`
	Photos    []photoJSON   `json:"photos"`
	IsMe      bool          `json:"isMe"`
}

type avatarJSON struct {
	AvatarURL string `json:"avatarUrl"`
}

type handlers struct {
	log     *slog.Logger
	db      *db.DB
	uploads *upload.Store
}

// Routes registers the profile endpoints. Each one requires a session.
// The caller wraps the mux with session LoadAndSave and CSRF.
// Name and email are not writable here; Google sign-in is the only writer.
func Routes(mux *http.ServeMux, log *slog.Logger, sessions *session.Manager, database *db.DB, uploads *upload.Store) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	h := &handlers{log: log, db: database, uploads: uploads}
	guard := httpx.RequireUser(log, sessions.UserID, sqlc.New(database.Reader()).GetUser, sessions.Destroy)
	// Avatar and photo uploads share one bucket: 20 per user per minute.
	uploadLimit := httpx.NewLimiter(20, time.Minute)
	const uploadBody = int64(upload.MaxBytes + multipartSlack)
	mux.Handle("GET /api/users/search", guard(http.HandlerFunc(h.search)))
	mux.Handle("GET /api/users/{id}", guard(http.HandlerFunc(h.get)))
	mux.Handle("PUT /api/me/interests", guard(httpx.LimitBody(httpx.JSONBodyLimit)(http.HandlerFunc(h.putInterests))))
	mux.Handle("POST /api/me/avatar", guard(httpx.LimitBody(uploadBody)(httpx.RateLimit(uploadLimit)(http.HandlerFunc(h.postAvatar)))))
	mux.Handle("POST /api/me/photos", guard(httpx.LimitBody(uploadBody)(httpx.RateLimit(uploadLimit)(http.HandlerFunc(h.postPhoto)))))
	mux.Handle("DELETE /api/me/photos/{id}", guard(httpx.LimitBody(httpx.SmallBodyLimit)(http.HandlerFunc(h.deletePhoto))))
}

func (h *handlers) get(w http.ResponseWriter, r *http.Request) {
	me, ok := httpx.UserFrom(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in required.")
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "Profile not found.")
		return
	}

	tx, err := h.db.Reader().BeginTx(r.Context(), nil)
	if err != nil {
		h.log.Error("profile read", "err", err, "user_id", id)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	defer func() { _ = tx.Rollback() }()

	q := sqlc.New(tx)
	user, err := q.GetUser(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.WriteError(w, http.StatusNotFound, "not_found", "Profile not found.")
			return
		}
		h.log.Error("profile user", "err", err, "user_id", id)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	interests, err := loadInterests(r.Context(), q, id)
	if err != nil {
		h.log.Error("profile interests", "err", err, "user_id", id)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	rows, err := q.ListUserPhotos(r.Context(), id)
	if err != nil {
		h.log.Error("profile photos", "err", err, "user_id", id)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}

	photos := make([]photoJSON, 0, len(rows))
	for _, row := range rows {
		photos = append(photos, photoJSON{ID: row.ID, URL: upload.URL(row.Path)})
	}
	httpx.WriteJSON(w, http.StatusOK, profileJSON{
		ID:        user.ID,
		Name:      user.Name,
		Email:     user.Email,
		AvatarURL: avatarPointer(user.AvatarPath),
		Interests: interests,
		Photos:    photos,
		IsMe:      me.ID == user.ID,
	})
}

func (h *handlers) putInterests(w http.ResponseWriter, r *http.Request) {
	user, ok := httpx.UserFrom(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in required.")
		return
	}
	body, ok := decodeInterests(w, r)
	if !ok {
		return
	}
	// PUT replaces every field. A missing field is empty. Name and email in the
	// body are ignored; those columns are not updated.
	normalized, label, ok := normalizeInterests(body)
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "too_long", fmt.Sprintf("%s must be 500 characters or fewer.", label))
		return
	}
	row, err := sqlc.New(h.db.Writer()).UpsertInterests(r.Context(), sqlc.UpsertInterestsParams{
		UserID:    user.ID,
		Hobby:     normalized.Hobby,
		Music:     normalized.Music,
		Movies:    normalized.Movies,
		Other:     normalized.Other,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		h.log.Error("save interests", "err", err, "user_id", user.ID)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, interestsJSON{
		Hobby:  row.Hobby,
		Music:  row.Music,
		Movies: row.Movies,
		Other:  row.Other,
	})
}

func (h *handlers) postAvatar(w http.ResponseWriter, r *http.Request) {
	user, ok := httpx.UserFrom(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in required.")
		return
	}
	newPath, ok := h.saveUpload(w, r, upload.Avatar)
	if !ok {
		return
	}
	kept := false
	defer func() {
		if !kept {
			h.discardUpload(newPath)
		}
	}()

	old, err := h.swapAvatar(r.Context(), user.ID, newPath)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.WriteError(w, http.StatusNotFound, "not_found", "Profile not found.")
			return
		}
		h.log.Error("update avatar", "err", err, "user_id", user.ID)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	kept = true
	if old != "" && old != newPath {
		if err := h.removeStored(old); err != nil {
			h.log.Error("delete old avatar", "err", err, "user_id", user.ID)
			httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
			return
		}
	}
	httpx.WriteJSON(w, http.StatusOK, avatarJSON{AvatarURL: upload.URL(newPath)})
}

func (h *handlers) postPhoto(w http.ResponseWriter, r *http.Request) {
	user, ok := httpx.UserFrom(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in required.")
		return
	}
	newPath, ok := h.saveUpload(w, r, upload.Photo)
	if !ok {
		return
	}
	kept := false
	defer func() {
		if !kept {
			h.discardUpload(newPath)
		}
	}()

	// Save the file before taking the writer lock. The limit is checked again
	// in the same transaction as the insert, which is what rejects a 6th photo.
	row, err := h.insertPhoto(r.Context(), user.ID, newPath)
	if err != nil {
		if errors.Is(err, errPhotoLimit) {
			httpx.WriteError(w, http.StatusConflict, "photo_limit", "You can add up to 5 photos.")
			return
		}
		h.log.Error("insert photo", "err", err, "user_id", user.ID)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	kept = true
	httpx.WriteJSON(w, http.StatusCreated, photoJSON{ID: row.ID, URL: upload.URL(row.Path)})
}

func (h *handlers) deletePhoto(w http.ResponseWriter, r *http.Request) {
	user, ok := httpx.UserFrom(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in required.")
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "Photo not found.")
		return
	}
	path, err := sqlc.New(h.db.Writer()).DeleteUserPhoto(r.Context(), sqlc.DeleteUserPhotoParams{
		ID:     id,
		UserID: user.ID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.WriteError(w, http.StatusNotFound, "not_found", "Photo not found.")
			return
		}
		h.log.Error("delete photo", "err", err, "user_id", user.ID, "photo_id", id)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	if err := h.removeStored(path); err != nil {
		h.log.Error("delete photo file", "err", err, "user_id", user.ID, "photo_id", id)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) swapAvatar(ctx context.Context, userID int64, newPath string) (string, error) {
	tx, err := h.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	q := sqlc.New(tx)
	current, err := q.GetUser(ctx, userID)
	if err != nil {
		return "", err
	}
	if _, err := q.UpdateAvatarPath(ctx, sqlc.UpdateAvatarPathParams{
		AvatarPath: &newPath,
		ID:         userID,
	}); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	if current.AvatarPath == nil {
		return "", nil
	}
	return *current.AvatarPath, nil
}

// insertPhoto counts and inserts on one write transaction. A check outside
// the transaction would let two uploads both observe 4 photos and insert a 6th.
func (h *handlers) insertPhoto(ctx context.Context, userID int64, path string) (sqlc.Photo, error) {
	tx, err := h.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return sqlc.Photo{}, err
	}
	defer func() { _ = tx.Rollback() }()

	q := sqlc.New(tx)
	n, err := q.CountUserPhotos(ctx, userID)
	if err != nil {
		return sqlc.Photo{}, err
	}
	if n >= maxPhotos {
		return sqlc.Photo{}, errPhotoLimit
	}
	row, err := q.CreatePhoto(ctx, sqlc.CreatePhotoParams{
		UserID:    userID,
		Path:      path,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return sqlc.Photo{}, err
	}
	if err := tx.Commit(); err != nil {
		return sqlc.Photo{}, err
	}
	return row, nil
}

func (h *handlers) saveUpload(w http.ResponseWriter, r *http.Request, kind upload.Kind) (string, bool) {
	file, ok := openUpload(w, r)
	if !ok {
		return "", false
	}
	defer func() {
		_ = file.Close()
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	if h.uploads == nil || h.uploads.Dir == "" {
		h.log.Error("upload store is not configured")
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return "", false
	}
	path, err := h.uploads.Save(r.Context(), file, kind)
	if err != nil {
		writeImageError(h.log, w, err)
		return "", false
	}
	return path, true
}

func (h *handlers) discardUpload(rel string) {
	if err := h.removeStored(rel); err != nil {
		h.log.Warn("discard upload", "err", err)
	}
}

func (h *handlers) removeStored(rel string) error {
	if h.uploads == nil || strings.TrimSpace(rel) == "" {
		return nil
	}
	err := h.uploads.Delete(rel)
	// Delete wraps the os error, and os.IsNotExist does not unwrap that.
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func openUpload(w http.ResponseWriter, r *http.Request) (io.ReadCloser, bool) {
	// maxMemory only decides when a part spills to disk. MaxBytesReader is the cap.
	r.Body = http.MaxBytesReader(w, r.Body, upload.MaxBytes+multipartSlack)
	if err := r.ParseMultipartForm(8 << 20); err != nil { //nolint:gosec // G120: body is limited by MaxBytesReader above
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			httpx.WriteError(w, http.StatusBadRequest, "too_large", "Image must be 10MB or smaller.")
			return nil, false
		}
		httpx.WriteError(w, http.StatusBadRequest, "invalid", "Choose an image file.")
		return nil, false
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid", "Choose an image file.")
		return nil, false
	}
	return file, true
}

func writeImageError(log *slog.Logger, w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, upload.ErrTooLarge):
		httpx.WriteError(w, http.StatusBadRequest, "too_large", "Image must be 10MB or smaller.")
	case errors.Is(err, upload.ErrDimensions):
		httpx.WriteError(w, http.StatusBadRequest, "invalid_image", "That image is too big. Use one under 8000 pixels.")
	case errors.Is(err, upload.ErrInvalidImage):
		httpx.WriteError(w, http.StatusBadRequest, "invalid_image", "That image could not be read. Try another image.")
	case errors.Is(err, upload.ErrUnsupportedType), errors.Is(err, upload.ErrEmpty):
		httpx.WriteError(w, http.StatusBadRequest, "unsupported_type", "Use a JPEG, PNG, GIF, or WebP image.")
	default:
		log.Error("save image", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
	}
}

func decodeInterests(w http.ResponseWriter, r *http.Request) (interestsJSON, bool) {
	var body interestsJSON
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, httpx.JSONBodyLimit))
	if err := dec.Decode(&body); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			httpx.WriteError(w, http.StatusBadRequest, "too_large", "Request body is too large.")
			return interestsJSON{}, false
		}
		httpx.WriteError(w, http.StatusBadRequest, "invalid", "Check the interests and try again.")
		return interestsJSON{}, false
	}
	return body, true
}

func normalizeInterests(in interestsJSON) (interestsJSON, string, bool) {
	fields := []struct {
		label string
		value *string
	}{
		{"Hobby", &in.Hobby},
		{"Music", &in.Music},
		{"Movies", &in.Movies},
		{"Other", &in.Other},
	}
	for _, field := range fields {
		trimmed := strings.TrimSpace(*field.value)
		if utf8.RuneCountInString(trimmed) > maxInterestRunes {
			return interestsJSON{}, field.label, false
		}
		*field.value = trimmed
	}
	return in, "", true
}

func loadInterests(ctx context.Context, q *sqlc.Queries, userID int64) (interestsJSON, error) {
	row, err := q.GetInterests(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return interestsJSON{}, nil
		}
		return interestsJSON{}, err
	}
	return interestsJSON{Hobby: row.Hobby, Music: row.Music, Movies: row.Movies, Other: row.Other}, nil
}

func avatarPointer(path *string) *string {
	if path == nil || strings.TrimSpace(*path) == "" {
		return nil
	}
	u := upload.URL(*path)
	return &u
}

func parseID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}
