package httpx

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

const rateMessage = "Too many requests. Try again in a minute."

// Limiter allows limit events per key inside a sliding window.
// A denied call is not stored, so the oldest allowed call is what opens the next slot.
type Limiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	now    func() time.Time
	hits   map[string][]time.Time
}

// NewLimiter counts at most limit events per key per window.
func NewLimiter(limit int, window time.Duration) *Limiter {
	return &Limiter{
		limit:  limit,
		window: window,
		now:    time.Now,
		hits:   make(map[string][]time.Time),
	}
}

// Allow reports whether key may proceed. A denial's second result is Retry-After seconds.
func (l *Limiter) Allow(key string) (bool, int) {
	if l == nil || l.limit <= 0 {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if l.now != nil {
		now = l.now()
	}
	cutoff := now.Add(-l.window)
	kept := make([]time.Time, 0, l.limit+1)
	for _, ts := range l.hits[key] {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	if len(kept) >= l.limit {
		l.hits[key] = kept
		wait := kept[0].Add(l.window).Sub(now)
		secs := int((wait + time.Second - 1) / time.Second)
		if secs < 1 {
			secs = 1
		}
		return false, secs
	}
	l.hits[key] = append(kept, now)
	return true, 0
}

// RateLimit enforces lim for the signed-in user.
// A request with no user is passed through (RequireUser already rejected it).
// Over the limit the response is 429 {"code":"rate_limited"} and Retry-After.
func RateLimit(lim *Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := UserFrom(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			allowed, retry := lim.Allow(strconv.FormatInt(user.ID, 10))
			if allowed {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			WriteError(w, http.StatusTooManyRequests, "rate_limited", rateMessage)
		})
	}
}
