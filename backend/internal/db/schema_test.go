package db

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func TestDeleteUserCascades(t *testing.T) {
	database := openTemp(t)
	ctx := context.Background()
	w := database.Writer()

	// Bea=1 and Cy=3 stay. Ada=2 is deleted and is user_high in one chat and user_low in the other.
	insertUser(t, w, 1, "bea@example.com")
	insertUser(t, w, 2, "ada@example.com")
	insertUser(t, w, 3, "cy@example.com")

	exec(t, w, `INSERT INTO interests (user_id, hobby, updated_at) VALUES (2, 'chess', ?)`, ts)
	exec(t, w, `INSERT INTO photos (id, user_id, path, created_at) VALUES (10, 2, 'p/ada.png', ?)`, ts)
	exec(t, w, `INSERT INTO photos (id, user_id, path, created_at) VALUES (11, 1, 'p/bea.png', ?)`, ts)
	// Ada's wall, authored by Ada, with a reply from Bea.
	exec(t, w, `INSERT INTO wall_posts (id, wall_owner_id, author_id, body, created_at) VALUES (20, 2, 2, 'hello', ?)`, ts)
	exec(t, w, `INSERT INTO wall_posts (id, wall_owner_id, author_id, parent_id, body, created_at) VALUES (21, 2, 1, 20, 'hi', ?)`, ts)
	// Ada posts on Bea's wall.
	exec(t, w, `INSERT INTO wall_posts (id, wall_owner_id, author_id, body, created_at) VALUES (22, 1, 2, 'on your wall', ?)`, ts)
	// Bea's own post stays.
	exec(t, w, `INSERT INTO wall_posts (id, wall_owner_id, author_id, body, created_at) VALUES (23, 1, 1, 'mine', ?)`, ts)

	exec(t, w, `INSERT INTO chats (id, user_low, user_high, created_at, last_message_at) VALUES (30, 1, 2, ?, ?)`, ts, ts)
	exec(t, w, `INSERT INTO chats (id, user_low, user_high, created_at, last_message_at) VALUES (31, 2, 3, ?, ?)`, ts, ts)
	exec(t, w, `INSERT INTO messages (id, chat_id, sender_id, kind, body, created_at) VALUES (40, 30, 2, 'text', 'hey', ?)`, ts)
	exec(t, w, `INSERT INTO messages (id, chat_id, sender_id, kind, body, created_at) VALUES (41, 30, NULL, 'system', 'Matched again ☕', ?)`, ts)
	exec(t, w, `INSERT INTO messages (id, chat_id, sender_id, kind, body, created_at) VALUES (42, 31, 2, 'text', 'other', ?)`, ts)
	exec(t, w, `INSERT INTO chat_reads (chat_id, user_id, last_read_message_id) VALUES (30, 1, 40), (30, 2, 0), (31, 2, 0), (31, 3, 0)`)

	exec(t, w, `INSERT INTO sessions (token, data, expiry) VALUES ('tok', x'00', 1.5)`)
	exec(t, w, `INSERT INTO admin_audit (admin_email, action, target_email_sha256, created_at) VALUES ('admin@example.com', 'delete', 'abc', ?)`, ts)

	// A photo for a missing user is rejected.
	if _, err := w.ExecContext(ctx, `INSERT INTO photos (user_id, path, created_at) VALUES (99, 'nope', ?)`, ts); err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("fk insert err = %v", err)
	}

	exec(t, w, `DELETE FROM users WHERE id = 2`)

	if n := count(t, w, `SELECT count(*) FROM users WHERE id = 2`); n != 0 {
		t.Fatalf("user 2 still present")
	}
	if n := count(t, w, `SELECT count(*) FROM users`); n != 2 {
		t.Fatalf("users = %d, want 2", n)
	}
	if n := count(t, w, `SELECT count(*) FROM interests`); n != 0 {
		t.Fatalf("interests = %d", n)
	}
	if n := count(t, w, `SELECT count(*) FROM photos WHERE user_id = 2`); n != 0 {
		t.Fatalf("ada photos = %d", n)
	}
	if n := count(t, w, `SELECT count(*) FROM photos`); n != 1 {
		t.Fatalf("photos = %d, want bea's only", n)
	}
	// Posts Ada owned or wrote, including Bea's reply on Ada's wall, are gone.
	if n := count(t, w, `SELECT count(*) FROM wall_posts WHERE wall_owner_id = 2 OR author_id = 2`); n != 0 {
		t.Fatalf("ada posts left = %d", n)
	}
	if n := count(t, w, `SELECT count(*) FROM wall_posts WHERE id = 23`); n != 1 {
		t.Fatalf("bea's post missing")
	}
	if n := count(t, w, `SELECT count(*) FROM chats`); n != 0 {
		t.Fatalf("chats left = %d", n)
	}
	if n := count(t, w, `SELECT count(*) FROM messages`); n != 0 {
		t.Fatalf("messages left = %d", n)
	}
	if n := count(t, w, `SELECT count(*) FROM chat_reads`); n != 0 {
		t.Fatalf("chat_reads left = %d", n)
	}
	if n := count(t, w, `SELECT count(*) FROM sessions`); n != 1 {
		t.Fatalf("sessions = %d", n)
	}
	if n := count(t, w, `SELECT count(*) FROM admin_audit`); n != 1 {
		t.Fatalf("admin_audit = %d", n)
	}
}

func TestChatUserOrderCheck(t *testing.T) {
	database := openTemp(t)
	w := database.Writer()
	insertUser(t, w, 1, "ada@example.com")
	insertUser(t, w, 2, "bea@example.com")

	exec(t, w, `INSERT INTO chats (user_low, user_high, created_at, last_message_at) VALUES (1, 2, ?, ?)`, ts, ts)

	if _, err := w.Exec(`INSERT INTO chats (user_low, user_high, created_at, last_message_at) VALUES (2, 1, ?, ?)`, ts, ts); err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("high < low err = %v", err)
	}
	if _, err := w.Exec(`INSERT INTO chats (user_low, user_high, created_at, last_message_at) VALUES (1, 1, ?, ?)`, ts, ts); err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("equal err = %v", err)
	}
	if n := count(t, w, `SELECT count(*) FROM chats`); n != 1 {
		t.Fatalf("chats = %d, want 1", n)
	}
}

func TestMessageKindCheck(t *testing.T) {
	database := openTemp(t)
	w := database.Writer()
	insertUser(t, w, 1, "ada@example.com")
	insertUser(t, w, 2, "bea@example.com")
	exec(t, w, `INSERT INTO chats (id, user_low, user_high, created_at, last_message_at) VALUES (1, 1, 2, ?, ?)`, ts, ts)

	_, err := w.Exec(`INSERT INTO messages (chat_id, sender_id, kind, body, created_at) VALUES (1, 1, 'image', 'nope', ?)`, ts)
	if err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("kind err = %v", err)
	}
}

func insertUser(t *testing.T, w *sql.DB, id int64, email string) {
	t.Helper()
	exec(t, w, `INSERT INTO users (id, google_sub, email, name, created_at, last_login_at) VALUES (?, ?, ?, 'Name', ?, ?)`,
		id, "sub-"+email, email, ts, ts)
}

func exec(t *testing.T, w *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := w.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec %s: %v", query, err)
	}
}

func count(t *testing.T, w *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := w.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}
