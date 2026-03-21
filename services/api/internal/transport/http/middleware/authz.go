package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/paca/api/internal/platform/authz"
)

// Authz returns a middleware that enforces that the authenticated caller holds
// one of the required roles.  Must be placed after Authn.
func Authz(policy *authz.Policy, roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims := ClaimsFrom(c)
		if claims == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
			return
		}

		if err := policy.Require(claims.Role, roles...); err != nil {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			return
		}

		c.Next()
	}
}
