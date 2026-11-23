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
	ID                 string     `db:"id"`
	RecipientAccountID string     `db:"recipient_account_id"`
	ActorMemberID      *string    `db:"actor_member_id"`
	Type               string     `db:"type"`
	TaskID             *string    `db:"task_id"`
	ProjectID          *string    `db:"project_id"`
	ConversationID     *string    `db:"conversation_id"`
	DocumentID         *string    `db:"document_id"`
	Message            *string    `db:"message"`
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
	n.task_id, n.project_id, n.conversation_id, n.document_id, n.message, n.read_at, n.created_at,
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
	d.title AS document_title`

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
	var projectID *string
	if n.ProjectID != uuid.Nil {
		s := n.ProjectID.String()
		projectID = &s
	}
	var message *string
	if n.Message != "" {
		message = &n.Message
	}
	// The notification and its realtime delivery are written together: a
	// notification must never exist without the delivery that announces it, or
	// a lost announcement could not be retried.
	err := WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO notifications (id, recipient_account_id, actor_member_id, type, task_id, project_id, conversation_id, document_id, message, read_at, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			n.ID.String(), n.RecipientAccountID.String(), actorMemberID,
			string(n.Type), taskID, projectID, conversationID, documentID, message, n.ReadAt, n.CreatedAt,
		); err != nil {
			return err
		}
		// Not due immediately: the service publishes right after this commits and
		// marks the delivery sent, so the dispatcher only needs to step in when
		// that did not happen. The grace keeps it from racing the fast path into
		// a duplicate chime.
		_, err := tx.ExecContext(ctx, `
			INSERT INTO notification_deliveries (notification_id, channel, next_attempt_at)
			VALUES ($1, $2, $3)`,
			n.ID.String(), string(notificationdom.DeliveryChannelRealtime), time.Now().Add(realtimeDeliveryGrace),
		)
		if err != nil {
			return err
		}
		// A browser push only when there is somewhere to send it and the
		// recipient has not switched this type off — decided here, in the same
		// transaction, so a user with no push subscriptions costs no row. Due
		// at once: unlike the realtime announcement it has no fast path to wait
		// for, and the service worker itself stays quiet when the app is open.
		_, err = tx.ExecContext(ctx, `
			INSERT INTO notification_deliveries (notification_id, channel, next_attempt_at)
			SELECT $1, $2, $3
			WHERE EXISTS (SELECT 1 FROM push_subscriptions WHERE account_id = $4)
			  AND NOT EXISTS (
			      SELECT 1 FROM notification_preferences
			      WHERE account_id = $4 AND type = $5 AND channel = 'push' AND enabled = FALSE)`,
			n.ID.String(), string(notificationdom.DeliveryChannelPush), time.Now(),
			n.RecipientAccountID.String(), string(n.Type),
		)
		return err
	})
	if err != nil {
		return fmt.Errorf("notification repo: create: %w", err)
	}
	return nil
}

// realtimeDeliveryGrace is how long after creation the dispatcher first looks
// at a notification's realtime delivery — long enough for the creating
// request's own publish to land and mark it sent.
const realtimeDeliveryGrace = 30 * time.Second

// MarkDelivered marks a pending delivery of the notification sent.
func (r *NotificationRepository) MarkDelivered(ctx context.Context, notificationID uuid.UUID, channel notificationdom.DeliveryChannel) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE notification_deliveries
		SET status = 'sent', sent_at = NOW(), updated_at = NOW(), last_error = NULL
		WHERE notification_id = $1 AND channel = $2 AND status = 'pending'`,
		notificationID.String(), string(channel),
	)
	if err != nil {
		return fmt.Errorf("notification repo: mark delivered: %w", err)
	}
	return nil
}

var _ notificationdom.DeliveryRepository = (*NotificationRepository)(nil)

type pendingDeliveryRow struct {
	ID                 string    `db:"id"`
	Channel            string    `db:"channel"`
	Attempts           int       `db:"attempts"`
	NotificationID     string    `db:"notification_id"`
	RecipientAccountID string    `db:"recipient_account_id"`
	ActorMemberID      *string   `db:"actor_member_id"`
	Type               string    `db:"type"`
	TaskID             *string   `db:"task_id"`
	ConversationID     *string   `db:"conversation_id"`
	DocumentID         *string   `db:"document_id"`
	ProjectID          *string   `db:"project_id"`
	CreatedAt          time.Time `db:"created_at"`

	// Display fields, for channels that render the notification as text.
	ActorFullName     *string `db:"actor_full_name"`
	TaskTitle         *string `db:"task_title"`
	TaskNumber        *int    `db:"task_number"`
	ProjectName       *string `db:"project_name"`
	ConversationTitle *string `db:"conversation_title"`
	DocumentTitle     *string `db:"document_title"`
	Message           *string `db:"message"`
}

func optionalUUID(s *string) *uuid.UUID {
	if s == nil {
		return nil
	}
	id, err := uuid.Parse(*s)
	if err != nil {
		return nil
	}
	return &id
}

// ClaimDue locks due pending deliveries of channel (FOR UPDATE SKIP LOCKED, so
// replicas claim different rows), hands them to deliver and stores its
// outcomes, all in one transaction. The transaction stays open while deliver
// runs, which is what keeps another replica off the same rows; deliver must
// therefore be quick (a Valkey publish, not an HTTP call).
func (r *NotificationRepository) ClaimDue(ctx context.Context, channel notificationdom.DeliveryChannel, now time.Time, limit int, deliver func([]notificationdom.PendingDelivery) []notificationdom.DeliveryOutcome) (int, error) {
	claimed := 0
	err := WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		var rows []pendingDeliveryRow
		if err := tx.SelectContext(ctx, &rows, `
			SELECT d.id, d.channel, d.attempts, d.notification_id,
			       n.recipient_account_id, n.actor_member_id, n.type, n.task_id,
			       n.conversation_id, n.document_id, n.project_id, n.created_at, n.message,
			       aac.name AS actor_full_name, t.title AS task_title, t.task_number,
			       p.name AS project_name, c.title AS conversation_title, doc.title AS document_title
			FROM notification_deliveries d
			JOIN notifications n ON n.id = d.notification_id
			LEFT JOIN project_members pm ON pm.id = n.actor_member_id
			LEFT JOIN accounts aac ON aac.id = pm.account_id
			LEFT JOIN tasks t ON t.id = n.task_id AND t.deleted_at IS NULL
			LEFT JOIN agent_conversations c ON c.id = n.conversation_id
			LEFT JOIN documents doc ON doc.id = n.document_id AND doc.deleted_at IS NULL
			LEFT JOIN projects p ON p.id = n.project_id
			WHERE d.status = 'pending' AND d.channel = $1 AND d.next_attempt_at <= $2
			ORDER BY d.next_attempt_at
			LIMIT $3
			FOR UPDATE OF d SKIP LOCKED`, string(channel), now, limit); err != nil {
			return fmt.Errorf("claim: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}
		pending := make([]notificationdom.PendingDelivery, 0, len(rows))
		for _, row := range rows {
			id, err := uuid.Parse(row.ID)
			if err != nil {
				continue
			}
			n := notificationdom.Notification{
				RecipientAccountID: uuid.MustParse(row.RecipientAccountID),
				Type:               notificationdom.NotificationType(row.Type),
				ActorMemberID:      optionalUUID(row.ActorMemberID),
				TaskID:             optionalUUID(row.TaskID),
				ConversationID:     optionalUUID(row.ConversationID),
				DocumentID:         optionalUUID(row.DocumentID),
				CreatedAt:          row.CreatedAt,
			}
			n.ID = uuid.MustParse(row.NotificationID)
			if row.ActorFullName != nil {
				n.ActorFullName = *row.ActorFullName
			}
			if row.TaskTitle != nil {
				n.TaskTitle = *row.TaskTitle
			}
			if row.TaskNumber != nil {
				n.TaskNumber = *row.TaskNumber
			}
			if row.ProjectName != nil {
				n.ProjectName = *row.ProjectName
			}
			if row.ConversationTitle != nil {
				n.ConversationTitle = *row.ConversationTitle
			}
			if row.DocumentTitle != nil {
				n.DocumentTitle = *row.DocumentTitle
			}
			if row.Message != nil {
				n.Message = *row.Message
			}
			if p := optionalUUID(row.ProjectID); p != nil {
				n.ProjectID = *p
			}
			pending = append(pending, notificationdom.PendingDelivery{
				ID: id, Channel: notificationdom.DeliveryChannel(row.Channel), Attempts: row.Attempts, Notification: n,
			})
		}
		claimed = len(pending)

		for _, o := range deliver(pending) {
			if err := applyDeliveryOutcome(ctx, tx, o); err != nil {
				return err
			}
		}
		return nil
	})
	return claimed, err
}

func applyDeliveryOutcome(ctx context.Context, tx *sqlx.Tx, o notificationdom.DeliveryOutcome) error {
	if o.Err == nil {
		_, err := tx.ExecContext(ctx, `
			UPDATE notification_deliveries
			SET status = 'sent', attempts = attempts + 1, sent_at = NOW(), updated_at = NOW(), last_error = NULL
			WHERE id = $1`, o.ID.String())
		return err
	}
	status := "pending"
	if o.Failed {
		status = "failed"
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE notification_deliveries
		SET status = $2, attempts = attempts + 1, next_attempt_at = $3, last_error = $4, updated_at = NOW()
		WHERE id = $1`, o.ID.String(), status, o.NextAttemptAt, o.Err.Error())
	return err
}

// Prune deletes finished deliveries older than the given cutoffs.
func (r *NotificationRepository) Prune(ctx context.Context, sentBefore, failedBefore time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM notification_deliveries
		WHERE (status = 'sent' AND updated_at < $1)
		   OR (status = 'failed' AND updated_at < $2)`, sentBefore, failedBefore)
	if err != nil {
		return 0, fmt.Errorf("notification repo: prune deliveries: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
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
		LEFT JOIN accounts aac ON aac.id = pm.account_id
		LEFT JOIN users u ON u.id = pm.account_id
		LEFT JOIN agents ag ON ag.id = pm.account_id
		LEFT JOIN tasks t ON t.id = n.task_id AND t.deleted_at IS NULL
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

// DeliveryStats reads the delivery outbox's health, one entry per channel.
func (r *NotificationRepository) DeliveryStats(ctx context.Context, now time.Time) ([]notificationdom.ChannelStats, error) {
	byChannel := map[string]*notificationdom.ChannelStats{}
	get := func(ch string) *notificationdom.ChannelStats {
		if byChannel[ch] == nil {
			byChannel[ch] = &notificationdom.ChannelStats{Channel: notificationdom.DeliveryChannel(ch)}
		}
		return byChannel[ch]
	}
	hourAgo := now.Add(-time.Hour)
	overdueBefore := now.Add(-2 * time.Minute)

	var totals []struct {
		Channel  string     `db:"channel"`
		Status   string     `db:"status"`
		Count    int64      `db:"n"`
		Retrying int64      `db:"retrying"`
		Overdue  int64      `db:"overdue"`
		Oldest   *time.Time `db:"oldest"`
		LastHour int64      `db:"last_hour"`
	}
	if err := r.db.SelectContext(ctx, &totals, `
		SELECT channel, status, COUNT(*) AS n,
		       COUNT(*) FILTER (WHERE status = 'pending' AND attempts > 0) AS retrying,
		       COUNT(*) FILTER (WHERE status = 'pending' AND next_attempt_at < $2) AS overdue,
		       MIN(next_attempt_at) FILTER (WHERE status = 'pending' AND next_attempt_at < $2) AS oldest,
		       COUNT(*) FILTER (WHERE updated_at >= $1) AS last_hour
		FROM notification_deliveries
		GROUP BY channel, status`, hourAgo, overdueBefore); err != nil {
		return nil, fmt.Errorf("notification repo: delivery totals: %w", err)
	}
	for _, t := range totals {
		c := get(t.Channel)
		switch t.Status {
		case "pending":
			c.Pending, c.Retrying, c.Overdue, c.OldestOverdue = t.Count, t.Retrying, t.Overdue, t.Oldest
		case "sent":
			c.Sent, c.SentLastHour = t.Count, t.LastHour
		case "failed":
			c.Failed, c.FailedLastHour = t.Count, t.LastHour
		}
	}

	var latency []struct {
		Channel string   `db:"channel"`
		P50     *float64 `db:"p50"`
		P95     *float64 `db:"p95"`
	}
	if err := r.db.SelectContext(ctx, &latency, `
		SELECT d.channel,
		       percentile_cont(0.5) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (d.sent_at - n.created_at))) AS p50,
		       percentile_cont(0.95) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (d.sent_at - n.created_at))) AS p95
		FROM notification_deliveries d
		JOIN notifications n ON n.id = d.notification_id
		WHERE d.status = 'sent' AND d.sent_at >= $1
		GROUP BY d.channel`, hourAgo); err != nil {
		return nil, fmt.Errorf("notification repo: delivery latency: %w", err)
	}
	for _, l := range latency {
		c := get(l.Channel)
		c.LatencyP50, c.LatencyP95 = l.P50, l.P95
	}

	var errs []struct {
		Channel string `db:"channel"`
		Error   string `db:"error"`
		Count   int64  `db:"n"`
	}
	if err := r.db.SelectContext(ctx, &errs, `
		SELECT channel, COALESCE(last_error, '') AS error, COUNT(*) AS n
		FROM notification_deliveries
		WHERE status = 'failed' AND updated_at >= $1
		GROUP BY channel, last_error
		ORDER BY n DESC`, hourAgo); err != nil {
		return nil, fmt.Errorf("notification repo: delivery errors: %w", err)
	}
	for _, e := range errs {
		c := get(e.Channel)
		if len(c.TopErrors) < 3 {
			c.TopErrors = append(c.TopErrors, notificationdom.ErrorCount{Error: e.Error, Count: e.Count})
		}
	}

	out := make([]notificationdom.ChannelStats, 0, len(byChannel))
	for _, ch := range []string{"realtime", "push", "email"} {
		if c := byChannel[ch]; c != nil {
			out = append(out, *c)
		}
	}
	return out, nil
}

var _ notificationdom.DeliveryStatsRepository = (*NotificationRepository)(nil)
