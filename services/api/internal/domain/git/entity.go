// Package gitdom defines the platform-neutral Git integration: the repositories
// a project has linked, the bot identities that act on them, the events the
// platforms deliver, and the change-request cache. Platform specifics live in
// provider plugins behind platform/gitprovider; see
// docs/ai-agent/git-agent-workflow.md.
package gitdom

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// IdentityScope says whom an identity serves.
type IdentityScope string

// Identity scopes.
const (
	ScopeShared  IdentityScope = "shared"  // a workspace connection: every project may use it
	ScopeProject IdentityScope = "project" // a connection only one project may use
	ScopeAgent   IdentityScope = "agent"   // one agent's own identity
)

// Identity is a bot identity on a platform host. Secrets stay with the provider
// plugin; core keeps only what it needs to address the identity and to show it.
type Identity struct {
	ID                 uuid.UUID
	ProviderID         string
	Host               string
	ProviderIdentityID string // IdentityRef.ID
	Kind               string // app | bot_user | token
	Scope              IdentityScope
	ProjectID          *uuid.UUID // set for ScopeProject
	Handle             string     // what users @mention
	DisplayName        string
	ProviderRef        json.RawMessage // IdentityRef.ProviderRef, opaque
	ExpiresAt          *time.Time
	// RepositoryCount is how many linked repositories, in every project, use the
	// connection. Read for listings only; it is not stored.
	RepositoryCount int
	CreatedBy       *uuid.UUID
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Repository is a repository linked to a project.
type Repository struct {
	ID            uuid.UUID
	ProjectID     uuid.UUID
	ProviderID    string
	Host          string
	ExternalID    string
	FullPath      string
	DefaultBranch string
	CloneURL      string // https, never credentials
	// Description tells people and agents what the repository is for, so the
	// right one is picked in a project with several. User-written, untrusted.
	Description string
	IdentityID  *uuid.UUID
	ProviderRef json.RawMessage // RepoRef.ProviderRef, opaque
	WebhookRef  json.RawMessage
	// IdentityHandle is what users @mention to reach the bot that serves this
	// repository. Read for listings only; it is not stored on the repository.
	IdentityHandle string
	// WebhookURL is where the platform must deliver events for this repository
	// when the connection needs a webhook per repository and none could be
	// registered automatically. Read for listings only; it is not stored.
	WebhookURL string
	// WebhookAttempted tells whether Paca ever tried to register a webhook for
	// this repository (a repository carried over from a plugin's own storage
	// has not). Read for listings only; it is not stored on the repository.
	WebhookAttempted bool
	CreatedBy        *uuid.UUID
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// WebhookEndpoint is where a platform delivers events for one identity.
type WebhookEndpoint struct {
	Key        string
	ProviderID string
	IdentityID *uuid.UUID // nil until identity setup completes
	CreatedAt  time.Time
}

// InboxStatus is the lifecycle of a received event.
type InboxStatus string

// Inbox statuses.
const (
	InboxReceived InboxStatus = "received"
	InboxIgnored  InboxStatus = "ignored"
	InboxRouted   InboxStatus = "routed"
	InboxFailed   InboxStatus = "failed"
)

// InboxEvent is one normalized event for one linked repository.
type InboxEvent struct {
	EventID      string
	RepositoryID uuid.UUID
	EndpointKey  string
	Kind         string
	Action       string
	Payload      json.RawMessage
	Status       InboxStatus
	Reason       string
	ReceivedAt   time.Time
	ProcessedAt  *time.Time
}

// ChangeRequest is a pull/merge request seen on a linked repository.
type ChangeRequest struct {
	ID           uuid.UUID
	RepositoryID uuid.UUID
	Number       string
	Title        string
	State        string // open | closed | merged
	Draft        bool
	HeadRef      string
	HeadSHA      string
	BaseRef      string
	Author       string
	URL          string
	MergedAt     *time.Time
	// OpenedByAgentID/OpenedConversationID name the agent run that opened the
	// request through Paca; nil for requests opened elsewhere. Feedback and CI
	// failures on such a request go back to that agent.
	OpenedByAgentID      *uuid.UUID
	OpenedConversationID *uuid.UUID
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// ActionLogEntry audits one Git action taken on behalf of an agent or user.
type ActionLogEntry struct {
	ProjectID      *uuid.UUID
	RepositoryID   *uuid.UUID
	ConversationID *uuid.UUID
	ActorAccountID *uuid.UUID
	Tool           string
	Subject        string
	OK             bool
	Detail         string
}

// SetupSession is one identity-setup attempt: the link between the provider's
// opaque state and what core needs to finish the flow. It belongs to the user
// who started it and is single-use.
type SetupSession struct {
	State       string
	ProviderID  string
	Host        string
	Scope       IdentityScope
	AgentID     *uuid.UUID // set for ScopeAgent
	ProjectID   *uuid.UUID // set for ScopeProject
	EndpointKey string     // the webhook endpoint the new identity will own
	ReturnURL   string
	CreatedBy   uuid.UUID
	ExpiresAt   time.Time
	CompletedAt *time.Time
	CreatedAt   time.Time
}

// AccessSession is one attempt to grant an identity access to another account
// or organization, started from a project. It belongs to the user who started
// it, is bound to that project and is single-use.
type AccessSession struct {
	State       string
	ProviderID  string
	IdentityID  uuid.UUID
	ProjectID   uuid.UUID
	Owner       string // the owner the user asked for; may differ from what they granted
	ReturnURL   string
	CreatedBy   uuid.UUID
	ExpiresAt   time.Time
	CompletedAt *time.Time
	CreatedAt   time.Time
}

// IndexedComment records a comment Paca posted, so a later reply on its thread
// can be routed back to the agent that wrote it.
type IndexedComment struct {
	RepositoryID   uuid.UUID
	CommentID      string
	SubjectKind    string // issue | change_request
	SubjectNumber  string
	ThreadID       string
	AgentID        *uuid.UUID
	ConversationID *uuid.UUID
	CreatedAt      time.Time
}

// ProjectSettings is a project's Git workflow configuration.
type ProjectSettings struct {
	ProjectID               uuid.UUID
	DefaultResponderAgentID *uuid.UUID
	Paused                  bool
	// MinActorPermission is the lowest repository permission an actor needs to
	// start an agent run (read | triage | write | maintain | admin).
	MinActorPermission string
	UpdatedAt          time.Time
}

// DefaultMinActorPermission is the actor bar when a project set none.
const DefaultMinActorPermission = "write"

// TriggerKind names an event an agent can be started by.
type TriggerKind string

// Triggers an agent can be started by.
const (
	TriggerMention       TriggerKind = "mention"
	TriggerOwnCRFeedback TriggerKind = "own_cr_feedback"
	TriggerCIFailure     TriggerKind = "ci_failure"
	TriggerReviewRequest TriggerKind = "review_request"
	TriggerAssignment    TriggerKind = "assignment"
	TriggerLabel         TriggerKind = "label"
	TriggerAutoReview    TriggerKind = "auto_review"
)

// TriggerKinds lists every trigger, in display order.
var TriggerKinds = []TriggerKind{
	TriggerMention, TriggerOwnCRFeedback, TriggerCIFailure, TriggerReviewRequest,
	TriggerAssignment, TriggerLabel, TriggerAutoReview,
}

// Valid reports whether k is a defined trigger.
func (k TriggerKind) Valid() bool {
	for _, v := range TriggerKinds {
		if v == k {
			return true
		}
	}
	return false
}

// EnabledByDefault is whether an agent with no row for k has it on. Auto-review
// is opt-in: it reviews every change request without being asked.
func (k TriggerKind) EnabledByDefault() bool { return k != TriggerAutoReview }

// AgentTrigger is one agent's setting for one trigger.
type AgentTrigger struct {
	AgentID uuid.UUID
	Kind    TriggerKind
	Enabled bool
	// Config holds trigger-specific filters; for auto_review
	// {"branches": [...], "paths": [...], "authors": [...]}.
	Config json.RawMessage
}

// RunStatus is the lifecycle of an agent run an event started.
type RunStatus string

// Run statuses.
const (
	RunQueued    RunStatus = "queued"
	RunRunning   RunStatus = "running"
	RunSucceeded RunStatus = "succeeded"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
)

// Active reports whether the run has not finished.
func (s RunStatus) Active() bool { return s == RunQueued || s == RunRunning }

// Run is one agent run started by a Git event.
type Run struct {
	ID             uuid.UUID
	EventID        string
	RepositoryID   uuid.UUID
	AgentID        uuid.UUID
	Trigger        TriggerKind
	SubjectKind    string
	SubjectNumber  string
	HeadSHA        string
	ConversationID *uuid.UUID
	Status         RunStatus
	// StatusRef is the provider's reference to the run's status comment/check.
	StatusRef  string
	CreatedAt  time.Time
	FinishedAt *time.Time
}

// ProjectRun is a run with the repository it ran on, for listings.
type ProjectRun struct {
	Run
	RepositoryPath string
}

// CI states of a change request's checks.
const (
	CIPassing = "passing"
	CIFailing = "failing"
	CIPending = "pending"
)

// CISummary is the state of a change request's checks, derived from the
// platform when asked for and never stored.
type CISummary struct {
	State   string // passing | failing | pending
	Total   int
	Passed  int
	Failed  int
	Pending int
	Failing []string // names of the failing checks, capped
}

// TaskChangeRequest is a change request linked to a task, with its repository.
type TaskChangeRequest struct {
	ChangeRequest
	RepositoryPath string
	ProviderID     string
	CI             *CISummary // nil when it has no checks or they could not be read
}

// TaskBranch is a branch linked to a task.
type TaskBranch struct {
	RepositoryID   uuid.UUID
	RepositoryPath string
	ProviderID     string
	Branch         string
}

// TaskGit is what Git knows about one task.
type TaskGit struct {
	ChangeRequests []*TaskChangeRequest
	Branches       []*TaskBranch
}
