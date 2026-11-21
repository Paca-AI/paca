package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	projectdom "github.com/Paca-AI/api/internal/domain/project"
	roledom "github.com/Paca-AI/api/internal/domain/role"
)

// --- sqlx models ------------------------------------------------------------

type projectRecord struct {
	ID              string     `db:"id"`
	Name            string     `db:"name"`
	Description     string     `db:"description"`
	TaskIDPrefix    string     `db:"task_id_prefix"`
	Settings        []byte     `db:"settings"`
	AvatarKey       *string    `db:"avatar_key"`
	AvatarThumbKey  *string    `db:"avatar_thumb_key"`
	CreatedBy       *string    `db:"created_by"`
	CreatedAt       time.Time  `db:"created_at"`
	DeletedAt       *time.Time `db:"deleted_at"`
	JevAPIKeySecret string     `db:"jev_api_key_secret"`
	JevBaseURL      string     `db:"jev_base_url"`
	JevModel        string     `db:"jev_model"`
}

// projectMemberReadRow is the result of the SELECT … JOIN query.
type projectMemberReadRow struct {
	ID                  string     `db:"id"`
	ProjectID           string     `db:"project_id"`
	AccountID           string     `db:"account_id"`
	UserID              *string    `db:"user_id"`
	MemberType          string     `db:"member_type"`
	AgentID             *string    `db:"agent_id"`
	Username            string     `db:"username"`
	FullName            string     `db:"full_name"`
	AgentName           string     `db:"agent_name"`
	AgentHandle         string     `db:"agent_handle"`
	UserAvatarKey       *string    `db:"user_avatar_key"`
	UserAvatarThumbKey  *string    `db:"user_avatar_thumb_key"`
	AgentAvatarKey      *string    `db:"agent_avatar_key"`
	AgentAvatarThumbKey *string    `db:"agent_avatar_thumb_key"`
	AgentType           string     `db:"agent_type"`
	AgentLLMProvider    string     `db:"agent_llm_provider"`
	AgentACPProvider    *string    `db:"agent_acp_provider"`
	AgentCLIProvider    *string    `db:"agent_cli_provider"`
	AgentDescription    string     `db:"agent_description"`
	Description         string     `db:"description"`
	CreatedAt           time.Time  `db:"created_at"`
	DeletedAt           *time.Time `db:"deleted_at"`
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

const projectSelectCols = `id, name, description, task_id_prefix, settings, avatar_key, avatar_thumb_key, created_by, created_at, deleted_at, jev_api_key_secret, jev_base_url, jev_model`
const projectSelectColsQualified = `projects.id, projects.name, projects.description, projects.task_id_prefix, projects.settings, projects.avatar_key, projects.avatar_thumb_key, projects.created_by, projects.created_at, projects.deleted_at, projects.jev_api_key_secret, projects.jev_base_url, projects.jev_model`

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
func (r *ProjectRepository) ListAccessible(ctx context.Context, accountID uuid.UUID, offset, limit int) ([]*projectdom.Project, int64, error) {
	var total int64
	if err := r.db.GetContext(ctx, &total, `
		SELECT COUNT(*) FROM projects
		JOIN project_members ON project_members.project_id = projects.id
		WHERE project_members.account_id = $1 AND project_members.deleted_at IS NULL AND projects.deleted_at IS NULL`, accountID.String()); err != nil {
		return nil, 0, fmt.Errorf("project repo: list accessible count: %w", err)
	}

	var records []projectRecord
	if err := r.db.SelectContext(ctx, &records, `
		SELECT `+projectSelectColsQualified+` FROM projects
		JOIN project_members ON project_members.project_id = projects.id
		WHERE project_members.account_id = $1 AND project_members.deleted_at IS NULL AND projects.deleted_at IS NULL
		ORDER BY projects.created_at ASC OFFSET $2 LIMIT $3`, accountID.String(), offset, limit); err != nil {
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

// FindByTaskIDPrefix returns the active project whose non-empty task_id_prefix matches
// the given prefix (case-insensitive).  Returns projectdom.ErrNotFound when
// no match exists.
func (r *ProjectRepository) FindByTaskIDPrefix(ctx context.Context, prefix string) (*projectdom.Project, error) {
	var record projectRecord
	err := r.db.GetContext(ctx, &record, `SELECT `+projectSelectCols+` FROM projects WHERE upper(task_id_prefix) = upper($1) AND task_id_prefix <> '' AND deleted_at IS NULL`, prefix)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, projectdom.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("project repo: find by task id prefix: %w", err)
	}
	return toProjectEntity(&record)
}

// Create persists a new project together with what setup describes — its
// roles and the creator's membership and role attachment — in one
// transaction, so a project never exists without them.
func (r *ProjectRepository) Create(ctx context.Context, p *projectdom.Project, setup projectdom.ProjectSetup) error {
	rec, err := fromProjectEntity(p)
	if err != nil {
		return err
	}
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO projects (id, name, description, task_id_prefix, settings, created_by, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			rec.ID, rec.Name, rec.Description, rec.TaskIDPrefix,
			rec.Settings, rec.CreatedBy, rec.CreatedAt,
		); err != nil {
			if constraint, ok := uniqueViolationConstraint(err); ok && constraint == "idx_projects_task_id_prefix" {
				return projectdom.ErrPrefixTaken
			}
			if isUniqueViolation(err) {
				return projectdom.ErrNameTaken
			}
			return fmt.Errorf("project repo: create: %w", err)
		}

		roleIDs := make(map[string]uuid.UUID, len(setup.Roles))
		for _, role := range setup.Roles {
			var id string
			if err := tx.GetContext(ctx, &id, `
				INSERT INTO roles (name, description, policy, project_id, is_system)
				VALUES ($1, $2, $3::jsonb, $4::uuid, $5) RETURNING id`,
				role.Name, role.Description, string(role.Policy), p.ID.String(), role.System); err != nil {
				if isRoleNameViolation(err) {
					return roledom.ErrNameTaken
				}
				return fmt.Errorf("project repo: create role %s: %w", role.Name, err)
			}
			parsed, err := uuid.Parse(id)
			if err != nil {
				return fmt.Errorf("project repo: create role %s: bad id: %w", role.Name, err)
			}
			roleIDs[role.Name] = parsed
		}

		if setup.Creator == nil {
			return nil
		}
		creatorRole, ok := roleIDs[setup.CreatorRole]
		if !ok {
			return fmt.Errorf("project repo: creator role %q is not among the project roles", setup.CreatorRole)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO project_members (id, project_id, account_id, created_at, deleted_at)
			VALUES ($1, $2, $3, NOW(), NULL)`,
			uuid.NewString(), p.ID.String(), setup.Creator.String()); err != nil {
			return fmt.Errorf("project repo: add creator: %w", err)
		}
		_, err := attachRolesTx(ctx, tx, *setup.Creator, &p.ID,
			[]uuid.UUID{creatorRole}, setup.Creator)
		return err
	})
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
		UPDATE projects SET name=$1, description=$2, task_id_prefix=$3, settings=$4,
		  avatar_key=$5, avatar_thumb_key=$6, created_by=$7
		WHERE id=$8`,
		p.Name, p.Description, p.TaskIDPrefix, settings,
		p.AvatarKey, p.AvatarThumbKey, createdBy, p.ID.String(),
	)
	if err != nil {
		if constraint, ok := uniqueViolationConstraint(err); ok && constraint == "idx_projects_task_id_prefix" {
			return projectdom.ErrPrefixTaken
		}
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

// UpdateJevConfig sets a project's Jev (AI decision API) credentials — a
// dedicated statement separate from Update, mirroring how agents.
// llm_api_key_secret is written through its own statement rather than
// folded into the general agent update (see agent_repository.go's
// UpdateAgent). apiKeySecret is stored exactly as given (already encrypted
// by the caller, service/project's encryptJevKey) — this method has no
// encryption awareness of its own.
func (r *ProjectRepository) UpdateJevConfig(ctx context.Context, projectID uuid.UUID, apiKeySecret, baseURL, model string) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE projects SET jev_api_key_secret=$1, jev_base_url=$2, jev_model=$3
		WHERE id=$4 AND deleted_at IS NULL`,
		apiKeySecret, baseURL, model, projectID.String(),
	)
	if err != nil {
		return fmt.Errorf("project repo: update jev config: %w", err)
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

// --- Project Members --------------------------------------------------------

// projectMemberCols reads a member: the membership row (pm), the shared
// identity from the member's account (ac) and the kind-specific rows (u for a
// user, a for an agent). member_type is the account kind, with 'human' for a
// user. It must be used with projectMemberFrom.
const projectMemberCols = `
	pm.id, pm.project_id, pm.account_id,
	CASE WHEN ac.kind = 'user' THEN pm.account_id END AS user_id,
	CASE ac.kind WHEN 'user' THEN 'human' ELSE ac.kind END AS member_type,
	CASE WHEN ac.kind = 'agent' THEN pm.account_id END AS agent_id,
	pm.created_at,
	COALESCE(u.username, '') AS username,
	CASE WHEN ac.kind <> 'agent' THEN ac.name ELSE '' END AS full_name,
	CASE WHEN ac.kind = 'agent' THEN ac.name ELSE '' END AS agent_name,
	COALESCE(a.handle, '') AS agent_handle,
	CASE WHEN ac.kind <> 'agent' THEN ac.avatar_key END AS user_avatar_key,
	CASE WHEN ac.kind <> 'agent' THEN ac.avatar_thumb_key END AS user_avatar_thumb_key,
	CASE WHEN ac.kind = 'agent' THEN ac.avatar_key END AS agent_avatar_key,
	CASE WHEN ac.kind = 'agent' THEN ac.avatar_thumb_key END AS agent_avatar_thumb_key,
	COALESCE(a.agent_type, '') AS agent_type, COALESCE(a.llm_provider, '') AS agent_llm_provider,
	a.acp_provider AS agent_acp_provider,
	a.cli_provider AS agent_cli_provider,
	CASE WHEN ac.kind = 'agent' THEN ac.description ELSE '' END AS agent_description,
	COALESCE(pm.description, '') AS description`

const projectMemberFrom = `project_members pm
		JOIN accounts ac ON ac.id = pm.account_id
		LEFT JOIN users u ON u.id = pm.account_id
		LEFT JOIN agents a ON a.id = pm.account_id`

// ListAllMembers returns all active (non-deleted) members of a project enriched with user and role info.
func (r *ProjectRepository) ListAllMembers(ctx context.Context, projectID uuid.UUID) ([]*projectdom.ProjectMember, error) {
	var rows []projectMemberReadRow
	if err := r.db.SelectContext(ctx, &rows, `
		SELECT `+projectMemberCols+`
		FROM `+projectMemberFrom+`
		WHERE pm.project_id = $1 AND pm.deleted_at IS NULL
		ORDER BY COALESCE(u.username, a.handle, ac.name) ASC`, projectID.String()); err != nil {
		return nil, fmt.Errorf("project repo: list members: %w", err)
	}

	members := make([]*projectdom.ProjectMember, 0, len(rows))
	for i := range rows {
		members = append(members, toMemberEntity(&rows[i]))
	}
	if err := r.loadMemberRoles(ctx, projectID, members); err != nil {
		return nil, err
	}
	return members, nil
}

const memberNameSortKey = `LOWER(COALESCE(u.username, a.handle, ac.name))`

// memberListWhere builds the WHERE clause (and args, numbered from $1) for a
// project's member list.
func memberListWhere(projectID uuid.UUID, filter projectdom.MemberFilter) (string, []any) {
	args := []any{projectID.String()}
	where := `pm.project_id = $1 AND pm.deleted_at IS NULL`
	if filter.RoleID != nil {
		args = append(args, filter.RoleID.String())
		where += fmt.Sprintf(` AND EXISTS (
			SELECT 1 FROM role_attachments ra
			WHERE ra.project_id = pm.project_id AND ra.account_id = pm.account_id
			  AND ra.role_id = $%d::uuid)`, len(args))
	}
	if filter.MemberType != "" {
		args = append(args, filter.MemberType)
		where += fmt.Sprintf(` AND (CASE ac.kind WHEN 'user' THEN 'human' ELSE ac.kind END) = $%d`, len(args))
	}
	if len(filter.AccountIDs) > 0 {
		ids := make([]string, 0, len(filter.AccountIDs))
		for _, id := range filter.AccountIDs {
			ids = append(ids, id.String())
		}
		args = append(args, ids)
		where += fmt.Sprintf(` AND (pm.account_id = ANY($%[1]d::uuid[]) OR pm.id = ANY($%[1]d::uuid[]))`, len(args))
	}
	for _, word := range strings.Fields(filter.Search) {
		args = append(args, "%"+escapeLike(word)+"%")
		n := len(args)
		where += fmt.Sprintf(` AND (LOWER(COALESCE(u.username, '')) LIKE LOWER($%[1]d) ESCAPE '\' OR LOWER(ac.name) LIKE LOWER($%[1]d) ESCAPE '\' OR LOWER(COALESCE(a.handle, '')) LIKE LOWER($%[1]d) ESCAPE '\')`, n)
	}
	return where, args
}

// CountMembers returns the number of active members of the project matching filter.
func (r *ProjectRepository) CountMembers(ctx context.Context, projectID uuid.UUID, filter projectdom.MemberFilter) (int64, error) {
	where, args := memberListWhere(projectID, filter)
	var n int64
	if err := r.db.GetContext(ctx, &n, `SELECT COUNT(*) FROM `+projectMemberFrom+` WHERE `+where, args...); err != nil {
		return 0, fmt.Errorf("project repo: count members: %w", err)
	}
	return n, nil
}

// ListMembers returns up to limit members ordered by name after the
// member named by cursorAfter, and whether more follow. Filtering happens in
// the query, before the page is cut.
func (r *ProjectRepository) ListMembers(ctx context.Context, projectID uuid.UUID, limit int, cursorAfter *string, filter projectdom.MemberFilter) ([]*projectdom.ProjectMember, bool, error) {
	if limit <= 0 {
		limit = 20
	}
	where, args := memberListWhere(projectID, filter)
	if cursorAfter != nil {
		key, cursorID, err := projectdom.DecodeMemberCursor(*cursorAfter)
		if err != nil {
			return nil, false, err
		}
		args = append(args, key, cursorID.String())
		where += fmt.Sprintf(` AND (`+memberNameSortKey+`, pm.id) > (LOWER($%d::text), $%d::uuid)`, len(args)-1, len(args))
	}
	args = append(args, limit+1)

	var rows []projectMemberReadRow
	if err := r.db.SelectContext(ctx, &rows, `
		SELECT `+projectMemberCols+`
		FROM `+projectMemberFrom+`
		WHERE `+where+fmt.Sprintf(`
		ORDER BY `+memberNameSortKey+`, pm.id
		LIMIT $%d`, len(args)), args...); err != nil {
		return nil, false, fmt.Errorf("project repo: list members after: %w", err)
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	members := make([]*projectdom.ProjectMember, 0, len(rows))
	for i := range rows {
		members = append(members, toMemberEntity(&rows[i]))
	}
	if !filter.SkipRoles {
		if err := r.loadMemberRoles(ctx, projectID, members); err != nil {
			return nil, false, err
		}
	}
	return members, hasMore, nil
}

// loadMemberRoles fills in Roles on members of one project: for each member,
// the roles attached to its principal inside that project, sorted by name.
func (r *ProjectRepository) loadMemberRoles(ctx context.Context, projectID uuid.UUID, members []*projectdom.ProjectMember) error {
	if len(members) == 0 {
		return nil
	}
	accounts := make([]string, 0, len(members))
	for _, m := range members {
		accounts = append(accounts, m.AccountID.String())
	}
	var rows []struct {
		AccountID string `db:"account_id"`
		RoleID    string `db:"role_id"`
		Name      string `db:"name"`
	}
	if err := r.db.SelectContext(ctx, &rows, `
		SELECT ra.account_id::text AS account_id, r.id::text AS role_id, r.name
		FROM role_attachments ra JOIN roles r ON r.id = ra.role_id
		WHERE ra.project_id = $1::uuid AND ra.account_id = ANY($2::uuid[])
		ORDER BY r.name, r.id`, projectID.String(), accounts); err != nil {
		return fmt.Errorf("project repo: load member roles: %w", err)
	}
	byAccount := make(map[string][]roledom.Summary, len(rows))
	for _, row := range rows {
		rid, err := uuid.Parse(row.RoleID)
		if err != nil {
			return fmt.Errorf("project repo: load member roles: bad role id %q: %w", row.RoleID, err)
		}
		byAccount[row.AccountID] = append(byAccount[row.AccountID], roledom.Summary{ID: rid, Name: row.Name})
	}
	for _, m := range members {
		m.Roles = byAccount[m.AccountID.String()]
		if m.Roles == nil {
			m.Roles = []roledom.Summary{}
		}
	}
	return nil
}

// memberWithRoles loads the roles of a single member read.
func (r *ProjectRepository) memberWithRoles(ctx context.Context, m *projectdom.ProjectMember) (*projectdom.ProjectMember, error) {
	if err := r.loadMemberRoles(ctx, m.ProjectID, []*projectdom.ProjectMember{m}); err != nil {
		return nil, err
	}
	return m, nil
}

// CountDistinctAgentsByProjects returns the number of distinct agents with
// an active membership across all of projectIDs, in a single query — the
// cross-project equivalent of calling ListAllMembers once per project and
// deduping agent_id in application code.
func (r *ProjectRepository) CountDistinctAgentsByProjects(ctx context.Context, projectIDs []uuid.UUID) (int64, error) {
	if len(projectIDs) == 0 {
		return 0, nil
	}

	b := newQueryBuilder()
	b.addInClause("project_id", uuidSliceToStrSlice(projectIDs))

	var count int64
	query := `
		SELECT COUNT(DISTINCT pm.account_id)
		FROM project_members pm
		JOIN accounts ac ON ac.id = pm.account_id AND ac.kind = 'agent'
		WHERE pm.deleted_at IS NULL AND pm.` + b.whereClauses[0]
	if err := r.db.GetContext(ctx, &count, query, b.args...); err != nil {
		return 0, fmt.Errorf("project repo: count distinct agents: %w", err)
	}
	return count, nil
}

// FindMember returns the active member record of an account (user, agent or
// service account) in the project.
func (r *ProjectRepository) FindMember(ctx context.Context, projectID, accountID uuid.UUID) (*projectdom.ProjectMember, error) {
	var row projectMemberReadRow
	err := r.db.GetContext(ctx, &row, `
		SELECT `+projectMemberCols+`
		FROM `+projectMemberFrom+`
		WHERE pm.project_id = $1 AND pm.account_id = $2 AND pm.deleted_at IS NULL`,
		projectID.String(), accountID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, projectdom.ErrMemberNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("project repo: find member: %w", err)
	}
	return r.memberWithRoles(ctx, toMemberEntity(&row))
}

// FindMemberByID returns the active member record for the given project_members.id.
// Used by the notification service to resolve an assignee member ID to a user ID.
func (r *ProjectRepository) FindMemberByID(ctx context.Context, memberID uuid.UUID) (*projectdom.ProjectMember, error) {
	var row projectMemberReadRow
	err := r.db.GetContext(ctx, &row, `
		SELECT `+projectMemberCols+`
		FROM `+projectMemberFrom+`
		WHERE pm.id = $1 AND pm.deleted_at IS NULL`, memberID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, projectdom.ErrMemberNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("project repo: find member by id: %w", err)
	}
	return r.memberWithRoles(ctx, toMemberEntity(&row))
}

// AddMember inserts a project_members row, or restores a previously
// soft-deleted one, and attaches roleIDs to the member inside the project, all
// in one transaction. Whatever role attachments the principal still has in
// the project are dropped first, so the member starts with exactly roleIDs.
func (r *ProjectRepository) AddMember(ctx context.Context, m *projectdom.ProjectMember, roleIDs []uuid.UUID, createdBy *uuid.UUID) error {
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		// First try to restore a previously soft-deleted membership for this
		// project+user pair, updating its description (preserving original
		// created_at).
		restore, err := tx.ExecContext(ctx, `
			UPDATE project_members
			SET description = $3, deleted_at = NULL
			WHERE project_id = $1 AND account_id = $2 AND deleted_at IS NOT NULL`,
			m.ProjectID.String(), m.AccountID.String(), m.Description,
		)
		if err != nil {
			return fmt.Errorf("project repo: restore member: %w", err)
		}
		restored, _ := restore.RowsAffected()
		if restored == 0 {
			// No soft-deleted row to restore; insert a fresh membership.
			result, err := tx.ExecContext(ctx, `
				INSERT INTO project_members (id, project_id, account_id, description, created_at, deleted_at)
				VALUES ($1, $2, $3, $4, NOW(), NULL)
				ON CONFLICT (project_id, account_id) WHERE deleted_at IS NULL DO NOTHING`,
				m.ID.String(), m.ProjectID.String(), m.AccountID.String(), m.Description,
			)
			if err != nil {
				return fmt.Errorf("project repo: add member: %w", err)
			}
			if n, _ := result.RowsAffected(); n == 0 {
				return projectdom.ErrMemberAlreadyAdded
			}
		}
		return replaceProjectAttachmentsTx(ctx, tx, m.AccountID, m.ProjectID, roleIDs, createdBy)
	})
}

// AddAgentMember inserts a project_members row for an AI agent and attaches
// roleIDs to the agent inside the project, in one transaction.
func (r *ProjectRepository) AddAgentMember(ctx context.Context, memberID, projectID, agentID uuid.UUID, roleIDs []uuid.UUID, createdBy *uuid.UUID) error {
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO project_members (id, project_id, account_id, created_at, deleted_at)
			VALUES ($1, $2, $3, NOW(), NULL)
			ON CONFLICT (project_id, account_id) WHERE deleted_at IS NULL DO NOTHING`,
			memberID.String(), projectID.String(), agentID.String(),
		)
		if err != nil {
			return fmt.Errorf("project repo: add agent member: %w", err)
		}
		if n, _ := result.RowsAffected(); n == 0 {
			return projectdom.ErrMemberAlreadyAdded
		}
		return replaceProjectAttachmentsTx(ctx, tx, agentID, projectID, roleIDs, createdBy)
	})
}

// replaceProjectAttachmentsTx makes the principal's attachments inside the
// project exactly roleIDs. It is used right after a membership row was
// created, when any attachment the principal still has there is stale (the
// principal was not an active member, so it granted nothing).
func replaceProjectAttachmentsTx(ctx context.Context, tx *sqlx.Tx, accountID, projectID uuid.UUID, roleIDs []uuid.UUID, createdBy *uuid.UUID) error {
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM role_attachments
		WHERE project_id = $1::uuid AND account_id = $2::uuid`,
		projectID.String(), accountID.String()); err != nil {
		return fmt.Errorf("project repo: clear stale attachments: %w", err)
	}
	_, err := attachRolesTx(ctx, tx, accountID, &projectID, roleIDs, createdBy)
	return err
}

// RemoveAgentMember soft-deletes the membership row for the given agent.
func (r *ProjectRepository) RemoveAgentMember(ctx context.Context, projectID, agentID uuid.UUID) error {
	now := time.Now().UTC()
	var n int64
	err := r.db.QueryRowContext(ctx, removeMembersSQL(`project_id = $2 AND account_id = $3`),
		now, projectID.String(), agentID.String()).Scan(&n)
	if err != nil {
		return fmt.Errorf("project repo: remove agent member: %w", err)
	}
	return nil
}

// removeMembersSQL soft-deletes the project_members rows matching where
// (with $1 = deleted_at) and, in the same statement, deletes the removed
// principals' role_attachments scoped to that project, so a removed member
// keeps no IAM role there (re-adding them starts from nothing). It returns
// the number of membership rows removed.
func removeMembersSQL(where string) string {
	return `
		WITH removed AS (
			UPDATE project_members SET deleted_at = $1
			WHERE ` + where + ` AND deleted_at IS NULL
			RETURNING project_id, account_id
		), detached AS (
			DELETE FROM role_attachments ra
			USING removed m
			WHERE ra.project_id = m.project_id AND ra.account_id = m.account_id
			RETURNING 1
		)
		SELECT count(*) FROM removed`
}

// RemoveMember soft-deletes the membership row for the given project + user.
func (r *ProjectRepository) RemoveMember(ctx context.Context, projectID, accountID uuid.UUID) error {
	now := time.Now().UTC()
	var n int64
	err := r.db.QueryRowContext(ctx, removeMembersSQL(`project_id = $2 AND account_id = $3`),
		now, projectID.String(), accountID.String()).Scan(&n)
	if err != nil {
		return fmt.Errorf("project repo: remove member: %w", err)
	}
	if n == 0 {
		return projectdom.ErrMemberNotFound
	}
	return nil
}

// UpdateMemberDescription changes the Jev-facing description of an existing
// active project member by member ID.
func (r *ProjectRepository) UpdateMemberDescription(ctx context.Context, memberID uuid.UUID, description string) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE project_members SET description = $1 WHERE id = $2 AND deleted_at IS NULL`,
		description, memberID.String(),
	)
	if err != nil {
		return fmt.Errorf("project repo: update member description: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return projectdom.ErrMemberNotFound
	}
	return nil
}

// RemoveMemberByMemberID soft-deletes the membership row for the given member ID.
func (r *ProjectRepository) RemoveMemberByMemberID(ctx context.Context, memberID uuid.UUID) error {
	now := time.Now().UTC()
	var n int64
	err := r.db.QueryRowContext(ctx, removeMembersSQL(`id = $2`), now, memberID.String()).Scan(&n)
	if err != nil {
		return fmt.Errorf("project repo: remove member: %w", err)
	}
	if n == 0 {
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
		ID:              id,
		Name:            rec.Name,
		Description:     rec.Description,
		TaskIDPrefix:    rec.TaskIDPrefix,
		Settings:        settings,
		AvatarKey:       rec.AvatarKey,
		AvatarThumbKey:  rec.AvatarThumbKey,
		CreatedBy:       createdBy,
		CreatedAt:       rec.CreatedAt,
		DeletedAt:       rec.DeletedAt,
		JevAPIKeySecret: rec.JevAPIKeySecret,
		JevBaseURL:      rec.JevBaseURL,
		JevModel:        rec.JevModel,
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
		Settings:     settings,
		CreatedBy:    createdBy,
		CreatedAt:    p.CreatedAt,
	}, nil
}

func toMemberEntity(row *projectMemberReadRow) *projectdom.ProjectMember {
	id, _ := uuid.Parse(row.ID)
	projectID, _ := uuid.Parse(row.ProjectID)
	m := &projectdom.ProjectMember{
		ID:                  id,
		ProjectID:           projectID,
		Roles:               []roledom.Summary{},
		Username:            row.Username,
		FullName:            row.FullName,
		CreatedAt:           row.CreatedAt,
		DeletedAt:           row.DeletedAt,
		MemberType:          row.MemberType,
		AgentName:           row.AgentName,
		AgentHandle:         row.AgentHandle,
		UserAvatarKey:       row.UserAvatarKey,
		UserAvatarThumbKey:  row.UserAvatarThumbKey,
		AgentAvatarKey:      row.AgentAvatarKey,
		AgentAvatarThumbKey: row.AgentAvatarThumbKey,
		AgentType:           row.AgentType,
		AgentLLMProvider:    row.AgentLLMProvider,
		AgentACPProvider:    row.AgentACPProvider,
		AgentCLIProvider:    row.AgentCLIProvider,
		AgentDescription:    row.AgentDescription,
		Description:         row.Description,
	}
	m.AccountID, _ = uuid.Parse(row.AccountID)
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
