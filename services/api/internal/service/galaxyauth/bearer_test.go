package galaxyauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/platform/oidc"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// newFakeJWKSIssuer serves discovery + JWKS for a generated RSA key and
// returns the issuer URL, the signing key, and the kid.
func newFakeJWKSIssuer(t *testing.T) (string, *rsa.PrivateKey, string) {
	t.Helper()

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	const keyID = "galaxy-key-1"

	var issuerURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 issuerURL,
			"authorization_endpoint": issuerURL + "/oauth/authorize",
			"token_endpoint":         issuerURL + "/oauth/token",
			"jwks_uri":               issuerURL + "/.well-known/jwks.json",
		})
	})
	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		pub := &privKey.PublicKey
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{{
				"kty": "RSA",
				"kid": keyID,
				"use": "sig",
				"alg": "RS256",
				"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString([]byte{0x01, 0x00, 0x01}),
			}},
		})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	issuerURL = srv.URL
	return issuerURL, privKey, keyID
}

func signBearer(t *testing.T, issuer string, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	if _, ok := claims["iss"]; !ok {
		claims["iss"] = issuer
	}
	if _, ok := claims["exp"]; !ok {
		claims["exp"] = time.Now().Add(time.Hour).Unix()
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign bearer: %v", err)
	}
	return signed
}

func newBearerFixture(t *testing.T) (*BearerAuthenticator, *memUserStore, func(claims jwt.MapClaims) string) {
	t.Helper()
	issuer, key, kid := newFakeJWKSIssuer(t)
	store := newMemUserStore()
	auth := NewBearerAuthenticator(
		oidc.NewProvider(issuer),
		store,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	return auth, store, func(claims jwt.MapClaims) string {
		return signBearer(t, issuer, key, kid, claims)
	}
}

func TestBearerActAsObjectResolvesEffectivePrincipal(t *testing.T) {
	auth, store, sign := newBearerFixture(t)

	agentUser := &userdom.User{ID: uuid.New(), Username: "platform-agent"}
	humanUser := &userdom.User{ID: uuid.New(), Username: "cao"}
	store.users = append(store.users, agentUser, humanUser)
	store.oidcSub[agentUser.ID] = "platform-sub"
	store.oidcSub[humanUser.ID] = "cao-sub"

	token := sign(jwt.MapClaims{
		"sub":          "platform-sub",
		"act_as":       map[string]any{"sub": "cao-sub"},
		"act_as_agent": map[string]any{"name": "wiki-agent"},
	})

	u, agentName, err := auth.AuthenticateBearer(context.Background(), token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.ID != humanUser.ID {
		t.Fatalf("expected act_as principal %q, resolved %q", humanUser.Username, u.Username)
	}
	if agentName != "wiki-agent" {
		t.Fatalf("expected agent attribution wiki-agent, got %q", agentName)
	}
}

func TestBearerActAsStringResolvesEffectivePrincipal(t *testing.T) {
	auth, store, sign := newBearerFixture(t)

	humanUser := &userdom.User{ID: uuid.New(), Username: "cao"}
	store.users = append(store.users, humanUser)
	store.oidcSub[humanUser.ID] = "cao-sub"

	token := sign(jwt.MapClaims{
		"sub":          "platform-sub",
		"act_as":       "cao-sub",
		"act_as_agent": "briefing-agent",
	})

	u, agentName, err := auth.AuthenticateBearer(context.Background(), token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.ID != humanUser.ID {
		t.Fatal("expected string-form act_as to resolve the effective principal")
	}
	if agentName != "briefing-agent" {
		t.Fatalf("agent attribution = %q", agentName)
	}
}

func TestBearerWithoutActAsFallsBackToTokenSub(t *testing.T) {
	auth, store, sign := newBearerFixture(t)

	user := &userdom.User{ID: uuid.New(), Username: "cao"}
	store.users = append(store.users, user)
	store.oidcSub[user.ID] = "cao-sub"

	u, agentName, err := auth.AuthenticateBearer(context.Background(), sign(jwt.MapClaims{"sub": "cao-sub"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.ID != user.ID {
		t.Fatal("expected token sub to be the principal when act_as is absent")
	}
	if agentName != "" {
		t.Fatalf("expected no agent attribution, got %q", agentName)
	}
}

func TestBearerUnknownPrincipalIsRejectedWithoutAutoCreate(t *testing.T) {
	auth, store, sign := newBearerFixture(t)

	_, _, err := auth.AuthenticateBearer(context.Background(), sign(jwt.MapClaims{"sub": "nobody-sub"}))
	if err == nil {
		t.Fatal("expected error for unknown principal")
	}
	if len(store.users) != 0 {
		t.Fatal("bearer path must never auto-create users")
	}
}

func TestBearerRejectsWrongIssuer(t *testing.T) {
	auth, store, _ := newBearerFixture(t)

	user := &userdom.User{ID: uuid.New(), Username: "cao"}
	store.users = append(store.users, user)
	store.oidcSub[user.ID] = "cao-sub"

	// Token signed by a DIFFERENT issuer/key must be rejected.
	otherIssuer, otherKey, otherKid := newFakeJWKSIssuer(t)
	forged := signBearer(t, otherIssuer, otherKey, otherKid, jwt.MapClaims{"sub": "cao-sub"})

	if _, _, err := auth.AuthenticateBearer(context.Background(), forged); err == nil {
		t.Fatal("expected token from a different issuer to be rejected")
	}
}

func TestBearerRejectsExpiredToken(t *testing.T) {
	auth, store, sign := newBearerFixture(t)

	user := &userdom.User{ID: uuid.New(), Username: "cao"}
	store.users = append(store.users, user)
	store.oidcSub[user.ID] = "cao-sub"

	expired := sign(jwt.MapClaims{"sub": "cao-sub", "exp": time.Now().Add(-time.Minute).Unix()})
	if _, _, err := auth.AuthenticateBearer(context.Background(), expired); err == nil {
		t.Fatal("expected expired token to be rejected")
	}
}
