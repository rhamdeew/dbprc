package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rhamdeew/dbprc/backend/internal/config"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	cfg := config.Config{Env: "test", Port: 0, Version: "abc123"}
	srv := httptest.NewServer(New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil))))
	t.Cleanup(srv.Close)
	return srv
}

func getJSON(t *testing.T, url string) (int, map[string]string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, body
}

func TestHealthz(t *testing.T) {
	srv := newTestServer(t)
	code, body := getJSON(t, srv.URL+"/healthz")
	if code != http.StatusOK || body["status"] != "ok" || body["version"] != "abc123" {
		t.Fatalf("got %d %v", code, body)
	}
}

func TestVersion(t *testing.T) {
	srv := newTestServer(t)
	code, body := getJSON(t, srv.URL+"/api/version")
	if code != http.StatusOK || body["version"] != "abc123" || body["env"] != "test" {
		t.Fatalf("got %d %v", code, body)
	}
}

func TestUnknownRoute(t *testing.T) {
	srv := newTestServer(t)
	resp, err := http.Get(srv.URL + "/nope")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	srv := newTestServer(t)
	resp, err := http.Post(srv.URL+"/healthz", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("got %d", resp.StatusCode)
	}
}
