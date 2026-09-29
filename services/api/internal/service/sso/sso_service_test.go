package sso

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"

	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	ssodom "github.com/Paca-AI/api/internal/domain/sso"
	userdom "github.com/Paca-AI/api/internal/domain/user"
)

// ---------------------------------------------------------------------------
// fakes
// ---------------------------------------------------------------------------

type fakeRepo struct {
	mu         sync.Mutex
	providers  map[uuid.UUID]*ssodom.Provider
	identities []*ssodom.Identity
	locks      sync.Map // "provider:subject" -> *sync.Mutex
}

func (r *fakeRepo) LockIdentity(_ context.Context, providerID uuid.UUID, subject string) (func(), error) {
	m, _ := r.locks.LoadOrStore(providerID.String()+":"+subject, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock, nil
}

func newFakeRepo() *fakeRepo { return &fakeRepo{providers: map[uuid.UUID]*ssodom.Provider{}} }

func (r *fakeRepo) ListProviders(context.Context) ([]*ssodom.Provider, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*ssodom.Provider
	for _, p := range r.providers {
		cp := *p
		out = append(out, &cp)
	}
	return out, nil
}
func (r *fakeRepo) FindProviderByID(_ context.Context, id uuid.UUID) (*ssodom.Provider, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.providers[id]; ok {
		cp := *p
		return &cp, nil
	}
	return nil, ssodom.ErrProviderNotFound
}
func (r *fakeRepo) FindProviderBySlug(_ context.Context, slug string) (*ssodom.Provider, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.providers {
		if p.Slug == slug {
			cp := *p
			return &cp, nil
		}
	}
	return nil, ssodom.ErrProviderNotFound
}
func (r *fakeRepo) CreateProvider(_ context.Context, p *ssodom.Provider) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, q := range r.providers {
		if q.Slug == p.Slug {
			return ssodom.ErrSlugTaken
		}
	}
	cp := *p
	r.providers[p.ID] = &cp
	return nil
}
func (r *fakeRepo) UpdateProvider(_ context.Context, p *ssodom.Provider) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *p
	r.providers[p.ID] = &cp
	return nil
}
func (r *fakeRepo) DeleteProvider(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.providers, id)
	return nil
}
func (r *fakeRepo) FindIdentity(_ context.Context, providerID uuid.UUID, subject string) (*ssodom.Identity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, i := range r.identities {
		if i.ProviderID == providerID && i.Subject == subject {
			return i, nil
		}
	}
	return nil, nil
}
func (r *fakeRepo) CreateIdentity(_ context.Context, i *ssodom.Identity) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, x := range r.identities {
		if x.ProviderID == i.ProviderID && x.Subject == i.Subject {
			return ssodom.ErrIdentityExists
		}
	}
	r.identities = append(r.identities, i)
	return nil
}
func (r *fakeRepo) TouchIdentity(context.Context, uuid.UUID, *string) error { return nil }

type fakeStates struct {
	mu sync.Mutex
	m  map[string]*ssodom.AuthAttempt
}

func (s *fakeStates) Put(_ context.Context, state string, a *ssodom.AuthAttempt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[state] = a
	return nil
}
func (s *fakeStates) Take(_ context.Context, state string) (*ssodom.AuthAttempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.m[state]
	delete(s.m, state)
	return a, nil
}

// fakeUsers implements the userdom.Repository methods SSO uses, and
// UserCreator.
type fakeUsers struct {
	userdom.Repository
	mu   sync.Mutex
	byID map[uuid.UUID]*userdom.User
	// createDelay slows provisioning so concurrent sign-ins overlap.
	createDelay time.Duration
}

func newFakeUsers(us ...*userdom.User) *fakeUsers {
	f := &fakeUsers{byID: map[uuid.UUID]*userdom.User{}}
	for _, u := range us {
		f.byID[u.ID] = u
	}
	return f
}
func (f *fakeUsers) FindByID(_ context.Context, id uuid.UUID) (*userdom.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.byID[id]; ok && u.DeletedAt == nil {
		return u, nil
	}
	return nil, userdom.ErrNotFound
}
func (f *fakeUsers) FindByUsername(_ context.Context, name string) (*userdom.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.byID {
		if u.Username == name && u.DeletedAt == nil {
			return u, nil
		}
	}
	return nil, userdom.ErrNotFound
}
func (f *fakeUsers) FindByEmail(_ context.Context, email string) (*userdom.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.byID {
		if u.Email != nil && *u.Email == email && u.DeletedAt == nil {
			return u, nil
		}
	}
	return nil, userdom.ErrNotFound
}

// fakeCreator provisions into a fakeUsers (usersvc.Service.Create's role).
type fakeCreator struct{ *fakeUsers }

func (f fakeCreator) Create(_ context.Context, in userdom.CreateInput) (*userdom.User, error) {
	time.Sleep(f.createDelay)
	f.mu.Lock()
	defer f.mu.Unlock()
	u := &userdom.User{ID: uuid.New(), Username: in.Username, FullName: in.FullName, Role: "USER"}
	if in.Email != "" {
		u.Email = &in.Email
	}
	f.byID[u.ID] = u
	return u, nil
}

type fakeSessions struct{ issuedFor *userdom.User }

func (f *fakeSessions) IssueSession(u *userdom.User, _ bool) (*domainauth.TokenPair, error) {
	f.issuedFor = u
	return &domainauth.TokenPair{AccessToken: "access-" + u.Username}, nil
}

// ---------------------------------------------------------------------------
// fake identity provider
// ---------------------------------------------------------------------------

// fakeIDP is a minimal OIDC provider: discovery, JWKS, and a token endpoint
// that returns an RS256 ID token carrying claims plus the nonce and PKCE
// challenge it was given at the authorize step.
type fakeIDP struct {
	t      *testing.T
	srv    *httptest.Server
	key    *rsa.PrivateKey
	claims map[string]any

	issuerSuffix string // appended to the advertised issuer (e.g. "/")

	mu        sync.Mutex
	nonce     string
	challenge string
}

func newFakeIDP(t *testing.T, claims map[string]any) *fakeIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &fakeIDP{t: t, key: key, claims: claims}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                idp.srv.URL + idp.issuerSuffix,
			"authorization_endpoint":                idp.srv.URL + "/authorize",
			"token_endpoint":                        idp.srv.URL + "/token",
			"jwks_uri":                              idp.srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"},
		}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("code") != "good-code" || r.Form.Get("code_verifier") == "" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		idp.mu.Lock()
		nonce := idp.nonce
		idp.mu.Unlock()
		payload := map[string]any{
			"iss":   idp.srv.URL,
			"aud":   "paca-client",
			"exp":   time.Now().Add(time.Hour).Unix(),
			"iat":   time.Now().Unix(),
			"nonce": nonce,
		}
		for k, v := range idp.claims {
			payload[k] = v
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at", "token_type": "Bearer", "expires_in": 3600,
			"id_token": idp.sign(payload),
		})
	})
	idp.srv = httptest.NewServer(mux)
	t.Cleanup(idp.srv.Close)
	return idp
}

func (idp *fakeIDP) sign(payload map[string]any) string {
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: idp.key},
		(&jose.SignerOptions{}).WithHeader("kid", "k1"))
	if err != nil {
		idp.t.Fatal(err)
	}
	b, _ := json.Marshal(payload)
	obj, err := signer.Sign(b)
	if err != nil {
		idp.t.Fatal(err)
	}
	s, _ := obj.CompactSerialize()
	return s
}

// authorize plays the browser visiting authURL: records the nonce and
// returns the state the provider would redirect back with.
func (idp *fakeIDP) authorize(t *testing.T, authURL string) string {
	t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		t.Fatalf("auth URL lacks PKCE: %s", authURL)
	}
	if !strings.HasSuffix(q.Get("redirect_uri"), "/api/v1/auth/sso/corp/callback") {
		t.Fatalf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	idp.mu.Lock()
	idp.nonce = q.Get("nonce")
	idp.challenge = q.Get("code_challenge")
	idp.mu.Unlock()
	return q.Get("state")
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func newTestService(t *testing.T, users *fakeUsers) (*Service, *fakeRepo, *fakeSessions) {
	t.Helper()
	repo := newFakeRepo()
	sessions := &fakeSessions{}
	svc := New(repo, &fakeStates{m: map[string]*ssodom.AuthAttempt{}}, users, fakeCreator{users}, sessions,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	return svc, repo, sessions
}

func createCorp(t *testing.T, svc *Service, issuer string, mutate func(*ssodom.ProviderInput)) *ssodom.Provider {
	t.Helper()
	secretVal := "shh"
	in := ssodom.ProviderInput{
		Slug: "corp", DisplayName: "Corp SSO", IssuerURL: issuer, ClientID: "paca-client",
		ClientSecret: &secretVal, Enabled: true, AutoProvision: true,
	}
	if mutate != nil {
		mutate(&in)
	}
	p, err := svc.CreateProvider(context.Background(), in)
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	return p
}

func TestLoginFlow_ProvisionsThenSignsBackIn(t *testing.T) {
	idp := newFakeIDP(t, map[string]any{
		"sub": "abc-123", "email": "Jane.Doe@Corp.example", "email_verified": true,
		"name": "Jane Doe", "preferred_username": "jane.doe@corp.example",
	})
	users := newFakeUsers()
	svc, _, sessions := newTestService(t, users)
	createCorp(t, svc, idp.srv.URL, nil)
	ctx := context.Background()

	for round := 1; round <= 2; round++ {
		authURL, binding, err := svc.BeginLogin(ctx, "corp", "https://paca.example", true, "/projects/1")
		if err != nil {
			t.Fatalf("BeginLogin: %v", err)
		}
		state := idp.authorize(t, authURL)
		pair, redirect, err := svc.CompleteLogin(ctx, "corp", "https://paca.example", "good-code", state, binding)
		if err != nil {
			t.Fatalf("round %d CompleteLogin: %v", round, err)
		}
		if redirect != "/projects/1" || pair.AccessToken != "access-jane.doe" {
			t.Fatalf("round %d: redirect=%q token=%q", round, redirect, pair.AccessToken)
		}
	}
	if len(users.byID) != 1 {
		t.Fatalf("expected exactly one provisioned user, got %d", len(users.byID))
	}
	u := sessions.issuedFor
	if u.FullName != "Jane Doe" || u.Email == nil || *u.Email != "jane.doe@corp.example" {
		t.Fatalf("provisioned user = %+v", u)
	}
}

func TestCompleteLogin_RejectsWrongBrowserAndReplay(t *testing.T) {
	idp := newFakeIDP(t, map[string]any{"sub": "abc", "email": "a@corp.example", "email_verified": true})
	svc, _, _ := newTestService(t, newFakeUsers())
	createCorp(t, svc, idp.srv.URL, nil)
	ctx := context.Background()

	authURL, binding, err := svc.BeginLogin(ctx, "corp", "https://paca.example", false, "")
	if err != nil {
		t.Fatal(err)
	}
	state := idp.authorize(t, authURL)

	if _, _, err := svc.CompleteLogin(ctx, "corp", "https://paca.example", "good-code", state, "other-browser"); !errors.Is(err, ssodom.ErrStateInvalid) {
		t.Fatalf("wrong binding: err = %v, want ErrStateInvalid", err)
	}
	// The failed attempt consumed the state: the real browser can't reuse it.
	if _, _, err := svc.CompleteLogin(ctx, "corp", "https://paca.example", "good-code", state, binding); !errors.Is(err, ssodom.ErrStateInvalid) {
		t.Fatalf("replay: err = %v, want ErrStateInvalid", err)
	}
}

func TestCompleteLogin_BadCodeFails(t *testing.T) {
	idp := newFakeIDP(t, map[string]any{"sub": "abc"})
	svc, _, _ := newTestService(t, newFakeUsers())
	createCorp(t, svc, idp.srv.URL, nil)
	ctx := context.Background()

	authURL, binding, _ := svc.BeginLogin(ctx, "corp", "https://paca.example", false, "")
	state := idp.authorize(t, authURL)
	if _, _, err := svc.CompleteLogin(ctx, "corp", "https://paca.example", "bad-code", state, binding); !errors.Is(err, ssodom.ErrExchangeFailed) {
		t.Fatalf("err = %v, want ErrExchangeFailed", err)
	}
}

func TestBeginLogin_DisabledProviderIsNotFound(t *testing.T) {
	svc, _, _ := newTestService(t, newFakeUsers())
	createCorp(t, svc, "https://unused.example", func(in *ssodom.ProviderInput) { in.Enabled = false })
	if _, _, err := svc.BeginLogin(context.Background(), "corp", "https://paca.example", false, ""); !errors.Is(err, ssodom.ErrProviderNotFound) {
		t.Fatalf("err = %v, want ErrProviderNotFound", err)
	}
}

func TestCreateProvider_EnabledRequiresDiscovery(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	svc, _, _ := newTestService(t, newFakeUsers())
	_, err := svc.CreateProvider(context.Background(), ssodom.ProviderInput{
		Slug: "corp", DisplayName: "Corp", IssuerURL: dead.URL, ClientID: "c", Enabled: true,
	})
	if !errors.Is(err, ssodom.ErrDiscoveryFailed) {
		t.Fatalf("err = %v, want ErrDiscoveryFailed", err)
	}
}

func TestCreateProvider_Validation(t *testing.T) {
	svc, _, _ := newTestService(t, newFakeUsers())
	base := ssodom.ProviderInput{Slug: "corp", DisplayName: "Corp", IssuerURL: "https://idp.example", ClientID: "c"}
	cases := map[string]func(*ssodom.ProviderInput){
		"bad slug":       func(in *ssodom.ProviderInput) { in.Slug = "Corp SSO!" },
		"no name":        func(in *ssodom.ProviderInput) { in.DisplayName = " " },
		"no client id":   func(in *ssodom.ProviderInput) { in.ClientID = "" },
		"ftp issuer":     func(in *ssodom.ProviderInput) { in.IssuerURL = "ftp://idp.example" },
		"relative issue": func(in *ssodom.ProviderInput) { in.IssuerURL = "/idp" },
		"bad domain":     func(in *ssodom.ProviderInput) { in.AllowedDomains = []string{"@corp.example"} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := base
			mutate(&in)
			if _, err := svc.CreateProvider(context.Background(), in); !errors.Is(err, ssodom.ErrInvalidProvider) {
				t.Fatalf("err = %v, want ErrInvalidProvider", err)
			}
		})
	}

	p, err := svc.CreateProvider(context.Background(), ssodom.ProviderInput{
		Slug: " Corp ", DisplayName: "Corp", IssuerURL: "https://idp.example/", ClientID: "c",
		Scopes: []string{"email", "email"}, AllowedDomains: []string{" Corp.Example "},
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Slug != "corp" || p.IssuerURL != "https://idp.example/" ||
		strings.Join(p.Scopes, " ") != "openid email" || p.AllowedDomains[0] != "corp.example" {
		t.Fatalf("normalized provider = %+v", p)
	}
}

func TestCreateProvider_PreservesTrailingSlashIssuer(t *testing.T) {
	svc, _, _ := newTestService(t, newFakeUsers())
	idp := newFakeIDP(t, nil)
	idp.issuerSuffix = "/"
	p, err := svc.CreateProvider(context.Background(), ssodom.ProviderInput{
		Slug: "corp", DisplayName: "Corp", IssuerURL: idp.srv.URL + "/", ClientID: "c", Enabled: true,
	})
	if err != nil {
		t.Fatalf("slash-terminated issuer rejected: %v", err)
	}
	if p.IssuerURL != idp.srv.URL+"/" {
		t.Fatalf("issuer = %q, want trailing slash preserved", p.IssuerURL)
	}
}

func TestUpdateProvider_NilSecretKeepsStored(t *testing.T) {
	svc, repo, _ := newTestService(t, newFakeUsers())
	p := createCorp(t, svc, "https://idp.example", func(in *ssodom.ProviderInput) { in.Enabled = false })
	_, err := svc.UpdateProvider(context.Background(), p.ID, ssodom.ProviderInput{
		Slug: "corp", DisplayName: "Renamed", IssuerURL: "https://idp.example", ClientID: "paca-client",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := repo.providers[p.ID]; got.ClientSecret != "shh" || got.DisplayName != "Renamed" {
		t.Fatalf("stored = %+v", got)
	}
}

func TestResolveUser(t *testing.T) {
	ctx := context.Background()
	existingEmail := "boss@corp.example"
	existing := &userdom.User{ID: uuid.New(), Username: "boss", Email: &existingEmail}

	provider := func(mutate func(*ssodom.Provider)) *ssodom.Provider {
		p := &ssodom.Provider{ID: uuid.New(), Slug: "corp", Enabled: true}
		if mutate != nil {
			mutate(p)
		}
		return p
	}

	t.Run("unverified email never links", func(t *testing.T) {
		svc, _, _ := newTestService(t, newFakeUsers(existing))
		p := provider(func(p *ssodom.Provider) { p.LinkByEmail = true })
		_, err := svc.ResolveUser(ctx, p, Claims{Subject: "s", Email: existingEmail})
		if !errors.Is(err, ssodom.ErrNoAccount) {
			t.Fatalf("err = %v, want ErrNoAccount", err)
		}
	})

	t.Run("verified email links when enabled", func(t *testing.T) {
		svc, repo, _ := newTestService(t, newFakeUsers(existing))
		p := provider(func(p *ssodom.Provider) { p.LinkByEmail = true })
		u, err := svc.ResolveUser(ctx, p, Claims{Subject: "s", Email: "BOSS@corp.example", EmailVerified: true})
		if err != nil || u.ID != existing.ID {
			t.Fatalf("u=%v err=%v", u, err)
		}
		if len(repo.identities) != 1 || repo.identities[0].UserID != existing.ID {
			t.Fatalf("identity not linked: %+v", repo.identities)
		}
	})

	t.Run("verified email of existing account without linking", func(t *testing.T) {
		svc, _, _ := newTestService(t, newFakeUsers(existing))
		p := provider(func(p *ssodom.Provider) { p.AutoProvision = true })
		_, err := svc.ResolveUser(ctx, p, Claims{Subject: "s", Email: existingEmail, EmailVerified: true})
		if !errors.Is(err, ssodom.ErrAccountExists) {
			t.Fatalf("err = %v, want ErrAccountExists", err)
		}
	})

	t.Run("domain allow-list", func(t *testing.T) {
		svc, _, _ := newTestService(t, newFakeUsers())
		p := provider(func(p *ssodom.Provider) {
			p.AutoProvision = true
			p.AllowedDomains = []string{"corp.example"}
		})
		for _, c := range []Claims{
			{Subject: "s1", Email: "x@evil.example", EmailVerified: true},
			{Subject: "s2", Email: "x@corp.example"}, // unverified
			{Subject: "s3"},                          // no email
		} {
			if _, err := svc.ResolveUser(ctx, p, c); !errors.Is(err, ssodom.ErrEmailNotAllowed) {
				t.Fatalf("%+v: err = %v, want ErrEmailNotAllowed", c, err)
			}
		}
		if _, err := svc.ResolveUser(ctx, p, Claims{Subject: "s4", Email: "x@corp.example", EmailVerified: true}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("no provisioning", func(t *testing.T) {
		svc, _, _ := newTestService(t, newFakeUsers())
		if _, err := svc.ResolveUser(ctx, provider(nil), Claims{Subject: "s"}); !errors.Is(err, ssodom.ErrNoAccount) {
			t.Fatalf("err = %v, want ErrNoAccount", err)
		}
	})

	t.Run("deleted linked user stays out", func(t *testing.T) {
		now := time.Now()
		gone := &userdom.User{ID: uuid.New(), Username: "gone", DeletedAt: &now}
		svc, repo, _ := newTestService(t, newFakeUsers(gone))
		p := provider(func(p *ssodom.Provider) { p.AutoProvision = true })
		repo.identities = []*ssodom.Identity{{ID: uuid.New(), UserID: gone.ID, ProviderID: p.ID, Subject: "s"}}
		if _, err := svc.ResolveUser(ctx, p, Claims{Subject: "s"}); !errors.Is(err, ssodom.ErrNoAccount) {
			t.Fatalf("err = %v, want ErrNoAccount", err)
		}
	})

	t.Run("verified email without @ does not panic", func(t *testing.T) {
		svc, _, _ := newTestService(t, newFakeUsers())
		p := provider(func(p *ssodom.Provider) { p.AutoProvision = true })
		u, err := svc.ResolveUser(ctx, p, Claims{Subject: "s", Email: "not-an-email", EmailVerified: true})
		if err != nil || u.Username != "not-an-email" {
			t.Fatalf("u=%+v err=%v", u, err)
		}
	})

	t.Run("concurrent first sign-ins create one account", func(t *testing.T) {
		users := newFakeUsers()
		users.createDelay = 20 * time.Millisecond
		svc, repo, _ := newTestService(t, users)
		p := provider(func(p *ssodom.Provider) { p.AutoProvision = true })
		const n = 10
		ids := make(chan uuid.UUID, n)
		var wg sync.WaitGroup
		for range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				u, err := svc.ResolveUser(ctx, p, Claims{Subject: "s", PreferredUsername: "jane"})
				if err != nil {
					t.Errorf("ResolveUser: %v", err)
					return
				}
				ids <- u.ID
			}()
		}
		wg.Wait()
		close(ids)
		var first uuid.UUID
		for id := range ids {
			if first == uuid.Nil {
				first = id
			} else if id != first {
				t.Fatalf("sign-ins resolved to different accounts: %s and %s", first, id)
			}
		}
		if len(users.byID) != 1 || len(repo.identities) != 1 {
			t.Fatalf("want 1 account and 1 link, got %d accounts and %d links", len(users.byID), len(repo.identities))
		}
	})

	t.Run("username collision gets a suffix", func(t *testing.T) {
		taken := &userdom.User{ID: uuid.New(), Username: "jane"}
		svc, _, _ := newTestService(t, newFakeUsers(taken))
		p := provider(func(p *ssodom.Provider) { p.AutoProvision = true })
		u, err := svc.ResolveUser(ctx, p, Claims{Subject: "s", PreferredUsername: "Jane"})
		if err != nil || u.Username != "jane2" {
			t.Fatalf("u=%+v err=%v", u, err)
		}
	})
}

func TestWithSuffix(t *testing.T) {
	if got := withSuffix(strings.Repeat("a", 32), "2"); got != strings.Repeat("a", 31)+"2" {
		t.Errorf("got %q", got)
	}
	if got := withSuffix("jane", strings.Repeat("x", 40)); len(got) != maxUsernameLen {
		t.Errorf("oversized suffix: len %d", len(got))
	}
}

func TestUsernameBase(t *testing.T) {
	cases := []struct {
		c     Claims
		email string
		want  string
	}{
		{Claims{PreferredUsername: "Jane.Doe@corp.example"}, "", "jane.doe"},
		{Claims{PreferredUsername: "DOMAIN\\jdoe"}, "", "domain-jdoe"},
		{Claims{}, "j.smith@corp.example", "j.smith"},
		{Claims{PreferredUsername: "ab"}, "", "user"},
		{Claims{}, "", "user"},
		{Claims{PreferredUsername: strings.Repeat("a", 50)}, "", strings.Repeat("a", 32)},
	}
	for _, tc := range cases {
		if got := usernameBase(tc.c, tc.email); got != tc.want {
			t.Errorf("usernameBase(%+v, %q) = %q, want %q", tc.c, tc.email, got, tc.want)
		}
	}
}

func TestSafeRedirect(t *testing.T) {
	cases := map[string]string{
		"":                       DefaultRedirect,
		"/projects/1?tab=board":  "/projects/1?tab=board",
		"//evil.example":         DefaultRedirect,
		"/\\evil.example":        DefaultRedirect,
		"https://evil.example/x": DefaultRedirect,
		"javascript:alert(1)":    DefaultRedirect,
		"home":                   DefaultRedirect,
	}
	for in, want := range cases {
		if got := SafeRedirect(in); got != want {
			t.Errorf("SafeRedirect(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFlexBool(t *testing.T) {
	var c Claims
	if err := json.Unmarshal([]byte(`{"email_verified":"true"}`), &c); err != nil || !bool(c.EmailVerified) {
		t.Fatalf("string true: %v %v", c.EmailVerified, err)
	}
	if err := json.Unmarshal([]byte(`{"email_verified":false}`), &c); err != nil || bool(c.EmailVerified) {
		t.Fatalf("bool false: %v %v", c.EmailVerified, err)
	}
}

func TestBlockedProviderIP(t *testing.T) {
	for addr, want := range map[string]bool{
		"169.254.169.254":        true,
		"fe80::1":                true,
		"0.0.0.0":                true,
		"224.0.0.1":              true,
		"127.0.0.1":              false,
		"10.1.2.3":               false,
		"93.184.216.34":          false,
		"fe80::1%eth0":           true,
		"::ffff:169.254.169.254": true,
	} {
		if got := blockedProviderIP(netip.MustParseAddr(addr)); got != want {
			t.Errorf("blockedProviderIP(%s) = %v, want %v", addr, got, want)
		}
	}
}
