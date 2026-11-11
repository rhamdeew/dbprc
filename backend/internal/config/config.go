// Package config loads application configuration from environment variables.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Version is set at build time via -ldflags "-X .../config.Version=<sha>".
var Version = "dev"

// DefaultSessionCookie is the session cookie name when SESSION_COOKIE is unset.
const DefaultSessionCookie = "arc_sid"

// DefaultOIDCIssuer is the Google issuer used when OIDC_ISSUER is unset.
const DefaultOIDCIssuer = "https://accounts.google.com"

// Config holds runtime settings. Add new fields here; read them only in Load.
type Config struct {
	Env              string // dev | test | staging | prod
	Port             int
	Version          string
	DataDir          string // SQLite and other local state; file is DataDir/arc.db
	UploadDir        string // image files written by internal/upload
	StaticDir        string // built frontend; dev and test only. Empty in staging and prod
	AppBaseURL       string // public origin the browser uses; CSRF compares Origin to it
	SessionCookie    string // session cookie name
	AdminEmails      []string
	OIDCIssuer       string // OIDC issuer; ID tokens must use this iss
	OIDCClientID     string
	OIDCClientSecret string
	AllowedDomain    string // Google hosted domain (hd) required on the ID token
}

// Load reads configuration from the environment using getenv (os.Getenv in production).
func Load(getenv func(string) string) (Config, error) {
	issuer, err := oidcIssuer(getenv("OIDC_ISSUER"))
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		Env:              valueOr(getenv("APP_ENV"), "dev"),
		Port:             8080,
		Version:          Version,
		DataDir:          valueOr(getenv("DATA_DIR"), "./data"),
		UploadDir:        valueOr(getenv("UPLOAD_DIR"), "./uploads"),
		StaticDir:        strings.TrimSpace(getenv("STATIC_DIR")),
		SessionCookie:    valueOr(strings.TrimSpace(getenv("SESSION_COOKIE")), DefaultSessionCookie),
		AdminEmails:      splitEmails(getenv("ADMIN_EMAILS")),
		OIDCIssuer:       issuer,
		OIDCClientID:     strings.TrimSpace(getenv("OIDC_CLIENT_ID")),
		OIDCClientSecret: strings.TrimSpace(getenv("OIDC_CLIENT_SECRET")),
		AllowedDomain:    strings.ToLower(strings.TrimSpace(getenv("ALLOWED_DOMAIN"))),
	}
	if raw := strings.TrimSpace(getenv("APP_BASE_URL")); raw != "" {
		if _, err := Origin(raw); err != nil {
			return Config{}, fmt.Errorf("invalid APP_BASE_URL %q", raw)
		}
		cfg.AppBaseURL = raw
	}
	if !validCookieName(cfg.SessionCookie) {
		return Config{}, fmt.Errorf("invalid SESSION_COOKIE %q", cfg.SessionCookie)
	}
	if p := getenv("PORT"); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n <= 0 || n > 65535 {
			return Config{}, fmt.Errorf("invalid PORT %q", p)
		}
		cfg.Port = n
	}
	switch cfg.Env {
	case "dev", "test", "staging", "prod":
	default:
		return Config{}, fmt.Errorf("invalid APP_ENV %q", cfg.Env)
	}
	// The web container serves the frontend in staging and prod. STATIC_DIR
	// would skip that and the Basic Auth in front of it.
	if cfg.StaticDir != "" && cfg.Env != "dev" && cfg.Env != "test" {
		return Config{}, fmt.Errorf("STATIC_DIR is only allowed when APP_ENV is dev or test")
	}
	return cfg, nil
}

// FromOS is Load(os.Getenv).
func FromOS() (Config, error) { return Load(os.Getenv) }

// DBPath is the SQLite file $DATA_DIR/arc.db.
func (c Config) DBPath() string {
	return filepath.Join(c.DataDir, "arc.db")
}

// IsAdmin reports whether email is listed in ADMIN_EMAILS.
// Comparison is case-insensitive. Surrounding spaces in the list are ignored.
func (c Config) IsAdmin(email string) bool {
	email = strings.TrimSpace(email)
	if email == "" {
		return false
	}
	for _, admin := range c.AdminEmails {
		if strings.EqualFold(admin, email) {
			return true
		}
	}
	return false
}

// Origin is the scheme://host of an absolute http(s) URL. A path is allowed and ignored.
func Origin(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("invalid URL")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("invalid URL scheme")
	}
	return scheme + "://" + strings.ToLower(u.Host), nil
}

func splitEmails(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func validCookieName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r <= 0x20 || r >= 0x7f || strings.ContainsRune(",;:\\=", r) {
			return false
		}
	}
	return true
}

func valueOr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// oidcIssuer returns the issuer with one trailing slash removed.
// An empty value is DefaultOIDCIssuer. The result must match the provider's iss claim.
func oidcIssuer(raw string) (string, error) {
	issuer := strings.TrimRight(strings.TrimSpace(raw), "/")
	if issuer == "" {
		return DefaultOIDCIssuer, nil
	}
	u, err := url.Parse(issuer)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("invalid OIDC_ISSUER %q", raw)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return "", fmt.Errorf("invalid OIDC_ISSUER %q", raw)
	}
	return issuer, nil
}
