package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
)

// --- sqlx model -------------------------------------------------------------

// notificationReadRow is the result of the enriched SELECT … JOIN query.
type notificationReadRow struct {
	ID              string     `db:"id"`
	RecipientUserID string     `db:"recipient_user_id"`
	ActorMemberID   *string    `db:"actor_member_id"`
	Type            string     `db:"type"`
	TaskID          *string    `db:"task_id"`
	ProjectID       string     `db:"project_id"`
	ReadAt          *time.Time `db:"read_at"`
	CreatedAt       time.Time  `db:"created_at"`

	// Joined fields.
	ActorFullName       *string `db:"actor_full_name"`
	ActorUsername       *string `db:"actor_username"`
	ActorAvatarKey      *string `db:"actor_avatar_key"`
	ActorAvatarThumbKey *string `db:"actor_avatar_thumb_key"`
	// ActorMemberType/ActorAgentType/ActorAgentLLMProvider/ActorAgentACPProvider
	// let the frontend pick a default provider-logo avatar for agent actors
	// with no custom avatar uploaded — see projectMemberCols for the sibling
	// query this mirrors.
	ActorMemberType       string  `db:"actor_member_type"`
	ActorAgentType        string  `db:"actor_agent_type"`
	ActorAgentLLMProvider string  `db:"actor_agent_llm_provider"`
	ActorAgentACPProvider *string `db:"actor_agent_acp_provider"`
	TaskTitle             *string `db:"task_title"`
	TaskNumber            *int    `db:"task_number"`
	ProjectName           string  `db:"project_name"`
}

// --- Repository struct -------------------------------------------------------

// NotificationRepository implements notificationdom.Repository.
type NotificationRepository struct {
	db *sqlx.DB
}

// NewNotificationRepository returns a new NotificationRepository backed by db.
func NewNotificationRepository(db *sqlx.DB) *NotificationRepository {
	return &NotificationRepository{db: db}
}

// --- Helpers ----------------------------------------------------------------

const notificationReadCols = `
	n.id, n.recipient_user_id, n.actor_member_id, n.type,
	n.task_id, n.project_id, n.read_at, n.created_at,
	COALESCE(u.full_name, ag.name) AS actor_full_name,
	COALESCE(u.username, ag.handle) AS actor_username,
	COALESCE(u.avatar_key, ag.avatar_key) AS actor_avatar_key,
	COALESCE(u.avatar_thumb_key, ag.avatar_thumb_key) AS actor_avatar_thumb_key,
	COALESCE(pm.member_type, '') AS actor_member_type,
	COALESCE(ag.agent_type, '') AS actor_agent_type,
	COALESCE(ag.llm_provider, '') AS actor_agent_llm_provider,
	ag.acp_provider AS actor_agent_acp_provider,
	t.title AS task_title, t.task_number,
	p.name AS project_name`

func notificationFromRow(r notificationReadRow) *notificationdom.Notification {
	n := &notificationdom.Notification{
		ID:              uuid.MustParse(r.ID),
		RecipientUserID: uuid.MustParse(r.RecipientUserID),
		Type:            notificationdom.NotificationType(r.Type),
		ProjectID:       uuid.MustParse(r.ProjectID),
		ProjectName:     r.ProjectName,
		ReadAt:          r.ReadAt,
		CreatedAt:       r.CreatedAt,
	}
	if r.ActorMemberID != nil {
		id := uuid.MustParse(*r.ActorMemberID)
		n.ActorMemberID = &id
	}
	if r.ActorFullName != nil {
		n.ActorFullName = *r.ActorFullName
	}
	if r.ActorUsername != nil {
		n.ActorUsername = *r.ActorUsername
	}
	n.ActorAvatarKey = r.ActorAvatarKey
	n.ActorAvatarThumbKey = r.ActorAvatarThumbKey
	n.ActorMemberType = r.ActorMemberType
	n.ActorAgentType = r.ActorAgentType
	n.ActorAgentLLMProvider = r.ActorAgentLLMProvider
	n.ActorAgentACPProvider = r.ActorAgentACPProvider
	if r.TaskID != nil {
		id := uuid.MustParse(*r.TaskID)
		n.TaskID = &id
	}
	if r.TaskTitle != nil {
		n.TaskTitle = *r.TaskTitle
	}
	if r.TaskNumber != nil {
		n.TaskNumber = *r.TaskNumber
	}
	return n
}

// --- Repository methods -----------------------------------------------------

// Create persists a new notification.
func (r *NotificationRepository) Create(ctx context.Context, n *notificationdom.Notification) error {
	var actorMemberID *string
	if n.ActorMemberID != nil {
		s := n.ActorMemberID.String()
		actorMemberID = &s
	}
	var taskID *string
	if n.TaskID != nil {
		s := n.TaskID.String()
		taskID = &s
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO notifications (id, recipient_user_id, actor_member_id, type, task_id, project_id, read_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		n.ID.String(), n.RecipientUserID.String(), actorMemberID,
		string(n.Type), taskID, n.ProjectID.String(), n.ReadAt, n.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("notification repo: create: %w", err)
	}
	return nil
}

// ListForUser returns up to limit notifications for the given user, newest
// first (created_at DESC, id DESC), keyset-paginated via cursorAfter. It
// fetches one row beyond limit to detect whether more pages remain, without
// a separate COUNT query — same approach as AgentRepository.ListConversations.
func (r *NotificationRepository) ListForUser(ctx context.Context, userID uuid.UUID, limit int, cursorAfter *string) ([]*notificationdom.Notification, bool, error) {
	if limit <= 0 {
		limit = 50
	}

	// userID is always $1; the cursor pair (if present) is $2/$3, and the
	// limit placeholder is numbered last so it works whether or not the
	// cursor clause is present.
	args := []any{userID.String()}
	whereCursor := ""
	if cursorAfter != nil {
		cur, err := notificationdom.DecodeNotificationCursor(*cursorAfter)
		if err != nil {
			return nil, false, fmt.Errorf("%w: %s", notificationdom.ErrInvalidCursor, err)
		}
		whereCursor = fmt.Sprintf(" AND (n.created_at, n.id) < ($%d, $%d)", len(args)+1, len(args)+2)
		args = append(args, cur.CreatedAt, cur.ID)
	}
	limitPlaceholder := fmt.Sprintf("$%d", len(args)+1)
	args = append(args, limit+1)

	var rows []notificationReadRow
	query := `
		SELECT ` + notificationReadCols + `
		FROM notifications n
		LEFT JOIN project_members pm ON pm.id = n.actor_member_id
		LEFT JOIN users u ON u.id = pm.user_id AND u.deleted_at IS NULL
		LEFT JOIN agents ag ON ag.id = pm.agent_id
		LEFT JOIN tasks t ON t.id = n.task_id AND t.deleted_at IS NULL
		JOIN projects p ON p.id = n.project_id
		WHERE n.recipient_user_id = $1` + whereCursor + `
		ORDER BY n.created_at DESC, n.id DESC
		LIMIT ` + limitPlaceholder
	if err := r.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, false, fmt.Errorf("notification repo: list: %w", err)
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	out := make([]*notificationdom.Notification, 0, len(rows))
	for _, row := range rows {
		out = append(out, notificationFromRow(row))
	}
	return out, hasMore, nil
}

// UnreadCount returns the count of unread notifications for the given user.
func (r *NotificationRepository) UnreadCount(ctx context.Context, userID uuid.UUID) (int64, error) {
	var count int64
	err := r.db.GetContext(ctx, &count, `
		SELECT COUNT(*) FROM notifications WHERE recipient_user_id = $1 AND read_at IS NULL`, userID.String())
	if err != nil {
		return 0, fmt.Errorf("notification repo: unread count: %w", err)
	}
	return count, nil
}

// MarkAsRead sets read_at on a notification owned by userID.
// Idempotent: calling it on an already-read notification succeeds as a no-op.
func (r *NotificationRepository) MarkAsRead(ctx context.Context, id, userID uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE notifications SET read_at = $1 WHERE id = $2 AND recipient_user_id = $3`,
		time.Now(), id.String(), userID.String(),
	)
	if err != nil {
		return fmt.Errorf("notification repo: mark as read: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return notificationdom.ErrNotificationNotFound
	}
	return nil
}

// MarkAllAsRead sets read_at on all unread notifications for userID.
func (r *NotificationRepository) MarkAllAsRead(ctx context.Context, userID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE notifications SET read_at = $1 WHERE recipient_user_id = $2 AND read_at IS NULL`,
		time.Now(), userID.String(),
	)
	if err != nil {
		return fmt.Errorf("notification repo: mark all as read: %w", err)
	}
	return nil
}
