package e2e_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// e2eIdP is a minimal OpenID Connect provider whose /authorize endpoint
// signs the user in immediately as whoever `claims` describes and redirects
// straight back to the caller's redirect_uri — the whole browser round trip
// with no login form.
type e2eIdP struct {
	srv *httptest.Server
	key *rsa.PrivateKey

	mu     sync.Mutex
	claims map[string]any
	nonces map[string]string // code -> nonce
}

func newE2EIdP(t *testing.T) *e2eIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &e2eIdP{key: key, nonces: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                idp.srv.URL,
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
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		code := "code-" + q.Get("state")[:8]
		idp.mu.Lock()
		idp.nonces[code] = q.Get("nonce")
		idp.mu.Unlock()
		back := q.Get("redirect_uri") + "?" + url.Values{"code": {code}, "state": {q.Get("state")}}.Encode()
		http.Redirect(w, r, back, http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		idp.mu.Lock()
		nonce, ok := idp.nonces[r.Form.Get("code")]
		delete(idp.nonces, r.Form.Get("code"))
		claims := idp.claims
		idp.mu.Unlock()
		if !ok || r.Form.Get("code_verifier") == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		payload := map[string]any{
			"iss": idp.srv.URL, "aud": "paca-e2e", "nonce": nonce,
			"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
		}
		for k, v := range claims {
			payload[k] = v
		}
		signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key},
			(&jose.SignerOptions{}).WithHeader("kid", "k1"))
		b, _ := json.Marshal(payload)
		obj, _ := signer.Sign(b)
		idToken, _ := obj.CompactSerialize()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at", "token_type": "Bearer", "expires_in": 3600, "id_token": idToken,
		})
	})
	idp.srv = httptest.NewServer(mux)
	t.Cleanup(idp.srv.Close)
	return idp
}

func (idp *e2eIdP) signInAs(claims map[string]any) {
	idp.mu.Lock()
	idp.claims = claims
	idp.mu.Unlock()
}

// ssoBrowser is a cookie-keeping client that follows the redirect chain
// Paca → provider → Paca callback, stopping at the first redirect back into
// the web app (which this API-only test server does not serve).
func ssoBrowser(t *testing.T, env *e2eEnv) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			if strings.HasPrefix(req.URL.String(), env.base) && !strings.HasPrefix(req.URL.Path, "/api/") {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

// ssoSignIn starts a sign-in through slug and returns where the flow landed
// in the web app (e.g. "/home" or "/?sso_error=no_account").
func ssoSignIn(t *testing.T, env *e2eEnv, browser *http.Client, slug, redirect string) string {
	t.Helper()
	req := mustRequest(env.ctx, t, http.MethodGet,
		env.base+"/api/v1/auth/sso/"+slug+"/login?redirect="+url.QueryEscape(redirect), nil)
	resp := mustDo(t, browser, req)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("sso flow ended with HTTP %d at %s, want a redirect into the app", resp.StatusCode, resp.Request.URL)
	}
	return resp.Header.Get("Location")
}

// TestSSOSignIn covers SSO end to end on a real database: provider
// management is gated by settings.sso.write, a first sign-in provisions an
// account with the default role, a second one signs back into the same
// account, and a provider that doesn't provision refuses unknown users.
func TestSSOSignIn(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	idp := newE2EIdP(t)

	const password = "supersecret"
	seedUser(t, env, "root", password, "Root")
	assignGlobalRolesByName(t, env, "root", "SUPER_ADMIN")
	seedUser(t, env, "admin", password, "Admin")
	assignGlobalRolesByName(t, env, "admin", "ADMIN")
	root := newLoggedInClient(t, env, "root", password)
	admin := newLoggedInClient(t, env, "admin", password)

	provider := map[string]any{
		"slug": "corp", "display_name": "Corp SSO", "issuer_url": idp.srv.URL,
		"client_id": "paca-e2e", "client_secret": "s3cret",
		"enabled": true, "auto_provision": true,
	}

	t.Run("ADMIN_cannot_manage_providers", func(t *testing.T) {
		status, _ := doJSON(t, env, admin, http.MethodPost, "/api/v1/admin/sso/providers", provider)
		if status != http.StatusForbidden {
			t.Fatalf("want 403, got %d", status)
		}
	})

	var providerID string
	t.Run("SUPER_ADMIN_creates_a_provider", func(t *testing.T) {
		status, out := doJSON(t, env, root, http.MethodPost, "/api/v1/admin/sso/providers", provider)
		if status != http.StatusCreated {
			t.Fatalf("want 201, got %d (%s: %s)", status, out.ErrorCode, out.Error)
		}
		data := assertDataMap(t, out)
		providerID, _ = data["id"].(string)
		if _, leaked := data["client_secret"]; leaked || data["has_client_secret"] != true {
			t.Fatalf("secret must not be returned, only flagged: %v", data)
		}
		if cb, _ := data["callback_url"].(string); cb != env.base+"/api/v1/auth/sso/corp/callback" {
			t.Fatalf("callback_url = %q", cb)
		}
	})

	t.Run("login_page_lists_enabled_providers_publicly", func(t *testing.T) {
		status, out := doJSON(t, env, &http.Client{}, http.MethodGet, "/api/v1/auth/sso/providers", nil)
		items, _ := out.Data.([]any)
		if status != http.StatusOK || len(items) != 1 {
			t.Fatalf("status %d, providers %v", status, out.Data)
		}
		if item, _ := items[0].(map[string]any); item["slug"] != "corp" || item["issuer_url"] != nil {
			t.Fatalf("public listing = %v", item)
		}
	})

	idp.signInAs(map[string]any{
		"sub": "u-1", "email": "jane@corp.example", "email_verified": true,
		"name": "Jane Doe", "preferred_username": "jane",
	})

	var janeID string
	t.Run("first_sign_in_provisions_an_account", func(t *testing.T) {
		browser := ssoBrowser(t, env)
		if landed := ssoSignIn(t, env, browser, "corp", "/projects"); landed != "/projects" {
			t.Fatalf("landed on %q, want /projects", landed)
		}
		status, out := doJSON(t, env, browser, http.MethodGet, "/api/v1/users/me", nil)
		if status != http.StatusOK {
			t.Fatalf("GET /users/me: want 200, got %d", status)
		}
		me := assertDataMap(t, out)
		if me["username"] != "jane" || me["full_name"] != "Jane Doe" || me["email"] != "jane@corp.example" || me["role"] != "USER" {
			t.Fatalf("provisioned user = %v", me)
		}
		janeID, _ = me["id"].(string)
	})

	t.Run("second_sign_in_reuses_the_account", func(t *testing.T) {
		browser := ssoBrowser(t, env)
		ssoSignIn(t, env, browser, "corp", "")
		_, out := doJSON(t, env, browser, http.MethodGet, "/api/v1/users/me", nil)
		if me := assertDataMap(t, out); me["id"] != janeID {
			t.Fatalf("signed in as %v, want jane (%s)", me["id"], janeID)
		}
	})

	t.Run("open_redirects_are_dropped", func(t *testing.T) {
		if landed := ssoSignIn(t, env, ssoBrowser(t, env), "corp", "https://evil.example"); landed != "/home" {
			t.Fatalf("landed on %q, want /home", landed)
		}
	})

	t.Run("without_provisioning_unknown_users_are_refused", func(t *testing.T) {
		update := map[string]any{}
		for k, v := range provider {
			update[k] = v
		}
		delete(update, "client_secret") // keeps the stored one
		update["auto_provision"] = false
		if status, out := doJSON(t, env, root, http.MethodPut, "/api/v1/admin/sso/providers/"+providerID, update); status != http.StatusOK {
			t.Fatalf("update: want 200, got %d (%s)", status, out.ErrorCode)
		}
		idp.signInAs(map[string]any{"sub": "u-2", "email": "bob@corp.example", "email_verified": true})
		if landed := ssoSignIn(t, env, ssoBrowser(t, env), "corp", ""); landed != "/?sso_error=no_account" {
			t.Fatalf("landed on %q, want the no_account error", landed)
		}
	})

	t.Run("disabled_providers_cannot_be_used", func(t *testing.T) {
		update := map[string]any{}
		for k, v := range provider {
			update[k] = v
		}
		update["enabled"] = false
		if status, _ := doJSON(t, env, root, http.MethodPut, "/api/v1/admin/sso/providers/"+providerID, update); status != http.StatusOK {
			t.Fatalf("disable: want 200, got %d", status)
		}
		if landed := ssoSignIn(t, env, ssoBrowser(t, env), "corp", ""); landed != "/?sso_error=provider_not_found" {
			t.Fatalf("landed on %q", landed)
		}
	})

	t.Run("deleting_the_provider", func(t *testing.T) {
		if status, _ := doJSON(t, env, root, http.MethodDelete, "/api/v1/admin/sso/providers/"+providerID, nil); status != http.StatusNoContent {
			t.Fatalf("delete: want 204, got %d", status)
		}
		_, out := doJSON(t, env, root, http.MethodGet, "/api/v1/admin/sso/providers", nil)
		if items, _ := out.Data.([]any); len(items) != 0 {
			t.Fatalf("providers after delete = %v", items)
		}
	})
}
