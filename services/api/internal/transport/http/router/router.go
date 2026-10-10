// Package router wires global middleware and all route groups onto a chi.Router.
package router

import (
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/platform/authz/iam"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	"github.com/Paca-AI/api/internal/transport/http/httpx"
	httpmw "github.com/Paca-AI/api/internal/transport/http/middleware"
)

// Deps holds all handler and middleware dependencies.
type Deps struct {
	TokenManager *jwttoken.Manager
	APIKeyAuth   httpmw.APIKeyAuthenticator
	// IAM decides every route gate (see guards).
	IAM                  *iam.Authorizer
	ProjectVisibilitySvc httpmw.ProjectVisibilityChecker
	Health               *handler.HealthHandler
	Version              *handler.VersionHandler
	Auth                 *handler.AuthHandler
	User                 *handler.UserHandler
	// Role serves the IAM roles and attachments API; RoleAttachments backs the
	// assignment gates (which roles a request adds and removes). Both are
	// optional: without Role the API is not mounted.
	Role            *handler.RoleHandler
	RoleAttachments httpmw.RoleAttachmentLookup
	Project         *handler.ProjectHandler
	Task            *handler.TaskHandler
	Sprint          *handler.SprintHandler
	View            *handler.ViewHandler
	Attachment      *handler.AttachmentHandler
	Document        *handler.DocumentHandler
	DocFile         *handler.DocFileHandler
	Notification    *handler.NotificationHandler
	APIKey          *handler.APIKeyHandler
	Plugin          *handler.PluginHandler
	Skills          *handler.SkillsHandler
	Agent           *handler.AgentHandler
	// AgentEnvironments / SessionEnvironments back the chat environment
	// gates (guards.ChatEnvironment / SessionEnvironment).
	AgentEnvironments httpmw.AgentEnvironmentLookup
	// MemberPrincipals resolves the assignees of a task request to principals.
	TaskNumbers         httpmw.TaskNumberLookup
	MemberPrincipals    httpmw.MemberPrincipalLookup
	SessionEnvironments httpmw.SessionEnvironmentLookup
	Environment         *handler.EnvironmentHandler
	Annotation          *handler.AnnotationHandler
	Conversation        *handler.ConversationHandler
	Automation          *handler.AutomationHandler
	Settings            *handler.SettingsHandler
	SSO                 *handler.SSOHandler
	ProjectActivity     *handler.ProjectActivityHandler
	ProjectExport       *handler.ProjectExportHandler
	Log                 *slog.Logger
	// CORSAllowedOrigins is the CORS allow-list — see corsMiddleware. A nil
	// or empty slice (the zero value, so every existing caller of this
	// struct literal keeps working unchanged) is treated the same as ["*"]:
	// reflect Access-Control-Allow-Origin: * for every request.
	CORSAllowedOrigins []string
	// AuthRateLimit caps requests per minute per client IP on each public
	// credential endpoint under /auth; refresh gets 3x. 0 (the zero value)
	// disables the limits.
	AuthRateLimit int
}

// New builds and returns a configured http.Handler.
func New(deps Deps) http.Handler {
	r := chi.NewRouter()

	// require builds the permission gate each route declares — see guards.
	require := newGuards(deps)

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

			// Environment deployment config (subdomain base / SSH bastion
			// host) — public, no auth required, same "read on every
			// relevant page load" shape as /branding above. Not
			// project-scoped (unlike every other /environments route
			// below): both values are a single deployment-wide setting,
			// not something that varies per project.
			if deps.Environment != nil {
				r.Get("/environments/config", deps.Environment.GetConfig)
			}

			// Auth. Every public endpoint here is rate-limited per client IP
			// (password guessing, token-set brute force, and SSO login
			// filling Redis with sign-in attempts). Each endpoint has its own
			// budget so refreshes can't starve logins.
			authLimitWith := func(perMinute int, onLimited http.Handler) func(http.Handler) http.Handler {
				if perMinute <= 0 {
					return func(next http.Handler) http.Handler { return next }
				}
				return httpmw.RateLimit(perMinute, time.Minute, onLimited)
			}
			authLimit := func(perMinute int) func(http.Handler) http.Handler {
				return authLimitWith(perMinute, nil)
			}
			r.Route("/auth", func(r chi.Router) {
				r.With(authLimit(deps.AuthRateLimit)).Post("/login", deps.Auth.Login)
				r.With(authLimit(3*deps.AuthRateLimit)).Post("/refresh", deps.Auth.Refresh)
				r.With(authLimit(3*deps.AuthRateLimit)).Post("/annotation-refresh", deps.Auth.AnnotationRefresh)
				r.With(httpmw.Authn(deps.TokenManager)).Post("/logout", deps.Auth.Logout)
				// Public — the link a password-set-token email points to;
				// the token itself (not a session) proves the caller's right
				// to act on the account.
				r.With(authLimit(deps.AuthRateLimit)).Post("/password/set", deps.User.SetPassword)

				// SSO / OIDC sign-in — public: the login page lists the
				// enabled providers, and login/callback are top-level
				// browser navigations to and back from the provider.
				if deps.SSO != nil {
					r.Get("/sso/providers", deps.SSO.ListPublicProviders)
					// Browser navigations: over the limit, back to the login
					// page with an error rather than a bare JSON 429.
					ssoLimited := http.HandlerFunc(deps.SSO.RateLimited)
					r.With(authLimitWith(deps.AuthRateLimit, ssoLimited)).Get("/sso/{slug}/login", deps.SSO.Login)
					r.With(authLimitWith(deps.AuthRateLimit, ssoLimited)).Get("/sso/{slug}/callback", deps.SSO.Callback)
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

				// User management. Create/update edit the profile only: a user's
				// roles are a privilege of their own (roles:assign) and are
				// changed solely by PUT /users/{userId}/roles below.
				r.With(require.Global(iam.ActionUsersRead)).Get("/users", deps.User.ListUsers)
				r.With(require.Global(iam.ActionUsersRead)).Get("/users/cursor", deps.User.ListUsersByCursor)
				r.With(require.Global(iam.ActionUsersWrite)).Post("/users", deps.User.CreateUser)
				r.With(require.Global(iam.ActionUsersRead)).Get("/users/{userId}", deps.User.GetUserByID)
				r.With(require.Global(iam.ActionUsersWrite)).Patch("/users/{userId}", deps.User.AdminUpdateUser)
				r.With(require.Global(iam.ActionUsersWrite)).Patch("/users/{userId}/password", deps.User.ResetPassword)
				r.With(require.Global(iam.ActionUsersDelete)).Delete("/users/{userId}", deps.User.DeleteUser)

				// IAM roles and attachments (platform roles, project_id NULL).
				// Reads and writes are gated on the role named in the URL
				// ("role/{roleId}"; collection routes on "role/*"). Saving a
				// policy additionally passes the escalation guard, and so does
				// making a role the default: nobody may hand out more than
				// they hold. Attachments are replace-sets (a user's
				// platform-wide roles, and a global agent's) gated like
				// iam:PassRole: roles:assign on role/{roleId} of each role the
				// request adds or removes; what the assigner holds is not asked.
				if deps.Role != nil {
					r.With(require.Global(iam.ActionRolesRead)).Get("/roles", deps.Role.List)
					r.With(require.Global(iam.ActionRolesWrite)).Post("/roles", deps.Role.Create)
					r.With(require.PlatformEntity("role", "roleId", resRole, iam.ActionRolesRead)).Get("/roles/{roleId}", deps.Role.Get)
					r.With(require.PlatformEntity("role", "roleId", resRole, iam.ActionRolesWrite)).Put("/roles/{roleId}", deps.Role.Update)
					r.With(require.PlatformEntity("role", "roleId", resRole, iam.ActionRolesWrite)).Delete("/roles/{roleId}", deps.Role.Delete)
					r.With(require.PlatformEntity("role", "roleId", resRole, iam.ActionRolesWrite)).Put("/roles/{roleId}/default", deps.Role.SetDefault)

					r.With(require.PlatformEntity("user", "userId", resUser, iam.ActionRolesRead)).Get("/users/{userId}/roles", deps.Role.ListUserRoles)
					r.With(require.AssignUserRoles()).Put("/users/{userId}/roles", deps.Role.ReplaceUserRoles)

					r.With(require.GlobalAgentRoles(iam.ActionRolesRead, iam.ActionAgentsRead)).Get("/agents/{agentId}/roles", deps.Role.ListAgentRoles)
					r.With(require.GlobalAgentRoles(iam.ActionAgentsWrite), require.AssignGlobalAgentRoles()).Put("/agents/{agentId}/roles", deps.Role.ReplaceAgentRoles)
				}

				// Global agent management — CRUD for AgentScopeGlobal agents,
				// mirroring the user/global-role shape above. Global agents are
				// not tied to any project; they're attached to one later via the
				// same "invite a member" flow used for humans (POST
				// /projects/{projectId}/members with agent_id set — see below).
				if deps.Agent != nil {
					r.With(require.Global(iam.ActionAgentsRead)).Get("/agents", deps.Agent.ListGlobalAgents)
					r.With(require.Global(iam.ActionAgentsWrite)).Post("/agents", deps.Agent.CreateGlobalAgent)
					r.With(require.Global(iam.ActionAgentsRead)).Get("/agents/{agentId}", deps.Agent.GetGlobalAgent)
					r.With(require.Global(iam.ActionAgentsWrite)).Patch("/agents/{agentId}", deps.Agent.UpdateGlobalAgent)
					r.With(require.Global(iam.ActionAgentsWrite)).Delete("/agents/{agentId}", deps.Agent.DeleteGlobalAgent)

					// An agent's roles are replaced through PUT /agents/{agentId}/roles
					// (roles:assign on top of agents:write), registered with the role routes.

					// ACP local bridge
					r.With(require.Global(iam.ActionAgentsWrite)).Post("/agents/{agentId}/acp-bridge-token", deps.Agent.GenerateGlobalACPBridgeToken)
					r.With(require.Global(iam.ActionAgentsRead)).Get("/agents/{agentId}/acp-bridge-status", deps.Agent.GetGlobalACPBridgeStatus)
					r.With(require.Global(iam.ActionAgentsWrite)).Post("/agents/{agentId}/mcp-agent-key", deps.Agent.GenerateGlobalAgentMCPKey)

					// Avatar
					r.With(require.Global(iam.ActionAgentsWrite)).Post("/agents/{agentId}/avatar/initiate-upload", deps.Agent.InitiateGlobalAvatarUpload)
					r.With(require.Global(iam.ActionAgentsWrite)).Post("/agents/{agentId}/avatar/complete-upload", deps.Agent.CompleteGlobalAvatarUpload)
					r.With(require.Global(iam.ActionAgentsWrite)).Delete("/agents/{agentId}/avatar", deps.Agent.DeleteGlobalAvatar)

					// MCP servers
					r.With(require.Global(iam.ActionAgentsRead)).Get("/agents/{agentId}/mcp-servers", deps.Agent.ListGlobalAgentMCPServers)
					r.With(require.Global(iam.ActionAgentsWrite)).Post("/agents/{agentId}/mcp-servers", deps.Agent.AddGlobalAgentMCPServer)
					r.With(require.Global(iam.ActionAgentsWrite)).Patch("/agents/{agentId}/mcp-servers/{serverId}", deps.Agent.UpdateGlobalAgentMCPServer)
					r.With(require.Global(iam.ActionAgentsWrite)).Delete("/agents/{agentId}/mcp-servers/{serverId}", deps.Agent.DeleteGlobalAgentMCPServer)

					// Skills
					r.With(require.Global(iam.ActionAgentsRead)).Get("/agents/{agentId}/skills", deps.Agent.ListGlobalAgentSkills)
					r.With(require.Global(iam.ActionAgentsWrite)).Post("/agents/{agentId}/skills", deps.Agent.AddGlobalAgentSkill)
					r.With(require.Global(iam.ActionAgentsWrite)).Patch("/agents/{agentId}/skills/{skillId}", deps.Agent.UpdateGlobalAgentSkill)
					r.With(require.Global(iam.ActionAgentsWrite)).Delete("/agents/{agentId}/skills/{skillId}", deps.Agent.DeleteGlobalAgentSkill)

					// Environment variables
					r.With(require.Global(iam.ActionAgentsRead)).Get("/agents/{agentId}/env-vars", deps.Agent.ListGlobalAgentEnvVars)
					r.With(require.Global(iam.ActionAgentsWrite)).Post("/agents/{agentId}/env-vars", deps.Agent.AddGlobalAgentEnvVar)
					r.With(require.Global(iam.ActionAgentsWrite)).Patch("/agents/{agentId}/env-vars/{envVarId}", deps.Agent.UpdateGlobalAgentEnvVar)
					r.With(require.Global(iam.ActionAgentsWrite)).Delete("/agents/{agentId}/env-vars/{envVarId}", deps.Agent.DeleteGlobalAgentEnvVar)
				}

				// Workspace branding (logo/favicon/primary color) — a
				// singleton, so no {id} in the path. Sub-routed under
				// "/settings/logo" and "/settings/favicon" with an "/avatar/…"
				// suffix so the frontend can drive both through the same
				// generic avatar-upload client/component used for
				// users/agents/projects (which always POSTs/DELETEs to
				// "{basePath}/avatar/…").
				if deps.Settings != nil {
					write := require.Global(iam.ActionSettingsWrite)
					r.With(write).Patch("/settings", deps.Settings.UpdateSettings)
					r.With(write).Post("/settings/logo/avatar/initiate-upload", deps.Settings.InitiateLogoUpload)
					r.With(write).Post("/settings/logo/avatar/complete-upload", deps.Settings.CompleteLogoUpload)
					r.With(write).Delete("/settings/logo/avatar", deps.Settings.DeleteLogo)
					r.With(write).Post("/settings/favicon/avatar/initiate-upload", deps.Settings.InitiateFaviconUpload)
					r.With(write).Post("/settings/favicon/avatar/complete-upload", deps.Settings.CompleteFaviconUpload)
					r.With(write).Delete("/settings/favicon/avatar", deps.Settings.DeleteFavicon)
				}

				// SSO / OIDC identity providers. A separate permission from
				// settings:write — see iam.ActionSettingsSSOWrite for
				// why it is root-equivalent. Reads are gated by it too: the
				// list carries each provider's full configuration.
				if deps.SSO != nil {
					sso := require.Global(iam.ActionSettingsSSOWrite)
					r.With(sso).Get("/sso/providers", deps.SSO.ListProviders)
					r.With(sso).Post("/sso/providers", deps.SSO.CreateProvider)
					r.With(sso).Put("/sso/providers/{providerId}", deps.SSO.UpdateProvider)
					r.With(sso).Delete("/sso/providers/{providerId}", deps.SSO.DeleteProvider)
				}
			})

			// IAM role editor helpers — pure functions over the action registry and
			// the attribute schema (no workspace data is read), so any
			// authenticated caller may use them. The one exception is a
			// simulation that names a principal: that exposes the
			// principal's grants and needs roles:read on role/*.
			if deps.Role != nil {
				r.Route("/roles", func(r chi.Router) {
					r.Use(httpmw.Authn(deps.TokenManager, deps.APIKeyAuth))
					r.Use(httpmw.RequireFreshPassword())
					r.Get("/actions", deps.Role.Actions)
					r.Get("/attribute-schema", deps.Role.AttributeSchema)
					r.Post("/validate", deps.Role.Validate)
					r.With(require.SimulateWithPrincipal()).Post("/simulate", deps.Role.Simulate)
				})
			}

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
				r.With(require.Global(iam.ActionProjectsCreate)).Post("/projects", deps.Project.CreateProject)
			})

			// Port forward resolution — how the Paca browser extension
			// turns "I'm on host:<port>" into "this is environment X in
			// project Y" before it knows which project-scoped endpoint to
			// call next. Not project-scoped in the URL (the caller doesn't
			// know the project yet); scoped instead to the caller's own
			// accessible projects inside the handler itself (see
			// AnnotationRepository.ResolvePortForward).
			if deps.Annotation != nil {
				r.Group(func(r chi.Router) {
					r.Use(httpmw.Authn(deps.TokenManager, deps.APIKeyAuth))
					r.Get("/port-forwards/resolve", deps.Annotation.ResolvePortForward)
				})
			}

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

					// Agent self-service reads of a conversation by ID (any
					// project, or global) — the read_conversation MCP tool's
					// path when a user attaches a Conversation as chat context.
					// Deliberately separate from the human-facing
					// /projects/{projectId}/conversations and
					// /agents/conversations routes below: both of those
					// authorize against a human member/actor identity a bare
					// agent doesn't have (see
					// agentdom.Service.GetConversationForAgent's doc comment).
					if deps.Conversation != nil {
						r.Get("/me/conversations/{conversationId}", deps.Conversation.GetConversationForAgent)
						r.Get("/me/conversations/{conversationId}/events", deps.Conversation.GetConversationEventsForAgent)
					}

					// Global chat — chatting with a global agent from the home
					// page / admin pages, no project context. Any authenticated
					// human may chat with any global agent, same as any project
					// member may chat with a project agent — deliberately not
					// gated behind PermissionConversationsRead (a regular
					// USER-role human has no global conversations.* permission
					// by default, and this chat is meant to be available to
					// every user).
					// Auto mode's resolve step for global chat — ungated like the
					// routes below it (every global agent is a candidate, no
					// per-caller filtering needed — see ResolveGlobalAutoAgent's
					// doc comment).
					r.Post("/resolve-auto", deps.Agent.ResolveGlobalAutoAgent)
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
						r.Patch("/conversations/{conversationId}", deps.Conversation.UpdateGlobalConversation)
						r.Delete("/conversations/{conversationId}", deps.Conversation.DeleteGlobalConversation)
					}
				})
			}

			// Single-project routes — optional auth for public project support
			r.Route("/projects/{projectId}", func(r chi.Router) {
				r.Use(httpmw.OptionalAuthn(deps.TokenManager, deps.APIKeyAuth))
				r.Use(httpmw.RequireFreshPassword())

				r.With(require.ProjectOrPublic(iam.ActionProjectsRead)).Get("/", deps.Project.GetProject)
				r.With(require.Project(iam.ActionProjectsWrite)).Patch("/", deps.Project.UpdateProject)
				r.With(require.Project(iam.ActionProjectsDelete)).Delete("/", deps.Project.DeleteProject)
				r.With(require.Project(iam.ActionProjectsWrite)).Patch("/jev-config", deps.Project.UpdateJevConfig)
				r.With(require.Project(iam.ActionProjectsWrite)).Post("/jev-config/test", deps.Project.TestJevConfig)

				// Avatar
				r.With(require.Project(iam.ActionProjectsWrite)).Post("/avatar/initiate-upload", deps.Project.InitiateAvatarUpload)
				r.With(require.Project(iam.ActionProjectsWrite)).Post("/avatar/complete-upload", deps.Project.CompleteAvatarUpload)
				r.With(require.Project(iam.ActionProjectsWrite)).Delete("/avatar", deps.Project.DeleteAvatar)

				// Activity log
				r.With(require.Project(iam.ActionProjectActivitiesRead)).Get("/activities", deps.ProjectActivity.ListActivities)

				// Exports — asynchronous dumps of project data. Every route needs
				// project.export, including list/get/download: an export is the
				// whole project in one file, so even seeing that one exists, or
				// fetching it, is gated like requesting it.
				r.Route("/exports", func(r chi.Router) {
					r.With(require.Project(iam.ActionProjectExport)).Post("/", deps.ProjectExport.RequestExport)
					r.With(require.Project(iam.ActionProjectExport)).Get("/", deps.ProjectExport.ListExports)
					r.With(require.Project(iam.ActionProjectExport)).Get("/{exportId}", deps.ProjectExport.GetExport)
					r.With(require.Project(iam.ActionProjectExport)).Get("/{exportId}/download", deps.ProjectExport.DownloadExport)
				})

				// Members
				r.Route("/members", func(r chi.Router) {
					r.With(require.ProjectOrPublic(iam.ActionProjectMembersRead)).Get("/", deps.Project.ListMembers)
					// role_ids names the roles the new member (or invited agent) is
					// given: the caller needs roles:assign on each one inside this project.
					r.With(require.Project(iam.ActionProjectMembersWrite), require.AssignNewProjectPrincipalRoles()).Post("/", deps.Project.AddMember)
					r.Get("/me/permissions", deps.Project.GetMyProjectPermissions)
					r.With(require.Project(iam.ActionProjectMembersWrite)).Patch("/{memberId}", deps.Project.UpdateMember)
					r.With(require.Project(iam.ActionProjectMembersWrite)).Delete("/{memberId}", deps.Project.RemoveMember)
					if deps.Role != nil {
						// A member's roles within this project (replace-set). Only
						// roles:assign is required, checked on each role added or
						// removed; project.members:write is not.
						r.With(require.Project(iam.ActionProjectMembersRead)).Get("/{memberId}/roles", deps.Role.ListMemberRoles)
						r.With(require.AssignMemberRoles()).Put("/{memberId}/roles", deps.Role.ReplaceMemberRoles)
					}
				})

				// IAM roles owned by the project (plus, for listing and reading, the
				// platform roles that may be attached here). Resources:
				// "project/{projectId}/role/*" (list, create) and
				// ".../role/{roleId}"; saving a policy passes the escalation
				// guard. The helper routes mirror /roles/* and need roles:read
				// on the project.
				if deps.Role != nil {
					r.Route("/roles", func(r chi.Router) {
						r.With(require.ProjectRoleCollection(iam.ActionRolesRead)).Get("/", deps.Role.List)
						r.With(require.ProjectRoleCollection(iam.ActionRolesWrite)).Post("/", deps.Role.Create)
						r.With(require.Project(iam.ActionRolesRead)).Get("/actions", deps.Role.Actions)
						r.With(require.Project(iam.ActionRolesRead)).Get("/attribute-schema", deps.Role.AttributeSchema)
						r.With(require.Project(iam.ActionRolesRead)).Post("/validate", deps.Role.Validate)
						// Naming a principal in the body exposes that principal's
						// workspace-wide grants, so it additionally needs roles:read on
						// role/* (the same rule as POST /roles/simulate); a bare policy
						// can be simulated with project roles:read alone.
						r.With(require.Project(iam.ActionRolesRead), require.SimulateWithPrincipal()).Post("/simulate", deps.Role.Simulate)
						r.With(require.ProjectRole(iam.ActionRolesRead)).Get("/{roleId}", deps.Role.Get)
						r.With(require.ProjectRole(iam.ActionRolesWrite)).Put("/{roleId}", deps.Role.Update)
						r.With(require.ProjectRole(iam.ActionRolesWrite)).Delete("/{roleId}", deps.Role.Delete)
					})
				}

				// Task types — project *schema* (which task types exist).
				// Redefining the type list is gated on
				// project.settings.task_types:write, a different capability
				// from editing a task's own content (see authz.
				// PermissionProjectSettingsTaskTypesWrite's doc comment);
				// viewing it is gated on tasks:read like the type list's own
				// consumer (a task's type badge) rather than a dedicated
				// read permission — no meaningful boundary in seeing what
				// types exist that isn't already crossed by seeing the tasks
				// that use them.
				r.Route("/task-types", func(r chi.Router) {
					r.With(require.ProjectOrPublic(iam.ActionTasksRead)).Get("/", deps.Task.ListTaskTypes)
					r.With(require.Project(iam.ActionProjectSettingsTaskTypesWrite)).Post("/", deps.Task.CreateTaskType)
					r.With(require.Project(iam.ActionProjectSettingsTaskTypesWrite)).Patch("/{typeId}", deps.Task.UpdateTaskType)
					r.With(require.Project(iam.ActionProjectSettingsTaskTypesWrite)).Delete("/{typeId}", deps.Task.DeleteTaskType)
					r.With(require.Project(iam.ActionProjectSettingsTaskTypesWrite)).Put("/{typeId}/set-default", deps.Task.SetDefaultTaskType)
				})

				// Task statuses — project *schema* (which statuses exist,
				// their order, which is the default), same split as task
				// types above (view via tasks:read, redefine via
				// project.settings.task_statuses:write). Moving a task
				// *between* existing statuses (PATCH /tasks/{id}, or
				// drag-and-drop via /views/{id}/task-positions below) stays
				// on tasks:write — that's editing a task, not the status
				// list.
				r.Route("/task-statuses", func(r chi.Router) {
					r.With(require.ProjectOrPublic(iam.ActionTasksRead)).Get("/", deps.Task.ListTaskStatuses)
					r.With(require.Project(iam.ActionProjectSettingsTaskStatusesWrite)).Post("/", deps.Task.CreateTaskStatus)
					// Static /positions must be registered before /{statusId}.
					r.With(require.Project(iam.ActionProjectSettingsTaskStatusesWrite)).Put("/positions", deps.Task.ReorderTaskStatuses)
					r.With(require.Project(iam.ActionProjectSettingsTaskStatusesWrite)).Patch("/{statusId}", deps.Task.UpdateTaskStatus)
					r.With(require.Project(iam.ActionProjectSettingsTaskStatusesWrite)).Delete("/{statusId}", deps.Task.DeleteTaskStatus)
					r.With(require.Project(iam.ActionProjectSettingsTaskStatusesWrite)).Put("/{statusId}/set-default", deps.Task.SetDefaultTaskStatus)
				})

				// Automation graph — reuses the workflows:read/write permission
				// keys (already seeded on every default/project role) rather
				// than introducing automations.* and a permission-backfill
				// migration.
				if deps.Automation != nil {
					r.Route("/automations", func(r chi.Router) {
						r.With(require.Project(iam.ActionWorkflowsRead)).Get("/", deps.Automation.ListAutomations)
						r.With(require.Project(iam.ActionWorkflowsWrite)).Post("/", deps.Automation.CreateAutomation)
						r.With(require.ProjectEntity("workflow", "automationId", resWorkflow, iam.ActionWorkflowsRead)).Get("/{automationId}", deps.Automation.GetAutomation)
						r.With(require.ProjectEntity("workflow", "automationId", resWorkflow, iam.ActionWorkflowsWrite)).Patch("/{automationId}", deps.Automation.UpdateAutomation)
						r.With(require.ProjectEntity("workflow", "automationId", resWorkflow, iam.ActionWorkflowsWrite)).Delete("/{automationId}", deps.Automation.DeleteAutomation)
						r.With(require.ProjectEntity("workflow", "automationId", resWorkflow, iam.ActionWorkflowsWrite)).Post("/{automationId}/activate", deps.Automation.ActivateAutomation)
						r.With(require.ProjectEntity("workflow", "automationId", resWorkflow, iam.ActionWorkflowsWrite)).Post("/{automationId}/deactivate", deps.Automation.DeactivateAutomation)

						r.With(require.ProjectEntity("workflow", "automationId", resWorkflow, iam.ActionWorkflowsWrite)).Post("/{automationId}/nodes", deps.Automation.AddAutomationNode)
						r.With(require.ProjectEntity("workflow", "automationId", resWorkflow, iam.ActionWorkflowsWrite)).Patch("/{automationId}/nodes/{nodeId}", deps.Automation.UpdateAutomationNode)
						r.With(require.ProjectEntity("workflow", "automationId", resWorkflow, iam.ActionWorkflowsWrite)).Delete("/{automationId}/nodes/{nodeId}", deps.Automation.RemoveAutomationNode)
						r.With(require.ProjectEntity("workflow", "automationId", resWorkflow, iam.ActionWorkflowsWrite)).Post("/{automationId}/nodes/{nodeId}/webhook-token", deps.Automation.GenerateWebhookToken)

						r.With(require.ProjectEntity("workflow", "automationId", resWorkflow, iam.ActionWorkflowsWrite)).Post("/{automationId}/edges", deps.Automation.AddAutomationEdge)
						r.With(require.ProjectEntity("workflow", "automationId", resWorkflow, iam.ActionWorkflowsWrite)).Delete("/{automationId}/edges/{edgeId}", deps.Automation.RemoveAutomationEdge)

						r.With(require.ProjectEntity("workflow", "automationId", resWorkflow, iam.ActionWorkflowsRead)).Get("/{automationId}/runs", deps.Automation.ListAutomationRuns)
						r.With(require.ProjectEntity("workflow", "automationId", resWorkflow, iam.ActionWorkflowsRead)).Get("/{automationId}/runs/{runId}/steps", deps.Automation.ListAutomationRunSteps)
					})
					r.With(require.Project(iam.ActionWorkflowsRead)).Get("/automation-dependency-map", deps.Automation.GetAutomationDependencyMap)
					r.With(require.Project(iam.ActionWorkflowsRead)).Get("/automation-plugin-node-types", deps.Automation.ListPluginNodeTypes)
				}

				// Sprints
				r.Route("/sprints", func(r chi.Router) {
					r.With(require.ProjectOrPublic(iam.ActionSprintsRead)).Get("/", deps.Sprint.ListSprints)
					r.With(require.Project(iam.ActionSprintsWrite)).Post("/", deps.Sprint.CreateSprint)
					r.With(require.ProjectEntityOrPublic("sprint", "sprintId", resSprint, iam.ActionSprintsRead)).Get("/{sprintId}", deps.Sprint.GetSprint)
					r.With(require.ProjectEntity("sprint", "sprintId", resSprint, iam.ActionSprintsWrite)).Patch("/{sprintId}", deps.Sprint.UpdateSprint)
					r.With(require.ProjectEntity("sprint", "sprintId", resSprint, iam.ActionSprintsWrite)).Delete("/{sprintId}", deps.Sprint.DeleteSprint)
					r.With(require.ProjectEntity("sprint", "sprintId", resSprint, iam.ActionSprintsWrite)).Post("/{sprintId}/complete", deps.Sprint.CompleteSprint)
				})

				// Views — gated on their own views:read/write, not a
				// borrowed sprints:read/write (there was no dedicated
				// permission for the view resource itself before). Moving a
				// *task* within a view (below) stays on tasks.*.
				r.Route("/views", func(r chi.Router) {
					r.With(require.ProjectOrPublic(iam.ActionViewsRead)).Get("/", deps.View.ListViews)
					r.With(require.Project(iam.ActionViewsWrite), require.ViewCreate()).Post("/", deps.View.CreateView)
					// Static /positions must be registered before /{viewId}.
					r.With(require.Project(iam.ActionViewsWrite), require.ViewReorderItems()).Put("/positions", deps.View.ReorderViews)
					r.With(require.ProjectEntityOrPublic("view", "viewId", resView, iam.ActionViewsRead)).Get("/{viewId}", deps.View.GetView)
					r.With(require.ProjectEntity("view", "viewId", resView, iam.ActionViewsWrite)).Patch("/{viewId}", deps.View.UpdateView)
					// Personal (per-user) view config: only needs read access to
					// the view — a viewer may sort/filter their own view without
					// permission to mutate the shared view.
					r.With(require.ProjectEntity("view", "viewId", resView, iam.ActionViewsRead)).Put("/{viewId}/config", deps.View.UpdateMyViewConfig)
					// Clearing a personal override needs no more privilege
					// than setting one.
					r.With(require.ProjectEntity("view", "viewId", resView, iam.ActionViewsRead)).Delete("/{viewId}/config", deps.View.ClearMyViewConfig)
					r.With(require.ProjectEntity("view", "viewId", resView, iam.ActionViewsWrite)).Delete("/{viewId}", deps.View.DeleteView)
					r.With(require.ProjectOrPublic(iam.ActionTasksRead)).Get("/{viewId}/task-positions", deps.View.ListTaskPositions)
					r.With(require.Project(iam.ActionTasksWrite), require.TaskPositionItems()).Put("/{viewId}/task-positions", deps.View.BulkMoveTasks)
					r.With(require.ProjectEntity("task", "taskId", resTask, iam.ActionTasksWrite)).Put("/{viewId}/task-positions/{taskId}", deps.View.MoveTask)
				})

				// Tasks
				r.Route("/tasks", func(r chi.Router) {
					r.With(require.ProjectOrPublic(iam.ActionTasksRead)).Get("/", deps.Task.ListTasks)
					r.With(require.Project(iam.ActionTasksWrite), require.TaskCreate()).Post("/", deps.Task.CreateTask)
					r.With(require.TaskByNumberOrPublic(iam.ActionTasksRead)).Get("/by-number/{taskNumber}", deps.Task.GetTaskByNumber)
					r.With(require.ProjectEntityOrPublic("task", "taskId", resTask, iam.ActionTasksRead)).Get("/{taskId}", deps.Task.GetTask)
					r.With(require.ProjectEntity("task", "taskId", resTask, iam.ActionTasksWrite), require.TaskChange()).Patch("/{taskId}", deps.Task.UpdateTask)
					r.With(require.ProjectEntity("task", "taskId", resTask, iam.ActionTasksWrite)).Delete("/{taskId}", deps.Task.DeleteTask)

					if deps.Agent != nil {
						r.With(require.ProjectEntity("task", "taskId", resTask, iam.ActionTasksWrite)).Post("/{taskId}/write-with-ai", deps.Agent.WriteTaskDescriptionWithAI)
					}

					// Activities
					r.Route("/{taskId}/activities", func(r chi.Router) {
						r.With(require.ProjectEntityOrPublic("task", "taskId", resTask, iam.ActionTasksRead)).Get("/", deps.Task.ListTaskActivities)
						r.With(require.ProjectEntity("task", "taskId", resTask, iam.ActionTasksWrite)).Post("/comments", deps.Task.AddComment)
						r.With(require.ProjectEntity("task", "taskId", resTask, iam.ActionTasksWrite)).Patch("/comments/{commentId}", deps.Task.UpdateComment)
						r.With(require.ProjectEntity("task", "taskId", resTask, iam.ActionTasksWrite)).Delete("/comments/{commentId}", deps.Task.DeleteComment)
					})

					// Links
					r.Route("/{taskId}/links", func(r chi.Router) {
						r.With(require.ProjectEntityOrPublic("task", "taskId", resTask, iam.ActionTasksRead)).Get("/", deps.Task.ListTaskLinks)
						r.With(require.ProjectEntity("task", "taskId", resTask, iam.ActionTasksWrite)).Post("/", deps.Task.CreateTaskLink)
						r.With(require.ProjectEntity("task", "taskId", resTask, iam.ActionTasksWrite)).Delete("/{linkId}", deps.Task.DeleteTaskLink)
					})

					// Attachments
					r.Route("/{taskId}/attachments", func(r chi.Router) {
						r.With(require.ProjectEntityOrPublic("task", "taskId", resTask, iam.ActionTasksRead)).Get("/", deps.Attachment.ListTaskAttachments)
						r.With(require.ProjectEntity("task", "taskId", resTask, iam.ActionTasksWrite)).Post("/initiate-upload", deps.Attachment.InitiateUpload)
						r.With(require.ProjectEntity("task", "taskId", resTask, iam.ActionTasksWrite)).Post("/complete-upload", deps.Attachment.CompleteUpload)
						r.With(require.ProjectEntityOrPublic("task", "taskId", resTask, iam.ActionTasksRead)).Get("/{attachmentId}/download-url", deps.Attachment.GetDownloadURL)
						r.With(require.ProjectEntityOrPublic("task", "taskId", resTask, iam.ActionTasksRead)).Get("/{attachmentId}/content", deps.Attachment.GetAttachmentContent)
						r.With(require.ProjectEntity("task", "taskId", resTask, iam.ActionTasksWrite)).Delete("/{attachmentId}", deps.Attachment.DeleteTaskAttachment)
					})
				})

				// Custom field definitions — project schema, same split as
				// task types/statuses above (view via tasks:read, redefine
				// via project.settings.custom_fields:write).
				r.Route("/custom-fields", func(r chi.Router) {
					r.With(require.ProjectOrPublic(iam.ActionTasksRead)).Get("/", deps.Task.ListCustomFieldDefinitions)
					r.With(require.Project(iam.ActionProjectSettingsCustomFieldsWrite)).Post("/", deps.Task.CreateCustomFieldDefinition)
					r.With(require.ProjectOrPublic(iam.ActionTasksRead)).Get("/{fieldId}", deps.Task.GetCustomFieldDefinition)
					r.With(require.Project(iam.ActionProjectSettingsCustomFieldsWrite)).Patch("/{fieldId}", deps.Task.UpdateCustomFieldDefinition)
					r.With(require.Project(iam.ActionProjectSettingsCustomFieldsWrite)).Delete("/{fieldId}", deps.Task.DeleteCustomFieldDefinition)
				})

				// Documentation
				r.Route("/docs", func(r chi.Router) {
					// Folders
					r.Route("/folders", func(r chi.Router) {
						r.With(require.ProjectOrPublic(iam.ActionDocsRead)).Get("/", deps.Document.ListFolders)
						r.With(require.Project(iam.ActionDocsWrite)).Post("/", deps.Document.CreateFolder)
						r.With(require.Project(iam.ActionDocsWrite)).Patch("/{folderId}", deps.Document.UpdateFolder)
						r.With(require.Project(iam.ActionDocsWrite)).Delete("/{folderId}", deps.Document.DeleteFolder)
					})

					// Documents — search (registered before /{docId} so "search"
					// isn't parsed as a document ID)
					r.With(require.ProjectOrPublic(iam.ActionDocsRead)).Get("/search", deps.Document.SearchDocuments)

					// Documents — collection
					r.With(require.ProjectOrPublic(iam.ActionDocsRead)).Get("/", deps.Document.ListDocuments)
					r.With(require.Project(iam.ActionDocsWrite), require.DocCreate()).Post("/", deps.Document.CreateDocument)

					// Documents — single item
					r.Route("/{docId}", func(r chi.Router) {
						r.With(require.ProjectEntityOrPublic("doc", "docId", resDoc, iam.ActionDocsRead)).Get("/", deps.Document.GetDocument)
						r.With(require.ProjectEntity("doc", "docId", resDoc, iam.ActionDocsWrite), require.DocChange()).Patch("/", deps.Document.UpdateDocument)
						r.With(require.ProjectEntity("doc", "docId", resDoc, iam.ActionDocsWrite)).Delete("/", deps.Document.DeleteDocument)

						// Snapshots
						r.Route("/snapshots", func(r chi.Router) {
							r.With(require.ProjectEntityOrPublic("doc", "docId", resDoc, iam.ActionDocsRead)).Get("/", deps.Document.ListSnapshots)
							r.With(require.ProjectEntityOrPublic("doc", "docId", resDoc, iam.ActionDocsRead)).Get("/{snapshotId}", deps.Document.GetSnapshot)
						})

						// Activity log
						r.With(require.ProjectEntityOrPublic("doc", "docId", resDoc, iam.ActionDocsRead)).Get("/activities", deps.Document.ListActivities)

						// Comments
						r.With(require.ProjectEntity("doc", "docId", resDoc, iam.ActionDocsWrite)).Post("/comments", deps.Document.AddComment)
						r.With(require.ProjectEntity("doc", "docId", resDoc, iam.ActionDocsWrite)).Patch("/comments/{commentId}", deps.Document.UpdateComment)
						r.With(require.ProjectEntity("doc", "docId", resDoc, iam.ActionDocsWrite)).Delete("/comments/{commentId}", deps.Document.DeleteComment)

						// Doc file uploads
						r.With(require.ProjectEntity("doc", "docId", resDoc, iam.ActionDocsWrite)).Post("/files/initiate-upload", deps.DocFile.InitiateDocUpload)
						r.With(require.ProjectEntity("doc", "docId", resDoc, iam.ActionDocsWrite)).Post("/files/complete-upload", deps.DocFile.CompleteDocUpload)
						r.With(require.ProjectEntityOrPublic("doc", "docId", resDoc, iam.ActionDocsRead)).Get("/files/{fileId}/download-url", deps.DocFile.GetDocFileDownloadURL)
						r.With(require.ProjectEntity("doc", "docId", resDoc, iam.ActionDocsWrite)).Delete("/files/{fileId}", deps.DocFile.DeleteDocFile)
					})
				})

				// Agents
				if deps.Agent != nil {
					r.Route("/agents", func(r chi.Router) {
						r.With(require.Project(iam.ActionAgentsRead)).Get("/", deps.Agent.ListAgents)
						// CreateAgent inserts a project_members row bound to a
						// caller-supplied role_ids (each needs roles:assign), which is a
						// membership-granting operation — so, like the sibling
						// POST /members below, it requires project.members:write
						// in addition to agents:write (GHSA-xxc8-ggm7-vmxp).
						r.With(require.Project(iam.ActionAgentsWrite, iam.ActionProjectMembersWrite), require.AssignNewProjectPrincipalRoles()).Post("/", deps.Agent.CreateAgent)
						r.With(require.AgentUse(iam.ActionAgentsRead)).Get("/{agentId}", deps.Agent.GetAgent)
						r.With(require.AgentUse(iam.ActionAgentsWrite)).Patch("/{agentId}", deps.Agent.UpdateAgent)
						r.With(require.AgentUse(iam.ActionAgentsWrite)).Delete("/{agentId}", deps.Agent.DeleteAgent)

						// ACP local bridge
						r.With(require.AgentUse(iam.ActionAgentsWrite)).Post("/{agentId}/acp-bridge-token", deps.Agent.GenerateACPBridgeToken)
						r.With(require.AgentUse(iam.ActionAgentsRead)).Get("/{agentId}/acp-bridge-status", deps.Agent.GetACPBridgeStatus)
						r.With(require.AgentUse(iam.ActionAgentsWrite)).Post("/{agentId}/mcp-agent-key", deps.Agent.GenerateAgentMCPKey)

						// Provider CLI — Write, not Read: it runs a live probe inside the
						// agent's environment and persists cli_login_verified_at.
						r.With(require.AgentUse(iam.ActionAgentsWrite)).Post("/{agentId}/verify-cli-login", deps.Agent.VerifyCLILogin)

						// Avatar
						r.With(require.AgentUse(iam.ActionAgentsWrite)).Post("/{agentId}/avatar/initiate-upload", deps.Agent.InitiateAvatarUpload)
						r.With(require.AgentUse(iam.ActionAgentsWrite)).Post("/{agentId}/avatar/complete-upload", deps.Agent.CompleteAvatarUpload)
						r.With(require.AgentUse(iam.ActionAgentsWrite)).Delete("/{agentId}/avatar", deps.Agent.DeleteAvatar)

						// Activity feed
						r.With(require.AgentUse(iam.ActionAgentsRead)).Get("/{agentId}/activities", deps.Agent.ListAgentActivities)

						// MCP servers
						r.With(require.AgentUse(iam.ActionAgentsRead)).Get("/{agentId}/mcp-servers", deps.Agent.ListMCPServers)
						r.With(require.AgentUse(iam.ActionAgentsWrite)).Post("/{agentId}/mcp-servers", deps.Agent.AddMCPServer)
						r.With(require.AgentUse(iam.ActionAgentsWrite)).Patch("/{agentId}/mcp-servers/{serverId}", deps.Agent.UpdateMCPServer)
						r.With(require.AgentUse(iam.ActionAgentsWrite)).Delete("/{agentId}/mcp-servers/{serverId}", deps.Agent.DeleteMCPServer)

						// Skills
						r.With(require.AgentUse(iam.ActionAgentsRead)).Get("/{agentId}/skills", deps.Agent.ListSkills)
						r.With(require.AgentUse(iam.ActionAgentsWrite)).Post("/{agentId}/skills", deps.Agent.AddSkill)
						r.With(require.AgentUse(iam.ActionAgentsWrite)).Patch("/{agentId}/skills/{skillId}", deps.Agent.UpdateSkill)
						r.With(require.AgentUse(iam.ActionAgentsWrite)).Delete("/{agentId}/skills/{skillId}", deps.Agent.DeleteSkill)

						// Environment variables
						r.With(require.AgentUse(iam.ActionAgentsRead)).Get("/{agentId}/env-vars", deps.Agent.ListEnvVars)
						r.With(require.AgentUse(iam.ActionAgentsWrite)).Post("/{agentId}/env-vars", deps.Agent.AddEnvVar)
						r.With(require.AgentUse(iam.ActionAgentsWrite)).Patch("/{agentId}/env-vars/{envVarId}", deps.Agent.UpdateEnvVar)
						r.With(require.AgentUse(iam.ActionAgentsWrite)).Delete("/{agentId}/env-vars/{envVarId}", deps.Agent.DeleteEnvVar)

						// Chat sessions. A session's messages ARE a conversation, so
						// these are gated on conversations:read/write, not agents.*
						// (which governs the agent entity's own configuration —
						// MCP servers, skills, env vars, etc). Starting a session
						// and sending into one both create/drive a conversation (a
						// real agent turn, possibly inside a live sandbox) — Write,
						// the same tier as every conversation-mutating route below.
						// Auto mode's resolve step — no {agentId} yet (that's the
						// whole point), so it checks the project.
						r.With(require.Project(iam.ActionConversationsWrite)).
							Post("/resolve-auto", deps.Agent.ResolveAutoAgent)
						// require.AgentUse authorizes on
						// project/{projectId}/agent/{agentId}, so a role scoped
						// to one agent applies (see guards). The handlers bind
						// the URL agent to the session/conversation they load.
						r.With(require.AgentUse(iam.ActionConversationsRead)).
							Get("/{agentId}/chat-sessions", deps.Agent.ListChatSessions)
						r.With(require.AgentUse(iam.ActionConversationsWrite), require.ChatEnvironment()).
							Post("/{agentId}/chat-sessions", deps.Agent.StartChatSession)
						r.With(require.AgentUse(iam.ActionConversationsWrite), require.SessionEnvironment()).
							Post("/{agentId}/chat-sessions/{sessionId}/messages", deps.Agent.SendChatMessage)
					})
				}

				// Project-wide annotation search — unlike the annotation
				// routes nested under one specific port forward (below,
				// inside Environments), this searches across every port
				// forward in the project. Backs BlockNote mention search,
				// the agent conversation's attach-context picker, and the
				// MCP list_annotations/get_annotation tools.
				if deps.Annotation != nil {
					r.Route("/annotations", func(r chi.Router) {
						r.With(require.Project(iam.ActionAnnotationsRead)).Get("/", deps.Annotation.SearchInProject)
						r.With(require.ProjectEntity("annotation", "annotationId", resAnnotation, iam.ActionAnnotationsRead)).Get("/{annotationId}", deps.Annotation.GetInProject)
					})
				}

				// Environments (static, long-lived sandboxes — see
				// docs/ai-agent/environment-management.md). Gated on their
				// own dedicated environments:read/write/connect permissions
				// rather than reusing agents:read/write: managing an
				// environment's configuration (Write) is a distinct
				// capability from gaining a live interactive session inside
				// it (Connect — terminal-ticket only), so the two need to be
				// grantable independently.
				if deps.Environment != nil {
					r.Route("/environments", func(r chi.Router) {
						r.With(require.Project(iam.ActionEnvironmentsRead)).Get("/", deps.Environment.ListEnvironments)
						r.With(require.Project(iam.ActionEnvironmentsWrite)).Post("/", deps.Environment.CreateEnvironment)
						r.With(require.Environment(iam.ActionEnvironmentsRead)).Get("/{environmentId}", deps.Environment.GetEnvironment)
						r.With(require.Environment(iam.ActionEnvironmentsWrite)).Patch("/{environmentId}", deps.Environment.UpdateEnvironment)
						r.With(require.Environment(iam.ActionEnvironmentsWrite)).Delete("/{environmentId}", deps.Environment.DeleteEnvironment)

						r.With(require.Environment(iam.ActionEnvironmentsWrite)).Post("/{environmentId}/start", deps.Environment.StartEnvironment)
						r.With(require.Environment(iam.ActionEnvironmentsWrite)).Post("/{environmentId}/stop", deps.Environment.StopEnvironment)
						r.With(require.Environment(iam.ActionEnvironmentsWrite)).Post("/{environmentId}/restart", deps.Environment.RestartEnvironment)
						r.With(require.Environment(iam.ActionEnvironmentsRead)).Post("/{environmentId}/heartbeat", deps.Environment.Heartbeat)
						// Read-gated like Browse/ListFolders above — a live
						// probe, not a mutating action, and the create-agent
						// dialog's own "Verify login" button needs this before
						// the agent (and its own agents:write-gated verify
						// endpoint) exists yet — see EnvironmentHandler.
						// VerifyCLILogin's own doc comment.
						r.With(require.Environment(iam.ActionEnvironmentsRead)).Post("/{environmentId}/verify-cli-login", deps.Environment.VerifyCLILogin)
						// Every {environmentId} route is authorized on
						// project/{projectId}/environment/{environmentId}
						// (require.Environment), so an environment-scoped role
						// applies to all of them.

						// Mints a ticket for agent-runner's live-usage
						// WebSocket (internal/acpbridge/stats.go). Not a
						// mutating action, but it does hand the caller a live
						// feed of what's running inside the container, so it
						// is gated on the environment resource like the rest.
						r.With(require.Environment(iam.ActionEnvironmentsRead)).Post("/{environmentId}/stats-ticket", deps.Environment.StatsTicket)

						// Folders
						r.With(require.Environment(iam.ActionEnvironmentsRead)).Get("/{environmentId}/folders", deps.Environment.ListFolders)
						r.With(require.Environment(iam.ActionEnvironmentsWrite)).Post("/{environmentId}/folders", deps.Environment.AddFolder)
						r.With(require.Environment(iam.ActionEnvironmentsWrite)).Delete("/{environmentId}/folders/{folderId}", deps.Environment.DeleteFolder)
						r.With(require.Environment(iam.ActionEnvironmentsRead)).Get("/{environmentId}/browse", deps.Environment.BrowseFolder)

						// SSH keys — registering/removing a key grants shell
						// access the same way the terminal ticket below does
						// (the ssh command connects as root), so this is
						// gated on Connect, not Write, mirroring
						// TerminalTicket: a Write-only member can configure
						// the environment but shouldn't be able to mint
						// themselves shell access this way, and a
						// Connect-only member gains nothing new here since
						// they can already open a root shell via the
						// browser terminal.
						r.With(require.Environment(iam.ActionEnvironmentsRead)).Get("/{environmentId}/ssh-keys", deps.Environment.ListSSHKeys)
						r.With(require.Environment(iam.ActionEnvironmentsConnect)).Post("/{environmentId}/ssh-keys", deps.Environment.AddSSHKey)
						r.With(require.Environment(iam.ActionEnvironmentsConnect)).Delete("/{environmentId}/ssh-keys/{keyId}", deps.Environment.DeleteSSHKey)

						// Port forwards
						r.With(require.Environment(iam.ActionEnvironmentsRead)).Get("/{environmentId}/port-forwards", deps.Environment.ListPortForwards)
						r.With(require.Environment(iam.ActionEnvironmentsWrite)).Post("/{environmentId}/port-forwards", deps.Environment.AddPortForward)
						r.With(require.Environment(iam.ActionEnvironmentsRead)).
							Get("/{environmentId}/port-forwards/{portForwardId}", deps.Environment.GetPortForward)
						r.With(require.Environment(iam.ActionEnvironmentsWrite)).
							Delete("/{environmentId}/port-forwards/{portForwardId}", deps.Environment.DeletePortForward)

						// Browser terminal — a minted ticket grants an
						// interactive shell inside the environment's
						// container, so this is gated on the dedicated
						// Connect permission, not Write: being able to
						// configure an environment doesn't by itself imply
						// being able to open a shell inside it.
						r.With(require.Environment(iam.ActionEnvironmentsConnect)).Post("/{environmentId}/terminal-ticket", deps.Environment.TerminalTicket)

						// Page annotations — on-page comments pinned via the
						// Paca browser extension (apps/extension), created
						// directly against this API from a content script
						// running on the environment's own forwarded preview
						// page (see corsMiddleware's same-hostname branch
						// below for how that's authenticated). Nested under
						// the specific port forward, not the environment
						// directly: a comment belongs to one port forward's
						// running app, and an environment can have several.
						// Resolve is a separate tier from Write, same
						// reasoning as Connect above: triaging/dismissing a
						// comment shouldn't require the ability to author or
						// delete one. Gated on the environment resource
						// (require.Environment), except create-task's
						// tasks:write, which is about the project's tasks and
						// stays a project gate. Routes on one annotation are
						// also authorized on the annotation itself
						// (project/{projectId}/annotation/{annotationId}), so a
						// Deny on it or a role limited to some annotations
						// applies; the two list routes are filtered to the
						// readable annotations in SQL (see AnnotationHandler).
						if deps.Annotation != nil {
							r.Route("/{environmentId}/port-forwards/{portForwardId}/annotations", func(r chi.Router) {
								r.With(require.Environment(iam.ActionAnnotationsRead)).Get("/", deps.Annotation.List)
								// The four POST routes below all decode their
								// body with plain encoding/json, which parses
								// JSON regardless of the declared Content-Type
								// — so httpmw.RequireJSONContentType is what
								// actually makes that header meaningful. These
								// routes are reachable via the SameSite=None
								// domainauth.ScopeAnnotation cookie (see
								// AuthHandler.setAnnotationTokenCookies and
								// httpmw.AnnotationExtensionPathPattern);
								// without this, a cross-site request using a
								// CORS-safelisted Content-Type (e.g.
								// text/plain) would count as a "simple
								// request" under the Fetch spec, skip CORS
								// preflight entirely, and still be parsed —
								// cookie attached — regardless of
								// corsMiddleware's same-hostname check, which
								// only ever gates a *preflighted* request.
								r.With(require.Environment(iam.ActionAnnotationsWrite), httpmw.RequireJSONContentType()).Post("/", deps.Annotation.Create)
								r.With(require.Environment(iam.ActionAnnotationsWrite), httpmw.RequireJSONContentType()).
									Post("/upload-url", deps.Annotation.InitiateScreenshotUpload)
								r.With(require.Environment(iam.ActionAnnotationsRead), require.ProjectEntity("annotation", "annotationId", resAnnotation, iam.ActionAnnotationsRead)).Get("/{annotationId}", deps.Annotation.Get)
								r.With(require.Environment(iam.ActionAnnotationsWrite), require.ProjectEntity("annotation", "annotationId", resAnnotation, iam.ActionAnnotationsWrite), httpmw.RequireJSONContentType()).
									Post("/{annotationId}/complete-upload", deps.Annotation.CompleteScreenshotUpload)
								r.With(require.Environment(iam.ActionAnnotationsRead), require.ProjectEntity("annotation", "annotationId", resAnnotation, iam.ActionAnnotationsRead)).Get("/{annotationId}/screenshot-url", deps.Annotation.GetScreenshotURL)
								r.With(require.Environment(iam.ActionAnnotationsResolve), require.ProjectEntity("annotation", "annotationId", resAnnotation, iam.ActionAnnotationsResolve)).Patch("/{annotationId}/resolve", deps.Annotation.Resolve)
								r.With(require.Environment(iam.ActionAnnotationsResolve), require.ProjectEntity("annotation", "annotationId", resAnnotation, iam.ActionAnnotationsResolve)).Patch("/{annotationId}/reopen", deps.Annotation.Reopen)
								r.With(require.Environment(iam.ActionAnnotationsWrite), require.ProjectEntity("annotation", "annotationId", resAnnotation, iam.ActionAnnotationsWrite), httpmw.RequireJSONContentType()).
									Post("/{annotationId}/comments", deps.Annotation.AddComment)
								r.With(require.Environment(iam.ActionAnnotationsWrite), require.ProjectEntity("annotation", "annotationId", resAnnotation, iam.ActionAnnotationsWrite), require.Project(iam.ActionTasksWrite), httpmw.RequireJSONContentType()).
									Post("/{annotationId}/create-task", deps.Annotation.CreateTask)
							})
						}
					})
				}

				// Conversations. Gated on their own conversations:read/write
				// permissions (not agents.*, which governs the agent entity's
				// configuration — see the chat-sessions block above for the
				// same split).
				if deps.Conversation != nil {
					r.Route("/conversations", func(r chi.Router) {
						r.With(require.Project(iam.ActionConversationsRead)).Get("/", deps.Conversation.ListConversations)
						r.With(require.ProjectEntity("conversation", "conversationId", resConversation, iam.ActionConversationsRead)).Get("/{conversationId}", deps.Conversation.GetConversation)
						r.With(require.ProjectEntity("conversation", "conversationId", resConversation, iam.ActionConversationsRead)).Get("/{conversationId}/events", deps.Conversation.ListConversationEvents)
						r.With(require.ProjectEntity("conversation", "conversationId", resConversation, iam.ActionConversationsWrite)).Post("/{conversationId}/stop", deps.Conversation.StopConversation)
						r.With(require.ProjectEntity("conversation", "conversationId", resConversation, iam.ActionConversationsWrite)).Post("/{conversationId}/pause", deps.Conversation.PauseConversation)
						// Heartbeat deliberately stays Read: it's a passive
						// "I'm still watching this" keep-alive (see
						// Service.Heartbeat's doc comment) fired by any open tab,
						// not a control action — a viewer legitimately watching a
						// running conversation shouldn't cause it to idle-timeout
						// out from under them.
						r.With(require.ProjectEntity("conversation", "conversationId", resConversation, iam.ActionConversationsRead)).Post("/{conversationId}/heartbeat", deps.Conversation.Heartbeat)
						// Write, not Read: sending a message resumes/drives the
						// conversation (dispatches a real agent turn), the same
						// capability tier as stop/pause above — a viewer (read
						// only) must not be able to steer a conversation just
						// because they can see it.
						r.With(require.ProjectEntity("conversation", "conversationId", resConversation, iam.ActionConversationsWrite)).Post("/{conversationId}/messages", deps.Conversation.SendConversationMessage)
						// Rename/delete — same Write tier as stop/pause/messages above.
						r.With(require.ProjectEntity("conversation", "conversationId", resConversation, iam.ActionConversationsWrite), httpmw.RequireJSONContentType()).
							Patch("/{conversationId}", deps.Conversation.UpdateConversation)
						r.With(require.ProjectEntity("conversation", "conversationId", resConversation, iam.ActionConversationsWrite)).Delete("/{conversationId}", deps.Conversation.DeleteConversation)
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
				// Gated on plugins:read/write (previously borrowed
				// users:write as a rough "is this someone important" proxy
				// — there was no dedicated permission for plugin
				// management).
				r.Route("/admin/plugins", func(r chi.Router) {
					r.Use(httpmw.Authn(deps.TokenManager, deps.APIKeyAuth))
					r.Use(httpmw.RequireFreshPassword())
					r.With(require.Global(iam.ActionPluginsRead)).Get("/marketplace", deps.Plugin.ListMarketplacePlugins)
					r.With(require.Global(iam.ActionPluginsWrite)).Post("/marketplace/install", deps.Plugin.InstallMarketplacePlugin)
					r.With(require.Global(iam.ActionPluginsWrite)).Post("/", deps.Plugin.InstallPlugin)
					r.With(require.Global(iam.ActionPluginsWrite)).Patch("/{pluginId}", deps.Plugin.UpdatePlugin)
					r.With(require.Global(iam.ActionPluginsWrite)).Post("/{pluginId}/upgrade", deps.Plugin.UpgradeMarketplacePlugin)
					r.With(require.Global(iam.ActionPluginsWrite)).Delete("/{pluginId}", deps.Plugin.DeletePlugin)
				})

				// Admin extension settings
				r.Route("/admin/plugin-extension-settings", func(r chi.Router) {
					r.Use(httpmw.Authn(deps.TokenManager, deps.APIKeyAuth))
					r.Use(httpmw.RequireFreshPassword())
					r.Use(require.Global(iam.ActionPluginsWrite))
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
//
// One narrow exception, checked before either of those: a request whose
// Origin has the same hostname as this server's own Host header (port
// ignored) *and* whose path matches httpmw.AnnotationExtensionPathPattern
// gets its exact Origin echoed back with Access-Control-Allow-Credentials:
// true, regardless of CORS_ORIGINS. This is what lets the Paca browser
// extension's content script — running directly on a forwarded environment
// port, e.g. paca.example.com:31842 — call this API with `credentials:
// "include"` and actually have a token attached: cookies are scoped by
// hostname, not by port, so the browser already holds one there. Which
// token depends on scheme, though: SameSite ignores port but NOT scheme
// (modern browsers' "Schemeful Same-Site"), so access_token/refresh_token
// (SameSite=Lax/Strict) only ride along when the forwarded port happens to
// share the Paca app's own scheme; the domainauth.ScopeAnnotation pair
// (SameSite=None — see AuthHandler.setAnnotationTokenCookies) is what
// covers the far more common case where it doesn't. Either way, without
// this branch the *response* would still be blocked from the extension's
// own JS by CORS, cookies notwithstanding.
//
// The path check matters as much as the hostname check: the forwarded port
// serves the *user's own dev app*, not code Paca controls, so any script
// running there — not just the extension's content script — can make this
// exact same credentialed request. Scoping the exception to
// httpmw.AnnotationExtensionPathPattern means that page can, at most, act
// on page annotations (and read its own port-forward's identity) on the
// caller's behalf; without this scoping it would get free, ambient,
// full-API access to every account the caller happens to be signed in as
// (projects, tasks, docs, admin routes, ...) just because they had that
// page open — and for a ScopeAnnotation token specifically,
// middleware.applyAuthn enforces the exact same path restriction
// independently of CORS, so it's held even if this Origin check were ever
// loosened. Also can't be widened by CORS_ORIGINS — the pattern is fixed at
// compile time, not configuration.
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
			case origin != "" && sameHostnameOrigin(origin, r.Host) &&
				httpmw.AnnotationExtensionPathPattern.MatchString(r.URL.Path):
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Vary", "Origin")
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

// sameHostnameOrigin reports whether origin (a request's Origin header,
// e.g. "https://paca.example.com:31842") has the same hostname as host (a
// request's Host header, e.g. "paca.example.com" or "paca.example.com:443")
// — ignoring port on both sides, and ignoring scheme entirely (a cookie's
// Secure flag, not this check, is what actually gates whether it's ever
// sent over plain HTTP in the first place). An unparseable origin never
// matches.
func sameHostnameOrigin(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Hostname() == "" {
		return false
	}
	reqHost := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		reqHost = h
	}
	return u.Hostname() == reqHost
}
