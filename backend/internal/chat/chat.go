// Package chat serves random pairing, the chat list, messages, and read cursors.
package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
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
	// maxBodyRunes is the limit after trimming. "4000 chars" counts characters, not bytes.
	maxBodyRunes  = 4000
	defaultLimit  = 50
	maxLimit      = 100
	jsonBodyLimit = 32 << 10

	kindText   = "text"
	kindSystem = "system"

	// rematchBody is the system line inserted when two people are paired again.
	rematchBody = "Matched again ☕"
)

// Chat is one pair. UserLow is less than UserHigh.
type Chat struct {
	ID            int64
	UserLow       int64
	UserHigh      int64
	CreatedAt     string
	LastMessageAt string
}

// Message is one stored line. SenderID is nil for a system line.
type Message struct {
	ID        int64
	ChatID    int64
	SenderID  *int64
	Kind      string
	Body      string
	CreatedAt string
}

// Notifier receives chat events after the write commits.
// userIDs is both members, user_low then user_high.
// ARC-26 replaces NopNotifier with the in-process hub.
type Notifier interface {
	ChatCreated(chat Chat, userIDs []int64)
	MessageCreated(msg Message, userIDs []int64)
}

// NopNotifier discards events.
type NopNotifier struct{}

func (NopNotifier) ChatCreated(Chat, []int64)       {}
func (NopNotifier) MessageCreated(Message, []int64) {}

type availabilityJSON struct {
	Available bool `json:"available"`
}

type partnerJSON struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	AvatarURL *string `json:"avatarUrl"`
}

type lastMessageJSON struct {
	Body      string `json:"body"`
	Kind      string `json:"kind"`
	CreatedAt string `json:"createdAt"`
}

type chatJSON struct {
	ID          int64            `json:"id"`
	Partner     partnerJSON      `json:"partner"`
	LastMessage *lastMessageJSON `json:"lastMessage"`
	UnreadCount int64            `json:"unreadCount"`
}

type randomJSON struct {
	Chat    chatJSON `json:"chat"`
	Rematch bool     `json:"rematch"`
}

type messageJSON struct {
	ID        int64  `json:"id"`
	SenderID  *int64 `json:"senderId"`
	Kind      string `json:"kind"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
}

type messagesJSON struct {
	Messages []messageJSON `json:"messages"`
}

type readJSON struct {
	LastMessageID int64 `json:"lastMessageId"`
}

type handlers struct {
	log      *slog.Logger
	db       *db.DB
	notifier Notifier
}

// Routes registers the chat endpoints. Each one requires a session.
// The caller wraps the mux with session LoadAndSave and CSRF.
// A nil notifier is NopNotifier.
func Routes(mux *http.ServeMux, log *slog.Logger, sessions *session.Manager, database *db.DB, notifier Notifier) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if notifier == nil {
		notifier = NopNotifier{}
	}
	h := &handlers{log: log, db: database, notifier: notifier}
	guard := httpx.RequireUser(log, sessions.UserID, sqlc.New(database.Reader()).GetUser, sessions.Destroy)
	mux.Handle("PUT /api/me/availability", guard(http.HandlerFunc(h.putAvailability)))
	mux.Handle("POST /api/chats/random", guard(http.HandlerFunc(h.random)))
	mux.Handle("GET /api/chats", guard(http.HandlerFunc(h.list)))
	mux.Handle("GET /api/chats/{id}/messages", guard(http.HandlerFunc(h.listMessages)))
	mux.Handle("POST /api/chats/{id}/messages", guard(http.HandlerFunc(h.postMessage)))
	mux.Handle("POST /api/chats/{id}/read", guard(http.HandlerFunc(h.markRead)))
}

func (h *handlers) putAvailability(w http.ResponseWriter, r *http.Request) {
	me, ok := httpx.UserFrom(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in required.")
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	available, ok := decodeAvailability(raw)
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "invalid", "Check availability and try again.")
		return
	}
	stored, err := sqlc.New(h.db.Writer()).SetUserAvailable(r.Context(), sqlc.SetUserAvailableParams{
		Available: boolInt(available),
		ID:        me.ID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.WriteError(w, http.StatusNotFound, "not_found", "Profile not found.")
			return
		}
		h.log.Error("set availability", "err", err, "user_id", me.ID)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, availabilityJSON{Available: stored != 0})
}

func (h *handlers) random(w http.ResponseWriter, r *http.Request) {
	me, ok := httpx.UserFrom(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in required.")
		return
	}
	res, err := h.match(r.Context(), me.ID)
	switch {
	case err == nil:
	case errors.Is(err, errUnavailable):
		httpx.WriteError(w, http.StatusConflict, "you_are_unavailable", "Turn availability on to start a random chat.")
		return
	case errors.Is(err, errNobody):
		httpx.WriteError(w, http.StatusNotFound, "nobody_available", "Nobody is available right now.")
		return
	case errors.Is(err, sql.ErrNoRows):
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in required.")
		return
	default:
		h.log.Error("random chat", "err", err, "user_id", me.ID)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	// The write connection is back in the pool before either callback runs.
	if res.created != nil {
		h.notifier.ChatCreated(*res.created, res.userIDs)
	}
	if res.message != nil {
		h.notifier.MessageCreated(*res.message, res.userIDs)
	}
	httpx.WriteJSON(w, http.StatusCreated, randomJSON{Chat: res.view, Rematch: res.rematch})
}

func (h *handlers) list(w http.ResponseWriter, r *http.Request) {
	me, ok := httpx.UserFrom(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in required.")
		return
	}
	rows, err := sqlc.New(h.db.Reader()).ListChatsForUser(r.Context(), me.ID)
	if err != nil {
		h.log.Error("list chats", "err", err, "user_id", me.ID)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	out := make([]chatJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, toChatJSON(row))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *handlers) listMessages(w http.ResponseWriter, r *http.Request) {
	me, ok := httpx.UserFrom(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in required.")
		return
	}
	chatID, err := parseID(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "Chat not found.")
		return
	}
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid", "Limit must be between 1 and 100.")
		return
	}
	before, hasBefore, err := parseBefore(r.URL.Query().Get("before"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid", "Check the message and try again.")
		return
	}

	tx, err := h.db.Reader().BeginTx(r.Context(), nil)
	if err != nil {
		h.log.Error("list messages", "err", err, "chat_id", chatID, "user_id", me.ID)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	defer func() { _ = tx.Rollback() }()

	q := sqlc.New(tx)
	if _, err := loadMember(r.Context(), q, chatID, me.ID); err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, errNotMember) {
			httpx.WriteError(w, http.StatusNotFound, "not_found", "Chat not found.")
			return
		}
		h.log.Error("load chat", "err", err, "chat_id", chatID, "user_id", me.ID)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	if hasBefore {
		_, err = q.GetMessageInChat(r.Context(), sqlc.GetMessageInChatParams{ID: before, ChatID: chatID})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				httpx.WriteError(w, http.StatusNotFound, "not_found", "Message not found.")
				return
			}
			h.log.Error("message cursor", "err", err, "chat_id", chatID, "user_id", me.ID)
			httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
			return
		}
	}
	page, err := listMessagePage(r.Context(), q, chatID, before, hasBefore, limit)
	if err != nil {
		h.log.Error("list messages", "err", err, "chat_id", chatID, "user_id", me.ID)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, messagesJSON{Messages: page})
}

func (h *handlers) postMessage(w http.ResponseWriter, r *http.Request) {
	me, ok := httpx.UserFrom(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in required.")
		return
	}
	chatID, err := parseID(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "Chat not found.")
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}

	// Finish the writer transaction before writing the response. Session commit
	// runs inside WriteHeader and needs the single writer connection.
	stored, chatRow, code, message, err := h.insertMessage(r.Context(), me.ID, chatID, raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, errNotMember) {
			httpx.WriteError(w, http.StatusNotFound, "not_found", "Chat not found.")
			return
		}
		h.log.Error("insert message", "err", err, "chat_id", chatID, "user_id", me.ID)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	if code != "" {
		httpx.WriteError(w, http.StatusBadRequest, code, message)
		return
	}
	h.notifier.MessageCreated(domainMessage(stored), []int64{chatRow.UserLow, chatRow.UserHigh})
	httpx.WriteJSON(w, http.StatusCreated, toMessageJSON(stored))
}

func (h *handlers) markRead(w http.ResponseWriter, r *http.Request) {
	me, ok := httpx.UserFrom(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in required.")
		return
	}
	chatID, err := parseID(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "Chat not found.")
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}

	cursor, status, err := h.advanceRead(r.Context(), me.ID, chatID, raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, errNotMember) {
			httpx.WriteError(w, http.StatusNotFound, "not_found", "Chat not found.")
			return
		}
		if errors.Is(err, errNoMessage) {
			httpx.WriteError(w, http.StatusNotFound, "not_found", "Message not found.")
			return
		}
		h.log.Error("mark read", "err", err, "chat_id", chatID, "user_id", me.ID)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	if status != 0 {
		httpx.WriteError(w, http.StatusBadRequest, "invalid", "Check the message and try again.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, readJSON{LastMessageID: cursor})
}

// insertMessage stores one text line. The writer transaction is finished
// before it returns, so the caller can write the HTTP response.
// A non-empty code is a 400 with that API code. ErrNoRows and errNotMember
// mean the chat is not visible to the user.
func (h *handlers) insertMessage(ctx context.Context, userID, chatID int64, raw []byte) (sqlc.Message, sqlc.Chat, string, string, error) {
	tx, err := h.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return sqlc.Message{}, sqlc.Chat{}, "", "", err
	}
	defer func() { _ = tx.Rollback() }()

	q := sqlc.New(tx)
	chatRow, err := loadMember(ctx, q, chatID, userID)
	if err != nil {
		return sqlc.Message{}, sqlc.Chat{}, "", "", err
	}
	text, code, message, ok := decodeMessage(raw)
	if !ok {
		return sqlc.Message{}, sqlc.Chat{}, code, message, nil
	}
	now := nowRFC3339()
	sender := userID
	stored, err := q.InsertMessage(ctx, sqlc.InsertMessageParams{
		ChatID:    chatID,
		SenderID:  &sender,
		Kind:      kindText,
		Body:      text,
		CreatedAt: now,
	})
	if err != nil {
		return sqlc.Message{}, sqlc.Chat{}, "", "", err
	}
	if err := q.TouchChat(ctx, sqlc.TouchChatParams{LastMessageAt: now, ID: chatID}); err != nil {
		return sqlc.Message{}, sqlc.Chat{}, "", "", err
	}
	if err := tx.Commit(); err != nil {
		return sqlc.Message{}, sqlc.Chat{}, "", "", err
	}
	return stored, chatRow, "", "", nil
}

// advanceRead moves the cursor when messageID is newer. status 400 means the
// body was rejected. The transaction is finished before return.
func (h *handlers) advanceRead(ctx context.Context, userID, chatID int64, raw []byte) (int64, int, error) {
	tx, err := h.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback() }()

	q := sqlc.New(tx)
	if _, err := loadMember(ctx, q, chatID, userID); err != nil {
		return 0, 0, err
	}
	messageID, ok := decodeRead(raw)
	if !ok {
		return 0, http.StatusBadRequest, nil
	}
	if _, err := q.GetMessageInChat(ctx, sqlc.GetMessageInChatParams{ID: messageID, ChatID: chatID}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0, errNoMessage
		}
		return 0, 0, err
	}
	cursor, err := q.AdvanceChatRead(ctx, sqlc.AdvanceChatReadParams{
		ChatID:            chatID,
		UserID:            userID,
		LastReadMessageID: messageID,
	})
	if err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return cursor, 0, nil
}

var errNotMember = errors.New("not a member")
var errNoMessage = errors.New("message not in chat")

func loadMember(ctx context.Context, q *sqlc.Queries, chatID, userID int64) (sqlc.Chat, error) {
	row, err := q.GetChatByID(ctx, chatID)
	if err != nil {
		return sqlc.Chat{}, err
	}
	if row.UserLow != userID && row.UserHigh != userID {
		return sqlc.Chat{}, errNotMember
	}
	return row, nil
}

func listMessagePage(ctx context.Context, q *sqlc.Queries, chatID, before int64, hasBefore bool, limit int64) ([]messageJSON, error) {
	var (
		rows []sqlc.Message
		err  error
	)
	if hasBefore {
		rows, err = q.ListMessagesBefore(ctx, sqlc.ListMessagesBeforeParams{ChatID: chatID, ID: before, Limit: limit})
	} else {
		rows, err = q.ListLatestMessages(ctx, sqlc.ListLatestMessagesParams{ChatID: chatID, Limit: limit})
	}
	if err != nil {
		return nil, err
	}
	out := make([]messageJSON, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		out = append(out, toMessageJSON(rows[i]))
	}
	return out, nil
}

func toChatJSON(row sqlc.ListChatsForUserRow) chatJSON {
	var last *lastMessageJSON
	if row.LastBody != nil && row.LastKind != nil && row.LastCreatedAt != nil {
		last = &lastMessageJSON{Body: *row.LastBody, Kind: *row.LastKind, CreatedAt: *row.LastCreatedAt}
	}
	return chatJSON{
		ID:          row.ID,
		Partner:     partnerJSON{ID: row.PartnerID, Name: row.PartnerName, AvatarURL: avatarURL(row.PartnerAvatarPath)},
		LastMessage: last,
		UnreadCount: row.UnreadCount,
	}
}

func toMessageJSON(row sqlc.Message) messageJSON {
	return messageJSON{
		ID:        row.ID,
		SenderID:  row.SenderID,
		Kind:      row.Kind,
		Body:      row.Body,
		CreatedAt: row.CreatedAt,
	}
}

func domainChat(row sqlc.Chat) Chat {
	return Chat{
		ID:            row.ID,
		UserLow:       row.UserLow,
		UserHigh:      row.UserHigh,
		CreatedAt:     row.CreatedAt,
		LastMessageAt: row.LastMessageAt,
	}
}

func domainMessage(row sqlc.Message) Message {
	return Message{
		ID:        row.ID,
		ChatID:    row.ChatID,
		SenderID:  row.SenderID,
		Kind:      row.Kind,
		Body:      row.Body,
		CreatedAt: row.CreatedAt,
	}
}

func avatarURL(path *string) *string {
	if path == nil || strings.TrimSpace(*path) == "" {
		return nil
	}
	u := upload.URL(*path)
	return &u
}

func readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, jsonBodyLimit))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid", "Check the request and try again.")
		return nil, false
	}
	return raw, true
}

func decodeAvailability(raw []byte) (bool, bool) {
	var body struct {
		Available *bool `json:"available"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || body.Available == nil {
		return false, false
	}
	return *body.Available, true
}

func decodeMessage(raw []byte) (text, code, message string, ok bool) {
	var body struct {
		Body *string `json:"body"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || body.Body == nil {
		return "", "invalid", "Check the message and try again.", false
	}
	text = strings.TrimSpace(*body.Body)
	if text == "" {
		return "", "invalid", "Write a message first.", false
	}
	if utf8.RuneCountInString(text) > maxBodyRunes {
		return "", "too_long", "Message must be 4000 characters or fewer.", false
	}
	return text, "", "", true
}

func decodeRead(raw []byte) (int64, bool) {
	var body struct {
		LastMessageID *int64 `json:"lastMessageId"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || body.LastMessageID == nil || *body.LastMessageID <= 0 {
		return 0, false
	}
	return *body.LastMessageID, true
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

func parseBefore(raw string) (int64, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false, nil
	}
	id, err := parseID(raw)
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func parseID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}

func boolInt(v bool) int64 {
	if v {
		return 1
	}
	return 0
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
