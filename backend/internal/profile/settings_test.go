package profile_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rhamdeew/dbprc/backend/internal/db/sqlc"
	"github.com/rhamdeew/dbprc/backend/internal/testutil"
)

func TestProfileFields(t *testing.T) {
	srv, _ := newProfileServer(t)
	db := testutil.Database(t, srv)
	owner := testutil.CreateUser(t, db, testutil.WithName("Ada Lovelace"))
	visitor := testutil.CreateUser(t, db)
	client := testutil.LoginAs(t, srv, owner)
	guest := testutil.LoginAs(t, srv, visitor)
	endpoint := srv.URL + "/api/me/profile"
	profileURL := srv.URL + "/api/users/" + itoa(owner.ID)

	for _, field := range []struct {
		key   string
		limit int
	}{{"nickname", 40}, {"hometown", 100}, {"about", 1000}} {
		t.Run(field.key, func(t *testing.T) {
			atLimit := strings.Repeat("☕", field.limit)
			requireSettingsOK(t, putJSON(t, client, endpoint, map[string]string{field.key: " " + atLimit + " "}))
			assertAPIError(t, putJSON(t, client, endpoint, map[string]string{field.key: atLimit + "☕"}), http.StatusBadRequest, "too_long")
			_, _, raw := getProfile(t, guest, profileURL)
			if raw[field.key] != atLimit {
				t.Fatalf("stored %s = %q", field.key, raw[field.key])
			}
		})
	}
	requireSettingsOK(t, putJSON(t, client, endpoint, map[string]string{
		"nickname": " Countess ", "hometown": " London ", "about": " first\nsecond ", "name": "Ignored", "email": "ignored@example.com",
	}))
	_, profile, _ := getProfile(t, guest, profileURL)
	if profile.Nickname != "Countess" || profile.Hometown != "London" || profile.About != "first\nsecond" ||
		profile.Name != owner.Name || profile.Email != owner.Email || profile.IsMe {
		t.Fatalf("visitor profile = %+v", profile)
	}

	for _, body := range []string{`null`, `{"nickname": false}`, `{} {}`} {
		assertAPIError(t, putJSON(t, client, endpoint, json.RawMessage(body)), http.StatusBadRequest, "invalid")
	}
	requireSettingsOK(t, putJSON(t, client, endpoint, map[string]string{}))
	_, profile, _ = getProfile(t, guest, profileURL)
	if profile.Nickname != "" || profile.Hometown != "" || profile.About != "" {
		t.Fatalf("cleared profile = %+v", profile)
	}
}

func TestStatusAndWallSettings(t *testing.T) {
	srv, _ := newProfileServer(t)
	db := testutil.Database(t, srv)
	owner := testutil.CreateUser(t, db)
	visitor := testutil.CreateUser(t, db)
	client := testutil.LoginAs(t, srv, owner)
	guest := testutil.LoginAs(t, srv, visitor)
	statusURL := srv.URL + "/api/me/status"
	settingsURL := srv.URL + "/api/me/wall-settings"
	profileURL := srv.URL + "/api/users/" + itoa(owner.ID)

	for _, endpoint := range []string{"/api/me/profile", "/api/me/status", "/api/me/wall-settings"} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, srv.URL+endpoint, strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", srv.URL)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		assertAPIError(t, res, http.StatusUnauthorized, "unauthenticated")
		assertAPIError(t, putJSON(t, client, srv.URL+endpoint, map[string]string{"padding": strings.Repeat("x", 32<<10)}), http.StatusBadRequest, "too_large")
	}
	_, profile, _ := getProfile(t, guest, profileURL)
	if profile.Status != "" || !profile.AllowAnonymous {
		t.Fatalf("default profile = %+v", profile)
	}

	atLimit := strings.Repeat("🌱", 140)
	requireSettingsOK(t, putJSON(t, client, statusURL, map[string]string{"status": " " + atLimit + " "}))
	assertAPIError(t, putJSON(t, client, statusURL, map[string]string{"status": atLimit + "🌱"}), http.StatusBadRequest, "too_long")
	for _, body := range []string{`{}`, `{"status":null}`, `{"status":"line\nline"}`, `{"status":"line\u2028line"}`} {
		assertAPIError(t, putJSON(t, client, statusURL, json.RawMessage(body)), http.StatusBadRequest, "invalid")
	}
	requireSettingsOK(t, putJSON(t, guest, statusURL, map[string]any{"status": "visitor", "id": owner.ID}))
	requireSettingsOK(t, putJSON(t, client, settingsURL, map[string]bool{"allowAnonymous": false}))
	assertAPIError(t, putJSON(t, client, settingsURL, json.RawMessage(`{}`)), http.StatusBadRequest, "invalid")
	requireSettingsOK(t, putJSON(t, guest, settingsURL, map[string]any{"allowAnonymous": true, "id": owner.ID}))

	_, profile, _ = getProfile(t, guest, profileURL)
	if profile.Status != atLimit || profile.AllowAnonymous {
		t.Fatalf("updated profile = %+v", profile)
	}

	// Clearing is an update and records its timestamp even after a previous status.
	const past = "2020-01-01T00:00:00Z"
	if _, err := db.Writer().ExecContext(t.Context(), "UPDATE users SET status_updated_at = ? WHERE id = ?", past, owner.ID); err != nil {
		t.Fatal(err)
	}
	requireSettingsOK(t, putJSON(t, client, statusURL, map[string]string{"status": "   "}))
	requireSettingsOK(t, putJSON(t, client, settingsURL, map[string]bool{"allowAnonymous": true}))
	row, err := sqlc.New(db.Reader()).GetUser(t.Context(), owner.ID)
	if err != nil || row.Status != "" || row.AllowAnonymous != 1 || row.StatusUpdatedAt == nil {
		t.Fatalf("cleared status or re-enabled anonymity: %+v, %v", row, err)
	}
	if updated, err := time.Parse(time.RFC3339, *row.StatusUpdatedAt); err != nil || time.Since(updated) > time.Minute {
		t.Fatalf("latest timestamp = %q, %v", *row.StatusUpdatedAt, err)
	}
}

func TestProfileSettingsRateLimit(t *testing.T) {
	srv, _ := newProfileServer(t)
	db := testutil.Database(t, srv)
	client := testutil.LoginAs(t, srv, testutil.CreateUser(t, db))
	for range 20 {
		requireSettingsOK(t, putJSON(t, client, srv.URL+"/api/me/status", map[string]string{"status": ""}))
	}
	assertAPIError(t, putJSON(t, client, srv.URL+"/api/me/profile", json.RawMessage(`{}`)), http.StatusTooManyRequests, "rate_limited")
}

func requireSettingsOK(t *testing.T, res *http.Response) {
	t.Helper()
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("settings returned %d", res.StatusCode)
	}
}
