package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/Paca-AI/api/internal/platform/authz"
	agentsvc "github.com/Paca-AI/api/internal/service/agent"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// aiAgentHTTPTimeout bounds every call this handler makes into the
// agent-runner service (LLM model listing, ACP bridge status/disconnect) so
// a slow or wedged agent-runner instance can't hang the calling request
// indefinitely.
const aiAgentHTTPTimeout = 10 * time.Second

type agentActivityRecorder interface {
	RecordActivity(ctx context.Context, in taskdom.RecordActivityInput) error
}

// agentGlobalPermissionReader resolves a global agent's own effective
// global-scope permissions. Satisfied directly by *postgres.AuthzPermissionStore
// (the same store instance the Authorizer itself uses).
type agentGlobalPermissionReader interface {
	ListAgentGlobalPermissions(ctx context.Context, agentID uuid.UUID) ([]authz.Permission, error)
}

// AgentHandler handles AI agent management endpoints.
type AgentHandler struct {
	svc                agentdom.Service
	aiAgentURL         string
	aiAgentInternalKey string
	publicURL          string
	httpClient         *http.Client
	activityRec        agentActivityRecorder
	memberRepo         projectdom.MemberRepository
	globalPermReader   agentGlobalPermissionReader
	avatarSvc          attachmentdom.AvatarService
	taskChecker        attachmentdom.TaskOwnerChecker
}

// NewAgentHandler returns an AgentHandler wired to the agent service.
// publicURL is the externally reachable base URL (e.g. https://paca.example.com)
// used to build the local-bridge "run this command" snippet. aiAgentInternalKey
// authenticates calls into agent-runner's internal-only routes and must match
// agent-runner's own INTERNAL_API_KEY.
func NewAgentHandler(svc agentdom.Service, aiAgentURL, aiAgentInternalKey, publicURL string) *AgentHandler {
	return &AgentHandler{
		svc:                svc,
		aiAgentURL:         aiAgentURL,
		aiAgentInternalKey: aiAgentInternalKey,
		publicURL:          publicURL,
		httpClient:         &http.Client{Timeout: aiAgentHTTPTimeout},
	}
}

// WithActivityRecorder attaches an activity recorder so that an
// "agent.session.started" activity is recorded when a description-write is triggered.
func (h *AgentHandler) WithActivityRecorder(r agentActivityRecorder) *AgentHandler {
	h.activityRec = r
	return h
}

// WithMemberRepo attaches the project member repository used to resolve the
// authenticated caller's project_members.id (see resolveMemberID).
func (h *AgentHandler) WithMemberRepo(repo projectdom.MemberRepository) *AgentHandler {
	h.memberRepo = repo
	return h
}

// WithGlobalPermissionReader attaches the store used by
// GetMyGlobalPermissions to resolve a calling global agent's own
// global-scope permissions.
func (h *AgentHandler) WithGlobalPermissionReader(reader agentGlobalPermissionReader) *AgentHandler {
	h.globalPermReader = reader
	return h
}

// WithAvatarService configures avatar URL resolution for AgentResponse.
func (h *AgentHandler) WithAvatarService(svc attachmentdom.AvatarService) *AgentHandler {
	h.avatarSvc = svc
	return h
}

// WithTaskChecker attaches the checker used by WriteTaskDescriptionWithAI to
// verify the target task belongs to the caller's authorized project (the
// agent service itself has no task-repository dependency to do this).
func (h *AgentHandler) WithTaskChecker(checker attachmentdom.TaskOwnerChecker) *AgentHandler {
	h.taskChecker = checker
	return h
}

// toAgentResponse maps ag to an AgentResponse and, if an AvatarService is
// configured, resolves its avatar keys into presigned display URLs.
func (h *AgentHandler) toAgentResponse(ctx context.Context, ag *agentdom.Agent) dto.AgentResponse {
	resp := dto.AgentFromEntity(ag)
	if h.avatarSvc != nil {
		resp.AvatarURL, _ = h.avatarSvc.ResolveAvatarURL(ctx, ag.AvatarKey)
		resp.AvatarThumbURL, _ = h.avatarSvc.ResolveAvatarURL(ctx, ag.AvatarThumbKey)
	}
	return resp
}

// callerUserID extracts the authenticated human user's ID from the
// request's JWT claims. Used by the global-chat handlers, which have no
// project context and therefore no project_members.id to resolve (unlike
// resolveMemberID) — the caller's raw user ID is the actor identity.
func callerUserID(r *http.Request) (uuid.UUID, error) {
	claims := middleware.ClaimsFrom(r)
	if claims == nil {
		return uuid.Nil, apierr.New(apierr.CodeUnauthenticated, "unauthenticated")
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid subject claim")
	}
	return userID, nil
}

// resolveMemberID maps the authenticated caller's user ID to their
// project_members.id within projectID. agent_chat_sessions.member_id and
// agent_conversations.triggered_by_member_id both store the acting project
// member rather than the raw user ID -- the latter has a foreign key to
// project_members(id) -- so callers must not use claims.Subject directly.
func (h *AgentHandler) resolveMemberID(r *http.Request, projectID uuid.UUID) (uuid.UUID, error) {
	if h.memberRepo == nil {
		return uuid.Nil, apierr.New(apierr.CodeInternalError, "member resolver not available")
	}
	claims := middleware.ClaimsFrom(r)
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid subject claim")
	}
	member, err := h.memberRepo.FindMemberByUserProject(r.Context(), userID, projectID)
	if err != nil {
		return uuid.Nil, err
	}
	return member.ID, nil
}

// --- Agents -----------------------------------------------------------------

// ListAgents handles GET /projects/:projectId/agents. An optional ?scope=
// query param ("project" or "global") narrows the result to just that
// AgentScope — e.g. the project Agents management page passes
// scope=project to hide invited global agents it can't manage from there;
// callers that want every agent the project can act through (the @mention
// picker, task assignment) omit it. See AgentRepository.ListAgents.
func (h *AgentHandler) ListAgents(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	scope := agentdom.AgentScope(r.URL.Query().Get("scope"))
	if scope != "" && scope != agentdom.AgentScopeProject && scope != agentdom.AgentScopeGlobal {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid scope"))
		return
	}
	agents, err := h.svc.ListAgents(r.Context(), projectID, scope)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.AgentResponse, 0, len(agents))
	for _, a := range agents {
		resp = append(resp, h.toAgentResponse(r.Context(), a))
	}
	presenter.OK(w, r, map[string]any{"items": resp})
}

// GetAgent handles GET /projects/:projectId/agents/:agentId.
func (h *AgentHandler) GetAgent(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	agentID, err := parseParamUUID(r, "agentId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	a, err := h.svc.GetAgent(r.Context(), projectID, agentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, h.toAgentResponse(r.Context(), a))
}

// CreateAgent handles POST /projects/:projectId/agents.
func (h *AgentHandler) CreateAgent(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.CreateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	agentType := req.AgentType
	if agentType == "" {
		agentType = agentdom.AgentTypeLLM
	}
	switch {
	case req.Name == "":
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "name is required"))
		return
	case req.Handle == "":
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "handle is required"))
		return
	case req.ProjectRoleID == uuid.Nil:
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "project_role_id is required"))
		return
	}
	switch agentType {
	case agentdom.AgentTypeLLM:
		// llm_base_url is intentionally not required here: the agents table
		// column defaults to '' (see migration 000022's note on this), and
		// several LLM providers resolve a default base URL on their own —
		// requiring it at the API layer would reject otherwise-valid
		// requests that rely on that default.
		switch {
		case req.LLMProvider == "":
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "llm_provider is required"))
			return
		case req.LLMModel == "":
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "llm_model is required"))
			return
		case req.LLMAPIKey == "":
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "llm_api_key is required"))
			return
		}
	case agentdom.AgentTypeACP:
		if req.ACPProvider == "" {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "acp_provider is required"))
			return
		}
	case agentdom.AgentTypeProviderCLI:
		switch {
		case req.CLIProvider == "":
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "cli_provider is required"))
			return
		case req.DefaultEnvironmentID == nil || *req.DefaultEnvironmentID == uuid.Nil:
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "default_environment_id is required for provider_cli agents"))
			return
		}
	default:
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "agent_type must be one of: llm, acp, provider_cli"))
		return
	}
	claims := middleware.ClaimsFrom(r)
	callerID, _ := uuid.Parse(claims.Subject)

	a, err := h.svc.CreateAgent(r.Context(), projectID, agentdom.CreateAgentInput{
		Name:                 req.Name,
		Handle:               req.Handle,
		AgentType:            agentType,
		LLMProvider:          req.LLMProvider,
		LLMModel:             req.LLMModel,
		LLMAPIKey:            req.LLMAPIKey,
		LLMBaseURL:           req.LLMBaseURL,
		ACPProvider:          req.ACPProvider,
		ACPCommand:           req.ACPCommand,
		CLIProvider:          req.CLIProvider,
		CLIModel:             req.CLIModel,
		CLIAuthMode:          req.CLIAuthMode,
		CLIAPIKey:            req.CLIAPIKey,
		SystemPrompt:         req.SystemPrompt,
		MaxIterations:        req.MaxIterations,
		TimeoutMinutes:       req.TimeoutMinutes,
		ParallelismLimit:     req.ParallelismLimit,
		GitCommitterName:     req.GitCommitterName,
		GitCommitterEmail:    req.GitCommitterEmail,
		DockerEnabled:        req.DockerEnabled,
		DefaultEnvironmentID: req.DefaultEnvironmentID,
		DefaultFolderID:      req.DefaultFolderID,
		ProjectRoleID:        req.ProjectRoleID,
		CreatedBy:            &callerID,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, h.toAgentResponse(r.Context(), a))
}

// UpdateAgent handles PATCH /projects/:projectId/agents/:agentId.
func (h *AgentHandler) UpdateAgent(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	agentID, err := parseParamUUID(r, "agentId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.UpdateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	a, err := h.svc.UpdateAgent(r.Context(), projectID, agentID, agentdom.UpdateAgentInput{
		Name:                 req.Name,
		Handle:               req.Handle,
		LLMProvider:          req.LLMProvider,
		LLMModel:             req.LLMModel,
		LLMAPIKey:            req.LLMAPIKey,
		LLMBaseURL:           req.LLMBaseURL,
		ACPProvider:          req.ACPProvider,
		ACPCommand:           req.ACPCommand,
		CLIProvider:          req.CLIProvider,
		CLIModel:             req.CLIModel,
		CLIAuthMode:          req.CLIAuthMode,
		CLIAPIKey:            req.CLIAPIKey,
		SystemPrompt:         req.SystemPrompt,
		MaxIterations:        req.MaxIterations,
		TimeoutMinutes:       req.TimeoutMinutes,
		ParallelismLimit:     req.ParallelismLimit,
		GitCommitterName:     req.GitCommitterName,
		GitCommitterEmail:    req.GitCommitterEmail,
		DockerEnabled:        req.DockerEnabled,
		DefaultEnvironmentID: req.DefaultEnvironmentID,
		DefaultFolderID:      req.DefaultFolderID,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, h.toAgentResponse(r.Context(), a))
}

// DeleteAgent handles DELETE /projects/:projectId/agents/:agentId.
func (h *AgentHandler) DeleteAgent(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	agentID, err := parseParamUUID(r, "agentId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.DeleteAgent(r.Context(), projectID, agentID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"message": "agent deleted"})
}

// --- Global Agents (admin) ---------------------------------------------------

// ListGlobalAgents handles GET /admin/agents.
func (h *AgentHandler) ListGlobalAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := h.svc.ListGlobalAgents(r.Context())
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.AgentResponse, 0, len(agents))
	for _, a := range agents {
		resp = append(resp, h.toAgentResponse(r.Context(), a))
	}
	presenter.OK(w, r, map[string]any{"items": resp})
}

// GetGlobalAgent handles GET /admin/agents/:agentId.
func (h *AgentHandler) GetGlobalAgent(w http.ResponseWriter, r *http.Request) {
	agentID, err := parseParamUUID(r, "agentId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	a, err := h.svc.GetGlobalAgent(r.Context(), agentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, h.toAgentResponse(r.Context(), a))
}

// CreateGlobalAgent handles POST /admin/agents.
func (h *AgentHandler) CreateGlobalAgent(w http.ResponseWriter, r *http.Request) {
	var req dto.CreateGlobalAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	agentType := req.AgentType
	if agentType == "" {
		agentType = agentdom.AgentTypeLLM
	}
	switch {
	case req.Name == "":
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "name is required"))
		return
	case req.Handle == "":
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "handle is required"))
		return
	}
	switch agentType {
	case agentdom.AgentTypeLLM:
		switch {
		case req.LLMProvider == "":
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "llm_provider is required"))
			return
		case req.LLMModel == "":
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "llm_model is required"))
			return
		case req.LLMAPIKey == "":
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "llm_api_key is required"))
			return
		}
	case agentdom.AgentTypeACP:
		if req.ACPProvider == "" {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "acp_provider is required"))
			return
		}
	default:
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "agent_type must be one of: llm, acp"))
		return
	}
	claims := middleware.ClaimsFrom(r)
	callerID, _ := uuid.Parse(claims.Subject)

	a, err := h.svc.CreateGlobalAgent(r.Context(), agentdom.CreateGlobalAgentInput{
		Name:              req.Name,
		Handle:            req.Handle,
		AgentType:         agentType,
		LLMProvider:       req.LLMProvider,
		LLMModel:          req.LLMModel,
		LLMAPIKey:         req.LLMAPIKey,
		LLMBaseURL:        req.LLMBaseURL,
		ACPProvider:       req.ACPProvider,
		ACPCommand:        req.ACPCommand,
		SystemPrompt:      req.SystemPrompt,
		MaxIterations:     req.MaxIterations,
		TimeoutMinutes:    req.TimeoutMinutes,
		ParallelismLimit:  req.ParallelismLimit,
		GitCommitterName:  req.GitCommitterName,
		GitCommitterEmail: req.GitCommitterEmail,
		DockerEnabled:     req.DockerEnabled,
		GlobalRoleID:      req.GlobalRoleID,
		CreatedBy:         &callerID,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, h.toAgentResponse(r.Context(), a))
}

// UpdateGlobalAgent handles PATCH /admin/agents/:agentId.
func (h *AgentHandler) UpdateGlobalAgent(w http.ResponseWriter, r *http.Request) {
	agentID, err := parseParamUUID(r, "agentId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.UpdateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	a, err := h.svc.UpdateGlobalAgent(r.Context(), agentID, agentdom.UpdateAgentInput{
		Name:              req.Name,
		Handle:            req.Handle,
		LLMProvider:       req.LLMProvider,
		LLMModel:          req.LLMModel,
		LLMAPIKey:         req.LLMAPIKey,
		LLMBaseURL:        req.LLMBaseURL,
		ACPProvider:       req.ACPProvider,
		ACPCommand:        req.ACPCommand,
		SystemPrompt:      req.SystemPrompt,
		MaxIterations:     req.MaxIterations,
		TimeoutMinutes:    req.TimeoutMinutes,
		ParallelismLimit:  req.ParallelismLimit,
		GitCommitterName:  req.GitCommitterName,
		GitCommitterEmail: req.GitCommitterEmail,
		DockerEnabled:     req.DockerEnabled,
		GlobalRoleID:      req.GlobalRoleID,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, h.toAgentResponse(r.Context(), a))
}

// DeleteGlobalAgent handles DELETE /admin/agents/:agentId.
func (h *AgentHandler) DeleteGlobalAgent(w http.ResponseWriter, r *http.Request) {
	agentID, err := parseParamUUID(r, "agentId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.DeleteGlobalAgent(r.Context(), agentID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"message": "agent deleted"})
}

// GetMyGlobalPermissions handles GET /agents/me/global-permissions.
// Agent-API-key-authenticated only (X-Agent-ID header) — returns the
// calling global agent's own effective global-scope permissions, resolved
// via its global_role_id. This is what the MCP server (services/ai-agent)
// calls to populate its permission map for a global-scope conversation.
func (h *AgentHandler) GetMyGlobalPermissions(w http.ResponseWriter, r *http.Request) {
	agentID, ok := middleware.AgentIDFromRequest(r)
	if !ok {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "agent identity required"))
		return
	}
	if h.globalPermReader == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "permission reader not available"))
		return
	}
	perms, err := h.globalPermReader.ListAgentGlobalPermissions(r.Context(), agentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	out := make([]string, 0, len(perms))
	for _, p := range perms {
		out = append(out, string(p))
	}
	presenter.OK(w, r, map[string]any{"permissions": out})
}

// GetMyInvitedProjects handles GET /agents/me/projects. Agent-API-key
// authenticated only — returns the IDs of every project the calling global
// agent currently has an active membership in. Used by the MCP server to
// discover which projects to probe for per-project permissions when running
// in unpinned (global chat) mode.
func (h *AgentHandler) GetMyInvitedProjects(w http.ResponseWriter, r *http.Request) {
	agentID, ok := middleware.AgentIDFromRequest(r)
	if !ok {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "agent identity required"))
		return
	}
	projectIDs, err := h.svc.ListInvitedProjectIDs(r.Context(), agentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"project_ids": projectIDs})
}

// --- MCP Servers ------------------------------------------------------------

// parseAgentForProject parses projectId and agentId, verifies the agent belongs
// to the project, and returns both IDs. Handlers that operate on agent sub-resources
// (MCP servers, skills) must call this instead of parsing agentId alone.
func (h *AgentHandler) parseAgentForProject(r *http.Request) (projectID, agentID uuid.UUID, err error) {
	projectID, err = parseProjectID(r)
	if err != nil {
		return
	}
	agentID, err = parseParamUUID(r, "agentId")
	if err != nil {
		return
	}
	// Verify the agent belongs to the project (prevents cross-project access).
	if _, err = h.svc.GetAgent(r.Context(), projectID, agentID); err != nil {
		return
	}
	return
}

// parseGlobalAgent parses agentId and verifies it identifies a global-scope
// agent. Sibling of parseAgentForProject for the admin-mounted sub-resource
// routes (MCP servers, skills, env vars) on a global agent.
func (h *AgentHandler) parseGlobalAgent(r *http.Request) (agentID uuid.UUID, err error) {
	agentID, err = parseParamUUID(r, "agentId")
	if err != nil {
		return uuid.Nil, err
	}
	if _, err = h.svc.GetGlobalAgent(r.Context(), agentID); err != nil {
		return uuid.Nil, err
	}
	return agentID, nil
}

// ListMCPServers handles GET /projects/:projectId/agents/:agentId/mcp-servers.
func (h *AgentHandler) ListMCPServers(w http.ResponseWriter, r *http.Request) {
	_, agentID, err := h.parseAgentForProject(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	servers, err := h.svc.ListMCPServers(r.Context(), agentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.AgentMCPServerResponse, 0, len(servers))
	for _, s := range servers {
		resp = append(resp, dto.MCPServerFromEntity(s))
	}
	presenter.OK(w, r, map[string]any{"items": resp})
}

// AddMCPServer handles POST /projects/:projectId/agents/:agentId/mcp-servers.
func (h *AgentHandler) AddMCPServer(w http.ResponseWriter, r *http.Request) {
	_, agentID, err := h.parseAgentForProject(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.AddMCPServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	if req.ServerName == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "server_name is required"))
		return
	}
	switch req.Transport {
	case "stdio", "sse", "http":
	default:
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "transport must be one of: stdio, sse, http"))
		return
	}
	srv, err := h.svc.AddMCPServer(r.Context(), agentID, agentdom.AddMCPServerInput{
		ServerName: req.ServerName,
		Transport:  req.Transport,
		Command:    req.Command,
		Args:       req.Args,
		URL:        req.URL,
		Env:        req.Env,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.MCPServerFromEntity(srv))
}

// UpdateMCPServer handles PATCH /projects/:projectId/agents/:agentId/mcp-servers/:serverId.
func (h *AgentHandler) UpdateMCPServer(w http.ResponseWriter, r *http.Request) {
	_, agentID, err := h.parseAgentForProject(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	serverID, err := parseParamUUID(r, "serverId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.UpdateMCPServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	srv, err := h.svc.UpdateMCPServer(r.Context(), agentID, serverID, agentdom.UpdateMCPServerInput{
		Command:   req.Command,
		Args:      req.Args,
		URL:       req.URL,
		Env:       req.Env,
		IsEnabled: req.IsEnabled,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.MCPServerFromEntity(srv))
}

// DeleteMCPServer handles DELETE /projects/:projectId/agents/:agentId/mcp-servers/:serverId.
func (h *AgentHandler) DeleteMCPServer(w http.ResponseWriter, r *http.Request) {
	_, agentID, err := h.parseAgentForProject(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	serverID, err := parseParamUUID(r, "serverId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.DeleteMCPServer(r.Context(), agentID, serverID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"message": "mcp server deleted"})
}

// --- Global Agent MCP Servers (admin) ----------------------------------------

// ListGlobalAgentMCPServers handles GET /admin/agents/:agentId/mcp-servers.
func (h *AgentHandler) ListGlobalAgentMCPServers(w http.ResponseWriter, r *http.Request) {
	agentID, err := h.parseGlobalAgent(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	servers, err := h.svc.ListMCPServers(r.Context(), agentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.AgentMCPServerResponse, 0, len(servers))
	for _, s := range servers {
		resp = append(resp, dto.MCPServerFromEntity(s))
	}
	presenter.OK(w, r, map[string]any{"items": resp})
}

// AddGlobalAgentMCPServer handles POST /admin/agents/:agentId/mcp-servers.
func (h *AgentHandler) AddGlobalAgentMCPServer(w http.ResponseWriter, r *http.Request) {
	agentID, err := h.parseGlobalAgent(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.AddMCPServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	if req.ServerName == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "server_name is required"))
		return
	}
	switch req.Transport {
	case "stdio", "sse", "http":
	default:
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "transport must be one of: stdio, sse, http"))
		return
	}
	srv, err := h.svc.AddMCPServer(r.Context(), agentID, agentdom.AddMCPServerInput{
		ServerName: req.ServerName,
		Transport:  req.Transport,
		Command:    req.Command,
		Args:       req.Args,
		URL:        req.URL,
		Env:        req.Env,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.MCPServerFromEntity(srv))
}

// UpdateGlobalAgentMCPServer handles PATCH /admin/agents/:agentId/mcp-servers/:serverId.
func (h *AgentHandler) UpdateGlobalAgentMCPServer(w http.ResponseWriter, r *http.Request) {
	agentID, err := h.parseGlobalAgent(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	serverID, err := parseParamUUID(r, "serverId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.UpdateMCPServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	srv, err := h.svc.UpdateMCPServer(r.Context(), agentID, serverID, agentdom.UpdateMCPServerInput{
		Command:   req.Command,
		Args:      req.Args,
		URL:       req.URL,
		Env:       req.Env,
		IsEnabled: req.IsEnabled,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.MCPServerFromEntity(srv))
}

// DeleteGlobalAgentMCPServer handles DELETE /admin/agents/:agentId/mcp-servers/:serverId.
func (h *AgentHandler) DeleteGlobalAgentMCPServer(w http.ResponseWriter, r *http.Request) {
	agentID, err := h.parseGlobalAgent(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	serverID, err := parseParamUUID(r, "serverId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.DeleteMCPServer(r.Context(), agentID, serverID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"message": "mcp server deleted"})
}

// --- Skills -----------------------------------------------------------------

// ListSkills handles GET /projects/:projectId/agents/:agentId/skills.
func (h *AgentHandler) ListSkills(w http.ResponseWriter, r *http.Request) {
	_, agentID, err := h.parseAgentForProject(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	skills, err := h.svc.ListSkills(r.Context(), agentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.AgentSkillResponse, 0, len(skills))
	for _, s := range skills {
		resp = append(resp, dto.SkillFromEntity(s))
	}
	presenter.OK(w, r, map[string]any{"items": resp})
}

// AddSkill handles POST /projects/:projectId/agents/:agentId/skills.
func (h *AgentHandler) AddSkill(w http.ResponseWriter, r *http.Request) {
	_, agentID, err := h.parseAgentForProject(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.AddSkillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	if req.SkillName == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "skill_name is required"))
		return
	}
	switch req.SkillSource {
	case "inline", "marketplace", "github_url":
	default:
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "skill_source must be one of: inline, marketplace, github_url"))
		return
	}
	skill, err := h.svc.AddSkill(r.Context(), agentID, agentdom.AddSkillInput{
		SkillName:    req.SkillName,
		SkillSource:  req.SkillSource,
		SkillContent: req.SkillContent,
		SourceURL:    req.SourceURL,
		Triggers:     req.Triggers,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.SkillFromEntity(skill))
}

// UpdateSkill handles PATCH /projects/:projectId/agents/:agentId/skills/:skillId.
func (h *AgentHandler) UpdateSkill(w http.ResponseWriter, r *http.Request) {
	_, agentID, err := h.parseAgentForProject(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	skillID, err := parseParamUUID(r, "skillId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.UpdateSkillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	skill, err := h.svc.UpdateSkill(r.Context(), agentID, skillID, agentdom.UpdateSkillInput{
		SkillContent: req.SkillContent,
		Triggers:     req.Triggers,
		IsEnabled:    req.IsEnabled,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.SkillFromEntity(skill))
}

// DeleteSkill handles DELETE /projects/:projectId/agents/:agentId/skills/:skillId.
func (h *AgentHandler) DeleteSkill(w http.ResponseWriter, r *http.Request) {
	_, agentID, err := h.parseAgentForProject(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	skillID, err := parseParamUUID(r, "skillId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.DeleteSkill(r.Context(), agentID, skillID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"message": "skill deleted"})
}

// --- Global Agent Skills (admin) ---------------------------------------------

// ListGlobalAgentSkills handles GET /admin/agents/:agentId/skills.
func (h *AgentHandler) ListGlobalAgentSkills(w http.ResponseWriter, r *http.Request) {
	agentID, err := h.parseGlobalAgent(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	skills, err := h.svc.ListSkills(r.Context(), agentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.AgentSkillResponse, 0, len(skills))
	for _, s := range skills {
		resp = append(resp, dto.SkillFromEntity(s))
	}
	presenter.OK(w, r, map[string]any{"items": resp})
}

// AddGlobalAgentSkill handles POST /admin/agents/:agentId/skills.
func (h *AgentHandler) AddGlobalAgentSkill(w http.ResponseWriter, r *http.Request) {
	agentID, err := h.parseGlobalAgent(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.AddSkillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	if req.SkillName == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "skill_name is required"))
		return
	}
	switch req.SkillSource {
	case "inline", "marketplace", "github_url":
	default:
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "skill_source must be one of: inline, marketplace, github_url"))
		return
	}
	skill, err := h.svc.AddSkill(r.Context(), agentID, agentdom.AddSkillInput{
		SkillName:    req.SkillName,
		SkillSource:  req.SkillSource,
		SkillContent: req.SkillContent,
		SourceURL:    req.SourceURL,
		Triggers:     req.Triggers,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.SkillFromEntity(skill))
}

// UpdateGlobalAgentSkill handles PATCH /admin/agents/:agentId/skills/:skillId.
func (h *AgentHandler) UpdateGlobalAgentSkill(w http.ResponseWriter, r *http.Request) {
	agentID, err := h.parseGlobalAgent(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	skillID, err := parseParamUUID(r, "skillId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.UpdateSkillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	skill, err := h.svc.UpdateSkill(r.Context(), agentID, skillID, agentdom.UpdateSkillInput{
		SkillContent: req.SkillContent,
		Triggers:     req.Triggers,
		IsEnabled:    req.IsEnabled,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.SkillFromEntity(skill))
}

// DeleteGlobalAgentSkill handles DELETE /admin/agents/:agentId/skills/:skillId.
func (h *AgentHandler) DeleteGlobalAgentSkill(w http.ResponseWriter, r *http.Request) {
	agentID, err := h.parseGlobalAgent(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	skillID, err := parseParamUUID(r, "skillId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.DeleteSkill(r.Context(), agentID, skillID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"message": "skill deleted"})
}

// --- Environment Variables ---------------------------------------------------

// ListEnvVars handles GET /projects/:projectId/agents/:agentId/env-vars.
func (h *AgentHandler) ListEnvVars(w http.ResponseWriter, r *http.Request) {
	_, agentID, err := h.parseAgentForProject(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	envVars, err := h.svc.ListEnvVars(r.Context(), agentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.AgentEnvVarResponse, 0, len(envVars))
	for _, v := range envVars {
		resp = append(resp, dto.EnvVarFromEntity(v))
	}
	presenter.OK(w, r, map[string]any{"items": resp})
}

// AddEnvVar handles POST /projects/:projectId/agents/:agentId/env-vars.
func (h *AgentHandler) AddEnvVar(w http.ResponseWriter, r *http.Request) {
	_, agentID, err := h.parseAgentForProject(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.AddEnvVarRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	if req.Key == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "key is required"))
		return
	}
	if req.Value == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "value is required"))
		return
	}
	envVar, err := h.svc.AddEnvVar(r.Context(), agentID, agentdom.AddEnvVarInput{
		Key:   req.Key,
		Value: req.Value,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.EnvVarFromEntity(envVar))
}

// UpdateEnvVar handles PATCH /projects/:projectId/agents/:agentId/env-vars/:envVarId.
func (h *AgentHandler) UpdateEnvVar(w http.ResponseWriter, r *http.Request) {
	_, agentID, err := h.parseAgentForProject(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	envVarID, err := parseParamUUID(r, "envVarId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.UpdateEnvVarRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	if req.Value == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "value is required"))
		return
	}
	envVar, err := h.svc.UpdateEnvVar(r.Context(), agentID, envVarID, agentdom.UpdateEnvVarInput{
		Value: req.Value,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.EnvVarFromEntity(envVar))
}

// DeleteEnvVar handles DELETE /projects/:projectId/agents/:agentId/env-vars/:envVarId.
func (h *AgentHandler) DeleteEnvVar(w http.ResponseWriter, r *http.Request) {
	_, agentID, err := h.parseAgentForProject(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	envVarID, err := parseParamUUID(r, "envVarId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.DeleteEnvVar(r.Context(), agentID, envVarID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"message": "environment variable deleted"})
}

// --- Global Agent Environment Variables (admin) ------------------------------

// ListGlobalAgentEnvVars handles GET /admin/agents/:agentId/env-vars.
func (h *AgentHandler) ListGlobalAgentEnvVars(w http.ResponseWriter, r *http.Request) {
	agentID, err := h.parseGlobalAgent(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	envVars, err := h.svc.ListEnvVars(r.Context(), agentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.AgentEnvVarResponse, 0, len(envVars))
	for _, v := range envVars {
		resp = append(resp, dto.EnvVarFromEntity(v))
	}
	presenter.OK(w, r, map[string]any{"items": resp})
}

// AddGlobalAgentEnvVar handles POST /admin/agents/:agentId/env-vars.
func (h *AgentHandler) AddGlobalAgentEnvVar(w http.ResponseWriter, r *http.Request) {
	agentID, err := h.parseGlobalAgent(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.AddEnvVarRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	if req.Key == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "key is required"))
		return
	}
	if req.Value == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "value is required"))
		return
	}
	envVar, err := h.svc.AddEnvVar(r.Context(), agentID, agentdom.AddEnvVarInput{
		Key:   req.Key,
		Value: req.Value,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.EnvVarFromEntity(envVar))
}

// UpdateGlobalAgentEnvVar handles PATCH /admin/agents/:agentId/env-vars/:envVarId.
func (h *AgentHandler) UpdateGlobalAgentEnvVar(w http.ResponseWriter, r *http.Request) {
	agentID, err := h.parseGlobalAgent(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	envVarID, err := parseParamUUID(r, "envVarId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.UpdateEnvVarRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	if req.Value == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "value is required"))
		return
	}
	envVar, err := h.svc.UpdateEnvVar(r.Context(), agentID, envVarID, agentdom.UpdateEnvVarInput{
		Value: req.Value,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.EnvVarFromEntity(envVar))
}

// DeleteGlobalAgentEnvVar handles DELETE /admin/agents/:agentId/env-vars/:envVarId.
func (h *AgentHandler) DeleteGlobalAgentEnvVar(w http.ResponseWriter, r *http.Request) {
	agentID, err := h.parseGlobalAgent(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	envVarID, err := parseParamUUID(r, "envVarId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.DeleteEnvVar(r.Context(), agentID, envVarID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"message": "environment variable deleted"})
}

// --- Chat Sessions ----------------------------------------------------------

// ListChatSessions handles GET /projects/:projectId/agents/:agentId/chat-sessions.
func (h *AgentHandler) ListChatSessions(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	agentID, err := parseParamUUID(r, "agentId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	memberID, err := h.resolveMemberID(r, projectID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	sessions, err := h.svc.ListChatSessions(r.Context(), projectID, agentID, memberID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.AgentChatSessionResponse, 0, len(sessions))
	for _, s := range sessions {
		resp = append(resp, dto.ChatSessionFromEntity(s))
	}
	presenter.OK(w, r, map[string]any{"items": resp})
}

// StartChatSession handles POST /projects/:projectId/agents/:agentId/chat-sessions.
func (h *AgentHandler) StartChatSession(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	agentID, err := parseParamUUID(r, "agentId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.StartChatSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	if req.Message == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "message is required"))
		return
	}
	if err := agentdom.ValidateContextItems(req.ContextItems); err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, err.Error()))
		return
	}
	memberID, err := h.resolveMemberID(r, projectID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	session, conv, err := h.svc.StartChatSession(r.Context(), projectID, agentID, memberID, req.Message, req.EnvironmentID, req.FolderID, req.ContextItems, req.OnBusy)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, map[string]any{
		"session":      dto.ChatSessionFromEntity(session),
		"conversation": dto.ConversationFromEntity(conv),
	})
}

// SendChatMessage handles POST /projects/:projectId/agents/:agentId/chat-sessions/:sessionId/messages.
func (h *AgentHandler) SendChatMessage(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	sessionID, err := parseParamUUID(r, "sessionId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.SendChatMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	if req.Message == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "message is required"))
		return
	}
	if err := agentdom.ValidateContextItems(req.ContextItems); err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, err.Error()))
		return
	}
	memberID, err := h.resolveMemberID(r, projectID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	conv, err := h.svc.SendChatMessage(r.Context(), projectID, sessionID, memberID, req.Message, req.ContextItems, req.OnBusy)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, map[string]any{"conversation": dto.ConversationFromEntity(conv)})
}

// --- Global Chat Sessions -----------------------------------------------------
//
// Chatting with a global agent from the home page / admin pages — no
// project context. The caller is always a JWT-authenticated human; there is
// no project_members.id to resolve (unlike resolveMemberID above), only the
// raw user ID from the JWT subject (see callerUserID).

// ListGlobalChatSessions handles GET /agents/:agentId/chat-sessions (global).
func (h *AgentHandler) ListGlobalChatSessions(w http.ResponseWriter, r *http.Request) {
	agentID, err := parseParamUUID(r, "agentId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	userID, err := callerUserID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	sessions, err := h.svc.ListGlobalChatSessions(r.Context(), agentID, userID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.AgentChatSessionResponse, 0, len(sessions))
	for _, s := range sessions {
		resp = append(resp, dto.ChatSessionFromEntity(s))
	}
	presenter.OK(w, r, map[string]any{"items": resp})
}

// StartGlobalChatSession handles POST /agents/:agentId/chat-sessions (global).
func (h *AgentHandler) StartGlobalChatSession(w http.ResponseWriter, r *http.Request) {
	agentID, err := parseParamUUID(r, "agentId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.StartChatSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	if req.Message == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "message is required"))
		return
	}
	if err := agentdom.ValidateContextItems(req.ContextItems); err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, err.Error()))
		return
	}
	userID, err := callerUserID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	session, conv, err := h.svc.StartGlobalChatSession(r.Context(), agentID, userID, req.Message, req.ContextItems, req.OnBusy)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, map[string]any{
		"session":      dto.ChatSessionFromEntity(session),
		"conversation": dto.ConversationFromEntity(conv),
	})
}

// SendGlobalChatMessage handles POST /agents/chat-sessions/:sessionId/messages (global).
func (h *AgentHandler) SendGlobalChatMessage(w http.ResponseWriter, r *http.Request) {
	sessionID, err := parseParamUUID(r, "sessionId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.SendChatMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	if req.Message == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "message is required"))
		return
	}
	if err := agentdom.ValidateContextItems(req.ContextItems); err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, err.Error()))
		return
	}
	userID, err := callerUserID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	conv, err := h.svc.SendGlobalChatMessage(r.Context(), sessionID, userID, req.Message, req.ContextItems, req.OnBusy)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, map[string]any{"conversation": dto.ConversationFromEntity(conv)})
}

// --- Write with AI ----------------------------------------------------------

// WriteTaskDescriptionWithAI handles POST /projects/:projectId/tasks/:taskId/write-with-ai.
// It triggers the selected agent to write the description for the given task.
func (h *AgentHandler) WriteTaskDescriptionWithAI(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	taskID, err := parseParamUUID(r, "taskId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.WriteWithAIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	if req.AgentID == uuid.Nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "agent_id is required"))
		return
	}

	if h.taskChecker == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "task checker not configured"))
		return
	}
	if err := h.taskChecker.TaskBelongsToProject(r.Context(), projectID, taskID); err != nil {
		presenter.Error(w, r, err)
		return
	}

	memberID, err := h.resolveMemberID(r, projectID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	conv, err := h.svc.TriggerDescriptionWrite(r.Context(), projectID, req.AgentID, taskID, memberID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	if conv != nil && h.activityRec != nil {
		content, _ := json.Marshal(map[string]any{
			"conversation_id": conv.ID.String(),
			"agent_id":        req.AgentID.String(),
		})
		agentID := req.AgentID
		_ = h.activityRec.RecordActivity(r.Context(), taskdom.RecordActivityInput{
			TaskID:       taskID,
			ProjectID:    projectID,
			ActorAgentID: &agentID,
			ActivityType: taskdom.ActivityTypeAgentSessionStarted,
			Content:      content,
		})
	}

	presenter.Created(w, r, map[string]any{"conversation": dto.ConversationFromEntity(conv)})
}

// --- helpers ----------------------------------------------------------------

func parseParamUUID(r *http.Request, param string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, param))
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, fmt.Sprintf("invalid %s", param))
	}
	return id, nil
}

// --- Skill templates --------------------------------------------------------

// ListSkillTemplates handles GET /agents/skill-templates.
// Returns the hardcoded built-in skill template catalog.
func (h *AgentHandler) ListSkillTemplates(w http.ResponseWriter, r *http.Request) {
	templates := agentsvc.ListSkillTemplates()
	resp := make([]dto.SkillTemplateResponse, 0, len(templates))
	for _, t := range templates {
		resp = append(resp, dto.SkillTemplateFromEntity(t))
	}
	presenter.OK(w, r, resp)
}

// --- LLM models proxy -------------------------------------------------------

// GetLLMModels handles GET /agents/llm-models.
// It proxies the request to the ai-agent service and returns the verified
// LLM models grouped by provider.
func (h *AgentHandler) GetLLMModels(w http.ResponseWriter, r *http.Request) {
	if h.aiAgentURL == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "agent-runner service URL not configured"))
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, h.aiAgentURL+"/llm/models", nil)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "failed to create request"))
		return
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "failed to reach agent-runner service"))
		return
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "failed to read agent-runner response"))
		return
	}

	if resp.StatusCode != http.StatusOK {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "agent-runner service returned an error"))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// --- ACP local bridge --------------------------------------------------------

// GenerateACPBridgeToken handles POST
// /projects/:projectId/agents/:agentId/acp-bridge-token. It issues a new
// local-bridge auth token, returning the plaintext once — the caller must
// copy it now, since only its hash is persisted.
func (h *AgentHandler) GenerateACPBridgeToken(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	agentID, err := parseParamUUID(r, "agentId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	token, err := h.svc.GenerateACPBridgeToken(r.Context(), projectID, agentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	// Best-effort: force-close any bridge session still connected with the
	// token that was just replaced, so it can't keep using it indefinitely.
	// Run in the background (own context, not r.Context()) so a slow or
	// unreachable ai-agent doesn't add latency to token generation itself.
	go h.disconnectACPBridge(agentID)
	runCommand := fmt.Sprintf("paca-acp-bridge run --agent-id %s --token %s", agentID, token)
	if h.publicURL != "" {
		runCommand += fmt.Sprintf(" --server %s", h.publicURL)
	}
	presenter.OK(w, r, dto.GenerateACPBridgeTokenResponse{
		Token:      token,
		RunCommand: runCommand,
	})
}

// --- Provider CLI ------------------------------------------------------------

// VerifyCLILogin handles POST
// /projects/:projectId/agents/:agentId/verify-cli-login. It probes (never
// an interactive CLI invocation — see agentsvc.Service.VerifyCLILogin's own
// doc comment) whether a provider_cli agent's underlying CLI is currently
// authenticated inside its default environment, and, on success, persists
// the verification timestamp. See EnvironmentHandler.VerifyCLILogin for the
// environment-scoped sibling used before an agent exists yet (the
// create-agent dialog's own "Verify login" button).
func (h *AgentHandler) VerifyCLILogin(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	agentID, err := parseParamUUID(r, "agentId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	authenticated, err := h.svc.VerifyCLILogin(r.Context(), projectID, agentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.VerifyCLILoginResponse{Authenticated: authenticated})
}

// GenerateGlobalACPBridgeToken handles POST
// /admin/agents/:agentId/acp-bridge-token — GenerateACPBridgeToken's
// global-agent sibling.
func (h *AgentHandler) GenerateGlobalACPBridgeToken(w http.ResponseWriter, r *http.Request) {
	agentID, err := parseParamUUID(r, "agentId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	token, err := h.svc.GenerateGlobalACPBridgeToken(r.Context(), agentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	go h.disconnectACPBridge(agentID)
	runCommand := fmt.Sprintf("paca-acp-bridge run --agent-id %s --token %s", agentID, token)
	if h.publicURL != "" {
		runCommand += fmt.Sprintf(" --server %s", h.publicURL)
	}
	presenter.OK(w, r, dto.GenerateACPBridgeTokenResponse{
		Token:      token,
		RunCommand: runCommand,
	})
}

// disconnectACPBridge best-effort force-closes any bridge session currently
// connected for agentID by calling ai-agent's internal POST
// /agent-bridge/disconnect/:agentId (see routes/bridge.py). Called after a
// bridge token is regenerated — errors are logged, not surfaced, since the
// token has already been persisted regardless of whether this succeeds; a
// stale session left connected in the failure case is bounded by the
// presence TTL and will eventually be treated as offline.
func (h *AgentHandler) disconnectACPBridge(agentID uuid.UUID) {
	if h.aiAgentURL == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), aiAgentHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost,
		fmt.Sprintf("%s/agent-bridge/disconnect/%s", h.aiAgentURL, agentID), nil,
	)
	if err != nil {
		return
	}
	req.Header.Set("X-Internal-Token", h.aiAgentInternalKey)
	resp, err := h.httpClient.Do(req)
	if err != nil {
		slog.Warn("disconnect ACP bridge session after token regeneration", "agent_id", agentID, "error", err)
		return
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		slog.Warn("disconnect ACP bridge session after token regeneration", "agent_id", agentID, "status", resp.StatusCode)
	}
}

// GetACPBridgeStatus handles GET
// /projects/:projectId/agents/:agentId/acp-bridge-status. It proxies to the
// ai-agent service's internal presence check so the frontend can show a live
// connected/disconnected badge for the agent's local bridge.
func (h *AgentHandler) GetACPBridgeStatus(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	agentID, err := parseParamUUID(r, "agentId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	agent, err := h.svc.GetAgent(r.Context(), projectID, agentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	h.proxyACPBridgeStatus(w, r, agent)
}

// GetGlobalACPBridgeStatus handles GET /admin/agents/:agentId/acp-bridge-status
// — GetACPBridgeStatus's global-agent sibling.
func (h *AgentHandler) GetGlobalACPBridgeStatus(w http.ResponseWriter, r *http.Request) {
	agentID, err := parseParamUUID(r, "agentId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	agent, err := h.svc.GetGlobalAgent(r.Context(), agentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	h.proxyACPBridgeStatus(w, r, agent)
}

// proxyACPBridgeStatus proxies to ai-agent's internal bridge presence check
// for agent, shared by GetACPBridgeStatus and GetGlobalACPBridgeStatus once
// each has resolved+verified the agent via its own ownership check.
func (h *AgentHandler) proxyACPBridgeStatus(w http.ResponseWriter, r *http.Request, agent *agentdom.Agent) {
	if agent.AgentType != agentdom.AgentTypeACP {
		presenter.Error(w, r, agentdom.ErrAgentTypeInvalid)
		return
	}
	if h.aiAgentURL == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "agent-runner service URL not configured"))
		return
	}

	req, err := http.NewRequestWithContext(
		r.Context(), http.MethodGet,
		fmt.Sprintf("%s/agent-bridge/status/%s", h.aiAgentURL, agent.ID), nil,
	)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "failed to create request"))
		return
	}
	req.Header.Set("X-Internal-Token", h.aiAgentInternalKey)
	resp, err := h.httpClient.Do(req)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "failed to reach agent-runner service"))
		return
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "failed to read agent-runner response"))
		return
	}
	if resp.StatusCode != http.StatusOK {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "agent-runner service returned an error"))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// GenerateAgentMCPKey handles POST
// /projects/:projectId/agents/:agentId/mcp-agent-key. It issues a new MCP
// API key bound to this specific agent, replacing any existing one, and
// returns the plaintext once — the caller must copy it now, since only its
// hash is persisted. Gated on PermissionAgentsWrite by the router, same as
// GenerateACPBridgeToken.
func (h *AgentHandler) GenerateAgentMCPKey(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	agentID, err := parseParamUUID(r, "agentId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	token, err := h.svc.GenerateAgentMCPKey(r.Context(), projectID, agentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.GenerateMCPAgentKeyResponse{Token: token})
}

// GenerateGlobalAgentMCPKey handles POST /admin/agents/:agentId/mcp-agent-key
// — GenerateAgentMCPKey's global-agent sibling.
func (h *AgentHandler) GenerateGlobalAgentMCPKey(w http.ResponseWriter, r *http.Request) {
	agentID, err := parseParamUUID(r, "agentId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	token, err := h.svc.GenerateGlobalAgentMCPKey(r.Context(), agentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.GenerateMCPAgentKeyResponse{Token: token})
}

// --- Avatar -------------------------------------------------------------------

// InitiateAvatarUpload handles POST /projects/:projectId/agents/:agentId/avatar/initiate-upload.
func (h *AgentHandler) InitiateAvatarUpload(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	agentID, err := parseParamUUID(r, "agentId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	claims := middleware.ClaimsFrom(r)
	if claims == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}
	uploaderID, err := uuid.Parse(claims.Subject)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "invalid subject in token"))
		return
	}

	var req dto.InitiateUploadRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}

	session, err := h.svc.InitiateAvatarUpload(r.Context(), projectID, agentID, req.FileName, req.ContentType, req.FileSize, uploaderID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.UploadSessionFromDomain(session))
}

// CompleteAvatarUpload handles POST /projects/:projectId/agents/:agentId/avatar/complete-upload.
func (h *AgentHandler) CompleteAvatarUpload(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	agentID, err := parseParamUUID(r, "agentId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.CompleteAvatarUploadRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}

	a, err := h.svc.CompleteAvatarUpload(r.Context(), projectID, agentID, req.FileID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, h.toAgentResponse(r.Context(), a))
}

// DeleteAvatar handles DELETE /projects/:projectId/agents/:agentId/avatar.
func (h *AgentHandler) DeleteAvatar(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	agentID, err := parseParamUUID(r, "agentId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	a, err := h.svc.RemoveAvatar(r.Context(), projectID, agentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, h.toAgentResponse(r.Context(), a))
}

// InitiateGlobalAvatarUpload handles POST /admin/agents/:agentId/avatar/initiate-upload.
func (h *AgentHandler) InitiateGlobalAvatarUpload(w http.ResponseWriter, r *http.Request) {
	agentID, err := parseParamUUID(r, "agentId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	claims := middleware.ClaimsFrom(r)
	if claims == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}
	uploaderID, err := uuid.Parse(claims.Subject)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "invalid subject in token"))
		return
	}

	var req dto.InitiateUploadRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}

	session, err := h.svc.InitiateGlobalAvatarUpload(r.Context(), agentID, req.FileName, req.ContentType, req.FileSize, uploaderID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.UploadSessionFromDomain(session))
}

// CompleteGlobalAvatarUpload handles POST /admin/agents/:agentId/avatar/complete-upload.
func (h *AgentHandler) CompleteGlobalAvatarUpload(w http.ResponseWriter, r *http.Request) {
	agentID, err := parseParamUUID(r, "agentId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.CompleteAvatarUploadRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}

	a, err := h.svc.CompleteGlobalAvatarUpload(r.Context(), agentID, req.FileID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, h.toAgentResponse(r.Context(), a))
}

// DeleteGlobalAvatar handles DELETE /admin/agents/:agentId/avatar.
func (h *AgentHandler) DeleteGlobalAvatar(w http.ResponseWriter, r *http.Request) {
	agentID, err := parseParamUUID(r, "agentId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	a, err := h.svc.RemoveGlobalAvatar(r.Context(), agentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, h.toAgentResponse(r.Context(), a))
}

// --- Activity Feed ------------------------------------------------------------

var validActivitySourceTypes = []string{"task", "doc"}

// ListAgentActivities handles GET /projects/:projectId/agents/:agentId/activities.
//
// Supported query params (all optional, combine with AND):
//   - type=<task|doc>|<task,doc>            filter by one or more source types
//   - created_after=<YYYY-MM-DD|RFC3339>    activities created on/after this date/instant
//   - created_before=<YYYY-MM-DD|RFC3339>   activities created before this date/instant
//   - search=<text>                         matches the linked task/doc title or activity content
//   - cursor=<opaque>, page_size=<1-200>    keyset pagination (see next_cursor in response)
func (h *AgentHandler) ListAgentActivities(w http.ResponseWriter, r *http.Request) {
	projectID, agentID, err := h.parseAgentForProject(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if h.memberRepo == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "member resolver not available"))
		return
	}
	member, err := h.memberRepo.FindMemberByAgent(r.Context(), projectID, agentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	pageSize, err := parsePageSize(r, 20, 200)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	filter := agentdom.ListAgentActivitiesFilter{ActorMemberID: member.ID}
	if typesRaw := r.URL.Query().Get("type"); typesRaw != "" {
		for _, t := range splitCommaList(typesRaw) {
			if !slices.Contains(validActivitySourceTypes, t) {
				presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid type: "+t))
				return
			}
			filter.SourceTypes = append(filter.SourceTypes, agentdom.ActivitySourceType(t))
		}
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("created_after")); raw != "" {
		t, ok := parseCreatedAfterBound(raw)
		if !ok {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid created_after"))
			return
		}
		filter.CreatedAfter = t
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("created_before")); raw != "" {
		t, ok := parseCreatedBeforeBound(raw)
		if !ok {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid created_before"))
			return
		}
		filter.CreatedBefore = t
	}
	if search := strings.TrimSpace(r.URL.Query().Get("search")); search != "" {
		filter.Search = &search
	}
	if cursorRaw := r.URL.Query().Get("cursor"); cursorRaw != "" {
		filter.CursorAfter = &cursorRaw
	}

	items, hasMore, err := h.svc.ListAgentActivities(r.Context(), filter, pageSize)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.AgentActivityResponse, 0, len(items))
	for _, item := range items {
		resp = append(resp, dto.AgentActivityFromEntity(item))
	}

	var nextCursor *string
	if hasMore && len(items) > 0 {
		s := agentdom.EncodeActivityFeedCursor(items[len(items)-1])
		nextCursor = &s
	}
	presenter.OK(w, r, map[string]any{
		"items":       resp,
		"page_size":   pageSize,
		"next_cursor": nextCursor,
	})
}
