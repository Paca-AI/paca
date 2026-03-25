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

// Authn validates the access JWT and stores the parsed claims in the context.
// It first checks the access_token HttpOnly cookie, then falls back to the
// Authorization: Bearer header for API/CLI clients.
func Authn(tm *jwttoken.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenStr := ""

		// 1. Try the access_token cookie (browser clients).
		if cookie, err := c.Cookie("access_token"); err == nil && cookie != "" {
			tokenStr = cookie
		}

		// 2. Fall back to Authorization: Bearer header (API/CLI clients).
		if tokenStr == "" {
			header := c.GetHeader("Authorization")
			if header != "" {
				parts := strings.SplitN(header, " ", 2)
				if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
					tokenStr = parts[1]
				}
			}
		}

		if tokenStr == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing authentication"})
			return
		}

		claims, err := tm.Verify(tokenStr)
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
