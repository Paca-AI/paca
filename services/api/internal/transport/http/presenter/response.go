// Package presenter maps domain/infrastructure errors to HTTP responses and
// wraps all payloads in a consistent envelope.
package presenter

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/paca/api/internal/apierr"
	domainauth "github.com/paca/api/internal/domain/auth"
	globalroledom "github.com/paca/api/internal/domain/globalrole"
	userdom "github.com/paca/api/internal/domain/user"
)

// envelope is the standard JSON wrapper for every response.
type envelope struct {
	Success   bool   `json:"success"`
	Data      any    `json:"data,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
	Error     string `json:"error,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// OK writes a 200 success response.
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, envelope{
		Success:   true,
		Data:      data,
		RequestID: requestID(c),
	})
}

// Created writes a 201 success response.
func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, envelope{
		Success:   true,
		Data:      data,
		RequestID: requestID(c),
	})
}

// NoContent writes a 204 No Content response with no body.
func NoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

// Error maps a domain/service error to an HTTP status + error code and writes
// a JSON error envelope.  If err is an *apierr.Error, its code is used
// directly; otherwise the code is derived from known domain sentinel errors.
func Error(c *gin.Context, err error) {
	status, code := statusAndCodeFor(err)

	// For internal/unexpected errors, avoid leaking implementation details to clients.
	publicMsg := err.Error()
	if status == http.StatusInternalServerError || code == apierr.CodeInternalError {
		publicMsg = "internal server error"
	}

	c.AbortWithStatusJSON(status, envelope{
		Success:   false,
		ErrorCode: string(code),
		Error:     publicMsg,
		RequestID: requestID(c),
	})
}

// statusAndCodeFor returns the HTTP status and apierr.Code for err.
func statusAndCodeFor(err error) (int, apierr.Code) {
	// Prefer an explicit apierr.Error if one was constructed upstream.
	var apiErr *apierr.Error
	if errors.As(err, &apiErr) {
		return httpStatusForCode(apiErr.Code), apiErr.Code
	}

	// Map domain sentinel errors to codes.
	switch {
	case errors.Is(err, domainauth.ErrInvalidCredentials):
		return http.StatusUnauthorized, apierr.CodeInvalidCredentials
	case errors.Is(err, domainauth.ErrTokenInvalid):
		return http.StatusUnauthorized, apierr.CodeTokenInvalid
	case errors.Is(err, domainauth.ErrSessionInvalidated):
		return http.StatusUnauthorized, apierr.CodeTokenInvalid
	case errors.Is(err, userdom.ErrNotFound):
		return http.StatusNotFound, apierr.CodeUserNotFound
	case errors.Is(err, userdom.ErrUsernameTaken):
		return http.StatusConflict, apierr.CodeUsernameTaken
	case errors.Is(err, userdom.ErrForbidden):
		return http.StatusForbidden, apierr.CodeForbidden
	case errors.Is(err, userdom.ErrInvalidCurrentPassword):
		return http.StatusUnprocessableEntity, apierr.CodeInvalidCurrentPassword
	case errors.Is(err, globalroledom.ErrNotFound):
		return http.StatusNotFound, apierr.CodeGlobalRoleNotFound
	case errors.Is(err, globalroledom.ErrNameTaken):
		return http.StatusConflict, apierr.CodeGlobalRoleNameTaken
	case errors.Is(err, globalroledom.ErrInvalidName):
		return http.StatusBadRequest, apierr.CodeGlobalRoleNameInvalid
	case errors.Is(err, globalroledom.ErrHasAssignedUsers):
		return http.StatusConflict, apierr.CodeGlobalRoleHasUsers
	default:
		return http.StatusInternalServerError, apierr.CodeInternalError
	}
}

// httpStatusForCode maps an apierr.Code to its conventional HTTP status code.
func httpStatusForCode(code apierr.Code) int {
	switch code {
	case apierr.CodeInvalidCredentials,
		apierr.CodeMissingToken,
		apierr.CodeTokenInvalid,
		apierr.CodeUnauthenticated:
		return http.StatusUnauthorized
	case apierr.CodeUserNotFound:
		return http.StatusNotFound
	case apierr.CodeUsernameTaken:
		return http.StatusConflict
	case apierr.CodeForbidden:
		return http.StatusForbidden
	case apierr.CodeGlobalRoleNotFound:
		return http.StatusNotFound
	case apierr.CodeGlobalRoleNameTaken:
		return http.StatusConflict
	case apierr.CodeGlobalRoleNameInvalid:
		return http.StatusBadRequest
	case apierr.CodeGlobalRoleHasUsers:
		return http.StatusConflict
	case apierr.CodeBadRequest:
		return http.StatusBadRequest
	case apierr.CodePasswordChangeRequired:
		return http.StatusForbidden
	case apierr.CodeInvalidCurrentPassword:
		return http.StatusUnprocessableEntity
	case apierr.CodeInternalError:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

func requestID(c *gin.Context) string {
	if id, ok := c.Get("request_id"); ok {
		if s, ok := id.(string); ok {
			return s
		}
	}
	return ""
}
