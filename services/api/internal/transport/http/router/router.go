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
	Task         *handler.TaskHandler
	Sprint       *handler.SprintHandler
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
		}

		// Project routes — list/create are collection-level; all other actions are
		// project-scoped and accessible to members with appropriate roles.
		projects := v1.Group("/projects")
		projects.Use(httpmw.Authn(deps.TokenManager))
		projects.Use(httpmw.RequireFreshPassword())
		{
			// Collection routes
			// ListProjects self-selects: global projects.read → all projects,
			// otherwise → only the caller's accessible projects.
			projects.GET("", deps.Project.ListProjects)
			projects.POST("",
				httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionProjectsCreate),
				deps.Project.CreateProject,
			)

			// Single-project routes
			project := projects.Group("/:projectId")
			{
				project.GET("",
					// Allow: global projects.read OR project-scoped projects.read
					// (any project member role with projects.read can view the project).
					httpmw.RequireAnyPermissions(deps.Authorizer,
						httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
						httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
					),
					deps.Project.GetProject,
				)
				project.PATCH("",
					httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionProjectsWrite),
					deps.Project.UpdateProject,
				)
				project.DELETE("",
					httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionProjectsDelete),
					deps.Project.DeleteProject,
				)

				members := project.Group("/members")
				{
					members.GET("",
						// Allow: global projects.read (view-all grant) OR project-scoped members.read.
						httpmw.RequireAnyPermissions(deps.Authorizer,
							httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
							httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionProjectMembersRead}},
						),
						deps.Project.ListMembers,
					)
					members.POST("",
						httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionProjectMembersWrite),
						deps.Project.AddMember,
					)
					members.PATCH("/:userId",
						httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionProjectMembersWrite),
						deps.Project.UpdateMemberRole,
					)
					members.DELETE("/:userId",
						httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionProjectMembersWrite),
						deps.Project.RemoveMember,
					)
				}

				roles := project.Group("/roles")
				{
					roles.GET("",
						// Allow: global projects.read (view-all grant) OR project-scoped roles.read.
						httpmw.RequireAnyPermissions(deps.Authorizer,
							httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
							httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionProjectRolesRead}},
						),
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

				// Task types — project-scoped configuration
				taskTypes := project.Group("/task-types")
				{
					taskTypes.GET("",
						httpmw.RequireAnyPermissions(deps.Authorizer,
							httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
							httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionTasksRead}},
						),
						deps.Task.ListTaskTypes,
					)
					taskTypes.POST("",
						httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite),
						deps.Task.CreateTaskType,
					)
					taskTypes.PATCH("/:typeId",
						httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite),
						deps.Task.UpdateTaskType,
					)
					taskTypes.DELETE("/:typeId",
						httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite),
						deps.Task.DeleteTaskType,
					)
				}

				// Task statuses — project-scoped workflow configuration
				taskStatuses := project.Group("/task-statuses")
				{
					taskStatuses.GET("",
						httpmw.RequireAnyPermissions(deps.Authorizer,
							httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
							httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionTasksRead}},
						),
						deps.Task.ListTaskStatuses,
					)
					taskStatuses.POST("",
						httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite),
						deps.Task.CreateTaskStatus,
					)
					taskStatuses.PATCH("/:statusId",
						httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite),
						deps.Task.UpdateTaskStatus,
					)
					taskStatuses.DELETE("/:statusId",
						httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite),
						deps.Task.DeleteTaskStatus,
					)
				}

				// Sprints
				sprints := project.Group("/sprints")
				{
					sprints.GET("",
						httpmw.RequireAnyPermissions(deps.Authorizer,
							httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
							httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionSprintsRead}},
						),
						deps.Sprint.ListSprints,
					)
					sprints.POST("",
						httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionSprintsWrite),
						deps.Sprint.CreateSprint,
					)
					sprints.GET("/:sprintId",
						httpmw.RequireAnyPermissions(deps.Authorizer,
							httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
							httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionSprintsRead}},
						),
						deps.Sprint.GetSprint,
					)
					sprints.PATCH("/:sprintId",
						httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionSprintsWrite),
						deps.Sprint.UpdateSprint,
					)
					sprints.DELETE("/:sprintId",
						httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionSprintsWrite),
						deps.Sprint.DeleteSprint,
					)
					// Sprint tasks view
					sprints.GET("/:sprintId/tasks",
						httpmw.RequireAnyPermissions(deps.Authorizer,
							httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
							httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionTasksRead}},
						),
						deps.Task.GetSprintTasks,
					)
				}

				// Product backlog — tasks not assigned to any sprint
				project.GET("/product-backlog",
					httpmw.RequireAnyPermissions(deps.Authorizer,
						httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
						httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionTasksRead}},
					),
					deps.Task.ListBacklogTasks,
				)

				// Tasks — core work items
				tasks := project.Group("/tasks")
				{
					tasks.GET("",
						httpmw.RequireAnyPermissions(deps.Authorizer,
							httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
							httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionTasksRead}},
						),
						deps.Task.ListTasks,
					)
					tasks.POST("",
						httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite),
						deps.Task.CreateTask,
					)
					tasks.GET("/:taskId",
						httpmw.RequireAnyPermissions(deps.Authorizer,
							httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
							httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionTasksRead}},
						),
						deps.Task.GetTask,
					)
					tasks.PATCH("/:taskId",
						httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite),
						deps.Task.UpdateTask,
					)
					tasks.DELETE("/:taskId",
						httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite),
						deps.Task.DeleteTask,
					)
				}
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
