package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// BindJSON binds the JSON body into dst and aborts with 400 on failure.
// Use this in handlers to keep binding logic concise.
func BindJSON(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return false
	}
	return true
}
