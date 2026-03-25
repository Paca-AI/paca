// Package presenter maps domain/infrastructure errors to HTTP responses and
// wraps all payloads in a consistent envelope.
package presenter

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/paca/api/internal/apierr"
	domainauth "github.com/paca/api/internal/domain/auth"
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

// Error maps a domain/service error to an HTTP status + error code and writes
// a JSON error envelope.  If err is an *apierr.Error, its code is used
// directly; otherwise the code is derived from known domain sentinel errors.
func Error(c *gin.Context, err error) {
	status, code := statusAndCodeFor(err)
	c.AbortWithStatusJSON(status, envelope{
		Success:   false,
		ErrorCode: string(code),
		Error:     err.Error(),
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
	case apierr.CodeBadRequest:
		return http.StatusBadRequest
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
