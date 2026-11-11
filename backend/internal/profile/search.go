package profile

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/rhamdeew/dbprc/backend/internal/db/sqlc"
	"github.com/rhamdeew/dbprc/backend/internal/httpx"
)

const (
	minSearchRunes = 4
	searchLimit    = 10
)

type searchHit struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Email     string  `json:"email"`
	AvatarURL *string `json:"avatarUrl"`
}

func (h *handlers) search(w http.ResponseWriter, r *http.Request) {
	if _, ok := httpx.UserFrom(r.Context()); !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in required.")
		return
	}
	q := r.URL.Query().Get("q")
	if utf8.RuneCountInString(q) < minSearchRunes {
		httpx.WriteJSON(w, http.StatusOK, []searchHit{})
		return
	}
	rows, err := sqlc.New(h.db.Reader()).SearchUsers(r.Context(), sqlc.SearchUsersParams{
		Pattern:  likePrefix(q),
		RowLimit: searchLimit,
	})
	if err != nil {
		h.log.Error("search users", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	out := make([]searchHit, 0, len(rows))
	for _, row := range rows {
		out = append(out, searchHit{
			ID:        row.ID,
			Name:      row.Name,
			Email:     row.Email,
			AvatarURL: avatarPointer(row.AvatarPath),
		})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// likePrefix escapes LIKE metacharacters and appends the prefix wildcard.
func likePrefix(q string) string {
	var b strings.Builder
	b.Grow(len(q) + 8)
	for _, r := range q {
		switch r {
		case '\\', '%', '_':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('%')
	return b.String()
}
