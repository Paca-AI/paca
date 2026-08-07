package automationdom

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Repository defines persistence operations for the automation aggregate.
type Repository interface {
	CreateAutomation(ctx context.Context, a *Automation) error
	FindAutomationByID(ctx context.Context, id uuid.UUID) (*Automation, error)
	ListAutomations(ctx context.Context, projectID uuid.UUID, status *Status) ([]*Automation, error)
	// UpdateAutomation persists changes to name, description, and/or status.
	UpdateAutomation(ctx context.Context, a *Automation) error
	// DeleteAutomation soft-deletes an automation (cascades nodes/edges via FK).
	DeleteAutomation(ctx context.Context, id uuid.UUID) error

	// LoadGraph returns the full node/edge set for an automation in as few
	// round trips as practical, for the single-fetch canvas GET.
	LoadGraph(ctx context.Context, automationID uuid.UUID) (*Graph, error)

	CreateNode(ctx context.Context, n *Node) error
	FindNodeByID(ctx context.Context, id uuid.UUID) (*Node, error)
	ListNodesByAutomation(ctx context.Context, automationID uuid.UUID) ([]*Node, error)
	// UpdateNode persists config and/or position changes.
	UpdateNode(ctx context.Context, n *Node) error
	DeleteNode(ctx context.Context, id uuid.UUID) error

	CreateEdge(ctx context.Context, e *Edge) error
	FindEdgeByID(ctx context.Context, id uuid.UUID) (*Edge, error)
	ListEdgesByAutomation(ctx context.Context, automationID uuid.UUID) ([]*Edge, error)
	DeleteEdge(ctx context.Context, id uuid.UUID) error

	// --- Execution-engine read paths (consumed by the automation worker) -----

	// ListEnabledTriggerNodesByType returns every trigger-kind node of
	// triggerType whose owning automation is active and belongs to
	// projectID — the hot lookup made on every candidate event. Further
	// per-node config matching (e.g. a specific status_id) happens in Go
	// after this call.
	ListEnabledTriggerNodesByType(ctx context.Context, projectID uuid.UUID, triggerType TriggerType) ([]*Node, error)
	// ListPredecessorTriggersWatching returns every enabled predecessor_done
	// trigger node whose watched_task_ids includes taskID — used to
	// re-evaluate the AND-join whenever any watched task's status changes.
	ListPredecessorTriggersWatching(ctx context.Context, taskID uuid.UUID) ([]*Node, error)
	// ListOutgoingEdges returns every edge whose source is sourceNodeID.
	ListOutgoingEdges(ctx context.Context, sourceNodeID uuid.UUID) ([]*Edge, error)
	// FindAutomationByNodeID returns the automation owning nodeID, used to
	// re-check it's still active mid-walk (an automation can be
	// archived/reverted-to-draft while a run is in flight).
	FindAutomationByNodeID(ctx context.Context, nodeID uuid.UUID) (*Automation, error)

	// --- Runs / run steps ------------------------------------------------------

	CreateRun(ctx context.Context, r *Run) error
	UpdateRun(ctx context.Context, r *Run) error
	ListRunsByAutomation(ctx context.Context, automationID uuid.UUID, limit int) ([]*Run, error)
	CreateRunStep(ctx context.Context, s *RunStep) error
	ListRunStepsByRun(ctx context.Context, runID uuid.UUID) ([]*RunStep, error)

	// --- trigger_ai_agent pause/resume ------------------------------------------

	// CreatePendingAgentWait records that a graph walk is paused at
	// w.NodeID, waiting on w.ConversationID to reach a terminal status.
	CreatePendingAgentWait(ctx context.Context, w *PendingAgentWait) error
	// ClaimPendingAgentWait atomically deletes and returns the pending wait
	// for conversationID, or (nil, nil) if none exists — either the
	// conversation never had one (most conversations aren't automation-
	// driven at all) or it was already claimed by an earlier, at-least-once
	// redelivery of the same terminal-status message.
	ClaimPendingAgentWait(ctx context.Context, conversationID uuid.UUID) (*PendingAgentWait, error)
	// CountPendingAgentWaits reports how many waits are still outstanding
	// for runID — used to decide whether a run can be finalized yet.
	CountPendingAgentWaits(ctx context.Context, runID uuid.UUID) (int, error)

	// --- wait pause/resume -------------------------------------------------------

	// CreatePendingDelay records that a graph walk is paused at d.NodeID
	// until d.ResumeAt.
	CreatePendingDelay(ctx context.Context, d *PendingDelay) error
	// ClaimDueDelays atomically deletes and returns every pending delay
	// whose ResumeAt has passed — a scheduler tick can have several due at
	// once, unlike ClaimPendingAgentWait's single-row-by-key claim.
	ClaimDueDelays(ctx context.Context) ([]*PendingDelay, error)
	// CountPendingDelays reports how many delays are still outstanding for
	// runID — used alongside CountPendingAgentWaits to decide whether a run
	// can be finalized yet.
	CountPendingDelays(ctx context.Context, runID uuid.UUID) (int, error)

	// --- Due-date scheduling ---------------------------------------------------

	// ListDueDateCandidates returns, for every enabled due_date_reached
	// trigger node, the tasks whose due date currently falls within that
	// node's configured offset window and haven't already fired for that
	// node (see HasDueDateFired). Returned as (node, taskID) pairs.
	ListDueDateCandidates(ctx context.Context) ([]DueDateCandidate, error)
	// HasDueDateFired reports whether nodeID has already fired for taskID.
	HasDueDateFired(ctx context.Context, nodeID, taskID uuid.UUID) (bool, error)
	// RecordDueDateFire marks (nodeID, taskID) as fired so it isn't refired
	// on a later poll tick.
	RecordDueDateFire(ctx context.Context, automationID, nodeID, taskID uuid.UUID) error

	// --- Cron scheduling --------------------------------------------------------

	// ListCronCandidates returns every enabled TriggerCron node paired with
	// the last time it fired (nil if never), for the cron scheduler's
	// per-tick "is it due" check.
	ListCronCandidates(ctx context.Context) ([]CronCandidate, error)
	// RecordCronFire upserts nodeID's last-fired-at timestamp — cron is
	// recurring, so (unlike RecordDueDateFire) this is a single row per
	// node kept current, not an append-only log.
	RecordCronFire(ctx context.Context, automationID, nodeID uuid.UUID, firedAt time.Time) error

	// --- Webhook trigger tokens -------------------------------------------------

	// CreateOrRotateWebhookToken atomically revokes nodeID's current active
	// token (if any) and inserts tok as the new active one, storing
	// tokenHash (the raw token is never persisted).
	CreateOrRotateWebhookToken(ctx context.Context, tok *WebhookToken, tokenHash string) (*WebhookToken, error)
	// FindActiveWebhookTokenByNodeID returns nodeID's current non-revoked
	// token (for display: prefix/created_at/last_used_at — never the
	// hash), or ErrNotFound if none has been generated (or all have been
	// revoked without a replacement).
	FindActiveWebhookTokenByNodeID(ctx context.Context, nodeID uuid.UUID) (*WebhookToken, error)
	// VerifyWebhookToken reports whether tokenHash matches nodeID's current
	// active token by doing the hash comparison as part of the query (same
	// pattern as apikeydom.Repository.FindByHash), returning ErrWebhookTokenInvalid
	// — not ErrNotFound — on any mismatch, so a wrong hash and "no token was
	// ever generated for this node" are indistinguishable to the caller.
	VerifyWebhookToken(ctx context.Context, nodeID uuid.UUID, tokenHash string) (*WebhookToken, error)
	// RecordWebhookTokenUsed best-effort timestamps a successful webhook
	// call for display in the UI.
	RecordWebhookTokenUsed(ctx context.Context, tokenID uuid.UUID, at time.Time) error

	// --- Read path for the "is this status/member/field referenced" guard ----

	// StatusUsedByAutomation reports whether any non-deleted automation's
	// node config references statusID. Conservative by design (a simple
	// containment check across the whole config blob): a false positive
	// (blocking a status delete that's actually unrelated) is safe, a false
	// negative (silently orphaning a live automation's config) is not.
	StatusUsedByAutomation(ctx context.Context, statusID uuid.UUID) (bool, error)
}

// DueDateCandidate pairs a due_date_reached trigger node with a task whose
// due date currently falls within that node's offset window.
type DueDateCandidate struct {
	Node   *Node
	TaskID uuid.UUID
}
