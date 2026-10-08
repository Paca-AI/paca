package presenter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Paca-AI/api/internal/apierr"
	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	roledom "github.com/Paca-AI/api/internal/domain/role"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
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

func TestError_IssuesIncludedForPolicyErrors(t *testing.T) {
	w := httptest.NewRecorder()
	Error(w, newTestRequest(""), apierr.NewWithIssues(apierr.CodeRolePolicyInvalid, "the policy is not valid",
		[]apierr.Issue{{Path: "statements[0].actions[1]", Message: `unknown action "x:y"`}}))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", w.Code)
	}
	var env envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.ErrorCode != "ROLE_POLICY_INVALID" || len(env.Issues) != 1 || env.Issues[0].Path != "statements[0].actions[1]" {
		t.Fatalf("envelope = %+v", env)
	}
	// ... and omitted for every other error
	w = httptest.NewRecorder()
	Error(w, newTestRequest(""), apierr.New(apierr.CodeBadRequest, "x"))
	if jsonHasKey(t, w.Body.String(), "issues") {
		t.Fatalf("issues must be omitted when empty: %s", w.Body.String())
	}
}

func TestError_RoleDomainMapping(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   apierr.Code
	}{
		{roledom.ErrNotFound, 404, apierr.CodeRoleNotFound},
		{roledom.ErrNameTaken, 409, apierr.CodeRoleNameTaken},
		{roledom.ErrNameInvalid, 400, apierr.CodeRoleNameInvalid},
		{roledom.ErrSystemRole, 409, apierr.CodeRoleIsSystem},
		{roledom.ErrIsDefault, 409, apierr.CodeRoleIsDefault},
		{roledom.ErrLastWildcard, 409, apierr.CodeRoleLastAdmin},
		{roledom.ErrNotAttachable, 422, apierr.CodeRoleNotAttachable},
		{fmt.Errorf("wrapped: %w", roledom.ErrNotAttachable), 422, apierr.CodeRoleNotAttachable},
		{roledom.ErrMemberNotFound, 404, apierr.CodeProjectMemberNotFound},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		Error(w, newTestRequest(""), tc.err)
		var env envelope
		_ = json.Unmarshal(w.Body.Bytes(), &env)
		if w.Code != tc.status || env.ErrorCode != string(tc.code) {
			t.Errorf("%v: %d %s, want %d %s", tc.err, w.Code, env.ErrorCode, tc.status, tc.code)
		}
		// the same code raised as an *apierr.Error maps to the same status
		w = httptest.NewRecorder()
		Error(w, newTestRequest(""), apierr.New(tc.code, "x"))
		if w.Code != tc.status {
			t.Errorf("apierr %s: status %d, want %d", tc.code, w.Code, tc.status)
		}
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

// The default of a kind (role, task status, task type) cannot be
// deleted; that is a conflict with the current state, so 409 with a code the
// UI can tell apart from "still in use".
func TestStatusAndCodeFor_DefaultCannotBeDeleted(t *testing.T) {
	cases := []struct {
		err      error
		wantCode apierr.Code
	}{
		{roledom.ErrIsDefault, apierr.CodeRoleIsDefault},
		{roledom.ErrNoDefault, apierr.CodeRoleNoDefault},
		{taskdom.ErrStatusIsDefault, apierr.CodeTaskStatusIsDefault},
		{taskdom.ErrTypeIsDefault, apierr.CodeTaskTypeIsDefault},
	}

	for _, tc := range cases {
		status, code := statusAndCodeFor(tc.err)
		if status != http.StatusConflict || code != tc.wantCode {
			t.Fatalf("for %v expected (409,%s), got (%d,%s)", tc.err, tc.wantCode, status, code)
		}
	}
}

// TestStatusAndCodeFor_ProviderCLIErrors locks in that every provider_cli
// validation error agentsvc.Service can return maps to a 400 Bad Request
// with its own error code — before this test existed, none of these six
// sentinels had a case in statusAndCodeFor's switch, so they silently fell
// through to the default (500 Internal Server Error, CodeInternalError,
// and a scrubbed "internal server error" message) for what are actually
// ordinary client input errors (an invalid cli_provider, a missing
// default_environment_id, etc.) — see Error's own sanitization branch for
// why that default is specifically the wrong outcome for a 4xx-shaped
// error: it hides the real message from the client and logs a false-positive
// slog.Error for every occurrence.
func TestStatusAndCodeFor_ProviderCLIErrors(t *testing.T) {
	cases := []struct {
		err      error
		wantCode apierr.Code
	}{
		{agentdom.ErrCLIProviderInvalid, apierr.CodeAgentCLIProviderInvalid},
		{agentdom.ErrCLIAuthModeInvalid, apierr.CodeAgentCLIAuthModeInvalid},
		{agentdom.ErrCLIProviderNoAPIKeyAuth, apierr.CodeAgentCLIProviderNoAPIKeyAuth},
		{agentdom.ErrDefaultEnvironmentRequiredForCLIProvider, apierr.CodeAgentDefaultEnvironmentRequiredForCLIProvider},
		{agentdom.ErrCLIProviderNotSupportedForGlobalAgents, apierr.CodeAgentCLIProviderNotSupportedForGlobalAgents},
		{agentdom.ErrAgentNotProviderCLI, apierr.CodeAgentNotProviderCLI},
	}

	for _, tc := range cases {
		status, code := statusAndCodeFor(tc.err)
		if status != http.StatusBadRequest || code != tc.wantCode {
			t.Errorf("for %v expected (400,%s), got (%d,%s)", tc.err, tc.wantCode, status, code)
		}
	}
}

// TestStatusAndCodeFor_SkillNameInvalid is the same regression guard as
// TestStatusAndCodeFor_ProviderCLIErrors, for agentdom.ErrSkillNameInvalid
// (see validateSkillName's own doc comment on why an unmapped case here
// specifically matters: a 500 would also mean this rejection gets logged
// as an unhandled server error on every occurrence, not just a wrong
// status code).
func TestStatusAndCodeFor_SkillNameInvalid(t *testing.T) {
	status, code := statusAndCodeFor(agentdom.ErrSkillNameInvalid)
	if status != http.StatusBadRequest || code != apierr.CodeAgentSkillNameInvalid {
		t.Errorf("expected (400,%s), got (%d,%s)", apierr.CodeAgentSkillNameInvalid, status, code)
	}
}
