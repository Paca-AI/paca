package handler_test

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
	"net/url"
	"strings"
	"testing"
	"time"

	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/platform/oidc"
	"github.com/Paca-AI/api/internal/service/galaxyauth"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// fake issuer (discovery + JWKS + token endpoint) backed by httptest
// ---------------------------------------------------------------------------

type fakeIssuer struct {
	srv        *httptest.Server
	privKey    *rsa.PrivateKey
	keyID      string
	clientID   string
	idClaims   jwt.MapClaims // extra claims merged into the issued id_token
	seenCode   string
	seenSecret string
	seenPKCE   string
}

func newFakeIssuer(t *testing.T, clientID string) *fakeIssuer {
	t.Helper()

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	fi := &fakeIssuer{privKey: privKey, keyID: "test-key-1", clientID: clientID}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 fi.srv.URL,
			"authorization_endpoint": fi.srv.URL + "/oauth/authorize",
			"token_endpoint":         fi.srv.URL + "/oauth/token",
			"jwks_uri":               fi.srv.URL + "/.well-known/jwks.json",
		})
	})
	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		pub := &fi.privKey.PublicKey
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{{
				"kty": "RSA",
				"kid": fi.keyID,
				"use": "sig",
				"alg": "RS256",
				"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString([]byte{0x01, 0x00, 0x01}),
			}},
		})
	})
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		fi.seenCode = r.PostFormValue("code")
		fi.seenSecret = r.PostFormValue("client_secret")
		fi.seenPKCE = r.PostFormValue("code_verifier")
		if r.PostFormValue("grant_type") != "authorization_code" {
			http.Error(w, "bad grant", http.StatusBadRequest)
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "issuer-access-token",
			"token_type":   "Bearer",
			"id_token":     fi.signIDToken(t),
		})
	})

	fi.srv = httptest.NewServer(mux)
	t.Cleanup(fi.srv.Close)
	return fi
}

func (fi *fakeIssuer) signIDToken(t *testing.T) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss": fi.srv.URL,
		"aud": fi.clientID,
		"sub": "vortex-sub-1",
		"exp": time.Now().Add(5 * time.Minute).Unix(),
		"iat": time.Now().Unix(),
	}
	for k, v := range fi.idClaims {
		claims[k] = v
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = fi.keyID
	signed, err := token.SignedString(fi.privKey)
	if err != nil {
		t.Fatalf("sign id_token: %v", err)
	}
	return signed
}

// ---------------------------------------------------------------------------
// fakes for user resolution and session issuance
// ---------------------------------------------------------------------------

type fakeResolver struct {
	lastIdentity galaxyauth.Identity
	user         *userdom.User
	err          error
}

func (f *fakeResolver) ResolveOIDCUser(_ context.Context, id galaxyauth.Identity) (*userdom.User, error) {
	f.lastIdentity = id
	if f.err != nil {
		return nil, f.err
	}
	return f.user, nil
}

type fakeSessionIssuer struct {
	lastUser *userdom.User
}

func (f *fakeSessionIssuer) IssueSession(_ context.Context, u *userdom.User, _ bool) (*domainauth.TokenPair, error) {
	f.lastUser = u
	return &domainauth.TokenPair{
		AccessToken:  "session-access-token",
		RefreshToken: "session-refresh-token",
		RefreshTTL:   time.Hour,
	}, nil
}

func newOIDCTestHandler(t *testing.T, fi *fakeIssuer, resolver *fakeResolver, sessions *fakeSessionIssuer) *handler.OIDCHandler {
	t.Helper()
	authHandler := handler.NewAuthHandler(&mockAuthSvc{}, testCookieConfig)
	return handler.NewOIDCHandler(
		oidc.NewProvider(fi.srv.URL),
		handler.OIDCOptions{
			ClientID:     fi.clientID,
			ClientSecret: "test-client-secret",
			RedirectURL:  "http://paca.local/api/v1/auth/oidc/callback",
			Scopes:       "openid profile email",
		},
		resolver,
		sessions,
		authHandler,
		[]byte("test-state-secret"),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
}

// doLogin performs GET /auth/oidc/login and returns the redirect URL plus the
// state cookie the browser would store.
func doLogin(t *testing.T, h *handler.OIDCHandler) (*url.URL, *http.Cookie) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.Login(rec, httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/login", nil))

	if rec.Code != http.StatusFound {
		t.Fatalf("login: expected 302, got %d (%s)", rec.Code, rec.Body.String())
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("login: parse redirect: %v", err)
	}

	var stateCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "oidc_state" {
			stateCookie = c
		}
	}
	if stateCookie == nil {
		t.Fatal("login: no oidc_state cookie set")
	}
	return loc, stateCookie
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func TestOIDCLoginRedirectsToAuthorizationEndpoint(t *testing.T) {
	fi := newFakeIssuer(t, "paca-client")
	h := newOIDCTestHandler(t, fi, &fakeResolver{}, &fakeSessionIssuer{})

	loc, _ := doLogin(t, h)

	if got := strings.TrimSuffix(loc.String(), "?"+loc.RawQuery); got != fi.srv.URL+"/oauth/authorize" {
		t.Fatalf("expected redirect to authorization endpoint, got %s", loc)
	}
	q := loc.Query()
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %q, want code", q.Get("response_type"))
	}
	if q.Get("client_id") != "paca-client" {
		t.Errorf("client_id = %q", q.Get("client_id"))
	}
	if q.Get("state") == "" {
		t.Error("state missing from redirect")
	}
	if q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256" {
		t.Errorf("PKCE params missing: challenge=%q method=%q", q.Get("code_challenge"), q.Get("code_challenge_method"))
	}
}

func TestOIDCCallbackHappyPath(t *testing.T) {
	fi := newFakeIssuer(t, "paca-client")
	fi.idClaims = jwt.MapClaims{
		"email":              "cao@example.com",
		"name":               "Cao Phan",
		"preferred_username": "cao.phan",
	}

	resolver := &fakeResolver{user: &userdom.User{
		ID:       uuid.New(),
		Username: "cao.phan",
		Role:     "USER",
	}}
	sessions := &fakeSessionIssuer{}
	h := newOIDCTestHandler(t, fi, resolver, sessions)

	loc, stateCookie := doLogin(t, h)
	state := loc.Query().Get("state")
	challenge := loc.Query().Get("code_challenge")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?code=test-code&state="+url.QueryEscape(state), nil)
	req.AddCookie(stateCookie)
	rec := httptest.NewRecorder()
	h.Callback(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("callback: expected 302, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/" {
		t.Errorf("callback redirect = %q, want /", got)
	}

	// Token exchange used client_secret_post + the PKCE verifier from the cookie.
	if fi.seenCode != "test-code" {
		t.Errorf("token endpoint saw code %q", fi.seenCode)
	}
	if fi.seenSecret != "test-client-secret" {
		t.Errorf("token endpoint saw client_secret %q", fi.seenSecret)
	}
	if oidc.PKCEChallengeS256(fi.seenPKCE) != challenge {
		t.Error("code_verifier does not match the code_challenge from the login redirect")
	}

	// Claims were extracted from the verified id_token.
	if resolver.lastIdentity.Subject != "vortex-sub-1" {
		t.Errorf("resolved subject = %q", resolver.lastIdentity.Subject)
	}
	if resolver.lastIdentity.Email != "cao@example.com" {
		t.Errorf("resolved email = %q", resolver.lastIdentity.Email)
	}
	if resolver.lastIdentity.PreferredUsername != "cao.phan" {
		t.Errorf("resolved preferred_username = %q", resolver.lastIdentity.PreferredUsername)
	}

	// The same session cookies as password login were issued.
	if sessions.lastUser == nil || sessions.lastUser.ID != resolver.user.ID {
		t.Fatal("session was not issued for the resolved user")
	}
	cookies := rec.Result().Cookies()
	var access, refresh, stateCleared bool
	for _, c := range cookies {
		switch c.Name {
		case "access_token":
			access = c.Value == "session-access-token"
		case "refresh_token":
			refresh = c.Value == "session-refresh-token"
		case "oidc_state":
			stateCleared = c.MaxAge < 0
		}
	}
	if !access || !refresh {
		t.Errorf("session cookies not set correctly (access=%v refresh=%v)", access, refresh)
	}
	if !stateCleared {
		t.Error("oidc_state cookie was not cleared")
	}
}

func TestOIDCCallbackRejectsStateMismatch(t *testing.T) {
	fi := newFakeIssuer(t, "paca-client")
	h := newOIDCTestHandler(t, fi, &fakeResolver{}, &fakeSessionIssuer{})

	_, stateCookie := doLogin(t, h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?code=test-code&state=forged-state", nil)
	req.AddCookie(stateCookie)
	rec := httptest.NewRecorder()
	h.Callback(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on state mismatch, got %d", rec.Code)
	}
	if fi.seenCode != "" {
		t.Error("code must not be exchanged when state does not match")
	}
}

func TestOIDCCallbackRejectsWrongAudience(t *testing.T) {
	fi := newFakeIssuer(t, "paca-client")
	fi.idClaims = jwt.MapClaims{"aud": "some-other-client"}
	h := newOIDCTestHandler(t, fi, &fakeResolver{user: &userdom.User{ID: uuid.New()}}, &fakeSessionIssuer{})

	loc, stateCookie := doLogin(t, h)
	state := loc.Query().Get("state")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?code=test-code&state="+url.QueryEscape(state), nil)
	req.AddCookie(stateCookie)
	rec := httptest.NewRecorder()
	h.Callback(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong audience, got %d", rec.Code)
	}
}
