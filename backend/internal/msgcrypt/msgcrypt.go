// Package msgcrypt encrypts chat bodies at rest. It does not provide end-to-end encryption.
package msgcrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"strconv"
	"strings"
)

// Cipher writes with the current key and reads with either the current or old key.
// It is safe for concurrent use.
type Cipher struct {
	current cipher.AEAD
	old     cipher.AEAD
}

// New requires a 32-byte current key and an optional 32-byte old key.
func New(key, old []byte) (*Cipher, error) {
	current, err := gcm(key)
	if err != nil {
		return nil, err
	}
	c := &Cipher{current: current}
	if len(old) != 0 {
		c.old, err = gcm(old)
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

func gcm(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("message key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Encrypt uses a fresh 12-byte nonce and binds the body to its chat id.
func (c *Cipher) Encrypt(chatID int64, body string) string {
	nonce := make([]byte, c.current.NonceSize())
	// crypto/rand.Read terminates the process if the operating system RNG fails.
	_, _ = rand.Read(nonce)
	sealed := c.current.Seal(nonce, nonce, []byte(body), []byte(strconv.FormatInt(chatID, 10)))
	return "v1:" + base64.StdEncoding.EncodeToString(sealed)
}

// Decrypt rejects plaintext, unknown versions, malformed or unauthenticated bodies.
func (c *Cipher) Decrypt(chatID int64, body string) (string, error) {
	if !strings.HasPrefix(body, "v1:") {
		return "", errors.New("unsupported message format")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(body, "v1:"))
	if err != nil || len(raw) < c.current.NonceSize()+c.current.Overhead() {
		return "", errors.New("invalid encrypted message")
	}
	nonce, sealed := raw[:12], raw[12:]
	aad := []byte(strconv.FormatInt(chatID, 10))
	plain, err := c.current.Open(nil, nonce, sealed, aad)
	if err != nil && c.old != nil {
		plain, err = c.old.Open(nil, nonce, sealed, aad)
	}
	if err != nil {
		return "", errors.New("message authentication failed")
	}
	return string(plain), nil
}

// Body keeps a damaged row from failing an entire response. Only ids are logged.
func (c *Cipher) Body(log *slog.Logger, messageID, chatID int64, body string) string {
	plain, err := c.Decrypt(chatID, body)
	if err != nil {
		log.Error("decrypt message", "message_id", messageID, "chat_id", chatID)
		return "[message unavailable]"
	}
	return plain
}
