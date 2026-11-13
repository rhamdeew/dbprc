package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/rhamdeew/dbprc/backend/internal/config"
	"github.com/rhamdeew/dbprc/backend/internal/db"
	"github.com/rhamdeew/dbprc/backend/internal/db/sqlc"
	"github.com/rhamdeew/dbprc/backend/internal/session"
	"github.com/rhamdeew/dbprc/backend/internal/upload"
)

const (
	oidcCookieName = "arc_oidc"
	oidcCookiePath = "/auth/google"
	oidcCookieTTL  = 10 * time.Minute

	loginErrDomain     = "domain"
	loginErrUnverified = "unverified"
	loginErrState      = "state"
	loginErrProvider   = "provider"

	maxNextLen = 512
)

// googleLogin is the Google OIDC authorization-code flow.
// The state cookie is HMAC-signed with a process-local key, so a restart
// invalidates logins that have not finished. One API process is the deployment.
type googleLogin struct {
	cfg      config.Config
	log      *slog.Logger
	sessions *session.Manager
	db       *db.DB
	uploads  *upload.Store
	client   *http.Client
	pictures *http.Client
	key      []byte

	mu       sync.Mutex
	provider *oidc.Provider
}

type oidcFlow struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	Next     string `json:"r"`
	Exp      int64  `json:"e"`
}

type identityClaims struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	HD            string `json:"hd"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

func newGoogleLogin(cfg config.Config, log *slog.Logger, sessions *session.Manager, database *db.DB, uploads *upload.Store) *googleLogin {
	return &googleLogin{
		cfg:      cfg,
		log:      log,
		sessions: sessions,
		db:       database,
		uploads:  uploads,
		client:   &http.Client{Timeout: 10 * time.Second},
		pictures: pictureClient(),
		key:      newLoginKey(),
	}
}

func newLoginKey() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("auth: oidc cookie key: " + err.Error())
	}
	return key
}

func pictureClient() *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("picture: too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return errors.New("picture: redirect")
			}
			return nil
		},
	}
}

func (g *googleLogin) enabled() bool {
	return strings.TrimSpace(g.cfg.OIDCIssuer) != "" &&
		strings.TrimSpace(g.cfg.OIDCClientID) != "" &&
		g.cfg.OIDCClientSecret != "" &&
		strings.TrimSpace(g.cfg.AllowedDomain) != "" &&
		strings.TrimSpace(g.cfg.AppBaseURL) != ""
}

func (g *googleLogin) start(w http.ResponseWriter, r *http.Request) {
	if !g.enabled() {
		g.fail(w, r, loginErrProvider)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	p, err := g.oidcProvider(ctx)
	if err != nil {
		g.log.Warn("google login", "result", loginErrProvider, "err", err)
		g.fail(w, r, loginErrProvider)
		return
	}
	conf, err := g.oauthConfig(p)
	if err != nil {
		g.log.Warn("google login", "result", loginErrProvider, "err", err)
		g.fail(w, r, loginErrProvider)
		return
	}
	state, err := randomToken()
	if err != nil {
		g.fail(w, r, loginErrProvider)
		return
	}
	nonce, err := randomToken()
	if err != nil {
		g.fail(w, r, loginErrProvider)
		return
	}
	verifier := oauth2.GenerateVerifier()
	sealed, err := sealFlow(g.key, oidcFlow{
		State:    state,
		Nonce:    nonce,
		Verifier: verifier,
		Next:     r.URL.Query().Get("next"),
		Exp:      time.Now().Add(oidcCookieTTL).Unix(),
	})
	if err != nil {
		g.fail(w, r, loginErrProvider)
		return
	}
	http.SetCookie(w, g.flowCookie(sealed, oidcCookieTTL))
	http.Redirect(w, r, conf.AuthCodeURL(state,
		oauth2.S256ChallengeOption(verifier),
		oidc.Nonce(nonce),
		oauth2.SetAuthURLParam("hd", strings.TrimSpace(g.cfg.AllowedDomain)),
	), http.StatusFound)
}

func (g *googleLogin) callback(w http.ResponseWriter, r *http.Request) {
	if !g.enabled() {
		g.fail(w, r, loginErrProvider)
		return
	}
	flow, err := g.readFlow(r)
	g.clearFlowCookie(w)
	if err != nil || subtle.ConstantTimeCompare([]byte(flow.State), []byte(r.URL.Query().Get("state"))) != 1 {
		g.fail(w, r, loginErrState)
		return
	}
	if r.URL.Query().Get("error") != "" || r.URL.Query().Get("code") == "" {
		g.fail(w, r, loginErrProvider)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	p, err := g.oidcProvider(ctx)
	if err != nil {
		g.log.Warn("google login", "result", loginErrProvider, "err", err)
		g.fail(w, r, loginErrProvider)
		return
	}
	conf, err := g.oauthConfig(p)
	if err != nil {
		g.fail(w, r, loginErrProvider)
		return
	}
	ctx = oidc.ClientContext(ctx, g.client)
	tok, err := conf.Exchange(ctx, r.URL.Query().Get("code"), oauth2.VerifierOption(flow.Verifier))
	if err != nil {
		g.log.Warn("google login", "result", loginErrProvider)
		g.fail(w, r, loginErrProvider)
		return
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		g.fail(w, r, loginErrProvider)
		return
	}
	idToken, err := p.Verifier(&oidc.Config{ClientID: g.cfg.OIDCClientID}).Verify(ctx, raw)
	if err != nil {
		g.log.Warn("google login", "result", loginErrProvider)
		g.fail(w, r, loginErrProvider)
		return
	}
	if subtle.ConstantTimeCompare([]byte(flow.Nonce), []byte(idToken.Nonce)) != 1 {
		g.fail(w, r, loginErrState)
		return
	}
	var claims identityClaims
	if err := idToken.Claims(&claims); err != nil {
		g.fail(w, r, loginErrProvider)
		return
	}
	email, name, code := identityOf(claims, idToken.Subject, g.cfg.AllowedDomain)
	if code != "" {
		g.fail(w, r, code)
		return
	}

	user, err := g.upsert(ctx, idToken.Subject, email, name, claims.Picture)
	if err != nil {
		g.log.Error("google login", "err", err)
		g.fail(w, r, loginErrProvider)
		return
	}
	if err := g.sessions.RenewToken(r.Context()); err != nil {
		g.log.Error("google login", "err", err)
		g.fail(w, r, loginErrProvider)
		return
	}
	g.sessions.PutUserID(r.Context(), user.ID)
	http.Redirect(w, r, safeNext(flow.Next), http.StatusFound)
}

// identityOf checks the token claims. The hosted domain comes from the token,
// not from the hd parameter on the authorize URL.
func identityOf(claims identityClaims, subject, allowedDomain string) (email, name, code string) {
	if strings.TrimSpace(subject) == "" {
		return "", "", loginErrProvider
	}
	allowed := strings.TrimSpace(allowedDomain)
	if allowed == "" || !strings.EqualFold(strings.TrimSpace(claims.HD), allowed) {
		return "", "", loginErrDomain
	}
	if !claims.EmailVerified {
		return "", "", loginErrUnverified
	}
	email = strings.TrimSpace(claims.Email)
	at := strings.LastIndex(email, "@")
	if at <= 0 || at == len(email)-1 || strings.ContainsAny(email, " \t\r\n") {
		return "", "", loginErrProvider
	}
	name = strings.TrimSpace(claims.Name)
	if name == "" {
		name = email[:at]
	}
	return email, name, ""
}

func (g *googleLogin) upsert(ctx context.Context, sub, email, name, picture string) (sqlc.User, error) {
	q := sqlc.New(g.db.Writer())
	existing, err := q.GetUserByGoogleSub(ctx, sub)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return sqlc.User{}, err
	}
	first := errors.Is(err, sql.ErrNoRows)

	var avatar *string
	if first && strings.TrimSpace(picture) != "" {
		path, saveErr := g.savePicture(ctx, picture)
		if saveErr != nil {
			g.log.Warn("google avatar", "err", "download failed")
		} else {
			avatar = &path
		}
	}

	user, err := g.writeUser(ctx, first, existing, sub, email, name, avatar)
	if avatar != nil && (err != nil || user.AvatarPath == nil || *user.AvatarPath != *avatar) {
		if delErr := g.uploads.Delete(*avatar); delErr != nil {
			g.log.Warn("google avatar", "err", "delete failed")
		}
	}
	return user, err
}

func (g *googleLogin) writeUser(ctx context.Context, first bool, existing sqlc.User, sub, email, name string, avatar *string) (sqlc.User, error) {
	tx, err := g.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return sqlc.User{}, err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339)
	q := sqlc.New(tx)
	var user sqlc.User
	if first {
		user, err = q.CreateUser(ctx, sqlc.CreateUserParams{
			GoogleSub:   sub,
			Email:       email,
			Name:        name,
			AvatarPath:  avatar,
			Available:   1,
			CreatedAt:   now,
			LastLoginAt: now,
		})
		if isUniqueSub(err) {
			// A concurrent first login inserted this subject. Keep their avatar.
			if rbErr := tx.Rollback(); rbErr != nil {
				return sqlc.User{}, rbErr
			}
			existing, err = sqlc.New(g.db.Writer()).GetUserByGoogleSub(ctx, sub)
			if err != nil {
				return sqlc.User{}, err
			}
			return g.writeUser(ctx, false, existing, sub, email, name, nil)
		}
	} else {
		user, err = q.UpdateUserLogin(ctx, sqlc.UpdateUserLoginParams{
			Email:       email,
			Name:        name,
			LastLoginAt: now,
			ID:          existing.ID,
		})
	}
	if err != nil {
		return sqlc.User{}, err
	}
	if err := tx.Commit(); err != nil {
		return sqlc.User{}, err
	}
	return user, nil
}

func isUniqueSub(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE") && strings.Contains(msg, "google_sub")
}

func (g *googleLogin) savePicture(ctx context.Context, raw string) (string, error) {
	if g.uploads == nil || g.uploads.Dir == "" {
		return "", errors.New("picture: no upload dir")
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return "", errors.New("picture: url")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	// The picture URL is the verified ID token's picture claim, and only http(s) is fetched.
	resp, err := g.pictures.Do(req) //nolint:gosec // G704: scheme is checked above; a failure leaves the user with no avatar.
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", errors.New("picture: status")
	}
	return g.uploads.Save(ctx, io.LimitReader(resp.Body, upload.MaxBytes+1), upload.Avatar)
}

func (g *googleLogin) oidcProvider(ctx context.Context) (*oidc.Provider, error) {
	g.mu.Lock()
	p := g.provider
	g.mu.Unlock()
	if p != nil {
		return p, nil
	}
	ctx = oidc.ClientContext(ctx, g.client)
	p, err := oidc.NewProvider(ctx, strings.TrimSpace(g.cfg.OIDCIssuer))
	if err != nil {
		return nil, err
	}
	g.mu.Lock()
	if g.provider == nil {
		g.provider = p
	} else {
		p = g.provider
	}
	g.mu.Unlock()
	return p, nil
}

func (g *googleLogin) oauthConfig(p *oidc.Provider) (*oauth2.Config, error) {
	redirect, err := callbackURL(g.cfg.AppBaseURL)
	if err != nil {
		return nil, err
	}
	return &oauth2.Config{
		ClientID:     g.cfg.OIDCClientID,
		ClientSecret: g.cfg.OIDCClientSecret,
		RedirectURL:  redirect,
		Endpoint:     p.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
	}, nil
}

func callbackURL(appBase string) (string, error) {
	origin, err := config.Origin(appBase)
	if err != nil {
		return "", err
	}
	return origin + "/auth/google/callback", nil
}

func (g *googleLogin) readFlow(r *http.Request) (oidcFlow, error) {
	cookie, err := r.Cookie(oidcCookieName)
	if err != nil {
		return oidcFlow{}, err
	}
	return openFlow(g.key, cookie.Value)
}

func (g *googleLogin) flowCookie(value string, ttl time.Duration) *http.Cookie {
	return &http.Cookie{ //nolint:gosec // G124: Secure matches the session cookie; HttpOnly and SameSite=Lax are set.
		Name:     oidcCookieName,
		Value:    value,
		Path:     oidcCookiePath,
		MaxAge:   int(ttl.Seconds()),
		Expires:  time.Now().Add(ttl),
		HttpOnly: true,
		Secure:   g.cfg.Env != "dev",
		SameSite: http.SameSiteLaxMode,
	}
}

func (g *googleLogin) clearFlowCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure matches the session cookie; this clears the state cookie.
		Name:     oidcCookieName,
		Value:    "",
		Path:     oidcCookiePath,
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
		HttpOnly: true,
		Secure:   g.cfg.Env != "dev",
		SameSite: http.SameSiteLaxMode,
	})
}

func (g *googleLogin) fail(w http.ResponseWriter, r *http.Request, code string) {
	g.clearFlowCookie(w)
	g.log.Warn("google login", "result", code)
	http.Redirect(w, r, "/login?error="+url.QueryEscape(code), http.StatusFound)
}

func sealFlow(key []byte, flow oidcFlow) (string, error) {
	payload, err := json.Marshal(flow)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func openFlow(key []byte, raw string) (oidcFlow, error) {
	payloadB64, sigB64, ok := strings.Cut(raw, ".")
	if !ok || payloadB64 == "" || sigB64 == "" || strings.Contains(sigB64, ".") {
		return oidcFlow{}, errors.New("state cookie")
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return oidcFlow{}, errors.New("state cookie")
	}
	sig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return oidcFlow{}, errors.New("state cookie")
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return oidcFlow{}, errors.New("state cookie")
	}
	var flow oidcFlow
	if err := json.Unmarshal(payload, &flow); err != nil {
		return oidcFlow{}, errors.New("state cookie")
	}
	if flow.State == "" || flow.Nonce == "" || flow.Verifier == "" || time.Now().Unix() > flow.Exp {
		return oidcFlow{}, errors.New("state cookie")
	}
	return flow, nil
}

func randomToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// safeNext allows a same-site path. Anything else, including protocol-relative
// URLs, becomes the home page.
func safeNext(next string) string {
	if next == "" || len(next) > maxNextLen || strings.ContainsAny(next, "\r\n\x00\\") {
		return "/"
	}
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return "/"
	}
	u, err := url.Parse(next)
	if err != nil || u.IsAbs() || u.Host != "" || !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") {
		return "/"
	}
	return next
}
