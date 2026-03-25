// Package presenter maps domain/infrastructure errors to HTTP responses and
// wraps all payloads in a consistent envelope.
package presenter

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	userdom "github.com/paca/api/internal/domain/user"
)

// envelope is the standard JSON wrapper for every response.
type envelope struct {
	Success   bool   `json:"success"`
	Data      any    `json:"data,omitempty"`
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

// Error maps a domain/service error to an appropriate HTTP status and writes
// a JSON error envelope.
func Error(c *gin.Context, err error) {
	status := statusFor(err)
	c.AbortWithStatusJSON(status, envelope{
		Success:   false,
		Error:     err.Error(),
		RequestID: requestID(c),
	})
}

// statusFor derives an HTTP status code from a known domain error.
func statusFor(err error) int {
	switch {
	case errors.Is(err, userdom.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, userdom.ErrUsernameTaken):
		return http.StatusConflict
	case errors.Is(err, userdom.ErrForbidden):
		return http.StatusForbidden
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
