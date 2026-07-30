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

	projectdom "github.com/Paca-AI/api/internal/domain/project"
)

// --- sqlx models ------------------------------------------------------------

type projectRecord struct {
	ID           string     `db:"id"`
	Name         string     `db:"name"`
	Description  string     `db:"description"`
	TaskIDPrefix string     `db:"task_id_prefix"`
	IsPublic     bool       `db:"is_public"`
	Settings     []byte     `db:"settings"`
	CreatedBy    *string    `db:"created_by"`
	CreatedAt    time.Time  `db:"created_at"`
	DeletedAt    *time.Time `db:"deleted_at"`
}

type projectRoleRecord struct {
	ID          string    `db:"id"`
	ProjectID   *string   `db:"project_id"`
	RoleName    string    `db:"role_name"`
	Permissions []byte    `db:"permissions"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}

// projectMemberReadRow is the result of the SELECT … JOIN query.
type projectMemberReadRow struct {
	ID            string     `db:"id"`
	ProjectID     string     `db:"project_id"`
	UserID        *string    `db:"user_id"`
	ProjectRoleID string     `db:"project_role_id"`
	MemberType    string     `db:"member_type"`
	AgentID       *string    `db:"agent_id"`
	Username      string     `db:"username"`
	FullName      string     `db:"full_name"`
	RoleName      string     `db:"role_name"`
	AgentName     string     `db:"agent_name"`
	AgentHandle   string     `db:"agent_handle"`
	CreatedAt     time.Time  `db:"created_at"`
	DeletedAt     *time.Time `db:"deleted_at"`
}

// --- Repository -------------------------------------------------------------

// ProjectRepository is the sqlx implementation of projectdom.Repository.
type ProjectRepository struct {
	db *sqlx.DB
}

// NewProjectRepository returns a new ProjectRepository.
func NewProjectRepository(db *sqlx.DB) *ProjectRepository {
	return &ProjectRepository{db: db}
}

const projectSelectCols = `id, name, description, task_id_prefix, is_public, settings, created_by, created_at, deleted_at`
const projectSelectColsQualified = `projects.id, projects.name, projects.description, projects.task_id_prefix, projects.is_public, projects.settings, projects.created_by, projects.created_at, projects.deleted_at`

// --- Projects ---------------------------------------------------------------

// List returns a page of projects and the total count.
func (r *ProjectRepository) List(ctx context.Context, offset, limit int) ([]*projectdom.Project, int64, error) {
	var total int64
	if err := r.db.GetContext(ctx, &total, `SELECT COUNT(*) FROM projects WHERE deleted_at IS NULL`); err != nil {
		return nil, 0, fmt.Errorf("project repo: list count: %w", err)
	}

	var records []projectRecord
	if err := r.db.SelectContext(ctx, &records, `SELECT `+projectSelectCols+` FROM projects WHERE deleted_at IS NULL ORDER BY created_at ASC OFFSET $1 LIMIT $2`, offset, limit); err != nil {
		return nil, 0, fmt.Errorf("project repo: list: %w", err)
	}

	projects := make([]*projectdom.Project, 0, len(records))
	for i := range records {
		p, err := toProjectEntity(&records[i])
		if err != nil {
			return nil, 0, err
		}
		projects = append(projects, p)
	}
	return projects, total, nil
}

// ListAccessible returns the projects that the given user is an active member of.
func (r *ProjectRepository) ListAccessible(ctx context.Context, userID uuid.UUID, offset, limit int) ([]*projectdom.Project, int64, error) {
	var total int64
	if err := r.db.GetContext(ctx, &total, `
		SELECT COUNT(*) FROM projects
		JOIN project_members ON project_members.project_id = projects.id
		WHERE project_members.user_id = $1 AND project_members.deleted_at IS NULL AND projects.deleted_at IS NULL`, userID.String()); err != nil {
		return nil, 0, fmt.Errorf("project repo: list accessible count: %w", err)
	}

	var records []projectRecord
	if err := r.db.SelectContext(ctx, &records, `
		SELECT `+projectSelectColsQualified+` FROM projects
		JOIN project_members ON project_members.project_id = projects.id
		WHERE project_members.user_id = $1 AND project_members.deleted_at IS NULL AND projects.deleted_at IS NULL
		ORDER BY projects.created_at ASC OFFSET $2 LIMIT $3`, userID.String(), offset, limit); err != nil {
		return nil, 0, fmt.Errorf("project repo: list accessible: %w", err)
	}

	projects := make([]*projectdom.Project, 0, len(records))
	for i := range records {
		p, err := toProjectEntity(&records[i])
		if err != nil {
			return nil, 0, err
		}
		projects = append(projects, p)
	}
	return projects, total, nil
}

// FindByID returns a project by its primary key.
func (r *ProjectRepository) FindByID(ctx context.Context, id uuid.UUID) (*projectdom.Project, error) {
	var record projectRecord
	err := r.db.GetContext(ctx, &record, `SELECT `+projectSelectCols+` FROM projects WHERE id = $1 AND deleted_at IS NULL`, id.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, projectdom.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("project repo: find by id: %w", err)
	}
	return toProjectEntity(&record)
}

// FindByTaskIDPrefix returns the first project whose task_id_prefix matches
// the given prefix (case-insensitive).  Returns projectdom.ErrNotFound when
// no match exists.
func (r *ProjectRepository) FindByTaskIDPrefix(ctx context.Context, prefix string) (*projectdom.Project, error) {
	var record projectRecord
	err := r.db.GetContext(ctx, &record, `SELECT `+projectSelectCols+` FROM projects WHERE upper(task_id_prefix) = upper($1) AND deleted_at IS NULL`, prefix)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, projectdom.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("project repo: find by task id prefix: %w", err)
	}
	return toProjectEntity(&record)
}

// Create persists a new project.
func (r *ProjectRepository) Create(ctx context.Context, p *projectdom.Project) error {
	rec, err := fromProjectEntity(p)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO projects (id, name, description, task_id_prefix, is_public, settings, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		rec.ID, rec.Name, rec.Description, rec.TaskIDPrefix, rec.IsPublic,
		rec.Settings, rec.CreatedBy, rec.CreatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return projectdom.ErrNameTaken
		}
		return fmt.Errorf("project repo: create: %w", err)
	}
	return nil
}

// Update saves changes to a project.
func (r *ProjectRepository) Update(ctx context.Context, p *projectdom.Project) error {
	settings, err := json.Marshal(p.Settings)
	if err != nil {
		return fmt.Errorf("project repo: marshal settings: %w", err)
	}

	var createdBy *string
	if p.CreatedBy != nil {
		s := p.CreatedBy.String()
		createdBy = &s
	}

	result, err := r.db.ExecContext(ctx, `
		UPDATE projects SET name=$1, description=$2, task_id_prefix=$3, is_public=$4, settings=$5, created_by=$6
		WHERE id=$7`,
		p.Name, p.Description, p.TaskIDPrefix, p.IsPublic, settings, createdBy, p.ID.String(),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return projectdom.ErrNameTaken
		}
		return fmt.Errorf("project repo: update: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return projectdom.ErrNotFound
	}
	return nil
}

// Delete soft-deletes a project by setting deleted_at.
func (r *ProjectRepository) Delete(ctx context.Context, id uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, `UPDATE projects SET deleted_at = $1 WHERE id = $2 AND deleted_at IS NULL`, time.Now(), id.String())
	if err != nil {
		return fmt.Errorf("project repo: delete: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return projectdom.ErrNotFound
	}
	return nil
}

// --- Project Roles ----------------------------------------------------------

const projectRoleCols = `id, project_id, role_name, permissions, created_at, updated_at`

// ListRoles returns project-scoped roles for a given project.
func (r *ProjectRepository) ListRoles(ctx context.Context, projectID uuid.UUID) ([]*projectdom.ProjectRole, error) {
	var records []projectRoleRecord
	if err := r.db.SelectContext(ctx, &records, `SELECT `+projectRoleCols+` FROM project_roles WHERE project_id = $1 ORDER BY role_name ASC`, projectID.String()); err != nil {
		return nil, fmt.Errorf("project repo: list roles: %w", err)
	}

	roles := make([]*projectdom.ProjectRole, 0, len(records))
	for i := range records {
		role, err := toProjectRoleEntity(&records[i])
		if err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, nil
}

// FindRoleByID returns a project role by its primary key.
func (r *ProjectRepository) FindRoleByID(ctx context.Context, id uuid.UUID) (*projectdom.ProjectRole, error) {
	var record projectRoleRecord
	err := r.db.GetContext(ctx, &record, `SELECT `+projectRoleCols+` FROM project_roles WHERE id = $1`, id.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, projectdom.ErrRoleNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("project repo: find role by id: %w", err)
	}
	return toProjectRoleEntity(&record)
}

// FindRoleByName returns a role by name within the given project scope.
func (r *ProjectRepository) FindRoleByName(ctx context.Context, projectID uuid.UUID, name string) (*projectdom.ProjectRole, error) {
	var record projectRoleRecord
	err := r.db.GetContext(ctx, &record, `SELECT `+projectRoleCols+` FROM project_roles WHERE project_id = $1 AND role_name = $2`, projectID.String(), name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, projectdom.ErrRoleNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("project repo: find role by name: %w", err)
	}
	return toProjectRoleEntity(&record)
}

// CreateRole persists a new project role.
func (r *ProjectRepository) CreateRole(ctx context.Context, role *projectdom.ProjectRole) error {
	rec, err := fromProjectRoleEntity(role)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO project_roles (id, project_id, role_name, permissions, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		rec.ID, rec.ProjectID, rec.RoleName, rec.Permissions, rec.CreatedAt, rec.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return projectdom.ErrRoleNameTaken
		}
		return fmt.Errorf("project repo: create role: %w", err)
	}
	return nil
}

// UpdateRole saves changes to a project role.
func (r *ProjectRepository) UpdateRole(ctx context.Context, role *projectdom.ProjectRole) error {
	perms, err := json.Marshal(role.Permissions)
	if err != nil {
		return fmt.Errorf("project repo: marshal role permissions: %w", err)
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE project_roles SET role_name=$1, permissions=$2, updated_at=$3 WHERE id=$4`,
		role.RoleName, perms, role.UpdatedAt, role.ID.String(),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return projectdom.ErrRoleNameTaken
		}
		return fmt.Errorf("project repo: update role: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return projectdom.ErrRoleNotFound
	}
	return nil
}

// DeleteRole removes a project role.
func (r *ProjectRepository) DeleteRole(ctx context.Context, id uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM project_roles WHERE id = $1`, id.String())
	if err != nil {
		return fmt.Errorf("project repo: delete role: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return projectdom.ErrRoleNotFound
	}
	return nil
}

// CountMembersWithRole returns the number of active project members assigned to the role.
func (r *ProjectRepository) CountMembersWithRole(ctx context.Context, roleID uuid.UUID) (int64, error) {
	var count int64
	if err := r.db.GetContext(ctx, &count, `SELECT COUNT(*) FROM project_members WHERE project_role_id = $1`, roleID.String()); err != nil {
		return 0, fmt.Errorf("project repo: count members with role: %w", err)
	}
	return count, nil
}

// --- Project Members --------------------------------------------------------

const projectMemberCols = `
	pm.id, pm.project_id, pm.user_id, pm.project_role_id, pm.member_type, pm.agent_id, pm.created_at,
	COALESCE(u.username, '') AS username, COALESCE(u.full_name, '') AS full_name, pr.role_name,
	COALESCE(a.name, '') AS agent_name, COALESCE(a.handle, '') AS agent_handle`

// ListMembers returns all active (non-deleted) members of a project enriched with user and role info.
func (r *ProjectRepository) ListMembers(ctx context.Context, projectID uuid.UUID) ([]*projectdom.ProjectMember, error) {
	var rows []projectMemberReadRow
	if err := r.db.SelectContext(ctx, &rows, `
		SELECT `+projectMemberCols+`
		FROM project_members pm
		LEFT JOIN users u ON u.id = pm.user_id AND u.deleted_at IS NULL
		JOIN project_roles pr ON pr.id = pm.project_role_id
		LEFT JOIN agents a ON a.id = pm.agent_id AND a.deleted_at IS NULL
		WHERE pm.project_id = $1 AND pm.deleted_at IS NULL
		ORDER BY COALESCE(u.username, a.handle) ASC`, projectID.String()); err != nil {
		return nil, fmt.Errorf("project repo: list members: %w", err)
	}

	members := make([]*projectdom.ProjectMember, 0, len(rows))
	for i := range rows {
		members = append(members, toMemberEntity(&rows[i]))
	}
	return members, nil
}

// FindMember returns a single active member record for the given project + user combo.
func (r *ProjectRepository) FindMember(ctx context.Context, projectID, userID uuid.UUID) (*projectdom.ProjectMember, error) {
	var row projectMemberReadRow
	err := r.db.GetContext(ctx, &row, `
		SELECT `+projectMemberCols+`
		FROM project_members pm
		LEFT JOIN users u ON u.id = pm.user_id AND u.deleted_at IS NULL
		JOIN project_roles pr ON pr.id = pm.project_role_id
		LEFT JOIN agents a ON a.id = pm.agent_id AND a.deleted_at IS NULL
		WHERE pm.project_id = $1 AND pm.user_id = $2 AND pm.deleted_at IS NULL`,
		projectID.String(), userID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, projectdom.ErrMemberNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("project repo: find member: %w", err)
	}
	return toMemberEntity(&row), nil
}

// FindMemberByAgent returns a single active member record for the given project + agent combo.
func (r *ProjectRepository) FindMemberByAgent(ctx context.Context, projectID, agentID uuid.UUID) (*projectdom.ProjectMember, error) {
	var row projectMemberReadRow
	err := r.db.GetContext(ctx, &row, `
		SELECT `+projectMemberCols+`
		FROM project_members pm
		LEFT JOIN users u ON u.id = pm.user_id AND u.deleted_at IS NULL
		JOIN project_roles pr ON pr.id = pm.project_role_id
		LEFT JOIN agents a ON a.id = pm.agent_id AND a.deleted_at IS NULL
		WHERE pm.project_id = $1 AND pm.agent_id = $2 AND pm.member_type = 'agent' AND pm.deleted_at IS NULL`,
		projectID.String(), agentID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, projectdom.ErrMemberNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("project repo: find member by agent: %w", err)
	}
	return toMemberEntity(&row), nil
}

// FindMemberByUserProject returns the active member record for a (user_id, project_id)
// pair. It is used by the activity consumer to resolve a user UUID to a member UUID.
func (r *ProjectRepository) FindMemberByUserProject(ctx context.Context, userID, projectID uuid.UUID) (*projectdom.ProjectMember, error) {
	return r.FindMember(ctx, projectID, userID)
}

// FindMemberByActor resolves an actor to a project member.
// When agentID is non-nil the agent's member record is returned via
// FindMemberByAgent; otherwise the user's member record is returned via
// FindMemberByUserProject. This is the single canonical actor-resolution method
// used by activity services and stream consumers.
func (r *ProjectRepository) FindMemberByActor(ctx context.Context, projectID, actorID uuid.UUID, agentID *uuid.UUID) (*projectdom.ProjectMember, error) {
	if agentID != nil {
		return r.FindMemberByAgent(ctx, projectID, *agentID)
	}
	return r.FindMemberByUserProject(ctx, actorID, projectID)
}

// FindMemberByID returns the active member record for the given project_members.id.
// Used by the notification service to resolve an assignee member ID to a user ID.
func (r *ProjectRepository) FindMemberByID(ctx context.Context, memberID uuid.UUID) (*projectdom.ProjectMember, error) {
	var row projectMemberReadRow
	err := r.db.GetContext(ctx, &row, `
		SELECT `+projectMemberCols+`
		FROM project_members pm
		LEFT JOIN users u ON u.id = pm.user_id AND u.deleted_at IS NULL
		JOIN project_roles pr ON pr.id = pm.project_role_id
		LEFT JOIN agents a ON a.id = pm.agent_id AND a.deleted_at IS NULL
		WHERE pm.id = $1 AND pm.deleted_at IS NULL`, memberID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, projectdom.ErrMemberNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("project repo: find member by id: %w", err)
	}
	return toMemberEntity(&row), nil
}

// AddMember inserts a project_members row, or restores a previously soft-deleted one.
func (r *ProjectRepository) AddMember(ctx context.Context, m *projectdom.ProjectMember) error {
	// First try to restore a previously soft-deleted membership for this
	// project+user pair, updating only the role (preserving original created_at).
	restore, err := r.db.ExecContext(ctx, `
		UPDATE project_members
		SET project_role_id = $1, deleted_at = NULL
		WHERE project_id = $2 AND user_id = $3 AND deleted_at IS NOT NULL`,
		m.ProjectRoleID.String(), m.ProjectID.String(), m.UserID.String(),
	)
	if err != nil {
		return fmt.Errorf("project repo: restore member: %w", err)
	}
	if n, _ := restore.RowsAffected(); n > 0 {
		return nil
	}

	// No soft-deleted row to restore; insert a fresh membership.
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO project_members (id, project_id, user_id, project_role_id, member_type, created_at, deleted_at)
		VALUES ($1, $2, $3, $4, 'human', NOW(), NULL)
		ON CONFLICT (project_id, user_id) WHERE deleted_at IS NULL DO NOTHING`,
		m.ID.String(), m.ProjectID.String(), m.UserID.String(), m.ProjectRoleID.String(),
	)
	if err != nil {
		return fmt.Errorf("project repo: add member: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return projectdom.ErrMemberAlreadyAdded
	}
	return nil
}

// AddAgentMember inserts a project_members row for an AI agent.
func (r *ProjectRepository) AddAgentMember(ctx context.Context, memberID, projectID, agentID, roleID uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO project_members (id, project_id, agent_id, project_role_id, member_type, user_id, created_at, deleted_at)
		VALUES ($1, $2, $3, $4, 'agent', NULL, NOW(), NULL)
		ON CONFLICT (project_id, agent_id) WHERE deleted_at IS NULL AND member_type = 'agent' DO NOTHING`,
		memberID.String(), projectID.String(), agentID.String(), roleID.String(),
	)
	if err != nil {
		return fmt.Errorf("project repo: add agent member: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return projectdom.ErrMemberAlreadyAdded
	}
	return nil
}

// RemoveAgentMember soft-deletes the membership row for the given agent.
func (r *ProjectRepository) RemoveAgentMember(ctx context.Context, projectID, agentID uuid.UUID) error {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx, `
		UPDATE project_members SET deleted_at = $1
		WHERE project_id = $2 AND agent_id = $3 AND member_type = 'agent'`, now, projectID.String(), agentID.String())
	if err != nil {
		return fmt.Errorf("project repo: remove agent member: %w", err)
	}
	return nil
}

// UpdateMemberRole changes the role of an existing active project member.
func (r *ProjectRepository) UpdateMemberRole(ctx context.Context, projectID, userID, roleID uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE project_members SET project_role_id = $1 WHERE project_id = $2 AND user_id = $3 AND deleted_at IS NULL`,
		roleID.String(), projectID.String(), userID.String(),
	)
	if err != nil {
		return fmt.Errorf("project repo: update member role: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return projectdom.ErrMemberNotFound
	}
	return nil
}

// RemoveMember soft-deletes the membership row for the given project + user.
func (r *ProjectRepository) RemoveMember(ctx context.Context, projectID, userID uuid.UUID) error {
	now := time.Now().UTC()
	result, err := r.db.ExecContext(ctx, `
		UPDATE project_members SET deleted_at = $1 WHERE project_id = $2 AND user_id = $3 AND deleted_at IS NULL`,
		now, projectID.String(), userID.String(),
	)
	if err != nil {
		return fmt.Errorf("project repo: remove member: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return projectdom.ErrMemberNotFound
	}
	return nil
}

// UpdateMemberRoleByMemberID changes the role of an existing active project member by member ID.
func (r *ProjectRepository) UpdateMemberRoleByMemberID(ctx context.Context, memberID, roleID uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE project_members SET project_role_id = $1 WHERE id = $2 AND deleted_at IS NULL`,
		roleID.String(), memberID.String(),
	)
	if err != nil {
		return fmt.Errorf("project repo: update member role: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return projectdom.ErrMemberNotFound
	}
	return nil
}

// RemoveMemberByMemberID soft-deletes the membership row for the given member ID.
func (r *ProjectRepository) RemoveMemberByMemberID(ctx context.Context, memberID uuid.UUID) error {
	now := time.Now().UTC()
	result, err := r.db.ExecContext(ctx, `
		UPDATE project_members SET deleted_at = $1 WHERE id = $2 AND deleted_at IS NULL`,
		now, memberID.String(),
	)
	if err != nil {
		return fmt.Errorf("project repo: remove member: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return projectdom.ErrMemberNotFound
	}
	return nil
}

// --- Mapping helpers --------------------------------------------------------

func toProjectEntity(rec *projectRecord) (*projectdom.Project, error) {
	id, err := uuid.Parse(rec.ID)
	if err != nil {
		return nil, fmt.Errorf("project repo: parse id: %w", err)
	}
	settings := map[string]any{}
	if len(rec.Settings) > 0 {
		if err := json.Unmarshal(rec.Settings, &settings); err != nil {
			return nil, fmt.Errorf("project repo: unmarshal settings: %w", err)
		}
	}
	var createdBy *uuid.UUID
	if rec.CreatedBy != nil {
		if uid, err := uuid.Parse(*rec.CreatedBy); err == nil {
			createdBy = &uid
		}
	}
	return &projectdom.Project{
		ID:           id,
		Name:         rec.Name,
		Description:  rec.Description,
		TaskIDPrefix: rec.TaskIDPrefix,
		IsPublic:     rec.IsPublic,
		Settings:     settings,
		CreatedBy:    createdBy,
		CreatedAt:    rec.CreatedAt,
		DeletedAt:    rec.DeletedAt,
	}, nil
}

func fromProjectEntity(p *projectdom.Project) (*projectRecord, error) {
	settings, err := json.Marshal(p.Settings)
	if err != nil {
		return nil, fmt.Errorf("project repo: marshal settings: %w", err)
	}
	var createdBy *string
	if p.CreatedBy != nil {
		s := p.CreatedBy.String()
		createdBy = &s
	}
	return &projectRecord{
		ID:           p.ID.String(),
		Name:         p.Name,
		Description:  p.Description,
		TaskIDPrefix: p.TaskIDPrefix,
		IsPublic:     p.IsPublic,
		Settings:     settings,
		CreatedBy:    createdBy,
		CreatedAt:    p.CreatedAt,
	}, nil
}

func toProjectRoleEntity(rec *projectRoleRecord) (*projectdom.ProjectRole, error) {
	id, err := uuid.Parse(rec.ID)
	if err != nil {
		return nil, fmt.Errorf("project role repo: parse id: %w", err)
	}
	perms := map[string]any{}
	if len(rec.Permissions) > 0 {
		if err := json.Unmarshal(rec.Permissions, &perms); err != nil {
			return nil, fmt.Errorf("project role repo: unmarshal permissions: %w", err)
		}
	}
	var projectID *uuid.UUID
	if rec.ProjectID != nil {
		if uid, err := uuid.Parse(*rec.ProjectID); err == nil {
			projectID = &uid
		}
	}
	return &projectdom.ProjectRole{
		ID:          id,
		ProjectID:   projectID,
		RoleName:    rec.RoleName,
		Permissions: perms,
		CreatedAt:   rec.CreatedAt,
		UpdatedAt:   rec.UpdatedAt,
	}, nil
}

func fromProjectRoleEntity(r *projectdom.ProjectRole) (*projectRoleRecord, error) {
	perms, err := json.Marshal(r.Permissions)
	if err != nil {
		return nil, fmt.Errorf("project role repo: marshal permissions: %w", err)
	}
	var projectID *string
	if r.ProjectID != nil {
		s := r.ProjectID.String()
		projectID = &s
	}
	return &projectRoleRecord{
		ID:          r.ID.String(),
		ProjectID:   projectID,
		RoleName:    r.RoleName,
		Permissions: perms,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}, nil
}

func toMemberEntity(row *projectMemberReadRow) *projectdom.ProjectMember {
	id, _ := uuid.Parse(row.ID)
	projectID, _ := uuid.Parse(row.ProjectID)
	roleID, _ := uuid.Parse(row.ProjectRoleID)
	m := &projectdom.ProjectMember{
		ID:            id,
		ProjectID:     projectID,
		ProjectRoleID: roleID,
		Username:      row.Username,
		FullName:      row.FullName,
		RoleName:      row.RoleName,
		CreatedAt:     row.CreatedAt,
		DeletedAt:     row.DeletedAt,
		MemberType:    row.MemberType,
		AgentName:     row.AgentName,
		AgentHandle:   row.AgentHandle,
	}
	if row.UserID != nil {
		userID, _ := uuid.Parse(*row.UserID)
		m.UserID = userID
	}
	if row.AgentID != nil {
		agentID, _ := uuid.Parse(*row.AgentID)
		m.AgentID = &agentID
	}
	return m
}
