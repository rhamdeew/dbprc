package sqlc

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/rhamdeew/dbprc/backend/internal/db"
)

func TestUserCRUD(t *testing.T) {
	ctx := context.Background()
	database := openDB(t)
	q := New(database.Writer())
	const ts = "2026-11-10T12:00:00Z"
	avatar := "avatars/ada.png"

	created, err := q.CreateUser(ctx, CreateUserParams{
		GoogleSub:   "google-sub",
		Email:       "Ada@Example.com",
		Name:        "Ada",
		AvatarPath:  &avatar,
		Available:   1,
		CreatedAt:   ts,
		LastLoginAt: ts,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.AvatarPath == nil || *created.AvatarPath != avatar {
		t.Fatalf("created %+v", created)
	}

	byID, err := q.GetUser(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	byEmail, err := q.GetUserByEmail(ctx, "ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	bySub, err := q.GetUserByGoogleSub(ctx, "google-sub")
	if err != nil {
		t.Fatal(err)
	}
	if byID.ID != created.ID || byEmail.Email != "Ada@Example.com" || bySub.GoogleSub != "google-sub" {
		t.Fatalf("lookup %+v %+v %+v", byID, byEmail, bySub)
	}
	if _, err := q.GetUser(ctx, created.ID+99); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing user err = %v", err)
	}

	updated, err := q.UpdateUser(ctx, UpdateUserParams{
		Email:       "ada@example.com",
		Name:        "Ada Lovelace",
		AvatarPath:  nil,
		Available:   0,
		LastLoginAt: "2026-11-10T13:00:00Z",
		ID:          created.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Ada Lovelace" || updated.AvatarPath != nil || updated.Available != 0 || updated.GoogleSub != "google-sub" {
		t.Fatalf("updated %+v", updated)
	}

	if _, err := q.CreateUser(ctx, CreateUserParams{
		GoogleSub:   "other-sub",
		Email:       "ADA@example.com",
		Name:        "Dup",
		Available:   1,
		CreatedAt:   ts,
		LastLoginAt: ts,
	}); err == nil {
		t.Fatal("expected case-insensitive email unique violation")
	}

	tx, err := database.Writer().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.WithTx(tx).CreateUser(ctx, CreateUserParams{
		GoogleSub:   "tx-sub",
		Email:       "tx@example.com",
		Name:        "Tx",
		Available:   1,
		CreatedAt:   ts,
		LastLoginAt: ts,
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := q.GetUserByEmail(ctx, "tx@example.com"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("rolled back user err = %v", err)
	}

	if _, err := database.Writer().ExecContext(ctx,
		`INSERT INTO photos (user_id, path, created_at) VALUES (?, ?, ?)`,
		created.ID, "photos/a.png", ts,
	); err != nil {
		t.Fatal(err)
	}
	if err := q.DeleteUser(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := q.GetUser(ctx, created.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("after delete err = %v", err)
	}
	var photos int
	if err := database.Reader().QueryRowContext(ctx, `SELECT count(*) FROM photos`).Scan(&photos); err != nil {
		t.Fatal(err)
	}
	if photos != 0 {
		t.Fatalf("photos left = %d", photos)
	}
}

func openDB(t *testing.T) *db.DB {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "arc.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return database
}
