package db

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const ts = "2026-11-10T12:00:00Z"

func openTemp(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nested dir", "arc.db")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	database, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return database
}

func TestOpenRejectsEmptyPath(t *testing.T) {
	if _, err := Open(context.Background(), ""); err == nil {
		t.Fatal("expected error")
	}
}

func TestOpenPragmasAndPools(t *testing.T) {
	database := openTemp(t)
	ctx := context.Background()

	if got := database.Writer().Stats().MaxOpenConnections; got != writePoolMax {
		t.Fatalf("write pool max = %d, want %d", got, writePoolMax)
	}
	if got := database.Reader().Stats().MaxOpenConnections; got != readPoolMax {
		t.Fatalf("read pool max = %d, want %d", got, readPoolMax)
	}
	for _, pool := range []*sql.DB{database.Writer(), database.Reader()} {
		if err := verifyPragmas(ctx, pool); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := database.Writer().ExecContext(ctx,
		`INSERT INTO users (google_sub, email, name, created_at, last_login_at) VALUES (?, ?, ?, ?, ?)`,
		"sub", "ada@example.com", "Ada", ts, ts,
	); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := database.Reader().QueryRowContext(ctx, `SELECT name FROM users WHERE email = ?`, "ada@example.com").Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "Ada" {
		t.Fatalf("reader saw %q", name)
	}
}

func TestMigrateUpDownUp(t *testing.T) {
	database := openTemp(t)
	ctx := context.Background()

	assertTable(t, database.Writer(), "users", true)
	assertTable(t, database.Writer(), "chats", true)
	assertTable(t, database.Writer(), "goose_db_version", true)

	if err := database.migrateDown(ctx); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"users", "interests", "photos", "sessions", "wall_posts", "chats", "messages", "chat_reads", "admin_audit"} {
		assertTable(t, database.Writer(), table, false)
	}

	if err := migrateUp(ctx, database.Writer()); err != nil {
		t.Fatal(err)
	}
	assertTable(t, database.Writer(), "users", true)
	assertTable(t, database.Writer(), "messages", true)

	if _, err := database.Writer().ExecContext(ctx,
		`INSERT INTO users (google_sub, email, name, created_at, last_login_at) VALUES (?, ?, ?, ?, ?)`,
		"sub", "ada@example.com", "Ada", ts, ts,
	); err != nil {
		t.Fatalf("insert after second up: %v", err)
	}

	// Opening the same file again does not re-apply migrations.
	path := pragmaDBPath(t, database.Writer())
	again, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = again.Close() })
	var n int
	if err := again.Reader().QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("users = %d, want 1", n)
	}
}

func assertTable(t *testing.T, pool *sql.DB, name string, want bool) {
	t.Helper()
	var n int
	err := pool.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if (n == 1) != want {
		t.Fatalf("table %s exists = %v, want %v", name, n == 1, want)
	}
}

func pragmaDBPath(t *testing.T, pool *sql.DB) string {
	t.Helper()
	rows, err := pool.Query(`PRAGMA database_list`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var seq int
		var name, file string
		if err := rows.Scan(&seq, &name, &file); err != nil {
			t.Fatal(err)
		}
		if name == "main" && file != "" {
			return file
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	t.Fatal("database_list has no main file")
	return ""
}

func TestVerifyPragmasRejectsPartialSetup(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		pragmas []string
		want    string
	}{
		{name: "defaults", want: "journal_mode"},
		{name: "wal only", pragmas: []string{"journal_mode(WAL)"}, want: "foreign_keys"},
		{name: "no busy timeout", pragmas: []string{"journal_mode(WAL)", "foreign_keys(ON)"}, want: "busy_timeout"},
		{name: "no synchronous", pragmas: []string{"journal_mode(WAL)", "foreign_keys(ON)", "busy_timeout(5000)"}, want: "synchronous"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			pool := openRaw(t, tt.pragmas)
			err := verifyPragmas(context.Background(), pool)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestOpenRejectsFileParent(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), filepath.Join(blocker, "arc.db")); err == nil {
		t.Fatal("expected error")
	}
}

func openRaw(t *testing.T, pragmas []string) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plain.db")
	query := url.Values{}
	for _, pragma := range pragmas {
		query.Add("_pragma", pragma)
	}
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}).String()
	pool, err := sql.Open(driverName, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := pool.Ping(); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestSQLiteDSNEscapesPath(t *testing.T) {
	dsn, err := sqliteDSN("/tmp/my data/arc.db")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dsn, "my%20data") || !strings.Contains(dsn, "_pragma=") {
		t.Fatalf("dsn %q", dsn)
	}
}
