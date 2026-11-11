package httpx

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLimitBody(t *testing.T) {
	var reads int
	h := LimitBody(4)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads++
		b, err := io.ReadAll(r.Body)
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				WriteError(w, http.StatusBadRequest, "too_large", tooLargeMessage)
				return
			}
			t.Errorf("read: %v", err)
			WriteError(w, http.StatusBadRequest, "invalid", "bad")
			return
		}
		if r.ContentLength > 4 {
			t.Errorf("handler saw %d bytes past the cap", r.ContentLength)
		}
		if len(b) > 4 {
			t.Errorf("read %d bytes", len(b))
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	over := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("abcde"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, over)
	if rec.Code != http.StatusBadRequest || reads != 0 {
		t.Fatalf("oversize status %d reads %d body %s", rec.Code, reads, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"too_large"`) {
		t.Fatalf("body %s", rec.Body.String())
	}

	ok := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("abcd"))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, ok)
	if rec.Code != http.StatusNoContent || reads != 1 {
		t.Fatalf("exact status %d reads %d", rec.Code, reads)
	}

	empty := httptest.NewRequest(http.MethodPost, "/", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, empty)
	if rec.Code != http.StatusNoContent || reads != 2 {
		t.Fatalf("empty status %d reads %d", rec.Code, reads)
	}

	chunked := httptest.NewRequest(http.MethodPost, "/", nil)
	chunked.Body = io.NopCloser(strings.NewReader("abcdef"))
	chunked.ContentLength = -1
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, chunked)
	if rec.Code != http.StatusBadRequest || reads != 3 {
		t.Fatalf("chunked status %d reads %d body %s", rec.Code, reads, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"too_large"`) {
		t.Fatalf("chunked body %s", rec.Body.String())
	}
}
