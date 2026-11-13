package testutil

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rhamdeew/dbprc/backend/internal/config"
	"github.com/rhamdeew/dbprc/backend/internal/db/sqlc"
)

func TestNewServer(t *testing.T) {
	srv := NewServer(t)
	code, body := getJSON(t, srv.URL+"/healthz")
	if code != http.StatusOK || body["status"] != "ok" || body["version"] != "test" {
		t.Fatalf("healthz %d %v", code, body)
	}
	code, body = getJSON(t, srv.URL+"/api/version")
	if code != http.StatusOK || body["version"] != "test" || body["env"] != "test" {
		t.Fatalf("version %d %v", code, body)
	}

	custom := NewServer(t, WithConfig(config.Config{Env: "dev", Version: "abc"}), nil)
	code, body = getJSON(t, custom.URL+"/api/version")
	if code != http.StatusOK || body["version"] != "abc" || body["env"] != "dev" {
		t.Fatalf("custom %d %v", code, body)
	}

	var logs bytes.Buffer
	logged := NewServer(t, WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))
	getJSON(t, logged.URL+"/healthz")
	if logs.Len() != 0 {
		t.Fatalf("healthz was logged: %s", logs.String())
	}
	getJSON(t, logged.URL+"/api/version")
	if !strings.Contains(logs.String(), "/api/version") {
		t.Fatalf("version was not logged: %s", logs.String())
	}

	quiet := NewServer(t, WithLogger(nil))
	if code, _ := getJSON(t, quiet.URL+"/healthz"); code != http.StatusOK {
		t.Fatalf("nil logger status %d", code)
	}
}

func TestLoginAsSetsOriginOnMutatingRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"origin": r.Header.Get("Origin"),
			"cookie": r.Header.Get("Cookie"),
			"method": r.Method,
		})
	}))
	t.Cleanup(srv.Close)

	shared := srv.Client().Transport
	client := LoginAs(t, srv, sqlc.User{ID: 7, Email: "ada@example.com"})
	if srv.Client().Transport != shared {
		t.Fatal("LoginAs changed the server's shared client")
	}

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		req, err := http.NewRequest(method, srv.URL+"/x", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", "https://evil.example")
		res := doJSON(t, client, req)
		if res["origin"] != srv.URL || res["cookie"] != "" || res["method"] != method {
			t.Fatalf("%s headers %v", method, res)
		}
		if req.Header.Get("Origin") != "https://evil.example" {
			t.Fatalf("%s mutated the caller request", method)
		}
	}

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://kept.example")
	res := doJSON(t, client, req)
	if res["origin"] != "https://kept.example" || res["cookie"] != "" {
		t.Fatalf("GET headers %v", res)
	}

	var seen string
	headSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Origin")
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(headSrv.Close)
	headClient := LoginAs(t, headSrv, sqlc.User{ID: 3, Email: "bea@example.com"})
	headRes, err := headClient.Head(headSrv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = headRes.Body.Close()
	if seen != "" {
		t.Fatalf("HEAD origin %q", seen)
	}

	tr := client.Transport.(*sessionTransport)
	tr.cookie = &http.Cookie{Name: "session", Value: "tok"}
	post, err := http.NewRequest(http.MethodPost, srv.URL+"/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	res = doJSON(t, client, post)
	if res["cookie"] != "session=tok" || res["origin"] != srv.URL {
		t.Fatalf("cookie hook %v", res)
	}
	get, err := http.NewRequest(http.MethodGet, srv.URL+"/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	res = doJSON(t, client, get)
	if res["cookie"] != "session=tok" || res["origin"] != "" {
		t.Fatalf("GET cookie %v", res)
	}

	app := NewServer(t)
	authed := LoginAs(t, app, sqlc.User{ID: 1, Email: "ada@example.com"})
	cookie := SessionCookie(authed)
	if cookie == nil || cookie.Name != "arc_sid" || cookie.Value == "" {
		t.Fatalf("app cookie %#v", cookie)
	}
	code, body := getJSONClient(t, authed, app.URL+"/healthz")
	if code != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("authed healthz %d %v", code, body)
	}
	me, err := http.NewRequest(http.MethodGet, app.URL+"/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := doJSON(t, authed, me)
	if got["status"] != "ok" {
		t.Fatalf("healthz with cookie %v", got)
	}
}

func TestSessionTransportNilBase(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"origin":"`+r.Header.Get("Origin")+`"}`)
	}))
	t.Cleanup(srv.Close)
	client := &http.Client{Transport: &sessionTransport{origin: srv.URL}}
	req, err := http.NewRequest(http.MethodPost, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	res := doJSON(t, client, req)
	if res["origin"] != srv.URL {
		t.Fatalf("origin %v", res)
	}
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

func getJSONClient(t *testing.T, client *http.Client, url string) (int, map[string]string) {
	t.Helper()
	resp, err := client.Get(url)
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

func doJSON(t *testing.T, client *http.Client, req *http.Request) map[string]string {
	t.Helper()
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body
}
