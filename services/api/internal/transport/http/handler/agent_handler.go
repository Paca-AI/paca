package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	agentsvc "github.com/Paca-AI/api/internal/service/agent"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// aiAgentHTTPTimeout bounds every call this handler makes into the ai-agent
// service (LLM model listing, ACP bridge status/disconnect) so a slow or
// wedged ai-agent instance can't hang the calling request indefinitely.
const aiAgentHTTPTimeout = 10 * time.Second

type agentActivityRecorder interface {
	RecordActivity(ctx context.Context, in taskdom.RecordActivityInput) error
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
}

// NewAgentHandler returns an AgentHandler wired to the agent service.
// publicURL is the externally reachable base URL (e.g. https://paca.example.com)
// used to build the local-bridge "run this command" snippet. aiAgentInternalKey
// authenticates calls into ai-agent's internal-only routes and must match
// ai-agent's own INTERNAL_API_KEY.
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

// ListAgents handles GET /projects/:projectId/agents.
func (h *AgentHandler) ListAgents(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	agents, err := h.svc.ListAgents(r.Context(), projectID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.AgentResponse, 0, len(agents))
	for _, a := range agents {
		resp = append(resp, dto.AgentFromEntity(a))
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
	presenter.OK(w, r, dto.AgentFromEntity(a))
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
	default:
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "agent_type must be one of: llm, acp"))
		return
	}
	claims := middleware.ClaimsFrom(r)
	callerID, _ := uuid.Parse(claims.Subject)

	a, err := h.svc.CreateAgent(r.Context(), projectID, agentdom.CreateAgentInput{
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
		GitCommitterName:  req.GitCommitterName,
		GitCommitterEmail: req.GitCommitterEmail,
		ProjectRoleID:     req.ProjectRoleID,
		CreatedBy:         &callerID,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.AgentFromEntity(a))
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
		GitCommitterName:  req.GitCommitterName,
		GitCommitterEmail: req.GitCommitterEmail,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.AgentFromEntity(a))
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
	memberID, err := h.resolveMemberID(r, projectID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	session, conv, err := h.svc.StartChatSession(r.Context(), projectID, agentID, memberID, req.Message)
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
	memberID, err := h.resolveMemberID(r, projectID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	conv, err := h.svc.SendChatMessage(r.Context(), projectID, sessionID, memberID, req.Message)
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
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "ai-agent service URL not configured"))
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, h.aiAgentURL+"/llm/models", nil)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "failed to create request"))
		return
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "failed to reach ai-agent service"))
		return
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "failed to read ai-agent response"))
		return
	}

	if resp.StatusCode != http.StatusOK {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "ai-agent service returned an error"))
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
	runCommand := fmt.Sprintf("uvx paca-acp-bridge run --agent-id %s --token %s", agentID, token)
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
	_, agentID, err := h.parseAgentForProject(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if h.aiAgentURL == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "ai-agent service URL not configured"))
		return
	}

	req, err := http.NewRequestWithContext(
		r.Context(), http.MethodGet,
		fmt.Sprintf("%s/agent-bridge/status/%s", h.aiAgentURL, agentID), nil,
	)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "failed to create request"))
		return
	}
	req.Header.Set("X-Internal-Token", h.aiAgentInternalKey)
	resp, err := h.httpClient.Do(req)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "failed to reach ai-agent service"))
		return
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "failed to read ai-agent response"))
		return
	}
	if resp.StatusCode != http.StatusOK {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "ai-agent service returned an error"))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
