// Package router wires global middleware and all route groups onto a chi.Router.
package router

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/Paca-AI/api/internal/platform/authz"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	"github.com/Paca-AI/api/internal/transport/http/httpx"
	httpmw "github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

// Deps holds all handler and middleware dependencies.
type Deps struct {
	TokenManager         *jwttoken.Manager
	APIKeyAuth           httpmw.APIKeyAuthenticator
	Authorizer           *authz.Authorizer
	ProjectVisibilitySvc httpmw.ProjectVisibilityChecker
	Health               *handler.HealthHandler
	Auth                 *handler.AuthHandler
	User                 *handler.UserHandler
	GlobalRole           *handler.GlobalRoleHandler
	Project              *handler.ProjectHandler
	Task                 *handler.TaskHandler
	Sprint               *handler.SprintHandler
	View                 *handler.ViewHandler
	Attachment           *handler.AttachmentHandler
	Document             *handler.DocumentHandler
	DocFile              *handler.DocFileHandler
	Notification         *handler.NotificationHandler
	APIKey               *handler.APIKeyHandler
	Plugin               *handler.PluginHandler
	Agent                *handler.AgentHandler
	Conversation         *handler.ConversationHandler
	Workflow             *handler.WorkflowHandler
	Log                  *slog.Logger
}

// New builds and returns a configured http.Handler.
func New(deps Deps) http.Handler {
	r := chi.NewRouter()

	// Global middleware
	r.Use(requestIDMiddleware())
	r.Use(loggerMiddleware(deps.Log))
	r.Use(chimw.Recoverer)
	r.Use(corsMiddleware())

	r.Route("/api", func(r chi.Router) {
		// Public routes
		r.Get("/healthz", deps.Health.Check)

		r.Route("/v1", func(r chi.Router) {
			// Auth
			r.Route("/auth", func(r chi.Router) {
				r.Post("/login", deps.Auth.Login)
				r.Post("/refresh", deps.Auth.Refresh)
				r.With(httpmw.Authn(deps.TokenManager)).Post("/logout", deps.Auth.Logout)
			})

			// Users
			r.Route("/users", func(r chi.Router) {
				r.Use(httpmw.Authn(deps.TokenManager, deps.APIKeyAuth))
				// Password change allowed even with MustChangePassword=true.
				r.With(httpmw.RequireJWTAuth()).Patch("/me/password", deps.User.ChangeMyPassword)

				// All other self-service routes require a fresh password.
				r.Group(func(r chi.Router) {
					r.Use(httpmw.RequireFreshPassword())
					r.Get("/me", deps.User.GetMe)
					r.Patch("/me", deps.User.UpdateMe)
					r.Get("/me/global-permissions", deps.User.GetMyGlobalPermissions)

					// API key management — JWT/cookie auth only.
					if deps.APIKey != nil {
						r.Group(func(r chi.Router) {
							r.Use(httpmw.RequireJWTAuth())
							r.Get("/me/api-keys", deps.APIKey.List)
							r.Post("/me/api-keys", deps.APIKey.Create)
							r.Delete("/me/api-keys/{keyId}", deps.APIKey.Revoke)
						})
					}

					// Notification routes
					if deps.Notification != nil {
						r.Get("/me/notifications", deps.Notification.List)
						r.Patch("/me/notifications/{notificationId}/read", deps.Notification.MarkAsRead)
						r.Post("/me/notifications/read-all", deps.Notification.MarkAllAsRead)
					}
				})
			})

			// Admin
			r.Route("/admin", func(r chi.Router) {
				r.Use(httpmw.Authn(deps.TokenManager, deps.APIKeyAuth))
				r.Use(httpmw.RequireFreshPassword())

				// User management
				r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionUsersRead)).
					Get("/users", deps.User.ListUsers)
				r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionUsersWrite)).
					Post("/users", deps.User.CreateUser)
				r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionUsersRead)).
					Get("/users/{userId}", deps.User.GetUserByID)
				r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionUsersWrite)).
					Patch("/users/{userId}", deps.User.AdminUpdateUser)
				r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionUsersWrite)).
					Patch("/users/{userId}/password", deps.User.ResetPassword)
				r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionUsersDelete)).
					Delete("/users/{userId}", deps.User.DeleteUser)

				// Global role management
				r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionGlobalRolesRead)).
					Get("/global-roles", deps.GlobalRole.List)
				r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionGlobalRolesWrite)).
					Post("/global-roles", deps.GlobalRole.Create)
				r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionGlobalRolesWrite)).
					Patch("/global-roles/{roleId}", deps.GlobalRole.Update)
				r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionGlobalRolesWrite)).
					Delete("/global-roles/{roleId}", deps.GlobalRole.Delete)
				r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionGlobalRolesAssign)).
					Put("/users/{userId}/global-roles", deps.GlobalRole.ReplaceUserRoles)
			})

			// Projects — collection routes
			r.Route("/projects", func(r chi.Router) {
				r.Use(httpmw.Authn(deps.TokenManager, deps.APIKeyAuth))
				r.Use(httpmw.RequireFreshPassword())
				r.Get("/", deps.Project.ListProjects)
				r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionProjectsCreate)).
					Post("/", deps.Project.CreateProject)
			})

			// LLM models — accessible to any authenticated user
			if deps.Agent != nil {
				r.Route("/agents", func(r chi.Router) {
					r.Use(httpmw.Authn(deps.TokenManager, deps.APIKeyAuth))
					r.Use(httpmw.RequireFreshPassword())
					r.Get("/llm-models", deps.Agent.GetLLMModels)
					r.Get("/skill-templates", deps.Agent.ListSkillTemplates)
				})
			}

			// Single-project routes — optional auth for public project support
			r.Route("/projects/{projectId}", func(r chi.Router) {
				r.Use(httpmw.OptionalAuthn(deps.TokenManager, deps.APIKeyAuth))
				r.Use(httpmw.RequireFreshPassword())

				r.With(httpmw.RequirePublicProjectOrPermissions(deps.ProjectVisibilitySvc, deps.Authorizer,
					httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
					httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
				)).Get("/", deps.Project.GetProject)
				r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionProjectsWrite)).
					Patch("/", deps.Project.UpdateProject)
				r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionProjectsDelete)).
					Delete("/", deps.Project.DeleteProject)

				// Members
				r.Route("/members", func(r chi.Router) {
					r.With(httpmw.RequirePublicProjectOrPermissions(deps.ProjectVisibilitySvc, deps.Authorizer,
						httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
						httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionProjectMembersRead}},
					)).Get("/", deps.Project.ListMembers)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionProjectMembersWrite)).
						Post("/", deps.Project.AddMember)
					r.Get("/me/permissions", deps.Project.GetMyProjectPermissions)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionProjectMembersWrite)).
						Patch("/{memberId}", deps.Project.UpdateMemberRole)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionProjectMembersWrite)).
						Delete("/{memberId}", deps.Project.RemoveMember)
				})

				// Roles
				r.Route("/roles", func(r chi.Router) {
					r.With(httpmw.RequirePublicProjectOrPermissions(deps.ProjectVisibilitySvc, deps.Authorizer,
						httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
						httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionProjectRolesRead}},
					)).Get("/", deps.Project.ListRoles)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionProjectRolesWrite)).
						Post("/", deps.Project.CreateRole)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionProjectRolesWrite)).
						Patch("/{roleId}", deps.Project.UpdateRole)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionProjectRolesWrite)).
						Delete("/{roleId}", deps.Project.DeleteRole)
				})

				// Task types
				r.Route("/task-types", func(r chi.Router) {
					r.With(httpmw.RequirePublicProjectOrPermissions(deps.ProjectVisibilitySvc, deps.Authorizer,
						httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
						httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionTasksRead}},
					)).Get("/", deps.Task.ListTaskTypes)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite)).
						Post("/", deps.Task.CreateTaskType)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite)).
						Patch("/{typeId}", deps.Task.UpdateTaskType)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite)).
						Delete("/{typeId}", deps.Task.DeleteTaskType)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite)).
						Put("/{typeId}/set-default", deps.Task.SetDefaultTaskType)
				})

				// Task statuses
				r.Route("/task-statuses", func(r chi.Router) {
					r.With(httpmw.RequirePublicProjectOrPermissions(deps.ProjectVisibilitySvc, deps.Authorizer,
						httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
						httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionTasksRead}},
					)).Get("/", deps.Task.ListTaskStatuses)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite)).
						Post("/", deps.Task.CreateTaskStatus)
					// Static /positions must be registered before /{statusId}.
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite)).
						Put("/positions", deps.Task.ReorderTaskStatuses)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite)).
						Patch("/{statusId}", deps.Task.UpdateTaskStatus)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite)).
						Delete("/{statusId}", deps.Task.DeleteTaskStatus)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite)).
						Put("/{statusId}/set-default", deps.Task.SetDefaultTaskStatus)
				})

				// Automation workflows
				if deps.Workflow != nil {
					r.Route("/workflows", func(r chi.Router) {
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsRead)).
							Get("/", deps.Workflow.ListWorkflows)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsWrite)).
							Post("/", deps.Workflow.CreateWorkflow)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsRead)).
							Get("/{workflowId}", deps.Workflow.GetWorkflow)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsWrite)).
							Patch("/{workflowId}", deps.Workflow.UpdateWorkflow)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsWrite)).
							Delete("/{workflowId}", deps.Workflow.DeleteWorkflow)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsWrite)).
							Post("/{workflowId}/activate", deps.Workflow.ActivateWorkflow)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsWrite)).
							Post("/{workflowId}/archive", deps.Workflow.ArchiveWorkflow)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsWrite)).
							Post("/{workflowId}/revert-to-draft", deps.Workflow.RevertWorkflowToDraft)

						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsWrite)).
							Post("/{workflowId}/nodes", deps.Workflow.AddWorkflowNode)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsWrite)).
							Patch("/{workflowId}/nodes/{nodeId}", deps.Workflow.UpdateWorkflowNode)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsWrite)).
							Delete("/{workflowId}/nodes/{nodeId}", deps.Workflow.RemoveWorkflowNode)

						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsWrite)).
							Post("/{workflowId}/status-rules", deps.Workflow.SetWorkflowStatusRule)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsWrite)).
							Delete("/{workflowId}/status-rules/{ruleId}", deps.Workflow.RemoveWorkflowStatusRule)

						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsWrite)).
							Post("/{workflowId}/status-transitions", deps.Workflow.SetWorkflowStatusTransition)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsWrite)).
							Delete("/{workflowId}/status-transitions/{transitionId}", deps.Workflow.RemoveWorkflowStatusTransition)

						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsWrite)).
							Post("/{workflowId}/edges", deps.Workflow.AddWorkflowEdge)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsWrite)).
							Delete("/{workflowId}/edges/{edgeId}", deps.Workflow.RemoveWorkflowEdge)
					})
				}

				// Sprints
				r.Route("/sprints", func(r chi.Router) {
					r.With(httpmw.RequirePublicProjectOrPermissions(deps.ProjectVisibilitySvc, deps.Authorizer,
						httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
						httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionSprintsRead}},
					)).Get("/", deps.Sprint.ListSprints)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionSprintsWrite)).
						Post("/", deps.Sprint.CreateSprint)
					r.With(httpmw.RequirePublicProjectOrPermissions(deps.ProjectVisibilitySvc, deps.Authorizer,
						httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
						httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionSprintsRead}},
					)).Get("/{sprintId}", deps.Sprint.GetSprint)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionSprintsWrite)).
						Patch("/{sprintId}", deps.Sprint.UpdateSprint)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionSprintsWrite)).
						Delete("/{sprintId}", deps.Sprint.DeleteSprint)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionSprintsWrite)).
						Post("/{sprintId}/complete", deps.Sprint.CompleteSprint)
				})

				// Views
				r.Route("/views", func(r chi.Router) {
					r.With(httpmw.RequirePublicProjectOrPermissions(deps.ProjectVisibilitySvc, deps.Authorizer,
						httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
						httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionSprintsRead}},
					)).Get("/", deps.View.ListViews)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionSprintsWrite)).
						Post("/", deps.View.CreateView)
					// Static /positions must be registered before /{viewId}.
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionSprintsWrite)).
						Put("/positions", deps.View.ReorderViews)
					r.With(httpmw.RequirePublicProjectOrPermissions(deps.ProjectVisibilitySvc, deps.Authorizer,
						httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
						httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionSprintsRead}},
					)).Get("/{viewId}", deps.View.GetView)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionSprintsWrite)).
						Patch("/{viewId}", deps.View.UpdateView)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionSprintsWrite)).
						Delete("/{viewId}", deps.View.DeleteView)
					r.With(httpmw.RequirePublicProjectOrPermissions(deps.ProjectVisibilitySvc, deps.Authorizer,
						httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
						httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionTasksRead}},
					)).Get("/{viewId}/task-positions", deps.View.ListTaskPositions)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite)).
						Put("/{viewId}/task-positions", deps.View.BulkMoveTasks)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite)).
						Put("/{viewId}/task-positions/{taskId}", deps.View.MoveTask)
				})

				// Tasks
				r.Route("/tasks", func(r chi.Router) {
					r.With(httpmw.RequirePublicProjectOrPermissions(deps.ProjectVisibilitySvc, deps.Authorizer,
						httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
						httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionTasksRead}},
					)).Get("/", deps.Task.ListTasks)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite)).
						Post("/", deps.Task.CreateTask)
					r.With(httpmw.RequirePublicProjectOrPermissions(deps.ProjectVisibilitySvc, deps.Authorizer,
						httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
						httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionTasksRead}},
					)).Get("/by-number/{taskNumber}", deps.Task.GetTaskByNumber)
					r.With(httpmw.RequirePublicProjectOrPermissions(deps.ProjectVisibilitySvc, deps.Authorizer,
						httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
						httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionTasksRead}},
					)).Get("/{taskId}", deps.Task.GetTask)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite)).
						Patch("/{taskId}", deps.Task.UpdateTask)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite)).
						Delete("/{taskId}", deps.Task.DeleteTask)

					if deps.Agent != nil {
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite)).
							Post("/{taskId}/write-with-ai", deps.Agent.WriteTaskDescriptionWithAI)
					}

					// Activities
					r.Route("/{taskId}/activities", func(r chi.Router) {
						r.With(httpmw.RequirePublicProjectOrPermissions(deps.ProjectVisibilitySvc, deps.Authorizer,
							httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
							httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionTasksRead}},
						)).Get("/", deps.Task.ListTaskActivities)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite)).
							Post("/comments", deps.Task.AddComment)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite)).
							Patch("/comments/{commentId}", deps.Task.UpdateComment)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite)).
							Delete("/comments/{commentId}", deps.Task.DeleteComment)
					})

					// Links
					r.Route("/{taskId}/links", func(r chi.Router) {
						r.With(httpmw.RequirePublicProjectOrPermissions(deps.ProjectVisibilitySvc, deps.Authorizer,
							httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
							httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionTasksRead}},
						)).Get("/", deps.Task.ListTaskLinks)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite)).
							Post("/", deps.Task.CreateTaskLink)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite)).
							Delete("/{linkId}", deps.Task.DeleteTaskLink)
					})

					// Workflows this task belongs to (read-only, for the task detail view)
					if deps.Workflow != nil {
						r.With(httpmw.RequirePublicProjectOrPermissions(deps.ProjectVisibilitySvc, deps.Authorizer,
							httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
							httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionWorkflowsRead}},
						)).Get("/{taskId}/workflows", deps.Workflow.ListWorkflowsForTask)
					}

					// Attachments
					r.Route("/{taskId}/attachments", func(r chi.Router) {
						r.With(httpmw.RequirePublicProjectOrPermissions(deps.ProjectVisibilitySvc, deps.Authorizer,
							httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
							httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionTasksRead}},
						)).Get("/", deps.Attachment.ListTaskAttachments)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite)).
							Post("/initiate-upload", deps.Attachment.InitiateUpload)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite)).
							Post("/complete-upload", deps.Attachment.CompleteUpload)
						r.With(httpmw.RequirePublicProjectOrPermissions(deps.ProjectVisibilitySvc, deps.Authorizer,
							httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
							httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionTasksRead}},
						)).Get("/{attachmentId}/download-url", deps.Attachment.GetDownloadURL)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite)).
							Delete("/{attachmentId}", deps.Attachment.DeleteTaskAttachment)
					})
				})

				// Custom field definitions
				r.Route("/custom-fields", func(r chi.Router) {
					r.With(httpmw.RequirePublicProjectOrPermissions(deps.ProjectVisibilitySvc, deps.Authorizer,
						httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
						httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionTasksRead}},
					)).Get("/", deps.Task.ListCustomFieldDefinitions)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite)).
						Post("/", deps.Task.CreateCustomFieldDefinition)
					r.With(httpmw.RequirePublicProjectOrPermissions(deps.ProjectVisibilitySvc, deps.Authorizer,
						httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
						httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionTasksRead}},
					)).Get("/{fieldId}", deps.Task.GetCustomFieldDefinition)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite)).
						Patch("/{fieldId}", deps.Task.UpdateCustomFieldDefinition)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite)).
						Delete("/{fieldId}", deps.Task.DeleteCustomFieldDefinition)
				})

				// Documentation
				r.Route("/docs", func(r chi.Router) {
					// Folders
					r.Route("/folders", func(r chi.Router) {
						r.With(httpmw.RequirePublicProjectOrPermissions(deps.ProjectVisibilitySvc, deps.Authorizer,
							httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
							httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionDocsRead}},
						)).Get("/", deps.Document.ListFolders)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionDocsWrite)).
							Post("/", deps.Document.CreateFolder)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionDocsWrite)).
							Patch("/{folderId}", deps.Document.UpdateFolder)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionDocsWrite)).
							Delete("/{folderId}", deps.Document.DeleteFolder)
					})

					// Documents — collection
					r.With(httpmw.RequirePublicProjectOrPermissions(deps.ProjectVisibilitySvc, deps.Authorizer,
						httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
						httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionDocsRead}},
					)).Get("/", deps.Document.ListDocuments)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionDocsWrite)).
						Post("/", deps.Document.CreateDocument)

					// Documents — single item
					r.Route("/{docId}", func(r chi.Router) {
						r.With(httpmw.RequirePublicProjectOrPermissions(deps.ProjectVisibilitySvc, deps.Authorizer,
							httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
							httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionDocsRead}},
						)).Get("/", deps.Document.GetDocument)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionDocsWrite)).
							Patch("/", deps.Document.UpdateDocument)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionDocsWrite)).
							Delete("/", deps.Document.DeleteDocument)

						// Snapshots
						r.Route("/snapshots", func(r chi.Router) {
							r.With(httpmw.RequirePublicProjectOrPermissions(deps.ProjectVisibilitySvc, deps.Authorizer,
								httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
								httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionDocsRead}},
							)).Get("/", deps.Document.ListSnapshots)
							r.With(httpmw.RequirePublicProjectOrPermissions(deps.ProjectVisibilitySvc, deps.Authorizer,
								httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
								httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionDocsRead}},
							)).Get("/{snapshotId}", deps.Document.GetSnapshot)
						})

						// Activity log
						r.With(httpmw.RequirePublicProjectOrPermissions(deps.ProjectVisibilitySvc, deps.Authorizer,
							httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
							httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionDocsRead}},
						)).Get("/activities", deps.Document.ListActivities)

						// Comments
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionDocsWrite)).
							Post("/comments", deps.Document.AddComment)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionDocsWrite)).
							Patch("/comments/{commentId}", deps.Document.UpdateComment)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionDocsWrite)).
							Delete("/comments/{commentId}", deps.Document.DeleteComment)

						// Doc file uploads
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionDocsWrite)).
							Post("/files/initiate-upload", deps.DocFile.InitiateDocUpload)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionDocsWrite)).
							Post("/files/complete-upload", deps.DocFile.CompleteDocUpload)
						r.With(httpmw.RequirePublicProjectOrPermissions(deps.ProjectVisibilitySvc, deps.Authorizer,
							httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
							httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionDocsRead}},
						)).Get("/files/{fileId}/download-url", deps.DocFile.GetDocFileDownloadURL)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionDocsWrite)).
							Delete("/files/{fileId}", deps.DocFile.DeleteDocFile)
					})
				})

				// Agents
				if deps.Agent != nil {
					r.Route("/agents", func(r chi.Router) {
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsRead)).
							Get("/", deps.Agent.ListAgents)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsWrite)).
							Post("/", deps.Agent.CreateAgent)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsRead)).
							Get("/{agentId}", deps.Agent.GetAgent)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsWrite)).
							Patch("/{agentId}", deps.Agent.UpdateAgent)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsWrite)).
							Delete("/{agentId}", deps.Agent.DeleteAgent)

						// ACP local bridge
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsWrite)).
							Post("/{agentId}/acp-bridge-token", deps.Agent.GenerateACPBridgeToken)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsRead)).
							Get("/{agentId}/acp-bridge-status", deps.Agent.GetACPBridgeStatus)

						// MCP servers
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsRead)).
							Get("/{agentId}/mcp-servers", deps.Agent.ListMCPServers)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsWrite)).
							Post("/{agentId}/mcp-servers", deps.Agent.AddMCPServer)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsWrite)).
							Patch("/{agentId}/mcp-servers/{serverId}", deps.Agent.UpdateMCPServer)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsWrite)).
							Delete("/{agentId}/mcp-servers/{serverId}", deps.Agent.DeleteMCPServer)

						// Skills
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsRead)).
							Get("/{agentId}/skills", deps.Agent.ListSkills)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsWrite)).
							Post("/{agentId}/skills", deps.Agent.AddSkill)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsWrite)).
							Patch("/{agentId}/skills/{skillId}", deps.Agent.UpdateSkill)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsWrite)).
							Delete("/{agentId}/skills/{skillId}", deps.Agent.DeleteSkill)

						// Environment variables
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsRead)).
							Get("/{agentId}/env-vars", deps.Agent.ListEnvVars)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsWrite)).
							Post("/{agentId}/env-vars", deps.Agent.AddEnvVar)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsWrite)).
							Patch("/{agentId}/env-vars/{envVarId}", deps.Agent.UpdateEnvVar)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsWrite)).
							Delete("/{agentId}/env-vars/{envVarId}", deps.Agent.DeleteEnvVar)

						// Chat sessions
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsRead)).
							Get("/{agentId}/chat-sessions", deps.Agent.ListChatSessions)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsRead)).
							Post("/{agentId}/chat-sessions", deps.Agent.StartChatSession)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsRead)).
							Post("/{agentId}/chat-sessions/{sessionId}/messages", deps.Agent.SendChatMessage)
					})
				}

				// Conversations
				if deps.Conversation != nil {
					r.Route("/conversations", func(r chi.Router) {
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsRead)).
							Get("/", deps.Conversation.ListConversations)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsRead)).
							Get("/{conversationId}", deps.Conversation.GetConversation)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsRead)).
							Get("/{conversationId}/events", deps.Conversation.ListConversationEvents)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsWrite)).
							Post("/{conversationId}/stop", deps.Conversation.StopConversation)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsWrite)).
							Post("/{conversationId}/pause", deps.Conversation.PauseConversation)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsRead)).
							Post("/{conversationId}/heartbeat", deps.Conversation.Heartbeat)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsRead)).
							Post("/{conversationId}/messages", deps.Conversation.SendConversationMessage)
					})
				}
			})

			// Plugin routes
			if deps.Plugin != nil {
				// Public listing — optional auth
				r.Group(func(r chi.Router) {
					r.Use(httpmw.OptionalAuthn(deps.TokenManager, deps.APIKeyAuth))
					r.Use(httpmw.RequireFreshPassword())
					r.Get("/plugins", deps.Plugin.ListPlugins)
				})

				// Plugin proxy — no authentication enforced at router level;
				// per-route middleware policy is applied inside ProxyRequest.
				r.Handle("/plugins/{pluginId}/*", http.HandlerFunc(deps.Plugin.ProxyRequest))

				// Admin plugin management
				r.Route("/admin/plugins", func(r chi.Router) {
					r.Use(httpmw.Authn(deps.TokenManager, deps.APIKeyAuth))
					r.Use(httpmw.RequireFreshPassword())
					r.Use(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionUsersWrite))
					r.Get("/marketplace", deps.Plugin.ListMarketplacePlugins)
					r.Post("/marketplace/install", deps.Plugin.InstallMarketplacePlugin)
					r.Post("/", deps.Plugin.InstallPlugin)
					r.Patch("/{pluginId}", deps.Plugin.UpdatePlugin)
					r.Post("/{pluginId}/upgrade", deps.Plugin.UpgradeMarketplacePlugin)
					r.Delete("/{pluginId}", deps.Plugin.DeletePlugin)
				})

				// Admin extension settings
				r.Route("/admin/plugin-extension-settings", func(r chi.Router) {
					r.Use(httpmw.Authn(deps.TokenManager, deps.APIKeyAuth))
					r.Use(httpmw.RequireFreshPassword())
					r.Use(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionUsersWrite))
					r.Patch("/", deps.Plugin.UpdateExtensionSetting)
				})
			}
		})
	})

	return r
}

// statusRecorder wraps http.ResponseWriter to capture the response status code.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

// requestIDMiddleware attaches a UUID request ID to every request context and
// response header.
func requestIDMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-ID")
			if id == "" {
				id = uuid.NewString()
			}
			ctx := httpx.WithRequestID(r.Context(), id)
			w.Header().Set("X-Request-ID", id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// loggerMiddleware logs method, path, status, and latency via slog.
func loggerMiddleware(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sr := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sr, r)
			log.Info("http",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sr.status,
				"latency_ms", time.Since(start).Milliseconds(),
				"request_id", httpx.RequestIDFromContext(r.Context()),
			)
		})
	}
}

// corsMiddleware sets permissive CORS headers (tighten in production).
func corsMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
