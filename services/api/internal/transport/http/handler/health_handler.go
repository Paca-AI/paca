// Package handler contains the HTTP request handlers for the API service.
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// HealthHandler serves the /healthz endpoint.
type HealthHandler struct{}

// NewHealthHandler returns a HealthHandler.
func NewHealthHandler() *HealthHandler { return &HealthHandler{} }

// Check responds with a 200 OK and a short status payload.
func (h *HealthHandler) Check(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
