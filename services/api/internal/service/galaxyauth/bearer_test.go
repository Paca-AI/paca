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

	u, agentName, err := auth.AuthenticateBearer(context.Background(), token, http.MethodGet)
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

	u, agentName, err := auth.AuthenticateBearer(context.Background(), token, http.MethodGet)
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

	u, agentName, err := auth.AuthenticateBearer(context.Background(), sign(jwt.MapClaims{"sub": "cao-sub"}), http.MethodGet)
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

	_, _, err := auth.AuthenticateBearer(context.Background(), sign(jwt.MapClaims{"sub": "nobody-sub"}), http.MethodGet)
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

	if _, _, err := auth.AuthenticateBearer(context.Background(), forged, http.MethodGet); err == nil {
		t.Fatal("expected token from a different issuer to be rejected")
	}
}

func TestBearerRejectsExpiredToken(t *testing.T) {
	auth, store, sign := newBearerFixture(t)

	user := &userdom.User{ID: uuid.New(), Username: "cao"}
	store.users = append(store.users, user)
	store.oidcSub[user.ID] = "cao-sub"

	expired := sign(jwt.MapClaims{"sub": "cao-sub", "exp": time.Now().Add(-time.Minute).Unix()})
	if _, _, err := auth.AuthenticateBearer(context.Background(), expired, http.MethodGet); err == nil {
		t.Fatal("expected expired token to be rejected")
	}
}

// newBearerFixtureWithIssuerClaims mirrors newBearerFixture but constructs
// the provider with an extra-issuer-claims allow-list (ADR-038: identity's
// mint-service-token stamps iss="galaxy-nexus", not the discovery URL).
func newBearerFixtureWithIssuerClaims(
	t *testing.T, extraClaims []string,
) (*BearerAuthenticator, *memUserStore, func(claims jwt.MapClaims) string) {
	t.Helper()
	issuer, key, kid := newFakeJWKSIssuer(t)
	store := newMemUserStore()
	auth := NewBearerAuthenticator(
		oidc.NewProviderWithIssuerClaims(issuer, extraClaims),
		store,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	return auth, store, func(claims jwt.MapClaims) string {
		return signBearer(t, issuer, key, kid, claims)
	}
}

func TestBearerAcceptsConfiguredExtraIssuerClaim(t *testing.T) {
	auth, store, sign := newBearerFixtureWithIssuerClaims(t, []string{"galaxy-nexus"})

	user := &userdom.User{ID: uuid.New(), Username: "cao"}
	store.users = append(store.users, user)
	store.oidcSub[user.ID] = "cao-sub"

	// Signed by OUR issuer's key, but stamped with the logical issuer name —
	// exactly what identity's /internal/mint-service-token produces.
	token := sign(jwt.MapClaims{
		"iss":          "galaxy-nexus",
		"sub":          "paca-service@galaxy.internal.nexus",
		"act_as":       "cao-sub",
		"act_as_agent": "paca-ai",
	})

	u, agentName, err := auth.AuthenticateBearer(context.Background(), token, http.MethodGet)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.ID != user.ID {
		t.Fatal("expected act_as principal to resolve with the extra issuer claim")
	}
	if agentName != "paca-ai" {
		t.Fatalf("agent attribution = %q", agentName)
	}
}

func TestBearerExtraIssuerClaimStillRejectsUnlistedIssuers(t *testing.T) {
	auth, store, sign := newBearerFixtureWithIssuerClaims(t, []string{"galaxy-nexus"})

	user := &userdom.User{ID: uuid.New(), Username: "cao"}
	store.users = append(store.users, user)
	store.oidcSub[user.ID] = "cao-sub"

	if _, _, err := auth.AuthenticateBearer(context.Background(),
		sign(jwt.MapClaims{"iss": "evil-issuer", "sub": "cao-sub"}), http.MethodGet); err == nil {
		t.Fatal("expected unlisted iss claim to be rejected even with a valid signature")
	}
}

func TestBearerExtraIssuerClaimDoesNotRelaxSignatureCheck(t *testing.T) {
	auth, store, _ := newBearerFixtureWithIssuerClaims(t, []string{"galaxy-nexus"})

	user := &userdom.User{ID: uuid.New(), Username: "cao"}
	store.users = append(store.users, user)
	store.oidcSub[user.ID] = "cao-sub"

	// A token from a DIFFERENT key/issuer carrying the allow-listed iss claim
	// must still fail signature verification against OUR issuer's JWKS.
	otherIssuer, otherKey, otherKid := newFakeJWKSIssuer(t)
	forged := signBearer(t, otherIssuer, otherKey, otherKid,
		jwt.MapClaims{"iss": "galaxy-nexus", "sub": "cao-sub"})

	if _, _, err := auth.AuthenticateBearer(context.Background(), forged, http.MethodGet); err == nil {
		t.Fatal("expected forged token (foreign key, allow-listed iss) to be rejected")
	}
}

// ---------------------------------------------------------------------------
// PACA-C1: audience + scope enforcement
// ---------------------------------------------------------------------------

// seedUser registers a user resolvable by the given oidc_sub.
func seedUser(store *memUserStore, sub string) *userdom.User {
	u := &userdom.User{ID: uuid.New(), Username: sub}
	store.users = append(store.users, u)
	store.oidcSub[u.ID] = sub
	return u
}

func TestBearerEnforcesAudience(t *testing.T) {
	auth, store, sign := newBearerFixture(t)
	auth.WithResourceAudience("paca-api")
	seedUser(store, "cao-sub")

	// Wrong audience → rejected even though the signature and principal are ok.
	wrong := sign(jwt.MapClaims{"sub": "cao-sub", "aud": "some-other-service"})
	if _, _, err := auth.AuthenticateBearer(context.Background(), wrong, http.MethodGet); err == nil {
		t.Fatal("expected token minted for a different audience to be rejected")
	}

	// Correct audience → accepted.
	right := sign(jwt.MapClaims{"sub": "cao-sub", "aud": "paca-api"})
	if _, _, err := auth.AuthenticateBearer(context.Background(), right, http.MethodGet); err != nil {
		t.Fatalf("expected token with correct audience to be accepted, got %v", err)
	}
}

func TestBearerScopeRejectsForeignResource(t *testing.T) {
	auth, store, sign := newBearerFixture(t)
	auth.WithResourceScopePrefix("mcp:paca:")
	seedUser(store, "cao-sub")

	// Token scoped to another MCP resource (wiki) but not Paca — confused deputy.
	token := sign(jwt.MapClaims{"sub": "cao-sub", "scope": "mcp:wiki:read mcp:wiki:write"})
	if _, _, err := auth.AuthenticateBearer(context.Background(), token, http.MethodGet); err == nil {
		t.Fatal("expected foreign-resource-scoped token to be rejected")
	}
}

func TestBearerScopeReadOnlyDeniedOnWrite(t *testing.T) {
	auth, store, sign := newBearerFixture(t)
	auth.WithResourceScopePrefix("mcp:paca:")
	seedUser(store, "cao-sub")

	token := sign(jwt.MapClaims{"sub": "cao-sub", "scope": "mcp:paca:read"})

	// Read method is allowed.
	if _, _, err := auth.AuthenticateBearer(context.Background(), token, http.MethodGet); err != nil {
		t.Fatalf("read-only token should be accepted on GET, got %v", err)
	}
	// Write method is denied.
	for _, m := range []string{http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete} {
		if _, _, err := auth.AuthenticateBearer(context.Background(), token, m); err == nil {
			t.Fatalf("read-only token must be denied on %s", m)
		}
	}
}

func TestBearerScopeWriteAllowed(t *testing.T) {
	auth, store, sign := newBearerFixture(t)
	auth.WithResourceScopePrefix("mcp:paca:")
	seedUser(store, "cao-sub")

	token := sign(jwt.MapClaims{"sub": "cao-sub", "scope": "mcp:paca:read mcp:paca:write"})
	if _, _, err := auth.AuthenticateBearer(context.Background(), token, http.MethodPost); err != nil {
		t.Fatalf("write-scoped token should be accepted on POST, got %v", err)
	}
}

func TestBearerScopeNoScopeClaimUnaffected(t *testing.T) {
	auth, store, sign := newBearerFixture(t)
	auth.WithResourceScopePrefix("mcp:paca:")
	seedUser(store, "cao-sub")

	// Service/act_as tokens carry no scope claim; they must still work on writes.
	token := sign(jwt.MapClaims{"sub": "cao-sub"})
	if _, _, err := auth.AuthenticateBearer(context.Background(), token, http.MethodPost); err != nil {
		t.Fatalf("token without a scope claim should be unaffected, got %v", err)
	}
}

func TestBearerScopeGenericScopesIgnored(t *testing.T) {
	auth, store, sign := newBearerFixture(t)
	auth.WithResourceScopePrefix("mcp:paca:")
	seedUser(store, "cao-sub")

	// Generic OIDC scopes are not resource-family scopes and must not trigger
	// the foreign-resource rejection.
	token := sign(jwt.MapClaims{"sub": "cao-sub", "scope": "openid profile email"})
	if _, _, err := auth.AuthenticateBearer(context.Background(), token, http.MethodPost); err != nil {
		t.Fatalf("generic OIDC scopes should not gate resource access, got %v", err)
	}
}

func TestBearerWithoutExtraClaimsKeepsStrictIssuer(t *testing.T) {
	auth, store, sign := newBearerFixture(t)

	user := &userdom.User{ID: uuid.New(), Username: "cao"}
	store.users = append(store.users, user)
	store.oidcSub[user.ID] = "cao-sub"

	// Default (no allow-list) stays strict: a logical issuer name is rejected
	// even though the signature is ours.
	if _, _, err := auth.AuthenticateBearer(context.Background(),
		sign(jwt.MapClaims{"iss": "galaxy-nexus", "sub": "cao-sub"}), http.MethodGet); err == nil {
		t.Fatal("expected non-URL iss to be rejected in strict mode")
	}
}
