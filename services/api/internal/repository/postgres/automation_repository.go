package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	automationdom "github.com/Paca-AI/api/internal/domain/automation"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
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

// AutomationRepository is the sqlx implementation of automationdom.Repository.
type AutomationRepository struct {
	db *sqlx.DB
}

// NewAutomationRepository returns a new AutomationRepository.
func NewAutomationRepository(db *sqlx.DB) *AutomationRepository {
	return &AutomationRepository{db: db}
}

// --- Automation ---------------------------------------------------------------

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

func (r *AutomationRepository) DeleteAutomation(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `UPDATE automations SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL`, id.String())
	return err
}

// --- Graph ----------------------------------------------------------------

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

func (r *AutomationRepository) CreateNode(ctx context.Context, n *automationdom.Node) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO automation_nodes (id, automation_id, kind, type, config, pos_x, pos_y, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		n.ID.String(), n.AutomationID.String(), string(n.Kind), n.Type, []byte(n.Config), n.PosX, n.PosY, n.CreatedAt, n.UpdatedAt,
	)
	return err
}

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

func (r *AutomationRepository) UpdateNode(ctx context.Context, n *automationdom.Node) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE automation_nodes SET config = $1, pos_x = $2, pos_y = $3, updated_at = $4 WHERE id = $5`,
		[]byte(n.Config), n.PosX, n.PosY, n.UpdatedAt, n.ID.String(),
	)
	return err
}

func (r *AutomationRepository) DeleteNode(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM automation_nodes WHERE id = $1`, id.String())
	return err
}

// --- Edges ------------------------------------------------------------------

func (r *AutomationRepository) CreateEdge(ctx context.Context, e *automationdom.Edge) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO automation_edges (id, automation_id, source_node_id, source_handle, target_node_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		e.ID.String(), e.AutomationID.String(), e.SourceNodeID.String(), e.SourceHandle, e.TargetNodeID.String(), e.CreatedAt,
	)
	return err
}

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

func (r *AutomationRepository) DeleteEdge(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM automation_edges WHERE id = $1`, id.String())
	return err
}

// --- Execution-engine read paths ---------------------------------------------

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

func (r *AutomationRepository) UpdateRun(ctx context.Context, run *automationdom.Run) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE automation_runs SET status = $1, finished_at = $2 WHERE id = $3`,
		string(run.Status), run.FinishedAt, run.ID.String(),
	)
	return err
}

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

// --- Due-date scheduling -------------------------------------------------------

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
		n, err := rows[i].nodeRecord.toDomain()
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

func (r *AutomationRepository) HasDueDateFired(ctx context.Context, nodeID, taskID uuid.UUID) (bool, error) {
	const q = `SELECT EXISTS(SELECT 1 FROM automation_due_date_fires WHERE node_id = $1 AND task_id = $2)`
	var exists bool
	if err := r.db.GetContext(ctx, &exists, q, nodeID.String(), taskID.String()); err != nil {
		return false, err
	}
	return exists, nil
}

func (r *AutomationRepository) RecordDueDateFire(ctx context.Context, automationID, nodeID, taskID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO automation_due_date_fires (automation_id, node_id, task_id, fired_at)
		VALUES ($1, $2, $3, NOW()) ON CONFLICT (node_id, task_id) DO NOTHING`,
		automationID.String(), nodeID.String(), taskID.String(),
	)
	return err
}

// --- Cron scheduling ------------------------------------------------------------

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
		n, err := rows[i].nodeRecord.toDomain()
		if err != nil {
			return nil, err
		}
		out = append(out, automationdom.CronCandidate{Node: n, LastFiredAt: rows[i].LastFiredAt})
	}
	return out, nil
}

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
