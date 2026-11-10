package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/rhamdeew/dbprc/backend/internal/upload"
)

func TestSafeNext(t *testing.T) {
	long := "/" + strings.Repeat("a", maxNextLen)
	tests := []struct {
		in, want string
	}{
		{in: "", want: "/"},
		{in: "/messenger", want: "/messenger"},
		{in: "/u/1?x=1", want: "/u/1?x=1"},
		{in: "/login?error=domain", want: "/login?error=domain"},
		{in: "//evil.com", want: "/"},
		{in: "//evil.com/path", want: "/"},
		{in: "https://evil.com", want: "/"},
		{in: "http://evil.com/x", want: "/"},
		{in: "/\\evil.com", want: "/"},
		{in: "\\evil.com", want: "/"},
		{in: "messenger", want: "/"},
		{in: "/ok\nSet-Cookie:x", want: "/"},
		{in: long, want: "/"},
	}
	for _, tt := range tests {
		if got := safeNext(tt.in); got != tt.want {
			t.Errorf("safeNext(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFlowCookieRejectsTamperAndExpiry(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	sealed, err := sealFlow(key, oidcFlow{
		State: "state", Nonce: "nonce", Verifier: "verifier", Next: "/ok",
		Exp: time.Now().Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := openFlow(key, sealed)
	if err != nil || got.State != "state" || got.Next != "/ok" || got.Verifier != "verifier" {
		t.Fatalf("open %+v %v", got, err)
	}
	if _, err := openFlow([]byte("other-key-other-key-other-key-ok"), sealed); err == nil {
		t.Fatal("other key")
	}
	parts := strings.Split(sealed, ".")
	flipped := parts[0][:len(parts[0])-1] + "A" + "." + parts[1]
	if flipped == sealed {
		flipped = parts[0] + "." + parts[1][:len(parts[1])-1] + "B"
	}
	if _, err := openFlow(key, flipped); err == nil {
		t.Fatal("tampered")
	}
	if _, err := openFlow(key, "not-a-cookie"); err == nil {
		t.Fatal("shape")
	}

	expired, err := sealFlow(key, oidcFlow{
		State: "state", Nonce: "nonce", Verifier: "verifier", Exp: time.Now().Add(-time.Second).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openFlow(key, expired); err == nil {
		t.Fatal("expired")
	}
	empty, err := sealFlow(key, oidcFlow{Exp: time.Now().Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openFlow(key, empty); err == nil {
		t.Fatal("empty fields")
	}
}

func TestIdentityOf(t *testing.T) {
	email, name, code := identityOf(identityClaims{
		Email: " Ada@Example.com ", EmailVerified: true, HD: "Example.com", Name: " Ada ",
	}, "sub", "example.com")
	if code != "" || email != "Ada@Example.com" || name != "Ada" {
		t.Fatalf("got %q %q %q", email, name, code)
	}
	_, _, code = identityOf(identityClaims{Email: "a@example.com", EmailVerified: true, HD: "other.test"}, "sub", "example.com")
	if code != loginErrDomain {
		t.Fatalf("domain %q", code)
	}
	_, _, code = identityOf(identityClaims{Email: "a@example.com", EmailVerified: false, HD: "example.com"}, "sub", "example.com")
	if code != loginErrUnverified {
		t.Fatalf("unverified %q", code)
	}
	_, name, code = identityOf(identityClaims{Email: "ada@example.com", EmailVerified: true, HD: "example.com"}, "sub", "example.com")
	if code != "" || name != "ada" {
		t.Fatalf("name fallback %q %q", name, code)
	}
	if _, _, code = identityOf(identityClaims{EmailVerified: true, HD: "example.com"}, "", "example.com"); code != loginErrProvider {
		t.Fatalf("subject %q", code)
	}
	if _, _, code = identityOf(identityClaims{Email: "not-an-email", EmailVerified: true, HD: "example.com"}, "sub", "example.com"); code != loginErrProvider {
		t.Fatalf("email %q", code)
	}
}

func TestSavePictureRejectsBadURL(t *testing.T) {
	g := &googleLogin{uploads: &upload.Store{Dir: t.TempDir()}, pictures: pictureClient()}
	for _, raw := range []string{"", "ftp://example.com/a.png", "http://user:pass@example.com/a.png", "file:///tmp/a.png", "not a url"} {
		if _, err := g.savePicture(t.Context(), raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	g.uploads = nil
	if _, err := g.savePicture(t.Context(), "https://example.com/a.png"); err == nil {
		t.Fatal("nil store")
	}
}
