package presenter

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Paca-AI/api/internal/apierr"
	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	settingsdom "github.com/Paca-AI/api/internal/domain/settings"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/transport/http/httpx"
)

func newTestRequest(requestID string) *http.Request {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	if requestID != "" {
		r = r.WithContext(httpx.WithRequestID(r.Context(), requestID))
	}
	return r
}

func TestOK(t *testing.T) {
	w := httptest.NewRecorder()
	r := newTestRequest("req-1")

	OK(w, r, map[string]any{"x": 1})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var env envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !env.Success {
		t.Fatal("expected success=true")
	}
	if env.RequestID != "req-1" {
		t.Fatalf("expected request_id req-1, got %q", env.RequestID)
	}
}

func TestCreated(t *testing.T) {
	w := httptest.NewRecorder()
	r := newTestRequest("")

	Created(w, r, map[string]any{"created": true})

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
}

func TestError_DomainMapping(t *testing.T) {
	w := httptest.NewRecorder()
	r := newTestRequest("")

	Error(w, r, userdom.ErrNotFound)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}

	var env envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if env.ErrorCode != string(apierr.CodeUserNotFound) {
		t.Fatalf("expected %q, got %q", apierr.CodeUserNotFound, env.ErrorCode)
	}
}

func TestError_APIErrorCodeMapping(t *testing.T) {
	w := httptest.NewRecorder()
	r := newTestRequest("")

	Error(w, r, apierr.New(apierr.CodeBadRequest, "bad request body"))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}

	var env envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if env.ErrorCode != string(apierr.CodeBadRequest) {
		t.Fatalf("expected %q, got %q", apierr.CodeBadRequest, env.ErrorCode)
	}
	if env.Error != "bad request body" {
		t.Fatalf("expected message passthrough, got %q", env.Error)
	}
}

func TestError_DetailsIncludedForAPIErrorWithDetails(t *testing.T) {
	w := httptest.NewRecorder()
	r := newTestRequest("")

	Error(w, r, apierr.NewWithDetails(
		apierr.CodePluginIncompatibleHostVersion,
		"plugin requires a newer host",
		map[string]string{"required_version": "v0.11.2", "host_version": "v0.10.0"},
	))

	var env envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if env.ErrorDetails["required_version"] != "v0.11.2" {
		t.Fatalf("expected required_version detail %q, got %q", "v0.11.2", env.ErrorDetails["required_version"])
	}
	if env.ErrorDetails["host_version"] != "v0.10.0" {
		t.Fatalf("expected host_version detail %q, got %q", "v0.10.0", env.ErrorDetails["host_version"])
	}
}

func TestError_DetailsOmittedWhenNotSet(t *testing.T) {
	w := httptest.NewRecorder()
	r := newTestRequest("")

	Error(w, r, apierr.New(apierr.CodeBadRequest, "bad request body"))

	if strings := w.Body.String(); jsonHasKey(t, strings, "error_details") {
		t.Fatalf("expected error_details to be omitted, got body: %s", strings)
	}
}

func jsonHasKey(t *testing.T, body, key string) bool {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	_, ok := raw[key]
	return ok
}

func TestError_InternalMessageIsSanitized(t *testing.T) {
	w := httptest.NewRecorder()
	r := newTestRequest("")

	Error(w, r, errors.New("db exploded"))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}

	var env envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if env.ErrorCode != string(apierr.CodeInternalError) {
		t.Fatalf("expected INTERNAL_ERROR, got %q", env.ErrorCode)
	}
	if env.Error != "internal server error" {
		t.Fatalf("expected sanitized internal message, got %q", env.Error)
	}
}

func TestStatusAndCodeFor_DomainAuthErrors(t *testing.T) {
	cases := []struct {
		err        error
		wantStatus int
		wantCode   apierr.Code
	}{
		{domainauth.ErrInvalidCredentials, http.StatusUnauthorized, apierr.CodeInvalidCredentials},
		{domainauth.ErrTokenInvalid, http.StatusUnauthorized, apierr.CodeTokenInvalid},
		{domainauth.ErrSessionInvalidated, http.StatusUnauthorized, apierr.CodeTokenInvalid},
	}

	for _, tc := range cases {
		status, code := statusAndCodeFor(tc.err)
		if status != tc.wantStatus || code != tc.wantCode {
			t.Fatalf("for %v expected (%d,%s), got (%d,%s)", tc.err, tc.wantStatus, tc.wantCode, status, code)
		}
	}
}

func TestStatusAndCodeFor_SSOAdminInvariant(t *testing.T) {
	status, code := statusAndCodeFor(settingsdom.ErrSSOAdminRequired)
	if status != http.StatusConflict || code != apierr.CodeSSOAdminRequired {
		t.Fatalf("expected (409,%s), got (%d,%s)", apierr.CodeSSOAdminRequired, status, code)
	}
}
