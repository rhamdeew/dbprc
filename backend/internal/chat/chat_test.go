package chat_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"log/slog"

	"github.com/rhamdeew/dbprc/backend/internal/chat"
	"github.com/rhamdeew/dbprc/backend/internal/config"
	"github.com/rhamdeew/dbprc/backend/internal/db"
	"github.com/rhamdeew/dbprc/backend/internal/httpx"
	"github.com/rhamdeew/dbprc/backend/internal/server"
	"github.com/rhamdeew/dbprc/backend/internal/testutil"
)

func TestAvailability(t *testing.T) {
	srv := newServer(t, nil)
	database := testutil.Database(t, srv)
	me := testutil.CreateUser(t, database, testutil.WithName("Ada"))
	other := testutil.CreateUser(t, database, testutil.WithName("Bea"))
	client := testutil.LoginAs(t, srv, me)
	otherClient := testutil.LoginAs(t, srv, other)

	code, raw := do(t, client, http.MethodPut, srv.URL+"/api/me/availability", map[string]any{"available": false})
	if code != http.StatusOK {
		t.Fatalf("put %d %s", code, raw)
	}
	var flag availabilityView
	mustDecode(t, raw, &flag)
	if flag.Available {
		t.Fatalf("available %#v", flag)
	}
	if bytes.Contains(raw, []byte("@")) {
		t.Fatalf("email leaked: %s", raw)
	}
	meBody := getMe(t, client, srv)
	if meBody.Available {
		t.Fatal("me still available")
	}

	code, raw = do(t, client, http.MethodPost, srv.URL+"/api/chats/random", nil)
	requireCode(t, code, raw, http.StatusConflict, "you_are_unavailable", "Turn availability on to start a random chat.")
	if countWhere(t, database, `SELECT count(*) FROM chats`) != 0 {
		t.Fatal("chat created while unavailable")
	}

	code, raw = do(t, otherClient, http.MethodPost, srv.URL+"/api/chats/random", nil)
	requireCode(t, code, raw, http.StatusNotFound, "nobody_available", "Nobody is available right now.")

	for _, body := range []any{map[string]any{}, map[string]any{"available": "yes"}, nil} {
		code, raw = do(t, client, http.MethodPut, srv.URL+"/api/me/availability", body)
		requireCode(t, code, raw, http.StatusBadRequest, "invalid", "Check availability and try again.")
	}

	code, raw = do(t, client, http.MethodPut, srv.URL+"/api/me/availability", map[string]any{"available": true})
	if code != http.StatusOK {
		t.Fatalf("turn on %d %s", code, raw)
	}
	mustDecode(t, raw, &flag)
	if !flag.Available || !getMe(t, client, srv).Available {
		t.Fatal("availability did not turn on")
	}
	code, raw = do(t, client, http.MethodPost, srv.URL+"/api/chats/random", nil)
	if code != http.StatusCreated {
		t.Fatalf("match after turning on %d %s", code, raw)
	}
}

func TestRandomPrefersFreshPartner(t *testing.T) {
	srv := newServer(t, nil)
	database := testutil.Database(t, srv)
	me := testutil.CreateUser(t, database, testutil.WithName("Me"))
	old := testutil.CreateUser(t, database, testutil.WithName("Old"))
	fresh1 := testutil.CreateUser(t, database, testutil.WithName("Fresh One"), testutil.WithAvatarPath("avatar/f1.jpg"))
	fresh2 := testutil.CreateUser(t, database, testutil.WithName("Fresh Two"), testutil.WithAvatarPath("avatar/f2.jpg"))
	off := testutil.CreateUser(t, database, testutil.WithName("Off"), testutil.WithAvailable(false))
	testutil.CreateChat(t, database, testutil.WithChatUsers(me.ID, old.ID))
	client := testutil.LoginAs(t, srv, me)

	wantName := map[int64]string{fresh1.ID: "Fresh One", fresh2.ID: "Fresh Two"}
	wantAvatar := map[int64]string{fresh1.ID: "/uploads/avatar/f1.jpg", fresh2.ID: "/uploads/avatar/f2.jpg"}
	seen := map[int64]bool{}
	// Two calls: after the first fresh partner is taken out of the pool, the
	// second call has to be the other one. Staying at two keeps the 10/min cap.
	for round := 0; round < 2; round++ {
		code, raw := do(t, client, http.MethodPost, srv.URL+"/api/chats/random", nil)
		if code != http.StatusCreated {
			t.Fatalf("round %d: %d %s", round, code, raw)
		}
		var body randomView
		mustDecode(t, raw, &body)
		partner := body.Chat.Partner.ID
		if body.Rematch || partner == me.ID || partner == old.ID || partner == off.ID || wantName[partner] == "" {
			t.Fatalf("round %d picked %+v", round, body)
		}
		if seen[partner] {
			t.Fatalf("round %d repeated %d", round, partner)
		}
		if body.Chat.UnreadCount != 0 || body.Chat.LastMessage != nil || body.Chat.Partner.Name != wantName[partner] {
			t.Fatalf("round %d view %+v", round, body.Chat)
		}
		if body.Chat.Partner.AvatarURL == nil || *body.Chat.Partner.AvatarURL != wantAvatar[partner] {
			t.Fatalf("round %d avatar %+v", round, body.Chat.Partner)
		}
		low, high := chatPair(t, database, body.Chat.ID)
		if low >= high || (low != me.ID && high != me.ID) || (low != partner && high != partner) {
			t.Fatalf("pair %d %d for %d and %d", low, high, me.ID, partner)
		}
		seen[partner] = true
		deleteChat(t, database, body.Chat.ID)
		if _, err := database.Writer().ExecContext(t.Context(), `UPDATE users SET available = 0 WHERE id = ?`, partner); err != nil {
			t.Fatal(err)
		}
	}
	if !seen[fresh1.ID] || !seen[fresh2.ID] {
		t.Fatalf("partners seen %v", seen)
	}
	if n := countWhere(t, database, `SELECT count(*) FROM chats`); n != 1 {
		t.Fatalf("chats left %d", n)
	}
}

func TestRandomRematch(t *testing.T) {
	srv := newServer(t, nil)
	database := testutil.Database(t, srv)
	me := testutil.CreateUser(t, database, testutil.WithName("Me"))
	first := testutil.CreateUser(t, database, testutil.WithName("Pat One"))
	second := testutil.CreateUser(t, database, testutil.WithName("Pat Two"))
	off := testutil.CreateUser(t, database, testutil.WithAvailable(false))
	c1 := testutil.CreateChat(t, database, testutil.WithChatUsers(me.ID, first.ID))
	c2 := testutil.CreateChat(t, database, testutil.WithChatUsers(me.ID, second.ID))
	setLastMessageAt(t, database, c1.ID, "2020-01-01T00:00:00Z")
	setLastMessageAt(t, database, c2.ID, "2020-01-02T00:00:00Z")
	client := testutil.LoginAs(t, srv, me)
	known := map[int64]bool{c1.ID: true, c2.ID: true}
	partners := map[int64]bool{first.ID: true, second.ID: true}

	for i := 0; i < 2; i++ {
		code, raw := do(t, client, http.MethodPost, srv.URL+"/api/chats/random", nil)
		if code != http.StatusCreated {
			t.Fatalf("rematch %d: %d %s", i, code, raw)
		}
		var body randomView
		mustDecode(t, raw, &body)
		if !body.Rematch || !known[body.Chat.ID] || !partners[body.Chat.Partner.ID] || body.Chat.Partner.ID == off.ID {
			t.Fatalf("rematch %d %+v", i, body)
		}
		if body.Chat.LastMessage == nil || body.Chat.LastMessage.Body != "Matched again ☕" || body.Chat.LastMessage.Kind != "system" {
			t.Fatalf("last %+v", body.Chat.LastMessage)
		}
		assertRFC3339(t, body.Chat.LastMessage.CreatedAt)
		if body.Chat.UnreadCount < 1 {
			t.Fatalf("unread %d", body.Chat.UnreadCount)
		}
		page := getMessages(t, client, srv, body.Chat.ID, "")
		last := page.Messages[len(page.Messages)-1]
		if last.Kind != "system" || last.Body != "Matched again ☕" || last.SenderID != nil {
			t.Fatalf("stored %+v", last)
		}
		var ts string
		if err := database.Writer().QueryRowContext(t.Context(), `SELECT last_message_at FROM chats WHERE id = ?`, body.Chat.ID).Scan(&ts); err != nil {
			t.Fatal(err)
		}
		if ts == "2020-01-01T00:00:00Z" || ts == "2020-01-02T00:00:00Z" {
			t.Fatalf("last_message_at not bumped: %s", ts)
		}
	}
	if n := countWhere(t, database, `SELECT count(*) FROM chats`); n != 2 {
		t.Fatalf("chats %d", n)
	}
	if n := countWhere(t, database, `SELECT count(*) FROM messages WHERE kind = 'system' AND body = 'Matched again ☕'`); n != 2 {
		t.Fatalf("system messages %d", n)
	}
}

func TestRandomNobodyAvailable(t *testing.T) {
	srv := newServer(t, nil)
	database := testutil.Database(t, srv)
	me := testutil.CreateUser(t, database)
	off := testutil.CreateUser(t, database, testutil.WithAvailable(false))
	client := testutil.LoginAs(t, srv, me)
	code, raw := do(t, client, http.MethodPost, srv.URL+"/api/chats/random", nil)
	requireCode(t, code, raw, http.StatusNotFound, "nobody_available", "Nobody is available right now.")
	_ = off

	alone := newServer(t, nil)
	aloneDB := testutil.Database(t, alone)
	solo := testutil.CreateUser(t, aloneDB)
	code, raw = do(t, testutil.LoginAs(t, alone, solo), http.MethodPost, alone.URL+"/api/chats/random", nil)
	requireCode(t, code, raw, http.StatusNotFound, "nobody_available", "")
	if countWhere(t, aloneDB, `SELECT count(*) FROM chats`) != 0 {
		t.Fatal("chat stored")
	}
}

func TestRandomConcurrentNoDuplicatePair(t *testing.T) {
	rec := &recorder{}
	srv := newServer(t, rec)
	database := testutil.Database(t, srv)
	a := testutil.CreateUser(t, database, testutil.WithName("Ann"))
	b := testutil.CreateUser(t, database, testutil.WithName("Bo"))
	ca := testutil.LoginAs(t, srv, a)
	cb := testutil.LoginAs(t, srv, b)
	ca.Timeout = 30 * time.Second
	cb.Timeout = 30 * time.Second

	const n = 20
	codes := make([]int, n)
	raws := make([][]byte, n)
	bodies := make([]randomView, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		client := ca
		if i%2 == 1 {
			client = cb
		}
		go func(i int, client *http.Client) {
			defer wg.Done()
			codes[i], raws[i], errs[i] = doErr(client, http.MethodPost, srv.URL+"/api/chats/random", nil)
			if errs[i] == nil && codes[i] == http.StatusCreated {
				errs[i] = json.Unmarshal(raws[i], &bodies[i])
			}
		}(i, client)
	}
	wg.Wait()

	fresh := 0
	var chatID int64
	for i := 0; i < n; i++ {
		if errs[i] != nil || codes[i] != http.StatusCreated {
			t.Fatalf("call %d: status %d err %v body %s", i, codes[i], errs[i], raws[i])
		}
		if chatID == 0 {
			chatID = bodies[i].Chat.ID
		}
		if bodies[i].Chat.ID != chatID {
			t.Fatalf("chat id %d and %d", chatID, bodies[i].Chat.ID)
		}
		if !bodies[i].Rematch {
			fresh++
		}
		partner := bodies[i].Chat.Partner.ID
		if partner != a.ID && partner != b.ID {
			t.Fatalf("partner %d", partner)
		}
	}
	if fresh != 1 {
		t.Fatalf("new chats %d", fresh)
	}
	if countWhere(t, database, `SELECT count(*) FROM chats`) != 1 {
		t.Fatal("duplicate pair")
	}
	low, high := chatPair(t, database, chatID)
	if low >= high || low != min(a.ID, b.ID) || high != max(a.ID, b.ID) {
		t.Fatalf("pair %d %d", low, high)
	}
	if nmsg := countWhere(t, database, `SELECT count(*) FROM messages`); nmsg != n-1 {
		t.Fatalf("messages %d", nmsg)
	}
	chats, msgs := rec.snapshot()
	if len(chats) != 1 || len(msgs) != n-1 {
		t.Fatalf("events chats %d msgs %d", len(chats), len(msgs))
	}
	if chats[0].chat.ID != chatID || len(chats[0].ids) != 2 || chats[0].ids[0] != low || chats[0].ids[1] != high {
		t.Fatalf("chat event %+v", chats[0])
	}
	for _, msg := range msgs {
		if msg.msg.Kind != "system" || msg.msg.Body != "Matched again ☕" || msg.msg.SenderID != nil || msg.msg.ChatID != chatID {
			t.Fatalf("message event %+v", msg.msg)
		}
		if len(msg.ids) != 2 || msg.ids[0] != low || msg.ids[1] != high {
			t.Fatalf("message users %v", msg.ids)
		}
	}
}

func TestChatListOrderAndUnread(t *testing.T) {
	srv := newServer(t, nil)
	database := testutil.Database(t, srv)
	me := testutil.CreateUser(t, database, testutil.WithName("Me"))
	bea := testutil.CreateUser(t, database, testutil.WithName("Bea"), testutil.WithAvatarPath("avatar/bea.jpg"))
	cy := testutil.CreateUser(t, database, testutil.WithName("Cy"))
	oldChat := testutil.CreateChat(t, database, testutil.WithChatUsers(me.ID, bea.ID))
	newChat := testutil.CreateChat(t, database, testutil.WithChatUsers(me.ID, cy.ID))
	setLastMessageAt(t, database, oldChat.ID, "2020-01-01T00:00:00Z")
	setLastMessageAt(t, database, newChat.ID, "2021-01-01T00:00:00Z")
	client := testutil.LoginAs(t, srv, me)
	beaClient := testutil.LoginAs(t, srv, bea)

	list := getChats(t, client, srv)
	if len(list) != 2 || list[0].ID != newChat.ID || list[1].ID != oldChat.ID {
		t.Fatalf("order %+v", list)
	}
	if list[0].LastMessage != nil || list[1].LastMessage != nil || list[0].UnreadCount != 0 || list[1].UnreadCount != 0 {
		t.Fatalf("empty %+v", list)
	}
	if list[1].Partner.Name != "Bea" || list[1].Partner.AvatarURL == nil || *list[1].Partner.AvatarURL != "/uploads/avatar/bea.jpg" {
		t.Fatalf("partner %+v", list[1].Partner)
	}
	if list[0].Partner.AvatarURL != nil {
		t.Fatalf("cy avatar %#v", list[0].Partner.AvatarURL)
	}

	code, raw := do(t, client, http.MethodPost, chatMessagesURL(srv, oldChat.ID), map[string]any{"body": "  hello  "})
	if code != http.StatusCreated {
		t.Fatalf("post %d %s", code, raw)
	}
	var sent messageView
	mustDecode(t, raw, &sent)
	if sent.Body != "hello" || sent.Kind != "text" || sent.SenderID == nil || *sent.SenderID != me.ID {
		t.Fatalf("sent %+v", sent)
	}
	assertRFC3339(t, sent.CreatedAt)

	list = getChats(t, client, srv)
	if len(list) != 2 || list[0].ID != oldChat.ID || list[1].ID != newChat.ID {
		t.Fatalf("reordered %+v", list)
	}
	if list[0].UnreadCount != 0 || list[0].LastMessage == nil || list[0].LastMessage.Body != "hello" || list[0].LastMessage.Kind != "text" {
		t.Fatalf("mine %+v", list[0])
	}
	beaList := getChats(t, beaClient, srv)
	if len(beaList) != 1 || beaList[0].ID != oldChat.ID || beaList[0].UnreadCount != 1 || beaList[0].Partner.ID != me.ID {
		t.Fatalf("bea %+v", beaList)
	}
	rawList := listRaw(t, client, srv)
	if bytes.Contains(rawList, []byte("@")) || bytes.Contains(rawList, []byte("email")) {
		t.Fatalf("leak %s", rawList)
	}
}

func TestMessagesPaginationAndValidation(t *testing.T) {
	srv := newServer(t, nil)
	database := testutil.Database(t, srv)
	me := testutil.CreateUser(t, database)
	other := testutil.CreateUser(t, database)
	row := testutil.CreateChat(t, database, testutil.WithChatUsers(me.ID, other.ID))
	client := testutil.LoginAs(t, srv, me)
	otherClient := testutil.LoginAs(t, srv, other)

	empty := getMessages(t, client, srv, row.ID, "")
	if len(empty.Messages) != 0 || messagesNull(t, client, srv, row.ID, "") {
		t.Fatal("empty page")
	}

	for i := 1; i <= 55; i++ {
		testutil.CreateMessage(t, database, testutil.WithChat(row.ID), testutil.WithSender(other.ID), testutil.WithMessageBody(strconv.Itoa(i)))
	}
	page := getMessages(t, client, srv, row.ID, "")
	if len(page.Messages) != 50 || page.Messages[0].Body != "6" || page.Messages[49].Body != "55" {
		t.Fatalf("default page %d %s..%s", len(page.Messages), firstBody(page), lastBody(page))
	}
	for i := 1; i < len(page.Messages); i++ {
		if page.Messages[i].ID <= page.Messages[i-1].ID {
			t.Fatal("not chronological")
		}
	}
	latest := getMessages(t, client, srv, row.ID, "limit=2")
	if len(latest.Messages) != 2 || latest.Messages[0].Body != "54" || latest.Messages[1].Body != "55" {
		t.Fatalf("latest %+v", latest.Messages)
	}
	older := getMessages(t, client, srv, row.ID, "limit=2&before="+itoa(latest.Messages[0].ID))
	if len(older.Messages) != 2 || older.Messages[0].Body != "52" || older.Messages[1].Body != "53" {
		t.Fatalf("older %+v", older.Messages)
	}
	first := getMessages(t, client, srv, row.ID, "before="+itoa(page.Messages[0].ID))
	if len(first.Messages) != 5 || first.Messages[0].Body != "1" || first.Messages[4].Body != "5" {
		t.Fatalf("first page %+v", first.Messages)
	}
	none := getMessages(t, client, srv, row.ID, "before="+itoa(first.Messages[0].ID))
	if len(none.Messages) != 0 {
		t.Fatalf("nothing left %+v", none.Messages)
	}

	foreign := testutil.CreateMessage(t, database, testutil.WithSender(me.ID))
	code, raw := do(t, client, http.MethodGet, chatMessagesURL(srv, row.ID)+"?before="+itoa(foreign.ID), nil)
	requireCode(t, code, raw, http.StatusNotFound, "not_found", "Message not found.")

	for _, q := range []string{"limit=0", "limit=101", "limit=-1", "limit=abc", "before=abc", "before=0"} {
		code, raw = do(t, client, http.MethodGet, chatMessagesURL(srv, row.ID)+"?"+q, nil)
		requireCode(t, code, raw, http.StatusBadRequest, "invalid", "")
	}
	for _, id := range []string{"nope", "0", "-1"} {
		code, raw = do(t, client, http.MethodGet, srv.URL+"/api/chats/"+id+"/messages", nil)
		requireCode(t, code, raw, http.StatusNotFound, "not_found", "Chat not found.")
	}

	before := countWhere(t, database, `SELECT count(*) FROM messages WHERE chat_id = ?`, row.ID)
	cases := []struct {
		body    any
		status  int
		code    string
		message string
	}{
		{map[string]any{"body": ""}, http.StatusBadRequest, "invalid", "Write a message first."},
		{map[string]any{"body": "   "}, http.StatusBadRequest, "invalid", "Write a message first."},
		{map[string]any{}, http.StatusBadRequest, "invalid", "Check the message and try again."},
		{map[string]any{"body": nil}, http.StatusBadRequest, "invalid", "Check the message and try again."},
		{map[string]any{"body": 12}, http.StatusBadRequest, "invalid", "Check the message and try again."},
		{map[string]any{"body": strings.Repeat("é", 4001)}, http.StatusBadRequest, "too_long", "Message must be 4000 characters or fewer."},
		{map[string]any{"body": strings.Repeat("🙂", 4001)}, http.StatusBadRequest, "too_long", "Message must be 4000 characters or fewer."},
	}
	for _, tc := range cases {
		code, raw = do(t, client, http.MethodPost, chatMessagesURL(srv, row.ID), tc.body)
		requireCode(t, code, raw, tc.status, tc.code, tc.message)
	}
	if got := countWhere(t, database, `SELECT count(*) FROM messages WHERE chat_id = ?`, row.ID); got != before {
		t.Fatalf("messages %d want %d", got, before)
	}

	code, raw = do(t, client, http.MethodPost, chatMessagesURL(srv, row.ID), map[string]any{"body": strings.Repeat("é", 4000)})
	if code != http.StatusCreated {
		t.Fatalf("4000 %d %s", code, raw)
	}
	code, raw = do(t, client, http.MethodPost, chatMessagesURL(srv, row.ID), map[string]any{"body": strings.Repeat("🙂", 4000)})
	if code != http.StatusCreated {
		t.Fatalf("emoji %d %s", code, raw)
	}

	code, raw = do(t, client, http.MethodPut, srv.URL+"/api/me/availability", map[string]any{"available": false})
	if code != http.StatusOK {
		t.Fatalf("me off %d %s", code, raw)
	}
	code, raw = do(t, otherClient, http.MethodPut, srv.URL+"/api/me/availability", map[string]any{"available": false})
	if code != http.StatusOK {
		t.Fatalf("other off %d %s", code, raw)
	}
	code, raw = do(t, otherClient, http.MethodPost, chatMessagesURL(srv, row.ID), map[string]any{"body": "still here"})
	if code != http.StatusCreated {
		t.Fatalf("unavailable send %d %s", code, raw)
	}
	var sent messageView
	mustDecode(t, raw, &sent)
	if sent.Body != "still here" || sent.SenderID == nil || *sent.SenderID != other.ID {
		t.Fatalf("sent %+v", sent)
	}
}

func TestReadCursorIsMonotonic(t *testing.T) {
	srv := newServer(t, nil)
	database := testutil.Database(t, srv)
	me := testutil.CreateUser(t, database, testutil.WithName("Me"))
	other := testutil.CreateUser(t, database, testutil.WithName("Other"))
	row := testutil.CreateChat(t, database, testutil.WithChatUsers(me.ID, other.ID))
	first := testutil.CreateMessage(t, database, testutil.WithChat(row.ID), testutil.WithSender(other.ID), testutil.WithMessageBody("one"))
	second := testutil.CreateMessage(t, database, testutil.WithChat(row.ID), testutil.WithSender(other.ID), testutil.WithMessageBody("two"))
	// The extra partner is unavailable, so the later re-match can only land on other.
	foreignChat := testutil.CreateChat(t, database, testutil.WithChatUsers(me.ID, testutil.CreateUser(t, database, testutil.WithAvailable(false)).ID))
	foreign := testutil.CreateMessage(t, database, testutil.WithChat(foreignChat.ID), testutil.WithSender(me.ID), testutil.WithMessageBody("nope"))
	client := testutil.LoginAs(t, srv, me)

	list := getChats(t, client, srv)
	var mine chatView
	for _, item := range list {
		if item.ID == row.ID {
			mine = item
		}
	}
	if mine.UnreadCount != 2 {
		t.Fatalf("unread %d", mine.UnreadCount)
	}

	cursor := readUpTo(t, client, srv, row.ID, first.ID)
	if cursor != first.ID || unread(t, client, srv, row.ID) != 1 {
		t.Fatalf("after first cursor %d unread %d", cursor, unread(t, client, srv, row.ID))
	}
	again := readUpTo(t, client, srv, row.ID, first.ID)
	if again != first.ID || unread(t, client, srv, row.ID) != 1 {
		t.Fatalf("backwards cursor %d", again)
	}
	cursor = readUpTo(t, client, srv, row.ID, second.ID)
	if cursor != second.ID || unread(t, client, srv, row.ID) != 0 {
		t.Fatalf("caught up cursor %d unread %d", cursor, unread(t, client, srv, row.ID))
	}
	if got := readUpTo(t, client, srv, row.ID, first.ID); got != second.ID || unread(t, client, srv, row.ID) != 0 {
		t.Fatalf("stayed %d", got)
	}

	code, raw := do(t, client, http.MethodPost, chatReadURL(srv, row.ID), map[string]any{"lastMessageId": foreign.ID})
	requireCode(t, code, raw, http.StatusNotFound, "not_found", "Message not found.")
	if got := readUpTo(t, client, srv, row.ID, second.ID); got != second.ID {
		t.Fatalf("cursor moved to %d", got)
	}
	for _, body := range []any{map[string]any{}, map[string]any{"lastMessageId": 0}, map[string]any{"lastMessageId": -3}} {
		code, raw = do(t, client, http.MethodPost, chatReadURL(srv, row.ID), body)
		requireCode(t, code, raw, http.StatusBadRequest, "invalid", "Check the message and try again.")
	}
	if n := countWhere(t, database, `SELECT count(*) FROM chat_reads WHERE chat_id = ? AND user_id = ?`, row.ID, me.ID); n != 1 {
		t.Fatalf("read rows %d", n)
	}
	var stored int64
	if err := database.Writer().QueryRowContext(t.Context(), `SELECT last_read_message_id FROM chat_reads WHERE chat_id = ? AND user_id = ?`, row.ID, me.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != second.ID {
		t.Fatalf("stored %d want %d", stored, second.ID)
	}

	code, raw = do(t, client, http.MethodPost, srv.URL+"/api/chats/random", nil)
	if code != http.StatusCreated {
		t.Fatalf("rematch %d %s", code, raw)
	}
	if unread(t, client, srv, row.ID) != 1 {
		t.Fatal("system message was not unread")
	}
	page := getMessages(t, client, srv, row.ID, "")
	system := page.Messages[len(page.Messages)-1]
	if system.Kind != "system" || system.SenderID != nil || system.Body != "Matched again ☕" {
		t.Fatalf("system %+v", system)
	}
	if got := readUpTo(t, client, srv, row.ID, system.ID); got != system.ID || unread(t, client, srv, row.ID) != 0 {
		t.Fatalf("read system %d unread %d", got, unread(t, client, srv, row.ID))
	}
}

func TestNonMemberIsNotFound(t *testing.T) {
	srv := newServer(t, nil)
	database := testutil.Database(t, srv)
	a := testutil.CreateUser(t, database)
	b := testutil.CreateUser(t, database)
	stranger := testutil.CreateUser(t, database)
	row := testutil.CreateChat(t, database, testutil.WithChatUsers(a.ID, b.ID))
	msg := testutil.CreateMessage(t, database, testutil.WithChat(row.ID), testutil.WithSender(a.ID), testutil.WithMessageBody("secret"))
	client := testutil.LoginAs(t, srv, stranger)

	code, raw := do(t, client, http.MethodGet, chatMessagesURL(srv, row.ID)+"?before="+itoa(msg.ID), nil)
	requireCode(t, code, raw, http.StatusNotFound, "not_found", "Chat not found.")
	code, raw = do(t, client, http.MethodPost, chatMessagesURL(srv, row.ID), map[string]any{"body": ""})
	requireCode(t, code, raw, http.StatusNotFound, "not_found", "Chat not found.")
	code, raw = do(t, client, http.MethodPost, chatMessagesURL(srv, row.ID), map[string]any{"body": "hi"})
	requireCode(t, code, raw, http.StatusNotFound, "not_found", "Chat not found.")
	code, raw = do(t, client, http.MethodPost, chatReadURL(srv, row.ID), map[string]any{})
	requireCode(t, code, raw, http.StatusNotFound, "not_found", "Chat not found.")
	code, raw = do(t, client, http.MethodPost, chatReadURL(srv, row.ID), map[string]any{"lastMessageId": msg.ID})
	requireCode(t, code, raw, http.StatusNotFound, "not_found", "Chat not found.")
	code, raw = do(t, client, http.MethodGet, srv.URL+"/api/chats/999999/messages", nil)
	requireCode(t, code, raw, http.StatusNotFound, "not_found", "Chat not found.")

	if getChats(t, client, srv) == nil || len(getChats(t, client, srv)) != 0 {
		t.Fatal("stranger sees a chat")
	}
	if n := countWhere(t, database, `SELECT count(*) FROM messages WHERE chat_id = ?`, row.ID); n != 1 {
		t.Fatalf("messages %d", n)
	}
	if n := countWhere(t, database, `SELECT count(*) FROM chat_reads WHERE user_id = ?`, stranger.ID); n != 0 {
		t.Fatalf("reads %d", n)
	}
}

func TestNotifier(t *testing.T) {
	rec := &recorder{}
	srv := newServer(t, rec)
	database := testutil.Database(t, srv)
	a := testutil.CreateUser(t, database, testutil.WithName("Ann"))
	b := testutil.CreateUser(t, database, testutil.WithName("Bo"))
	client := testutil.LoginAs(t, srv, a)

	code, raw := do(t, client, http.MethodPost, srv.URL+"/api/chats/random", nil)
	if code != http.StatusCreated {
		t.Fatalf("match %d %s", code, raw)
	}
	var created randomView
	mustDecode(t, raw, &created)
	chats, msgs := rec.snapshot()
	if len(chats) != 1 || len(msgs) != 0 || chats[0].chat.ID != created.Chat.ID || created.Chat.Partner.ID != b.ID {
		t.Fatalf("after create chats %d msgs %d partner %d", len(chats), len(msgs), created.Chat.Partner.ID)
	}
	low, high := chatPair(t, database, created.Chat.ID)
	if chats[0].ids[0] != low || chats[0].ids[1] != high || chats[0].chat.UserLow != low {
		t.Fatalf("ids %+v", chats[0])
	}

	code, raw = do(t, client, http.MethodPost, chatMessagesURL(srv, created.Chat.ID), map[string]any{"body": "hi"})
	if code != http.StatusCreated {
		t.Fatalf("post %d %s", code, raw)
	}
	_, msgs = rec.snapshot()
	if len(msgs) != 1 || msgs[0].msg.Body != "hi" || msgs[0].msg.Kind != "text" || msgs[0].msg.SenderID == nil || *msgs[0].msg.SenderID != a.ID {
		t.Fatalf("text event %+v", msgs)
	}

	code, raw = do(t, client, http.MethodPost, srv.URL+"/api/chats/random", nil)
	if code != http.StatusCreated {
		t.Fatalf("rematch %d %s", code, raw)
	}
	chats, msgs = rec.snapshot()
	if len(chats) != 1 || len(msgs) != 2 || msgs[1].msg.Kind != "system" || msgs[1].msg.Body != "Matched again ☕" || msgs[1].msg.SenderID != nil {
		t.Fatalf("after rematch chats %d last %+v", len(chats), msgs[len(msgs)-1].msg)
	}

	offSrv := newServer(t, rec)
	offDB := testutil.Database(t, offSrv)
	off := testutil.CreateUser(t, offDB, testutil.WithAvailable(false))
	testutil.CreateUser(t, offDB)
	before, _ := rec.snapshot()
	code, raw = do(t, testutil.LoginAs(t, offSrv, off), http.MethodPost, offSrv.URL+"/api/chats/random", nil)
	requireCode(t, code, raw, http.StatusConflict, "you_are_unavailable", "")
	after, _ := rec.snapshot()
	if len(after) != len(before) {
		t.Fatalf("notified on 409: %d to %d", len(before), len(after))
	}
}

func TestUnauthenticated(t *testing.T) {
	srv := newServer(t, nil)
	client := anonClient(srv)
	requests := []struct {
		method string
		url    string
		body   any
	}{
		{http.MethodPut, srv.URL + "/api/me/availability", map[string]any{"available": false}},
		{http.MethodPost, srv.URL + "/api/chats/random", nil},
		{http.MethodGet, srv.URL + "/api/chats", nil},
		{http.MethodGet, srv.URL + "/api/chats/1/messages", nil},
		{http.MethodPost, srv.URL + "/api/chats/1/messages", map[string]any{"body": "hi"}},
		{http.MethodPost, srv.URL + "/api/chats/1/read", map[string]any{"lastMessageId": 1}},
	}
	for _, req := range requests {
		code, raw := do(t, client, req.method, req.url, req.body)
		requireCode(t, code, raw, http.StatusUnauthorized, "unauthenticated", "Sign in required.")
	}
}

type availabilityView struct {
	Available bool `json:"available"`
}

type partnerView struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	AvatarURL *string `json:"avatarUrl"`
}

type lastView struct {
	Body      string `json:"body"`
	Kind      string `json:"kind"`
	CreatedAt string `json:"createdAt"`
}

type chatView struct {
	ID          int64       `json:"id"`
	Partner     partnerView `json:"partner"`
	LastMessage *lastView   `json:"lastMessage"`
	UnreadCount int64       `json:"unreadCount"`
}

type randomView struct {
	Chat    chatView `json:"chat"`
	Rematch bool     `json:"rematch"`
}

type messageView struct {
	ID        int64  `json:"id"`
	SenderID  *int64 `json:"senderId"`
	Kind      string `json:"kind"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
}

type messagesView struct {
	Messages []messageView `json:"messages"`
}

type readView struct {
	LastMessageID int64 `json:"lastMessageId"`
}

type meView struct {
	Available bool `json:"available"`
}

type notifiedChat struct {
	chat chat.Chat
	ids  []int64
}

type notifiedMsg struct {
	msg chat.Message
	ids []int64
}

type recorder struct {
	mu    sync.Mutex
	chats []notifiedChat
	msgs  []notifiedMsg
}

func (r *recorder) ChatCreated(c chat.Chat, ids []int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.chats = append(r.chats, notifiedChat{chat: c, ids: append([]int64(nil), ids...)})
}

func (r *recorder) MessageCreated(m chat.Message, ids []int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, notifiedMsg{msg: m, ids: append([]int64(nil), ids...)})
}

func (r *recorder) snapshot() ([]notifiedChat, []notifiedMsg) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]notifiedChat(nil), r.chats...), append([]notifiedMsg(nil), r.msgs...)
}

func newServer(t *testing.T, notifier chat.Notifier) *httptest.Server {
	t.Helper()
	database := testutil.NewDB(t)
	cfg := config.Config{Env: "test", Version: "test"}
	unstarted := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	cfg.AppBaseURL = "http://" + unstarted.Listener.Addr().String()
	app := server.New(cfg, server.Deps{
		DB:           database,
		Log:          slog.New(slog.DiscardHandler),
		ChatNotifier: notifier,
	})
	unstarted.Config.Handler = app
	t.Cleanup(func() {
		unstarted.Close()
		app.Close()
	})
	unstarted.Start()
	return unstarted
}

type originTransport struct {
	base   http.RoundTripper
	origin string
}

func (t *originTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		req.Header.Set("Origin", t.origin)
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

func anonClient(srv *httptest.Server) *http.Client {
	base := srv.Client()
	client := *base
	client.Transport = &originTransport{base: base.Transport, origin: srv.URL}
	return &client
}

func do(t *testing.T, client *http.Client, method, url string, body any) (int, []byte) {
	t.Helper()
	code, raw, err := doErr(client, method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("{")) && !bytes.Contains(raw, []byte("[")) {
		t.Fatalf("not json: %s", raw)
	}
	return code, raw
}

func doErr(client *http.Client, method, url string, body any) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return res.StatusCode, nil, err
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/json" {
		return res.StatusCode, raw, errContentType(ct)
	}
	return res.StatusCode, raw, nil
}

type errContentType string

func (e errContentType) Error() string { return "content-type " + string(e) }

func mustDecode(t *testing.T, raw []byte, dest any) {
	t.Helper()
	if err := json.Unmarshal(raw, dest); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
}

func requireCode(t *testing.T, code int, raw []byte, status int, apiCode, message string) {
	t.Helper()
	if code != status {
		t.Fatalf("status %d, want %d (%s)", code, status, raw)
	}
	var body httpx.Error
	mustDecode(t, raw, &body)
	if body.Code != apiCode || (message != "" && body.Message != message) {
		t.Fatalf("error %+v want %s %q", body, apiCode, message)
	}
	var keys map[string]any
	mustDecode(t, raw, &keys)
	if len(keys) != 2 {
		t.Fatalf("keys %v", keys)
	}
}

func getMe(t *testing.T, client *http.Client, srv *httptest.Server) meView {
	t.Helper()
	code, raw := do(t, client, http.MethodGet, srv.URL+"/api/me", nil)
	if code != http.StatusOK {
		t.Fatalf("me %d %s", code, raw)
	}
	var body meView
	mustDecode(t, raw, &body)
	return body
}

func getChats(t *testing.T, client *http.Client, srv *httptest.Server) []chatView {
	t.Helper()
	code, raw := do(t, client, http.MethodGet, srv.URL+"/api/chats", nil)
	if code != http.StatusOK {
		t.Fatalf("list %d %s", code, raw)
	}
	if bytes.TrimSpace(raw)[0] != '[' {
		t.Fatalf("list %s", raw)
	}
	var body []chatView
	mustDecode(t, raw, &body)
	if body == nil {
		t.Fatal("null list")
	}
	return body
}

func listRaw(t *testing.T, client *http.Client, srv *httptest.Server) []byte {
	t.Helper()
	code, raw := do(t, client, http.MethodGet, srv.URL+"/api/chats", nil)
	if code != http.StatusOK {
		t.Fatalf("list %d %s", code, raw)
	}
	return raw
}

func getMessages(t *testing.T, client *http.Client, srv *httptest.Server, chatID int64, query string) messagesView {
	t.Helper()
	url := chatMessagesURL(srv, chatID)
	if query != "" {
		url += "?" + query
	}
	code, raw := do(t, client, http.MethodGet, url, nil)
	if code != http.StatusOK {
		t.Fatalf("messages %s: %d %s", query, code, raw)
	}
	var body messagesView
	mustDecode(t, raw, &body)
	if body.Messages == nil {
		t.Fatal("null messages")
	}
	return body
}

func messagesNull(t *testing.T, client *http.Client, srv *httptest.Server, chatID int64, query string) bool {
	t.Helper()
	url := chatMessagesURL(srv, chatID)
	if query != "" {
		url += "?" + query
	}
	_, raw := do(t, client, http.MethodGet, url, nil)
	var keys map[string]any
	mustDecode(t, raw, &keys)
	msgs, ok := keys["messages"].([]any)
	return !ok || msgs == nil
}

func TestRandomAndMessageLimits(t *testing.T) {
	srv := newServer(t, nil)
	database := testutil.Database(t, srv)
	me := testutil.CreateUser(t, database, testutil.WithName("Me"))
	other := testutil.CreateUser(t, database, testutil.WithName("Other"))
	client := testutil.LoginAs(t, srv, me)
	otherClient := testutil.LoginAs(t, srv, other)

	over := bytes.Repeat([]byte("a"), int(httpx.SmallBodyLimit)+1)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/chats/random", bytes.NewReader(over))
	if err != nil {
		t.Fatal(err)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	requireCode(t, res.StatusCode, raw, http.StatusBadRequest, "too_large", "Request body is too large.")
	if n := countWhere(t, database, `SELECT count(*) FROM chats`); n != 0 {
		t.Fatalf("chats %d", n)
	}

	for i := 1; i <= 10; i++ {
		code, body := do(t, client, http.MethodPost, srv.URL+"/api/chats/random", nil)
		if code != http.StatusCreated {
			t.Fatalf("random %d: %d %s", i, code, body)
		}
	}
	code, body := do(t, client, http.MethodPost, srv.URL+"/api/chats/random", nil)
	requireCode(t, code, body, http.StatusTooManyRequests, "rate_limited", "Too many requests. Try again in a minute.")
	code, body = do(t, otherClient, http.MethodPost, srv.URL+"/api/chats/random", nil)
	if code != http.StatusCreated {
		t.Fatalf("other random %d %s", code, body)
	}

	listed := getChats(t, client, srv)
	if len(listed) != 1 {
		t.Fatalf("chats %+v", listed)
	}
	msgURL := chatMessagesURL(srv, listed[0].ID)
	big := bytes.Repeat([]byte("b"), int(httpx.JSONBodyLimit)+1)
	req, err = http.NewRequest(http.MethodPost, msgURL, bytes.NewReader(big))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = otherClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = io.ReadAll(res.Body)
	_ = res.Body.Close()
	requireCode(t, res.StatusCode, raw, http.StatusBadRequest, "too_large", "Request body is too large.")

	for i := 1; i <= 60; i++ {
		code, body = do(t, otherClient, http.MethodPost, msgURL, map[string]any{"body": "m"})
		if code != http.StatusCreated {
			t.Fatalf("message %d: %d %s", i, code, body)
		}
	}
	code, body = do(t, otherClient, http.MethodPost, msgURL, map[string]any{"body": "over"})
	requireCode(t, code, body, http.StatusTooManyRequests, "rate_limited", "")
	code, body = do(t, client, http.MethodPost, msgURL, map[string]any{"body": "still ok"})
	if code != http.StatusCreated {
		t.Fatalf("other sender %d %s", code, body)
	}
}

func readUpTo(t *testing.T, client *http.Client, srv *httptest.Server, chatID, messageID int64) int64 {
	t.Helper()
	code, raw := do(t, client, http.MethodPost, chatReadURL(srv, chatID), map[string]any{"lastMessageId": messageID})
	if code != http.StatusOK {
		t.Fatalf("read %d %s", code, raw)
	}
	var body readView
	mustDecode(t, raw, &body)
	return body.LastMessageID
}

func unread(t *testing.T, client *http.Client, srv *httptest.Server, chatID int64) int64 {
	t.Helper()
	for _, item := range getChats(t, client, srv) {
		if item.ID == chatID {
			return item.UnreadCount
		}
	}
	t.Fatalf("chat %d missing", chatID)
	return 0
}

func chatMessagesURL(srv *httptest.Server, id int64) string {
	return srv.URL + "/api/chats/" + itoa(id) + "/messages"
}

func chatReadURL(srv *httptest.Server, id int64) string {
	return srv.URL + "/api/chats/" + itoa(id) + "/read"
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

func assertRFC3339(t *testing.T, value string) {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, value)
	if err != nil || ts.Location() != time.UTC {
		t.Fatalf("time %q: %v", value, err)
	}
}

func countWhere(t *testing.T, database *db.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := database.Writer().QueryRowContext(t.Context(), query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func chatPair(t *testing.T, database *db.DB, id int64) (low, high int64) {
	t.Helper()
	err := database.Writer().QueryRowContext(t.Context(), `SELECT user_low, user_high FROM chats WHERE id = ?`, id).Scan(&low, &high)
	if err != nil {
		t.Fatal(err)
	}
	return low, high
}

func deleteChat(t *testing.T, database *db.DB, id int64) {
	t.Helper()
	res, err := database.Writer().ExecContext(t.Context(), `DELETE FROM chats WHERE id = ?`, id)
	if err != nil {
		t.Fatal(err)
	}
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		t.Fatalf("delete chat: %v rows %d", err, n)
	}
}

func setLastMessageAt(t *testing.T, database *db.DB, id int64, ts string) {
	t.Helper()
	res, err := database.Writer().ExecContext(t.Context(), `UPDATE chats SET last_message_at = ? WHERE id = ?`, ts, id)
	if err != nil {
		t.Fatal(err)
	}
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		t.Fatalf("touch chat: %v rows %d", err, n)
	}
}

func firstBody(page messagesView) string {
	if len(page.Messages) == 0 {
		return ""
	}
	return page.Messages[0].Body
}

func lastBody(page messagesView) string {
	if len(page.Messages) == 0 {
		return ""
	}
	return page.Messages[len(page.Messages)-1].Body
}
