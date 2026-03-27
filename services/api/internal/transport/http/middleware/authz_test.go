package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	domainauth "github.com/paca/api/internal/domain/auth"
	"github.com/paca/api/internal/platform/authz"
)

func withClaims(role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(claimsKey, &domainauth.Claims{
			RegisteredClaims: jwt.RegisteredClaims{Subject: "user-id"},
			Role:             role,
			Kind:             "access",
		})
		c.Next()
	}
}

func TestAuthz_Unauthenticated(t *testing.T) {
	r := gin.New()
	r.GET("/admin", Authz(authz.NewPolicy(), "ADMIN"), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestAuthz_Forbidden(t *testing.T) {
	r := gin.New()
	r.GET("/admin", withClaims("USER"), Authz(authz.NewPolicy(), "ADMIN"), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestAuthz_Allowed(t *testing.T) {
	r := gin.New()
	r.GET("/admin", withClaims("ADMIN"), Authz(authz.NewPolicy(), "ADMIN"), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
