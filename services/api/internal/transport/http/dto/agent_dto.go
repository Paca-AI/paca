package dto

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
)

// =========================================================================
// Agent DTOs
// =========================================================================

// AgentResponse is the public view of an agent. ProjectID is nil for a
// global-scope agent (AgentScope == "global"); GlobalRoleID is only ever
// set for a global-scope agent.
type AgentResponse struct {
	ID           uuid.UUID  `json:"id"`
	ProjectID    *uuid.UUID `json:"project_id,omitempty"`
	AgentScope   string     `json:"agent_scope"`
	GlobalRoleID *uuid.UUID `json:"global_role_id,omitempty"`
	MemberID     *uuid.UUID `json:"member_id,omitempty"`
	Name         string     `json:"name"`
	Handle       string     `json:"handle"`
	// AvatarURL/AvatarThumbURL are presigned GET URLs, populated by the
	// handler (not this mapper) via attachmentdom.AvatarService — nil when
	// no avatar has been uploaded.
	AvatarURL         *string  `json:"avatar_url,omitempty"`
	AvatarThumbURL    *string  `json:"avatar_thumb_url,omitempty"`
	AgentType         string   `json:"agent_type"`
	LLMProvider       string   `json:"llm_provider"`
	LLMModel          string   `json:"llm_model"`
	LLMBaseURL        string   `json:"llm_base_url"`
	ACPProvider       *string  `json:"acp_provider,omitempty"`
	ACPCommand        []string `json:"acp_command,omitempty"`
	HasACPBridgeToken bool     `json:"has_acp_bridge_token"`
	HasMCPAPIKey      bool     `json:"has_mcp_api_key"`
	// CLIProvider/CLIModel/CLIAuthMode/HasCLIAPIKey/CLILoginVerifiedAt are
	// provider_cli-only, empty/nil for other agent types — see
	// agentdom.Agent.CLIProvider's doc comment. HasCLIAPIKey mirrors
	// HasACPBridgeToken/HasMCPAPIKey: the raw key is never exposed.
	CLIProvider        *string    `json:"cli_provider,omitempty"`
	CLIModel           string     `json:"cli_model,omitempty"`
	CLIAuthMode        string     `json:"cli_auth_mode,omitempty"`
	HasCLIAPIKey       bool       `json:"has_cli_api_key"`
	CLILoginVerifiedAt *time.Time `json:"cli_login_verified_at,omitempty"`
	SystemPrompt       string     `json:"system_prompt"`
	MaxIterations      int        `json:"max_iterations"`
	TimeoutMinutes     int        `json:"timeout_minutes"`
	GitCommitterName   string     `json:"git_committer_name"`
	GitCommitterEmail  string     `json:"git_committer_email"`
	DockerEnabled      bool       `json:"docker_enabled"`
	// ParallelismLimit caps how many of this agent's conversations may be
	// "running" at once — see agentdom.Agent.ParallelismLimit's doc comment.
	ParallelismLimit int `json:"parallelism_limit"`
	// DefaultEnvironmentID is the static environment (environmentdom.Environment)
	// this agent's conversations attach to by default — nil for a global-scope
	// agent, or a project-scoped agent with no default set. See
	// agentdom.Agent.DefaultEnvironmentID's doc comment.
	DefaultEnvironmentID *uuid.UUID `json:"default_environment_id,omitempty"`
	// DefaultFolderID is which folder inside DefaultEnvironmentID this
	// agent's conversations work in by default — nil unless
	// DefaultEnvironmentID is also set. See
	// agentdom.Agent.DefaultFolderID's doc comment.
	DefaultFolderID *uuid.UUID               `json:"default_folder_id,omitempty"`
	CreatedBy       *uuid.UUID               `json:"created_by,omitempty"`
	CreatedAt       time.Time                `json:"created_at"`
	UpdatedAt       time.Time                `json:"updated_at"`
	MCPServers      []AgentMCPServerResponse `json:"mcp_servers,omitempty"`
	Skills          []AgentSkillResponse     `json:"skills,omitempty"`
	EnvVars         []AgentEnvVarResponse    `json:"env_vars,omitempty"`
}

// CreateAgentRequest is the body for POST /projects/:projectId/agents.
// LLM fields are required when agent_type is "llm" (the default when
// omitted); ACP fields are required when agent_type is "acp" — validated in
// the handler since it depends on the value of AgentType itself.
// SystemPrompt, GitCommitterName, and GitCommitterEmail are LLM-only too —
// the service silently drops them for "acp" agents (see agent.CreateAgent).
type CreateAgentRequest struct {
	Name        string   `json:"name" binding:"required"`
	Handle      string   `json:"handle" binding:"required"`
	AgentType   string   `json:"agent_type"`
	LLMProvider string   `json:"llm_provider"`
	LLMModel    string   `json:"llm_model"`
	LLMAPIKey   string   `json:"llm_api_key"`
	LLMBaseURL  string   `json:"llm_base_url"`
	ACPProvider string   `json:"acp_provider"`
	ACPCommand  []string `json:"acp_command"`
	// CLIProvider/CLIModel/CLIAuthMode/CLIAPIKey are required (and the
	// fields above meaningless) when agent_type is "provider_cli" — see
	// agentdom.Agent.CLIProvider's doc comment. CLIAuthMode defaults to
	// "login" when omitted.
	CLIProvider       string `json:"cli_provider"`
	CLIModel          string `json:"cli_model"`
	CLIAuthMode       string `json:"cli_auth_mode"`
	CLIAPIKey         string `json:"cli_api_key"`
	SystemPrompt      string `json:"system_prompt"`
	MaxIterations     int    `json:"max_iterations"`
	TimeoutMinutes    int    `json:"timeout_minutes"`
	GitCommitterName  string `json:"git_committer_name"`
	GitCommitterEmail string `json:"git_committer_email"`
	DockerEnabled     bool   `json:"docker_enabled"`
	// ParallelismLimit: omit or 0 defaults to 1 — see
	// agentdom.Agent.ParallelismLimit's doc comment.
	ParallelismLimit int `json:"parallelism_limit"`
	// DefaultEnvironmentID optionally sets the static environment this
	// agent's conversations attach to by default — must belong to this same
	// project (validated in agent.CreateAgent). MANDATORY (not optional)
	// when agent_type is "provider_cli" — see
	// agentdom.ErrDefaultEnvironmentRequiredForCLIProvider.
	DefaultEnvironmentID *uuid.UUID `json:"default_environment_id"`
	// DefaultFolderID optionally sets which folder inside
	// DefaultEnvironmentID this agent's conversations work in by default —
	// must belong to DefaultEnvironmentID, also set in this same request
	// (validated in agent.CreateAgent).
	DefaultFolderID *uuid.UUID `json:"default_folder_id"`
	ProjectRoleID   uuid.UUID  `json:"project_role_id" binding:"required"`
}

// UpdateAgentRequest is the body for PATCH /projects/:projectId/agents/:agentId
// and PATCH /admin/agents/:agentId. GlobalRoleID is only meaningful for the
// latter (global agents) — pass a zero UUID to clear an assigned role.
type UpdateAgentRequest struct {
	Name        *string  `json:"name"`
	Handle      *string  `json:"handle"`
	LLMProvider *string  `json:"llm_provider"`
	LLMModel    *string  `json:"llm_model"`
	LLMAPIKey   *string  `json:"llm_api_key"`
	LLMBaseURL  *string  `json:"llm_base_url"`
	ACPProvider *string  `json:"acp_provider"`
	ACPCommand  []string `json:"acp_command"`
	// CLIProvider/CLIModel/CLIAuthMode/CLIAPIKey: nil means "unchanged",
	// same convention as every other pointer field here — only meaningful
	// for an existing provider_cli agent.
	CLIProvider       *string `json:"cli_provider"`
	CLIModel          *string `json:"cli_model"`
	CLIAuthMode       *string `json:"cli_auth_mode"`
	CLIAPIKey         *string `json:"cli_api_key"`
	SystemPrompt      *string `json:"system_prompt"`
	MaxIterations     *int    `json:"max_iterations"`
	TimeoutMinutes    *int    `json:"timeout_minutes"`
	GitCommitterName  *string `json:"git_committer_name"`
	GitCommitterEmail *string `json:"git_committer_email"`
	DockerEnabled     *bool   `json:"docker_enabled"`
	// ParallelismLimit: nil means unchanged, same convention as every other
	// pointer field here.
	ParallelismLimit *int       `json:"parallelism_limit"`
	GlobalRoleID     *uuid.UUID `json:"global_role_id"`
	// DefaultEnvironmentID: omit to leave unchanged, pass a zero UUID
	// ("00000000-0000-0000-0000-000000000000") to clear it, or a real
	// environment ID to set it — see agentdom.UpdateAgentInput.
	// DefaultEnvironmentID's doc comment. Ignored for global-scope agents.
	DefaultEnvironmentID *uuid.UUID `json:"default_environment_id"`
	// DefaultFolderID: same omit/zero-UUID/real-ID contract as
	// DefaultEnvironmentID above — see agentdom.UpdateAgentInput.
	// DefaultFolderID's doc comment. Ignored for global-scope agents.
	DefaultFolderID *uuid.UUID `json:"default_folder_id"`
}

// CreateGlobalAgentRequest is the body for POST /admin/agents. Mirrors
// CreateAgentRequest minus ProjectRoleID (nothing to assign at creation
// time — a global agent gets a project role only later, when invited into a
// project), plus GlobalRoleID.
type CreateGlobalAgentRequest struct {
	Name              string     `json:"name" binding:"required"`
	Handle            string     `json:"handle" binding:"required"`
	AgentType         string     `json:"agent_type"`
	LLMProvider       string     `json:"llm_provider"`
	LLMModel          string     `json:"llm_model"`
	LLMAPIKey         string     `json:"llm_api_key"`
	LLMBaseURL        string     `json:"llm_base_url"`
	ACPProvider       string     `json:"acp_provider"`
	ACPCommand        []string   `json:"acp_command"`
	SystemPrompt      string     `json:"system_prompt"`
	MaxIterations     int        `json:"max_iterations"`
	TimeoutMinutes    int        `json:"timeout_minutes"`
	GitCommitterName  string     `json:"git_committer_name"`
	GitCommitterEmail string     `json:"git_committer_email"`
	DockerEnabled     bool       `json:"docker_enabled"`
	ParallelismLimit  int        `json:"parallelism_limit"`
	GlobalRoleID      *uuid.UUID `json:"global_role_id"`
}

// GenerateACPBridgeTokenResponse is the body returned for POST
// /projects/:projectId/agents/:agentId/acp-bridge-token. Token is shown once
// and cannot be retrieved again — only its hash is persisted.
type GenerateACPBridgeTokenResponse struct {
	Token      string `json:"token"`
	RunCommand string `json:"run_command"`
}

// GenerateMCPAgentKeyResponse is the body returned for POST
// /projects/:projectId/agents/:agentId/mcp-agent-key (and its
// /admin/agents/:agentId/mcp-agent-key global sibling). Token is shown once
// and cannot be retrieved again — only its hash is persisted, and
// generating a new one invalidates whatever key was live before (same
// one-live-key-at-a-time behavior as GenerateACPBridgeTokenResponse). Used
// as PACA_API_KEY alongside PACA_AGENT_ID in the agent's MCP connect
// command so tool calls are attributed to the agent, not to whichever human
// requested this.
type GenerateMCPAgentKeyResponse struct {
	Token string `json:"token"`
}

// VerifyCLILoginResponse is the body returned for POST
// /projects/:projectId/agents/:agentId/verify-cli-login and its
// environment-scoped sibling, POST
// /projects/:projectId/environments/:environmentId/verify-cli-login
// (EnvironmentHandler.VerifyCLILogin).
type VerifyCLILoginResponse struct {
	Authenticated bool `json:"authenticated"`
}

// AgentFromEntity maps an Agent entity to AgentResponse.
func AgentFromEntity(a *agentdom.Agent) AgentResponse {
	scope := string(a.AgentScope)
	if scope == "" {
		scope = string(agentdom.AgentScopeProject)
	}
	resp := AgentResponse{
		ID:                   a.ID,
		AgentScope:           scope,
		GlobalRoleID:         a.GlobalRoleID,
		MemberID:             a.MemberID,
		Name:                 a.Name,
		Handle:               a.Handle,
		AgentType:            a.AgentType,
		LLMProvider:          a.LLMProvider,
		LLMModel:             a.LLMModel,
		LLMBaseURL:           a.LLMBaseURL,
		ACPProvider:          a.ACPProvider,
		ACPCommand:           a.ACPCommand,
		HasACPBridgeToken:    a.HasACPBridgeToken,
		HasMCPAPIKey:         a.HasMCPAPIKey,
		CLIProvider:          a.CLIProvider,
		CLIModel:             a.CLIModel,
		CLIAuthMode:          a.CLIAuthMode,
		HasCLIAPIKey:         a.CLIAPIKeySecret != "",
		CLILoginVerifiedAt:   a.CLILoginVerifiedAt,
		SystemPrompt:         a.SystemPrompt,
		MaxIterations:        a.MaxIterations,
		TimeoutMinutes:       a.TimeoutMinutes,
		GitCommitterName:     a.GitCommitterName,
		GitCommitterEmail:    a.GitCommitterEmail,
		DockerEnabled:        a.DockerEnabled,
		ParallelismLimit:     a.ParallelismLimit,
		DefaultEnvironmentID: a.DefaultEnvironmentID,
		DefaultFolderID:      a.DefaultFolderID,
		CreatedBy:            a.CreatedBy,
		CreatedAt:            a.CreatedAt,
		UpdatedAt:            a.UpdatedAt,
	}
	if a.ProjectID != uuid.Nil {
		id := a.ProjectID
		resp.ProjectID = &id
	}
	if len(a.MCPServers) > 0 {
		resp.MCPServers = make([]AgentMCPServerResponse, 0, len(a.MCPServers))
		for _, s := range a.MCPServers {
			resp.MCPServers = append(resp.MCPServers, MCPServerFromEntity(s))
		}
	}
	if len(a.Skills) > 0 {
		resp.Skills = make([]AgentSkillResponse, 0, len(a.Skills))
		for _, s := range a.Skills {
			resp.Skills = append(resp.Skills, SkillFromEntity(s))
		}
	}
	if len(a.EnvVars) > 0 {
		resp.EnvVars = make([]AgentEnvVarResponse, 0, len(a.EnvVars))
		for _, v := range a.EnvVars {
			resp.EnvVars = append(resp.EnvVars, EnvVarFromEntity(v))
		}
	}
	return resp
}

// =========================================================================
// MCP Server DTOs
// =========================================================================

// AgentMCPServerResponse is the public view of an MCP server configuration.
type AgentMCPServerResponse struct {
	ID         uuid.UUID         `json:"id"`
	AgentID    uuid.UUID         `json:"agent_id"`
	ServerName string            `json:"server_name"`
	Transport  string            `json:"transport"`
	Command    *string           `json:"command,omitempty"`
	Args       []string          `json:"args"`
	URL        *string           `json:"url,omitempty"`
	Env        map[string]string `json:"env"`
	IsEnabled  bool              `json:"is_enabled"`
	CreatedAt  time.Time         `json:"created_at"`
}

// AddMCPServerRequest is the body for POST /agents/:agentId/mcp-servers.
type AddMCPServerRequest struct {
	ServerName string            `json:"server_name" binding:"required"`
	Transport  string            `json:"transport" binding:"required,oneof=stdio sse http"`
	Command    *string           `json:"command"`
	Args       []string          `json:"args"`
	URL        *string           `json:"url"`
	Env        map[string]string `json:"env"`
}

// UpdateMCPServerRequest is the body for PATCH /agents/:agentId/mcp-servers/:serverId.
type UpdateMCPServerRequest struct {
	Command   *string           `json:"command"`
	Args      []string          `json:"args"`
	URL       *string           `json:"url"`
	Env       map[string]string `json:"env"`
	IsEnabled *bool             `json:"is_enabled"`
}

// secretEnvKeyPatterns lists substrings that indicate an env var holds a secret.
// Values whose keys contain any of these (case-insensitive) are redacted in API responses.
var secretEnvKeyPatterns = []string{"key", "token", "secret", "password", "pass", "auth", "credential", "private"}

// maskSecretEnv returns a copy of env with likely-secret values replaced by "***".
func maskSecretEnv(env map[string]string) map[string]string {
	if len(env) == 0 {
		return map[string]string{}
	}
	masked := make(map[string]string, len(env))
	for k, v := range env {
		kLower := strings.ToLower(k)
		redact := false
		for _, pat := range secretEnvKeyPatterns {
			if strings.Contains(kLower, pat) {
				redact = true
				break
			}
		}
		if redact {
			masked[k] = "***"
		} else {
			masked[k] = v
		}
	}
	return masked
}

// MCPServerFromEntity maps an AgentMCPServer entity to its DTO.
func MCPServerFromEntity(s *agentdom.AgentMCPServer) AgentMCPServerResponse {
	args := s.Args
	if args == nil {
		args = []string{}
	}
	return AgentMCPServerResponse{
		ID:         s.ID,
		AgentID:    s.AgentID,
		ServerName: s.ServerName,
		Transport:  s.Transport,
		Command:    s.Command,
		Args:       args,
		URL:        s.URL,
		Env:        maskSecretEnv(s.Env),
		IsEnabled:  s.IsEnabled,
		CreatedAt:  s.CreatedAt,
	}
}

// =========================================================================
// Skill DTOs
// =========================================================================

// AgentSkillResponse is the public view of an agent skill.
type AgentSkillResponse struct {
	ID           uuid.UUID `json:"id"`
	AgentID      uuid.UUID `json:"agent_id"`
	SkillName    string    `json:"skill_name"`
	SkillSource  string    `json:"skill_source"`
	SkillContent string    `json:"skill_content"`
	SourceURL    *string   `json:"source_url,omitempty"`
	Triggers     []string  `json:"triggers"`
	IsEnabled    bool      `json:"is_enabled"`
	CreatedAt    time.Time `json:"created_at"`
}

// AddSkillRequest is the body for POST /agents/:agentId/skills.
type AddSkillRequest struct {
	SkillName    string   `json:"skill_name" binding:"required"`
	SkillSource  string   `json:"skill_source" binding:"required,oneof=inline marketplace github_url"`
	SkillContent string   `json:"skill_content"`
	SourceURL    *string  `json:"source_url"`
	Triggers     []string `json:"triggers"`
}

// UpdateSkillRequest is the body for PATCH /agents/:agentId/skills/:skillId.
type UpdateSkillRequest struct {
	SkillContent *string  `json:"skill_content"`
	Triggers     []string `json:"triggers"`
	IsEnabled    *bool    `json:"is_enabled"`
}

// SkillFromEntity maps an AgentSkill entity to its DTO.
func SkillFromEntity(s *agentdom.AgentSkill) AgentSkillResponse {
	triggers := s.Triggers
	if triggers == nil {
		triggers = []string{}
	}
	return AgentSkillResponse{
		ID:           s.ID,
		AgentID:      s.AgentID,
		SkillName:    s.SkillName,
		SkillSource:  s.SkillSource,
		SkillContent: s.SkillContent,
		SourceURL:    s.SourceURL,
		Triggers:     triggers,
		IsEnabled:    s.IsEnabled,
		CreatedAt:    s.CreatedAt,
	}
}

// =========================================================================
// Environment Variable DTOs
// =========================================================================

// AgentEnvVarResponse is the public view of a secret environment variable.
// Value is always redacted — the plaintext is never returned once set.
type AgentEnvVarResponse struct {
	ID        uuid.UUID `json:"id"`
	AgentID   uuid.UUID `json:"agent_id"`
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	CreatedAt time.Time `json:"created_at"`
}

// AddEnvVarRequest is the body for POST /agents/:agentId/env-vars.
type AddEnvVarRequest struct {
	Key   string `json:"key" binding:"required"`
	Value string `json:"value" binding:"required"`
}

// UpdateEnvVarRequest is the body for PATCH /agents/:agentId/env-vars/:envVarId.
type UpdateEnvVarRequest struct {
	Value string `json:"value" binding:"required"`
}

// EnvVarFromEntity maps an AgentEnvironmentVariable entity to its DTO. The
// value is always masked; it is never decrypted for API responses.
func EnvVarFromEntity(v *agentdom.AgentEnvironmentVariable) AgentEnvVarResponse {
	return AgentEnvVarResponse{
		ID:        v.ID,
		AgentID:   v.AgentID,
		Key:       v.Key,
		Value:     "***",
		CreatedAt: v.CreatedAt,
	}
}

// WriteWithAIRequest is the body for POST /projects/:projectId/tasks/:taskId/write-with-ai.
type WriteWithAIRequest struct {
	AgentID uuid.UUID `json:"agent_id" binding:"required"`
}

// =========================================================================
// Conversation DTOs
// =========================================================================

// AgentConversationResponse is the public view of a conversation. ProjectID
// is nil for a global-chat conversation, which carries ActorUserID instead
// of TriggeredByMemberID.
type AgentConversationResponse struct {
	ID                  uuid.UUID  `json:"id"`
	AgentID             uuid.UUID  `json:"agent_id"`
	ProjectID           *uuid.UUID `json:"project_id,omitempty"`
	TriggerType         string     `json:"trigger_type"`
	TaskID              *uuid.UUID `json:"task_id,omitempty"`
	ChatSessionID       *uuid.UUID `json:"chat_session_id,omitempty"`
	TriggeredByMemberID *uuid.UUID `json:"triggered_by_member_id,omitempty"`
	ActorUserID         *uuid.UUID `json:"actor_user_id,omitempty"`
	Status              string     `json:"status"`
	IterationCount      int        `json:"iteration_count"`
	InputTokens         int64      `json:"input_tokens"`
	OutputTokens        int64      `json:"output_tokens"`
	TotalTokens         int64      `json:"total_tokens"`
	CostUSD             *float64   `json:"cost_usd,omitempty"`
	ErrorMessage        *string    `json:"error_message,omitempty"`
	// BranchName was dropped along with agent_conversations.branch_name
	// (migration 000042_add_environments.sql) — that column
	// encoded 1-conversation-owns-1-container, a shape static environments
	// replace (see agentdom.AgentConversation.EnvironmentID's doc comment).
	// No replacement field: branch_name no longer appears in conversation
	// API responses at all.
	PRUrl       *string    `json:"pr_url,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	AgentName   string     `json:"agent_name,omitempty"`
	AgentHandle string     `json:"agent_handle,omitempty"`
	// EnvironmentID mirrors agentdom.AgentConversation.EnvironmentID: set only
	// when this conversation is attached to a static environment rather than
	// an ephemeral sandbox. The web app uses its presence to keep the
	// composer open past a terminal status and to skip the idle-timer
	// heartbeat, both of which only make sense for an ephemeral sandbox — see
	// conversation-view.tsx's canReply and heartbeat effect.
	EnvironmentID *uuid.UUID `json:"environment_id,omitempty"`
}

// AgentConversationEventResponse is the public view of a conversation event.
type AgentConversationEventResponse struct {
	ID             uuid.UUID      `json:"id"`
	ConversationID uuid.UUID      `json:"conversation_id"`
	EventIndex     int            `json:"event_index"`
	EventType      string         `json:"event_type"`
	EventSource    string         `json:"event_source"`
	Payload        map[string]any `json:"payload"`
	CreatedAt      time.Time      `json:"created_at"`
}

// SendMessageRequest is the body for POST /conversations/:id/messages.
type SendMessageRequest struct {
	Message string `json:"message" binding:"required"`
	// ContextItems are Task/Doc/Conversation/Automation references the user
	// attached to this message via the frontend composer's context-item
	// picker — see agentdom.ContextItemRef.
	ContextItems []agentdom.ContextItemRef `json:"context_items,omitempty"`
	// OnBusy is one of "" (ask, the default) | "queue" | "force" — see
	// agentdom.OnBusyQueue's doc comment. Only takes effect when this
	// reply resumes an ACP or environment-attached conversation in place;
	// ignored otherwise.
	OnBusy string `json:"on_busy,omitempty"`
}

// ConversationFromEntity maps an AgentConversation entity to its DTO.
func ConversationFromEntity(c *agentdom.AgentConversation) AgentConversationResponse {
	resp := AgentConversationResponse{
		ID:                  c.ID,
		AgentID:             c.AgentID,
		TriggerType:         c.TriggerType,
		TaskID:              c.TaskID,
		ChatSessionID:       c.ChatSessionID,
		TriggeredByMemberID: c.TriggeredByMemberID,
		ActorUserID:         c.ActorUserID,
		Status:              c.Status,
		IterationCount:      c.IterationCount,
		InputTokens:         c.InputTokens,
		OutputTokens:        c.OutputTokens,
		TotalTokens:         c.TotalTokens,
		CostUSD:             c.CostUSD,
		ErrorMessage:        c.ErrorMessage,
		PRUrl:               c.PRUrl,
		StartedAt:           c.StartedAt,
		FinishedAt:          c.FinishedAt,
		CreatedAt:           c.CreatedAt,
		AgentName:           c.AgentName,
		AgentHandle:         c.AgentHandle,
		EnvironmentID:       c.EnvironmentID,
	}
	if c.ProjectID != uuid.Nil {
		id := c.ProjectID
		resp.ProjectID = &id
	}
	return resp
}

// AgentActivityResponse is the public view of one item in an agent's unified
// task+doc activity feed.
type AgentActivityResponse struct {
	ID            uuid.UUID       `json:"id"`
	SourceType    string          `json:"source_type"`
	SourceID      uuid.UUID       `json:"source_id"`
	SourceTitle   string          `json:"source_title"`
	SourceDeleted bool            `json:"source_deleted"`
	ActivityType  string          `json:"activity_type"`
	Content       json.RawMessage `json:"content"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// AgentActivityFromEntity maps a domain ActivityFeedItem to an AgentActivityResponse DTO.
func AgentActivityFromEntity(a *agentdom.ActivityFeedItem) AgentActivityResponse {
	content := a.Content
	if len(content) == 0 {
		content = json.RawMessage("{}")
	}
	return AgentActivityResponse{
		ID:            a.ID,
		SourceType:    string(a.SourceType),
		SourceID:      a.SourceID,
		SourceTitle:   a.SourceTitle,
		SourceDeleted: a.SourceDeleted,
		ActivityType:  a.ActivityType,
		Content:       content,
		CreatedAt:     a.CreatedAt,
		UpdatedAt:     a.UpdatedAt,
	}
}

// ConversationEventFromEntity maps an AgentConversationEvent entity to its DTO.
func ConversationEventFromEntity(e *agentdom.AgentConversationEvent) AgentConversationEventResponse {
	payload := e.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	return AgentConversationEventResponse{
		ID:             e.ID,
		ConversationID: e.ConversationID,
		EventIndex:     e.EventIndex,
		EventType:      e.EventType,
		EventSource:    e.EventSource,
		Payload:        payload,
		CreatedAt:      e.CreatedAt,
	}
}

// =========================================================================
// Chat Session DTOs
// =========================================================================

// AgentChatSessionResponse is the public view of a chat session. ProjectID
// and MemberID are nil for a global chat session, which carries
// ActorUserID instead.
type AgentChatSessionResponse struct {
	ID            uuid.UUID  `json:"id"`
	AgentID       uuid.UUID  `json:"agent_id"`
	ProjectID     *uuid.UUID `json:"project_id,omitempty"`
	MemberID      *uuid.UUID `json:"member_id,omitempty"`
	ActorUserID   *uuid.UUID `json:"actor_user_id,omitempty"`
	Title         *string    `json:"title,omitempty"`
	LastMessageAt *time.Time `json:"last_message_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// StartChatSessionRequest is the body for POST /agents/:agentId/chat.
// EnvironmentID/FolderID are optional: omitting EnvironmentID falls back to
// the agent's own default_environment_id; omitting FolderID auto-selects
// the environment's sole folder, or fails if that's ambiguous — see
// agentdom.ChatSessionService.StartChatSession's doc comment.
type StartChatSessionRequest struct {
	Message       string     `json:"message" binding:"required"`
	EnvironmentID *uuid.UUID `json:"environment_id"`
	FolderID      *uuid.UUID `json:"folder_id"`
	// ContextItems are Task/Doc/Conversation/Automation references the user
	// attached to this message via the frontend composer's context-item
	// picker — see agentdom.ContextItemRef.
	ContextItems []agentdom.ContextItemRef `json:"context_items,omitempty"`
	// OnBusy is "" (ask, the default) | "queue" | "force" — see
	// agentdom.OnBusyQueue/OnBusyForce's doc comments. Only meaningful when
	// the agent is already at its parallelism_limit of running
	// conversations; ignored otherwise.
	OnBusy string `json:"on_busy,omitempty"`
}

// SendChatMessageRequest is the body for POST /chat-sessions/:sessionId/messages.
type SendChatMessageRequest struct {
	Message string `json:"message" binding:"required"`
	// ContextItems are Task/Doc/Conversation/Automation references the user
	// attached to this message via the frontend composer's context-item
	// picker — see agentdom.ContextItemRef.
	ContextItems []agentdom.ContextItemRef `json:"context_items,omitempty"`
	// OnBusy: see StartChatSessionRequest.OnBusy's doc comment.
	OnBusy string `json:"on_busy,omitempty"`
}

// ChatSessionFromEntity maps an AgentChatSession entity to its DTO.
func ChatSessionFromEntity(s *agentdom.AgentChatSession) AgentChatSessionResponse {
	resp := AgentChatSessionResponse{
		ID:            s.ID,
		AgentID:       s.AgentID,
		ActorUserID:   s.ActorUserID,
		Title:         s.Title,
		LastMessageAt: s.LastMessageAt,
		CreatedAt:     s.CreatedAt,
	}
	if s.ProjectID != uuid.Nil {
		id := s.ProjectID
		resp.ProjectID = &id
	}
	if s.MemberID != uuid.Nil {
		id := s.MemberID
		resp.MemberID = &id
	}
	return resp
}

// =========================================================================
// Skill Template DTOs
// =========================================================================

// SkillTemplateResponse is the public view of a built-in skill template.
type SkillTemplateResponse struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Content     string   `json:"content"`
	Triggers    []string `json:"triggers"`
}

// SkillTemplateFromEntity maps a SkillTemplate domain struct to its DTO.
func SkillTemplateFromEntity(t *agentdom.SkillTemplate) SkillTemplateResponse {
	triggers := t.Triggers
	if triggers == nil {
		triggers = []string{}
	}
	return SkillTemplateResponse{
		Slug:        t.Slug,
		Name:        t.Name,
		Description: t.Description,
		Content:     t.Content,
		Triggers:    triggers,
	}
}
