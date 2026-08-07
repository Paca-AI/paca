package automationdom

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

// Service defines automation-graph use cases. All methods that take a
// projectID verify the automation (or its node/edge ancestor) actually
// belongs to that project before mutating anything.
type Service interface {
	ListAutomations(ctx context.Context, projectID uuid.UUID, status *Status) ([]*Automation, error)
	GetAutomation(ctx context.Context, projectID, automationID uuid.UUID) (*Graph, error)
	CreateAutomation(ctx context.Context, in CreateAutomationInput) (*Automation, error)
	UpdateAutomation(ctx context.Context, projectID, automationID uuid.UUID, in UpdateAutomationInput) (*Automation, error)
	DeleteAutomation(ctx context.Context, projectID, automationID uuid.UUID) error

	Activate(ctx context.Context, projectID, automationID uuid.UUID) (*Automation, error)
	Deactivate(ctx context.Context, projectID, automationID uuid.UUID) (*Automation, error)

	AddNode(ctx context.Context, projectID, automationID uuid.UUID, in AddNodeInput) (*Node, error)
	UpdateNode(ctx context.Context, projectID, automationID, nodeID uuid.UUID, in UpdateNodeInput) (*Node, error)
	RemoveNode(ctx context.Context, projectID, automationID, nodeID uuid.UUID) error

	AddEdge(ctx context.Context, projectID, automationID uuid.UUID, in AddEdgeInput) (*Edge, error)
	RemoveEdge(ctx context.Context, projectID, automationID, edgeID uuid.UUID) error

	// ListRuns returns the most recent runs for an automation, newest first.
	ListRuns(ctx context.Context, projectID, automationID uuid.UUID, limit int) ([]*Run, error)
	// ListRunSteps returns the per-node execution trace for one run.
	ListRunSteps(ctx context.Context, projectID, automationID, runID uuid.UUID) ([]*RunStep, error)

	// DependencyMap returns a read-only, auto-generated view of every
	// active automation's predecessor_done trigger configs in a project —
	// recovering the old workflow canvas's bird's-eye view of task
	// dependencies without a second editable canvas.
	DependencyMap(ctx context.Context, projectID uuid.UUID) ([]DependencyMapEntry, error)

	// GenerateWebhookToken (re)generates the secret token for an api_trigger
	// node, revoking whichever token preceded it. The raw token is returned
	// only here — it's never persisted or retrievable again afterward.
	GenerateWebhookToken(ctx context.Context, projectID, automationID, nodeID uuid.UUID, actor WebhookTokenActor) (*WebhookToken, string, error)
	// HandleWebhookTrigger verifies rawToken against nodeID's active webhook
	// token and, if valid, dispatches a graph-walk run against the node's
	// configured target task. Every failure path (unknown node, wrong
	// type, inactive automation, wrong/missing token) returns
	// ErrWebhookTokenInvalid or ErrNotFound so an unauthenticated caller
	// can't distinguish "no such webhook" from "wrong token".
	HandleWebhookTrigger(ctx context.Context, nodeID uuid.UUID, rawToken string) error
}

// WebhookTokenActor identifies who requested a webhook token generation, for
// WebhookToken.CreatedBy — mirrors CreateAutomationInput's CreatedBy/AgentID
// split.
type WebhookTokenActor struct {
	UserID  *uuid.UUID
	AgentID *uuid.UUID
}

// CreateAutomationInput carries the fields required to create a draft automation.
type CreateAutomationInput struct {
	ProjectID   uuid.UUID
	Name        string
	Description string
	// CreatedBy is the authenticated actor's user UUID (not a
	// project_members.id) — the service resolves it to the actor's
	// project_members.id for this project before persisting.
	CreatedBy *uuid.UUID
	// AgentID is set when the request was authenticated as an AI agent (via
	// an agent API key), so CreatedBy resolves to the agent's own
	// project_members row instead of being looked up as a human user.
	AgentID *uuid.UUID
}

// UpdateAutomationInput carries the mutable metadata fields of an automation.
type UpdateAutomationInput struct {
	Name        *string
	Description *string
}

// AddNodeInput carries the fields required to add a node to the graph.
type AddNodeInput struct {
	Kind   Kind
	Type   string
	Config json.RawMessage
	PosX   float64
	PosY   float64
}

// UpdateNodeInput carries the mutable fields of a node.
type UpdateNodeInput struct {
	Config *json.RawMessage
	PosX   *float64
	PosY   *float64
}

// AddEdgeInput carries the fields required to link two nodes.
type AddEdgeInput struct {
	SourceNodeID uuid.UUID
	SourceHandle *string
	TargetNodeID uuid.UUID
}

// PluginNodeResolver checks whether a node type string is one a loaded
// plugin has registered, for each kind — consulted by the service's node
// validation as a fallback after the built-in type lists, and by nothing
// else in this package (the actual dispatch/execution call happens in the
// worker, which talks to the plugin runtime directly).
type PluginNodeResolver interface {
	IsPluginTrigger(nodeType string) bool
	IsPluginCondition(nodeType string) bool
	IsPluginAction(nodeType string) bool
}

// DependencyMapEntry is one predecessor-done relationship surfaced by
// DependencyMap: automation/node identify where the wait lives, TargetTaskID
// is the downstream task whose automation is waiting, WatchedTaskIDs are the
// upstream tasks it's waiting on.
type DependencyMapEntry struct {
	AutomationID   uuid.UUID
	AutomationName string
	NodeID         uuid.UUID
	TargetTaskID   uuid.UUID
	WatchedTaskIDs []uuid.UUID
}
