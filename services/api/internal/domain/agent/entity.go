// Package agentdom defines the AI Agent aggregate and its domain contracts.
package agentdom

import (
	"time"

	"github.com/google/uuid"
)

// Agent represents an AI agent belonging to a project.
type Agent struct {
	ID              uuid.UUID
	ProjectID       uuid.UUID
	Name            string
	Handle          string
	AvatarURL       *string
	AgentType       string // llm | acp
	LLMProvider     string
	LLMModel        string
	LLMAPIKeySecret string // reference to secrets store entry
	LLMBaseURL      string
	// ACPProvider is one of claude-code | codex | gemini-cli | custom; nil for
	// llm-type agents.
	ACPProvider *string
	// ACPCommand is the command + args used to launch the ACP server. Only
	// meaningful (and required) when ACPProvider == "custom" — built-in
	// providers resolve a default command via the OpenHands SDK's own
	// provider registry.
	ACPCommand []string
	// HasACPBridgeToken reports whether a local-bridge auth token has been
	// generated; the token itself (and its hash) are never exposed here.
	HasACPBridgeToken bool
	// ACPBridgeTokenHash is the SHA-256 hex digest of the current bridge
	// token, used only for verification — never serialized to API responses.
	ACPBridgeTokenHash string
	SystemPrompt       string
	MaxIterations      int
	TimeoutMinutes     int
	GitCommitterName   string
	GitCommitterEmail  string
	CreatedBy          *uuid.UUID
	CreatedAt          time.Time
	UpdatedAt          time.Time
	DeletedAt          *time.Time
	// Member ID in project_members (populated on create / list)
	MemberID   *uuid.UUID
	MCPServers []*AgentMCPServer
	Skills     []*AgentSkill
	EnvVars    []*AgentEnvironmentVariable
}

// AgentType values.
const (
	AgentTypeLLM = "llm"
	AgentTypeACP = "acp"
)

// ACPProvider values.
const (
	ACPProviderClaudeCode = "claude-code"
	ACPProviderCodex      = "codex"
	ACPProviderGeminiCLI  = "gemini-cli"
	ACPProviderCustom     = "custom"
)

// ValidACPProviders is the set of allowed acp_provider values.
var ValidACPProviders = map[string]bool{
	ACPProviderClaudeCode: true,
	ACPProviderCodex:      true,
	ACPProviderGeminiCLI:  true,
	ACPProviderCustom:     true,
}

// AgentMCPServer is a custom MCP server configuration attached to an agent.
type AgentMCPServer struct {
	ID         uuid.UUID
	AgentID    uuid.UUID
	ServerName string
	Transport  string // stdio | sse | http
	Command    *string
	Args       []string
	URL        *string
	Env        map[string]string
	IsEnabled  bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// AgentEnvironmentVariable is a secret environment variable injected into an
// agent's sandbox container at run time. Value is always stored encrypted;
// the plaintext is never persisted on this struct once it round-trips
// through the repository.
type AgentEnvironmentVariable struct {
	ID             uuid.UUID
	AgentID        uuid.UUID
	Key            string
	EncryptedValue string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// AgentSkill is a skill associated with an agent.
type AgentSkill struct {
	ID           uuid.UUID
	AgentID      uuid.UUID
	SkillName    string
	SkillSource  string // inline | marketplace | github_url
	SkillContent string
	SourceURL    *string
	Triggers     []string
	IsEnabled    bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ReservedSkillNames are names the ai-agent service assigns itself, per
// conversation trigger type, as fixed scaffolding (see
// services/ai-agent/src/agent/trigger_skills.py). They are always appended
// to an agent's skill list and are not meant to be user- or plugin-editable;
// a skill with one of these names would collide and fail conversation setup
// (AgentContext rejects duplicate skill names). Keep in sync with
// trigger_skills.py's get_trigger_skill().
var ReservedSkillNames = map[string]bool{
	"paca-trigger-task-assigned":     true,
	"paca-trigger-doc-comment":       true,
	"paca-trigger-chat":              true,
	"paca-trigger-description-write": true,
}

// IsReservedSkillName reports whether name is one of the fixed trigger-skill
// names reserved by the ai-agent runtime (see ReservedSkillNames). Both
// agent-owned skills and plugin-contributed skills are checked against this
// at creation/validation time.
func IsReservedSkillName(name string) bool {
	return ReservedSkillNames[name]
}

// SkillTemplate is a reusable, hardcoded skill definition that users can
// browse and apply when configuring their agents.  Templates are defined
// in code (not in the database) so they are always available without
// migrations and cannot be accidentally deleted.
type SkillTemplate struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Content     string   `json:"content"`
	Triggers    []string `json:"triggers"`
}

// AgentConversation tracks each OpenHands conversation session.
type AgentConversation struct {
	ID            uuid.UUID
	AgentID       uuid.UUID
	ProjectID     uuid.UUID
	TriggerType   string // task_assigned | comment_mention | chat_message | description_write
	TaskID        *uuid.UUID
	CommentID     *uuid.UUID
	ChatSessionID *uuid.UUID
	// TriggeredByMemberID is nil for conversations triggered by the
	// automation-workflow engine, which has no human member behind it.
	TriggeredByMemberID *uuid.UUID
	Status              string // queued | running | paused | finished | failed | stopped
	ContainerID         *string
	HostPort            *int
	IterationCount      int
	ErrorMessage        *string
	RepoPluginID        *uuid.UUID
	RepoCloneURL        *string
	BranchName          *string
	PRUrl               *string
	PersistenceDir      *string
	StartedAt           *time.Time
	FinishedAt          *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
	// Populated by JOIN
	AgentName   string
	AgentHandle string
	TaskTitle   *string
}

// AgentConversationEvent is a single event emitted by the OpenHands SDK.
type AgentConversationEvent struct {
	ID             uuid.UUID
	ConversationID uuid.UUID
	EventIndex     int
	EventType      string
	EventSource    string // agent | user | system
	Payload        map[string]any
	CreatedAt      time.Time
}

// AgentChatSession is a persistent chat session between a user and an agent.
type AgentChatSession struct {
	ID            uuid.UUID
	AgentID       uuid.UUID
	ProjectID     uuid.UUID
	MemberID      uuid.UUID
	Title         *string
	LastMessageAt *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
