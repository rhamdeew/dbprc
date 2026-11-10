package testutil

import (
	"testing"
	"time"
)

func TestNewDBMigratesAndCloses(t *testing.T) {
	var opened bool
	t.Run("open", func(t *testing.T) {
		database := NewDB(t)
		opened = true
		for _, table := range []string{"users", "photos", "sessions", "wall_posts", "chats", "messages"} {
			var n int
			err := database.Writer().QueryRowContext(t.Context(),
				`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&n)
			if err != nil {
				t.Fatal(err)
			}
			if n != 1 {
				t.Fatalf("table %s missing", table)
			}
		}
		user := CreateUser(t, database)
		if user.ID == 0 || user.CreatedAt == "" {
			t.Fatalf("user %+v", user)
		}
		if _, err := time.Parse(time.RFC3339, user.CreatedAt); err != nil {
			t.Fatal(err)
		}
	})
	if !opened {
		t.Fatal("subtest did not run")
	}
}

func TestNewDBCloseStopsQueries(t *testing.T) {
	var ping func() error
	t.Run("open", func(t *testing.T) {
		database := NewDB(t)
		ping = database.Writer().Ping
	})
	if err := ping(); err == nil {
		t.Fatal("expected closed database")
	}
}
