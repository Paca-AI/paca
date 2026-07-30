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
	ActorFullName *string `db:"actor_full_name"`
	ActorUsername *string `db:"actor_username"`
	TaskTitle     *string `db:"task_title"`
	TaskNumber    *int    `db:"task_number"`
	ProjectName   string  `db:"project_name"`
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
	u.full_name AS actor_full_name, u.username AS actor_username,
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

// ListForUser returns up to limit notifications for the given user, newest first.
func (r *NotificationRepository) ListForUser(ctx context.Context, userID uuid.UUID, limit int) ([]*notificationdom.Notification, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows []notificationReadRow
	err := r.db.SelectContext(ctx, &rows, `
		SELECT `+notificationReadCols+`
		FROM notifications n
		LEFT JOIN project_members pm ON pm.id = n.actor_member_id
		LEFT JOIN users u ON u.id = pm.user_id AND u.deleted_at IS NULL
		LEFT JOIN tasks t ON t.id = n.task_id AND t.deleted_at IS NULL
		JOIN projects p ON p.id = n.project_id
		WHERE n.recipient_user_id = $1
		ORDER BY n.created_at DESC
		LIMIT $2`, userID.String(), limit)
	if err != nil {
		return nil, fmt.Errorf("notification repo: list: %w", err)
	}

	out := make([]*notificationdom.Notification, 0, len(rows))
	for _, row := range rows {
		out = append(out, notificationFromRow(row))
	}
	return out, nil
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
