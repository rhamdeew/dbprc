// Package wall serves a colleague's wall: root posts, replies, and drawings.
package wall

import (
	"context"
	"database/sql"
	"errors"
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
	// maxBodyRunes is the limit after trimming. "2000 chars" counts characters, not bytes.
	maxBodyRunes = 2000
	defaultLimit = 20
	maxLimit     = 100
	// maxParentWalk caps how far a reply is followed to its root.
	maxParentWalk = 8
	// multipartSlack leaves room for boundaries around an image of upload.MaxBytes.
	multipartSlack = 1 << 20
)

var (
	errMissingWall = errors.New("wall not found")
	errNotOnWall   = errors.New("post not on wall")
	errParentLoop  = errors.New("wall parent loop")
)

type authorJSON struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	AvatarURL *string `json:"avatarUrl"`
}

type postJSON struct {
	ID          int64       `json:"id"`
	Body        string      `json:"body"`
	DrawingURL  *string     `json:"drawingUrl"`
	CreatedAt   string      `json:"createdAt"`
	IsAnonymous bool        `json:"isAnonymous"`
	Author      *authorJSON `json:"author"`
	CanDelete   bool        `json:"canDelete"`
}

type wallPostJSON struct {
	postJSON
	Replies []postJSON `json:"replies"`
}

type wallPageJSON struct {
	Posts []wallPostJSON `json:"posts"`
}

type wallForm struct {
	body      string
	anonymous bool
	parentID  int64
	hasParent bool
}

type postFields struct {
	id           int64
	body         string
	drawingPath  *string
	anonymous    bool
	createdAt    string
	authorID     int64
	authorName   string
	authorAvatar *string
}

type handlers struct {
	log     *slog.Logger
	db      *db.DB
	uploads *upload.Store
}

// Routes registers the wall endpoints. Each one requires a session.
// The caller wraps the mux with session LoadAndSave and CSRF.
func Routes(mux *http.ServeMux, log *slog.Logger, sessions *session.Manager, database *db.DB, uploads *upload.Store) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	h := &handlers{log: log, db: database, uploads: uploads}
	guard := httpx.RequireUser(log, sessions.UserID, sqlc.New(database.Reader()).GetUser, sessions.Destroy)
	// Every POST counts, including ones the handler rejects. 20 per user per minute.
	postLimit := httpx.NewLimiter(20, time.Minute)
	const uploadBody = int64(upload.MaxBytes + multipartSlack)
	mux.Handle("GET /api/users/{id}/wall", guard(http.HandlerFunc(h.get)))
	mux.Handle("POST /api/users/{id}/wall", guard(httpx.LimitBody(uploadBody)(httpx.RateLimit(postLimit)(http.HandlerFunc(h.post)))))
	mux.Handle("DELETE /api/wall/{postId}", guard(httpx.LimitBody(httpx.SmallBodyLimit)(http.HandlerFunc(h.delete))))
}

func (h *handlers) get(w http.ResponseWriter, r *http.Request) {
	me, ok := httpx.UserFrom(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in required.")
		return
	}
	wallID, err := parseID(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "Profile not found.")
		return
	}
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid", "Limit must be between 1 and 100.")
		return
	}
	beforeRaw := strings.TrimSpace(r.URL.Query().Get("before"))
	var beforeID int64
	if beforeRaw != "" {
		beforeID, err = parseID(beforeRaw)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid", "Check the post and try again.")
			return
		}
	}

	tx, err := h.db.Reader().BeginTx(r.Context(), nil)
	if err != nil {
		h.log.Error("list wall", "err", err, "wall_owner_id", wallID, "user_id", me.ID)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	defer func() { _ = tx.Rollback() }()

	page, err := loadPage(r.Context(), sqlc.New(tx), wallID, me.ID, beforeID, limit)
	if err != nil {
		writeReadError(h.log, w, err, wallID, me.ID)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}

func loadPage(ctx context.Context, q *sqlc.Queries, wallID, viewerID, beforeID, limit int64) (wallPageJSON, error) {
	if _, err := q.GetUser(ctx, wallID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return wallPageJSON{}, errMissingWall
		}
		return wallPageJSON{}, err
	}
	ownerView := viewerID == wallID
	var roots []postFields
	if beforeID == 0 {
		rows, err := q.ListRootWallPosts(ctx, sqlc.ListRootWallPostsParams{WallOwnerID: wallID, Limit: limit})
		if err != nil {
			return wallPageJSON{}, err
		}
		roots = make([]postFields, 0, len(rows))
		for _, row := range rows {
			roots = append(roots, rootFields(row.ID, row.Body, row.DrawingPath, row.IsAnonymous, row.CreatedAt, row.AuthorID, row.AuthorName, row.AuthorAvatarPath))
		}
	} else {
		cursor, err := q.GetWallPost(ctx, beforeID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return wallPageJSON{}, errNotOnWall
			}
			return wallPageJSON{}, err
		}
		// before is the last root the client already has. A reply, or a post on
		// another wall, is not a cursor for this page.
		if cursor.WallOwnerID != wallID || cursor.ParentID != nil {
			return wallPageJSON{}, errNotOnWall
		}
		rows, err := q.ListRootWallPostsBefore(ctx, sqlc.ListRootWallPostsBeforeParams{
			WallOwnerID: wallID,
			CreatedAt:   cursor.CreatedAt,
			CreatedAt_2: cursor.CreatedAt,
			ID:          cursor.ID,
			Limit:       limit,
		})
		if err != nil {
			return wallPageJSON{}, err
		}
		roots = make([]postFields, 0, len(rows))
		for _, row := range rows {
			roots = append(roots, rootFields(row.ID, row.Body, row.DrawingPath, row.IsAnonymous, row.CreatedAt, row.AuthorID, row.AuthorName, row.AuthorAvatarPath))
		}
	}

	replies := map[int64][]postJSON{}
	if len(roots) > 0 {
		ids := make([]int64, len(roots))
		for i := range roots {
			ids[i] = roots[i].id
		}
		rows, err := q.ListWallReplies(ctx, sqlc.ListWallRepliesParams{WallOwnerID: wallID, ParentIds: idPtrs(ids)})
		if err != nil {
			return wallPageJSON{}, err
		}
		for _, row := range rows {
			if row.ParentID == nil {
				continue
			}
			fields := rootFields(row.ID, row.Body, row.DrawingPath, row.IsAnonymous, row.CreatedAt, row.AuthorID, row.AuthorName, row.AuthorAvatarPath)
			replies[*row.ParentID] = append(replies[*row.ParentID], fields.toJSON(ownerView))
		}
	}

	posts := make([]wallPostJSON, 0, len(roots))
	for _, root := range roots {
		children := replies[root.id]
		if children == nil {
			children = []postJSON{}
		}
		posts = append(posts, wallPostJSON{postJSON: root.toJSON(ownerView), Replies: children})
	}
	return wallPageJSON{Posts: posts}, nil
}

func (h *handlers) post(w http.ResponseWriter, r *http.Request) {
	me, ok := httpx.UserFrom(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in required.")
		return
	}
	wallID, err := parseID(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "Profile not found.")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	form, ok := readWallForm(w, r)
	if !ok {
		return
	}
	hasDrawing := r.MultipartForm != nil && len(r.MultipartForm.File["drawing"]) > 0
	if form.body == "" && !hasDrawing {
		httpx.WriteError(w, http.StatusBadRequest, "invalid", "Write something or add a drawing.")
		return
	}

	var drawing *string
	kept := false
	if hasDrawing {
		path, saved := h.saveDrawing(w, r)
		if !saved {
			return
		}
		drawing = &path
		defer func() {
			if !kept {
				h.discardUpload(path)
			}
		}()
	}

	row, err := h.create(r.Context(), wallID, me, form, drawing)
	if err != nil {
		writePostError(h.log, w, err, wallID, me.ID)
		return
	}
	kept = true
	httpx.WriteJSON(w, http.StatusCreated, newPostJSON(row, me, wallID))
}

func (h *handlers) create(ctx context.Context, wallID int64, me sqlc.User, form wallForm, drawing *string) (sqlc.CreateWallPostRow, error) {
	tx, err := h.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return sqlc.CreateWallPostRow{}, err
	}
	defer func() { _ = tx.Rollback() }()

	q := sqlc.New(tx)
	if _, err := q.GetUser(ctx, wallID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sqlc.CreateWallPostRow{}, errMissingWall
		}
		return sqlc.CreateWallPostRow{}, err
	}
	var parent *int64
	if form.hasParent {
		// A reply to a reply is stored on the root, so the wall stays two levels deep.
		rootID, err := resolveParent(ctx, q, wallID, form.parentID)
		if err != nil {
			return sqlc.CreateWallPostRow{}, err
		}
		parent = &rootID
	}
	anon := int64(0)
	if form.anonymous {
		anon = 1
	}
	row, err := q.CreateWallPost(ctx, sqlc.CreateWallPostParams{
		WallOwnerID: wallID,
		AuthorID:    me.ID,
		ParentID:    parent,
		Body:        form.body,
		DrawingPath: drawing,
		IsAnonymous: anon,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return sqlc.CreateWallPostRow{}, err
	}
	if err := tx.Commit(); err != nil {
		return sqlc.CreateWallPostRow{}, err
	}
	return row, nil
}

// resolveParent returns the root id a new reply must use.
// parentID has to be on wallID. When that post is itself a reply, the walk
// follows parent_id until a root, and the new row points at that root.
func resolveParent(ctx context.Context, q *sqlc.Queries, wallID, parentID int64) (int64, error) {
	seen := make(map[int64]struct{}, 2)
	current := parentID
	for range maxParentWalk {
		if _, ok := seen[current]; ok {
			return 0, errParentLoop
		}
		seen[current] = struct{}{}
		post, err := q.GetWallPost(ctx, current)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return 0, errNotOnWall
			}
			return 0, err
		}
		if post.WallOwnerID != wallID {
			return 0, errNotOnWall
		}
		if post.ParentID == nil {
			return post.ID, nil
		}
		current = *post.ParentID
	}
	return 0, errParentLoop
}

func (h *handlers) delete(w http.ResponseWriter, r *http.Request) {
	me, ok := httpx.UserFrom(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in required.")
		return
	}
	id, err := parseID(r.PathValue("postId"))
	if err != nil {
		// Same response as a post the caller is not allowed to delete.
		httpx.WriteError(w, http.StatusNotFound, "not_found", "Post not found.")
		return
	}
	paths, err := h.removePost(r.Context(), me.ID, id)
	if err != nil {
		if errors.Is(err, errNotOnWall) {
			httpx.WriteError(w, http.StatusNotFound, "not_found", "Post not found.")
			return
		}
		h.log.Error("delete wall post", "err", err, "post_id", id, "user_id", me.ID)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	// Files go after commit. A rolled-back delete must keep the drawings the rows reference.
	for _, path := range paths {
		if err := h.removeStored(path); err != nil {
			h.log.Error("delete wall drawing", "err", err, "post_id", id, "user_id", me.ID)
			httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) removePost(ctx context.Context, viewerID, postID int64) ([]string, error) {
	tx, err := h.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	q := sqlc.New(tx)
	post, err := q.GetWallPost(ctx, postID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNotOnWall
		}
		return nil, err
	}
	// The author, and everyone else who does not own the wall, cannot delete.
	// The error matches a missing post so the response does not reveal it.
	if post.WallOwnerID != viewerID {
		return nil, errNotOnWall
	}
	rawPaths, err := q.ListDrawingPathsForDelete(ctx, postID)
	if err != nil {
		return nil, err
	}
	n, err := q.DeleteWallPost(ctx, sqlc.DeleteWallPostParams{ID: postID, WallOwnerID: viewerID})
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, errNotOnWall
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return cleanPaths(rawPaths), nil
}

func readWallForm(w http.ResponseWriter, r *http.Request) (wallForm, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, upload.MaxBytes+multipartSlack)
	if err := r.ParseMultipartForm(8 << 20); err != nil { //nolint:gosec // G120: body is limited by MaxBytesReader above
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			httpx.WriteError(w, http.StatusBadRequest, "too_large", "Image must be 10MB or smaller.")
			return wallForm{}, false
		}
		httpx.WriteError(w, http.StatusBadRequest, "invalid", "Check the post and try again.")
		return wallForm{}, false
	}
	body := strings.TrimSpace(r.FormValue("body"))
	if utf8.RuneCountInString(body) > maxBodyRunes {
		httpx.WriteError(w, http.StatusBadRequest, "too_long", "Post must be 2000 characters or fewer.")
		return wallForm{}, false
	}
	anonymous, err := parseAnonymous(r.FormValue("anonymous"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid", "Anonymous must be true or false.")
		return wallForm{}, false
	}
	parentRaw := strings.TrimSpace(r.FormValue("parentId"))
	var parentID int64
	if parentRaw != "" {
		parentID, err = parseID(parentRaw)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid", "Check the post and try again.")
			return wallForm{}, false
		}
	}
	return wallForm{body: body, anonymous: anonymous, parentID: parentID, hasParent: parentRaw != ""}, true
}

func parseAnonymous(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "false", "0", "no", "off":
		return false, nil
	case "true", "1", "yes", "on":
		return true, nil
	default:
		return false, errors.New("invalid anonymous")
	}
}

func (h *handlers) saveDrawing(w http.ResponseWriter, r *http.Request) (string, bool) {
	file, _, err := r.FormFile("drawing")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid", "Drawings must be a PNG.")
		return "", false
	}
	defer func() { _ = file.Close() }()
	if h.uploads == nil || h.uploads.Dir == "" {
		h.log.Error("upload store is not configured")
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return "", false
	}
	path, err := h.uploads.Save(r.Context(), file, upload.Drawing)
	if err != nil {
		writeDrawingError(h.log, w, err)
		return "", false
	}
	return path, true
}

func writeDrawingError(log *slog.Logger, w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, upload.ErrTooLarge):
		httpx.WriteError(w, http.StatusBadRequest, "too_large", "Image must be 10MB or smaller.")
	case errors.Is(err, upload.ErrDimensions):
		httpx.WriteError(w, http.StatusBadRequest, "invalid_image", "That image is too big. Use one under 8000 pixels.")
	case errors.Is(err, upload.ErrNotPNG), errors.Is(err, upload.ErrUnsupportedType), errors.Is(err, upload.ErrEmpty):
		httpx.WriteError(w, http.StatusBadRequest, "unsupported_type", "Drawings must be a PNG.")
	default:
		log.Error("save drawing", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
	}
}

func writeReadError(log *slog.Logger, w http.ResponseWriter, err error, wallID, viewerID int64) {
	switch {
	case errors.Is(err, errMissingWall):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "Profile not found.")
	case errors.Is(err, errNotOnWall):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "Post not found.")
	default:
		log.Error("list wall", "err", err, "wall_owner_id", wallID, "user_id", viewerID)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
	}
}

func writePostError(log *slog.Logger, w http.ResponseWriter, err error, wallID, viewerID int64) {
	switch {
	case errors.Is(err, errMissingWall):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "Profile not found.")
	case errors.Is(err, errNotOnWall):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "Post not found.")
	default:
		log.Error("save wall post", "err", err, "wall_owner_id", wallID, "user_id", viewerID)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
	}
}

func newPostJSON(row sqlc.CreateWallPostRow, me sqlc.User, wallID int64) postJSON {
	return postFields{
		id:           row.ID,
		body:         row.Body,
		drawingPath:  row.DrawingPath,
		anonymous:    row.IsAnonymous != 0,
		createdAt:    row.CreatedAt,
		authorID:     me.ID,
		authorName:   me.Name,
		authorAvatar: me.AvatarPath,
	}.toJSON(me.ID == wallID)
}

func rootFields(id int64, body string, drawing *string, anonymous int64, createdAt string, authorID int64, authorName string, authorAvatar *string) postFields {
	return postFields{
		id:           id,
		body:         body,
		drawingPath:  drawing,
		anonymous:    anonymous != 0,
		createdAt:    createdAt,
		authorID:     authorID,
		authorName:   authorName,
		authorAvatar: authorAvatar,
	}
}

// toJSON hides the author of an anonymous post from everyone except the wall owner.
// author_id is never written under any other name.
func (p postFields) toJSON(ownerView bool) postJSON {
	out := postJSON{
		ID:          p.id,
		Body:        p.body,
		DrawingURL:  drawingPointer(p.drawingPath),
		CreatedAt:   p.createdAt,
		IsAnonymous: p.anonymous,
		CanDelete:   ownerView,
	}
	if !p.anonymous || ownerView {
		out.Author = &authorJSON{
			ID:        p.authorID,
			Name:      p.authorName,
			AvatarURL: avatarPointer(p.authorAvatar),
		}
	}
	return out
}

func drawingPointer(path *string) *string {
	if path == nil || strings.TrimSpace(*path) == "" {
		return nil
	}
	u := upload.URL(*path)
	return &u
}

func avatarPointer(path *string) *string {
	if path == nil || strings.TrimSpace(*path) == "" {
		return nil
	}
	u := upload.URL(*path)
	return &u
}

func idPtrs(ids []int64) []*int64 {
	out := make([]*int64, len(ids))
	for i := range ids {
		id := ids[i]
		out[i] = &id
	}
	return out
}

func cleanPaths(in []*string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, path := range in {
		if path == nil {
			continue
		}
		trimmed := strings.TrimSpace(*path)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
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
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func parseLimit(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultLimit, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 1 || n > maxLimit {
		return 0, errors.New("invalid limit")
	}
	return n, nil
}

func parseID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}
