// Package router wires global middleware and all route groups onto a
// *gin.Engine.
package router

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paca/api/internal/platform/authz"
	"github.com/paca/api/internal/platform/token"
	"github.com/paca/api/internal/transport/http/handler"
	httpmw "github.com/paca/api/internal/transport/http/middleware"
)

// Deps holds all handler and middleware dependencies.
type Deps struct {
	TokenManager *token.Manager
	AuthzPolicy  *authz.Policy
	Health       *handler.HealthHandler
	Auth         *handler.AuthHandler
	User         *handler.UserHandler
	Log          *slog.Logger
}

// New builds and returns a configured *gin.Engine.
func New(deps Deps) *gin.Engine {
	r := gin.New()

	// Global middleware
	r.Use(requestIDMiddleware())
	r.Use(loggerMiddleware(deps.Log))
	r.Use(gin.Recovery())
	r.Use(corsMiddleware())

	// Public routes
	r.GET("/healthz", deps.Health.Check)

	v1 := r.Group("/v1")
	{
		auth := v1.Group("/auth")
		{
			auth.POST("/login", deps.Auth.Login)
			auth.POST("/refresh", deps.Auth.Refresh)
			auth.POST("/logout", httpmw.Authn(deps.TokenManager), deps.Auth.Logout)
		}

		users := v1.Group("/users")
		{
			// Public: register
			users.POST("", deps.User.Create)
			// Protected
			users.Use(httpmw.Authn(deps.TokenManager))
			users.GET("/me", deps.User.GetMe)
			users.PATCH("/:id", deps.User.Update)
			users.DELETE("/:id",
				httpmw.Authz(deps.AuthzPolicy, "ADMIN"),
				deps.User.Delete,
			)
		}
	}

	return r
}

// requestIDMiddleware attaches a UUID request ID to every request context and
// response header.
func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" {
			id = uuid.NewString()
		}
		c.Set("request_id", id)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

// loggerMiddleware logs method, path, status, and latency via slog.
func loggerMiddleware(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		log.Info("http",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"latency_ms", time.Since(start).Milliseconds(),
			"request_id", c.GetString("request_id"),
		)
	}
}

// corsMiddleware sets permissive CORS headers (tighten in production).
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
