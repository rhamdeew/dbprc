package httpx

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/rhamdeew/dbprc/backend/internal/config"
)

// CSRF rejects POST, PUT, PATCH, and DELETE unless the request is same-origin.
//
// A request with an Origin header is allowed only when that origin equals the
// origin of baseURL (the APP_BASE_URL value). When Origin is missing, the
// request is allowed only with Sec-Fetch-Site: same-origin. Anything else is
// 403. Other methods are passed through.
func CSRF(baseURL string) func(http.Handler) http.Handler {
	allowed, err := config.Origin(baseURL)
	if err != nil {
		allowed = ""
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !mutating(r.Method) {
				next.ServeHTTP(w, r)
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				if allowed != "" && headerOrigin(origin) == allowed {
					next.ServeHTTP(w, r)
					return
				}
				WriteError(w, http.StatusForbidden, "csrf", "Cross-site request blocked.")
				return
			}
			if r.Header.Get("Sec-Fetch-Site") == "same-origin" {
				next.ServeHTTP(w, r)
				return
			}
			WriteError(w, http.StatusForbidden, "csrf", "Cross-site request blocked.")
		})
	}
}

func mutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// headerOrigin returns the origin of an Origin header. A path, query, or
// userinfo means the header is not an origin.
func headerOrigin(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "null") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	if u.Path != "" && u.Path != "/" {
		return ""
	}
	origin, err := config.Origin(raw)
	if err != nil {
		return ""
	}
	return origin
}
