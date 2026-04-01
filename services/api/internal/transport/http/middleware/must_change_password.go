package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/paca/api/internal/apierr"
	"github.com/paca/api/internal/transport/http/presenter"
)

// RequireFreshPassword rejects any request whose access token carries
// MustChangePassword=true. Apply this middleware after Authn on every route
// except PATCH /users/me/password.
func RequireFreshPassword() gin.HandlerFunc {
	return func(c *gin.Context) {
		claims := ClaimsFrom(c)
		if claims != nil && claims.MustChangePassword {
			presenter.Error(c, apierr.New(
				apierr.CodePasswordChangeRequired,
				"you must change your password before accessing this resource",
			))
			return
		}
		c.Next()
	}
}
