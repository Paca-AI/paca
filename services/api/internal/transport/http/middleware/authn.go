// Package middleware provides per-route HTTP middleware for authentication and
// authorization.
package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	domainauth "github.com/paca/api/internal/domain/auth"
	jwttoken "github.com/paca/api/internal/platform/token"
)

const claimsKey = "claims"

// Authn validates the Bearer JWT in the Authorization header and stores the
// parsed claims in the context for downstream handlers.
func Authn(tm *jwttoken.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing Authorization header"})
			return
		}

		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "malformed Authorization header"})
			return
		}

		claims, err := tm.Verify(parts[1])
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			return
		}

		if claims.Kind != "access" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "expected access token"})
			return
		}

		c.Set(claimsKey, claims)
		c.Next()
	}
}

// ClaimsFrom retrieves the authenticated claims from the Gin context.
// It returns nil if no claims are present (e.g. on unauthenticated routes).
func ClaimsFrom(c *gin.Context) *domainauth.Claims {
	v, exists := c.Get(claimsKey)
	if !exists {
		return nil
	}
	claims, _ := v.(*domainauth.Claims)
	return claims
}
