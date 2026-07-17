package dto

import (
	"strings"
	"time"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	"github.com/google/uuid"
)

// =========================================================================
// Agent DTOs
// =========================================================================

// AgentResponse is the public view of an agent.
type AgentResponse struct {
	ID                uuid.UUID                `json:"id"`
	ProjectID         uuid.UUID                `json:"project_id"`
	MemberID          *uuid.UUID               `json:"member_id,omitempty"`
	Name              string                   `json:"name"`
	Handle            string                   `json:"handle"`
	AvatarURL         *string                  `json:"avatar_url,omitempty"`
	AgentType         string                   `json:"agent_type"`
	LLMProvider       string                   `json:"llm_provider"`
	LLMModel          string                   `json:"llm_model"`
	LLMBaseURL        string                   `json:"llm_base_url"`
	ACPProvider       *string                  `json:"acp_provider,omitempty"`
	ACPCommand        []string                 `json:"acp_command,omitempty"`
	HasACPBridgeToken bool                     `json:"has_acp_bridge_token"`
	SystemPrompt      string                   `json:"system_prompt"`
	MaxIterations     int                      `json:"max_iterations"`
	TimeoutMinutes    int                      `json:"timeout_minutes"`
	GitCommitterName  string                   `json:"git_committer_name"`
	GitCommitterEmail string                   `json:"git_committer_email"`
	CreatedBy         *uuid.UUID               `json:"created_by,omitempty"`
	CreatedAt         time.Time                `json:"created_at"`
	UpdatedAt         time.Time                `json:"updated_at"`
	MCPServers        []AgentMCPServerResponse `json:"mcp_servers,omitempty"`
	Skills            []AgentSkillResponse     `json:"skills,omitempty"`
	EnvVars           []AgentEnvVarResponse    `json:"env_vars,omitempty"`
}

// CreateAgentRequest is the body for POST /projects/:projectId/agents.
// LLM fields are required when agent_type is "llm" (the default when
// omitted); ACP fields are required when agent_type is "acp" — validated in
// the handler since it depends on the value of AgentType itself.
type CreateAgentRequest struct {
	Name              string    `json:"name" binding:"required"`
	Handle            string    `json:"handle" binding:"required"`
	AgentType         string    `json:"agent_type"`
	LLMProvider       string    `json:"llm_provider"`
	LLMModel          string    `json:"llm_model"`
	LLMAPIKey         string    `json:"llm_api_key"`
	LLMBaseURL        string    `json:"llm_base_url"`
	ACPProvider       string    `json:"acp_provider"`
	ACPCommand        []string  `json:"acp_command"`
	SystemPrompt      string    `json:"system_prompt"`
	MaxIterations     int       `json:"max_iterations"`
	TimeoutMinutes    int       `json:"timeout_minutes"`
	GitCommitterName  string    `json:"git_committer_name"`
	GitCommitterEmail string    `json:"git_committer_email"`
	ProjectRoleID     uuid.UUID `json:"project_role_id" binding:"required"`
}

// UpdateAgentRequest is the body for PATCH /projects/:projectId/agents/:agentId.
type UpdateAgentRequest struct {
	Name              *string  `json:"name"`
	Handle            *string  `json:"handle"`
	LLMProvider       *string  `json:"llm_provider"`
	LLMModel          *string  `json:"llm_model"`
	LLMAPIKey         *string  `json:"llm_api_key"`
	LLMBaseURL        *string  `json:"llm_base_url"`
	ACPProvider       *string  `json:"acp_provider"`
	ACPCommand        []string `json:"acp_command"`
	SystemPrompt      *string  `json:"system_prompt"`
	MaxIterations     *int     `json:"max_iterations"`
	TimeoutMinutes    *int     `json:"timeout_minutes"`
	GitCommitterName  *string  `json:"git_committer_name"`
	GitCommitterEmail *string  `json:"git_committer_email"`
}

// GenerateACPBridgeTokenResponse is the body returned for POST
// /projects/:projectId/agents/:agentId/acp-bridge-token. Token is shown once
// and cannot be retrieved again — only its hash is persisted.
type GenerateACPBridgeTokenResponse struct {
	Token      string `json:"token"`
	RunCommand string `json:"run_command"`
}

// AgentFromEntity maps an Agent entity to AgentResponse.
func AgentFromEntity(a *agentdom.Agent) AgentResponse {
	resp := AgentResponse{
		ID:                a.ID,
		ProjectID:         a.ProjectID,
		MemberID:          a.MemberID,
		Name:              a.Name,
		Handle:            a.Handle,
		AvatarURL:         a.AvatarURL,
		AgentType:         a.AgentType,
		LLMProvider:       a.LLMProvider,
		LLMModel:          a.LLMModel,
		LLMBaseURL:        a.LLMBaseURL,
		ACPProvider:       a.ACPProvider,
		ACPCommand:        a.ACPCommand,
		HasACPBridgeToken: a.HasACPBridgeToken,
		SystemPrompt:      a.SystemPrompt,
		MaxIterations:     a.MaxIterations,
		TimeoutMinutes:    a.TimeoutMinutes,
		GitCommitterName:  a.GitCommitterName,
		GitCommitterEmail: a.GitCommitterEmail,
		CreatedBy:         a.CreatedBy,
		CreatedAt:         a.CreatedAt,
		UpdatedAt:         a.UpdatedAt,
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

// AgentConversationResponse is the public view of a conversation.
type AgentConversationResponse struct {
	ID                  uuid.UUID  `json:"id"`
	AgentID             uuid.UUID  `json:"agent_id"`
	ProjectID           uuid.UUID  `json:"project_id"`
	TriggerType         string     `json:"trigger_type"`
	TaskID              *uuid.UUID `json:"task_id,omitempty"`
	ChatSessionID       *uuid.UUID `json:"chat_session_id,omitempty"`
	TriggeredByMemberID *uuid.UUID `json:"triggered_by_member_id,omitempty"`
	Status              string     `json:"status"`
	IterationCount      int        `json:"iteration_count"`
	BranchName          *string    `json:"branch_name,omitempty"`
	PRUrl               *string    `json:"pr_url,omitempty"`
	StartedAt           *time.Time `json:"started_at,omitempty"`
	FinishedAt          *time.Time `json:"finished_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	AgentName           string     `json:"agent_name,omitempty"`
	AgentHandle         string     `json:"agent_handle,omitempty"`
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
}

// ConversationFromEntity maps an AgentConversation entity to its DTO.
func ConversationFromEntity(c *agentdom.AgentConversation) AgentConversationResponse {
	return AgentConversationResponse{
		ID:                  c.ID,
		AgentID:             c.AgentID,
		ProjectID:           c.ProjectID,
		TriggerType:         c.TriggerType,
		TaskID:              c.TaskID,
		ChatSessionID:       c.ChatSessionID,
		TriggeredByMemberID: c.TriggeredByMemberID,
		Status:              c.Status,
		IterationCount:      c.IterationCount,
		BranchName:          c.BranchName,
		PRUrl:               c.PRUrl,
		StartedAt:           c.StartedAt,
		FinishedAt:          c.FinishedAt,
		CreatedAt:           c.CreatedAt,
		AgentName:           c.AgentName,
		AgentHandle:         c.AgentHandle,
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

// AgentChatSessionResponse is the public view of a chat session.
type AgentChatSessionResponse struct {
	ID            uuid.UUID  `json:"id"`
	AgentID       uuid.UUID  `json:"agent_id"`
	ProjectID     uuid.UUID  `json:"project_id"`
	MemberID      uuid.UUID  `json:"member_id"`
	Title         *string    `json:"title,omitempty"`
	LastMessageAt *time.Time `json:"last_message_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// StartChatSessionRequest is the body for POST /agents/:agentId/chat.
type StartChatSessionRequest struct {
	Message string `json:"message" binding:"required"`
}

// SendChatMessageRequest is the body for POST /chat-sessions/:sessionId/messages.
type SendChatMessageRequest struct {
	Message string `json:"message" binding:"required"`
}

// ChatSessionFromEntity maps an AgentChatSession entity to its DTO.
func ChatSessionFromEntity(s *agentdom.AgentChatSession) AgentChatSessionResponse {
	return AgentChatSessionResponse{
		ID:            s.ID,
		AgentID:       s.AgentID,
		ProjectID:     s.ProjectID,
		MemberID:      s.MemberID,
		Title:         s.Title,
		LastMessageAt: s.LastMessageAt,
		CreatedAt:     s.CreatedAt,
	}
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
