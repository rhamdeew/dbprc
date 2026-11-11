package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestRunRejectsBadConfig(t *testing.T) {
	t.Setenv("APP_ENV", "qa")
	err := run(context.Background(), discardLog())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunRejectsDatabaseDirectory(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_ENV", "test")
	t.Setenv("DATA_DIR", filepath.Join(blocker, "child"))
	t.Setenv("PORT", freePort(t))
	if err := run(context.Background(), discardLog()); err == nil {
		t.Fatal("expected error")
	}
}

func TestRunServesUntilCancel(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data dir")
	port := freePort(t)
	t.Setenv("APP_ENV", "test")
	t.Setenv("DATA_DIR", dir)
	t.Setenv("PORT", port)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- run(ctx, discardLog()) }()

	url := "http://127.0.0.1:" + port + "/healthz"
	deadline := time.Now().Add(5 * time.Second)
	var ok bool
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				ok = true
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ok {
		cancel()
		t.Fatalf("server did not become ready: %v", <-errCh)
	}
	if _, err := os.Stat(filepath.Join(dir, "arc.db")); err != nil {
		t.Fatal(err)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown timed out")
	}
}

func TestRunListenError(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)
	t.Setenv("APP_ENV", "test")
	t.Setenv("DATA_DIR", dir)
	t.Setenv("PORT", port)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := make(chan error, 1)
	go func() { first <- run(ctx, discardLog()) }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get("http://127.0.0.1:" + port + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("first server did not listen: %v", <-first)
		}
		time.Sleep(20 * time.Millisecond)
	}

	err := run(context.Background(), discardLog())
	if err == nil {
		t.Fatal("expected listen error")
	}
	cancel()
	select {
	case err = <-first:
		// Shutdown returns a context error. errcheck rejects a bare receive.
		_ = err
	case <-time.After(5 * time.Second):
		t.Fatal("first server did not stop")
	}
}

func discardLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}
