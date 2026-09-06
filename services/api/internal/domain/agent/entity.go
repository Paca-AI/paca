// Package agentdom defines the AI Agent aggregate and its domain contracts.
package agentdom

import (
	"fmt"
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
	// registry doesn't know about goose).
	ACPCommand []string
	// CLIProvider is one of claude-code | codex | cursor-agent | gemini-cli;
	// nil unless AgentType == provider_cli. Unlike ACPProvider (which names
	// the ACP client the *user's own machine* runs via apps/acp-bridge),
	// CLIProvider names which coding CLI Goose itself shells out to *inside*
	// agent-runner's own sandbox/environment container, using Goose's "CLI
	// providers" feature (GOOSE_PROVIDER=<CLIProvider>) — see
	// docs/ai-agent/overview.md's provider_cli section. Turn control still
	// goes through the exact same goose serve + ACP client path llm-type
	// agents use; only the underlying model provider differs.
	CLIProvider *string
	// CLIModel is passed through as GOOSE_MODEL when CLIProvider is set —
	// provider-specific free text (e.g. "sonnet"/"haiku" for claude-code),
	// not validated against Paca's own LLM model catalog.
	CLIModel string
	// CLIAuthMode is "api_key" or "login" (default). Goose itself never
	// brokers auth for a CLI provider — "Goose doesn't handle
	// authentication, it assumes the underlying CLI is already logged in
	// and functional" — so this is entirely about how *Paca* gets that CLI
	// authenticated: "api_key" injects CLIAPIKeySecret under that CLI's own
	// native non-interactive auth env var (see
	// CLIProvidersWithAPIKeyAuth); "login" requires the user to run the
	// CLI's own interactive login command in the agent's default
	// environment's terminal. cursor-agent supports "login" only in this
	// version.
	CLIAuthMode string
	// CLIAPIKeySecret is the encrypted value stored in
	// agents.cli_api_key_secret — decrypt with secret.Encryptor before
	// injecting into a container. Empty when CLIAuthMode is "login" or
	// unset. Never log this field.
	CLIAPIKeySecret string
	// CLILoginVerifiedAt is set by the "Verify login" action — a file-
	// existence probe run inside DefaultEnvironmentID, never an actual CLI
	// invocation (see docs/ai-agent/overview.md). Advisory only: never
	// re-validated automatically, so a login that later expires or is
	// revoked won't clear this. nil until the user has verified at least
	// once.
	CLILoginVerifiedAt *time.Time
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
	// DockerEnabled opts this agent into agent-runner's per-conversation
	// Docker-in-Docker sandbox sidecar (see
	// services/agent-runner/internal/sandbox/docker/dind.go and
	// services/agent-runner/internal/sandbox/k8s/dind.go) — off by default, since
	// most agents never run a Docker command and the sidecar is a real
	// per-session cost (a privileged container plus a private network) to
	// start unconditionally. LLM-only, same as SystemPrompt/GitCommitter*
	// above — an ACP agent's sandboxing is owned by the user's own local ACP
	// client, not agent-runner.
	DockerEnabled bool
	// DefaultEnvironmentID, when set, is the static environment
	// (environmentdom.Environment) this agent's conversations attach to by
	// default instead of getting a fresh ephemeral sandbox — see
	// docs/ai-agent/environment-management.md. Only meaningful for
	// project-scoped agents: a global agent has no single project's
	// environments to default to, so this stays nil for
	// AgentScopeGlobal agents (enforced by CreateAgent/UpdateAgent, not a
	// DB constraint — see that validation for why). Overridable per
	// conversation at chat-start.
	//
	// MANDATORY (not just optional) for AgentType == provider_cli: a
	// provider_cli agent's underlying CLI persists its own login
	// credentials on disk, which must survive across conversations/turns —
	// only a static environment's persistent volume provides that, so this
	// type never falls back to an ephemeral sandbox. Enforced by
	// CreateAgent/UpdateAgent and re-checked at every conversation start
	// (resolveConversationEnvironment), never just at creation time.
	DefaultEnvironmentID *uuid.UUID
	// DefaultFolderID, when set, is which folder
	// (environmentdom.EnvironmentFolder) inside DefaultEnvironmentID this
	// agent's conversations should work in by default — meaningless
	// without DefaultEnvironmentID also being set, since a folder only
	// ever belongs to exactly one environment (enforced by
	// CreateAgent/UpdateAgent, not a DB constraint — see that validation
	// for why). Overridable per conversation at chat-start, same as
	// DefaultEnvironmentID.
	DefaultFolderID *uuid.UUID
	// ParallelismLimit caps how many of this agent's conversations may be
	// status "running" at once, across every project it belongs to (a global
	// agent's conversations all still share the same default
	// environment/working directory regardless of which project triggered
	// them, so this is never scoped per-project). Defaults to 1: without an
	// explicit opt-in, an agent works through its conversations one at a
	// time rather than racing several turns against the same working
	// directory — see https://github.com/Paca-AI/paca/issues/462. A trigger
	// that would exceed it is held in agent_pending_triggers instead of
	// being dispatched — see PendingTrigger.
	ParallelismLimit int
	CreatedBy        *uuid.UUID
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        *time.Time
	// Member ID in project_members (populated on create / list)
	MemberID   *uuid.UUID
	MCPServers []*AgentMCPServer
	Skills     []*AgentSkill
	EnvVars    []*AgentEnvironmentVariable
}

// AgentType values.
const (
	AgentTypeLLM         = "llm"
	AgentTypeACP         = "acp"
	AgentTypeProviderCLI = "provider_cli"
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

// CLIProvider values — see Agent.CLIProvider's doc comment. Deliberately a
// separate set from ACPProvider's, even though claude-code/codex/gemini-cli
// spell the same as three ACP provider values: the two enumerate unrelated
// concepts (which CLI Goose shells out to inside agent-runner's own sandbox,
// vs. which ACP client the user's own machine runs), and CLIProvider adds
// cursor-agent, which has no ACP-provider equivalent.
const (
	CLIProviderClaudeCode = "claude-code"
	CLIProviderCodex      = "codex"
	CLIProviderCursor     = "cursor-agent"
	CLIProviderGeminiCLI  = "gemini-cli"
)

// ValidCLIProviders is the set of allowed cli_provider values.
var ValidCLIProviders = map[string]bool{
	CLIProviderClaudeCode: true,
	CLIProviderCodex:      true,
	CLIProviderCursor:     true,
	CLIProviderGeminiCLI:  true,
}

// CLIAuthMode values — see Agent.CLIAuthMode's doc comment.
const (
	CLIAuthModeAPIKey = "api_key"
	CLIAuthModeLogin  = "login"
)

// CLIProvidersWithAPIKeyAuth is the set of cli_provider values that support
// CLIAuthModeAPIKey — each CLI's own native non-interactive auth env var
// (see executor's cliProviderAPIKeyEnvVar on the agent-runner side),
// completely independent of Goose's own provider/API-key mechanism, which
// does not apply once GOOSE_PROVIDER names a CLI provider. cursor-agent has
// no known non-interactive API-key auth path as of this writing — login via
// the environment terminal only.
var CLIProvidersWithAPIKeyAuth = map[string]bool{
	CLIProviderClaudeCode: true,
	CLIProviderCodex:      true,
	CLIProviderGeminiCLI:  true,
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

// ConversationAudience discriminates who may read/write a conversation's
// transcript. It is a STORED GENERATED column (see migration
// 000038_add_agent_conversation_audience) derived from chat_session_id /
// actor_user_id, so it can never drift from those source columns.
type ConversationAudience string

// ConversationAudience values.
const (
	// AudienceOwnerPrivate marks a chat conversation (project chat backed by a
	// chat session, or global chat backed by actor_user_id): only the acting
	// member (project) or actor user (global) may read/write the transcript.
	AudienceOwnerPrivate ConversationAudience = "owner_private"
	// AudienceProjectShared marks a sessionless run (task_assigned /
	// comment_mention / description_write / automation_message): any project
	// member with agents:read may read it.
	AudienceProjectShared ConversationAudience = "project_shared"
)

// ContextItemType discriminates which kind of resource a ContextItemRef
// points at.
type ContextItemType string

// ContextItemType values.
const (
	ContextItemTask         ContextItemType = "task"
	ContextItemDoc          ContextItemType = "doc"
	ContextItemConversation ContextItemType = "conversation"
	ContextItemAutomation   ContextItemType = "automation"
	ContextItemAnnotation   ContextItemType = "annotation"
)

// ContextItemRef is a reference to a Task, Doc, Conversation, or Automation
// the user attached to a chat message from the frontend composer's
// context-item picker (shown as removable badges above the composer). It
// rides the send-message DTOs → the Redis/Valkey trigger stream (JSON-
// encoded into a single flat string field, since AppendFlat needs scalar
// values — see agentsvc.Service.publishTrigger) → agent-runner, which
// renders it into a "## Attached Context" prompt hint telling the agent
// which MCP tool to call for full details. It is also persisted verbatim
// (as a nested JSON array, not re-stringified) on the conversation's
// user_message event payload so the frontend can redisplay the badges when
// a conversation is reloaded. services/agent-runner keeps its own
// byte-identical copy of this type (internal/agent/context_item.go) since
// it is a separate Go module and cannot import this package — see that
// copy's doc comment.
type ContextItemRef struct {
	Type      ContextItemType `json:"type"`
	ID        string          `json:"id"`
	ProjectID *string         `json:"project_id,omitempty"`
	Title     string          `json:"title"`
}

// MaxContextItems bounds how many ContextItemRefs a single send-message
// request may carry — see ValidateContextItems. The UI's composer never
// stages more than a handful; this just keeps a malformed or abusive client
// payload from ballooning the persisted event row and every prompt built
// from it.
const MaxContextItems = 20

// MaxContextItemTitleLength bounds ContextItemRef.Title — see
// ValidateContextItems.
const MaxContextItemTitleLength = 500

// ValidateContextItems rejects a client-supplied context_items payload that
// is too large or malformed: too many items, an unrecognized Type, a blank
// ID, or an oversized Title. Called at the HTTP boundary (see the
// send-message/chat-session handlers) before the items are persisted
// verbatim and rendered into every subsequent prompt — see ContextItemRef's
// own doc comment for that data flow.
func ValidateContextItems(items []ContextItemRef) error {
	if len(items) > MaxContextItems {
		return fmt.Errorf("context_items: at most %d items allowed, got %d", MaxContextItems, len(items))
	}
	for i, item := range items {
		switch item.Type {
		case ContextItemTask, ContextItemDoc, ContextItemConversation, ContextItemAutomation, ContextItemAnnotation:
		default:
			return fmt.Errorf("context_items[%d]: unknown type %q", i, item.Type)
		}
		if item.ID == "" {
			return fmt.Errorf("context_items[%d]: id is required", i)
		}
		if len(item.Title) > MaxContextItemTitleLength {
			return fmt.Errorf("context_items[%d]: title exceeds %d characters", i, MaxContextItemTitleLength)
		}
	}
	return nil
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
	ActorUserID *uuid.UUID
	// Audience is the derived transcript audience (owner_private |
	// project_shared) — see ConversationAudience. Read-only on this entity:
	// it is a generated column, never set by the repository on write.
	Audience ConversationAudience
	Status   string // queued | running | paused | finished | failed | stopped
	// EnvironmentID/EnvironmentFolderID are set only when this conversation
	// is attached to a static environment (environmentdom.Environment)
	// instead of getting a fresh ephemeral sandbox — see
	// docs/ai-agent/environment-management.md. Replaced the old
	// ContainerID/HostPort/RepoCloneURL/BranchName/PersistenceDir columns
	// (migration 000042_add_environments.sql), which encoded a
	// different cardinality (one conversation owning one container) that a
	// many-conversations-to-one-environment model doesn't fit.
	EnvironmentID       *uuid.UUID
	EnvironmentFolderID *uuid.UUID
	IterationCount      int
	// InputTokens/OutputTokens/TotalTokens/CostUSD are computed live from
	// agent_conversation_events (event_type = 'turn_usage'), the same
	// pattern IterationCount uses — see conversationCols's doc comment in
	// the postgres repository. CostUSD is nil until at least one turn has
	// reported a cost.
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
	CostUSD      *float64
	ErrorMessage *string
	RepoPluginID *uuid.UUID
	PRUrl        *string
	StartedAt    *time.Time
	FinishedAt   *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
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

// PendingTrigger is a trigger held back from dispatch, either because its
// agent was already at ParallelismLimit running conversations (see
// Agent.ParallelismLimit's doc comment) or because its target environment
// folder already had another conversation running in it, from any agent
// (see checkFolderCapacity's doc comment) — EnvironmentID/EnvironmentFolderID
// are nil in the former case. Topic/Payload are exactly what would have
// been passed to the service's publishTrigger at the moment the
// conversation was created; AdvanceQueue/AdvanceFolderQueue replay them
// unchanged once a slot frees up. Payload is a flat string map (not
// arbitrary JSON) because that's what publishTrigger's own AppendFlat call
// requires.
type PendingTrigger struct {
	ID             uuid.UUID
	AgentID        uuid.UUID
	ConversationID uuid.UUID
	Topic          string
	Payload        map[string]string
	// EnvironmentID/EnvironmentFolderID mirror the same fields already
	// embedded (as strings) in Payload — promoted to first-class fields so
	// DequeueOldestPendingTriggerForFolder can find "whichever queued
	// trigger, from any agent, was waiting on this folder" without parsing
	// Payload. nil for a trigger that was only ever blocked by its agent's
	// own ParallelismLimit (no shared folder to protect).
	EnvironmentID       *uuid.UUID
	EnvironmentFolderID *uuid.UUID
	CreatedAt           time.Time
}

// OnBusy policy values a client may pass when sending an interactive chat
// message to an agent that is already at ParallelismLimit running
// conversations — see ChatSessionService.SendChatMessage's doc comment. The
// zero value "" ("ask") is the default: the call fails with
// apierr.CodeAgentParallelismLimitReached instead of creating anything,
// leaving it to the caller to re-request with one of the two values below.
const (
	// OnBusyQueue creates the conversation and holds its trigger in
	// PendingTrigger instead of dispatching it — the same behavior every
	// non-interactive trigger (task_assigned, comment_mention, etc.) always
	// gets, since there's no human to ask in those cases.
	OnBusyQueue = "queue"
	// OnBusyForce skips the parallelism check entirely and dispatches
	// immediately, exceeding ParallelismLimit if necessary — today's
	// unconditional pre-feature behavior.
	OnBusyForce = "force"
)

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
