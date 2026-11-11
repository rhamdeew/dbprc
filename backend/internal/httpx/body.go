package httpx

import "net/http"

const (
	// JSONBodyLimit is the most bytes a JSON route accepts.
	JSONBodyLimit int64 = 32 << 10
	// SmallBodyLimit is the most bytes a route that does not read a body accepts.
	SmallBodyLimit int64 = 1 << 10
)

const tooLargeMessage = "Request body is too large."

// LimitBody rejects a body larger than n with 400 {"code":"too_large"}.
//
// A known Content-Length above n is rejected before next runs. Otherwise the
// body is wrapped so a later read past n returns *http.MaxBytesError. The
// wrapper does not write the response; the handler maps that error to JSON.
func LimitBody(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body == nil || r.Body == http.NoBody || r.ContentLength == 0 {
				next.ServeHTTP(w, r)
				return
			}
			if r.ContentLength > n {
				WriteError(w, http.StatusBadRequest, "too_large", tooLargeMessage)
				return
			}
			r.Body = http.MaxBytesReader(nil, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}
