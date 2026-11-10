// Package db opens SQLite, applies embedded goose migrations, and keeps a single
// writer connection separate from the read pool.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"github.com/rhamdeew/dbprc/backend/migrations"
)

const (
	driverName = "sqlite"

	// writePoolMax is 1 because SQLite allows a single writer.
	writePoolMax = 1
	readPoolMax  = 8
)

// DB is the process-wide SQLite handle. Writer is the only connection that
// should run writes. Reader serves reads and may use several connections.
type DB struct {
	write *sql.DB
	read  *sql.DB
}

// Writer returns the write pool (max one connection).
func (d *DB) Writer() *sql.DB { return d.write }

// Reader returns the read pool.
func (d *DB) Reader() *sql.DB { return d.read }

// Open creates parent directories, opens path, checks the required PRAGMAs,
// and applies any pending migrations.
func Open(ctx context.Context, path string) (*DB, error) {
	if path == "" {
		return nil, errors.New("database path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	write, err := openPool(ctx, path, writePoolMax)
	if err != nil {
		return nil, err
	}
	if err := migrateUp(ctx, write); err != nil {
		_ = write.Close()
		return nil, err
	}

	read, err := openPool(ctx, path, readPoolMax)
	if err != nil {
		_ = write.Close()
		return nil, err
	}
	return &DB{write: write, read: read}, nil
}

// Close closes the read pool and then the write pool.
func (d *DB) Close() error {
	return errors.Join(d.read.Close(), d.write.Close())
}

func openPool(ctx context.Context, path string, maxOpen int) (*sql.DB, error) {
	dsn, err := sqliteDSN(path)
	if err != nil {
		return nil, err
	}
	pool, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	pool.SetMaxOpenConns(maxOpen)
	pool.SetMaxIdleConns(maxOpen)
	if err := pool.PingContext(ctx); err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := verifyPragmas(ctx, pool); err != nil {
		_ = pool.Close()
		return nil, err
	}
	return pool, nil
}

func sqliteDSN(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("database path: %w", err)
	}
	query := url.Values{}
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "journal_mode(WAL)")
	query.Add("_pragma", "foreign_keys(ON)")
	query.Add("_pragma", "synchronous(NORMAL)")
	u := url.URL{
		Scheme:   "file",
		Path:     abs,
		RawQuery: query.Encode(),
	}
	return u.String(), nil
}

func verifyPragmas(ctx context.Context, pool *sql.DB) error {
	var journal string
	if err := pool.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal); err != nil {
		return fmt.Errorf("read journal_mode: %w", err)
	}
	if !strings.EqualFold(journal, "wal") {
		return fmt.Errorf("journal_mode is %q, want wal", journal)
	}

	fk, err := pragmaInt(ctx, pool, "PRAGMA foreign_keys")
	if err != nil {
		return fmt.Errorf("read foreign_keys: %w", err)
	}
	if fk != 1 {
		return fmt.Errorf("foreign_keys is %d, want 1", fk)
	}

	busy, err := pragmaInt(ctx, pool, "PRAGMA busy_timeout")
	if err != nil {
		return fmt.Errorf("read busy_timeout: %w", err)
	}
	if busy != 5000 {
		return fmt.Errorf("busy_timeout is %d, want 5000", busy)
	}

	syncMode, err := pragmaInt(ctx, pool, "PRAGMA synchronous")
	if err != nil {
		return fmt.Errorf("read synchronous: %w", err)
	}
	// SQLite encodes NORMAL as 1.
	if syncMode != 1 {
		return fmt.Errorf("synchronous is %d, want 1 (NORMAL)", syncMode)
	}
	return nil
}

func pragmaInt(ctx context.Context, pool *sql.DB, query string) (int, error) {
	var v int
	err := pool.QueryRowContext(ctx, query).Scan(&v)
	return v, err
}

func newProvider(pool *sql.DB) (*goose.Provider, error) {
	provider, err := goose.NewProvider(
		goose.DialectSQLite3,
		pool,
		migrations.FS,
		goose.WithLogger(goose.NopLogger()),
	)
	if err != nil {
		return nil, fmt.Errorf("goose provider: %w", err)
	}
	return provider, nil
}

func migrateUp(ctx context.Context, pool *sql.DB) error {
	provider, err := newProvider(pool)
	if err != nil {
		return err
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

func (d *DB) migrateDown(ctx context.Context) error {
	provider, err := newProvider(d.write)
	if err != nil {
		return err
	}
	if _, err := provider.DownTo(ctx, 0); err != nil {
		return fmt.Errorf("migrate down: %w", err)
	}
	return nil
}
