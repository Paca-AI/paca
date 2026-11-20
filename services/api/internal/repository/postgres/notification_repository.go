package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
)

// --- sqlx model -------------------------------------------------------------

// notificationReadRow is the result of the enriched SELECT … JOIN query.
type notificationReadRow struct {
	ID                 string     `db:"id"`
	RecipientAccountID string     `db:"recipient_account_id"`
	ActorMemberID      *string    `db:"actor_member_id"`
	Type               string     `db:"type"`
	TaskID             *string    `db:"task_id"`
	ProjectID          *string    `db:"project_id"`
	ConversationID     *string    `db:"conversation_id"`
	DocumentID         *string    `db:"document_id"`
	Message            *string    `db:"message"`
	CommentID          *string    `db:"comment_id"`
	MentionSource      *string    `db:"mention_source"`
	Excerpt            *string    `db:"excerpt"`
	ReadAt             *time.Time `db:"read_at"`
	CreatedAt          time.Time  `db:"created_at"`

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
	ActorAgentCLIProvider *string `db:"actor_agent_cli_provider"`
	TaskTitle             *string `db:"task_title"`
	TaskNumber            *int    `db:"task_number"`
	ProjectName           *string `db:"project_name"`
	ConversationTitle     *string `db:"conversation_title"`
	DocumentTitle         *string `db:"document_title"`
	TaskStatusName        *string `db:"task_status_name"`
	TaskStatusColor       *string `db:"task_status_color"`
	SprintID              *string `db:"sprint_id"`
	SprintName            *string `db:"sprint_name"`
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
	n.id, n.recipient_account_id, n.actor_member_id, n.type,
	n.task_id, n.project_id, n.conversation_id, n.document_id, n.message, n.comment_id, n.mention_source, n.excerpt, n.read_at, n.created_at,
	aac.name AS actor_full_name,
	COALESCE(u.username, ag.handle) AS actor_username,
	aac.avatar_key AS actor_avatar_key,
	aac.avatar_thumb_key AS actor_avatar_thumb_key,
	CASE aac.kind WHEN 'user' THEN 'human' ELSE COALESCE(aac.kind, '') END AS actor_member_type,
	COALESCE(ag.agent_type, '') AS actor_agent_type,
	COALESCE(ag.llm_provider, '') AS actor_agent_llm_provider,
	ag.acp_provider AS actor_agent_acp_provider,
	ag.cli_provider AS actor_agent_cli_provider,
	t.title AS task_title, t.task_number,
	p.name AS project_name,
	c.title AS conversation_title,
	d.title AS document_title,
	ts.name AS task_status_name, ts.color AS task_status_color,
	t.sprint_id AS sprint_id, sp.name AS sprint_name`

func notificationFromRow(r notificationReadRow) *notificationdom.Notification {
	n := &notificationdom.Notification{
		ID:                 uuid.MustParse(r.ID),
		RecipientAccountID: uuid.MustParse(r.RecipientAccountID),
		Type:               notificationdom.NotificationType(r.Type),
		ReadAt:             r.ReadAt,
		CreatedAt:          r.CreatedAt,
	}
	if r.ProjectID != nil {
		n.ProjectID = uuid.MustParse(*r.ProjectID)
	}
	if r.ProjectName != nil {
		n.ProjectName = *r.ProjectName
	}
	if r.ConversationID != nil {
		id := uuid.MustParse(*r.ConversationID)
		n.ConversationID = &id
	}
	if r.DocumentID != nil {
		id := uuid.MustParse(*r.DocumentID)
		n.DocumentID = &id
	}
	if r.DocumentTitle != nil {
		n.DocumentTitle = *r.DocumentTitle
	}
	if r.Message != nil {
		n.Message = *r.Message
	}
	if r.CommentID != nil {
		id := uuid.MustParse(*r.CommentID)
		n.CommentID = &id
	}
	if r.MentionSource != nil {
		n.MentionSource = notificationdom.MentionSource(*r.MentionSource)
	}
	if r.Excerpt != nil {
		n.Excerpt = *r.Excerpt
	}
	if r.TaskStatusName != nil {
		n.TaskStatusName = *r.TaskStatusName
	}
	if r.TaskStatusColor != nil {
		n.TaskStatusColor = *r.TaskStatusColor
	}
	if r.SprintID != nil {
		id := uuid.MustParse(*r.SprintID)
		n.SprintID = &id
	}
	if r.SprintName != nil {
		n.SprintName = *r.SprintName
	}
	if r.ConversationTitle != nil {
		n.ConversationTitle = *r.ConversationTitle
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
	n.ActorAgentCLIProvider = r.ActorAgentCLIProvider
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
	var conversationID *string
	if n.ConversationID != nil {
		s := n.ConversationID.String()
		conversationID = &s
	}
	var documentID *string
	if n.DocumentID != nil {
		s := n.DocumentID.String()
		documentID = &s
	}
	var commentID *string
	if n.CommentID != nil {
		s := n.CommentID.String()
		commentID = &s
	}
	var mentionSource, excerpt *string
	if n.MentionSource != "" {
		s := string(n.MentionSource)
		mentionSource = &s
	}
	if n.Excerpt != "" {
		excerpt = &n.Excerpt
	}
	var projectID *string
	if n.ProjectID != uuid.Nil {
		s := n.ProjectID.String()
		projectID = &s
	}
	var message *string
	if n.Message != "" {
		message = &n.Message
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO notifications (id, recipient_account_id, actor_member_id, type, task_id, project_id, conversation_id, document_id, message, comment_id, mention_source, excerpt, read_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		n.ID.String(), n.RecipientAccountID.String(), actorMemberID,
		string(n.Type), taskID, projectID, conversationID, documentID, message,
		commentID, mentionSource, excerpt, n.ReadAt, n.CreatedAt,
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
func (r *NotificationRepository) ListForUser(ctx context.Context, userID uuid.UUID, limit int, cursorAfter *string, filter notificationdom.ListFilter) ([]*notificationdom.Notification, bool, error) {
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
	if len(filter.Types) > 0 {
		holders := make([]string, len(filter.Types))
		for i, t := range filter.Types {
			args = append(args, string(t))
			holders[i] = fmt.Sprintf("$%d", len(args))
		}
		whereCursor += " AND n.type IN (" + strings.Join(holders, ", ") + ")"
	}
	if filter.ProjectID != nil {
		whereCursor += fmt.Sprintf(" AND n.project_id = $%d", len(args)+1)
		args = append(args, filter.ProjectID.String())
	}
	if filter.AgentActor {
		whereCursor += " AND aac.kind = 'agent'"
	}
	limitPlaceholder := fmt.Sprintf("$%d", len(args)+1)
	args = append(args, limit+1)

	var rows []notificationReadRow
	query := `
		SELECT ` + notificationReadCols + `
		FROM notifications n
		LEFT JOIN project_members pm ON pm.id = n.actor_member_id
		LEFT JOIN accounts aac ON aac.id = pm.account_id
		LEFT JOIN users u ON u.id = pm.account_id
		LEFT JOIN agents ag ON ag.id = pm.account_id
		LEFT JOIN tasks t ON t.id = n.task_id AND t.deleted_at IS NULL
		LEFT JOIN task_statuses ts ON ts.id = t.status_id
		LEFT JOIN sprints sp ON sp.id = t.sprint_id
		LEFT JOIN agent_conversations c ON c.id = n.conversation_id
		LEFT JOIN documents d ON d.id = n.document_id AND d.deleted_at IS NULL
		LEFT JOIN projects p ON p.id = n.project_id
		WHERE n.recipient_account_id = $1` + whereCursor + `
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
		SELECT COUNT(*) FROM notifications WHERE recipient_account_id = $1 AND read_at IS NULL`, userID.String())
	if err != nil {
		return 0, fmt.Errorf("notification repo: unread count: %w", err)
	}
	return count, nil
}

// MarkAsRead sets read_at on a notification owned by userID.
// Idempotent: calling it on an already-read notification succeeds as a no-op.
func (r *NotificationRepository) MarkAsRead(ctx context.Context, id, userID uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE notifications SET read_at = $1 WHERE id = $2 AND recipient_account_id = $3`,
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
		UPDATE notifications SET read_at = $1 WHERE recipient_account_id = $2 AND read_at IS NULL`,
		time.Now(), userID.String(),
	)
	if err != nil {
		return fmt.Errorf("notification repo: mark all as read: %w", err)
	}
	return nil
}

// UnreadMentionsByTask returns the user's unread mention notifications grouped
// by task (tasks that still exist), optionally for one project.
func (r *NotificationRepository) UnreadMentionsByTask(ctx context.Context, userID uuid.UUID, projectID *uuid.UUID) ([]notificationdom.UnreadMentions, error) {
	type row struct {
		ProjectID string  `db:"project_id"`
		TaskID    string  `db:"task_id"`
		SprintID  *string `db:"sprint_id"`
		Count     int64   `db:"count"`
	}
	args := []any{userID.String()}
	where := ""
	if projectID != nil {
		where = " AND n.project_id = $2"
		args = append(args, projectID.String())
	}
	var rows []row
	err := r.db.SelectContext(ctx, &rows, `
		SELECT n.project_id, n.task_id, t.sprint_id, COUNT(*) AS count
		FROM notifications n
		JOIN tasks t ON t.id = n.task_id AND t.deleted_at IS NULL
		WHERE n.recipient_account_id = $1 AND n.read_at IS NULL
		  AND n.type = 'mentioned' AND n.task_id IS NOT NULL`+where+`
		GROUP BY n.project_id, n.task_id, t.sprint_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("notification repo: unread mentions: %w", err)
	}
	out := make([]notificationdom.UnreadMentions, 0, len(rows))
	for _, x := range rows {
		m := notificationdom.UnreadMentions{
			ProjectID: uuid.MustParse(x.ProjectID),
			TaskID:    uuid.MustParse(x.TaskID),
			Count:     x.Count,
		}
		if x.SprintID != nil {
			id := uuid.MustParse(*x.SprintID)
			m.SprintID = &id
		}
		out = append(out, m)
	}
	return out, nil
}

// MarkTaskMentionsRead sets read_at on userID's unread mention notifications
// for one task.
func (r *NotificationRepository) MarkTaskMentionsRead(ctx context.Context, userID, taskID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE notifications SET read_at = $1
		WHERE recipient_account_id = $2 AND task_id = $3 AND type = 'mentioned' AND read_at IS NULL`,
		time.Now(), userID.String(), taskID.String(),
	)
	if err != nil {
		return fmt.Errorf("notification repo: mark task mentions read: %w", err)
	}
	return nil
}

// TaskLastSeen returns when userID last viewed the task, nil if never.
func (r *NotificationRepository) TaskLastSeen(ctx context.Context, userID, taskID uuid.UUID) (*time.Time, error) {
	var at time.Time
	err := r.db.GetContext(ctx, &at, `
		SELECT last_seen_at FROM task_reads WHERE account_id = $1 AND task_id = $2`,
		userID.String(), taskID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("notification repo: task last seen: %w", err)
	}
	return &at, nil
}

// TouchTaskRead upserts the task's last-seen time (never backwards) and
// returns the time it replaced, in one statement. The task must belong to
// projectID and not be deleted; otherwise nothing is written and
// taskdom.ErrTaskNotFound is returned. A data-modifying CTE shares the
// statement's snapshot, so prev is the value from before the upsert.
func (r *NotificationRepository) TouchTaskRead(ctx context.Context, userID, projectID, taskID uuid.UUID, at time.Time) (*time.Time, error) {
	var res struct {
		Found int        `db:"found"`
		Prev  *time.Time `db:"prev"`
	}
	err := r.db.GetContext(ctx, &res, `
		WITH t AS (
			SELECT id FROM tasks WHERE id = $2 AND project_id = $3 AND deleted_at IS NULL
		), prev AS (
			SELECT last_seen_at FROM task_reads WHERE account_id = $1 AND task_id = $2
		), up AS (
			INSERT INTO task_reads (account_id, task_id, last_seen_at)
			SELECT $1, t.id, $4 FROM t
			ON CONFLICT (account_id, task_id) DO UPDATE
			SET last_seen_at = GREATEST(task_reads.last_seen_at, EXCLUDED.last_seen_at)
		)
		SELECT (SELECT COUNT(*) FROM t) AS found, (SELECT last_seen_at FROM prev) AS prev`,
		userID.String(), taskID.String(), projectID.String(), at)
	if err != nil {
		return nil, fmt.Errorf("notification repo: touch task read: %w", err)
	}
	if res.Found == 0 {
		return nil, taskdom.ErrTaskNotFound
	}
	return res.Prev, nil
}
