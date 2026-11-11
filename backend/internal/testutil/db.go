// Package testutil holds shared database, factory, and HTTP helpers for tests.
package testutil

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/rhamdeew/dbprc/backend/internal/db"
)

// NewDB opens a migrated SQLite database in a temp directory and closes it
// when the test ends.
func NewDB(t *testing.T) *db.DB {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)

	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "arc.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	return database
}

func begin(t *testing.T, database *db.DB) *sql.Tx {
	t.Helper()
	tx, err := database.Writer().BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	return tx
}

func commit(t *testing.T, tx *sql.Tx) {
	t.Helper()
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
