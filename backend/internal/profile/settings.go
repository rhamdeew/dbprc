package profile

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rhamdeew/dbprc/backend/internal/db/sqlc"
	"github.com/rhamdeew/dbprc/backend/internal/httpx"
)

type profileFieldsJSON struct {
	Nickname string `json:"nickname"`
	Hometown string `json:"hometown"`
	About    string `json:"about"`
}

type statusJSON struct {
	Status string `json:"status"`
}

type wallSettingsJSON struct {
	AllowAnonymous bool `json:"allowAnonymous"`
}

func (h *handlers) putProfileFields(w http.ResponseWriter, r *http.Request) {
	user, ok := httpx.UserFrom(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in required.")
		return
	}
	var body *profileFieldsJSON
	if !decodeSettings(w, r, &body) {
		return
	}
	if body == nil {
		writeInvalidSettings(w)
		return
	}

	fields := []struct {
		value *string
		label string
		limit int
	}{
		{&body.Nickname, "Nickname", 40},
		{&body.Hometown, "Hometown", 100},
		{&body.About, "About you", 1000},
	}
	for _, field := range fields {
		*field.value = strings.TrimSpace(*field.value)
		if !checkFieldLength(w, *field.value, field.label, field.limit) {
			return
		}
	}

	saved, err := sqlc.New(h.db.Writer()).UpdateProfileFields(r.Context(), sqlc.UpdateProfileFieldsParams{
		ID: user.ID, Nickname: body.Nickname, Hometown: body.Hometown, About: body.About,
	})
	h.writeSettings(w, user.ID, profileFieldsJSON(saved), err)
}

func (h *handlers) putStatus(w http.ResponseWriter, r *http.Request) {
	user, ok := httpx.UserFrom(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in required.")
		return
	}
	var body struct {
		Status *string `json:"status"`
	}
	if !decodeSettings(w, r, &body) {
		return
	}
	if body.Status == nil || strings.ContainsAny(*body.Status, "\r\n\v\f\u0085\u2028\u2029") {
		httpx.WriteError(w, http.StatusBadRequest, "invalid", "Enter a single-line status.")
		return
	}
	status := strings.TrimSpace(*body.Status)
	if !checkFieldLength(w, status, "Status", 140) {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	saved, err := sqlc.New(h.db.Writer()).UpdateProfileStatus(r.Context(), sqlc.UpdateProfileStatusParams{
		ID: user.ID, Status: status, StatusUpdatedAt: &now,
	})
	h.writeSettings(w, user.ID, statusJSON{Status: saved}, err)
}

func (h *handlers) putWallSettings(w http.ResponseWriter, r *http.Request) {
	user, ok := httpx.UserFrom(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in required.")
		return
	}
	var body struct {
		AllowAnonymous *bool `json:"allowAnonymous"`
	}
	if !decodeSettings(w, r, &body) {
		return
	}
	if body.AllowAnonymous == nil {
		writeInvalidSettings(w)
		return
	}
	var allow int64
	if *body.AllowAnonymous {
		allow = 1
	}
	saved, err := sqlc.New(h.db.Writer()).UpdateWallAnonymity(r.Context(), sqlc.UpdateWallAnonymityParams{
		ID: user.ID, AllowAnonymous: allow,
	})
	h.writeSettings(w, user.ID, wallSettingsJSON{AllowAnonymous: saved != 0}, err)
}

func decodeSettings(w http.ResponseWriter, r *http.Request, body any) bool {
	return decodeJSON(w, r, body, "Check your settings and try again.")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, body any, invalidMessage string) bool {
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(body)
	if err == nil {
		var extra json.RawMessage
		err = decoder.Decode(&extra)
		if errors.Is(err, io.EOF) {
			return true
		}
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		httpx.WriteError(w, http.StatusBadRequest, "too_large", "Request body is too large.")
	} else {
		httpx.WriteError(w, http.StatusBadRequest, "invalid", invalidMessage)
	}
	return false
}

func writeInvalidSettings(w http.ResponseWriter) {
	httpx.WriteError(w, http.StatusBadRequest, "invalid", "Check your settings and try again.")
}

func checkFieldLength(w http.ResponseWriter, value, label string, limit int) bool {
	if utf8.RuneCountInString(value) <= limit {
		return true
	}
	httpx.WriteError(w, http.StatusBadRequest, "too_long", fmt.Sprintf("%s must be %d characters or fewer.", label, limit))
	return false
}

func (h *handlers) writeSettings(w http.ResponseWriter, userID int64, body any, err error) {
	switch {
	case err == nil:
		httpx.WriteJSON(w, http.StatusOK, body)
	case errors.Is(err, sql.ErrNoRows):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "Profile not found.")
	default:
		h.log.Error("save profile settings", "err", err, "user_id", userID)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
	}
}
