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

	activitydom "github.com/Paca-AI/api/internal/domain/activity"
)

// ActivityRepository is the one reader and writer of the activities table —
// the project feed, the per-entity timelines, the agent tab and comments all
// go through it.
type ActivityRepository struct {
	db *sqlx.DB
}

// NewActivityRepository returns an ActivityRepository backed by db.
func NewActivityRepository(db *sqlx.DB) *ActivityRepository {
	return &ActivityRepository{db: db}
}

type activityRecord struct {
	ID           string           `db:"id"`
	ProjectID    string           `db:"project_id"`
	EntityType   string           `db:"entity_type"`
	EntityID     *string          `db:"entity_id"`
	ActorID      *string          `db:"actor_id"`
	Origin       string           `db:"origin"`
	ActivityType string           `db:"activity_type"`
	Content      *json.RawMessage `db:"content"`
	CreatedAt    time.Time        `db:"created_at"`
	UpdatedAt    time.Time        `db:"updated_at"`
	DeletedAt    *time.Time       `db:"deleted_at"`

	ActorFullName       *string `db:"actor_full_name"`
	ActorUsername       *string `db:"actor_username"`
	ActorAvatarKey      *string `db:"actor_avatar_key"`
	ActorAvatarThumbKey *string `db:"actor_avatar_thumb_key"`
	EntityTitle         *string `db:"entity_title"`
	EntityDeleted       bool    `db:"entity_deleted"`
}

// activityTitleSQL is the entity's title, shared by the select list and the
// search filter. Soft-deleted rows still resolve, so a deleted task keeps its
// name in the log.
const activityTitleSQL = `CASE a.entity_type
	           WHEN 'task'        THEN t.title
	           WHEN 'doc'         THEN d.title
	           WHEN 'sprint'      THEN s.name
	           WHEN 'view'        THEN sv.name
	           WHEN 'automation'  THEN au.name
	           WHEN 'environment' THEN e.name
	           WHEN 'member'      THEN COALESCE(mu.full_name, mag.name)
	           WHEN 'project'     THEN pr.name
	           WHEN 'role'        THEN ro.role_name
	           WHEN 'agent'       THEN eag.name
	       END`

// activityDeletedSQL is true when the entity is gone — hard-deleted (no row)
// or soft-deleted.
const activityDeletedSQL = `CASE a.entity_type
	           WHEN 'task'        THEN t.id IS NULL OR t.deleted_at IS NOT NULL
	           WHEN 'doc'         THEN d.id IS NULL OR d.deleted_at IS NOT NULL
	           WHEN 'sprint'      THEN s.id IS NULL
	           WHEN 'view'        THEN sv.id IS NULL
	           WHEN 'automation'  THEN au.id IS NULL OR au.deleted_at IS NOT NULL
	           WHEN 'environment' THEN e.id IS NULL OR e.deleted_at IS NOT NULL
	           WHEN 'member'      THEN mpm.id IS NULL OR mpm.deleted_at IS NOT NULL
	           WHEN 'project'     THEN pr.id IS NULL OR pr.deleted_at IS NOT NULL
	           WHEN 'role'        THEN ro.id IS NULL
	           WHEN 'agent'       THEN eag.id IS NULL OR eag.deleted_at IS NOT NULL
	           ELSE FALSE
	       END`

// activitySelectSQL resolves the actor (user or agent member) and the
// entity's title in one pass. Titles are read live rather than stored so a
// rename shows up everywhere.
var activitySelectSQL = fmt.Sprintf(`
	SELECT a.id, a.project_id, a.entity_type, a.entity_id, a.actor_id,
	       a.origin, a.activity_type, a.content, a.created_at, a.updated_at, a.deleted_at,
	       COALESCE(u.full_name, ag.name)                    AS actor_full_name,
	       COALESCE(u.username, ag.handle)                   AS actor_username,
	       COALESCE(u.avatar_key, ag.avatar_key)             AS actor_avatar_key,
	       COALESCE(u.avatar_thumb_key, ag.avatar_thumb_key) AS actor_avatar_thumb_key,
	       %s AS entity_title,
	       COALESCE(%s, FALSE) AS entity_deleted
	FROM activities a
	LEFT JOIN project_members pm  ON pm.id = a.actor_id
	LEFT JOIN users u             ON u.id = pm.user_id
	LEFT JOIN agents ag           ON ag.id = pm.agent_id
	LEFT JOIN tasks t             ON a.entity_type = 'task'        AND t.id = a.entity_id
	LEFT JOIN documents d         ON a.entity_type = 'doc'         AND d.id = a.entity_id
	LEFT JOIN sprints s           ON a.entity_type = 'sprint'      AND s.id = a.entity_id
	LEFT JOIN sprint_views sv     ON a.entity_type = 'view'        AND sv.id = a.entity_id
	LEFT JOIN automations au      ON a.entity_type = 'automation'  AND au.id = a.entity_id
	LEFT JOIN environments e      ON a.entity_type = 'environment' AND e.id = a.entity_id
	LEFT JOIN project_members mpm ON a.entity_type = 'member'      AND mpm.id = a.entity_id
	LEFT JOIN users mu            ON mu.id = mpm.user_id
	LEFT JOIN agents mag          ON mag.id = mpm.agent_id
	LEFT JOIN projects pr         ON a.entity_type = 'project'     AND pr.id = a.entity_id
	LEFT JOIN project_roles ro    ON a.entity_type = 'role'        AND ro.id = a.entity_id
	LEFT JOIN agents eag          ON a.entity_type = 'agent'       AND eag.id = a.entity_id`,
	activityTitleSQL, activityDeletedSQL)

// Create inserts one activity entry. ON CONFLICT DO NOTHING makes a
// redelivered stream message idempotent, and makes the stream copy of a
// directly inserted comment a no-op.
func (r *ActivityRepository) Create(ctx context.Context, a *activitydom.Activity) error {
	content := a.Content
	if len(content) == 0 {
		content = json.RawMessage("{}")
	}
	updatedAt := a.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = a.CreatedAt
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO activities
			(id, project_id, entity_type, entity_id, actor_id, origin, activity_type, content, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO NOTHING`,
		a.ID.String(), a.ProjectID.String(), a.EntityType,
		uuidPtrString(a.EntityID), uuidPtrString(a.ActorID),
		a.Origin, a.ActivityType, content, a.CreatedAt, updatedAt,
	)
	return err
}

// List returns up to limit entries matching f, newest first.
func (r *ActivityRepository) List(ctx context.Context, f activitydom.ListFilter, limit int) ([]*activitydom.Activity, bool, error) {
	where := []string{"a.project_id = $1", "a.deleted_at IS NULL"}
	args := []any{f.ProjectID.String()}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	addIn := func(col string, vals []string) {
		if len(vals) == 0 {
			return
		}
		ph := make([]string, len(vals))
		for i, v := range vals {
			args = append(args, v)
			ph[i] = fmt.Sprintf("$%d", len(args))
		}
		where = append(where, col+" IN ("+strings.Join(ph, ", ")+")")
	}
	addIn("a.entity_type", f.EntityTypes)
	addIn("a.origin", f.Origins)
	addIn("a.activity_type", f.ActivityTypes)
	if len(f.ActorMemberIDs) > 0 {
		ids := make([]string, len(f.ActorMemberIDs))
		for i, id := range f.ActorMemberIDs {
			ids[i] = id.String()
		}
		addIn("a.actor_id", ids)
	}
	if f.CreatedAfter != nil {
		add("a.created_at >= $%d", *f.CreatedAfter)
	}
	if f.CreatedBefore != nil {
		add("a.created_at < $%d", *f.CreatedBefore)
	}
	if f.Search != "" {
		add("(("+activityTitleSQL+") ILIKE $%[1]d OR a.content::text ILIKE $%[1]d)", "%"+escapeLikePattern(f.Search)+"%")
	}
	if f.Cursor != nil {
		args = append(args, f.Cursor.CreatedAt, f.Cursor.ID.String())
		where = append(where, fmt.Sprintf("(a.created_at, a.id) < ($%d, $%d)", len(args)-1, len(args)))
	}
	args = append(args, limit+1)
	query := activitySelectSQL + " WHERE " + strings.Join(where, " AND ") +
		fmt.Sprintf(" ORDER BY a.created_at DESC, a.id DESC LIMIT $%d", len(args))

	var records []activityRecord
	if err := r.db.SelectContext(ctx, &records, query, args...); err != nil {
		return nil, false, err
	}
	hasMore := len(records) > limit
	if hasMore {
		records = records[:limit]
	}
	return activitiesFromRecords(records), hasMore, nil
}

// ListForEntity returns one entity's non-deleted entries, oldest first.
func (r *ActivityRepository) ListForEntity(ctx context.Context, entityType string, entityID uuid.UUID) ([]*activitydom.Activity, error) {
	var records []activityRecord
	err := r.db.SelectContext(ctx, &records, activitySelectSQL+`
		WHERE a.entity_type = $1 AND a.entity_id = $2 AND a.deleted_at IS NULL
		ORDER BY a.created_at ASC`, entityType, entityID.String())
	if err != nil {
		return nil, err
	}
	return activitiesFromRecords(records), nil
}

// FindByID returns a single entry, including a soft-deleted one.
func (r *ActivityRepository) FindByID(ctx context.Context, id uuid.UUID) (*activitydom.Activity, error) {
	var rec activityRecord
	err := r.db.GetContext(ctx, &rec, activitySelectSQL+` WHERE a.id = $1`, id.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, activitydom.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return activityFromRecord(rec), nil
}

// UpdateContent replaces an entry's content.
func (r *ActivityRepository) UpdateContent(ctx context.Context, id uuid.UUID, content json.RawMessage, updatedAt time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE activities SET content = $1, updated_at = $2 WHERE id = $3`,
		content, updatedAt, id.String())
	return err
}

// SoftDelete marks an entry deleted.
func (r *ActivityRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE activities SET deleted_at = $1 WHERE id = $2`, time.Now(), id.String())
	return err
}

func activitiesFromRecords(records []activityRecord) []*activitydom.Activity {
	out := make([]*activitydom.Activity, 0, len(records))
	for _, rec := range records {
		out = append(out, activityFromRecord(rec))
	}
	return out
}

func activityFromRecord(r activityRecord) *activitydom.Activity {
	a := &activitydom.Activity{
		ID:                  uuid.MustParse(r.ID),
		ProjectID:           uuid.MustParse(r.ProjectID),
		EntityType:          r.EntityType,
		EntityID:            parseUUIDPtr(r.EntityID),
		ActorID:             parseUUIDPtr(r.ActorID),
		Origin:              r.Origin,
		ActivityType:        r.ActivityType,
		Content:             json.RawMessage("{}"),
		CreatedAt:           r.CreatedAt,
		UpdatedAt:           r.UpdatedAt,
		DeletedAt:           r.DeletedAt,
		ActorAvatarKey:      r.ActorAvatarKey,
		ActorAvatarThumbKey: r.ActorAvatarThumbKey,
		EntityDeleted:       r.EntityDeleted,
	}
	if r.Content != nil {
		a.Content = *r.Content
	}
	if r.ActorFullName != nil {
		a.ActorName = *r.ActorFullName
	}
	if r.ActorUsername != nil {
		a.ActorUsername = *r.ActorUsername
	}
	if r.EntityTitle != nil {
		a.EntityTitle = *r.EntityTitle
	}
	return a
}

func uuidPtrString(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}

func parseUUIDPtr(s *string) *uuid.UUID {
	if s == nil || *s == "" {
		return nil
	}
	id, err := uuid.Parse(*s)
	if err != nil {
		return nil
	}
	return &id
}
