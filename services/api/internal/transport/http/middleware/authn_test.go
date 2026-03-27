package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	jwttoken "github.com/paca/api/internal/platform/token"
)

func newTestTokenManager() *jwttoken.Manager {
	return jwttoken.New("test-secret", 15*time.Minute, 24*time.Hour)
}

func TestAuthn_MissingToken(t *testing.T) {
	r := gin.New()
	r.GET("/protected", Authn(newTestTokenManager()), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}

	var env struct {
		ErrorCode string `json:"error_code"`
	}
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if env.ErrorCode != "AUTH_MISSING_TOKEN" {
		t.Fatalf("expected AUTH_MISSING_TOKEN, got %q", env.ErrorCode)
	}
}

func TestAuthn_InvalidToken(t *testing.T) {
	r := gin.New()
	r.GET("/protected", Authn(newTestTokenManager()), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer not-a-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestAuthn_ValidAccessTokenInHeader(t *testing.T) {
	tm := newTestTokenManager()
	at, err := tm.IssueAccess("user-id", "alice", "USER", "fam")
	if err != nil {
		t.Fatalf("issue access token: %v", err)
	}

	r := gin.New()
	r.GET("/protected", Authn(tm), func(c *gin.Context) {
		claims := ClaimsFrom(c)
		if claims == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "claims missing"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"username": claims.Username})
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+at)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestAuthn_RefreshTokenRejected(t *testing.T) {
	tm := newTestTokenManager()
	rt, err := tm.IssueRefresh("user-id", "alice", "USER", "fam")
	if err != nil {
		t.Fatalf("issue refresh token: %v", err)
	}

	r := gin.New()
	r.GET("/protected", Authn(tm), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+rt)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestClaimsFrom_Missing(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if claims := ClaimsFrom(c); claims != nil {
		t.Fatal("expected nil claims when absent")
	}
}
