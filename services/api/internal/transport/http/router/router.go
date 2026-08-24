// Package router wires global middleware and all route groups onto a chi.Router.
package router

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/platform/authz"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	"github.com/Paca-AI/api/internal/transport/http/httpx"
	httpmw "github.com/Paca-AI/api/internal/transport/http/middleware"
)

// Deps holds all handler and middleware dependencies.
type Deps struct {
	TokenManager         *jwttoken.Manager
	APIKeyAuth           httpmw.APIKeyAuthenticator
	Authorizer           *authz.Authorizer
	ProjectVisibilitySvc httpmw.ProjectVisibilityChecker
	Health               *handler.HealthHandler
	Version              *handler.VersionHandler
	Auth                 *handler.AuthHandler
	// OIDC is backed by the runtime manager and remains registered while SSO
	// is disabled so an administrator can activate it without a restart.
	OIDC         *handler.OIDCHandler
	SSOSettings  *handler.SSOSettingsHandler
	User         *handler.UserHandler
	GlobalRole   *handler.GlobalRoleHandler
	Project      *handler.ProjectHandler
	Task         *handler.TaskHandler
	Sprint       *handler.SprintHandler
	View         *handler.ViewHandler
	Attachment   *handler.AttachmentHandler
	Document     *handler.DocumentHandler
	DocFile      *handler.DocFileHandler
	Notification *handler.NotificationHandler
	APIKey       *handler.APIKeyHandler
	Plugin       *handler.PluginHandler
	Skills       *handler.SkillsHandler
	Agent        *handler.AgentHandler
	Conversation *handler.ConversationHandler
	Automation   *handler.AutomationHandler
	Settings     *handler.SettingsHandler
	Log          *slog.Logger
	// CORSAllowedOrigins is the CORS allow-list — see corsMiddleware. A nil
	// or empty slice (the zero value, so every existing caller of this
	// struct literal keeps working unchanged) is treated the same as ["*"]:
	// reflect Access-Control-Allow-Origin: * for every request.
	CORSAllowedOrigins []string
}

// New builds and returns a configured http.Handler.
func New(deps Deps) http.Handler {
	r := chi.NewRouter()

	// Global middleware
	r.Use(requestIDMiddleware())
	r.Use(loggerMiddleware(deps.Log))
	r.Use(chimw.Recoverer)
	r.Use(corsMiddleware(deps.CORSAllowedOrigins))

	r.Route("/api", func(r chi.Router) {
		// Public routes
		r.Get("/healthz", deps.Health.Check)

		r.Route("/v1", func(r chi.Router) {
			// Version / update check — public, no auth required.
			if deps.Version != nil {
				r.Get("/version", deps.Version.Check)
				r.Get("/releases", deps.Version.ListReleases)
			}

			// Workspace branding — public, no auth required. Read pre-login
			// (login page) and on every page load, so it can't sit behind
			// the Authn middleware the way /admin/settings' writes do below.
			if deps.Settings != nil {
				r.Get("/branding", deps.Settings.GetBranding)
			}

			// Auth
			r.Route("/auth", func(r chi.Router) {
				r.Post("/login", deps.Auth.Login)
				r.Post("/refresh", deps.Auth.Refresh)
				r.With(httpmw.Authn(deps.TokenManager)).Post("/logout", deps.Auth.Logout)
				// Public — the link a password-set-token email points to;
				// the token itself (not a session) proves the caller's right
				// to act on the account.
				r.Post("/password/set", deps.User.SetPassword)

				// Public — login entry-point discovery for the login page
				// (which login methods exist; display data only).
				r.Get("/config", deps.Auth.Config)

				// Public — OIDC SSO browser endpoints. The whole flow runs
				// server-side; the SPA only navigates to /oidc/login.
				if deps.OIDC != nil {
					r.Get("/oidc/login", deps.OIDC.Login)
					r.Get("/oidc/callback", deps.OIDC.Callback)
				}
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
					r.Post("/me/avatar/initiate-upload", deps.User.InitiateAvatarUpload)
					r.Post("/me/avatar/complete-upload", deps.User.CompleteAvatarUpload)
					r.Delete("/me/avatar", deps.User.DeleteAvatar)

					// Cross-project "assigned to me" tasks — home page widget.
					if deps.Task != nil {
						r.Get("/me/tasks", deps.Task.ListAssignedToMe)
					}

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

				// Global agent management — CRUD for AgentScopeGlobal agents,
				// mirroring the user/global-role shape above. Global agents are
				// not tied to any project; they're attached to one later via the
				// same "invite a member" flow used for humans (POST
				// /projects/{projectId}/members with agent_id set — see below).
				if deps.Agent != nil {
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionAgentsRead)).
						Get("/agents", deps.Agent.ListGlobalAgents)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionAgentsWrite)).
						Post("/agents", deps.Agent.CreateGlobalAgent)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionAgentsRead)).
						Get("/agents/{agentId}", deps.Agent.GetGlobalAgent)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionAgentsWrite)).
						Patch("/agents/{agentId}", deps.Agent.UpdateGlobalAgent)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionAgentsWrite)).
						Delete("/agents/{agentId}", deps.Agent.DeleteGlobalAgent)

					// ACP local bridge
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionAgentsWrite)).
						Post("/agents/{agentId}/acp-bridge-token", deps.Agent.GenerateGlobalACPBridgeToken)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionAgentsRead)).
						Get("/agents/{agentId}/acp-bridge-status", deps.Agent.GetGlobalACPBridgeStatus)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionAgentsWrite)).
						Post("/agents/{agentId}/mcp-agent-key", deps.Agent.GenerateGlobalAgentMCPKey)

					// Avatar
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionAgentsWrite)).
						Post("/agents/{agentId}/avatar/initiate-upload", deps.Agent.InitiateGlobalAvatarUpload)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionAgentsWrite)).
						Post("/agents/{agentId}/avatar/complete-upload", deps.Agent.CompleteGlobalAvatarUpload)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionAgentsWrite)).
						Delete("/agents/{agentId}/avatar", deps.Agent.DeleteGlobalAvatar)

					// MCP servers
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionAgentsRead)).
						Get("/agents/{agentId}/mcp-servers", deps.Agent.ListGlobalAgentMCPServers)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionAgentsWrite)).
						Post("/agents/{agentId}/mcp-servers", deps.Agent.AddGlobalAgentMCPServer)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionAgentsWrite)).
						Patch("/agents/{agentId}/mcp-servers/{serverId}", deps.Agent.UpdateGlobalAgentMCPServer)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionAgentsWrite)).
						Delete("/agents/{agentId}/mcp-servers/{serverId}", deps.Agent.DeleteGlobalAgentMCPServer)

					// Skills
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionAgentsRead)).
						Get("/agents/{agentId}/skills", deps.Agent.ListGlobalAgentSkills)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionAgentsWrite)).
						Post("/agents/{agentId}/skills", deps.Agent.AddGlobalAgentSkill)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionAgentsWrite)).
						Patch("/agents/{agentId}/skills/{skillId}", deps.Agent.UpdateGlobalAgentSkill)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionAgentsWrite)).
						Delete("/agents/{agentId}/skills/{skillId}", deps.Agent.DeleteGlobalAgentSkill)

					// Environment variables
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionAgentsRead)).
						Get("/agents/{agentId}/env-vars", deps.Agent.ListGlobalAgentEnvVars)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionAgentsWrite)).
						Post("/agents/{agentId}/env-vars", deps.Agent.AddGlobalAgentEnvVar)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionAgentsWrite)).
						Patch("/agents/{agentId}/env-vars/{envVarId}", deps.Agent.UpdateGlobalAgentEnvVar)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionAgentsWrite)).
						Delete("/agents/{agentId}/env-vars/{envVarId}", deps.Agent.DeleteGlobalAgentEnvVar)
				}

				// Workspace branding (logo/favicon/primary color) — a
				// singleton, so no {id} in the path. Sub-routed under
				// "/settings/logo" and "/settings/favicon" with an "/avatar/…"
				// suffix so the frontend can drive both through the same
				// generic avatar-upload client/component used for
				// users/agents/projects (which always POSTs/DELETEs to
				// "{basePath}/avatar/…").
				if deps.Settings != nil {
					write := httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionSettingsWrite)
					r.With(write).Patch("/settings", deps.Settings.UpdateSettings)
					r.With(write).Post("/settings/logo/avatar/initiate-upload", deps.Settings.InitiateLogoUpload)
					r.With(write).Post("/settings/logo/avatar/complete-upload", deps.Settings.CompleteLogoUpload)
					r.With(write).Delete("/settings/logo/avatar", deps.Settings.DeleteLogo)
					r.With(write).Post("/settings/favicon/avatar/initiate-upload", deps.Settings.InitiateFaviconUpload)
					r.With(write).Post("/settings/favicon/avatar/complete-upload", deps.Settings.CompleteFaviconUpload)
					r.With(write).Delete("/settings/favicon/avatar", deps.Settings.DeleteFavicon)
				}
				if deps.SSOSettings != nil {
					authenticationWrite := httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionAuthenticationWrite)
					r.With(httpmw.RequireJWTAuth(), authenticationWrite).Get("/settings/sso", deps.SSOSettings.Get)
					r.With(httpmw.RequireJWTAuth(), authenticationWrite).Patch("/settings/sso", deps.SSOSettings.Update)
				}
			})

			// Projects — collection routes.
			// Registered via r.Group (not r.Route) so these stay in the same
			// routing tree as the "/projects/{projectId}" mount below — chi
			// treats two separate Route()/Mount() calls sharing the "projects"
			// prefix as competing mounts, and the {projectId} one wins even for
			// paths like /projects/workspace-stats, shadowing the static route.
			r.Group(func(r chi.Router) {
				r.Use(httpmw.Authn(deps.TokenManager, deps.APIKeyAuth))
				r.Use(httpmw.RequireFreshPassword())
				r.Get("/projects", deps.Project.ListProjects)
				r.Get("/projects/workspace-stats", deps.Project.GetWorkspaceStats)
				r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.GlobalScope(), authz.PermissionProjectsCreate)).
					Post("/projects", deps.Project.CreateProject)
			})

			// LLM models, global agents, and global chat — accessible to any
			// authenticated user (human or agent-API-key). Static children
			// ("llm-models", "skill-templates", "me", "chat-sessions",
			// "conversations") are matched before the "{agentId}" wildcard
			// branch — chi, like any radix-tree router, always prefers a
			// literal path segment over a param at the same level, the same
			// way "/projects/workspace-stats" is disambiguated from
			// "/projects/{projectId}" elsewhere in this file.
			if deps.Agent != nil {
				r.Route("/agents", func(r chi.Router) {
					r.Use(httpmw.Authn(deps.TokenManager, deps.APIKeyAuth))
					r.Use(httpmw.RequireFreshPassword())
					r.Get("/llm-models", deps.Agent.GetLLMModels)
					r.Get("/skill-templates", deps.Agent.ListSkillTemplates)

					// Any authenticated human may browse global agents (to chat
					// with one) — unlike /admin/agents, this is intentionally
					// not permission-gated, matching how any project member can
					// already see a project's agents. It's the same handler as
					// the admin listing, just reachable without an admin
					// permission.
					r.Get("/", deps.Agent.ListGlobalAgents)

					// Agent self-service (agent-API-key authenticated via
					// X-Agent-ID) — what the MCP server calls when running as a
					// global agent to populate its own permission map.
					r.Get("/me/global-permissions", deps.Agent.GetMyGlobalPermissions)
					r.Get("/me/projects", deps.Agent.GetMyInvitedProjects)

					// Global chat — chatting with a global agent from the home
					// page / admin pages, no project context. Any authenticated
					// human may chat with any global agent, same as any project
					// member may chat with a project agent — deliberately not
					// gated behind PermissionAgentsRead (a regular USER-role
					// human has no global agents.* permission by default, and
					// this chat is meant to be available to every user).
					r.Get("/{agentId}/chat-sessions", deps.Agent.ListGlobalChatSessions)
					r.Post("/{agentId}/chat-sessions", deps.Agent.StartGlobalChatSession)
					r.Post("/chat-sessions/{sessionId}/messages", deps.Agent.SendGlobalChatMessage)

					if deps.Conversation != nil {
						r.Get("/conversations", deps.Conversation.ListGlobalConversations)
						r.Get("/conversations/{conversationId}", deps.Conversation.GetGlobalConversation)
						r.Get("/conversations/{conversationId}/events", deps.Conversation.GetGlobalConversationEvents)
						r.Post("/conversations/{conversationId}/stop", deps.Conversation.StopGlobalConversation)
						r.Post("/conversations/{conversationId}/pause", deps.Conversation.PauseGlobalConversation)
						r.Post("/conversations/{conversationId}/heartbeat", deps.Conversation.GlobalConversationHeartbeat)
						r.Post("/conversations/{conversationId}/messages", deps.Conversation.SendGlobalConversationMessage)
					}
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

				// Avatar
				r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionProjectsWrite)).
					Post("/avatar/initiate-upload", deps.Project.InitiateAvatarUpload)
				r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionProjectsWrite)).
					Post("/avatar/complete-upload", deps.Project.CompleteAvatarUpload)
				r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionProjectsWrite)).
					Delete("/avatar", deps.Project.DeleteAvatar)

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

				// Automation graph — reuses the workflows.read/write permission
				// keys (already seeded on every default/project role) rather
				// than introducing automations.* and a permission-backfill
				// migration.
				if deps.Automation != nil {
					r.Route("/automations", func(r chi.Router) {
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsRead)).
							Get("/", deps.Automation.ListAutomations)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsWrite)).
							Post("/", deps.Automation.CreateAutomation)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsRead)).
							Get("/{automationId}", deps.Automation.GetAutomation)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsWrite)).
							Patch("/{automationId}", deps.Automation.UpdateAutomation)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsWrite)).
							Delete("/{automationId}", deps.Automation.DeleteAutomation)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsWrite)).
							Post("/{automationId}/activate", deps.Automation.ActivateAutomation)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsWrite)).
							Post("/{automationId}/deactivate", deps.Automation.DeactivateAutomation)

						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsWrite)).
							Post("/{automationId}/nodes", deps.Automation.AddAutomationNode)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsWrite)).
							Patch("/{automationId}/nodes/{nodeId}", deps.Automation.UpdateAutomationNode)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsWrite)).
							Delete("/{automationId}/nodes/{nodeId}", deps.Automation.RemoveAutomationNode)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsWrite)).
							Post("/{automationId}/nodes/{nodeId}/webhook-token", deps.Automation.GenerateWebhookToken)

						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsWrite)).
							Post("/{automationId}/edges", deps.Automation.AddAutomationEdge)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsWrite)).
							Delete("/{automationId}/edges/{edgeId}", deps.Automation.RemoveAutomationEdge)

						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsRead)).
							Get("/{automationId}/runs", deps.Automation.ListAutomationRuns)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsRead)).
							Get("/{automationId}/runs/{runId}/steps", deps.Automation.ListAutomationRunSteps)
					})
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsRead)).
						Get("/automation-dependency-map", deps.Automation.GetAutomationDependencyMap)
					r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionWorkflowsRead)).
						Get("/automation-plugin-node-types", deps.Automation.ListPluginNodeTypes)
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
						r.With(httpmw.RequirePublicProjectOrPermissions(deps.ProjectVisibilitySvc, deps.Authorizer,
							httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
							httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionTasksRead}},
						)).Get("/{attachmentId}/content", deps.Attachment.GetAttachmentContent)
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
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsWrite)).
							Post("/{agentId}/mcp-agent-key", deps.Agent.GenerateAgentMCPKey)

						// Avatar
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsWrite)).
							Post("/{agentId}/avatar/initiate-upload", deps.Agent.InitiateAvatarUpload)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsWrite)).
							Post("/{agentId}/avatar/complete-upload", deps.Agent.CompleteAvatarUpload)
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsWrite)).
							Delete("/{agentId}/avatar", deps.Agent.DeleteAvatar)

						// Activity feed
						r.With(httpmw.RequirePermissions(deps.Authorizer, httpmw.ProjectScopeFromParam("projectId"), authz.PermissionAgentsRead)).
							Get("/{agentId}/activities", deps.Agent.ListAgentActivities)

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

			// Bundled skills — public listing, same policy as /plugins below.
			if deps.Skills != nil {
				r.Group(func(r chi.Router) {
					r.Use(httpmw.OptionalAuthn(deps.TokenManager, deps.APIKeyAuth))
					r.Use(httpmw.RequireFreshPassword())
					r.Get("/skills", deps.Skills.ListSkills)
				})
			}

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

			// Automation webhook receiver — registered outside the
			// authenticated /projects/{projectId} group (same reasoning as
			// the plugin proxy above): the caller is an external system, not
			// a logged-in user, so it authenticates with its own secret
			// token (X-Webhook-Token header) checked inline by the handler
			// rather than router-level session/API-key middleware.
			if deps.Automation != nil {
				r.Post("/webhooks/automations/{nodeId}", deps.Automation.ReceiveWebhook)
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

// corsMiddleware sets CORS headers per the given allow-list. An empty list,
// or a list containing "*", reflects Access-Control-Allow-Origin: * for
// every request (the historical default — permissive, tighten in production
// by setting CORS_ORIGINS). Otherwise only an exact-match Origin gets
// Access-Control-Allow-Origin echoed back; everything else gets no CORS
// headers at all, so the browser blocks the response.
func corsMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	allowAll := len(allowedOrigins) == 0
	allowed := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		if o == "*" {
			allowAll = true
			break
		}
		allowed[o] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			switch {
			case allowAll:
				w.Header().Set("Access-Control-Allow-Origin", "*")
			case origin != "" && allowed[origin]:
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
			}
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
