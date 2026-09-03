package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSameHostnameOrigin(t *testing.T) {
	cases := []struct {
		name   string
		origin string
		host   string
		want   bool
	}{
		{"same host, different port", "http://paca.example.com:31842", "paca.example.com", true},
		{"same host, both with explicit ports", "https://paca.example.com:31842", "paca.example.com:443", true},
		{"same host, no port anywhere", "https://paca.example.com", "paca.example.com", true},
		{"different host", "https://evil.example.com:31842", "paca.example.com", false},
		{"subdomain is not the same host", "https://sub.paca.example.com", "paca.example.com", false},
		{"unparseable origin", "not a url", "paca.example.com", false},
		{"empty origin", "", "paca.example.com", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameHostnameOrigin(tc.origin, tc.host); got != tc.want {
				t.Errorf("sameHostnameOrigin(%q, %q) = %v, want %v", tc.origin, tc.host, got, tc.want)
			}
		})
	}
}

// TestCORSMiddleware_SameHostnameGetsCredentialedAccess confirms the
// extension's own use case works: a forwarded environment port on the same
// hostname as the API, a different port, gets an exact Origin echo plus
// Allow-Credentials — required for its content script's `credentials:
// "include"` fetch to actually have access_token/refresh_token attached
// and readable (see corsMiddleware's own doc comment).
func TestCORSMiddleware_SameHostnameGetsCredentialedAccess(t *testing.T) {
	mw := corsMiddleware(nil) // default allow-all config, as most deployments run
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://paca.example.com/api/v1/projects", nil)
	req.Host = "paca.example.com"
	req.Header.Set("Origin", "http://paca.example.com:31842")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://paca.example.com:31842" {
		t.Errorf("Allow-Origin = %q, want exact origin echo", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Allow-Credentials = %q, want \"true\"", got)
	}
}

// TestCORSMiddleware_UnrelatedOriginUnaffected confirms the new branch is
// narrow: a request from a genuinely different domain still gets today's
// unchanged behavior (wildcard echo, no credentials) rather than picking up
// Allow-Credentials by accident.
func TestCORSMiddleware_UnrelatedOriginUnaffected(t *testing.T) {
	mw := corsMiddleware(nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://paca.example.com/api/v1/projects", nil)
	req.Host = "paca.example.com"
	req.Header.Set("Origin", "https://evil.example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Allow-Origin = %q, want \"*\"", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("Allow-Credentials = %q, want empty (must never be set for an unrelated origin)", got)
	}
}

// TestCORSMiddleware_AllowListedOriginUnaffected confirms an operator's
// explicit CORS_ORIGINS allow-list still behaves exactly as before for an
// origin that isn't same-hostname.
func TestCORSMiddleware_AllowListedOriginUnaffected(t *testing.T) {
	mw := corsMiddleware([]string{"https://app.other.com"})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://paca.example.com/api/v1/projects", nil)
	req.Host = "paca.example.com"
	req.Header.Set("Origin", "https://app.other.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.other.com" {
		t.Errorf("Allow-Origin = %q, want exact allow-listed origin echo", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("Allow-Credentials = %q, want empty", got)
	}
}
