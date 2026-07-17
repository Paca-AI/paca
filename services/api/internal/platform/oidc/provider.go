// Package oidc implements a minimal OpenID Connect relying-party client used
// for Vortex SSO login and trusted-issuer bearer verification (ADR-038).
// It performs issuer discovery, caches the issuer's JWKS keyed by kid (with
// refresh on unknown kid), and verifies RS256-signed tokens.
package oidc

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// jwksMinRefreshInterval rate-limits refetches triggered by unknown kids so a
// flood of forged tokens cannot hammer the issuer's JWKS endpoint.
const jwksMinRefreshInterval = 30 * time.Second

// Discovery is the subset of the OIDC discovery document this client needs.
type Discovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
}

// Provider talks to a single OIDC issuer.  Discovery and JWKS documents are
// fetched lazily on first use and cached for the process lifetime (JWKS is
// refreshed when an unknown kid is encountered).
type Provider struct {
	issuer string
	client *http.Client

	mu            sync.Mutex
	discovery     *Discovery
	keys          map[string]*rsa.PublicKey
	lastJWKSFetch time.Time
}

// NewProvider returns a Provider for the given issuer base URL.
func NewProvider(issuer string) *Provider {
	return &Provider{
		issuer: issuer,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

// Issuer returns the configured issuer base URL.
func (p *Provider) Issuer() string { return p.issuer }

// Discover returns the cached discovery document, fetching it on first use.
func (p *Provider) Discover(ctx context.Context) (*Discovery, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.discoverLocked(ctx)
}

func (p *Provider) discoverLocked(ctx context.Context) (*Discovery, error) {
	if p.discovery != nil {
		return p.discovery, nil
	}

	var doc Discovery
	if err := p.getJSON(ctx, p.issuer+"/.well-known/openid-configuration", &doc); err != nil {
		return nil, fmt.Errorf("oidc: discovery: %w", err)
	}
	if doc.JWKSURI == "" {
		return nil, fmt.Errorf("oidc: discovery: issuer %q returned no jwks_uri", p.issuer)
	}

	p.discovery = &doc
	return p.discovery, nil
}

// VerifyToken parses and validates an RS256 token issued by this provider:
// signature against the issuer JWKS, iss, exp (required) and — when
// expectedAudience is non-empty — aud.  It returns the token's claims.
func (p *Provider) VerifyToken(ctx context.Context, rawToken, expectedAudience string) (jwt.MapClaims, error) {
	opts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(p.issuer),
		jwt.WithExpirationRequired(),
	}
	if expectedAudience != "" {
		opts = append(opts, jwt.WithAudience(expectedAudience))
	}

	token, err := jwt.Parse(rawToken, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		return p.keyForKID(ctx, kid)
	}, opts...)
	if err != nil {
		return nil, fmt.Errorf("oidc: verify token: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("oidc: verify token: invalid claims")
	}
	return claims, nil
}

// keyForKID returns the RSA public key for kid, refreshing the JWKS cache
// (rate-limited) when the kid is unknown.  An empty kid is accepted only when
// the JWKS contains exactly one key.
func (p *Provider) keyForKID(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if key := p.lookupKeyLocked(kid); key != nil {
		return key, nil
	}

	// Unknown kid — refresh unless a fetch happened very recently.
	if time.Since(p.lastJWKSFetch) >= jwksMinRefreshInterval || p.keys == nil {
		if err := p.fetchJWKSLocked(ctx); err != nil {
			return nil, err
		}
	}

	if key := p.lookupKeyLocked(kid); key != nil {
		return key, nil
	}
	return nil, fmt.Errorf("oidc: jwks: no key for kid %q", kid)
}

func (p *Provider) lookupKeyLocked(kid string) *rsa.PublicKey {
	if p.keys == nil {
		return nil
	}
	if kid == "" {
		if len(p.keys) == 1 {
			for _, k := range p.keys {
				return k
			}
		}
		return nil
	}
	return p.keys[kid]
}

// jwk is a single JSON Web Key; only RSA signing keys are supported.
type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func (p *Provider) fetchJWKSLocked(ctx context.Context) error {
	disc, err := p.discoverLocked(ctx)
	if err != nil {
		return err
	}

	var doc struct {
		Keys []jwk `json:"keys"`
	}
	if err := p.getJSON(ctx, disc.JWKSURI, &doc); err != nil {
		return fmt.Errorf("oidc: jwks: %w", err)
	}
	p.lastJWKSFetch = time.Now()

	keys := make(map[string]*rsa.PublicKey, len(doc.Keys))
	for _, k := range doc.Keys {
		if k.Kty != "RSA" || (k.Use != "" && k.Use != "sig") {
			continue
		}
		pub, err := rsaKeyFromJWK(k)
		if err != nil {
			continue // skip malformed keys; others may still verify
		}
		keys[k.Kid] = pub
	}
	p.keys = keys
	return nil
}

// rsaKeyFromJWK converts base64url-encoded modulus/exponent into a public key.
func rsaKeyFromJWK(k jwk) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("oidc: jwk %q: modulus: %w", k.Kid, err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("oidc: jwk %q: exponent: %w", k.Kid, err)
	}
	e := 0
	for _, b := range eBytes {
		e = e<<8 | int(b)
	}
	if e <= 0 {
		return nil, fmt.Errorf("oidc: jwk %q: invalid exponent", k.Kid)
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: e}, nil
}

// getJSON fetches url and decodes the JSON body into dst.
func (p *Provider) getJSON(ctx context.Context, url string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: unexpected status %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(body, dst)
}
