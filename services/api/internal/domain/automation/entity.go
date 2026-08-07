// Package automationdom defines the unified automation-graph aggregate and
// its domain contracts.
//
// An Automation is a project-scoped, active/inactive graph of typed
// steps — Trigger, Condition, Action — connected by directed edges. Unlike
// the workflow feature it replaces, a node no longer wraps a task: a Trigger
// node matches ANY task satisfying its type+config against an incoming
// event, a Condition node branches on a single field comparison evaluated
// against that event's task, and an Action node mutates it.
//
// Every automation whose trigger matches an event runs independently (no
// cross-automation priority); competing outcomes for the same decision are
// expressed as a single Condition node's branches, evaluated in order,
// first-true-wins, with an optional "else" fallback.
//
// "Done" (used by the predecessor_done trigger) is not tracked by this
// package at all — it means task_statuses.category = 'done', a concept that
// already exists independent of automation.
package automationdom

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
)

// Status is the lifecycle state of an Automation.
type Status string

const (
	// StatusActive means the engine evaluates this automation on every
	// relevant task event. The graph can still be edited while active; the
	// engine reads it fresh per event, so an edit takes effect on the next
	// event rather than requiring a deactivate/reactivate round-trip.
	StatusActive Status = "active"
	// StatusInactive means the engine ignores this automation — the initial
	// state for a newly created automation, and where Deactivate returns it
	// to. The graph remains fully editable while inactive.
	StatusInactive Status = "inactive"
)

// ValidStatuses is the set of allowed automation status values.
var ValidStatuses = map[Status]bool{
	StatusActive:   true,
	StatusInactive: true,
}

// Kind discriminates the three node kinds a graph can contain.
type Kind string

// The three node kinds.
const (
	KindTrigger   Kind = "trigger"
	KindCondition Kind = "condition"
	KindAction    Kind = "action"
)

// ValidKinds is the set of allowed node kind values.
var ValidKinds = map[Kind]bool{
	KindTrigger:   true,
	KindCondition: true,
	KindAction:    true,
}

// TriggerType enumerates the built-in Trigger node types. A plugin may
// contribute additional, reverse-DNS-namespaced trigger types not listed
// here — see the plugin automation bridge.
type TriggerType string

// Built-in trigger types.
const (
	TriggerStatusChanged   TriggerType = "status_changed"
	TriggerTaskCreated     TriggerType = "task_created"
	TriggerAssigneeChanged TriggerType = "assignee_changed"
	TriggerPriorityChanged TriggerType = "priority_changed"
	TriggerTagAdded        TriggerType = "tag_added"
	TriggerDueDateReached  TriggerType = "due_date_reached"
	// TriggerPredecessorDone subsumes the old workflow canvas's dependency
	// edges: it fires when ALL of its WatchedTaskIDs are (re-checked fresh,
	// statelessly, every time — see TriggerConfig) currently in a status
	// whose category is "done".
	TriggerPredecessorDone TriggerType = "predecessor_done"
	// TriggerCron fires on a recurring schedule (TriggerConfig.CronExpression)
	// rather than reacting to a task event. Like TriggerPredecessorDone, it
	// has no natural task of its own, so it always carries a fixed
	// TargetTaskID — the task the graph runs against on every fire.
	TriggerCron TriggerType = "cron"
	// TriggerAPITrigger fires when an external caller POSTs to this node's
	// webhook endpoint with a valid secret token (see WebhookToken). Like
	// TriggerCron, it always carries a fixed TargetTaskID.
	TriggerAPITrigger TriggerType = "api_trigger"

	// The four sprint triggers below start a walk with a Sprint in context
	// instead of a Task (see the worker's executeRunForSprint) — fired from
	// StreamSprintActivities, not the task-activity stream. TriggerConfig's
	// SprintID optionally narrows to one sprint, the same pattern
	// TriggerStatusChanged's StatusID uses to narrow to one status.
	TriggerSprintCreated TriggerType = "sprint_created"
	// TriggerSprintStarted fires on a sprint's transition into Active
	// status — the only status transition sprintsvc.Service distinguishes
	// today (see UpdateSprint's wasActive diff).
	TriggerSprintStarted   TriggerType = "sprint_started"
	TriggerSprintCompleted TriggerType = "sprint_completed"
	TriggerSprintDeleted   TriggerType = "sprint_deleted"
)

// ValidBuiltinTriggerTypes is the set of built-in trigger type values.
var ValidBuiltinTriggerTypes = map[TriggerType]bool{
	TriggerStatusChanged:   true,
	TriggerTaskCreated:     true,
	TriggerAssigneeChanged: true,
	TriggerPriorityChanged: true,
	TriggerTagAdded:        true,
	TriggerDueDateReached:  true,
	TriggerPredecessorDone: true,
	TriggerCron:            true,
	TriggerAPITrigger:      true,
	TriggerSprintCreated:   true,
	TriggerSprintStarted:   true,
	TriggerSprintCompleted: true,
	TriggerSprintDeleted:   true,
}

// ConditionNodeType is the sole "type" value for kind=condition nodes (there
// is only one condition node shape — an N-branch switch — unlike triggers
// and actions, which have several distinct types).
const ConditionNodeType = "condition"

// ActionType enumerates the built-in Action node types. A plugin may
// contribute additional, reverse-DNS-namespaced action types not listed
// here — see the plugin automation bridge.
type ActionType string

// Built-in action types.
const (
	// ActionUpdateTask sets one or more task fields at once (ActionConfig's
	// Update) — replaces the old one-node-per-field actions (assign,
	// set_status, set_priority, add_tag, set_custom_field), each of which
	// could only ever touch a single field per node.
	ActionUpdateTask     ActionType = "update_task"
	ActionTriggerAIAgent ActionType = "trigger_ai_agent"
	// ActionCallAPI makes an outbound HTTP request (ActionConfig's
	// Method/URL/Headers/Body) when the graph walk reaches it. Unlike every
	// other built-in action, it doesn't mutate the task, so it has no
	// "already applied" idempotency check — it always executes when visited.
	ActionCallAPI ActionType = "call_api"
	// ActionWait pauses the graph walk for ActionConfig.WaitMinutes before
	// continuing to its outgoing edges — no task involved, just a clock.
	// Mirrors trigger_ai_agent's pause-the-walk shape (see the worker's
	// PendingDelay/walkWait/WaitScheduler) rather than call_api's
	// run-to-completion one.
	ActionWait ActionType = "wait"
	// ActionUpdateSprint and ActionCompleteSprint resolve their target
	// sprint from the walk's own context (w.sprint if Sprint-triggered, else
	// task.SprintID if Task-triggered — see the worker's resolveSprint), not
	// from a Target the way task actions do. ActionCompleteSprint is its own
	// action rather than folded into ActionUpdateSprint's generic field-set,
	// since completing has the extra bulk-task-move side effect
	// sprintsvc.CompleteSprint provides (ActionConfig.MoveToSprintID) that a
	// plain status-field update wouldn't get.
	ActionUpdateSprint   ActionType = "update_sprint"
	ActionCompleteSprint ActionType = "complete_sprint"
)

// ValidBuiltinActionTypes is the set of built-in action type values.
var ValidBuiltinActionTypes = map[ActionType]bool{
	ActionUpdateTask:     true,
	ActionTriggerAIAgent: true,
	ActionCallAPI:        true,
	ActionWait:           true,
	ActionUpdateSprint:   true,
	ActionCompleteSprint: true,
}

// NodeRequiresTask reports whether a node needs a task in context to
// execute: true for every condition node (built-in or plugin — both
// evaluate task fields, though a condition using one of the sprint-scoped
// Fields tolerates a nil task too — see the worker's walk()) and every
// built-in action except call_api, trigger_ai_agent, wait, update_sprint,
// and complete_sprint. call_api operates on static config with no task
// involved; trigger_ai_agent starts an agent conversation either way, task
// bound (see the worker's applyTriggerAIAgentOnTask and
// agentsvc.TriggerTaskAssigned) or, with no task, as a standalone message
// (see applyDirectAgentMessage and agentsvc.TriggerDirectMessage) — so it
// works either way, and neither path touches the task's assignee. wait just
// pauses the walk for a duration, no task touched either. update_sprint/
// complete_sprint resolve their own sprint from context (see
// ActionUpdateSprint's doc comment) rather than strictly requiring a task —
// they can run from a Sprint-triggered walk with no task at all. A
// plugin-contributed action is conservatively treated as requiring a task,
// since there's no manifest flag today declaring otherwise.
//
// This is the single source of truth for two enforcement points that must
// agree: the service layer rejects an edge/node-update that would let a
// trigger with no target task (see TriggerConfig.TargetTaskID) reach a node
// this returns true for, and the worker's graph walk defensively refuses to
// visit such a node with a nil task rather than panic. Neither enforcement
// point currently walks from a Sprint trigger at all (validateTaskReachability
// only ever starts its BFS from predecessor_done/cron/api_trigger), so this
// function's KindCondition case staying unconditionally true doesn't block a
// legitimate Sprint-trigger → sprint-field-condition edge at creation time —
// only the worker's own runtime check needs the extra nil-task tolerance.
func NodeRequiresTask(kind Kind, nodeType string) bool {
	switch kind {
	case KindCondition:
		return true
	case KindAction:
		switch ActionType(nodeType) {
		case ActionCallAPI, ActionTriggerAIAgent, ActionWait, ActionUpdateSprint, ActionCompleteSprint:
			return false
		default:
			return true
		}
	default:
		return false
	}
}

// Automation is the aggregate root: a named automation graph within a project.
type Automation struct {
	ID          uuid.UUID
	ProjectID   uuid.UUID
	Name        string
	Description string
	Status      Status
	CreatedBy   *uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time
}

// Node is one Trigger/Condition/Action step in an automation's graph. Config
// is stored as raw JSON so each Kind/Type can carry its own shape — see
// TriggerConfig, ConditionConfig, ActionConfig — without a wide, mostly-null
// column set.
type Node struct {
	ID           uuid.UUID
	AutomationID uuid.UUID
	Kind         Kind
	Type         string
	Config       json.RawMessage
	PosX         float64
	PosY         float64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ElseHandle is the reserved branch handle a Condition node's fallback edge
// uses when none of its branches match.
const ElseHandle = "else"

// PluginConditionTrueHandle is the source handle a plugin-contributed
// condition node's "matched" edge uses. Unlike the built-in Condition node
// (an N-branch switch backed by ConditionConfig.Branches), a plugin
// condition is a simple boolean gate — EvaluateCondition returns
// Matched: true/false — so it only ever needs this one handle plus the
// shared ElseHandle fallback. Used by both the worker (walkPluginCondition
// in automation_consumer.go) and edge-handle validation (validateEdgeHandle
// in automation_service.go), which must agree on the same string.
const PluginConditionTrueHandle = "true"

// Edge is a directed link between two nodes in the same automation.
// SourceHandle is nil for Trigger/Action sources (a single outgoing path)
// and a branch handle (or ElseHandle) for a Condition source.
type Edge struct {
	ID           uuid.UUID
	AutomationID uuid.UUID
	SourceNodeID uuid.UUID
	SourceHandle *string
	TargetNodeID uuid.UUID
	CreatedAt    time.Time
}

// Graph bundles an automation with its full node/edge set, as returned by
// the single-fetch "get automation" read used to hydrate the canvas builder.
type Graph struct {
	Automation *Automation
	Nodes      []*Node
	Edges      []*Edge
}

// RunStatus is the lifecycle state of one graph execution.
type RunStatus string

// The three run lifecycle states.
const (
	RunStatusRunning   RunStatus = "running"
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
)

// Run is one execution of an automation's graph, started by a matched
// trigger node against one task. TaskID is nil when the trigger that
// started it has no target task configured (cron/api_trigger/
// predecessor_done with TargetTaskID unset) — only possible when the whole
// walk stays within call_api actions, the one node type that doesn't touch
// a task.
type Run struct {
	ID            uuid.UUID
	AutomationID  uuid.UUID
	TriggerNodeID uuid.UUID
	TaskID        *uuid.UUID
	Status        RunStatus
	StartedAt     time.Time
	FinishedAt    *time.Time
}

// RunStepStatus is the outcome of a single node visited during a Run.
type RunStepStatus string

// The three outcomes a visited node's run step can record.
const (
	RunStepCompleted RunStepStatus = "completed"
	RunStepFailed    RunStepStatus = "failed"
	RunStepSkipped   RunStepStatus = "skipped"
)

// RunStep is one node actually visited within a Run — the per-node execution
// trace the prior workflow/rule engines lacked.
type RunStep struct {
	ID             uuid.UUID
	RunID          uuid.UUID
	NodeID         uuid.UUID
	Status         RunStepStatus
	InputSnapshot  json.RawMessage
	OutputSnapshot json.RawMessage
	Error          string
	ExecutedAt     time.Time
}

// TriggerConfig carries every trigger-type-specific field. Only the fields
// relevant to a node's Type are set; the rest stay zero. Marshaled into
// Node.Config for kind=trigger nodes.
type TriggerConfig struct {
	// StatusID narrows TriggerStatusChanged to firing only when the task's
	// new status is this one. Nil means "any status change."
	StatusID *uuid.UUID `json:"status_id,omitempty"`
	// Tag narrows TriggerTagAdded to firing only when this specific tag is
	// the one that was added.
	Tag string `json:"tag,omitempty"`
	// DueDateOffsetMinutes configures TriggerDueDateReached: fire this many
	// minutes relative to the task's due date (0 = at the due date itself,
	// negative = that many minutes before, positive = after).
	DueDateOffsetMinutes *int `json:"due_date_offset_minutes,omitempty"`
	// WatchedTaskIDs configures TriggerPredecessorDone: the set of tasks
	// that must ALL currently be in a status with category "done" for this
	// trigger to fire. Re-checked fresh (not tracked as in-progress state)
	// every time a candidate event arrives for any one of them.
	WatchedTaskIDs []uuid.UUID `json:"watched_task_ids,omitempty"`
	// TargetTaskID configures TriggerPredecessorDone: the task the graph
	// walk runs against once every WatchedTaskIDs entry is done — mirrors
	// the old workflow canvas's "downstream node," a distinct task from its
	// predecessors, re-evaluated using its OWN current state rather than
	// any predecessor's.
	//
	// Also configures TriggerCron and TriggerAPITrigger: both fire on a
	// basis unrelated to any specific task event (a schedule, an external
	// POST), so — like TriggerPredecessorDone — they normally need an
	// explicit fixed task for the graph to run against.
	//
	// Optional: left nil, the trigger fires with no task at all. Since
	// every condition and every action except call_api needs a task to
	// evaluate/execute (see NodeRequiresTask), a trigger with no
	// TargetTaskID may only connect to call_api actions downstream —
	// enforced at edge-creation/node-update/activate time, not just here.
	TargetTaskID *uuid.UUID `json:"target_task_id,omitempty"`
	// CronExpression configures TriggerCron: a standard 5-field cron
	// expression (minute hour day-of-month month day-of-week), parsed via
	// robfig/cron's ParseStandard and evaluated in UTC.
	CronExpression string `json:"cron_expression,omitempty"`
	// SprintID narrows any of the four TriggerSprint* types to firing only
	// for this one sprint. Nil means "any sprint in the project" — the same
	// optional-narrowing shape as StatusID above.
	SprintID *uuid.UUID `json:"sprint_id,omitempty"`
}

// TaskTargetKind selects which task a condition leaf or action operates on,
// relative to the walk's own bound task — "self" (nil/empty also means
// self), the default, matches every automation's behavior before this type
// existed. The rest resolve relative to that task.
type TaskTargetKind string

// Recognized TaskTargetKind values.
const (
	TaskTargetSelf     TaskTargetKind = "self"
	TaskTargetParent   TaskTargetKind = "parent"
	TaskTargetChildren TaskTargetKind = "children"
	// TaskTargetBlocks and the three link kinds below it mirror task_link's
	// DisplayLinkType vocabulary exactly (see domain/task/entity.go's
	// TaskLink) — blocks and duplicates are directional (hence the
	// "is_x_by" inverse), relates_to is symmetric.
	TaskTargetBlocks         TaskTargetKind = "blocks"
	TaskTargetIsBlockedBy    TaskTargetKind = "is_blocked_by"
	TaskTargetRelatesTo      TaskTargetKind = "relates_to"
	TaskTargetDuplicates     TaskTargetKind = "duplicates"
	TaskTargetIsDuplicatedBy TaskTargetKind = "is_duplicated_by"
	// TaskTargetOther configures an explicit, otherwise-unrelated task,
	// chosen by ID — the same "pick a task" pattern as a trigger's
	// TargetTaskID.
	TaskTargetOther TaskTargetKind = "other"
)

// ValidTaskTargetKinds is the set of recognized TaskTargetKind values.
var ValidTaskTargetKinds = map[TaskTargetKind]bool{
	TaskTargetSelf:           true,
	TaskTargetParent:         true,
	TaskTargetChildren:       true,
	TaskTargetBlocks:         true,
	TaskTargetIsBlockedBy:    true,
	TaskTargetRelatesTo:      true,
	TaskTargetDuplicates:     true,
	TaskTargetIsDuplicatedBy: true,
	TaskTargetOther:          true,
}

// MultiValued reports whether Kind can resolve to more than one task —
// true for children and every link-based kind (a task can have several
// children, or block/relate to/duplicate several other tasks); false for
// self, parent (at most one), and other (a single explicit choice).
func (k TaskTargetKind) MultiValued() bool {
	switch k {
	case TaskTargetChildren, TaskTargetBlocks, TaskTargetIsBlockedBy, TaskTargetRelatesTo, TaskTargetDuplicates, TaskTargetIsDuplicatedBy:
		return true
	default:
		return false
	}
}

// TaskTarget lets a condition leaf or action node retarget itself onto a
// task other than the walk's own bound task. Nil (or a zero-value Kind)
// means "self" — the walk's task, unchanged from before this existed.
type TaskTarget struct {
	Kind TaskTargetKind `json:"kind,omitempty"`
	// OtherTaskID configures Kind == TaskTargetOther: the explicit task to
	// operate on, unrelated to the walk's own task. Unused otherwise.
	OtherTaskID *uuid.UUID `json:"other_task_id,omitempty"`
}

// ActionConfig carries every action-type-specific field. Only the fields
// relevant to a node's Type are set. Marshaled into Node.Config for
// kind=action nodes.
type ActionConfig struct {
	// Message is trigger_ai_agent's free-text instruction for the agent.
	Message string `json:"message,omitempty"`
	// MemberID is trigger_ai_agent's target agent — who runs the agent
	// conversation, unrelated to Update.AssigneeIDs (which reassigns the
	// task itself when Type is ActionUpdateTask).
	MemberID *uuid.UUID `json:"member_id,omitempty"`

	// Update carries every task-field change to apply when Type is
	// ActionUpdateTask — one node settable across any number of fields at
	// once (see TaskFieldUpdate), replacing the old one-node-per-field
	// actions (assign/set_status/set_priority/add_tag/set_custom_field).
	Update *TaskFieldUpdate `json:"update,omitempty"`

	// call_api: the outbound request to make when the walk reaches this
	// node. Headers/Body are stored and returned verbatim like every other
	// config field today (e.g. TaskFieldUpdate's CustomFields values) —
	// anyone with project read access to the automation can see them,
	// including any secret a user puts in an Authorization header. Not a
	// new gap, but worth noting since this is the first action whose
	// config plausibly holds a live secret.
	Method  string            `json:"method,omitempty"`  // call_api: GET/POST/PUT/PATCH/DELETE
	URL     string            `json:"url,omitempty"`     // call_api
	Headers map[string]string `json:"headers,omitempty"` // call_api
	Body    string            `json:"body,omitempty"`    // call_api: sent verbatim

	// Target retargets this action onto a task other than the walk's own
	// bound task (nil/self = today's behavior). Not supported by call_api,
	// which doesn't operate on a task at all. When Target.Kind.MultiValued()
	// resolves more than one task, the action fans out — applied once per
	// resolved task (see the worker's runAction).
	Target *TaskTarget `json:"target,omitempty"`

	// WaitMinutes is wait's pause duration — how long the graph walk stays
	// paused at this node (see the worker's PendingDelay/walkWait) before
	// continuing to its outgoing edges. Required, must be > 0 (validated at
	// strict/activate time by automationsvc).
	WaitMinutes *int `json:"wait_minutes,omitempty"`

	// SprintUpdate carries every sprint-field change to apply when Type is
	// ActionUpdateSprint — the sprint-scoped counterpart to Update, applied
	// to whichever sprint the walk resolves via the worker's resolveSprint.
	SprintUpdate *SprintFieldUpdate `json:"sprint_update,omitempty"`
	// MoveToSprintID is complete_sprint's optional destination for the
	// sprint's non-done tasks (nil = move to backlog) — mirrors
	// sprintdom.CompleteSprintInput.MoveToSprintID.
	MoveToSprintID *uuid.UUID `json:"move_to_sprint_id,omitempty"`
}

// SprintFieldUpdate is the sparse "which fields to set" payload for
// ActionUpdateSprint — same field set sprintdom.UpdateSprintInput exposes,
// with TaskFieldUpdate's simpler nil-means-untouched convention rather than
// UpdateSprintInput's explicit-clear **T plumbing (no built-in action node
// needs to explicitly clear a sprint field to nil today).
type SprintFieldUpdate struct {
	Name      string                  `json:"name,omitempty"`
	StartDate *time.Time              `json:"start_date,omitempty"`
	EndDate   *time.Time              `json:"end_date,omitempty"`
	Goal      *string                 `json:"goal,omitempty"`
	Status    *sprintdom.SprintStatus `json:"status,omitempty"`
}

// TaskFieldUpdate is the sparse "which fields to set" payload for
// ActionUpdateTask — same field set as taskdom's UpdateTaskInput (see
// services/api/internal/domain/task/service.go), minus its **T
// explicit-clear plumbing: a nil field here means "leave this field
// alone," matching how every one of the old single-field actions already
// worked (set_status could set a status but never clear one, etc.) — this
// merge doesn't add or remove that capability, just lets one action node
// touch several fields instead of exactly one.
type TaskFieldUpdate struct {
	TaskTypeID   *uuid.UUID      `json:"task_type_id,omitempty"`
	StatusID     *uuid.UUID      `json:"status_id,omitempty"`
	SprintID     *uuid.UUID      `json:"sprint_id,omitempty"`
	ParentTaskID *uuid.UUID      `json:"parent_task_id,omitempty"`
	Title        string          `json:"title,omitempty"`
	Description  json.RawMessage `json:"description,omitempty"`
	Importance   *int            `json:"importance,omitempty"`
	StoryPoints  *int            `json:"story_points,omitempty"`
	AssigneeIDs  []uuid.UUID     `json:"assignee_ids,omitempty"`
	ReporterID   *uuid.UUID      `json:"reporter_id,omitempty"`
	CustomFields map[string]any  `json:"custom_fields,omitempty"`
	StartDate    *time.Time      `json:"start_date,omitempty"`
	DueDate      *time.Time      `json:"due_date,omitempty"`
	Tags         []string        `json:"tags,omitempty"`
}

// ConditionConfig holds a Condition node's ordered branches. Marshaled into
// Node.Config for kind=condition nodes (Type is always ConditionNodeType).
type ConditionConfig struct {
	// Branches are evaluated in order; the first whose Tree.Evaluate(task)
	// is true is followed, via the edge whose SourceHandle equals that
	// branch's Handle. A 2-branch Condition is exactly today's binary IF.
	Branches []ConditionBranch `json:"branches"`
}

// ConditionBranch is one arm of a Condition node's switch.
type ConditionBranch struct {
	// Handle matches an Edge.SourceHandle — the wire this branch follows.
	Handle string `json:"handle"`
	// Label is a human-readable name for the branch, shown in the canvas
	// (e.g. "type = Bug"). Purely cosmetic.
	Label string         `json:"label,omitempty"`
	Tree  *ConditionLeaf `json:"tree"`
}

// CronCandidate pairs a TriggerCron node with the last time it fired (nil if
// it has never fired), for the cron scheduler's per-tick "is it due" check.
type CronCandidate struct {
	Node        *Node
	LastFiredAt *time.Time
}

// WalkContext is the set of walk-scoped identifiers — task, sprint, and any
// future context type a trigger might carry — that a paused node needs in
// order to resume correctly. Stored as a single JSON blob on
// PendingAgentWait/PendingDelay (see below) rather than one column per
// field, so adding a new context type is a new field here plus a version
// bump, never a migration.
type WalkContext struct {
	TaskID   *uuid.UUID `json:"task_id,omitempty"`
	SprintID *uuid.UUID `json:"sprint_id,omitempty"`
}

// PendingAgentWait is one in-flight "graph walk is paused here" marker,
// created when a trigger_ai_agent action node starts an agent conversation
// and deleted (via Repository.ClaimPendingAgentWait) once that conversation
// reaches a terminal status — see worker.AutomationConsumer.walkAction and
// handleAgentConversationStatus. AutomationID is stored directly (not
// derived by joining through NodeID) so resuming the walk can LoadGraph
// without an extra lookup. Context mirrors the walker's own task/sprint
// fields at the point the walk paused, so resumeWalkFrom can reconstruct
// them fresh on resume.
type PendingAgentWait struct {
	ID             uuid.UUID
	RunID          uuid.UUID
	NodeID         uuid.UUID
	AutomationID   uuid.UUID
	ConversationID uuid.UUID
	ProjectID      uuid.UUID
	Context        WalkContext
	CreatedAt      time.Time
}

// PendingDelay is one in-flight "graph walk is paused here" marker, created
// when a wait action node is visited and deleted (via
// Repository.ClaimDueDelays) once ResumeAt has passed — see the worker's
// walkWait and WaitScheduler. Same shape as PendingAgentWait, with ResumeAt
// standing in for a conversation ID as the thing being waited on.
type PendingDelay struct {
	ID           uuid.UUID
	RunID        uuid.UUID
	NodeID       uuid.UUID
	AutomationID uuid.UUID
	ProjectID    uuid.UUID
	Context      WalkContext
	ResumeAt     time.Time
	CreatedAt    time.Time
}

// WebhookToken is the secret credential an external caller presents to fire
// a TriggerAPITrigger node. Only TokenPrefix is ever exposed back to the
// project; the raw token is shown once at generation time and never
// persisted — only its SHA-256 hash is stored, checked in-handler.
// At most one non-revoked token exists per node at a time: generating a new
// one atomically revokes whichever one preceded it.
type WebhookToken struct {
	ID           uuid.UUID
	NodeID       uuid.UUID
	AutomationID uuid.UUID
	TokenPrefix  string
	CreatedBy    *uuid.UUID
	CreatedAt    time.Time
	LastUsedAt   *time.Time
	RevokedAt    *time.Time
}
