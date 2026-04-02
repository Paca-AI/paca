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
	jwttoken "github.com/paca/api/internal/platform/token"
	"github.com/paca/api/internal/transport/http/handler"
	httpmw "github.com/paca/api/internal/transport/http/middleware"
)

// Deps holds all handler and middleware dependencies.
type Deps struct {
	TokenManager *jwttoken.Manager
	Authorizer   *authz.Authorizer
	Health       *handler.HealthHandler
	Auth         *handler.AuthHandler
	User         *handler.UserHandler
	GlobalRole   *handler.GlobalRoleHandler
	Project      *handler.ProjectHandler
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

	api := r.Group("/api")

	// Public routes
	api.GET("/healthz", deps.Health.Check)

	v1 := api.Group("/v1")
	{
		auth := v1.Group("/auth")
		{
			auth.POST("/login", deps.Auth.Login)
			auth.POST("/refresh", deps.Auth.Refresh)
			auth.POST("/logout", httpmw.Authn(deps.TokenManager), deps.Auth.Logout)
		}

		users := v1.Group("/users")
		users.Use(httpmw.Authn(deps.TokenManager))
		{
			// Password change is allowed even when MustChangePassword=true so
			// that users can fulfil the forced-change requirement.
			users.PATCH("/me/password", deps.User.ChangeMyPassword)

			// All other self-service routes require a fresh (non-forced) password.
			me := users.Group("")
			me.Use(httpmw.RequireFreshPassword())
			{
				me.GET("/me", deps.User.GetMe)
				me.PATCH("/me", deps.User.UpdateMe)
				me.GET("/me/global-permissions", deps.User.GetMyGlobalPermissions)
			}
		}

		admin := v1.Group("/admin")
		admin.Use(httpmw.Authn(deps.TokenManager))
		admin.Use(httpmw.RequireFreshPassword())
		{
			// User management — requires users.* permissions
			admin.GET("/users",
				httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionUsersRead),
				deps.User.ListUsers,
			)
			admin.POST("/users",
				httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionUsersWrite),
				deps.User.CreateUser,
			)
			admin.GET("/users/:userId",
				httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionUsersRead),
				deps.User.GetUserByID,
			)
			admin.PATCH("/users/:userId",
				httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionUsersWrite),
				deps.User.AdminUpdateUser,
			)
			admin.PATCH("/users/:userId/password",
				httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionUsersWrite),
				deps.User.ResetPassword,
			)
			admin.DELETE("/users/:userId",
				httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionUsersDelete),
				deps.User.DeleteUser,
			)

			// Global role management — requires global_roles.* permissions
			admin.GET("/global-roles",
				httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionGlobalRolesRead),
				deps.GlobalRole.List,
			)
			admin.POST("/global-roles",
				httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionGlobalRolesWrite),
				deps.GlobalRole.Create,
			)
			admin.PATCH("/global-roles/:roleId",
				httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionGlobalRolesWrite),
				deps.GlobalRole.Update,
			)
			admin.DELETE("/global-roles/:roleId",
				httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionGlobalRolesWrite),
				deps.GlobalRole.Delete,
			)
			admin.PUT("/users/:userId/global-roles",
				httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionGlobalRolesAssign),
				deps.GlobalRole.ReplaceUserRoles,
			)

			// Project management — requires projects.* permissions
			admin.GET("/projects",
				httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionProjectsRead),
				deps.Project.ListProjects,
			)
			admin.POST("/projects",
				httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionProjectsWrite),
				deps.Project.CreateProject,
			)
			admin.GET("/projects/:projectId",
				httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionProjectsRead),
				deps.Project.GetProject,
			)
			admin.PATCH("/projects/:projectId",
				httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionProjectsWrite),
				deps.Project.UpdateProject,
			)
			admin.DELETE("/projects/:projectId",
				httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionProjectsWrite),
				deps.Project.DeleteProject,
			)
		}

		// Project-scoped routes — accessible to project members with appropriate roles.
		projects := v1.Group("/projects/:projectId")
		projects.Use(httpmw.Authn(deps.TokenManager))
		projects.Use(httpmw.RequireFreshPassword())
		{
			members := projects.Group("/members")
			{
				members.GET("",
					httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionProjectMembersRead),
					deps.Project.ListMembers,
				)
				members.POST("",
					httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionProjectMembersWrite),
					deps.Project.AddMember,
				)
				members.DELETE("/:userId",
					httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionProjectMembersWrite),
					deps.Project.RemoveMember,
				)
			}

			roles := projects.Group("/roles")
			{
				roles.GET("",
					httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionProjectRolesRead),
					deps.Project.ListRoles,
				)
				roles.POST("",
					httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionProjectRolesWrite),
					deps.Project.CreateRole,
				)
				roles.PATCH("/:roleId",
					httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionProjectRolesWrite),
					deps.Project.UpdateRole,
				)
				roles.DELETE("/:roleId",
					httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionProjectRolesWrite),
					deps.Project.DeleteRole,
				)
			}
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
