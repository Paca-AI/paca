package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newJSONOnlyTestHandler() http.Handler {
	return RequireJSONContentType()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

func TestRequireJSONContentType_Allows(t *testing.T) {
	cases := []string{
		"application/json",
		"application/json; charset=utf-8",
		"Application/JSON",
	}
	handler := newJSONOnlyTestHandler()
	for _, ct := range cases {
		t.Run(ct, func(t *testing.T) {
			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/x", strings.NewReader(`{}`))
			req.Header.Set("Content-Type", ct)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("Content-Type %q: expected 200, got %d (%s)", ct, w.Code, w.Body.String())
			}
		})
	}
}

// TestRequireJSONContentType_RejectsSafelistedTypes is the core property
// this middleware exists for: none of the three CORS-safelisted Content-Type
// values (which let a cross-site request skip preflight entirely, per the
// Fetch spec) may reach the handler. Without this rejection, a cross-site
// caller could smuggle a JSON body under one of these and still have it
// parsed by a handler that decodes with plain encoding/json — see this
// middleware's own doc comment for the full threat model.
func TestRequireJSONContentType_RejectsSafelistedTypes(t *testing.T) {
	cases := []string{
		"text/plain",
		"text/plain;charset=UTF-8",
		"application/x-www-form-urlencoded",
		"multipart/form-data; boundary=----x",
		"",
	}
	handler := newJSONOnlyTestHandler()
	for _, ct := range cases {
		t.Run(ct, func(t *testing.T) {
			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/x", strings.NewReader(`{}`))
			if ct != "" {
				req.Header.Set("Content-Type", ct)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("Content-Type %q: expected 400, got %d (%s)", ct, w.Code, w.Body.String())
			}
		})
	}
}
