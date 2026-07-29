package dto

import (
	"encoding/json"
	"time"

	automationdom "github.com/Paca-AI/api/internal/domain/automation"
	"github.com/google/uuid"
)

// --- Requests ------------------------------------------------------------------

type CreateAutomationRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type UpdateAutomationRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

type AddAutomationNodeRequest struct {
	Kind   string          `json:"kind"`
	Type   string          `json:"type"`
	Config json.RawMessage `json:"config"`
	PosX   float64         `json:"pos_x"`
	PosY   float64         `json:"pos_y"`
}

type UpdateAutomationNodeRequest struct {
	Config *json.RawMessage `json:"config"`
	PosX   *float64         `json:"pos_x"`
	PosY   *float64         `json:"pos_y"`
}

type AddAutomationEdgeRequest struct {
	SourceNodeID uuid.UUID `json:"source_node_id"`
	SourceHandle *string   `json:"source_handle"`
	TargetNodeID uuid.UUID `json:"target_node_id"`
}

// --- Responses -------------------------------------------------------------

type AutomationResponse struct {
	ID          uuid.UUID  `json:"id"`
	ProjectID   uuid.UUID  `json:"project_id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	CreatedBy   *uuid.UUID `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func NewAutomationResponse(a *automationdom.Automation) AutomationResponse {
	return AutomationResponse{
		ID:          a.ID,
		ProjectID:   a.ProjectID,
		Name:        a.Name,
		Description: a.Description,
		Status:      string(a.Status),
		CreatedBy:   a.CreatedBy,
		CreatedAt:   a.CreatedAt,
		UpdatedAt:   a.UpdatedAt,
	}
}

type AutomationNodeResponse struct {
	ID           uuid.UUID       `json:"id"`
	AutomationID uuid.UUID       `json:"automation_id"`
	Kind         string          `json:"kind"`
	Type         string          `json:"type"`
	Config       json.RawMessage `json:"config"`
	PosX         float64         `json:"pos_x"`
	PosY         float64         `json:"pos_y"`
}

func NewAutomationNodeResponse(n *automationdom.Node) AutomationNodeResponse {
	return AutomationNodeResponse{
		ID:           n.ID,
		AutomationID: n.AutomationID,
		Kind:         string(n.Kind),
		Type:         n.Type,
		Config:       n.Config,
		PosX:         n.PosX,
		PosY:         n.PosY,
	}
}

type AutomationEdgeResponse struct {
	ID           uuid.UUID `json:"id"`
	AutomationID uuid.UUID `json:"automation_id"`
	SourceNodeID uuid.UUID `json:"source_node_id"`
	SourceHandle *string   `json:"source_handle"`
	TargetNodeID uuid.UUID `json:"target_node_id"`
}

func NewAutomationEdgeResponse(e *automationdom.Edge) AutomationEdgeResponse {
	return AutomationEdgeResponse{
		ID:           e.ID,
		AutomationID: e.AutomationID,
		SourceNodeID: e.SourceNodeID,
		SourceHandle: e.SourceHandle,
		TargetNodeID: e.TargetNodeID,
	}
}

type AutomationGraphResponse struct {
	Automation AutomationResponse       `json:"automation"`
	Nodes      []AutomationNodeResponse `json:"nodes"`
	Edges      []AutomationEdgeResponse `json:"edges"`
}

func NewAutomationGraphResponse(g *automationdom.Graph) AutomationGraphResponse {
	nodes := make([]AutomationNodeResponse, len(g.Nodes))
	for i, n := range g.Nodes {
		nodes[i] = NewAutomationNodeResponse(n)
	}
	edges := make([]AutomationEdgeResponse, len(g.Edges))
	for i, e := range g.Edges {
		edges[i] = NewAutomationEdgeResponse(e)
	}
	return AutomationGraphResponse{
		Automation: NewAutomationResponse(g.Automation),
		Nodes:      nodes,
		Edges:      edges,
	}
}

type AutomationRunResponse struct {
	ID            uuid.UUID `json:"id"`
	AutomationID  uuid.UUID `json:"automation_id"`
	TriggerNodeID uuid.UUID `json:"trigger_node_id"`
	// TaskID is nil when the trigger that started this run had no target
	// task configured (only possible when the whole walk stayed within
	// call_api actions).
	TaskID     *uuid.UUID `json:"task_id"`
	Status     string     `json:"status"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

func NewAutomationRunResponse(r *automationdom.Run) AutomationRunResponse {
	return AutomationRunResponse{
		ID:            r.ID,
		AutomationID:  r.AutomationID,
		TriggerNodeID: r.TriggerNodeID,
		TaskID:        r.TaskID,
		Status:        string(r.Status),
		StartedAt:     r.StartedAt,
		FinishedAt:    r.FinishedAt,
	}
}

type AutomationRunStepResponse struct {
	ID             uuid.UUID       `json:"id"`
	RunID          uuid.UUID       `json:"run_id"`
	NodeID         uuid.UUID       `json:"node_id"`
	Status         string          `json:"status"`
	InputSnapshot  json.RawMessage `json:"input_snapshot"`
	OutputSnapshot json.RawMessage `json:"output_snapshot"`
	Error          string          `json:"error,omitempty"`
	ExecutedAt     time.Time       `json:"executed_at"`
}

func NewAutomationRunStepResponse(s *automationdom.RunStep) AutomationRunStepResponse {
	return AutomationRunStepResponse{
		ID:             s.ID,
		RunID:          s.RunID,
		NodeID:         s.NodeID,
		Status:         string(s.Status),
		InputSnapshot:  s.InputSnapshot,
		OutputSnapshot: s.OutputSnapshot,
		Error:          s.Error,
		ExecutedAt:     s.ExecutedAt,
	}
}

type DependencyMapEntryResponse struct {
	AutomationID   uuid.UUID   `json:"automation_id"`
	AutomationName string      `json:"automation_name"`
	NodeID         uuid.UUID   `json:"node_id"`
	TargetTaskID   uuid.UUID   `json:"target_task_id"`
	WatchedTaskIDs []uuid.UUID `json:"watched_task_ids"`
}

func NewDependencyMapEntryResponse(e automationdom.DependencyMapEntry) DependencyMapEntryResponse {
	return DependencyMapEntryResponse{
		AutomationID:   e.AutomationID,
		AutomationName: e.AutomationName,
		NodeID:         e.NodeID,
		TargetTaskID:   e.TargetTaskID,
		WatchedTaskIDs: e.WatchedTaskIDs,
	}
}

// WebhookTokenResponse is the public representation of an api_trigger
// node's webhook token. The raw token value is NEVER included here; it is
// returned only in CreateWebhookTokenResponse on first generation.
type WebhookTokenResponse struct {
	ID          uuid.UUID  `json:"id"`
	TokenPrefix string     `json:"token_prefix"`
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
}

// CreateWebhookTokenResponse is returned from
// POST /projects/:projectId/automations/:automationId/nodes/:nodeId/webhook-token.
// Token contains the full raw token — shown ONCE, never retrievable again.
type CreateWebhookTokenResponse struct {
	WebhookTokenResponse
	Token string `json:"token"`
}

func NewWebhookTokenResponse(t *automationdom.WebhookToken) WebhookTokenResponse {
	return WebhookTokenResponse{
		ID:          t.ID,
		TokenPrefix: t.TokenPrefix,
		CreatedAt:   t.CreatedAt,
		LastUsedAt:  t.LastUsedAt,
	}
}
