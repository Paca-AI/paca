package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	projectdom "github.com/paca/api/internal/domain/project"
	"gorm.io/gorm"
)

// --- GORM models ------------------------------------------------------------

type projectRecord struct {
	ID          string `gorm:"primarykey;type:uuid"`
	Name        string `gorm:"not null"`
	Description string
	Settings    []byte  `gorm:"type:jsonb;not null"`
	CreatedBy   *string `gorm:"type:uuid"`
	CreatedAt   time.Time
}

func (projectRecord) TableName() string { return "projects" }

type projectRoleRecord struct {
	ID          string  `gorm:"primarykey;type:uuid"`
	ProjectID   *string `gorm:"type:uuid;column:project_id"`
	RoleName    string  `gorm:"column:role_name;not null"`
	Permissions []byte  `gorm:"type:jsonb;not null"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (projectRoleRecord) TableName() string { return "project_roles" }

// projectMemberReadRow is the result of the SELECT … JOIN query.
type projectMemberReadRow struct {
	ID            string
	ProjectID     string
	UserID        string
	ProjectRoleID string
	Username      string
	FullName      string
	RoleName      string
}

// --- Repository -------------------------------------------------------------

// ProjectRepository is the GORM implementation of projectdom.Repository.
type ProjectRepository struct {
	db *gorm.DB
}

// NewProjectRepository returns a new ProjectRepository.
func NewProjectRepository(db *gorm.DB) *ProjectRepository {
	return &ProjectRepository{db: db}
}

// --- Projects ---------------------------------------------------------------

// List returns a page of projects and the total count.
func (r *ProjectRepository) List(ctx context.Context, offset, limit int) ([]*projectdom.Project, int64, error) {
	var total int64
	if err := r.db.WithContext(ctx).Table("projects").Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("project repo: list count: %w", err)
	}

	var records []projectRecord
	if err := r.db.WithContext(ctx).
		Order("created_at ASC").
		Offset(offset).
		Limit(limit).
		Find(&records).Error; err != nil {
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

// FindByID returns a project by its primary key.
func (r *ProjectRepository) FindByID(ctx context.Context, id uuid.UUID) (*projectdom.Project, error) {
	var record projectRecord
	result := r.db.WithContext(ctx).First(&record, "id = ?", id.String())
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, projectdom.ErrNotFound
	}
	if result.Error != nil {
		return nil, fmt.Errorf("project repo: find by id: %w", result.Error)
	}
	return toProjectEntity(&record)
}

// Create persists a new project.
func (r *ProjectRepository) Create(ctx context.Context, p *projectdom.Project) error {
	rec, err := fromProjectEntity(p)
	if err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Create(rec).Error; err != nil {
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

	result := r.db.WithContext(ctx).
		Model(&projectRecord{}).
		Where("id = ?", p.ID.String()).
		Updates(map[string]any{
			"name":        p.Name,
			"description": p.Description,
			"settings":    settings,
			"created_by":  createdBy,
		})
	if result.Error != nil {
		if isUniqueViolation(result.Error) {
			return projectdom.ErrNameTaken
		}
		return fmt.Errorf("project repo: update: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return projectdom.ErrNotFound
	}
	return nil
}

// Delete removes a project by ID. The DB schema cascades deletes to child tables.
func (r *ProjectRepository) Delete(ctx context.Context, id uuid.UUID) error {
	result := r.db.WithContext(ctx).Delete(&projectRecord{}, "id = ?", id.String())
	if result.Error != nil {
		return fmt.Errorf("project repo: delete: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return projectdom.ErrNotFound
	}
	return nil
}

// --- Project Roles ----------------------------------------------------------

// ListRoles returns project-scoped roles for a given project.
func (r *ProjectRepository) ListRoles(ctx context.Context, projectID uuid.UUID) ([]*projectdom.ProjectRole, error) {
	var records []projectRoleRecord
	if err := r.db.WithContext(ctx).
		Where("project_id = ?", projectID.String()).
		Order("role_name ASC").
		Find(&records).Error; err != nil {
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
	result := r.db.WithContext(ctx).First(&record, "id = ?", id.String())
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, projectdom.ErrRoleNotFound
	}
	if result.Error != nil {
		return nil, fmt.Errorf("project repo: find role by id: %w", result.Error)
	}
	return toProjectRoleEntity(&record)
}

// FindRoleByName returns a role by name within the given project scope.
func (r *ProjectRepository) FindRoleByName(ctx context.Context, projectID uuid.UUID, name string) (*projectdom.ProjectRole, error) {
	var record projectRoleRecord
	result := r.db.WithContext(ctx).
		Where("project_id = ? AND role_name = ?", projectID.String(), name).
		First(&record)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, projectdom.ErrRoleNotFound
	}
	if result.Error != nil {
		return nil, fmt.Errorf("project repo: find role by name: %w", result.Error)
	}
	return toProjectRoleEntity(&record)
}

// CreateRole persists a new project role.
func (r *ProjectRepository) CreateRole(ctx context.Context, role *projectdom.ProjectRole) error {
	rec, err := fromProjectRoleEntity(role)
	if err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Create(rec).Error; err != nil {
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
	result := r.db.WithContext(ctx).
		Model(&projectRoleRecord{}).
		Where("id = ?", role.ID.String()).
		Updates(map[string]any{
			"role_name":   role.RoleName,
			"permissions": perms,
			"updated_at":  role.UpdatedAt,
		})
	if result.Error != nil {
		if isUniqueViolation(result.Error) {
			return projectdom.ErrRoleNameTaken
		}
		return fmt.Errorf("project repo: update role: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return projectdom.ErrRoleNotFound
	}
	return nil
}

// DeleteRole removes a project role.
func (r *ProjectRepository) DeleteRole(ctx context.Context, id uuid.UUID) error {
	result := r.db.WithContext(ctx).Delete(&projectRoleRecord{}, "id = ?", id.String())
	if result.Error != nil {
		return fmt.Errorf("project repo: delete role: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return projectdom.ErrRoleNotFound
	}
	return nil
}

// CountMembersWithRole returns the number of project members assigned to the role.
func (r *ProjectRepository) CountMembersWithRole(ctx context.Context, roleID uuid.UUID) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).
		Table("project_members").
		Where("project_role_id = ?", roleID.String()).
		Count(&count).Error; err != nil {
		return 0, fmt.Errorf("project repo: count members with role: %w", err)
	}
	return count, nil
}

// --- Project Members --------------------------------------------------------

const projectMemberCols = `
	pm.id, pm.project_id, pm.user_id, pm.project_role_id,
	u.username, u.full_name, pr.role_name`

// ListMembers returns all members of a project enriched with user and role info.
func (r *ProjectRepository) ListMembers(ctx context.Context, projectID uuid.UUID) ([]*projectdom.ProjectMember, error) {
	var rows []projectMemberReadRow
	if err := r.db.WithContext(ctx).
		Table("project_members pm").
		Select(projectMemberCols).
		Joins("JOIN users u ON u.id = pm.user_id AND u.deleted_at IS NULL").
		Joins("JOIN project_roles pr ON pr.id = pm.project_role_id").
		Where("pm.project_id = ?", projectID.String()).
		Order("u.username ASC").
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("project repo: list members: %w", err)
	}

	members := make([]*projectdom.ProjectMember, 0, len(rows))
	for i := range rows {
		members = append(members, toMemberEntity(&rows[i]))
	}
	return members, nil
}

// FindMember returns a single member record for the given project + user combo.
func (r *ProjectRepository) FindMember(ctx context.Context, projectID, userID uuid.UUID) (*projectdom.ProjectMember, error) {
	var row projectMemberReadRow
	result := r.db.WithContext(ctx).
		Table("project_members pm").
		Select(projectMemberCols).
		Joins("JOIN users u ON u.id = pm.user_id AND u.deleted_at IS NULL").
		Joins("JOIN project_roles pr ON pr.id = pm.project_role_id").
		Where("pm.project_id = ? AND pm.user_id = ?", projectID.String(), userID.String()).
		Scan(&row)
	if result.Error != nil {
		return nil, fmt.Errorf("project repo: find member: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, projectdom.ErrMemberNotFound
	}
	return toMemberEntity(&row), nil
}

// AddMember inserts a project_members row.
func (r *ProjectRepository) AddMember(ctx context.Context, m *projectdom.ProjectMember) error {
	rec := map[string]any{
		"id":              m.ID.String(),
		"project_id":      m.ProjectID.String(),
		"user_id":         m.UserID.String(),
		"project_role_id": m.ProjectRoleID.String(),
	}
	if err := r.db.WithContext(ctx).Table("project_members").Create(rec).Error; err != nil {
		if isUniqueViolation(err) {
			return projectdom.ErrMemberAlreadyAdded
		}
		return fmt.Errorf("project repo: add member: %w", err)
	}
	return nil
}

// UpdateMemberRole changes the role of an existing project member.
func (r *ProjectRepository) UpdateMemberRole(ctx context.Context, projectID, userID, roleID uuid.UUID) error {
	result := r.db.WithContext(ctx).
		Table("project_members").
		Where("project_id = ? AND user_id = ?", projectID.String(), userID.String()).
		Update("project_role_id", roleID.String())
	if result.Error != nil {
		return fmt.Errorf("project repo: update member role: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return projectdom.ErrMemberNotFound
	}
	return nil
}

// RemoveMember deletes the membership row for the given project + user.
func (r *ProjectRepository) RemoveMember(ctx context.Context, projectID, userID uuid.UUID) error {
	result := r.db.WithContext(ctx).
		Table("project_members").
		Where("project_id = ? AND user_id = ?", projectID.String(), userID.String()).
		Delete(nil)
	if result.Error != nil {
		return fmt.Errorf("project repo: remove member: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return projectdom.ErrMemberNotFound
	}
	return nil
}

// --- Mapping helpers --------------------------------------------------------

func toProjectEntity(rec *projectRecord) (*projectdom.Project, error) {
	id, _ := uuid.Parse(rec.ID)
	var settings map[string]any
	if err := json.Unmarshal(rec.Settings, &settings); err != nil {
		settings = map[string]any{}
	}
	var createdBy *uuid.UUID
	if rec.CreatedBy != nil {
		if uid, err := uuid.Parse(*rec.CreatedBy); err == nil {
			createdBy = &uid
		}
	}
	return &projectdom.Project{
		ID:          id,
		Name:        rec.Name,
		Description: rec.Description,
		Settings:    settings,
		CreatedBy:   createdBy,
		CreatedAt:   rec.CreatedAt,
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
		ID:          p.ID.String(),
		Name:        p.Name,
		Description: p.Description,
		Settings:    settings,
		CreatedBy:   createdBy,
		CreatedAt:   p.CreatedAt,
	}, nil
}

func toProjectRoleEntity(rec *projectRoleRecord) (*projectdom.ProjectRole, error) {
	id, _ := uuid.Parse(rec.ID)
	var perms map[string]any
	if err := json.Unmarshal(rec.Permissions, &perms); err != nil {
		perms = map[string]any{}
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
	userID, _ := uuid.Parse(row.UserID)
	roleID, _ := uuid.Parse(row.ProjectRoleID)
	return &projectdom.ProjectMember{
		ID:            id,
		ProjectID:     projectID,
		UserID:        userID,
		ProjectRoleID: roleID,
		Username:      row.Username,
		FullName:      row.FullName,
		RoleName:      row.RoleName,
	}
}
