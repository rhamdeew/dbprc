package profile

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/rhamdeew/dbprc/backend/internal/db/sqlc"
	"github.com/rhamdeew/dbprc/backend/internal/httpx"
)

type photoLikesJSON struct {
	LikeCount int64 `json:"likeCount"`
	LikedByMe bool  `json:"likedByMe"`
}

type photoAuthorJSON struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	AvatarURL *string `json:"avatarUrl"`
}

type photoCommentJSON struct {
	ID        int64           `json:"id"`
	Author    photoAuthorJSON `json:"author"`
	Body      string          `json:"body"`
	CreatedAt string          `json:"createdAt"`
	CanDelete bool            `json:"canDelete"`
}

func (h *handlers) photoRoutes(mux *http.ServeMux, guard func(http.Handler) http.Handler) {
	likeLimit := httpx.RateLimit(httpx.NewLimiter(60, time.Minute))
	commentLimit := httpx.RateLimit(httpx.NewLimiter(20, time.Minute))
	mux.Handle("PUT /api/photos/{id}/like", guard(httpx.LimitBody(httpx.SmallBodyLimit)(likeLimit(h.handlePhoto(h.changePhotoLike)))))
	mux.Handle("DELETE /api/photos/{id}/like", guard(httpx.LimitBody(httpx.SmallBodyLimit)(likeLimit(h.handlePhoto(h.changePhotoLike)))))
	mux.Handle("GET /api/photos/{id}/comments", guard(h.handlePhoto(h.getPhotoComments)))
	mux.Handle("POST /api/photos/{id}/comments", guard(httpx.LimitBody(httpx.JSONBodyLimit)(commentLimit(h.handlePhoto(h.postPhotoComment)))))
	mux.Handle("DELETE /api/photos/{id}/comments/{commentId}", guard(httpx.LimitBody(httpx.SmallBodyLimit)(commentLimit(h.handlePhoto(h.deletePhotoComment)))))
}

func (h *handlers) handlePhoto(next func(http.ResponseWriter, *http.Request, sqlc.User, int64) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		viewer, _ := httpx.UserFrom(r.Context()) // Required by the route guard.
		photoID, err := parseID(r.PathValue("id"))
		if err != nil {
			err = sql.ErrNoRows
		} else {
			err = next(w, r, viewer, photoID)
		}
		switch {
		case err == nil:
		case errors.Is(err, sql.ErrNoRows):
			message := "Photo not found."
			if r.PathValue("commentId") != "" {
				message = "Comment not found."
			}
			httpx.WriteError(w, http.StatusNotFound, "not_found", message)
		default:
			h.log.Error("photo interaction", "err", err, "method", r.Method, "photo_id", photoID, "user_id", viewer.ID)
			httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		}
	})
}

func (h *handlers) changePhotoLike(w http.ResponseWriter, r *http.Request, viewer sqlc.User, photoID int64) error {
	ctx := r.Context()
	tx, err := h.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	q := sqlc.New(tx)
	if _, err := q.GetPhotoOwner(ctx, photoID); err != nil {
		return err
	}
	if r.Method == http.MethodPut {
		err = q.AddPhotoLike(ctx, sqlc.AddPhotoLikeParams{
			PhotoID: photoID, UserID: viewer.ID, CreatedAt: time.Now().UTC().Format(time.RFC3339),
		})
	} else {
		err = q.RemovePhotoLike(ctx, sqlc.RemovePhotoLikeParams{PhotoID: photoID, UserID: viewer.ID})
	}
	if err != nil {
		return err
	}
	likes, err := q.GetPhotoLikes(ctx, sqlc.GetPhotoLikesParams{PhotoID: photoID, ViewerID: viewer.ID})
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, photoLikesJSON{LikeCount: likes.LikeCount, LikedByMe: likes.LikedByMe != 0})
	return nil
}

func (h *handlers) getPhotoComments(w http.ResponseWriter, r *http.Request, viewer sqlc.User, photoID int64) error {
	ctx := r.Context()
	tx, err := h.db.Reader().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	q := sqlc.New(tx)
	ownerID, err := q.GetPhotoOwner(ctx, photoID)
	if err != nil {
		return err
	}
	rows, err := q.ListPhotoComments(ctx, photoID)
	if err != nil {
		return err
	}
	comments := make([]photoCommentJSON, 0, len(rows))
	for _, row := range rows {
		comments = append(comments, photoCommentJSON{
			ID:     row.ID,
			Author: photoAuthorJSON{ID: row.AuthorID, Name: row.Name, AvatarURL: avatarPointer(row.AvatarPath)},
			Body:   row.Body, CreatedAt: row.CreatedAt, CanDelete: viewer.ID == ownerID || viewer.ID == row.AuthorID,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Comments []photoCommentJSON `json:"comments"`
	}{Comments: comments})
	return nil
}

func (h *handlers) postPhotoComment(w http.ResponseWriter, r *http.Request, viewer sqlc.User, photoID int64) error {
	var input struct {
		Body string `json:"body"`
	}
	if !decodeJSON(w, r, &input, "Check your comment and try again.") {
		return nil
	}
	body := strings.TrimSpace(input.Body)
	if body == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid", "Write a comment first.")
		return nil
	}
	if !checkFieldLength(w, body, "Comment", 1000) {
		return nil
	}

	createdAt := time.Now().UTC().Format(time.RFC3339)
	id, err := h.insertPhotoComment(r.Context(), sqlc.CreatePhotoCommentParams{
		PhotoID: photoID, AuthorID: viewer.ID, Body: body, CreatedAt: createdAt,
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, photoCommentJSON{
		ID: id, Body: body, CreatedAt: createdAt, CanDelete: true,
		Author: photoAuthorJSON{ID: viewer.ID, Name: viewer.Name, AvatarURL: avatarPointer(viewer.AvatarPath)},
	})
	return nil
}

func (h *handlers) insertPhotoComment(ctx context.Context, input sqlc.CreatePhotoCommentParams) (int64, error) {
	tx, err := h.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	q := sqlc.New(tx)
	if _, err := q.GetPhotoOwner(ctx, input.PhotoID); err != nil {
		return 0, err
	}
	id, err := q.CreatePhotoComment(ctx, input)
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func (h *handlers) deletePhotoComment(w http.ResponseWriter, r *http.Request, viewer sqlc.User, photoID int64) error {
	id, err := parseID(r.PathValue("commentId"))
	if err != nil {
		return sql.ErrNoRows
	}
	_, err = sqlc.New(h.db.Writer()).DeletePhotoComment(r.Context(), sqlc.DeletePhotoCommentParams{
		ID: id, PhotoID: photoID, ViewerID: viewer.ID,
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
