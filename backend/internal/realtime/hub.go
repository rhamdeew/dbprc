// Package realtime is the in-process WebSocket hub.
//
// GET /ws requires a session. The upgrade runs only after that check.
// The hub implements chat.Notifier: each connected member receives
// chat.new and message.new after the write commits.
package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/rhamdeew/dbprc/backend/internal/chat"
	"github.com/rhamdeew/dbprc/backend/internal/db"
	"github.com/rhamdeew/dbprc/backend/internal/db/sqlc"
	"github.com/rhamdeew/dbprc/backend/internal/httpx"
	"github.com/rhamdeew/dbprc/backend/internal/session"
	"github.com/rhamdeew/dbprc/backend/internal/upload"
)

const (
	// sendBuffer is how many events a socket may have queued.
	// A full buffer means the client is too slow: that socket is closed.
	sendBuffer = 32
	// pingInterval is the time between server pings.
	pingInterval = 30 * time.Second
	// ioTimeout bounds one write or one ping, including the wait for a pong.
	ioTimeout = 10 * time.Second
	// viewTimeout bounds the read that builds a chat.new payload.
	viewTimeout = 2 * time.Second
)

// Hub maps a user id to the set of sockets open for that user.
type Hub struct {
	db             *db.DB
	log            *slog.Logger
	originPatterns []string
	pingEvery      time.Duration

	mu     sync.Mutex
	conns  map[int64]map[*client]struct{}
	closed bool
	wg     sync.WaitGroup
	once   sync.Once
}

type client struct {
	userID int64
	conn   *websocket.Conn
	send   chan []byte
}

type event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

type messageEvent struct {
	ChatID  int64       `json:"chatId"`
	Message messageJSON `json:"message"`
}

// JSON shapes match the chat REST objects so a client can use one type.
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

type messageJSON struct {
	ID        int64  `json:"id"`
	SenderID  *int64 `json:"senderId"`
	Kind      string `json:"kind"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
}

var errChatNotVisible = errors.New("chat not visible")

// New builds a hub. appBaseURL's host (including port) is the only extra
// origin allowed to upgrade; the request host is always allowed.
// A nil database panics. A nil logger discards logs.
func New(database *db.DB, log *slog.Logger, appBaseURL string) *Hub {
	if database == nil {
		panic("realtime: nil database")
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Hub{
		db:             database,
		log:            log,
		originPatterns: originPatterns(appBaseURL),
		pingEvery:      pingInterval,
		conns:          make(map[int64]map[*client]struct{}),
	}
}

// originPatterns is the host of APP_BASE_URL. coder/websocket matches it
// against the Origin header's host. An empty or non-http URL allows no
// extra origin (the request host is still allowed by the library).
func originPatterns(appBaseURL string) []string {
	host, ok := originHost(appBaseURL)
	if !ok {
		return nil
	}
	return []string{host}
}

func originHost(appBaseURL string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(appBaseURL))
	if err != nil || u.Host == "" {
		return "", false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return "", false
	}
	return strings.ToLower(u.Host), true
}

// Close closes every socket and waits until their handlers return.
// It is safe to call more than once. Shutdown must call this: http.Server
// does not close hijacked connections.
func (h *Hub) Close() {
	if h == nil {
		return
	}
	h.once.Do(func() {
		h.mu.Lock()
		h.closed = true
		all := make([]*client, 0)
		for _, set := range h.conns {
			for c := range set {
				all = append(all, c)
			}
		}
		h.conns = map[int64]map[*client]struct{}{}
		h.mu.Unlock()

		var wg sync.WaitGroup
		for _, c := range all {
			wg.Add(1)
			go func(c *client) {
				defer wg.Done()
				_ = c.conn.Close(websocket.StatusGoingAway, "shutting down")
			}(c)
		}
		wg.Wait()
		h.wg.Wait()
	})
}

// ConnCount is the number of open sockets for userID.
func (h *Hub) ConnCount(userID int64) int {
	if h == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.conns[userID])
}

// ChatCreated sends chat.new to each connected member.
// data is that member's chat list object (the partner is the other user).
func (h *Hub) ChatCreated(c chat.Chat, userIDs []int64) {
	for _, id := range userIDs {
		if h.ConnCount(id) == 0 {
			continue
		}
		view, err := h.chatView(id, c.ID)
		if err != nil {
			h.log.Error("ws chat.new", "err", err, "chat_id", c.ID, "user_id", id)
			continue
		}
		payload, err := json.Marshal(event{Type: "chat.new", Data: view})
		if err != nil {
			h.log.Error("ws chat.new", "err", err, "chat_id", c.ID, "user_id", id)
			continue
		}
		h.publish(id, payload)
	}
}

// MessageCreated sends message.new to each connected member.
// data is {"chatId", "message"} and message matches the REST message object.
func (h *Hub) MessageCreated(msg chat.Message, userIDs []int64) {
	payload, err := json.Marshal(event{
		Type: "message.new",
		Data: messageEvent{
			ChatID: msg.ChatID,
			Message: messageJSON{
				ID:        msg.ID,
				SenderID:  msg.SenderID,
				Kind:      msg.Kind,
				Body:      msg.Body,
				CreatedAt: msg.CreatedAt,
			},
		},
	})
	if err != nil {
		h.log.Error("ws message.new", "err", err, "chat_id", msg.ChatID)
		return
	}
	for _, id := range userIDs {
		h.publish(id, payload)
	}
}

// ServeHTTP upgrades an authenticated request. The caller must have already
// rejected a missing session; this repeats the check so a bare handler
// cannot upgrade anonymously.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	user, ok := httpx.UserFrom(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in required.")
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: h.originPatterns,
	})
	if err != nil {
		// Accept already wrote the status (403 for a foreign Origin).
		h.log.Info("ws accept", "err", err, "user_id", user.ID)
		return
	}
	h.handle(conn, user.ID)
}

// Routes registers GET /ws. A nil hub registers nothing.
// The handler requires a session. CSRF does not apply to GET.
func Routes(mux *http.ServeMux, log *slog.Logger, sessions *session.Manager, hub *Hub) {
	if hub == nil {
		return
	}
	guard := httpx.RequireUser(log, sessions.UserID, sqlc.New(hub.db.Reader()).GetUser, sessions.Destroy)
	mux.Handle("GET /ws", guard(hub))
}

func (h *Hub) handle(conn *websocket.Conn, userID int64) {
	c := &client{userID: userID, conn: conn, send: make(chan []byte, sendBuffer)}
	// Add to the WaitGroup under the same lock as closed, so Close cannot
	// pass Wait before this handler is counted.
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		_ = conn.Close(websocket.StatusGoingAway, "shutting down")
		return
	}
	h.wg.Add(1)
	set := h.conns[userID]
	if set == nil {
		set = make(map[*client]struct{})
		h.conns[userID] = set
	}
	set[c] = struct{}{}
	h.mu.Unlock()

	defer h.wg.Done()
	defer h.remove(c)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	// CloseRead handles pong and close. A data frame from the client ends
	// the socket: this hub is server push only.
	ctx := conn.CloseRead(context.Background())
	h.loop(ctx, c)
}

func (h *Hub) loop(ctx context.Context, c *client) {
	ticker := time.NewTicker(h.pingEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := ping(ctx, c.conn); err != nil {
				return
			}
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			if err := writeText(ctx, c.conn, msg); err != nil {
				return
			}
		}
	}
}

func ping(ctx context.Context, conn *websocket.Conn) error {
	pctx, cancel := context.WithTimeout(ctx, ioTimeout)
	defer cancel()
	return conn.Ping(pctx)
}

func writeText(ctx context.Context, conn *websocket.Conn, msg []byte) error {
	wctx, cancel := context.WithTimeout(ctx, ioTimeout)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, msg)
}

func (h *Hub) publish(userID int64, payload []byte) {
	h.mu.Lock()
	set := h.conns[userID]
	slow := make([]*client, 0)
	for c := range set {
		select {
		case c.send <- payload:
		default:
			slow = append(slow, c)
			delete(set, c)
		}
	}
	if len(set) == 0 {
		delete(h.conns, userID)
	}
	h.mu.Unlock()
	// CloseNow does not wait for a handshake, so a stuck peer cannot stall
	// the HTTP handler that is publishing.
	for _, c := range slow {
		_ = c.conn.CloseNow()
	}
}

func (h *Hub) remove(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	set := h.conns[c.userID]
	delete(set, c)
	if len(set) == 0 {
		delete(h.conns, c.userID)
	}
}

func (h *Hub) chatView(userID, chatID int64) (chatJSON, error) {
	ctx, cancel := context.WithTimeout(context.Background(), viewTimeout)
	defer cancel()
	rows, err := sqlc.New(h.db.Reader()).ListChatsForUser(ctx, userID)
	if err != nil {
		return chatJSON{}, err
	}
	for _, row := range rows {
		if row.ID == chatID {
			return toChatJSON(row), nil
		}
	}
	return chatJSON{}, errChatNotVisible
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

func avatarURL(path *string) *string {
	if path == nil || strings.TrimSpace(*path) == "" {
		return nil
	}
	u := upload.URL(*path)
	return &u
}
