// Package session configures the server-side session manager.
//
// Data lives in the sessions table through github.com/alexedwards/scs/sqlite3store.
// The store speaks database/sql, so it uses the existing modernc.org/sqlite pool
// and does not pull in mattn/go-sqlite3.
package session

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/alexedwards/scs/sqlite3store"
	"github.com/alexedwards/scs/v2"

	"github.com/rhamdeew/dbprc/backend/internal/config"
)

const (
	// Lifetime is the absolute session cap, set when the session is created.
	Lifetime = 30 * 24 * time.Hour
	// IdleTimeout is how long a session may sit unused before it expires.
	// Each request renews the idle deadline, but never past Lifetime.
	IdleTimeout = 7 * 24 * time.Hour

	cleanupInterval = 5 * time.Minute
	userIDKey       = "uid"
)

// Settings selects cookie flags. An empty CookieName becomes config.DefaultSessionCookie.
type Settings struct {
	CookieName string
	Secure     bool
}

// Manager is the process-wide session manager.
type Manager struct {
	*scs.SessionManager
	store *sqlite3store.SQLite3Store
	stop  sync.Once
}

// New stores sessions in pool, which must be the write pool (the sessions table
// is already migrated). Close stops the store's cleanup goroutine.
func New(pool *sql.DB, settings Settings, log *slog.Logger) *Manager {
	if pool == nil {
		panic("session: nil db")
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	name := settings.CookieName
	if name == "" {
		name = config.DefaultSessionCookie
	}
	store := sqlite3store.NewWithConfig(pool, sqlite3store.Config{
		CleanUpInterval: cleanupInterval,
		TableName:       "sessions",
	})
	sm := scs.New()
	sm.Store = store
	sm.Lifetime = Lifetime
	sm.IdleTimeout = IdleTimeout
	sm.Cookie.Name = name
	sm.Cookie.HttpOnly = true
	sm.Cookie.Secure = settings.Secure
	sm.Cookie.SameSite = http.SameSiteLaxMode
	sm.Cookie.Path = "/"
	sm.Cookie.Persist = true
	sm.ErrorFunc = func(w http.ResponseWriter, _ *http.Request, err error) {
		log.Error("session", "err", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
	return &Manager{SessionManager: sm, store: store}
}

// Close stops the background cleanup goroutine. It is safe to call more than once.
// Call it before closing the database.
func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.stop.Do(func() {
		if m.store != nil {
			m.store.StopCleanup()
		}
	})
}

// PutUserID stores the signed-in user id. The request context must already
// carry a session loaded by LoadAndSave or Load.
func (m *Manager) PutUserID(ctx context.Context, id int64) {
	m.Put(ctx, userIDKey, id)
}

// UserID returns the signed-in user id. ok is false when the session has none.
func (m *Manager) UserID(ctx context.Context) (int64, bool) {
	id := m.GetInt64(ctx, userIDKey)
	if id <= 0 {
		return 0, false
	}
	return id, true
}

// Mint creates a session for userID and returns the cookie a client should send.
// Tests use this to sign in without a browser. The cookie name and flags match
// the manager, so the LoadAndSave middleware accepts it.
func (m *Manager) Mint(ctx context.Context, userID int64) (*http.Cookie, error) {
	ctx, err := m.Load(ctx, "")
	if err != nil {
		return nil, err
	}
	m.PutUserID(ctx, userID)
	token, expiry, err := m.Commit(ctx)
	if err != nil {
		return nil, err
	}
	// Secure follows APP_ENV (false only for dev). HttpOnly and SameSite are fixed.
	return &http.Cookie{ //nolint:gosec // G124: Secure is conditional; HttpOnly and SameSite=Lax are set.
		Name:     m.Cookie.Name,
		Value:    token,
		Path:     m.Cookie.Path,
		HttpOnly: true,
		Secure:   m.Cookie.Secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  expiry,
	}, nil
}

// SessionUserID reads the uid stored in an scs session blob.
// ok is false when the blob is empty, not a session, or has no positive uid.
// The sessions table has no user column, so a delete has to find rows this way.
func SessionUserID(data []byte) (int64, bool) {
	if len(data) == 0 {
		return 0, false
	}
	_, values, err := (scs.GobCodec{}).Decode(data)
	if err != nil || values == nil {
		return 0, false
	}
	id, ok := values[userIDKey].(int64)
	if !ok || id <= 0 {
		return 0, false
	}
	return id, true
}
