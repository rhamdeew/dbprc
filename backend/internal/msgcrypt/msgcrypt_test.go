package msgcrypt

import (
	"bytes"
	"encoding/base64"
	"log/slog"
	"strings"
	"testing"
)

func newCipher(t *testing.T, key, old []byte) *Cipher {
	t.Helper()
	c, err := New(key, old)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestEncryption(t *testing.T) {
	c := newCipher(t, make([]byte, 32), nil)
	for _, text := range []string{"", "hello", "Matched again ☕", strings.Repeat("猫", 4000)} {
		encrypted := c.Encrypt(42, text)
		if !strings.HasPrefix(encrypted, "v1:") || encrypted == c.Encrypt(42, text) {
			t.Fatal("missing version or reused nonce")
		}
		got, err := c.Decrypt(42, encrypted)
		if err != nil || got != text {
			t.Fatalf("round trip: %v", err)
		}
		raw, err := base64.StdEncoding.DecodeString(encrypted[3:])
		if err != nil || len(raw) != 12+len(text)+16 {
			t.Fatal("unexpected encoding")
		}
	}
}

func TestRejectsInvalidCiphertext(t *testing.T) {
	c := newCipher(t, make([]byte, 32), nil)
	encrypted := c.Encrypt(42, "private")
	raw, err := base64.StdEncoding.DecodeString(encrypted[3:])
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 1
	for _, body := range []string{"private", "v2:" + encrypted[3:], "v1:!", "v1:AA==", "v1:" + base64.StdEncoding.EncodeToString(raw)} {
		if _, err := c.Decrypt(42, body); err == nil {
			t.Fatal("accepted invalid ciphertext")
		}
	}
	if _, err := c.Decrypt(43, encrypted); err == nil {
		t.Fatal("accepted different chat")
	}
	wrong := newCipher(t, bytes.Repeat([]byte{1}, 32), nil)
	if _, err := wrong.Decrypt(42, encrypted); err == nil {
		t.Fatal("accepted different key")
	}
}

func TestRotation(t *testing.T) {
	oldKey, newKey := make([]byte, 32), bytes.Repeat([]byte{1}, 32)
	old := newCipher(t, oldKey, nil)
	rotated := newCipher(t, newKey, oldKey)
	for _, body := range []string{old.Encrypt(7, "old"), rotated.Encrypt(7, "new")} {
		if _, err := rotated.Decrypt(7, body); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := old.Decrypt(7, rotated.Encrypt(7, "new")); err == nil {
		t.Fatal("writes used old key")
	}
	if _, err := rotated.Decrypt(7, "v1:"+base64.StdEncoding.EncodeToString(make([]byte, 40))); err == nil {
		t.Fatal("accepted invalid with fallback key")
	}
}

func TestKeysAndUnavailable(t *testing.T) {
	for _, size := range []int{0, 16, 24, 31, 33} {
		if _, err := New(make([]byte, size), nil); err == nil {
			t.Fatal("accepted invalid key length")
		}
	}
	if _, err := New(make([]byte, 32), make([]byte, 16)); err == nil {
		t.Fatal("accepted invalid old key")
	}
	c := newCipher(t, make([]byte, 32), nil)
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	if c.Body(log, 99, 42, "private") != "[message unavailable]" {
		t.Fatal("missing placeholder")
	}
	if !strings.Contains(logs.String(), "message_id=99") || strings.Contains(logs.String(), "private") {
		t.Fatal("unsafe log")
	}
	if c.Body(log, 100, 42, c.Encrypt(42, "ok")) != "ok" {
		t.Fatal("valid body")
	}
}
