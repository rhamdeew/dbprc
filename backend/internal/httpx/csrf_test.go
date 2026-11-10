package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCSRF(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := CSRF("https://Arc.Example/app")(next)
	tests := []struct {
		name   string
		method string
		origin string
		fetch  string
		want   int
	}{
		{name: "get ignores origin", method: http.MethodGet, origin: "https://evil.example", want: http.StatusNoContent},
		{name: "head", method: http.MethodHead, want: http.StatusNoContent},
		{name: "options", method: http.MethodOptions, want: http.StatusNoContent},
		{name: "post match", method: http.MethodPost, origin: "https://arc.example", want: http.StatusNoContent},
		{name: "post match case", method: http.MethodPost, origin: "HTTPS://ARC.EXAMPLE", fetch: "cross-site", want: http.StatusNoContent},
		{name: "post trailing slash", method: http.MethodPost, origin: "https://arc.example/", want: http.StatusNoContent},
		{name: "put match", method: http.MethodPut, origin: "https://arc.example", want: http.StatusNoContent},
		{name: "patch match", method: http.MethodPatch, origin: "https://arc.example", want: http.StatusNoContent},
		{name: "delete match", method: http.MethodDelete, origin: "https://arc.example", want: http.StatusNoContent},
		{name: "wrong origin wins over fetch", method: http.MethodPost, origin: "https://evil.example", fetch: "same-origin", want: http.StatusForbidden},
		{name: "wrong scheme", method: http.MethodPost, origin: "http://arc.example", want: http.StatusForbidden},
		{name: "wrong port", method: http.MethodPost, origin: "https://arc.example:8443", want: http.StatusForbidden},
		{name: "origin with path", method: http.MethodPost, origin: "https://arc.example/app", want: http.StatusForbidden},
		{name: "null origin", method: http.MethodPost, origin: "null", fetch: "same-origin", want: http.StatusForbidden},
		{name: "missing origin", method: http.MethodPost, want: http.StatusForbidden},
		{name: "missing origin cross-site", method: http.MethodPost, fetch: "cross-site", want: http.StatusForbidden},
		{name: "missing origin same-site", method: http.MethodPost, fetch: "same-site", want: http.StatusForbidden},
		{name: "missing origin none", method: http.MethodPost, fetch: "none", want: http.StatusForbidden},
		{name: "missing origin same-origin", method: http.MethodPost, fetch: "same-origin", want: http.StatusNoContent},
		{name: "fetch header is case sensitive", method: http.MethodDelete, fetch: "Same-Origin", want: http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "http://app.test/x", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if tt.fetch != "" {
				req.Header.Set("Sec-Fetch-Site", tt.fetch)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			res := rec.Result()
			defer func() { _ = res.Body.Close() }()
			if res.StatusCode != tt.want {
				t.Fatalf("status %d, want %d", res.StatusCode, tt.want)
			}
			if tt.want == http.StatusForbidden {
				if res.Header.Get("Content-Type") != "application/json" {
					t.Fatalf("content-type %q", res.Header.Get("Content-Type"))
				}
				var body Error
				if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if body.Code != "csrf" || body.Message == "" {
					t.Fatalf("body %+v", body)
				}
			}
		})
	}
}

func TestCSRFFailClosedWithoutBaseURL(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	for _, base := range []string{"", "://nope", "ftp://arc.example"} {
		h := CSRF(base)(next)
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.Header.Set("Origin", "https://arc.example")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("base %q status %d", base, rec.Code)
		}
		req = httptest.NewRequest(http.MethodPost, "/", nil)
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("base %q same-origin status %d", base, rec.Code)
		}
	}
}
