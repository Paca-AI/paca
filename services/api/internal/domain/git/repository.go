package gitdom

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Store is the persistence port of the Git integration.
type Store interface {
	// ── identities ──
	CreateIdentity(ctx context.Context, id *Identity) error
	FindIdentity(ctx context.Context, id uuid.UUID) (*Identity, error)
	FindIdentityByProviderRef(ctx context.Context, providerID, providerIdentityID string) (*Identity, error)
	ListSharedIdentities(ctx context.Context, providerID string) ([]*Identity, error)
	// ListProjectIdentities returns the connections a project can use: every
	// workspace connection plus the ones created for this project only.
	ListProjectIdentities(ctx context.Context, projectID uuid.UUID) ([]*Identity, error)
	SetAgentIdentity(ctx context.Context, agentID uuid.UUID, providerID string, identityID uuid.UUID) error
	FindAgentIdentity(ctx context.Context, agentID uuid.UUID, providerID string) (*Identity, error)
	DeleteIdentity(ctx context.Context, id uuid.UUID) error
	// DeleteAgentIdentity removes an agent's mapping (not the identity itself).
	DeleteAgentIdentity(ctx context.Context, agentID uuid.UUID, providerID string) error

	// ── identity setup sessions ──
	CreateSetupSession(ctx context.Context, s *SetupSession) error
	FindSetupSession(ctx context.Context, state string) (*SetupSession, error)
	// CompleteSetupSession marks the flow finished so its state cannot be reused.
	CompleteSetupSession(ctx context.Context, state string) error

	// ── access grant sessions ──
	CreateAccessSession(ctx context.Context, s *AccessSession) error
	FindAccessSession(ctx context.Context, state string) (*AccessSession, error)
	CompleteAccessSession(ctx context.Context, state string) error

	// ── webhook endpoints ──
	CreateWebhookEndpoint(ctx context.Context, e *WebhookEndpoint) error
	FindWebhookEndpoint(ctx context.Context, key string) (*WebhookEndpoint, error)
	BindWebhookEndpoint(ctx context.Context, key string, identityID uuid.UUID) error
	// DeleteWebhookEndpoint drops an endpoint that never got an identity.
	DeleteWebhookEndpoint(ctx context.Context, key string) error
	FindWebhookEndpointForIdentity(ctx context.Context, identityID uuid.UUID) (*WebhookEndpoint, error)

	// ── repositories ──
	CreateRepository(ctx context.Context, r *Repository) error // ErrAlreadyLinked on duplicate
	FindRepository(ctx context.Context, id uuid.UUID) (*Repository, error)
	ListProjectRepositories(ctx context.Context, projectID uuid.UUID) ([]*Repository, error)
	DeleteRepository(ctx context.Context, projectID, id uuid.UUID) error
	// WebhookAttempts returns the project's repositories for which a webhook
	// registration was ever attempted (successfully or not), from the audit log.
	WebhookAttempts(ctx context.Context, projectID uuid.UUID) (map[uuid.UUID]bool, error)
	// SetRepositoryDescription replaces a repository's description; ErrNotFound
	// when it is not linked to the project.
	SetRepositoryDescription(ctx context.Context, projectID, id uuid.UUID, description string) error
	// ListIdentityRepositories returns every repository (of any project) linked
	// through the connection.
	ListIdentityRepositories(ctx context.Context, identityID uuid.UUID) ([]*Repository, error)
	// SetRepositoryWebhook records (or, with an empty ref, clears) the webhook
	// registered on the platform for a repository.
	SetRepositoryWebhook(ctx context.Context, id uuid.UUID, ref json.RawMessage) error
	// SetRepositoryIdentity rebinds a repository to a connection (after its own
	// was deleted), with the provider-private reference that connection resolved,
	// and clears the webhook registered through the old one.
	SetRepositoryIdentity(ctx context.Context, id, identityID uuid.UUID, providerRef json.RawMessage) error
	// CountRepositories reports how many linked repositories (in every project)
	// use each connection.
	CountRepositories(ctx context.Context, identityIDs []uuid.UUID) (map[uuid.UUID]int, error)
	// FindLinkedRepositories returns every project's link to a repository
	// (case-insensitive path); it is the fan-out for an incoming event.
	FindLinkedRepositories(ctx context.Context, providerID, host, fullPath string) ([]*Repository, error)

	// ── event inbox ──
	// InsertInboxEvent stores the event unless (event id, repository) was seen,
	// reporting whether it was new — the idempotency guard for redelivery.
	InsertInboxEvent(ctx context.Context, e *InboxEvent) (inserted bool, err error)

	// DeleteInboxEvent removes an inbox row, so a delivery that could not be
	// fully processed is not taken for a replay when the platform sends it again.
	DeleteInboxEvent(ctx context.Context, eventID string, repositoryID uuid.UUID) error

	// ── change requests and task links ──
	UpsertChangeRequest(ctx context.Context, cr *ChangeRequest) (*ChangeRequest, error)
	LinkTaskToChangeRequest(ctx context.Context, taskID, changeRequestID uuid.UUID) error
	LinkTaskBranch(ctx context.Context, taskID, repositoryID uuid.UUID, branch string) error
	// ListTaskGit returns the change requests and branches linked to a task of the project.
	ListTaskGit(ctx context.Context, projectID, taskID uuid.UUID) (*TaskGit, error)
	// TaskInProject reports whether the task belongs to the project.
	TaskInProject(ctx context.Context, projectID, taskID uuid.UUID) (bool, error)
	UnlinkTaskFromChangeRequest(ctx context.Context, taskID, changeRequestID uuid.UUID) error
	UnlinkTaskBranch(ctx context.Context, taskID, repositoryID uuid.UUID, branch string) error
	// FindTaskByRef resolves "PREFIX-123" within a project to a task id.
	FindTaskByRef(ctx context.Context, projectID uuid.UUID, prefix string, number int) (uuid.UUID, bool, error)

	FindChangeRequest(ctx context.Context, repositoryID uuid.UUID, number string) (*ChangeRequest, error)

	// ── comment index ──
	IndexComment(ctx context.Context, c *IndexedComment) error
	FindIndexedComment(ctx context.Context, repositoryID uuid.UUID, commentID string) (*IndexedComment, error)
	// LatestIndexedForSubject returns the most recent comment Paca posted on a
	// subject, or ErrNotFound.
	LatestIndexedForSubject(ctx context.Context, repositoryID uuid.UUID, subjectKind, number string) (*IndexedComment, error)

	// ── workflow settings ──
	// GetProjectSettings returns the defaults when the project set none.
	GetProjectSettings(ctx context.Context, projectID uuid.UUID) (*ProjectSettings, error)
	UpsertProjectSettings(ctx context.Context, s *ProjectSettings) error
	ListAgentTriggers(ctx context.Context, agentID uuid.UUID) ([]*AgentTrigger, error)
	UpsertAgentTrigger(ctx context.Context, t *AgentTrigger) error

	// ── routing ──
	// ClaimInboxEvents hands out up to limit events still to route, marking them
	// claimed so a second worker skips them; an abandoned claim is handed out
	// again after a few minutes.
	// Only events at least minAge old are handed out, so a burst (a review and
	// its inline comments) is routed together.
	ClaimInboxEvents(ctx context.Context, limit int, minAge time.Duration) ([]*InboxEvent, error)
	FinishInboxEvent(ctx context.Context, eventID string, repositoryID uuid.UUID, status InboxStatus, reason string) error
	// ReleaseInboxEvent gives a claimed event back, to be handed out again after retryAfter.
	ReleaseInboxEvent(ctx context.Context, eventID string, repositoryID uuid.UUID, retryAfter time.Duration) error
	// FindAgentForIdentity returns the agent an agent-scoped identity belongs to.
	FindAgentForIdentity(ctx context.Context, identityID uuid.UUID) (agentID uuid.UUID, found bool, err error)
	// CountRuns counts the runs of a trigger an agent had on a subject.
	CountRuns(ctx context.Context, repositoryID, agentID uuid.UUID, trigger TriggerKind, subjectKind, number string) (int, error)
	// CreateRun stores a run, reporting false when (event, repository, agent) already has one.
	CreateRun(ctx context.Context, r *Run) (created bool, err error)
	UpdateRun(ctx context.Context, r *Run) error
	ListActiveRuns(ctx context.Context) ([]*Run, error)
	// ListProjectRuns returns a project's most recent runs, newest first.
	ListProjectRuns(ctx context.Context, projectID uuid.UUID, limit int) ([]*ProjectRun, error)
	// FindActiveRunForSubject finds an unfinished run of the agent on a subject.
	FindActiveRunForSubject(ctx context.Context, repositoryID, agentID uuid.UUID, subjectKind, number string) (*Run, error)

	// ── audit ──
	AppendActionLog(ctx context.Context, e *ActionLogEntry) error
}
