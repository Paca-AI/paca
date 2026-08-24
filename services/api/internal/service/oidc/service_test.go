package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"

	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	extiddom "github.com/Paca-AI/api/internal/domain/externalidentity"
	globalroledom "github.com/Paca-AI/api/internal/domain/globalrole"
	userdom "github.com/Paca-AI/api/internal/domain/user"
)

// ---------------------------------------------------------------------------
// Mock IdP: a minimal OIDC provider backed by an httptest server.
// ---------------------------------------------------------------------------

type mockIdP struct {
	server *httptest.Server
	key    *rsa.PrivateKey
	kid    string

	mu          sync.Mutex
	lastReqForm url.Values // form captured on the most recent token request

	// tokenClaims overrides the claims embedded in issued ID tokens.
	tokenClaims map[string]any
	// userInfoClaims overrides the claims served by the userinfo endpoint.
	userInfoClaims map[string]any
	// signKey, when non-nil, replaces the signing key (bad-signature tests).
	altKey *rsa.PrivateKey
	// breakTokenEndpoint makes the token endpoint return a 500.
	breakTokenEndpoint bool
	// breakUserInfo makes the userinfo endpoint return a 500.
	breakUserInfo bool
	// lastBearerToken records the Authorization header of the last userinfo
	// request (the access token from the code exchange).
	lastBearerToken string
}

func newMockIdP(t *testing.T) *mockIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	idp := &mockIdP{key: key, kid: "test-key-1"}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		base := idp.server.URL
		writeJSON(w, map[string]any{
			"issuer":                 base,
			"authorization_endpoint": base + "/authorize",
			"token_endpoint":         base + "/token",
			"userinfo_endpoint":      base + "/userinfo",
			"jwks_uri":               base + "/keys",
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		pub := jose.JSONWebKey{Key: &idp.key.PublicKey, KeyID: idp.kid, Algorithm: "RS256", Use: "sig"}
		writeJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{pub}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		idp.mu.Lock()
		idp.lastReqForm = r.PostForm
		idp.mu.Unlock()
		if idp.breakTokenEndpoint {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		idToken, err := idp.signIDToken()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{
			"access_token": "at-" + uuid.NewString(),
			"token_type":   "Bearer",
			"expires_in":   3600,
			"id_token":     idToken,
		})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		idp.mu.Lock()
		bearer := r.Header.Get("Authorization")
		idp.lastBearerToken = bearer
		claims := idp.userInfoClaims
		breakUserInfo := idp.breakUserInfo
		idp.mu.Unlock()
		if !strings.HasPrefix(bearer, "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if breakUserInfo {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		resp := map[string]any{"sub": "user-1234"}
		for k, v := range claims {
			resp[k] = v
		}
		writeJSON(w, resp)
	})

	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// signIDToken produces a signed JWT whose claims are the mock's current
// tokenClaims overlaid on the mandatory OIDC claims.
func (m *mockIdP) signIDToken() (string, error) {
	key := m.key
	if m.altKey != nil {
		key = m.altKey
	}
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		&jose.SignerOptions{ExtraHeaders: map[jose.HeaderKey]any{"kid": m.kid}},
	)
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	tokenClaims := m.tokenClaims
	m.mu.Unlock()
	now := time.Now()
	claims := map[string]any{
		"iss": m.server.URL,
		"aud": "paca-client",
		"sub": "user-1234",
		"iat": now.Unix(),
		"exp": now.Add(5 * time.Minute).Unix(),
	}
	for k, v := range tokenClaims {
		claims[k] = v
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	obj, err := signer.Sign(payload)
	if err != nil {
		return "", err
	}
	return obj.CompactSerialize()
}

// ---------------------------------------------------------------------------
// Stubs
// ---------------------------------------------------------------------------

// memTxStore is an in-memory LoginTxStore with TTL and single-use semantics.
type memTxStore struct {
	mu  sync.Mutex
	txs map[string]txEntry
	now func() time.Time
}
type txEntry struct {
	payload []byte
	expires time.Time
}

func newMemTxStore() *memTxStore {
	return &memTxStore{txs: map[string]txEntry{}, now: time.Now}
}

func (s *memTxStore) Save(_ context.Context, state string, payload []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.txs[state] = txEntry{payload: payload, expires: s.now().Add(ttl)}
	return nil
}

func (s *memTxStore) Consume(_ context.Context, state string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.txs[state]
	if !ok || s.now().After(entry.expires) {
		return nil, nil
	}
	delete(s.txs, state) // single-use
	return entry.payload, nil
}

// stubUsers implements userdom.Repository.
type stubUsers struct {
	mu             sync.Mutex
	byName         map[string]*userdom.User
	byID           map[uuid.UUID]*userdom.User
	byEmail        map[string]*userdom.User
	findByEmailErr error
}

func newStubUsers() *stubUsers {
	return &stubUsers{byName: map[string]*userdom.User{}, byID: map[uuid.UUID]*userdom.User{}, byEmail: map[string]*userdom.User{}}
}

func (s *stubUsers) add(u *userdom.User) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byName[u.Username] = u
	s.byID[u.ID] = u
	if u.Email != nil {
		s.byEmail[*u.Email] = u
	}
}

func (s *stubUsers) FindByID(_ context.Context, id uuid.UUID) (*userdom.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.byID[id]; ok && u.DeletedAt == nil {
		return u, nil
	}
	return nil, userdom.ErrNotFound
}

func (s *stubUsers) FindByUsername(_ context.Context, name string) (*userdom.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.byName[name]; ok && u.DeletedAt == nil {
		return u, nil
	}
	return nil, userdom.ErrNotFound
}

func (s *stubUsers) FindByUsernameIncludingDeleted(ctx context.Context, name string) (*userdom.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.byName[name]; ok {
		return u, nil
	}
	return nil, userdom.ErrNotFound
}

func (s *stubUsers) FindByEmail(_ context.Context, email string) (*userdom.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.findByEmailErr != nil {
		return nil, s.findByEmailErr
	}
	if u, ok := s.byEmail[email]; ok && u.DeletedAt == nil {
		return u, nil
	}
	return nil, userdom.ErrNotFound
}

func (s *stubUsers) List(_ context.Context, _, _ int) ([]*userdom.User, int64, error) {
	return nil, 0, nil
}
func (s *stubUsers) CountUsers(_ context.Context) (int64, error)     { return 0, nil }
func (s *stubUsers) Create(_ context.Context, _ *userdom.User) error { return nil }
func (s *stubUsers) Update(_ context.Context, _ *userdom.User) error { return nil }
func (s *stubUsers) Delete(_ context.Context, _ uuid.UUID) error     { return nil }

// stubIdentities implements extiddom.Repository. provisioned tracks the
// user+identity pairs created via ProvisionWithUser.
type stubIdentities struct {
	mu            sync.Mutex
	byIssuerSub   map[string]*extiddom.Identity
	provisioned   []*extiddom.Identity
	provisionUser []*userdom.User
	touchCount    int
	// onProvision mirrors the real repository's transactional behavior: the
	// user row becomes visible once the transaction commits.
	onProvision func(u *userdom.User)
	// usernameTakenOnFirst makes the next ProvisionWithUser call fail with
	// ErrUsernameTaken (simulating a race the pre-check lost).
	usernameTakenOnFirst bool
	// identityTakenOnFirst makes the next ProvisionWithUser call fail with
	// ErrIdentityTaken (simulating a concurrent first login that won the
	// (issuer, subject) unique-index race).
	identityTakenOnFirst bool
}

func newStubIdentities() *stubIdentities {
	return &stubIdentities{byIssuerSub: map[string]*extiddom.Identity{}}
}

func key(issuer, subject string) string { return issuer + "|" + subject }

func (s *stubIdentities) FindByIssuerSubject(_ context.Context, issuer, subject string) (*extiddom.Identity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.byIssuerSub[key(issuer, subject)]; ok {
		return id, nil
	}
	return nil, extiddom.ErrNotFound
}

func (s *stubIdentities) TouchLastLogin(_ context.Context, _ uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.touchCount++
	return nil
}

func (s *stubIdentities) ProvisionWithUser(_ context.Context, u *userdom.User, identity *extiddom.Identity) error {
	s.mu.Lock()
	if s.identityTakenOnFirst {
		s.identityTakenOnFirst = false
		s.mu.Unlock()
		return extiddom.ErrIdentityTaken
	}
	if s.usernameTakenOnFirst {
		s.usernameTakenOnFirst = false
		s.mu.Unlock()
		return userdom.ErrUsernameTaken
	}
	s.byIssuerSub[key(identity.Issuer, identity.Subject)] = identity
	s.provisioned = append(s.provisioned, identity)
	s.provisionUser = append(s.provisionUser, u)
	onProvision := s.onProvision
	s.mu.Unlock()
	if onProvision != nil {
		onProvision(u)
	}
	return nil
}

type stubRoles struct{}

func (stubRoles) FindByName(_ context.Context, name string) (*globalroledom.GlobalRole, error) {
	if name == "USER" {
		return &globalroledom.GlobalRole{ID: uuid.MustParse("33333333-3333-3333-3333-333333333333"), Name: "USER"}, nil
	}
	return nil, globalroledom.ErrNotFound
}

type stubSessions struct {
	mu       sync.Mutex
	issued   []uuid.UUID
	remember []bool
}

func (s *stubSessions) IssueSessionForUser(_ context.Context, userID uuid.UUID, rememberMe bool) (*domainauth.TokenPair, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.issued = append(s.issued, userID)
	s.remember = append(s.remember, rememberMe)
	return &domainauth.TokenPair{AccessToken: "at", RefreshToken: "rt", RefreshTTL: time.Hour}, nil
}

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

type harness struct {
	idp        *mockIdP
	svc        *Service
	txStore    *memTxStore
	users      *stubUsers
	identities *stubIdentities
	sessions   *stubSessions
}

func newHarness(t *testing.T, mutate func(*Options)) *harness {
	t.Helper()
	idp := newMockIdP(t)
	users := newStubUsers()
	identities := newStubIdentities()
	identities.onProvision = users.add // committed user rows become visible
	sessions := &stubSessions{}
	txStore := newMemTxStore()

	opts := Options{
		IssuerURL:     idp.server.URL,
		ClientID:      "paca-client",
		ClientSecret:  "s3cret",
		RedirectURL:   "https://paca.example.com/api/v1/auth/oidc/callback",
		Scopes:        []string{"openid", "profile", "email"},
		DisplayName:   "Company SSO",
		DefaultRole:   "USER",
		UsernameClaim: "preferred_username",
	}
	if mutate != nil {
		mutate(&opts)
	}
	svc, err := New(context.Background(), opts, txStore, users, identities, stubRoles{}, sessions, slog.Default())
	if err != nil {
		t.Fatalf("new oidc service: %v", err)
	}
	return &harness{idp: idp, svc: svc, txStore: txStore, users: users, identities: identities, sessions: sessions}
}

// beginLoginAndExtract drives BeginLogin and pulls state/nonce/challenge out
// of the resulting authorization URL.
func (h *harness) beginLoginAndExtract(t *testing.T) (authURL *url.URL, state, nonce, challenge string) {
	t.Helper()
	raw, state, err := h.svc.BeginLogin(context.Background())
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse auth url: %v", err)
	}
	q := u.Query()
	// The returned state must be the one carried in the authorization URL —
	// the handler mirrors it into the browser-binding cookie.
	if q.Get("state") != state {
		t.Fatalf("BeginLogin state %q does not match auth url state %q", state, q.Get("state"))
	}
	return u, state, q.Get("nonce"), q.Get("code_challenge")
}

// runCallback performs a full BeginLogin → Callback round-trip using the
// mock IdP's token endpoint.
func (h *harness) runCallback(t *testing.T) error {
	t.Helper()
	_, state, nonce, _ := h.beginLoginAndExtract(t)
	h.idp.mu.Lock()
	h.idp.tokenClaims = map[string]any{
		"nonce":              nonce,
		"preferred_username": "alice",
		"name":               "Alice Example",
		"email":              "alice@example.com",
		"email_verified":     true,
	}
	h.idp.mu.Unlock()
	_, err := h.svc.Callback(context.Background(), "auth-code-1", state)
	return err
}

// s256 computes the PKCE S256 challenge for a verifier
// (BASE64URL(SHA256(verifier)), unpadded).
func s256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// ---------------------------------------------------------------------------
// BeginLogin
// ---------------------------------------------------------------------------

func TestBeginLogin_BuildsProperAuthURL(t *testing.T) {
	h := newHarness(t, nil)
	u, state, nonce, challenge := h.beginLoginAndExtract(t)

	if state == "" || nonce == "" || challenge == "" {
		t.Fatalf("expected state/nonce/code_challenge in auth url: %s", u)
	}
	if got := u.Query().Get("client_id"); got != "paca-client" {
		t.Errorf("unexpected client_id %q", got)
	}
	if got := u.Query().Get("redirect_uri"); got != "https://paca.example.com/api/v1/auth/oidc/callback" {
		t.Errorf("unexpected redirect_uri %q", got)
	}
	if !strings.Contains(u.Query().Get("scope"), "openid") {
		t.Errorf("expected openid in scope, got %q", u.Query().Get("scope"))
	}
	if got := u.Query().Get("code_challenge_method"); got != "S256" {
		t.Errorf("expected S256 challenge method, got %q", got)
	}
	if u.Query().Get("response_type") != "code" {
		t.Errorf("expected authorization-code flow, got %q", u.Query().Get("response_type"))
	}
}

func TestBeginLogin_StateAndVerifierAreRandom(t *testing.T) {
	h := newHarness(t, nil)

	_, state1, _, _ := h.beginLoginAndExtract(t)
	payload1, _ := h.txStore.Consume(context.Background(), state1)

	_, state2, _, _ := h.beginLoginAndExtract(t)
	payload2, _ := h.txStore.Consume(context.Background(), state2)

	if state1 == state2 {
		t.Fatal("expected fresh random state per login")
	}
	var tx1, tx2 LoginTx
	_ = json.Unmarshal(payload1, &tx1)
	_ = json.Unmarshal(payload2, &tx2)
	if tx1.Nonce == tx2.Nonce || tx1.Verifier == tx2.Verifier {
		t.Fatal("expected fresh random nonce/verifier per login")
	}
}

func TestBeginLogin_ChallengeMatchesVerifier(t *testing.T) {
	h := newHarness(t, nil)
	_, state, _, challenge := h.beginLoginAndExtract(t)
	payload, err := h.txStore.Consume(context.Background(), state)
	if err != nil || payload == nil {
		t.Fatalf("expected stored login tx, got %v", err)
	}
	var tx LoginTx
	_ = json.Unmarshal(payload, &tx)
	if s256(tx.Verifier) != challenge {
		t.Fatalf("auth url challenge %q does not match S256(verifier)", challenge)
	}
}

// ---------------------------------------------------------------------------
// Callback — happy path and security rejections
// ---------------------------------------------------------------------------

func TestCallback_Success_JITProvisionsSSOUser(t *testing.T) {
	h := newHarness(t, nil)
	if err := h.runCallback(t); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(h.identities.provisioned) != 1 {
		t.Fatalf("expected one provisioned identity, got %d", len(h.identities.provisioned))
	}
	ident := h.identities.provisioned[0]
	if ident.Issuer != h.idp.server.URL || ident.Subject != "user-1234" {
		t.Errorf("unexpected identity key (%s, %s)", ident.Issuer, ident.Subject)
	}
	u := h.identities.provisionUser[0]
	if u.Username != "alice" {
		t.Errorf("expected username from preferred_username, got %q", u.Username)
	}
	if u.FullName != "Alice Example" || u.Email == nil || *u.Email != "alice@example.com" {
		t.Errorf("unexpected profile fields: %+v", u)
	}
	if u.MustChangePassword {
		t.Error("SSO user must not hit the must-change-password flow")
	}
	if u.PasswordLoginEnabled {
		t.Error("SSO-only user must have password login disabled")
	}
	if u.Role != "USER" {
		t.Errorf("expected default role USER, got %q", u.Role)
	}
	if len(h.sessions.issued) != 1 || h.sessions.issued[0] != u.ID {
		t.Fatalf("expected a session issued for the provisioned user")
	}
	if h.sessions.remember[0] {
		t.Error("OIDC sessions must use rememberMe=false (ephemeral TTL)")
	}
}

func TestCallback_ExistingIdentityResolvesSameUser(t *testing.T) {
	h := newHarness(t, nil)
	if err := h.runCallback(t); err != nil {
		t.Fatalf("first login: %v", err)
	}
	firstUserID := h.identities.provisionUser[0].ID

	// Second login: same (iss, sub) must resolve the same user and refresh
	// last-login — never provision a second account.
	if err := h.runCallback(t); err != nil {
		t.Fatalf("second login: %v", err)
	}
	if len(h.identities.provisioned) != 1 {
		t.Fatalf("expected identity to be reused, got %d provisions", len(h.identities.provisioned))
	}
	if len(h.sessions.issued) != 2 || h.sessions.issued[1] != firstUserID {
		t.Fatalf("expected second session for the same user")
	}
	if h.identities.touchCount != 1 {
		t.Errorf("expected last-login touch, got %d", h.identities.touchCount)
	}
}

func TestCallback_InvalidState(t *testing.T) {
	h := newHarness(t, nil)
	if _, err := h.svc.Callback(context.Background(), "code", "no-such-state"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("expected ErrInvalidState, got %v", err)
	}
}

func TestCallback_StateIsSingleUse(t *testing.T) {
	h := newHarness(t, nil)
	_, state, nonce, _ := h.beginLoginAndExtract(t)
	h.idp.mu.Lock()
	h.idp.tokenClaims = map[string]any{"nonce": nonce}
	h.idp.mu.Unlock()

	if _, err := h.svc.Callback(context.Background(), "code", state); err != nil {
		t.Fatalf("first use: %v", err)
	}
	// Replaying the same callback URL must fail — the state was consumed.
	if _, err := h.svc.Callback(context.Background(), "code", state); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("expected ErrInvalidState on replay, got %v", err)
	}
}

func TestCallback_ExpiredStateRejected(t *testing.T) {
	h := newHarness(t, nil)
	_, state, _, _ := h.beginLoginAndExtract(t)

	// Fast-forward time past the transaction TTL.
	now := h.txStore.now
	h.txStore.now = func() time.Time { return now().Add(11 * time.Minute) }

	_, err := h.svc.Callback(context.Background(), "code", state)
	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("expected ErrInvalidState for expired state, got %v", err)
	}
}

func TestCallback_NonceMismatch(t *testing.T) {
	h := newHarness(t, nil)
	_, state, _, _ := h.beginLoginAndExtract(t)
	// The IdP echoes a different nonce (token replay / MITM).
	h.idp.mu.Lock()
	h.idp.tokenClaims = map[string]any{"nonce": "attacker-nonce"}
	h.idp.mu.Unlock()

	_, err := h.svc.Callback(context.Background(), "code", state)
	if !errors.Is(err, ErrExchangeFailed) {
		t.Fatalf("expected ErrExchangeFailed, got %v", err)
	}
}

func TestCallback_WrongIssuerRejected(t *testing.T) {
	h := newHarness(t, nil)
	_, state, nonce, _ := h.beginLoginAndExtract(t)
	h.idp.mu.Lock()
	h.idp.tokenClaims = map[string]any{"nonce": nonce, "iss": "https://evil.example.com"}
	h.idp.mu.Unlock()

	_, err := h.svc.Callback(context.Background(), "code", state)
	if !errors.Is(err, ErrExchangeFailed) {
		t.Fatalf("expected ErrExchangeFailed, got %v", err)
	}
}

func TestCallback_WrongAudienceRejected(t *testing.T) {
	h := newHarness(t, nil)
	_, state, nonce, _ := h.beginLoginAndExtract(t)
	h.idp.mu.Lock()
	h.idp.tokenClaims = map[string]any{"nonce": nonce, "aud": "some-other-client"}
	h.idp.mu.Unlock()

	_, err := h.svc.Callback(context.Background(), "code", state)
	if !errors.Is(err, ErrExchangeFailed) {
		t.Fatalf("expected ErrExchangeFailed, got %v", err)
	}
}

func TestCallback_ExpiredTokenRejected(t *testing.T) {
	h := newHarness(t, nil)
	_, state, nonce, _ := h.beginLoginAndExtract(t)
	h.idp.mu.Lock()
	h.idp.tokenClaims = map[string]any{"nonce": nonce, "exp": time.Now().Add(-10 * time.Minute).Unix()}
	h.idp.mu.Unlock()

	_, err := h.svc.Callback(context.Background(), "code", state)
	if !errors.Is(err, ErrExchangeFailed) {
		t.Fatalf("expected ErrExchangeFailed, got %v", err)
	}
}

func TestCallback_InvalidSignatureRejected(t *testing.T) {
	h := newHarness(t, nil)
	_, state, nonce, _ := h.beginLoginAndExtract(t)
	// The token is signed by a different key than the published JWKS.
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate attacker key: %v", err)
	}
	h.idp.mu.Lock()
	h.idp.altKey = other
	h.idp.tokenClaims = map[string]any{"nonce": nonce}
	h.idp.mu.Unlock()

	_, err = h.svc.Callback(context.Background(), "code", state)
	if !errors.Is(err, ErrExchangeFailed) {
		t.Fatalf("expected ErrExchangeFailed, got %v", err)
	}
}

func TestCallback_TokenEndpointFailureIsGeneric(t *testing.T) {
	h := newHarness(t, nil)
	_, state, _, _ := h.beginLoginAndExtract(t)
	h.idp.mu.Lock()
	h.idp.breakTokenEndpoint = true
	h.idp.mu.Unlock()

	_, err := h.svc.Callback(context.Background(), "code", state)
	if !errors.Is(err, ErrExchangeFailed) {
		t.Fatalf("expected ErrExchangeFailed, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Identity resolution / JIT
// ---------------------------------------------------------------------------

func TestCallback_DeletedUserRejected(t *testing.T) {
	h := newHarness(t, nil)
	if err := h.runCallback(t); err != nil {
		t.Fatalf("first login: %v", err)
	}
	// Soft-delete the bound user; the binding stays but login must fail.
	u := h.identities.provisionUser[0]
	now := time.Now()
	u.DeletedAt = &now
	h.users.add(u)

	if err := h.runCallback(t); !errors.Is(err, ErrUserRejected) {
		t.Fatalf("expected ErrUserRejected, got %v", err)
	}
}

func TestCallback_SameEmailDifferentSubjectDoesNotLink(t *testing.T) {
	h := newHarness(t, nil)
	// A local user already owns the email.
	local := &userdom.User{
		ID:                   uuid.New(),
		Username:             "alice.local",
		PasswordHash:         "hash",
		Role:                 "USER",
		PasswordLoginEnabled: true,
	}
	email := "alice@example.com"
	local.Email = &email
	h.users.add(local)

	// An SSO identity with the same email but a different (iss, sub) must
	// NOT be linked to the local user — it provisions a separate account
	// (and drops the colliding email rather than claiming it).
	if err := h.runCallback(t); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(h.identities.provisionUser) != 1 {
		t.Fatal("expected a provisioned user")
	}
	created := h.identities.provisionUser[0]
	if created.ID == local.ID {
		t.Fatal("SSO identity must not bind to the same-email local user")
	}
	if created.Email != nil {
		t.Errorf("colliding email must be dropped, got %q", *created.Email)
	}
}

func TestCallback_EmailLookupFailureStopsJITProvisioning(t *testing.T) {
	h := newHarness(t, nil)
	wantErr := errors.New("database unavailable")
	h.users.mu.Lock()
	h.users.findByEmailErr = wantErr
	h.users.mu.Unlock()

	err := h.runCallback(t)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected email lookup failure, got %v", err)
	}
	if len(h.identities.provisionUser) != 0 {
		t.Fatal("must not provision a user after an email lookup failure")
	}
	if len(h.sessions.issued) != 0 {
		t.Fatal("must not issue a session after an email lookup failure")
	}
}

func TestCallback_UsernameCollisionGetsSuffix(t *testing.T) {
	h := newHarness(t, nil)
	// Occupy the preferred_username.
	h.users.add(&userdom.User{ID: uuid.New(), Username: "alice", Role: "USER", PasswordLoginEnabled: true})

	if err := h.runCallback(t); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	created := h.identities.provisionUser[0]
	if created.Username != "alice-2" {
		t.Fatalf("expected collision suffix alice-2, got %q", created.Username)
	}
}

func TestCallback_UsernameRaceRetried(t *testing.T) {
	h := newHarness(t, nil)
	h.identities.mu.Lock()
	h.identities.usernameTakenOnFirst = true
	h.identities.mu.Unlock()

	if err := h.runCallback(t); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	created := h.identities.provisionUser[0]
	if created.Username != "alice-2" {
		t.Fatalf("expected retry with alice-2, got %q", created.Username)
	}
}

// wantFallback mirrors the opaque username fallback: sso- + first 12 hex
// chars of SHA-256(issuer NUL subject).
func wantFallback(issuer, subject string) string {
	sum := sha256.Sum256([]byte(issuer + "\x00" + subject))
	return "sso-" + hex.EncodeToString(sum[:])[:12]
}

func TestUsernameCandidate(t *testing.T) {
	const issuer = "https://id.example.com"
	cases := []struct {
		claim, subject, want string
	}{
		{"Alice_Example", "s", "alice_example"},
		{"alice@example.com", "s", "alice-example.com"},
		{"ab", "abcdefgh123", wantFallback(issuer, "abcdefgh123")}, // too short → opaque fallback
		{"", "abcdefgh123", wantFallback(issuer, "abcdefgh123")},   // missing claim → fallback
		// The raw subject is an arbitrary IdP-local string — the fallback
		// must never leak it into the username.
		{"", "!!!---raw-subject---!!!", wantFallback(issuer, "!!!---raw-subject---!!!")},
		{"UPPER", "s", "upper"},
		{"--leader--", "abcdefgh123", "leader"}, // surrounding dashes trimmed
	}
	for _, tc := range cases {
		if got := usernameCandidate(tc.claim, issuer, tc.subject); got != tc.want {
			t.Errorf("usernameCandidate(%q, %q) = %q, want %q", tc.claim, tc.subject, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// UserInfo enrichment
// ---------------------------------------------------------------------------

// TestCallback_UserInfoEnrichesProfile verifies that profile claims served
// only by the UserInfo endpoint (not the ID token) still reach the JIT user —
// many standard providers do not put profile/email into the ID token.
func TestCallback_UserInfoEnrichesProfile(t *testing.T) {
	h := newHarness(t, nil)
	_, state, nonce, _ := h.beginLoginAndExtract(t)

	// ID token carries identity only; the profile lives in UserInfo.
	h.idp.mu.Lock()
	h.idp.tokenClaims = map[string]any{"nonce": nonce}
	h.idp.userInfoClaims = map[string]any{
		"preferred_username": "alice",
		"name":               "Alice Example",
		"email":              "alice@example.com",
		"email_verified":     true,
	}
	h.idp.mu.Unlock()

	if _, err := h.svc.Callback(context.Background(), "code", state); err != nil {
		t.Fatalf("callback: %v", err)
	}
	created := h.identities.provisionUser[0]
	if created.Username != "alice" || created.FullName != "Alice Example" {
		t.Errorf("expected userinfo-derived profile, got %+v", created)
	}
	if created.Email == nil || *created.Email != "alice@example.com" {
		t.Errorf("expected userinfo email, got %v", created.Email)
	}
	// The userinfo request must have been authenticated with the access
	// token issued by the code exchange.
	h.idp.mu.Lock()
	bearer := h.idp.lastBearerToken
	h.idp.mu.Unlock()
	if !strings.HasPrefix(bearer, "Bearer ") || strings.TrimPrefix(bearer, "Bearer ") == "" {
		t.Errorf("userinfo must be called with the exchanged access token, got %q", bearer)
	}
}

// TestCallback_UserInfoOnlyFillsMissingClaims: ID-token claims always win;
// UserInfo only completes what the ID token lacks.
func TestCallback_UserInfoOnlyFillsMissingClaims(t *testing.T) {
	h := newHarness(t, nil)
	_, state, nonce, _ := h.beginLoginAndExtract(t)

	h.idp.mu.Lock()
	h.idp.tokenClaims = map[string]any{
		"nonce":              nonce,
		"preferred_username": "idtoken-name",
		"name":               "ID Token Name",
		"email":              "idtoken@example.com",
		"email_verified":     true,
	}
	h.idp.userInfoClaims = map[string]any{
		"preferred_username": "userinfo-name",
		"name":               "UserInfo Name",
		"email":              "userinfo@example.com",
		"email_verified":     true,
	}
	h.idp.mu.Unlock()

	if _, err := h.svc.Callback(context.Background(), "code", state); err != nil {
		t.Fatalf("callback: %v", err)
	}
	created := h.identities.provisionUser[0]
	if created.Username != "idtoken-name" || created.FullName != "ID Token Name" {
		t.Errorf("id-token claims must win, got %+v", created)
	}
	if created.Email == nil || *created.Email != "idtoken@example.com" {
		t.Errorf("id-token email must win, got %v", created.Email)
	}
}

// TestCallback_UserInfoHonorsCustomUsernameClaim: the configured username
// claim resolves from UserInfo too, not only from the ID token.
func TestCallback_UserInfoHonorsCustomUsernameClaim(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.UsernameClaim = "nickname" })
	_, state, nonce, _ := h.beginLoginAndExtract(t)

	h.idp.mu.Lock()
	h.idp.tokenClaims = map[string]any{"nonce": nonce}
	h.idp.userInfoClaims = map[string]any{"nickname": "alice"}
	h.idp.mu.Unlock()

	if _, err := h.svc.Callback(context.Background(), "code", state); err != nil {
		t.Fatalf("callback: %v", err)
	}
	created := h.identities.provisionUser[0]
	if created.Username != "alice" {
		t.Errorf("expected username from configured claim via userinfo, got %q", created.Username)
	}
}

// TestCallback_UserInfoSubjectMismatchIgnored: a UserInfo response about a
// different subject must never be merged into the login.
func TestCallback_UserInfoSubjectMismatchIgnored(t *testing.T) {
	h := newHarness(t, nil)
	_, state, nonce, _ := h.beginLoginAndExtract(t)

	h.idp.mu.Lock()
	h.idp.tokenClaims = map[string]any{
		"nonce":              nonce,
		"preferred_username": "idtoken-name",
		"name":               "ID Token Name",
	}
	h.idp.userInfoClaims = map[string]any{
		"sub":                "someone-else",
		"preferred_username": "attacker",
		"name":               "Attacker Name",
		"email":              "attacker@example.com",
		"email_verified":     true,
	}
	h.idp.mu.Unlock()

	if _, err := h.svc.Callback(context.Background(), "code", state); err != nil {
		t.Fatalf("callback: %v", err)
	}
	created := h.identities.provisionUser[0]
	if created.Username != "idtoken-name" || created.FullName != "ID Token Name" {
		t.Errorf("mismatched userinfo must be ignored, got %+v", created)
	}
}

// TestCallback_UserInfoUnavailableFallsBack: when the userinfo endpoint
// fails, the ID-token claims stand — identity was already established.
func TestCallback_UserInfoUnavailableFallsBack(t *testing.T) {
	h := newHarness(t, nil)
	_, state, nonce, _ := h.beginLoginAndExtract(t)

	h.idp.mu.Lock()
	h.idp.tokenClaims = map[string]any{
		"nonce":              nonce,
		"preferred_username": "alice",
		"name":               "Alice Example",
		"email":              "alice@example.com",
		"email_verified":     true,
	}
	h.idp.breakUserInfo = true
	h.idp.mu.Unlock()

	if _, err := h.svc.Callback(context.Background(), "code", state); err != nil {
		t.Fatalf("callback: %v", err)
	}
	created := h.identities.provisionUser[0]
	if created.Username != "alice" || created.FullName != "Alice Example" {
		t.Errorf("expected id-token fallback profile, got %+v", created)
	}
}

// TestCallback_UnverifiedEmailNotStored: an email the IdP has not verified
// (email_verified missing or false) must not be written to the user record.
func TestCallback_UnverifiedEmailNotStored(t *testing.T) {
	h := newHarness(t, nil)
	_, state, nonce, _ := h.beginLoginAndExtract(t)

	h.idp.mu.Lock()
	h.idp.tokenClaims = map[string]any{
		"nonce":              nonce,
		"preferred_username": "alice",
		"name":               "Alice Example",
		"email":              "alice@example.com",
		// email_verified intentionally absent → false
	}
	h.idp.userInfoClaims = map[string]any{
		"email": "alice@elsewhere.example",
		// email_verified intentionally absent → false
	}
	h.idp.mu.Unlock()

	if _, err := h.svc.Callback(context.Background(), "code", state); err != nil {
		t.Fatalf("callback: %v", err)
	}
	created := h.identities.provisionUser[0]
	if created.Email != nil {
		t.Errorf("unverified email must not be stored, got %q", *created.Email)
	}
}

// ---------------------------------------------------------------------------
// Concurrent first-login race
// ---------------------------------------------------------------------------

// TestCallback_IdentityRaceResolvesWinner: when two concurrent first logins
// race and this one loses the (issuer, subject) unique-index race, the
// winner's bound user must be resolved instead of failing the login.
func TestCallback_IdentityRaceResolvesWinner(t *testing.T) {
	h := newHarness(t, nil)
	// Pre-register the winner: another request already bound (iss, sub) to
	// this user and committed.
	winner := &userdom.User{
		ID:                   uuid.New(),
		Username:             "winner",
		PasswordHash:         "hash",
		Role:                 "USER",
		PasswordLoginEnabled: false,
	}
	h.users.add(winner)
	h.identities.mu.Lock()
	h.identities.byIssuerSub[key(h.idp.server.URL, "user-1234")] = &extiddom.Identity{
		ID: uuid.New(), UserID: winner.ID, Provider: "oidc",
		Issuer: h.idp.server.URL, Subject: "user-1234",
	}
	h.identities.identityTakenOnFirst = true // simulate losing the insert race
	h.identities.mu.Unlock()

	if err := h.runCallback(t); err != nil {
		t.Fatalf("callback must resolve the race winner, got %v", err)
	}
	if len(h.sessions.issued) != 1 || h.sessions.issued[0] != winner.ID {
		t.Fatalf("expected a session for the winner user, got %v", h.sessions.issued)
	}
	if len(h.identities.provisionUser) != 0 {
		t.Fatalf("no new user must be provisioned when the race is lost")
	}
}
