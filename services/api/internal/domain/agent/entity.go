// Package agentdom defines the AI Agent aggregate and its domain contracts.
package agentdom

import (
	"time"

	"github.com/google/uuid"
)

// Agent represents an AI agent. A project-scoped agent (AgentScope ==
// AgentScopeProject) belongs to exactly one project (ProjectID set); a
// global agent (AgentScope == AgentScopeGlobal) belongs to none (ProjectID
// is the zero value, uuid.Nil) and is instead attached to zero or more
// projects indirectly via project_members rows, the same "add a member"
// mechanism used for humans (see project.MemberService.AddMember). This
// mirrors the existing ProjectMember.UserID convention ("zero-value for
// agent members") rather than using a pointer, to avoid rippling a type
// change through every existing project-scoped call site.
type Agent struct {
	ID        uuid.UUID
	ProjectID uuid.UUID // zero value (uuid.Nil) for global-scope agents
	// AgentScope is "project" (default) or "global". See the Agent doc
	// comment above.
	AgentScope AgentScope
	// GlobalRoleID is the global_roles row governing what this agent may do
	// via admin-shaped tools (create users, manage global roles, manage
	// projects) when acting with no project context. Only ever set for
	// AgentScopeGlobal agents; nil means no global-scope permissions.
	GlobalRoleID *uuid.UUID
	Name         string
	Handle       string
	// AvatarKey and AvatarThumbKey are object-storage keys for the two
	// server-generated avatar variants (256x256 full, 64x64 thumb). Both nil
	// when no avatar has been uploaded. See attachmentdom.AvatarService.
	AvatarKey       *string
	AvatarThumbKey  *string
	AgentType       string // llm | acp
	LLMProvider     string
	LLMModel        string
	LLMAPIKeySecret string // reference to secrets store entry
	LLMBaseURL      string
	// ACPProvider is one of claude-code | codex | gemini-cli | goose | custom;
	// nil for llm-type agents.
	ACPProvider *string
	// ACPCommand is the command + args used to launch the ACP server. Only
	// meaningful (and required) when ACPProvider == "custom" — the other
	// built-in providers resolve a default command themselves: claude-code /
	// codex / gemini-cli via the OpenHands SDK's own provider registry, goose
	// via a small local override in apps/acp-bridge's runner.py (the SDK's
	// registry doesn't know about goose — see docs/ai-agent/goose-migration.md).
	ACPCommand []string
	// HasACPBridgeToken reports whether a local-bridge auth token has been
	// generated; the token itself (and its hash) are never exposed here.
	HasACPBridgeToken bool
	// ACPBridgeTokenHash is the SHA-256 hex digest of the current bridge
	// token, used only for verification — never serialized to API responses.
	ACPBridgeTokenHash string
	// HasMCPAPIKey reports whether an MCP API key has been generated; the
	// key itself (and its hash) are never exposed here.
	HasMCPAPIKey bool
	// MCPAPIKeyHash is the SHA-256 hex digest of the current MCP API key,
	// used only for verification — never serialized to API responses.
	// Generating a new key overwrites this, so the previous key stops
	// authenticating immediately (only ever one live key per agent).
	MCPAPIKeyHash string
	// SystemPrompt, GitCommitterName, and GitCommitterEmail are LLM-only —
	// an ACP agent runs via the user's own local ACP client (see
	// ACPProvider), which owns its own system prompt and git identity, so
	// Paca never forwards these; they stay zero-valued on acp-type agents
	// (see CreateAgent/UpdateAgent in service/agent).
	SystemPrompt      string
	MaxIterations     int
	TimeoutMinutes    int
	GitCommitterName  string
	GitCommitterEmail string
	CreatedBy         *uuid.UUID
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DeletedAt         *time.Time
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

// AgentScope discriminates a project-owned agent from an instance-wide
// (global) one. See the Agent doc comment.
type AgentScope string

// AgentScope values.
const (
	AgentScopeProject AgentScope = "project"
	AgentScopeGlobal  AgentScope = "global"
)

// ACPProvider values.
const (
	ACPProviderClaudeCode = "claude-code"
	ACPProviderCodex      = "codex"
	ACPProviderGeminiCLI  = "gemini-cli"
	ACPProviderGoose      = "goose"
	ACPProviderCustom     = "custom"
)

// ValidACPProviders is the set of allowed acp_provider values.
var ValidACPProviders = map[string]bool{
	ACPProviderClaudeCode: true,
	ACPProviderCodex:      true,
	ACPProviderGeminiCLI:  true,
	ACPProviderGoose:      true,
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
	"paca-trigger-automation":        true,
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
	ProjectID     uuid.UUID // zero value (uuid.Nil) for a global-chat conversation
	TriggerType   string    // task_assigned | comment_mention | chat_message | description_write | automation_message
	TaskID        *uuid.UUID
	CommentID     *uuid.UUID
	ChatSessionID *uuid.UUID
	// TriggeredByMemberID is nil for conversations triggered by the
	// automation-workflow engine (no human member behind it) OR by the
	// global chat (ActorUserID is set instead — see below). Never set
	// together with ActorUserID.
	TriggeredByMemberID *uuid.UUID
	// ActorUserID is set only for a global-chat conversation (ProjectID is
	// uuid.Nil): the human user who sent the message, identified directly
	// since there may be no project_members row for them at all.
	ActorUserID    *uuid.UUID
	Status         string // queued | running | paused | finished | failed | stopped
	ContainerID    *string
	HostPort       *int
	IterationCount int
	ErrorMessage   *string
	RepoPluginID   *uuid.UUID
	RepoCloneURL   *string
	BranchName     *string
	PRUrl          *string
	PersistenceDir *string
	StartedAt      *time.Time
	FinishedAt     *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
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
// A project-scoped session (started from a project's own chat) has
// ProjectID and MemberID set and ActorUserID nil; a global-chat session
// (started from the home page / admin pages, no project context) has
// ProjectID and MemberID zero-valued and ActorUserID set instead — the
// human's identity carried directly, since they may not be a member of any
// project.
type AgentChatSession struct {
	ID            uuid.UUID
	AgentID       uuid.UUID
	ProjectID     uuid.UUID // zero value (uuid.Nil) for a global chat session
	MemberID      uuid.UUID // zero value (uuid.Nil) for a global chat session
	ActorUserID   *uuid.UUID
	Title         *string
	LastMessageAt *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
