package main

import (
	"encoding/json"
	"errors"

	"github.com/golang-jwt/jwt/v5"
	"github.com/oauth2-proxy/mockoidc"
)

const (
	defaultEmail   = "dev@example.com"
	defaultName    = "Dev"
	defaultHD      = "example.com"
	defaultPicture = "https://example.com/avatar.png"
)

// identity is the Google account the mock signs in.
// ID tokens always include email, email_verified, hd, name, and picture,
// including when the client asked only for the openid scope.
type identity struct {
	Subject       string
	Email         string
	Name          string
	HD            string
	EmailVerified bool
	Picture       string
}

func defaultIdentity() *identity {
	return newIdentity(defaultEmail, defaultName, defaultHD, true)
}

func newIdentity(email, name, hd string, verified bool) *identity {
	return &identity{
		Subject:       "sub:" + email,
		Email:         email,
		Name:          name,
		HD:            hd,
		EmailVerified: verified,
		Picture:       defaultPicture,
	}
}

func (u *identity) ID() string { return u.Subject }

func (u *identity) Userinfo([]string) ([]byte, error) {
	payload := struct {
		Subject       string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		HD            string `json:"hd,omitempty"`
		Name          string `json:"name,omitempty"`
		Picture       string `json:"picture,omitempty"`
	}{
		Subject:       u.ID(),
		Email:         u.Email,
		EmailVerified: u.EmailVerified,
		HD:            u.HD,
		Name:          u.Name,
		Picture:       u.Picture,
	}
	return json.Marshal(payload)
}

type idClaims struct {
	*mockoidc.IDTokenClaims
	Email         string `json:"email,omitempty"`
	EmailVerified bool   `json:"email_verified"`
	HD            string `json:"hd,omitempty"`
	Name          string `json:"name,omitempty"`
	Picture       string `json:"picture,omitempty"`
}

func (u *identity) Claims(_ []string, base *mockoidc.IDTokenClaims) (jwt.Claims, error) {
	if base == nil {
		return nil, errors.New("missing base claims")
	}
	return &idClaims{
		IDTokenClaims: base,
		Email:         u.Email,
		EmailVerified: u.EmailVerified,
		HD:            u.HD,
		Name:          u.Name,
		Picture:       u.Picture,
	}, nil
}
