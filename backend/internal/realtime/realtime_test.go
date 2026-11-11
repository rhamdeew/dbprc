package realtime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/rhamdeew/dbprc/backend/internal/config"
	"github.com/rhamdeew/dbprc/backend/internal/httpx"
	"github.com/rhamdeew/dbprc/backend/internal/server"
	"github.com/rhamdeew/dbprc/backend/internal/testutil"
)

func TestUnauthenticatedUpgrade(t *testing.T) {
	srv := testutil.NewServer(t)
	cases := []struct {
		name   string
		origin string
	}{
		{name: "no origin"},
		{name: "foreign origin", origin: "https://evil.example"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			header := http.Header{}
			if tc.origin != "" {
				header.Set("Origin", tc.origin)
			}
			resp := dialFail(t, srv, nil, header)
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status %d", resp.StatusCode)
			}
			if resp.Header.Get("Upgrade") != "" {
				t.Fatal("upgraded")
			}
			var body httpx.Error
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Code != "unauthenticated" || body.Message != "Sign in required." {
				t.Fatalf("body %+v", body)
			}
		})
	}
}

func TestOrigin(t *testing.T) {
	const base = "http://app.example:5173"
	srv := testutil.NewServer(t, testutil.WithConfig(config.Config{
		Env:        "test",
		Version:    "test",
		AppBaseURL: base,
	}))
	user := testutil.CreateUser(t, testutil.Database(t, srv))
	cookie := testutil.SessionCookie(testutil.LoginAs(t, srv, user))

	conn := dial(t, srv, cookie, base)
	_ = conn.Close(websocket.StatusNormalClosure, "")

	resp := dialFail(t, srv, cookie, http.Header{"Origin": []string{"https://evil.example"}})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d body %s", resp.StatusCode, readBody(t, resp))
	}
}

func TestEvents(t *testing.T) {
	srv := testutil.NewServer(t)
	app := srv.Config.Handler.(*server.Server)
	database := testutil.Database(t, srv)
	ann := testutil.CreateUser(t, database, testutil.WithName("Ann"), testutil.WithAvatarPath("avatars/ann.png"))
	bo := testutil.CreateUser(t, database, testutil.WithName("Bo"))
	// Unavailable, so the random match can only pair Ann with Bo.
	stranger := testutil.CreateUser(t, database, testutil.WithName("Cy"), testutil.WithAvailable(false))
	clientA := testutil.LoginAs(t, srv, ann)
	clientB := testutil.LoginAs(t, srv, bo)

	connA := dial(t, srv, testutil.SessionCookie(clientA), srv.URL)
	connB := dial(t, srv, testutil.SessionCookie(clientB), srv.URL)
	connB2 := dial(t, srv, testutil.SessionCookie(clientB), srv.URL)
	connC := dial(t, srv, testutil.SessionCookie(testutil.LoginAs(t, srv, stranger)), srv.URL)
	waitConns(t, app, ann.ID, 1)
	waitConns(t, app, bo.ID, 2)
	waitConns(t, app, stranger.ID, 1)

	code, raw := postJSON(t, clientA, srv.URL+"/api/chats/random", nil)
	if code != http.StatusCreated {
		t.Fatalf("match %d %s", code, raw)
	}
	var created struct {
		Chat    json.RawMessage `json:"chat"`
		Rematch bool            `json:"rematch"`
	}
	mustJSON(t, raw, &created)
	if created.Rematch {
		t.Fatal("expected a new pair")
	}

	evA := readEvent(t, connA)
	evB := readEvent(t, connB)
	evB2 := readEvent(t, connB2)
	if evA.Type != "chat.new" || evB.Type != "chat.new" || evB2.Type != "chat.new" {
		t.Fatalf("types %s %s %s", evA.Type, evB.Type, evB2.Type)
	}
	assertJSONEqual(t, evA.Data, created.Chat)
	assertJSONEqual(t, evB.Data, evB2.Data)

	var forBo chatView
	mustJSON(t, evB.Data, &forBo)
	if forBo.Partner.ID != ann.ID || forBo.Partner.Name != "Ann" || forBo.Partner.AvatarURL == nil || *forBo.Partner.AvatarURL != "/uploads/avatars/ann.png" {
		t.Fatalf("bo partner %+v", forBo.Partner)
	}
	if forBo.LastMessage != nil || forBo.UnreadCount != 0 {
		t.Fatalf("bo chat %+v", forBo)
	}
	listCode, listRaw := get(t, clientB, srv.URL+"/api/chats")
	if listCode != http.StatusOK {
		t.Fatalf("list %d %s", listCode, listRaw)
	}
	var listed []json.RawMessage
	mustJSON(t, listRaw, &listed)
	if len(listed) != 1 {
		t.Fatalf("list %s", listRaw)
	}
	assertJSONEqual(t, evB.Data, listed[0])

	code, raw = postJSON(t, clientA, fmt.Sprintf("%s/api/chats/%d/messages", srv.URL, forBo.ID), map[string]any{"body": "hello"})
	if code != http.StatusCreated {
		t.Fatalf("post %d %s", code, raw)
	}
	msgA := readEvent(t, connA)
	msgB := readEvent(t, connB)
	msgB2 := readEvent(t, connB2)
	if msgA.Type != "message.new" || msgB.Type != "message.new" || msgB2.Type != "message.new" {
		t.Fatalf("types %s %s %s", msgA.Type, msgB.Type, msgB2.Type)
	}
	assertMessage(t, msgB.Data, forBo.ID, raw)
	assertJSONEqual(t, msgB.Data, msgB2.Data)
	assertJSONEqual(t, msgA.Data, msgB.Data)

	ctx, cancel := context.WithTimeout(t.Context(), 400*time.Millisecond)
	defer cancel()
	if _, data, err := connC.Read(ctx); err == nil {
		t.Fatalf("stranger got %s", data)
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stranger read: %v", err)
	}
}

func TestShutdownClosesSockets(t *testing.T) {
	srv := testutil.NewServer(t)
	app := srv.Config.Handler.(*server.Server)
	user := testutil.CreateUser(t, testutil.Database(t, srv))
	conn := dial(t, srv, testutil.SessionCookie(testutil.LoginAs(t, srv, user)), srv.URL)
	waitConns(t, app, user.ID, 1)

	ctx := conn.CloseRead(context.Background())
	app.Close()
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("socket stayed open")
	}
	if app.WSConnCount(user.ID) != 0 {
		t.Fatalf("conns %d", app.WSConnCount(user.ID))
	}
	app.Close()
}

type wsEvent struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type chatView struct {
	ID      int64 `json:"id"`
	Partner struct {
		ID        int64   `json:"id"`
		Name      string  `json:"name"`
		AvatarURL *string `json:"avatarUrl"`
	} `json:"partner"`
	LastMessage *struct {
		Body string `json:"body"`
	} `json:"lastMessage"`
	UnreadCount int64 `json:"unreadCount"`
}

func assertMessage(t *testing.T, data []byte, chatID int64, httpMessage []byte) {
	t.Helper()
	var body struct {
		ChatID  int64           `json:"chatId"`
		Message json.RawMessage `json:"message"`
	}
	mustJSON(t, data, &body)
	if body.ChatID != chatID {
		t.Fatalf("chatId %d", body.ChatID)
	}
	assertJSONEqual(t, body.Message, httpMessage)
}

func assertJSONEqual(t *testing.T, a, b []byte) {
	t.Helper()
	var x, y any
	mustJSON(t, a, &x)
	mustJSON(t, b, &y)
	if !reflect.DeepEqual(x, y) {
		t.Fatalf("\nleft  %s\nright %s", a, b)
	}
}

func mustJSON(t *testing.T, raw []byte, dest any) {
	t.Helper()
	if err := json.Unmarshal(raw, dest); err != nil {
		t.Fatalf("json %s: %v", raw, err)
	}
}

func waitConns(t *testing.T, app *server.Server, userID int64, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if app.WSConnCount(userID) >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("user %d has %d sockets, want %d", userID, app.WSConnCount(userID), n)
}

func dial(t *testing.T, srv *httptest.Server, cookie *http.Cookie, origin string) *websocket.Conn {
	t.Helper()
	header := http.Header{}
	if origin != "" {
		header.Set("Origin", origin)
	}
	if cookie != nil {
		header.Set("Cookie", cookie.Name+"="+cookie.Value)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, srv.URL+"/ws", &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		status := 0
		body := ""
		if resp != nil {
			status = resp.StatusCode
			body = readBody(t, resp)
		}
		t.Fatalf("dial: %v status %d body %s", err, status, body)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

func dialFail(t *testing.T, srv *httptest.Server, cookie *http.Cookie, header http.Header) *http.Response {
	t.Helper()
	if header == nil {
		header = http.Header{}
	}
	if cookie != nil {
		header.Set("Cookie", cookie.Name+"="+cookie.Value)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, srv.URL+"/ws", &websocket.DialOptions{HTTPHeader: header})
	if err == nil {
		_ = conn.CloseNow()
		t.Fatal("dial succeeded")
	}
	if resp == nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func postJSON(t *testing.T, client *http.Client, url string, body any) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(http.MethodPost, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, raw
}

func get(t *testing.T, client *http.Client, url string) (int, []byte) {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, raw
}

func readEvent(t *testing.T, conn *websocket.Conn) wsEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var ev wsEvent
	mustJSON(t, data, &ev)
	return ev
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	if resp == nil || resp.Body == nil {
		return ""
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
