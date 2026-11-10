package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/oauth2-proxy/mockoidc"
)

func TestDevIssuer(t *testing.T) {
	if got, want := "http://"+publicHost+mockoidc.IssuerBase, "http://localhost:9999/oidc"; got != want {
		t.Fatalf("issuer %s, want %s", got, want)
	}
}

func TestDefaultUserLogin(t *testing.T) {
	srv := startServer(t, "")
	doc := getJSON(t, srv.origin()+mockoidc.DiscoveryEndpoint)
	if doc["issuer"] != srv.issuer() || doc["issuer"] != srv.oidc.Issuer() {
		t.Fatalf("discovery issuer %v, server %s, oidc %s", doc["issuer"], srv.issuer(), srv.oidc.Issuer())
	}
	if doc["authorization_endpoint"] != srv.origin()+mockoidc.AuthorizationEndpoint {
		t.Fatalf("authorization_endpoint %v", doc["authorization_endpoint"])
	}
	jwks := getJSON(t, srv.origin()+mockoidc.JWKSEndpoint)
	if jwks["keys"] == nil {
		t.Fatalf("jwks %v", jwks)
	}

	claims, access := login(t, srv, "openid")
	assertDefaultClaims(t, srv, claims)
	info := getJSONAuth(t, srv.origin()+mockoidc.UserinfoEndpoint, access)
	if info["email"] != defaultEmail || info["hd"] != defaultHD || info["name"] != defaultName || info["picture"] != defaultPicture {
		t.Fatalf("userinfo %v", info)
	}
	if info["email_verified"] != true || info["sub"] != "sub:"+defaultEmail {
		t.Fatalf("userinfo %v", info)
	}
}

func TestIssuerUsesPublicHost(t *testing.T) {
	addr := freeAddr(t)
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	public := "localhost:" + port
	srv := startServer(t, public, addr)
	if srv.issuer() != "http://"+public+"/oidc" {
		t.Fatalf("issuer %s", srv.issuer())
	}
	doc := getJSON(t, "http://"+addr+mockoidc.DiscoveryEndpoint)
	if doc["issuer"] != "http://"+public+"/oidc" {
		t.Fatalf("discovery %v", doc["issuer"])
	}
	if doc["token_endpoint"] != "http://"+public+mockoidc.TokenEndpoint {
		t.Fatalf("token endpoint %v", doc["token_endpoint"])
	}
}

func TestNextUserQueue(t *testing.T) {
	srv := startServer(t, "")

	ada := postNext(t, srv, `{"email":" ada@example.com ","name":"Ada","hd":"ada.dev","emailVerified":false}`)
	if ada["email"] != "ada@example.com" || ada["name"] != "Ada" || ada["hd"] != "ada.dev" || ada["emailVerified"] != false {
		t.Fatalf("normalized %v", ada)
	}
	bea := postNext(t, srv, `{"email":"bea@example.com"}`)
	if bea["name"] != "bea" || bea["hd"] != defaultHD || bea["emailVerified"] != true {
		t.Fatalf("defaults %v", bea)
	}

	first, _ := login(t, srv, "openid email profile")
	if first["email"] != "ada@example.com" || first["email_verified"] != false || first["hd"] != "ada.dev" || first["name"] != "Ada" {
		t.Fatalf("first login %v", first)
	}
	second, _ := login(t, srv, "openid")
	if second["email"] != "bea@example.com" || second["name"] != "bea" || second["hd"] != defaultHD || second["email_verified"] != true {
		t.Fatalf("second login %v", second)
	}
	third, _ := login(t, srv, "openid")
	assertDefaultClaims(t, srv, third)

	postNext(t, srv, `{"email":"cy@example.com","hd":""}`)
	emptyHD, _ := login(t, srv, "openid")
	if emptyHD["email"] != "cy@example.com" || emptyHD["email_verified"] != true {
		t.Fatalf("empty hd login %v", emptyHD)
	}
	if _, ok := emptyHD["hd"]; ok {
		t.Fatalf("empty hd was issued: %v", emptyHD["hd"])
	}
	if emptyHD["picture"] != defaultPicture {
		t.Fatalf("picture %v", emptyHD["picture"])
	}
}

func TestFailedAuthorizeDoesNotConsumeQueue(t *testing.T) {
	srv := startServer(t, "")
	postNext(t, srv, `{"email":"ada@example.com","name":"Ada"}`)

	resp, err := http.Get(srv.origin() + mockoidc.AuthorizationEndpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusFound {
		t.Fatalf("invalid authorize redirected")
	}

	postNext(t, srv, `{"email":"bea@example.com","name":"Bea"}`)
	claims, _ := login(t, srv, "openid")
	if claims["email"] != "ada@example.com" {
		t.Fatalf("failed authorize consumed the queue: %v", claims["email"])
	}
	next, _ := login(t, srv, "openid")
	if next["email"] != "bea@example.com" {
		t.Fatalf("second queued user %v", next["email"])
	}
}

func TestNextUserRejectsBadBodies(t *testing.T) {
	srv := startServer(t, "")
	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "not json", body: "nope", want: "request body must be json"},
		{name: "missing email", body: `{}`, want: "email is required"},
		{name: "blank email", body: `{"email":"  "}`, want: "email is required"},
		{name: "invalid email", body: `{"email":"not-an-email"}`, want: "email is invalid"},
		{name: "too large", body: `{"email":"a@b.c","name":"` + strings.Repeat("x", 70000) + `"}`, want: "request body is too large"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, srv.origin()+nextUserPath, strings.NewReader(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status %d", resp.StatusCode)
			}
			var body map[string]string
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["code"] != "invalid_body" || body["message"] != tt.want {
				t.Fatalf("body %v", body)
			}
		})
	}

	resp, err := http.Get(srv.origin() + nextUserPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET status %d", resp.StatusCode)
	}
}

func TestRunServesUntilCancel(t *testing.T) {
	addr := freeAddr(t)
	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- run(ctx, addr, "", log) }()

	deadline := time.Now().Add(5 * time.Second)
	var ready bool
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + mockoidc.DiscoveryEndpoint)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				ready = true
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ready {
		cancel()
		t.Fatalf("server did not become ready: %v", <-errCh)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown timed out")
	}
	// run has returned, so nothing else writes the log buffer.
	logged := logs.String()
	if strings.Contains(logged, devClientSecret) {
		t.Fatal("log contains the client secret")
	}
	if !strings.Contains(logged, "http://"+addr+"/oidc") || !strings.Contains(logged, devClientID) {
		t.Fatalf("log %s", logged)
	}
}

func TestRunListenError(t *testing.T) {
	addr := freeAddr(t)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	err = run(context.Background(), addr, addr, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil {
		t.Fatal("expected listen error")
	}
}

func TestClaimsRejectsNilBase(t *testing.T) {
	if _, err := defaultIdentity().Claims(nil, nil); err == nil {
		t.Fatal("expected error")
	}
}

func assertDefaultClaims(t *testing.T, srv *server, claims jwt.MapClaims) {
	t.Helper()
	if claims["email"] != defaultEmail || claims["email_verified"] != true || claims["hd"] != defaultHD {
		t.Fatalf("claims %v", claims)
	}
	if claims["name"] != defaultName || claims["picture"] != defaultPicture {
		t.Fatalf("profile %v", claims)
	}
	if claims["iss"] != srv.issuer() || claims["sub"] != "sub:"+defaultEmail || claims["nonce"] != "n-1" {
		t.Fatalf("registered %v", claims)
	}
}

func startServer(t *testing.T, public string, listen ...string) *server {
	t.Helper()
	addr := freeAddr(t)
	if len(listen) == 1 {
		addr = listen[0]
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := newServer(t.Context(), addr, public, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.shutdown(ctx); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	})
	// Poll the bound address. publicHost may be a name we do not dial from tests.
	waitReady(t, "http://"+addr)
	return srv
}

func waitReady(t *testing.T, origin string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(origin + mockoidc.DiscoveryEndpoint)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("mock oidc did not become ready")
}

func login(t *testing.T, srv *server, scope string) (jwt.MapClaims, string) {
	t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	q := url.Values{}
	q.Set("scope", scope)
	q.Set("state", "s")
	q.Set("nonce", "n-1")
	q.Set("client_id", devClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", "http://127.0.0.1/callback")
	resp, err := client.Get(srv.origin() + mockoidc.AuthorizationEndpoint + "?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("authorize %d %s", resp.StatusCode, body)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	code := loc.Query().Get("code")
	if code == "" {
		t.Fatalf("no code in %s", resp.Header.Get("Location"))
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", devClientID)
	form.Set("client_secret", devClientSecret)
	tokenResp, err := client.PostForm(srv.origin()+mockoidc.TokenEndpoint, form)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tokenResp.Body.Close() }()
	if tokenResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(tokenResp.Body)
		t.Fatalf("token %d %s", tokenResp.StatusCode, body)
	}
	var tok struct {
		IDToken     string `json:"id_token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(tokenResp.Body).Decode(&tok); err != nil {
		t.Fatal(err)
	}
	parsed, err := srv.oidc.Keypair.VerifyJWT(tok.IDToken, srv.oidc.Now)
	if err != nil {
		t.Fatal(err)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatalf("claims type %T", parsed.Claims)
	}
	return claims, tok.AccessToken
}

func postNext(t *testing.T, srv *server, body string) map[string]any {
	t.Helper()
	resp, err := http.Post(srv.origin()+nextUserPath, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("next-user %d %s", resp.StatusCode, raw)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func getJSON(t *testing.T, url string) map[string]any {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s %d", url, resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func getJSONAuth(t *testing.T, url, access string) map[string]any {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+access)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("userinfo %d %s", resp.StatusCode, body)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}
