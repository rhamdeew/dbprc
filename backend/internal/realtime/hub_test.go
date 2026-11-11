package realtime

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/rhamdeew/dbprc/backend/internal/chat"
	"github.com/rhamdeew/dbprc/backend/internal/db"
	"github.com/rhamdeew/dbprc/backend/internal/db/sqlc"
	"github.com/rhamdeew/dbprc/backend/internal/httpx"
)

func openDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(t.Context(), filepath.Join(t.TempDir(), "arc.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	return database
}

func discardLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestOriginPatterns(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "https://arc.dbdemo.live", want: "arc.dbdemo.live"},
		{in: "http://localhost:5173", want: "localhost:5173"},
		{in: "http://LocalHost:5173/app", want: "localhost:5173"},
		{in: "https://APP.EXAMPLE:443", want: "app.example:443"},
	}
	for _, tc := range cases {
		got := originPatterns(tc.in)
		if len(got) != 1 || got[0] != tc.want {
			t.Fatalf("%q: got %v", tc.in, got)
		}
	}
	for _, in := range []string{"", "not a url", "ftp://files.example", "/relative"} {
		if got := originPatterns(in); got != nil {
			t.Fatalf("%q: got %v", in, got)
		}
	}
}

func TestServeHTTPRequiresUser(t *testing.T) {
	hub := New(openDB(t), discardLog(), "http://app.example")
	t.Cleanup(hub.Close)
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	rr := httptest.NewRecorder()
	hub.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", rr.Code)
	}
	var body httpx.Error
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "unauthenticated" || rr.Header().Get("Upgrade") != "" {
		t.Fatalf("body %+v upgrade %q", body, rr.Header().Get("Upgrade"))
	}
}

func TestChatViewMissing(t *testing.T) {
	hub := New(openDB(t), discardLog(), "http://app.example")
	t.Cleanup(hub.Close)
	if _, err := hub.chatView(1, 99); err == nil {
		t.Fatal("expected error")
	}
}

func TestPublishNoSockets(t *testing.T) {
	hub := New(openDB(t), discardLog(), "http://app.example")
	t.Cleanup(hub.Close)
	hub.MessageCreated(chat.Message{
		ID: 1, ChatID: 2, Kind: "text", Body: "hi", CreatedAt: "2026-01-02T03:04:05Z",
	}, []int64{7})
	hub.ChatCreated(chat.Chat{ID: 2, UserLow: 7, UserHigh: 8}, []int64{7, 8})
	hub.Close()
	hub.Close()
}

func TestPing(t *testing.T) {
	hub := New(openDB(t), discardLog(), "http://localhost")
	hub.pingEvery = 20 * time.Millisecond
	t.Cleanup(hub.Close)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(httpx.WithUser(r.Context(), sqlc.User{ID: 1}))
		hub.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	pinged := make(chan struct{}, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, srv.URL+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{srv.URL}},
		OnPingReceived: func(context.Context, []byte) bool {
			select {
			case pinged <- struct{}{}:
			default:
			}
			return true
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	conn.CloseRead(context.Background())

	select {
	case <-pinged:
	case <-time.After(3 * time.Second):
		t.Fatal("no ping")
	}
}

func TestClientDataCloses(t *testing.T) {
	hub := New(openDB(t), discardLog(), "http://localhost")
	t.Cleanup(hub.Close)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(httpx.WithUser(r.Context(), sqlc.User{ID: 1}))
		hub.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, srv.URL+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{srv.URL}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"x":1}`)); err != nil {
		t.Fatal(err)
	}
	_, _, err = conn.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("status %v err %v", websocket.CloseStatus(err), err)
	}
}

func TestOverflowDropsSlowClient(t *testing.T) {
	hub := New(openDB(t), discardLog(), "http://app.example")
	t.Cleanup(hub.Close)

	slow, slowServer := dialPair(t)
	_, fastServer := dialPair(t)
	slowC := &client{userID: 7, conn: slowServer, send: make(chan []byte, sendBuffer)}
	fastC := &client{userID: 7, conn: fastServer, send: make(chan []byte, sendBuffer)}
	hub.mu.Lock()
	hub.conns[7] = map[*client]struct{}{slowC: {}, fastC: {}}
	hub.mu.Unlock()

	for range sendBuffer {
		slowC.send <- []byte("x")
	}
	sender := int64(8)
	hub.MessageCreated(chat.Message{
		ID: 3, ChatID: 4, SenderID: &sender, Kind: "text", Body: "hello", CreatedAt: "2026-01-02T03:04:05Z",
	}, []int64{7})

	if hub.ConnCount(7) != 1 {
		t.Fatalf("conns %d", hub.ConnCount(7))
	}
	hub.mu.Lock()
	_, slowStill := hub.conns[7][slowC]
	_, fastStill := hub.conns[7][fastC]
	hub.mu.Unlock()
	if slowStill || !fastStill {
		t.Fatalf("slow registered %v fast registered %v", slowStill, fastStill)
	}
	if len(fastC.send) != 1 {
		t.Fatalf("fast buffer %d", len(fastC.send))
	}
	var ev event
	if err := json.Unmarshal(<-fastC.send, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Type != "message.new" {
		t.Fatalf("type %s", ev.Type)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if _, _, err := slow.Read(ctx); err == nil {
		t.Fatal("slow client still open")
	}
}

func TestCloseRejectsNewSockets(t *testing.T) {
	hub := New(openDB(t), discardLog(), "http://localhost")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(httpx.WithUser(r.Context(), sqlc.User{ID: 1}))
		hub.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	hub.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, srv.URL+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{srv.URL}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	readCtx := conn.CloseRead(context.Background())
	select {
	case <-readCtx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("socket stayed open after shutdown")
	}
	if hub.ConnCount(1) != 0 {
		t.Fatalf("conns %d", hub.ConnCount(1))
	}
}

// dialPair returns the client conn and the server conn from a one-shot upgrade.
// The server handler returns after Accept, which is what Hijack allows.
func dialPair(t *testing.T) (clientConn, serverConn *websocket.Conn) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		accepted <- c
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	clientConn, _, err := websocket.Dial(ctx, srv.URL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = clientConn.CloseNow() })
	select {
	case serverConn = <-accepted:
	case <-ctx.Done():
		t.Fatal("accept did not finish")
	}
	return clientConn, serverConn
}
