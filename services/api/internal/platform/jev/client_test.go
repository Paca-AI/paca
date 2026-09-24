package jev

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNew_EmptyKeyReturnsNil(t *testing.T) {
	if c := New("", "", ""); c != nil {
		t.Fatalf("expected nil client for empty key, got %v", c)
	}
}

func TestClient_Enabled(t *testing.T) {
	var nilClient *Client
	if nilClient.Enabled() {
		t.Fatal("nil client should not be Enabled")
	}
	if !New("k", "", "").Enabled() {
		t.Fatal("client with a key should be Enabled")
	}
}

func TestSystemOne_NilClient(t *testing.T) {
	var c *Client
	_, err := c.SystemOne(context.Background(), "state", map[string]Question{"q": {Type: TypeNoul, Instructions: "?"}})
	if err == nil {
		t.Fatal("expected error calling SystemOne on a nil client")
	}
}

func TestSystemOne_NoQuestions(t *testing.T) {
	c := New("key", "", "")
	_, err := c.SystemOne(context.Background(), "state", nil)
	if err == nil {
		t.Fatal("expected error calling SystemOne with no questions")
	}
}

func TestSystemOne_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("unexpected Authorization header: %q", got)
		}
		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != DefaultModel {
			t.Errorf("expected model %q, got %q", DefaultModel, req.Model)
		}
		conf := 0.92
		_ = json.NewEncoder(w).Encode(Response{
			Model: "jev-1.13.0",
			Answers: map[string]Answer{
				"is_urgent": {Type: TypeNoul, Noul: 0.95},
				"category": {
					Type: TypeChoice, Choice: "billing",
					Probabilities: map[string]float64{"billing": 0.9, "technical": 0.1},
					Confidence:    &conf,
				},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)

	resp, err := c.SystemOne(context.Background(), "help me", map[string]Question{
		"is_urgent": {Type: TypeNoul, Instructions: "Is this urgent?"},
		"category":  {Type: TypeChoice, Instructions: "Category?", Criteria: map[string]any{"billing": "x", "technical": "y"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Answers["is_urgent"].Noul != 0.95 {
		t.Errorf("unexpected noul value: %v", resp.Answers["is_urgent"].Noul)
	}
	if resp.Answers["category"].Choice != "billing" {
		t.Errorf("unexpected choice value: %v", resp.Answers["category"].Choice)
	}
	if resp.Answers["category"].Confidence == nil || *resp.Answers["category"].Confidence != 0.92 {
		t.Errorf("unexpected confidence: %v", resp.Answers["category"].Confidence)
	}
	if resp.Answers["is_urgent"].Confidence != nil {
		t.Errorf("noul answer should have nil confidence, got %v", *resp.Answers["is_urgent"].Confidence)
	}
}

func TestSystemOne_NonRetryableError(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)

	_, err := c.SystemOne(context.Background(), "s", map[string]Question{"q": {Type: TypeNoul, Instructions: "?"}})
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !asAPIError(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("unexpected status code: %d", apiErr.StatusCode)
	}
	if calls != 1 {
		t.Errorf("expected exactly 1 call for a non-retryable error, got %d", calls)
	}
}

func TestSystemOne_RetriesRateLimit(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_ = json.NewEncoder(w).Encode(Response{Answers: map[string]Answer{"q": {Type: TypeNoul, Noul: 0.5}}})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)

	resp, err := c.SystemOne(context.Background(), "s", map[string]Question{"q": {Type: TypeNoul, Instructions: "?"}})
	if err != nil {
		t.Fatalf("unexpected error after retries: %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls (2 failures + 1 success), got %d", calls)
	}
	if resp.Answers["q"].Noul != 0.5 {
		t.Errorf("unexpected answer: %v", resp.Answers["q"])
	}
}

// TestNew_DefaultsToSSRFSafeClient guards the fix for a project admin being
// able to point jev_base_url at an internal address and have the API process
// issue an authenticated POST to it: New must install a transport whose
// DialContext is netguard's guarded one, not the default dialer. Asserted on
// the dialer rather than the transport type — netguard returns a plain
// *http.Transport (its SafeDialContext wired in), not a named type.
func TestNew_DefaultsToSSRFSafeClient(t *testing.T) {
	c := New("k", "", "")
	if c == nil {
		t.Fatal("expected a client for a non-empty key")
	}
	tr, ok := c.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected an *http.Transport, got %T", c.httpClient.Transport)
	}
	if tr.DialContext == nil {
		t.Fatal("expected a guarded DialContext, got nil")
	}
	// Functional check: the guarded dialer must refuse loopback.
	if _, err := tr.DialContext(context.Background(), "tcp", "127.0.0.1:9"); err == nil {
		t.Error("expected the default transport to refuse a loopback destination")
	} else if !strings.Contains(err.Error(), "private/internal") {
		t.Errorf("expected netguard's private-address error, got %v", err)
	}
}

// TestSystemOne_RejectsPrivateAddress proves the guard actually blocks the
// request rather than merely being installed: an httptest.Server (loopback)
// must be unreachable through the default client. Note this is what
// newTestClient exists to work around.
func TestSystemOne_RejectsPrivateAddress(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(Response{Answers: map[string]Answer{"q": {Type: TypeNoul, Noul: 0.5}}})
	}))
	defer srv.Close()

	c := New("test-key", "", "")
	c.baseURL = srv.URL

	if _, err := c.SystemOne(context.Background(), "s", map[string]Question{"q": {Type: TypeNoul, Instructions: "?"}}); err == nil {
		t.Fatal("expected the default client to refuse a loopback destination")
	}
	if calls != 0 {
		t.Errorf("expected the request to never reach the server, got %d calls", calls)
	}
}

// TestSystemOne_TruncatesErrorBody guards maxErrorBodyBytes: a failed
// response's body is kept for the log line, but not without bound.
func TestSystemOne_TruncatesErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(strings.Repeat("x", 4*maxErrorBodyBytes)))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)

	_, err := c.SystemOne(context.Background(), "s", map[string]Question{"q": {Type: TypeNoul, Instructions: "?"}})
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !asAPIError(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if len(apiErr.Body) > maxErrorBodyBytes+32 {
		t.Errorf("expected a truncated body, got %d bytes", len(apiErr.Body))
	}
	if !strings.HasSuffix(apiErr.Body, "(truncated)") {
		t.Errorf("expected a truncation marker, got %q", apiErr.Body[max(0, len(apiErr.Body)-32):])
	}
}

// TestSystemOne_KeepsShortErrorBody is the other half: a normal, small error
// body must come through verbatim — truncation shouldn't clip the useful case.
func TestSystemOne_KeepsShortErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)

	_, err := c.SystemOne(context.Background(), "s", map[string]Question{"q": {Type: TypeNoul, Instructions: "?"}})
	var apiErr *APIError
	if !asAPIError(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Body != `{"error":"bad key"}` {
		t.Errorf("unexpected body: %q", apiErr.Body)
	}
}

// newTestClient builds a client pointed at a local httptest.Server. New's
// default client is SSRF-safe (netguard) and would reject the server's
// loopback address, so the transport is swapped for a plain one — the same
// reason AutomationConsumer.WithHTTPClient exists.
func newTestClient(t *testing.T, srvURL string) *Client {
	t.Helper()
	c := New("test-key", "", "").WithHTTPClient(&http.Client{Timeout: 5 * time.Second})
	c.baseURL = srvURL
	return c
}

// asAPIError is errors.As without importing errors in every test that needs it.
func asAPIError(err error, target **APIError) bool {
	if e, ok := err.(*APIError); ok {
		*target = e
		return true
	}
	return false
}
