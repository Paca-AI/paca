package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	automationdom "github.com/Paca-AI/api/internal/domain/automation"
)

// --- sqlx models -------------------------------------------------------------

type automationRecord struct {
	ID          string     `db:"id"`
	ProjectID   string     `db:"project_id"`
	Name        string     `db:"name"`
	Description *string    `db:"description"`
	Status      string     `db:"status"`
	CreatedBy   *string    `db:"created_by"`
	CreatedAt   time.Time  `db:"created_at"`
	UpdatedAt   time.Time  `db:"updated_at"`
	DeletedAt   *time.Time `db:"deleted_at"`
}

func (rec *automationRecord) toDomain() (*automationdom.Automation, error) {
	id, err := uuid.Parse(rec.ID)
	if err != nil {
		return nil, err
	}
	projectID, err := uuid.Parse(rec.ProjectID)
	if err != nil {
		return nil, err
	}
	a := &automationdom.Automation{
		ID:        id,
		ProjectID: projectID,
		Name:      rec.Name,
		Status:    automationdom.Status(rec.Status),
		CreatedAt: rec.CreatedAt,
		UpdatedAt: rec.UpdatedAt,
		DeletedAt: rec.DeletedAt,
	}
	if rec.Description != nil {
		a.Description = *rec.Description
	}
	if rec.CreatedBy != nil {
		createdBy, err := uuid.Parse(*rec.CreatedBy)
		if err != nil {
			return nil, err
		}
		a.CreatedBy = &createdBy
	}
	return a, nil
}

type nodeRecord struct {
	ID           string    `db:"id"`
	AutomationID string    `db:"automation_id"`
	Kind         string    `db:"kind"`
	Type         string    `db:"type"`
	Config       []byte    `db:"config"`
	PosX         float64   `db:"pos_x"`
	PosY         float64   `db:"pos_y"`
	CreatedAt    time.Time `db:"created_at"`
	UpdatedAt    time.Time `db:"updated_at"`
}

func (rec *nodeRecord) toDomain() (*automationdom.Node, error) {
	id, err := uuid.Parse(rec.ID)
	if err != nil {
		return nil, err
	}
	automationID, err := uuid.Parse(rec.AutomationID)
	if err != nil {
		return nil, err
	}
	return &automationdom.Node{
		ID:           id,
		AutomationID: automationID,
		Kind:         automationdom.Kind(rec.Kind),
		Type:         rec.Type,
		Config:       rec.Config,
		PosX:         rec.PosX,
		PosY:         rec.PosY,
		CreatedAt:    rec.CreatedAt,
		UpdatedAt:    rec.UpdatedAt,
	}, nil
}

type webhookTokenRecord struct {
	ID           string     `db:"id"`
	NodeID       string     `db:"node_id"`
	AutomationID string     `db:"automation_id"`
	TokenPrefix  string     `db:"token_prefix"`
	CreatedBy    *string    `db:"created_by"`
	CreatedAt    time.Time  `db:"created_at"`
	LastUsedAt   *time.Time `db:"last_used_at"`
	RevokedAt    *time.Time `db:"revoked_at"`
}

func (rec *webhookTokenRecord) toDomain() (*automationdom.WebhookToken, error) {
	id, err := uuid.Parse(rec.ID)
	if err != nil {
		return nil, err
	}
	nodeID, err := uuid.Parse(rec.NodeID)
	if err != nil {
		return nil, err
	}
	automationID, err := uuid.Parse(rec.AutomationID)
	if err != nil {
		return nil, err
	}
	tok := &automationdom.WebhookToken{
		ID:           id,
		NodeID:       nodeID,
		AutomationID: automationID,
		TokenPrefix:  rec.TokenPrefix,
		CreatedAt:    rec.CreatedAt,
		LastUsedAt:   rec.LastUsedAt,
		RevokedAt:    rec.RevokedAt,
	}
	if rec.CreatedBy != nil {
		createdBy, err := uuid.Parse(*rec.CreatedBy)
		if err != nil {
			return nil, err
		}
		tok.CreatedBy = &createdBy
	}
	return tok, nil
}

type edgeRecord struct {
	ID           string    `db:"id"`
	AutomationID string    `db:"automation_id"`
	SourceNodeID string    `db:"source_node_id"`
	SourceHandle *string   `db:"source_handle"`
	TargetNodeID string    `db:"target_node_id"`
	CreatedAt    time.Time `db:"created_at"`
}

func (rec *edgeRecord) toDomain() (*automationdom.Edge, error) {
	id, err := uuid.Parse(rec.ID)
	if err != nil {
		return nil, err
	}
	automationID, err := uuid.Parse(rec.AutomationID)
	if err != nil {
		return nil, err
	}
	sourceID, err := uuid.Parse(rec.SourceNodeID)
	if err != nil {
		return nil, err
	}
	targetID, err := uuid.Parse(rec.TargetNodeID)
	if err != nil {
		return nil, err
	}
	return &automationdom.Edge{
		ID:           id,
		AutomationID: automationID,
		SourceNodeID: sourceID,
		SourceHandle: rec.SourceHandle,
		TargetNodeID: targetID,
		CreatedAt:    rec.CreatedAt,
	}, nil
}

type runRecord struct {
	ID            string     `db:"id"`
	AutomationID  string     `db:"automation_id"`
	TriggerNodeID string     `db:"trigger_node_id"`
	TaskID        *string    `db:"task_id"`
	Status        string     `db:"status"`
	StartedAt     time.Time  `db:"started_at"`
	FinishedAt    *time.Time `db:"finished_at"`
}

func (rec *runRecord) toDomain() (*automationdom.Run, error) {
	id, err := uuid.Parse(rec.ID)
	if err != nil {
		return nil, err
	}
	automationID, err := uuid.Parse(rec.AutomationID)
	if err != nil {
		return nil, err
	}
	triggerNodeID, err := uuid.Parse(rec.TriggerNodeID)
	if err != nil {
		return nil, err
	}
	var taskID *uuid.UUID
	if rec.TaskID != nil {
		parsed, err := uuid.Parse(*rec.TaskID)
		if err != nil {
			return nil, err
		}
		taskID = &parsed
	}
	return &automationdom.Run{
		ID:            id,
		AutomationID:  automationID,
		TriggerNodeID: triggerNodeID,
		TaskID:        taskID,
		Status:        automationdom.RunStatus(rec.Status),
		StartedAt:     rec.StartedAt,
		FinishedAt:    rec.FinishedAt,
	}, nil
}

type runStepRecord struct {
	ID             string    `db:"id"`
	RunID          string    `db:"run_id"`
	NodeID         string    `db:"node_id"`
	Status         string    `db:"status"`
	InputSnapshot  []byte    `db:"input_snapshot"`
	OutputSnapshot []byte    `db:"output_snapshot"`
	Error          *string   `db:"error"`
	ExecutedAt     time.Time `db:"executed_at"`
}

func (rec *runStepRecord) toDomain() (*automationdom.RunStep, error) {
	id, err := uuid.Parse(rec.ID)
	if err != nil {
		return nil, err
	}
	runID, err := uuid.Parse(rec.RunID)
	if err != nil {
		return nil, err
	}
	nodeID, err := uuid.Parse(rec.NodeID)
	if err != nil {
		return nil, err
	}
	s := &automationdom.RunStep{
		ID:             id,
		RunID:          runID,
		NodeID:         nodeID,
		Status:         automationdom.RunStepStatus(rec.Status),
		InputSnapshot:  rec.InputSnapshot,
		OutputSnapshot: rec.OutputSnapshot,
		ExecutedAt:     rec.ExecutedAt,
	}
	if rec.Error != nil {
		s.Error = *rec.Error
	}
	return s, nil
}

// decodeWalkContext parses a pending-wait row's context JSONB column. An
// empty/null column (rows written before the context column existed, or a
// context with no fields set) decodes to a zero-value WalkContext rather
// than erroring.
func decodeWalkContext(raw []byte) (automationdom.WalkContext, error) {
	var ctx automationdom.WalkContext
	if len(raw) == 0 {
		return ctx, nil
	}
	if err := json.Unmarshal(raw, &ctx); err != nil {
		return ctx, fmt.Errorf("decode walk context: %w", err)
	}
	return ctx, nil
}

type pendingAgentWaitRecord struct {
	ID             string    `db:"id"`
	RunID          string    `db:"run_id"`
	NodeID         string    `db:"node_id"`
	AutomationID   string    `db:"automation_id"`
	ConversationID string    `db:"conversation_id"`
	ProjectID      string    `db:"project_id"`
	Context        []byte    `db:"context"`
	CreatedAt      time.Time `db:"created_at"`
}

func (rec *pendingAgentWaitRecord) toDomain() (*automationdom.PendingAgentWait, error) {
	id, err := uuid.Parse(rec.ID)
	if err != nil {
		return nil, err
	}
	runID, err := uuid.Parse(rec.RunID)
	if err != nil {
		return nil, err
	}
	nodeID, err := uuid.Parse(rec.NodeID)
	if err != nil {
		return nil, err
	}
	automationID, err := uuid.Parse(rec.AutomationID)
	if err != nil {
		return nil, err
	}
	conversationID, err := uuid.Parse(rec.ConversationID)
	if err != nil {
		return nil, err
	}
	projectID, err := uuid.Parse(rec.ProjectID)
	if err != nil {
		return nil, err
	}
	walkCtx, err := decodeWalkContext(rec.Context)
	if err != nil {
		return nil, err
	}
	return &automationdom.PendingAgentWait{
		ID:             id,
		RunID:          runID,
		NodeID:         nodeID,
		AutomationID:   automationID,
		ConversationID: conversationID,
		ProjectID:      projectID,
		Context:        walkCtx,
		CreatedAt:      rec.CreatedAt,
	}, nil
}

type pendingDelayRecord struct {
	ID           string    `db:"id"`
	RunID        string    `db:"run_id"`
	NodeID       string    `db:"node_id"`
	AutomationID string    `db:"automation_id"`
	ProjectID    string    `db:"project_id"`
	Context      []byte    `db:"context"`
	ResumeAt     time.Time `db:"resume_at"`
	CreatedAt    time.Time `db:"created_at"`
}

func (rec *pendingDelayRecord) toDomain() (*automationdom.PendingDelay, error) {
	id, err := uuid.Parse(rec.ID)
	if err != nil {
		return nil, err
	}
	runID, err := uuid.Parse(rec.RunID)
	if err != nil {
		return nil, err
	}
	nodeID, err := uuid.Parse(rec.NodeID)
	if err != nil {
		return nil, err
	}
	automationID, err := uuid.Parse(rec.AutomationID)
	if err != nil {
		return nil, err
	}
	projectID, err := uuid.Parse(rec.ProjectID)
	if err != nil {
		return nil, err
	}
	walkCtx, err := decodeWalkContext(rec.Context)
	if err != nil {
		return nil, err
	}
	return &automationdom.PendingDelay{
		ID:           id,
		RunID:        runID,
		NodeID:       nodeID,
		AutomationID: automationID,
		ProjectID:    projectID,
		Context:      walkCtx,
		ResumeAt:     rec.ResumeAt,
		CreatedAt:    rec.CreatedAt,
	}, nil
}

// AutomationRepository is the sqlx implementation of automationdom.Repository.
type AutomationRepository struct {
	db *sqlx.DB
}

// NewAutomationRepository returns a new AutomationRepository.
func NewAutomationRepository(db *sqlx.DB) *AutomationRepository {
	return &AutomationRepository{db: db}
}

// --- Automation ---------------------------------------------------------------

// CreateAutomation implements automationdom.Repository.CreateAutomation.
func (r *AutomationRepository) CreateAutomation(ctx context.Context, a *automationdom.Automation) error {
	var createdBy *string
	if a.CreatedBy != nil {
		s := a.CreatedBy.String()
		createdBy = &s
	}
	var description *string
	if a.Description != "" {
		description = &a.Description
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO automations (id, project_id, name, description, status, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		a.ID.String(), a.ProjectID.String(), a.Name, description, string(a.Status), createdBy, a.CreatedAt, a.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("automation repo: create: %w", err)
	}
	return nil
}

// FindAutomationByID implements automationdom.Repository.FindAutomationByID.
func (r *AutomationRepository) FindAutomationByID(ctx context.Context, id uuid.UUID) (*automationdom.Automation, error) {
	const q = `
		SELECT id, project_id, name, description, status, created_by, created_at, updated_at, deleted_at
		FROM automations WHERE id = $1 AND deleted_at IS NULL`
	var rec automationRecord
	if err := r.db.GetContext(ctx, &rec, q, id.String()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, automationdom.ErrNotFound
		}
		return nil, err
	}
	return rec.toDomain()
}

// ListAutomations implements automationdom.Repository.ListAutomations.
func (r *AutomationRepository) ListAutomations(ctx context.Context, projectID uuid.UUID, status *automationdom.Status) ([]*automationdom.Automation, error) {
	q := `
		SELECT id, project_id, name, description, status, created_by, created_at, updated_at, deleted_at
		FROM automations WHERE project_id = $1 AND deleted_at IS NULL`
	args := []interface{}{projectID.String()}
	if status != nil {
		q += ` AND status = $2`
		args = append(args, string(*status))
	}
	q += ` ORDER BY created_at DESC`

	var recs []automationRecord
	if err := r.db.SelectContext(ctx, &recs, q, args...); err != nil {
		return nil, err
	}
	out := make([]*automationdom.Automation, 0, len(recs))
	for i := range recs {
		a, err := recs[i].toDomain()
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// UpdateAutomation implements automationdom.Repository.UpdateAutomation.
func (r *AutomationRepository) UpdateAutomation(ctx context.Context, a *automationdom.Automation) error {
	var description *string
	if a.Description != "" {
		description = &a.Description
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE automations SET name = $1, description = $2, status = $3, updated_at = $4
		WHERE id = $5 AND deleted_at IS NULL`,
		a.Name, description, string(a.Status), a.UpdatedAt, a.ID.String(),
	)
	return err
}

// DeleteAutomation implements automationdom.Repository.DeleteAutomation.
func (r *AutomationRepository) DeleteAutomation(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `UPDATE automations SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL`, id.String())
	return err
}

// --- Graph ----------------------------------------------------------------

// LoadGraph implements automationdom.Repository.LoadGraph.
func (r *AutomationRepository) LoadGraph(ctx context.Context, automationID uuid.UUID) (*automationdom.Graph, error) {
	nodes, err := r.ListNodesByAutomation(ctx, automationID)
	if err != nil {
		return nil, err
	}
	edges, err := r.ListEdgesByAutomation(ctx, automationID)
	if err != nil {
		return nil, err
	}
	return &automationdom.Graph{Nodes: nodes, Edges: edges}, nil
}

// --- Nodes ------------------------------------------------------------------

// CreateNode implements automationdom.Repository.CreateNode.
func (r *AutomationRepository) CreateNode(ctx context.Context, n *automationdom.Node) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO automation_nodes (id, automation_id, kind, type, config, pos_x, pos_y, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		n.ID.String(), n.AutomationID.String(), string(n.Kind), n.Type, []byte(n.Config), n.PosX, n.PosY, n.CreatedAt, n.UpdatedAt,
	)
	return err
}

// FindNodeByID implements automationdom.Repository.FindNodeByID.
func (r *AutomationRepository) FindNodeByID(ctx context.Context, id uuid.UUID) (*automationdom.Node, error) {
	const q = `
		SELECT id, automation_id, kind, type, config, pos_x, pos_y, created_at, updated_at
		FROM automation_nodes WHERE id = $1`
	var rec nodeRecord
	if err := r.db.GetContext(ctx, &rec, q, id.String()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, automationdom.ErrNodeNotFound
		}
		return nil, err
	}
	return rec.toDomain()
}

// ListNodesByAutomation implements automationdom.Repository.ListNodesByAutomation.
func (r *AutomationRepository) ListNodesByAutomation(ctx context.Context, automationID uuid.UUID) ([]*automationdom.Node, error) {
	const q = `
		SELECT id, automation_id, kind, type, config, pos_x, pos_y, created_at, updated_at
		FROM automation_nodes WHERE automation_id = $1 ORDER BY created_at ASC`
	var recs []nodeRecord
	if err := r.db.SelectContext(ctx, &recs, q, automationID.String()); err != nil {
		return nil, err
	}
	out := make([]*automationdom.Node, 0, len(recs))
	for i := range recs {
		n, err := recs[i].toDomain()
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// UpdateNode implements automationdom.Repository.UpdateNode.
func (r *AutomationRepository) UpdateNode(ctx context.Context, n *automationdom.Node) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE automation_nodes SET config = $1, pos_x = $2, pos_y = $3, updated_at = $4 WHERE id = $5`,
		[]byte(n.Config), n.PosX, n.PosY, n.UpdatedAt, n.ID.String(),
	)
	return err
}

// DeleteNode implements automationdom.Repository.DeleteNode.
func (r *AutomationRepository) DeleteNode(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM automation_nodes WHERE id = $1`, id.String())
	return err
}

// --- Edges ------------------------------------------------------------------

// CreateEdge implements automationdom.Repository.CreateEdge.
func (r *AutomationRepository) CreateEdge(ctx context.Context, e *automationdom.Edge) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO automation_edges (id, automation_id, source_node_id, source_handle, target_node_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		e.ID.String(), e.AutomationID.String(), e.SourceNodeID.String(), e.SourceHandle, e.TargetNodeID.String(), e.CreatedAt,
	)
	return err
}

// FindEdgeByID implements automationdom.Repository.FindEdgeByID.
func (r *AutomationRepository) FindEdgeByID(ctx context.Context, id uuid.UUID) (*automationdom.Edge, error) {
	const q = `
		SELECT id, automation_id, source_node_id, source_handle, target_node_id, created_at
		FROM automation_edges WHERE id = $1`
	var rec edgeRecord
	if err := r.db.GetContext(ctx, &rec, q, id.String()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, automationdom.ErrEdgeNotFound
		}
		return nil, err
	}
	return rec.toDomain()
}

// ListEdgesByAutomation implements automationdom.Repository.ListEdgesByAutomation.
func (r *AutomationRepository) ListEdgesByAutomation(ctx context.Context, automationID uuid.UUID) ([]*automationdom.Edge, error) {
	const q = `
		SELECT id, automation_id, source_node_id, source_handle, target_node_id, created_at
		FROM automation_edges WHERE automation_id = $1`
	var recs []edgeRecord
	if err := r.db.SelectContext(ctx, &recs, q, automationID.String()); err != nil {
		return nil, err
	}
	out := make([]*automationdom.Edge, 0, len(recs))
	for i := range recs {
		e, err := recs[i].toDomain()
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// DeleteEdge implements automationdom.Repository.DeleteEdge.
func (r *AutomationRepository) DeleteEdge(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM automation_edges WHERE id = $1`, id.String())
	return err
}

// --- Execution-engine read paths ---------------------------------------------

// ListEnabledTriggerNodesByType implements automationdom.Repository.ListEnabledTriggerNodesByType.
func (r *AutomationRepository) ListEnabledTriggerNodesByType(ctx context.Context, projectID uuid.UUID, triggerType automationdom.TriggerType) ([]*automationdom.Node, error) {
	const q = `
		SELECT n.id, n.automation_id, n.kind, n.type, n.config, n.pos_x, n.pos_y, n.created_at, n.updated_at
		FROM automation_nodes n
		JOIN automations a ON a.id = n.automation_id
		WHERE a.project_id = $1 AND a.status = 'active' AND a.deleted_at IS NULL
		  AND n.kind = 'trigger' AND n.type = $2`
	var recs []nodeRecord
	if err := r.db.SelectContext(ctx, &recs, q, projectID.String(), string(triggerType)); err != nil {
		return nil, err
	}
	out := make([]*automationdom.Node, 0, len(recs))
	for i := range recs {
		n, err := recs[i].toDomain()
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// ListPredecessorTriggersWatching implements automationdom.Repository.ListPredecessorTriggersWatching.
func (r *AutomationRepository) ListPredecessorTriggersWatching(ctx context.Context, taskID uuid.UUID) ([]*automationdom.Node, error) {
	const q = `
		SELECT n.id, n.automation_id, n.kind, n.type, n.config, n.pos_x, n.pos_y, n.created_at, n.updated_at
		FROM automation_nodes n
		JOIN automations a ON a.id = n.automation_id
		WHERE a.status = 'active' AND a.deleted_at IS NULL
		  AND n.kind = 'trigger' AND n.type = 'predecessor_done'
		  AND n.config->'watched_task_ids' @> $1::jsonb`
	needle := fmt.Sprintf(`["%s"]`, taskID.String())
	var recs []nodeRecord
	if err := r.db.SelectContext(ctx, &recs, q, needle); err != nil {
		return nil, err
	}
	out := make([]*automationdom.Node, 0, len(recs))
	for i := range recs {
		n, err := recs[i].toDomain()
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// ListOutgoingEdges implements automationdom.Repository.ListOutgoingEdges.
func (r *AutomationRepository) ListOutgoingEdges(ctx context.Context, sourceNodeID uuid.UUID) ([]*automationdom.Edge, error) {
	const q = `
		SELECT id, automation_id, source_node_id, source_handle, target_node_id, created_at
		FROM automation_edges WHERE source_node_id = $1`
	var recs []edgeRecord
	if err := r.db.SelectContext(ctx, &recs, q, sourceNodeID.String()); err != nil {
		return nil, err
	}
	out := make([]*automationdom.Edge, 0, len(recs))
	for i := range recs {
		e, err := recs[i].toDomain()
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// FindAutomationByNodeID implements automationdom.Repository.FindAutomationByNodeID.
func (r *AutomationRepository) FindAutomationByNodeID(ctx context.Context, nodeID uuid.UUID) (*automationdom.Automation, error) {
	const q = `
		SELECT a.id, a.project_id, a.name, a.description, a.status, a.created_by, a.created_at, a.updated_at, a.deleted_at
		FROM automations a
		JOIN automation_nodes n ON n.automation_id = a.id
		WHERE n.id = $1 AND a.deleted_at IS NULL`
	var rec automationRecord
	if err := r.db.GetContext(ctx, &rec, q, nodeID.String()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, automationdom.ErrNotFound
		}
		return nil, err
	}
	return rec.toDomain()
}

// --- Runs / run steps ---------------------------------------------------------

// CreateRun implements automationdom.Repository.CreateRun.
func (r *AutomationRepository) CreateRun(ctx context.Context, run *automationdom.Run) error {
	var taskID any
	if run.TaskID != nil {
		taskID = run.TaskID.String()
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO automation_runs (id, automation_id, trigger_node_id, task_id, status, started_at, finished_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		run.ID.String(), run.AutomationID.String(), run.TriggerNodeID.String(), taskID, string(run.Status), run.StartedAt, run.FinishedAt,
	)
	return err
}

// UpdateRun implements automationdom.Repository.UpdateRun.
func (r *AutomationRepository) UpdateRun(ctx context.Context, run *automationdom.Run) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE automation_runs SET status = $1, finished_at = $2 WHERE id = $3`,
		string(run.Status), run.FinishedAt, run.ID.String(),
	)
	return err
}

// FindRunByID implements automationdom.Repository.FindRunByID.
func (r *AutomationRepository) FindRunByID(ctx context.Context, id uuid.UUID) (*automationdom.Run, error) {
	const q = `
		SELECT id, automation_id, trigger_node_id, task_id, status, started_at, finished_at
		FROM automation_runs WHERE id = $1`
	var rec runRecord
	if err := r.db.GetContext(ctx, &rec, q, id.String()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, automationdom.ErrRunNotFound
		}
		return nil, err
	}
	return rec.toDomain()
}

// ListRunsByAutomation implements automationdom.Repository.ListRunsByAutomation.
func (r *AutomationRepository) ListRunsByAutomation(ctx context.Context, automationID uuid.UUID, limit int) ([]*automationdom.Run, error) {
	if limit <= 0 {
		limit = 50
	}
	const q = `
		SELECT id, automation_id, trigger_node_id, task_id, status, started_at, finished_at
		FROM automation_runs WHERE automation_id = $1 ORDER BY started_at DESC LIMIT $2`
	var recs []runRecord
	if err := r.db.SelectContext(ctx, &recs, q, automationID.String(), limit); err != nil {
		return nil, err
	}
	out := make([]*automationdom.Run, 0, len(recs))
	for i := range recs {
		run, err := recs[i].toDomain()
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, nil
}

// CreateRunStep implements automationdom.Repository.CreateRunStep.
func (r *AutomationRepository) CreateRunStep(ctx context.Context, s *automationdom.RunStep) error {
	var errText *string
	if s.Error != "" {
		errText = &s.Error
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO automation_run_steps (id, run_id, node_id, status, input_snapshot, output_snapshot, error, executed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		s.ID.String(), s.RunID.String(), s.NodeID.String(), string(s.Status), []byte(s.InputSnapshot), []byte(s.OutputSnapshot), errText, s.ExecutedAt,
	)
	return err
}

// ListRunStepsByRun implements automationdom.Repository.ListRunStepsByRun.
func (r *AutomationRepository) ListRunStepsByRun(ctx context.Context, runID uuid.UUID) ([]*automationdom.RunStep, error) {
	const q = `
		SELECT id, run_id, node_id, status, input_snapshot, output_snapshot, error, executed_at
		FROM automation_run_steps WHERE run_id = $1 ORDER BY executed_at ASC`
	var recs []runStepRecord
	if err := r.db.SelectContext(ctx, &recs, q, runID.String()); err != nil {
		return nil, err
	}
	out := make([]*automationdom.RunStep, 0, len(recs))
	for i := range recs {
		s, err := recs[i].toDomain()
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// --- trigger_ai_agent pause/resume --------------------------------------------

// CreatePendingAgentWait implements automationdom.Repository.CreatePendingAgentWait.
func (r *AutomationRepository) CreatePendingAgentWait(ctx context.Context, w *automationdom.PendingAgentWait) error {
	ctxJSON, err := json.Marshal(w.Context)
	if err != nil {
		return fmt.Errorf("encode walk context: %w", err)
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO automation_pending_agent_waits (id, run_id, node_id, automation_id, conversation_id, project_id, context, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		w.ID.String(), w.RunID.String(), w.NodeID.String(), w.AutomationID.String(), w.ConversationID.String(), w.ProjectID.String(), ctxJSON, w.CreatedAt,
	)
	return err
}

// FindPendingAgentWait implements automationdom.Repository.FindPendingAgentWait.
func (r *AutomationRepository) FindPendingAgentWait(ctx context.Context, conversationID uuid.UUID) (*automationdom.PendingAgentWait, error) {
	const q = `
		SELECT id, run_id, node_id, automation_id, conversation_id, project_id, context, created_at
		FROM automation_pending_agent_waits WHERE conversation_id = $1`
	var rec pendingAgentWaitRecord
	if err := r.db.GetContext(ctx, &rec, q, conversationID.String()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return rec.toDomain()
}

// DeletePendingAgentWait implements automationdom.Repository.DeletePendingAgentWait.
func (r *AutomationRepository) DeletePendingAgentWait(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM automation_pending_agent_waits WHERE id = $1`, id.String())
	return err
}

// CountPendingAgentWaits implements automationdom.Repository.CountPendingAgentWaits.
func (r *AutomationRepository) CountPendingAgentWaits(ctx context.Context, runID uuid.UUID) (int, error) {
	var count int
	if err := r.db.GetContext(ctx, &count, `SELECT COUNT(*) FROM automation_pending_agent_waits WHERE run_id = $1`, runID.String()); err != nil {
		return 0, err
	}
	return count, nil
}

// DeletePendingAgentWaitAndCountRemaining implements
// automationdom.Repository.DeletePendingAgentWaitAndCountRemaining.
//
// A fanned-out trigger_ai_agent node's sibling conversations can reach a
// terminal status within moments of each other and be processed by
// different consumer replicas truly concurrently — dispatchMessages calls
// handleAgentConversationStatus synchronously within one replica's own
// read loop, but each replica has its own Redis consumer-group identity
// (see AutomationConsumer.consumerName), so different conversations'
// messages can land on different replicas at the same time. Deciding "am I
// the last sibling to resolve" can't be a separate DELETE then COUNT (or
// COUNT then DELETE) in either order: two siblings resolving concurrently
// can each read a stale view of the other's row and either both conclude
// they're not last (stalling the run forever, since neither ever proceeds)
// or both conclude they are (firing downstream twice). A transaction-
// scoped Postgres advisory lock keyed on (runID, nodeID) serializes
// exactly this contended decision across replicas — concurrent siblings
// for the same node take their turn one at a time, each seeing the prior
// one's delete already applied — without blocking unrelated nodes or runs.
func (r *AutomationRepository) DeletePendingAgentWaitAndCountRemaining(ctx context.Context, id, runID, nodeID uuid.UUID) (int, error) {
	var remaining int
	err := WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		lockKey := runID.String() + ":" + nodeID.String()
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
			return fmt.Errorf("acquire fan-out lock: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM automation_pending_agent_waits WHERE id = $1`, id.String()); err != nil {
			return fmt.Errorf("delete pending agent wait: %w", err)
		}
		if err := tx.GetContext(ctx, &remaining,
			`SELECT COUNT(*) FROM automation_pending_agent_waits WHERE run_id = $1 AND node_id = $2`,
			runID.String(), nodeID.String(),
		); err != nil {
			return fmt.Errorf("count remaining siblings: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return remaining, nil
}

// --- wait pause/resume ---------------------------------------------------------

// CreatePendingDelay implements automationdom.Repository.CreatePendingDelay.
func (r *AutomationRepository) CreatePendingDelay(ctx context.Context, d *automationdom.PendingDelay) error {
	ctxJSON, err := json.Marshal(d.Context)
	if err != nil {
		return fmt.Errorf("encode walk context: %w", err)
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO automation_pending_delays (id, run_id, node_id, automation_id, project_id, context, resume_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		d.ID.String(), d.RunID.String(), d.NodeID.String(), d.AutomationID.String(), d.ProjectID.String(), ctxJSON, d.ResumeAt, d.CreatedAt,
	)
	return err
}

// ListDueDelays implements automationdom.Repository.ListDueDelays.
func (r *AutomationRepository) ListDueDelays(ctx context.Context) ([]*automationdom.PendingDelay, error) {
	const q = `
		SELECT id, run_id, node_id, automation_id, project_id, context, resume_at, created_at
		FROM automation_pending_delays WHERE resume_at <= NOW()`
	var recs []pendingDelayRecord
	if err := r.db.SelectContext(ctx, &recs, q); err != nil {
		return nil, err
	}
	out := make([]*automationdom.PendingDelay, 0, len(recs))
	for i := range recs {
		d, err := recs[i].toDomain()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// DeletePendingDelay implements automationdom.Repository.DeletePendingDelay.
func (r *AutomationRepository) DeletePendingDelay(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM automation_pending_delays WHERE id = $1`, id.String())
	return err
}

// CountPendingDelays implements automationdom.Repository.CountPendingDelays.
func (r *AutomationRepository) CountPendingDelays(ctx context.Context, runID uuid.UUID) (int, error) {
	var count int
	if err := r.db.GetContext(ctx, &count, `SELECT COUNT(*) FROM automation_pending_delays WHERE run_id = $1`, runID.String()); err != nil {
		return 0, err
	}
	return count, nil
}

// --- Due-date scheduling -------------------------------------------------------

// ListDueDateCandidates implements automationdom.Repository.ListDueDateCandidates.
func (r *AutomationRepository) ListDueDateCandidates(ctx context.Context) ([]automationdom.DueDateCandidate, error) {
	const q = `
		SELECT n.id, n.automation_id, n.kind, n.type, n.config, n.pos_x, n.pos_y, n.created_at, n.updated_at,
		       t.id AS task_id
		FROM automation_nodes n
		JOIN automations a ON a.id = n.automation_id
		JOIN tasks t ON t.project_id = a.project_id
		WHERE n.kind = 'trigger' AND n.type = 'due_date_reached'
		  AND a.status = 'active' AND a.deleted_at IS NULL
		  AND t.deleted_at IS NULL AND t.due_date IS NOT NULL
		  AND t.due_date + (COALESCE((n.config->>'due_date_offset_minutes')::int, 0) || ' minutes')::interval <= NOW()
		  AND NOT EXISTS (
		      SELECT 1 FROM automation_due_date_fires f WHERE f.node_id = n.id AND f.task_id = t.id
		  )`
	type row struct {
		nodeRecord
		TaskID string `db:"task_id"`
	}
	var rows []row
	if err := r.db.SelectContext(ctx, &rows, q); err != nil {
		return nil, err
	}
	out := make([]automationdom.DueDateCandidate, 0, len(rows))
	for i := range rows {
		n, err := rows[i].toDomain()
		if err != nil {
			return nil, err
		}
		taskID, err := uuid.Parse(rows[i].TaskID)
		if err != nil {
			return nil, err
		}
		out = append(out, automationdom.DueDateCandidate{Node: n, TaskID: taskID})
	}
	return out, nil
}

// HasDueDateFired implements automationdom.Repository.HasDueDateFired.
func (r *AutomationRepository) HasDueDateFired(ctx context.Context, nodeID, taskID uuid.UUID) (bool, error) {
	const q = `SELECT EXISTS(SELECT 1 FROM automation_due_date_fires WHERE node_id = $1 AND task_id = $2)`
	var exists bool
	if err := r.db.GetContext(ctx, &exists, q, nodeID.String(), taskID.String()); err != nil {
		return false, err
	}
	return exists, nil
}

// RecordDueDateFire implements automationdom.Repository.RecordDueDateFire.
func (r *AutomationRepository) RecordDueDateFire(ctx context.Context, automationID, nodeID, taskID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO automation_due_date_fires (automation_id, node_id, task_id, fired_at)
		VALUES ($1, $2, $3, NOW()) ON CONFLICT (node_id, task_id) DO NOTHING`,
		automationID.String(), nodeID.String(), taskID.String(),
	)
	return err
}

// --- Cron scheduling ------------------------------------------------------------

// ListCronCandidates implements automationdom.Repository.ListCronCandidates.
func (r *AutomationRepository) ListCronCandidates(ctx context.Context) ([]automationdom.CronCandidate, error) {
	const q = `
		SELECT n.id, n.automation_id, n.kind, n.type, n.config, n.pos_x, n.pos_y, n.created_at, n.updated_at,
		       f.last_fired_at
		FROM automation_nodes n
		JOIN automations a ON a.id = n.automation_id
		LEFT JOIN automation_cron_fires f ON f.node_id = n.id
		WHERE n.kind = 'trigger' AND n.type = 'cron'
		  AND a.status = 'active' AND a.deleted_at IS NULL`
	type row struct {
		nodeRecord
		LastFiredAt *time.Time `db:"last_fired_at"`
	}
	var rows []row
	if err := r.db.SelectContext(ctx, &rows, q); err != nil {
		return nil, err
	}
	out := make([]automationdom.CronCandidate, 0, len(rows))
	for i := range rows {
		n, err := rows[i].toDomain()
		if err != nil {
			return nil, err
		}
		out = append(out, automationdom.CronCandidate{Node: n, LastFiredAt: rows[i].LastFiredAt})
	}
	return out, nil
}

// RecordCronFire implements automationdom.Repository.RecordCronFire.
func (r *AutomationRepository) RecordCronFire(ctx context.Context, automationID, nodeID uuid.UUID, firedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO automation_cron_fires (node_id, automation_id, last_fired_at, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (node_id) DO UPDATE SET last_fired_at = EXCLUDED.last_fired_at, updated_at = NOW()`,
		nodeID.String(), automationID.String(), firedAt,
	)
	return err
}

// --- Webhook trigger tokens ------------------------------------------------------

// CreateOrRotateWebhookToken atomically revokes tok.NodeID's current active
// token (if any) and inserts tok as the new active one — the partial unique
// index on (node_id) WHERE revoked_at IS NULL guards against a concurrent
// double-rotate leaving two active tokens.
func (r *AutomationRepository) CreateOrRotateWebhookToken(ctx context.Context, tok *automationdom.WebhookToken, tokenHash string) (*automationdom.WebhookToken, error) {
	var createdBy *string
	if tok.CreatedBy != nil {
		s := tok.CreatedBy.String()
		createdBy = &s
	}
	err := WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			UPDATE automation_webhook_tokens SET revoked_at = NOW()
			WHERE node_id = $1 AND revoked_at IS NULL`,
			tok.NodeID.String(),
		); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO automation_webhook_tokens (id, node_id, automation_id, token_prefix, token_hash, created_by, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, NOW())`,
			tok.ID.String(), tok.NodeID.String(), tok.AutomationID.String(), tok.TokenPrefix, tokenHash, createdBy,
		)
		return err
	})
	if err != nil {
		return nil, err
	}
	return r.FindActiveWebhookTokenByNodeID(ctx, tok.NodeID)
}

// FindActiveWebhookTokenByNodeID implements automationdom.Repository.FindActiveWebhookTokenByNodeID.
func (r *AutomationRepository) FindActiveWebhookTokenByNodeID(ctx context.Context, nodeID uuid.UUID) (*automationdom.WebhookToken, error) {
	const q = `
		SELECT id, node_id, automation_id, token_prefix, created_by, created_at, last_used_at, revoked_at
		FROM automation_webhook_tokens WHERE node_id = $1 AND revoked_at IS NULL`
	var rec webhookTokenRecord
	if err := r.db.GetContext(ctx, &rec, q, nodeID.String()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, automationdom.ErrNotFound
		}
		return nil, err
	}
	return rec.toDomain()
}

// VerifyWebhookToken matches node_id AND token_hash in one query — the
// hash comparison is the database's exact-match lookup, same pattern this
// repo already uses for API keys (apikeydom.Repository.FindByHash).
func (r *AutomationRepository) VerifyWebhookToken(ctx context.Context, nodeID uuid.UUID, tokenHash string) (*automationdom.WebhookToken, error) {
	const q = `
		SELECT id, node_id, automation_id, token_prefix, created_by, created_at, last_used_at, revoked_at
		FROM automation_webhook_tokens WHERE node_id = $1 AND token_hash = $2 AND revoked_at IS NULL`
	var rec webhookTokenRecord
	if err := r.db.GetContext(ctx, &rec, q, nodeID.String(), tokenHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, automationdom.ErrWebhookTokenInvalid
		}
		return nil, err
	}
	return rec.toDomain()
}

// RecordWebhookTokenUsed implements automationdom.Repository.RecordWebhookTokenUsed.
func (r *AutomationRepository) RecordWebhookTokenUsed(ctx context.Context, tokenID uuid.UUID, at time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE automation_webhook_tokens SET last_used_at = $1 WHERE id = $2`,
		at, tokenID.String(),
	)
	return err
}

// --- Status-in-use guard -------------------------------------------------------

// StatusUsedByAutomation reports whether any non-deleted automation's node
// config references statusID anywhere. Deliberately a broad text-containment
// check across the whole JSONB blob rather than an exhaustive per-node-type
// field enumeration: a false positive (blocking an unrelated status delete)
// is safe, a false negative (silently orphaning a live automation) is not.
func (r *AutomationRepository) StatusUsedByAutomation(ctx context.Context, statusID uuid.UUID) (bool, error) {
	const q = `
		SELECT EXISTS(
			SELECT 1 FROM automation_nodes n
			JOIN automations a ON a.id = n.automation_id
			WHERE a.deleted_at IS NULL AND n.config::text LIKE '%' || $1 || '%'
		)`
	var exists bool
	if err := r.db.GetContext(ctx, &exists, q, statusID.String()); err != nil {
		return false, err
	}
	return exists, nil
}
