package testutil

import (
	"strings"
	"testing"
	"time"

	"github.com/rhamdeew/dbprc/backend/internal/db"
)

func TestCreateUserDefaultsAndOptions(t *testing.T) {
	database := NewDB(t)
	first := CreateUser(t, database, nil)
	second := CreateUser(t, database,
		WithEmail("Ada@Example.com"),
		WithName("Ada"),
		WithGoogleSub("google-ada"),
		WithAvailable(false),
		WithAvatarPath("avatars/ada.png"),
	)
	if first.ID == second.ID || first.Email == second.Email || first.GoogleSub == second.GoogleSub {
		t.Fatalf("users not distinct: %+v %+v", first, second)
	}
	if !strings.HasSuffix(first.Email, "@example.com") || first.Available != 1 || first.AvatarPath != nil {
		t.Fatalf("defaults %+v", first)
	}
	if !strings.HasSuffix(first.CreatedAt, "Z") || !strings.HasSuffix(first.LastLoginAt, "Z") {
		t.Fatalf("timestamps %+v", first)
	}
	if _, err := time.Parse(time.RFC3339, first.CreatedAt); err != nil {
		t.Fatal(err)
	}
	if second.Email != "Ada@Example.com" || second.Name != "Ada" || second.GoogleSub != "google-ada" || second.Available != 0 {
		t.Fatalf("options %+v", second)
	}
	if second.AvatarPath == nil || *second.AvatarPath != "avatars/ada.png" {
		t.Fatalf("avatar %+v", second)
	}
	if countRows(t, database, `SELECT count(*) FROM users`) != 2 {
		t.Fatal("user count")
	}
}

func TestCreatePhoto(t *testing.T) {
	database := NewDB(t)
	auto := CreatePhoto(t, database)
	if auto.ID == 0 || auto.UserID == 0 || auto.Path == "" || auto.CreatedAt == "" {
		t.Fatalf("auto %+v", auto)
	}
	owner := CreateUser(t, database, WithEmail("ada@example.com"))
	photo := CreatePhoto(t, database, WithPhotoUser(owner.ID), WithPhotoPath("p/ada.png"))
	if photo.UserID != owner.ID || photo.Path != "p/ada.png" {
		t.Fatalf("photo %+v", photo)
	}
	if countRows(t, database, `SELECT count(*) FROM users`) != 2 {
		t.Fatal("photo created an extra user")
	}
	if countRows(t, database, `SELECT count(*) FROM photos WHERE user_id = ?`, owner.ID) != 1 {
		t.Fatal("owner photo missing")
	}
}

func TestCreateWallPost(t *testing.T) {
	database := NewDB(t)
	post := CreateWallPost(t, database)
	if post.WallOwnerID == 0 || post.WallOwnerID != post.AuthorID || post.Body == "" || post.ParentID != nil || post.DrawingPath != nil || post.IsAnonymous {
		t.Fatalf("defaults %+v", post)
	}

	owner := CreateUser(t, database, WithEmail("owner@example.com"))
	author := CreateUser(t, database, WithEmail("author@example.com"))
	note := CreateWallPost(t, database,
		WithWallOwner(owner.ID),
		WithAuthor(author.ID),
		WithBody("hello"),
		WithAnonymous(),
		WithDrawingPath("drawings/a.png"),
	)
	if note.WallOwnerID != owner.ID || note.AuthorID != author.ID || note.Body != "hello" || !note.IsAnonymous {
		t.Fatalf("note %+v", note)
	}
	if note.DrawingPath == nil || *note.DrawingPath != "drawings/a.png" {
		t.Fatalf("drawing %+v", note)
	}

	ownWall := CreateWallPost(t, database, WithWallOwner(owner.ID), WithBody("mine"))
	if ownWall.AuthorID != owner.ID || ownWall.Body != "mine" {
		t.Fatalf("own wall %+v", ownWall)
	}
	authored := CreateWallPost(t, database, WithAuthor(author.ID), WithBody("theirs"))
	if authored.WallOwnerID != author.ID || authored.AuthorID != author.ID {
		t.Fatalf("author only %+v", authored)
	}

	reply := CreateWallPost(t, database,
		WithWallOwner(note.WallOwnerID),
		WithAuthor(owner.ID),
		WithParent(note.ID),
		WithBody("reply"),
	)
	if reply.ParentID == nil || *reply.ParentID != note.ID || reply.Body != "reply" {
		t.Fatalf("reply %+v", reply)
	}
}

func TestCreateChatOrdersUsers(t *testing.T) {
	database := NewDB(t)
	auto := CreateChat(t, database)
	if auto.UserLow == 0 || auto.UserLow >= auto.UserHigh || auto.CreatedAt == "" || auto.LastMessageAt != auto.CreatedAt {
		t.Fatalf("auto %+v", auto)
	}
	if countRows(t, database, `SELECT count(*) FROM users`) != 2 {
		t.Fatal("chat users")
	}

	low := CreateUser(t, database, WithEmail("low@example.com"))
	high := CreateUser(t, database, WithEmail("high@example.com"))
	if low.ID > high.ID {
		low, high = high, low
	}
	chat := CreateChat(t, database, WithChatUsers(high.ID, low.ID))
	if chat.UserLow != low.ID || chat.UserHigh != high.ID {
		t.Fatalf("order %+v want %d < %d", chat, low.ID, high.ID)
	}
}

func TestCreateMessage(t *testing.T) {
	database := NewDB(t)
	auto := CreateMessage(t, database)
	if auto.Kind != "text" || auto.Body == "" || auto.SenderID == nil || auto.ChatID == 0 {
		t.Fatalf("auto %+v", auto)
	}
	if countRows(t, database, `SELECT count(*) FROM users`) != 2 {
		t.Fatal("message users")
	}
	if countRows(t, database, `SELECT count(*) FROM chats`) != 1 {
		t.Fatal("message chat")
	}
	assertLastMessageAt(t, database, auto)

	chat := CreateChat(t, database)
	text := CreateMessage(t, database, WithChat(chat.ID), WithMessageBody("hey"), WithSender(chat.UserHigh))
	if text.ChatID != chat.ID || text.Body != "hey" || text.SenderID == nil || *text.SenderID != chat.UserHigh {
		t.Fatalf("text %+v", text)
	}
	assertLastMessageAt(t, database, text)

	system := CreateMessage(t, database, WithChat(chat.ID), WithKind("system"), WithMessageBody("Matched again ☕"))
	if system.Kind != "system" || system.Body != "Matched again ☕" || system.SenderID != nil {
		t.Fatalf("system %+v", system)
	}
	assertLastMessageAt(t, database, system)

	withSender := CreateMessage(t, database, WithChat(chat.ID), WithKind("system"), WithSender(chat.UserLow), WithMessageBody("again"))
	if withSender.SenderID == nil || *withSender.SenderID != chat.UserLow || withSender.Kind != "system" {
		t.Fatalf("system with sender %+v", withSender)
	}
}

func assertLastMessageAt(t *testing.T, database *db.DB, msg Message) {
	t.Helper()
	var last string
	err := database.Writer().QueryRowContext(t.Context(), `SELECT last_message_at FROM chats WHERE id = ?`, msg.ChatID).Scan(&last)
	if err != nil {
		t.Fatal(err)
	}
	if last != msg.CreatedAt {
		t.Fatalf("last_message_at %s, message %s", last, msg.CreatedAt)
	}
}

func countRows(t *testing.T, database *db.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := database.Writer().QueryRowContext(t.Context(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}
