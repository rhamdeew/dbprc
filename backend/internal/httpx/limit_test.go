package httpx

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rhamdeew/dbprc/backend/internal/db/sqlc"
)

func TestLimiterWindow(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	lim := NewLimiter(2, time.Minute)
	lim.now = func() time.Time { return now }

	if ok, _ := lim.Allow("a"); !ok {
		t.Fatal("first")
	}
	if ok, _ := lim.Allow("a"); !ok {
		t.Fatal("second")
	}
	ok, retry := lim.Allow("a")
	if ok || retry != 60 {
		t.Fatalf("deny ok=%v retry=%d", ok, retry)
	}
	if ok, _ := lim.Allow("b"); !ok {
		t.Fatal("other key")
	}
	now = now.Add(time.Minute)
	if ok, _ := lim.Allow("a"); !ok {
		t.Fatal("after window")
	}
	if ok, retry := lim.Allow("a"); !ok || retry != 0 {
		t.Fatalf("second slot after window ok=%v retry=%d", ok, retry)
	}
	if ok, _ := lim.Allow("a"); ok {
		t.Fatal("third in the new window")
	}
}

func TestLimiterConcurrent(t *testing.T) {
	lim := NewLimiter(50, time.Minute)
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	wg.Add(80)
	for range 80 {
		go func() {
			defer wg.Done()
			ok, _ := lim.Allow("same")
			if ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if allowed != 50 {
		t.Fatalf("allowed %d", allowed)
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	lim := NewLimiter(1, time.Minute)
	var calls int
	h := RateLimit(lim)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	}))

	anon := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, anon)
	if rec.Code != http.StatusNoContent || calls != 1 {
		t.Fatalf("anon %d calls %d", rec.Code, calls)
	}

	userReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		return req.WithContext(WithUser(req.Context(), sqlc.User{ID: 7}))
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, userReq())
	if rec.Code != http.StatusNoContent || calls != 2 {
		t.Fatalf("first user %d calls %d", rec.Code, calls)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, userReq())
	if rec.Code != http.StatusTooManyRequests || calls != 2 {
		t.Fatalf("second user %d calls %d body %s", rec.Code, calls, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") == "" || !strings.Contains(rec.Body.String(), `"code":"rate_limited"`) {
		t.Fatalf("headers %v body %s", rec.Header(), rec.Body.String())
	}

	other := httptest.NewRequest(http.MethodPost, "/", nil)
	other = other.WithContext(WithUser(other.Context(), sqlc.User{ID: 8}))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, other)
	if rec.Code != http.StatusNoContent || calls != 3 {
		t.Fatalf("other user %d calls %d", rec.Code, calls)
	}
}

func TestRetryAfterHeaderIsNumeric(t *testing.T) {
	lim := NewLimiter(1, 90*time.Second)
	h := RateLimit(lim)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req = req.WithContext(WithUser(req.Context(), sqlc.User{ID: 1}))
	h.ServeHTTP(httptest.NewRecorder(), req)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	secs, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	if err != nil || secs < 1 || secs > 90 {
		t.Fatalf("Retry-After %q err %v", rec.Header().Get("Retry-After"), err)
	}
}
