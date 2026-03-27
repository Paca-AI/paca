package presenter

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/paca/api/internal/apierr"
	domainauth "github.com/paca/api/internal/domain/auth"
	userdom "github.com/paca/api/internal/domain/user"
)

func TestOK(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("request_id", "req-1")

	OK(c, gin.H{"x": 1})

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
	c, _ := gin.CreateTestContext(w)

	Created(c, gin.H{"created": true})

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
}

func TestError_DomainMapping(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	Error(c, userdom.ErrNotFound)

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
	c, _ := gin.CreateTestContext(w)

	Error(c, apierr.New(apierr.CodeBadRequest, "bad request body"))

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

func TestError_InternalMessageIsSanitized(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	Error(c, errors.New("db exploded"))

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
