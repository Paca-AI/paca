package oidc

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// LoginState is the per-login-attempt payload carried in the short-lived,
// HMAC-signed oidc_state cookie: the CSRF state echoed by the issuer and the
// PKCE code verifier needed for the token exchange.
type LoginState struct {
	State     string `json:"s"`
	Verifier  string `json:"v"`
	ExpiresAt int64  `json:"e"` // unix seconds
}

// EncodeLoginState serialises st and appends an HMAC-SHA256 tag so the value
// can round-trip through an untrusted browser cookie without tampering.
func EncodeLoginState(secret []byte, st LoginState) (string, error) {
	payload, err := json.Marshal(st)
	if err != nil {
		return "", fmt.Errorf("oidc: encode state: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return encoded + "." + signState(secret, encoded), nil
}

// DecodeLoginState verifies the HMAC tag and expiry, returning the payload.
func DecodeLoginState(secret []byte, raw string) (*LoginState, error) {
	encoded, tag, ok := strings.Cut(raw, ".")
	if !ok {
		return nil, fmt.Errorf("oidc: decode state: malformed value")
	}
	if !hmac.Equal([]byte(signState(secret, encoded)), []byte(tag)) {
		return nil, fmt.Errorf("oidc: decode state: signature mismatch")
	}

	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("oidc: decode state: %w", err)
	}
	var st LoginState
	if err := json.Unmarshal(payload, &st); err != nil {
		return nil, fmt.Errorf("oidc: decode state: %w", err)
	}
	if time.Now().Unix() > st.ExpiresAt {
		return nil, fmt.Errorf("oidc: decode state: expired")
	}
	return &st, nil
}

func signState(secret []byte, encoded string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(encoded))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// RandomToken returns a URL-safe random string with n bytes of entropy,
// suitable for OAuth state values and PKCE verifiers (n=32 → 43 chars).
func RandomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("oidc: random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// PKCEChallengeS256 derives the S256 code challenge for a PKCE verifier.
func PKCEChallengeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
