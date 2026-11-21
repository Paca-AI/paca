package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	chatdom "github.com/Paca-AI/api/internal/domain/chat"
)

// ChatRepository implements chatdom.Repository on Postgres.
type ChatRepository struct{ db *sqlx.DB }

// NewChatRepository returns a ChatRepository.
func NewChatRepository(db *sqlx.DB) *ChatRepository { return &ChatRepository{db: db} }

var _ chatdom.Repository = (*ChatRepository)(nil)

// channelScopeColumns is the SQL behind the condition attributes of a channel;
// keep in step with the "channel" loader (chatLoaders in iam_attribute_loaders.go).
var channelScopeColumns = scopeColumns{
	"resource.id":        {scalar: "c.id"},
	"channel.kind":       {scalar: "c.kind"},
	"channel.member_ids": {multi: `ARRAY(SELECT cm.account_id::text FROM channel_members cm WHERE cm.channel_id = c.id)`},
}

type channelRecord struct {
	ID           string     `db:"id"`
	Scope        string     `db:"scope"`
	ProjectID    *string    `db:"project_id"`
	Kind         string     `db:"kind"`
	Name         string     `db:"name"`
	Topic        string     `db:"topic"`
	LastSeq      int64      `db:"last_seq"`
	ArchivedAt   *time.Time `db:"archived_at"`
	CreatedBy    *string    `db:"created_by"`
	CreatedAt    time.Time  `db:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at"`
	IsMember     bool       `db:"is_member"`
	LastReadSeq  int64      `db:"last_read_seq"`
	PeerID       *string    `db:"peer_id"`
	PeerName     *string    `db:"peer_name"`
	PeerKind     *string    `db:"peer_kind"`
	MentionCount int64      `db:"mention_count"`
	Starred      bool       `db:"starred"`
}

func (r channelRecord) entity() *chatdom.Channel {
	c := &chatdom.Channel{
		ID: uuid.MustParse(r.ID), Scope: r.Scope, Kind: r.Kind, Name: r.Name, Topic: r.Topic,
		LastSeq: r.LastSeq, ArchivedAt: r.ArchivedAt, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		IsMember: r.IsMember, LastReadSeq: r.LastReadSeq, MentionCount: r.MentionCount, Starred: r.Starred,
	}
	if r.ProjectID != nil {
		id := uuid.MustParse(*r.ProjectID)
		c.ProjectID = &id
	}
	if r.CreatedBy != nil {
		id := uuid.MustParse(*r.CreatedBy)
		c.CreatedBy = &id
	}
	if r.PeerID != nil && r.PeerName != nil && r.PeerKind != nil {
		c.Peer = &chatdom.DMPeer{AccountID: uuid.MustParse(*r.PeerID), Name: *r.PeerName, Kind: *r.PeerKind}
	}
	// Only a joined channel has a read pointer; posting moves the author's own.
	if u := r.LastSeq - r.LastReadSeq; r.IsMember && u > 0 {
		c.UnreadCount = u
	}
	return c
}

// channelCols selects a channel with the caller's read state; $1 is the caller.
const channelCols = `c.id::text, c.scope, c.project_id::text, c.kind, c.name, c.topic, c.last_seq, c.archived_at,
	c.created_by::text, c.created_at, c.updated_at,
	(cm.account_id IS NOT NULL) AS is_member, COALESCE(cm.last_read_seq, 0) AS last_read_seq,
	peer.id::text AS peer_id, peer.name AS peer_name, peer.kind AS peer_kind,
	CASE WHEN cm.account_id IS NULL THEN 0 ELSE (SELECT COUNT(*) FROM message_mentions mm JOIN messages mmm ON mmm.id = mm.message_id
		WHERE mm.account_id = $1::uuid AND mmm.channel_id = c.id AND mmm.deleted_at IS NULL AND mmm.seq > cm.last_read_seq) END AS mention_count,
	COALESCE(cm.starred, FALSE) AS starred`

// channelFrom joins the caller's read state ($1 is the caller) and, for a
// direct message, the other party.
const channelFrom = ` FROM channels c LEFT JOIN channel_members cm ON cm.channel_id = c.id AND cm.account_id = $1::uuid
	LEFT JOIN LATERAL (SELECT a.id, a.name, a.kind FROM channel_members pm JOIN accounts a ON a.id = pm.account_id
		WHERE c.kind = 'dm' AND pm.channel_id = c.id AND pm.account_id <> $1::uuid LIMIT 1) peer ON TRUE`

// projectArg is a project id as a query argument: nil for uuid.Nil, which
// names the workspace scope (channels with no project).
func projectArg(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id.String()
}

// ListChannels implements chatdom.Repository.
func (r *ChatRepository) ListChannels(ctx context.Context, projectID, accountID uuid.UUID, f chatdom.ChannelFilter) ([]*chatdom.Channel, error) {
	sink := &argList{args: []any{accountID.String(), projectID.String()}}
	clause, none, err := scopeSQL(ctx, "channel", channelScopeColumns, sink)
	if err != nil {
		return nil, err
	}
	if none {
		return []*chatdom.Channel{}, nil
	}
	q := `SELECT ` + channelCols + channelFrom + ` WHERE c.project_id = $2::uuid`
	if !f.IncludeArchived {
		q += ` AND c.archived_at IS NULL`
	}
	if clause != "" {
		q += ` AND ` + clause
	}
	q += ` ORDER BY lower(c.name), c.id`
	var recs []channelRecord
	if err := r.db.SelectContext(ctx, &recs, q, sink.args...); err != nil {
		return nil, fmt.Errorf("chat: list channels: %w", err)
	}
	out := make([]*chatdom.Channel, len(recs))
	for i, rec := range recs {
		out[i] = rec.entity()
	}
	return out, nil
}

// FindChannel implements chatdom.Repository.
func (r *ChatRepository) FindChannel(ctx context.Context, projectID, channelID, accountID uuid.UUID) (*chatdom.Channel, error) {
	var rec channelRecord
	err := r.db.GetContext(ctx, &rec, `SELECT `+channelCols+channelFrom+` WHERE c.id = $2::uuid AND c.project_id IS NOT DISTINCT FROM $3::uuid`,
		accountID.String(), channelID.String(), projectArg(projectID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, chatdom.ErrChannelNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("chat: find channel: %w", err)
	}
	return rec.entity(), nil
}

// CreateChannel implements chatdom.Repository. c.ID is generated when unset.
func (r *ChatRepository) CreateChannel(ctx context.Context, c *chatdom.Channel, members []uuid.UUID) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		var createdBy any
		if c.CreatedBy != nil {
			createdBy = c.CreatedBy.String()
		}
		err := tx.QueryRowxContext(ctx, `INSERT INTO channels (id, scope, project_id, kind, name, topic, created_by)
			VALUES ($1::uuid, $2, $3::uuid, $4, $5, $6, $7::uuid) RETURNING created_at, updated_at`,
			c.ID.String(), c.Scope, c.ProjectID.String(), c.Kind, c.Name, c.Topic, createdBy).Scan(&c.CreatedAt, &c.UpdatedAt)
		if err != nil {
			return mapChannelErr(err)
		}
		for _, m := range members {
			if _, err := tx.ExecContext(ctx, `INSERT INTO channel_members (channel_id, account_id) VALUES ($1::uuid, $2::uuid)
				ON CONFLICT DO NOTHING`, c.ID.String(), m.String()); err != nil {
				return fmt.Errorf("chat: add member: %w", err)
			}
		}
		return nil
	})
}

func mapChannelErr(err error) error {
	if isUniqueViolation(err) {
		return chatdom.ErrChannelNameTaken
	}
	return fmt.Errorf("chat: %w", err)
}

// UpdateChannel implements chatdom.Repository.
func (r *ChatRepository) UpdateChannel(ctx context.Context, projectID, channelID uuid.UUID, name, topic string) error {
	res, err := r.db.ExecContext(ctx, `UPDATE channels SET name = $3, topic = $4, updated_at = NOW()
		WHERE id = $1::uuid AND project_id = $2::uuid`, channelID.String(), projectID.String(), name, topic)
	if err != nil {
		return mapChannelErr(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return chatdom.ErrChannelNotFound
	}
	return nil
}

// SetArchived implements chatdom.Repository.
func (r *ChatRepository) SetArchived(ctx context.Context, projectID, channelID uuid.UUID, archived bool) error {
	q := `UPDATE channels SET archived_at = NULL, updated_at = NOW() WHERE id = $1::uuid AND project_id = $2::uuid`
	if archived {
		q = `UPDATE channels SET archived_at = COALESCE(archived_at, NOW()), updated_at = NOW() WHERE id = $1::uuid AND project_id = $2::uuid`
	}
	res, err := r.db.ExecContext(ctx, q, channelID.String(), projectID.String())
	if err != nil {
		return mapChannelErr(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return chatdom.ErrChannelNotFound
	}
	return nil
}

// EnsureGeneral implements chatdom.Repository.
func (r *ChatRepository) EnsureGeneral(ctx context.Context, projectID uuid.UUID, createdBy *uuid.UUID) error {
	var by any
	if createdBy != nil {
		by = createdBy.String()
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO channels (scope, project_id, kind, name, created_by)
		VALUES ('project', $1::uuid, 'public', $2, $3::uuid) ON CONFLICT DO NOTHING`, projectID.String(), chatdom.GeneralChannelName, by)
	if err != nil {
		return fmt.Errorf("chat: ensure general: %w", err)
	}
	return nil
}

// AddMember implements chatdom.Repository.
func (r *ChatRepository) AddMember(ctx context.Context, channelID, accountID uuid.UUID) error {
	// A new member starts at the channel head: history is not "unread".
	_, err := r.db.ExecContext(ctx, `INSERT INTO channel_members (channel_id, account_id, last_read_seq)
		SELECT id, $2::uuid, last_seq FROM channels WHERE id = $1::uuid ON CONFLICT DO NOTHING`, channelID.String(), accountID.String())
	if err != nil {
		return fmt.Errorf("chat: add member: %w", err)
	}
	return nil
}

// RemoveMember implements chatdom.Repository.
func (r *ChatRepository) RemoveMember(ctx context.Context, channelID, accountID uuid.UUID) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM channel_members WHERE channel_id = $1::uuid AND account_id = $2::uuid`,
		channelID.String(), accountID.String()); err != nil {
		return fmt.Errorf("chat: remove member: %w", err)
	}
	return nil
}

// IsProjectMember implements chatdom.Repository.
func (r *ChatRepository) IsProjectMember(ctx context.Context, projectID, accountID uuid.UUID) (bool, error) {
	var ok bool
	err := r.db.GetContext(ctx, &ok, `SELECT EXISTS (SELECT 1 FROM project_members
		WHERE project_id = $1::uuid AND account_id = $2::uuid AND deleted_at IS NULL)`, projectID.String(), accountID.String())
	if err != nil {
		return false, fmt.Errorf("chat: project member: %w", err)
	}
	return ok, nil
}

// SetNotificationPref implements chatdom.Repository.
func (r *ChatRepository) SetNotificationPref(ctx context.Context, channelID, accountID uuid.UUID, pref string) error {
	res, err := r.db.ExecContext(ctx, `UPDATE channel_members SET notification_pref = $3 WHERE channel_id = $1::uuid AND account_id = $2::uuid`,
		channelID.String(), accountID.String(), pref)
	if err != nil {
		return fmt.Errorf("chat: set pref: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return chatdom.ErrNotAMember
	}
	return nil
}

type messageRecord struct {
	ID           string          `db:"id"`
	ChannelID    string          `db:"channel_id"`
	Seq          int64           `db:"seq"`
	ThreadRootID *string         `db:"thread_root_id"`
	AuthorID     string          `db:"author_account_id"`
	AuthorName   string          `db:"author_name"`
	Content      json.RawMessage `db:"content"`
	ContentText  string          `db:"content_text"`
	EditedAt     *time.Time      `db:"edited_at"`
	DeletedAt    *time.Time      `db:"deleted_at"`
	CreatedAt    time.Time       `db:"created_at"`
	ReplyCount   int64           `db:"reply_count"`
	RunID        *string         `db:"run_id"`
}

const messageCols = `m.id::text, m.channel_id::text, m.seq, m.thread_root_id::text, m.author_account_id::text,
	COALESCE(a.name, '') AS author_name, m.content, m.content_text, m.edited_at, m.deleted_at, m.created_at,
	(SELECT COUNT(*) FROM messages r WHERE r.thread_root_id = m.id AND r.deleted_at IS NULL) AS reply_count,
	m.run_id::text AS run_id`

func (m messageRecord) entity() *chatdom.Message {
	e := &chatdom.Message{
		ID: uuid.MustParse(m.ID), ChannelID: uuid.MustParse(m.ChannelID), Seq: m.Seq,
		AuthorID: uuid.MustParse(m.AuthorID), AuthorName: m.AuthorName, Content: m.Content,
		ContentText: m.ContentText, EditedAt: m.EditedAt, DeletedAt: m.DeletedAt, CreatedAt: m.CreatedAt, ReplyCount: m.ReplyCount,
	}
	if m.ThreadRootID != nil {
		id := uuid.MustParse(*m.ThreadRootID)
		e.ThreadRootID = &id
	}
	if m.RunID != nil {
		id := uuid.MustParse(*m.RunID)
		e.RunID = &id
	}
	// A deleted message keeps its place (seq) but not its words.
	if m.DeletedAt != nil {
		e.Content, e.ContentText = json.RawMessage("[]"), ""
	}
	return e
}

// CreateMessage implements chatdom.Repository. The channel row is locked by
// the seq increment, so seq stays gapless and ordered per channel.
func (r *ChatRepository) CreateMessage(ctx context.Context, m *chatdom.Message) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		var archived *time.Time
		var seq int64
		err := tx.QueryRowxContext(ctx, `UPDATE channels SET last_seq = last_seq + 1 WHERE id = $1::uuid
			RETURNING last_seq, archived_at`, m.ChannelID.String()).Scan(&seq, &archived)
		if errors.Is(err, sql.ErrNoRows) {
			return chatdom.ErrChannelNotFound
		}
		if err != nil {
			return fmt.Errorf("chat: allocate seq: %w", err)
		}
		if archived != nil {
			return chatdom.ErrChannelArchived
		}
		var root any
		if m.ThreadRootID != nil {
			root = m.ThreadRootID.String()
			var ok bool
			if err := tx.GetContext(ctx, &ok, `SELECT EXISTS (SELECT 1 FROM messages WHERE id = $1::uuid AND channel_id = $2::uuid
				AND thread_root_id IS NULL AND deleted_at IS NULL)`, m.ThreadRootID.String(), m.ChannelID.String()); err != nil {
				return fmt.Errorf("chat: check thread root: %w", err)
			}
			if !ok {
				return chatdom.ErrThreadRootInvalid
			}
		}
		m.Seq = seq
		var runArg any
		if m.RunID != nil {
			runArg = m.RunID.String()
		}
		if err := tx.QueryRowxContext(ctx, `INSERT INTO messages (id, channel_id, seq, thread_root_id, author_account_id, content, content_text, run_id)
			VALUES ($1::uuid, $2::uuid, $3, $4::uuid, $5::uuid, $6::jsonb, $7, $8::uuid) RETURNING created_at`,
			m.ID.String(), m.ChannelID.String(), seq, root, m.AuthorID.String(), []byte(m.Content), m.ContentText, runArg).Scan(&m.CreatedAt); err != nil {
			return fmt.Errorf("chat: insert message: %w", err)
		}
		// Writing a message reads everything before it.
		if _, err := tx.ExecContext(ctx, `UPDATE channel_members SET last_read_seq = GREATEST(last_read_seq, $3)
			WHERE channel_id = $1::uuid AND account_id = $2::uuid`, m.ChannelID.String(), m.AuthorID.String(), seq); err != nil {
			return fmt.Errorf("chat: advance author read: %w", err)
		}
		return nil
	})
}

// ListMessages implements chatdom.Repository.
func (r *ChatRepository) ListMessages(ctx context.Context, channelID, accountID uuid.UUID, f chatdom.MessageFilter) ([]*chatdom.Message, bool, error) {
	limit := f.Limit
	if limit <= 0 || limit > chatdom.MaxPage {
		limit = chatdom.DefaultPage
	}
	args := []any{channelID.String()}
	q := `SELECT ` + messageCols + ` FROM messages m LEFT JOIN accounts a ON a.id = m.author_account_id WHERE m.channel_id = $1::uuid`
	if f.ThreadRootID != nil {
		args = append(args, f.ThreadRootID.String())
		q += fmt.Sprintf(` AND m.thread_root_id = $%d::uuid`, len(args))
	} else {
		q += ` AND m.thread_root_id IS NULL`
	}
	asc := false
	switch {
	case f.AfterSeq != nil:
		args = append(args, *f.AfterSeq)
		q += fmt.Sprintf(` AND m.seq > $%d`, len(args))
		asc = true
	case f.BeforeSeq != nil:
		args = append(args, *f.BeforeSeq)
		q += fmt.Sprintf(` AND m.seq < $%d`, len(args))
	}
	if asc {
		q += ` ORDER BY m.seq ASC`
	} else {
		q += ` ORDER BY m.seq DESC`
	}
	args = append(args, limit+1)
	q += fmt.Sprintf(` LIMIT $%d`, len(args))

	var recs []messageRecord
	if err := r.db.SelectContext(ctx, &recs, q, args...); err != nil {
		return nil, false, fmt.Errorf("chat: list messages: %w", err)
	}
	hasMore := len(recs) > limit
	if hasMore {
		recs = recs[:limit]
	}
	out := make([]*chatdom.Message, len(recs))
	for i, rec := range recs {
		out[i] = rec.entity()
	}
	if !asc {
		slices.Reverse(out) // always oldest first within a page
	}
	if err := r.attachReactions(ctx, out, accountID); err != nil {
		return nil, false, err
	}
	if err := r.attachFlags(ctx, out, accountID); err != nil {
		return nil, false, err
	}
	return out, hasMore, nil
}

// FindMessage implements chatdom.Repository.
func (r *ChatRepository) FindMessage(ctx context.Context, channelID, messageID, accountID uuid.UUID) (*chatdom.Message, error) {
	var rec messageRecord
	err := r.db.GetContext(ctx, &rec, `SELECT `+messageCols+` FROM messages m LEFT JOIN accounts a ON a.id = m.author_account_id
		WHERE m.id = $1::uuid AND m.channel_id = $2::uuid`, messageID.String(), channelID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, chatdom.ErrMessageNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("chat: find message: %w", err)
	}
	m := rec.entity()
	if err := r.attachReactions(ctx, []*chatdom.Message{m}, accountID); err != nil {
		return nil, err
	}
	if err := r.attachFlags(ctx, []*chatdom.Message{m}, accountID); err != nil {
		return nil, err
	}
	return m, nil
}

func (r *ChatRepository) attachReactions(ctx context.Context, msgs []*chatdom.Message, accountID uuid.UUID) error {
	if len(msgs) == 0 {
		return nil
	}
	ids := make([]string, len(msgs))
	byID := make(map[uuid.UUID]*chatdom.Message, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID.String()
		byID[m.ID] = m
	}
	var rows []struct {
		MessageID string `db:"message_id"`
		Emoji     string `db:"emoji"`
		Count     int64  `db:"n"`
		Mine      bool   `db:"mine"`
	}
	err := r.db.SelectContext(ctx, &rows, `SELECT message_id::text, emoji, COUNT(*) AS n, BOOL_OR(account_id = $2::uuid) AS mine
		FROM message_reactions WHERE message_id = ANY($1::uuid[]) GROUP BY message_id, emoji ORDER BY MIN(created_at)`,
		"{"+joinStrings(ids)+"}", accountID.String())
	if err != nil {
		return fmt.Errorf("chat: reactions: %w", err)
	}
	for _, row := range rows {
		if m := byID[uuid.MustParse(row.MessageID)]; m != nil && m.DeletedAt == nil {
			m.Reactions = append(m.Reactions, chatdom.ReactionCount{Emoji: row.Emoji, Count: row.Count, Mine: row.Mine})
		}
	}
	return nil
}

func joinStrings(in []string) string {
	out := ""
	for i, s := range in {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}

// EditMessage implements chatdom.Repository.
func (r *ChatRepository) EditMessage(ctx context.Context, channelID, messageID, authorID uuid.UUID, content []byte, contentText string) error {
	res, err := r.db.ExecContext(ctx, `UPDATE messages SET content = $4::jsonb, content_text = $5, edited_at = NOW()
		WHERE id = $1::uuid AND channel_id = $2::uuid AND author_account_id = $3::uuid AND deleted_at IS NULL`,
		messageID.String(), channelID.String(), authorID.String(), content, contentText)
	if err != nil {
		return fmt.Errorf("chat: edit message: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return chatdom.ErrMessageNotFound
	}
	return nil
}

// DeleteMessage implements chatdom.Repository.
func (r *ChatRepository) DeleteMessage(ctx context.Context, channelID, messageID uuid.UUID, authorID *uuid.UUID) error {
	q := `UPDATE messages SET deleted_at = NOW(), content = '[]'::jsonb, content_text = ''
		WHERE id = $1::uuid AND channel_id = $2::uuid AND deleted_at IS NULL`
	args := []any{messageID.String(), channelID.String()}
	if authorID != nil {
		q += ` AND author_account_id = $3::uuid`
		args = append(args, authorID.String())
	}
	res, err := r.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("chat: delete message: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return chatdom.ErrMessageNotFound
	}
	return nil
}

// AddReaction implements chatdom.Repository.
func (r *ChatRepository) AddReaction(ctx context.Context, messageID, accountID uuid.UUID, emoji string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO message_reactions (message_id, account_id, emoji) VALUES ($1::uuid, $2::uuid, $3)
		ON CONFLICT DO NOTHING`, messageID.String(), accountID.String(), emoji)
	if err != nil {
		return fmt.Errorf("chat: add reaction: %w", err)
	}
	return nil
}

// RemoveReaction implements chatdom.Repository.
func (r *ChatRepository) RemoveReaction(ctx context.Context, messageID, accountID uuid.UUID, emoji string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM message_reactions WHERE message_id = $1::uuid AND account_id = $2::uuid AND emoji = $3`,
		messageID.String(), accountID.String(), emoji)
	if err != nil {
		return fmt.Errorf("chat: remove reaction: %w", err)
	}
	return nil
}

// MarkRead implements chatdom.Repository.
func (r *ChatRepository) MarkRead(ctx context.Context, channelID, accountID uuid.UUID, seq int64) error {
	// Never past the channel head, never backwards.
	res, err := r.db.ExecContext(ctx, `UPDATE channel_members cm SET last_read_seq = GREATEST(cm.last_read_seq, LEAST($3, c.last_seq))
		FROM channels c WHERE c.id = cm.channel_id AND cm.channel_id = $1::uuid AND cm.account_id = $2::uuid`,
		channelID.String(), accountID.String(), seq)
	if err != nil {
		return fmt.Errorf("chat: mark read: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return chatdom.ErrNotAMember
	}
	return nil
}

// ChannelRecipients implements chatdom.Repository.
func (r *ChatRepository) ChannelRecipients(ctx context.Context, channelID uuid.UUID) ([]uuid.UUID, error) {
	var ids []string
	err := r.db.SelectContext(ctx, &ids, `SELECT cm.account_id::text FROM channel_members cm
		JOIN channels c ON c.id = cm.channel_id
		WHERE cm.channel_id = $1::uuid AND (c.project_id IS NULL OR EXISTS (SELECT 1 FROM project_members pm
			WHERE pm.project_id = c.project_id AND pm.account_id = cm.account_id AND pm.deleted_at IS NULL))
		ORDER BY cm.account_id`, channelID.String())
	if err != nil {
		return nil, fmt.Errorf("chat: recipients: %w", err)
	}
	out := make([]uuid.UUID, len(ids))
	for i, id := range ids {
		out[i] = uuid.MustParse(id)
	}
	return out, nil
}

// UnreadByProject implements chatdom.Repository. It counts only channels the
// caller has joined in projects they are still an active member of, and, as
// one row with a nil project, their direct messages; access to a channel is
// checked again when it is opened.
func (r *ChatRepository) UnreadByProject(ctx context.Context, accountID uuid.UUID) ([]chatdom.UnreadSummary, error) {
	var rows []struct {
		ProjectID   *string `db:"project_id"`
		ProjectName string  `db:"project_name"`
		Unread      int64   `db:"unread"`
		Channels    int64   `db:"channels"`
	}
	err := r.db.SelectContext(ctx, &rows, `
		SELECT c.project_id::text AS project_id, p.name AS project_name,
			COALESCE(SUM(c.last_seq - cm.last_read_seq), 0) AS unread, COUNT(*) AS channels
		FROM channel_members cm
		JOIN channels c ON c.id = cm.channel_id
		JOIN projects p ON p.id = c.project_id AND p.deleted_at IS NULL
		JOIN project_members pm ON pm.project_id = c.project_id AND pm.account_id = cm.account_id AND pm.deleted_at IS NULL
		WHERE cm.account_id = $1::uuid AND c.archived_at IS NULL AND c.last_seq > cm.last_read_seq
		GROUP BY c.project_id, p.name
		UNION ALL
		SELECT NULL, '', COALESCE(SUM(c.last_seq - cm.last_read_seq), 0), COUNT(*)
		FROM channel_members cm JOIN channels c ON c.id = cm.channel_id
		WHERE cm.account_id = $1::uuid AND c.project_id IS NULL AND c.kind = 'dm' AND c.archived_at IS NULL
		  AND c.last_seq > cm.last_read_seq
		HAVING COUNT(*) > 0
		ORDER BY 2`, accountID.String())
	if err != nil {
		return nil, fmt.Errorf("chat: unread: %w", err)
	}
	out := make([]chatdom.UnreadSummary, len(rows))
	for i, row := range rows {
		u := chatdom.UnreadSummary{ProjectName: row.ProjectName, Unread: row.Unread, Channels: row.Channels}
		if row.ProjectID != nil {
			id := uuid.MustParse(*row.ProjectID)
			u.ProjectID = &id
		}
		out[i] = u
	}
	return out, nil
}

type inboxRecord struct {
	ProjectID     *string    `db:"project_id"`
	ProjectName   *string    `db:"project_name"`
	PeerID        *string    `db:"peer_id"`
	PeerName      *string    `db:"peer_name"`
	PeerKind      *string    `db:"peer_kind"`
	ChannelID     string     `db:"channel_id"`
	ChannelName   string     `db:"channel_name"`
	Kind          string     `db:"kind"`
	LastSeq       int64      `db:"last_seq"`
	LastReadSeq   int64      `db:"last_read_seq"`
	LastMessageAt *time.Time `db:"last_at"`
	LastAuthor    *string    `db:"last_author"`
	Preview       *string    `db:"preview"`
	MentionCount  int64      `db:"mention_count"`
	Starred       bool       `db:"starred"`
}

// ListInbox implements chatdom.Repository: the project channels and the
// direct messages the caller follows, unread first, then by latest message.
func (r *ChatRepository) ListInbox(ctx context.Context, accountID uuid.UUID, f chatdom.InboxFilter) ([]chatdom.InboxItem, error) {
	limit := f.Limit
	if limit <= 0 || limit > chatdom.MaxInbox {
		limit = chatdom.DefaultInbox
	}
	items, err := r.listProjectInbox(ctx, accountID, f, limit)
	if err != nil {
		return nil, err
	}
	// A project filter asks about that project only; direct messages have none.
	if f.ProjectID == nil {
		dms, err := r.listDMInbox(ctx, accountID, f, limit)
		if err != nil {
			return nil, err
		}
		items = append(items, dms...)
		slices.SortStableFunc(items, func(a, b chatdom.InboxItem) int {
			ua, ub := a.UnreadCount > 0, b.UnreadCount > 0
			switch {
			case ua != ub && ua:
				return -1
			case ua != ub:
				return 1
			case a.LastMessageAt == nil && b.LastMessageAt == nil:
				return 0
			case a.LastMessageAt == nil:
				return 1
			case b.LastMessageAt == nil:
				return -1
			}
			return b.LastMessageAt.Compare(*a.LastMessageAt)
		})
		if len(items) > limit {
			items = items[:limit]
		}
	}
	return items, nil
}

func inboxItem(rec inboxRecord) chatdom.InboxItem {
	it := chatdom.InboxItem{
		ChannelID: uuid.MustParse(rec.ChannelID), ChannelName: rec.ChannelName, Kind: rec.Kind,
		LastSeq: rec.LastSeq, LastReadSeq: rec.LastReadSeq, LastMessageAt: rec.LastMessageAt, MentionCount: rec.MentionCount, Starred: rec.Starred,
	}
	if rec.ProjectID != nil {
		id := uuid.MustParse(*rec.ProjectID)
		it.ProjectID = &id
	}
	if rec.ProjectName != nil {
		it.ProjectName = *rec.ProjectName
	}
	if rec.PeerID != nil && rec.PeerName != nil && rec.PeerKind != nil {
		it.Peer = &chatdom.DMPeer{AccountID: uuid.MustParse(*rec.PeerID), Name: *rec.PeerName, Kind: *rec.PeerKind}
	}
	if u := rec.LastSeq - rec.LastReadSeq; u > 0 {
		it.UnreadCount = u
	}
	if rec.LastAuthor != nil {
		it.LastAuthor = *rec.LastAuthor
	}
	if rec.Preview != nil {
		it.Preview = truncateRunes(*rec.Preview, chatdom.PreviewLen)
	}
	return it
}

// listDMInbox lists the direct messages the caller follows and may read.
func (r *ChatRepository) listDMInbox(ctx context.Context, accountID uuid.UUID, f chatdom.InboxFilter, limit int) ([]chatdom.InboxItem, error) {
	sink := &argList{args: []any{accountID.String()}}
	clause, none, err := scopeSQL(ctx, "channel", channelScopeColumns, sink)
	if err != nil {
		return nil, err
	}
	if none {
		return nil, nil
	}
	q := `SELECT c.id::text AS channel_id, c.name AS channel_name, c.kind, c.last_seq, cm.last_read_seq,
			lm.created_at AS last_at, lm.author_name AS last_author, lm.content_text AS preview,
			(SELECT COUNT(*) FROM message_mentions mm JOIN messages mmm ON mmm.id = mm.message_id WHERE mm.account_id = $1::uuid AND mmm.channel_id = c.id AND mmm.deleted_at IS NULL AND mmm.seq > cm.last_read_seq) AS mention_count, cm.starred,
			peer.id::text AS peer_id, peer.name AS peer_name, peer.kind AS peer_kind
		FROM channel_members cm
		JOIN channels c ON c.id = cm.channel_id AND c.kind = 'dm' AND c.project_id IS NULL AND c.archived_at IS NULL
		LEFT JOIN LATERAL (SELECT a.id, a.name, a.kind FROM channel_members pm JOIN accounts a ON a.id = pm.account_id
			WHERE pm.channel_id = c.id AND pm.account_id <> $1::uuid LIMIT 1) peer ON TRUE
		LEFT JOIN LATERAL (
			SELECT m.created_at, a.name AS author_name, m.content_text FROM messages m
			LEFT JOIN accounts a ON a.id = m.author_account_id
			WHERE m.channel_id = c.id AND m.deleted_at IS NULL ORDER BY m.seq DESC LIMIT 1) lm ON TRUE
		WHERE cm.account_id = $1::uuid`
	if clause != "" {
		q += ` AND ` + clause
	}
	if f.UnreadOnly {
		q += ` AND c.last_seq > cm.last_read_seq`
	}
	q += ` ORDER BY (c.last_seq > cm.last_read_seq) DESC, lm.created_at DESC NULLS LAST, c.id LIMIT ` + sink.addArg(limit)
	var recs []inboxRecord
	if err := r.db.SelectContext(ctx, &recs, q, sink.args...); err != nil {
		return nil, fmt.Errorf("chat: dm inbox: %w", err)
	}
	out := make([]chatdom.InboxItem, len(recs))
	for i, rec := range recs {
		out[i] = inboxItem(rec)
	}
	return out, nil
}

// listProjectInbox lists the project channels the caller follows and may read.
func (r *ChatRepository) listProjectInbox(ctx context.Context, accountID uuid.UUID, f chatdom.InboxFilter, limit int) ([]chatdom.InboxItem, error) {
	sink := &argList{args: []any{accountID.String()}}
	// Which channels the caller may read, per project, from their own policies.
	// Without scopes attached (an internal caller) nothing is filtered.
	clause, none, err := projectScopesSQL(ctx, "channel", channelScopeColumns, "c.project_id", sink)
	if err != nil {
		return nil, err
	}
	if none {
		return nil, nil
	}
	q := `SELECT c.project_id::text AS project_id, p.name AS project_name, c.id::text AS channel_id, c.name AS channel_name,
			c.kind, c.last_seq, cm.last_read_seq, lm.created_at AS last_at, lm.author_name AS last_author, lm.content_text AS preview,
			(SELECT COUNT(*) FROM message_mentions mm JOIN messages mmm ON mmm.id = mm.message_id WHERE mm.account_id = $1::uuid AND mmm.channel_id = c.id AND mmm.deleted_at IS NULL AND mmm.seq > cm.last_read_seq) AS mention_count, cm.starred
		FROM channel_members cm
		JOIN channels c ON c.id = cm.channel_id AND c.archived_at IS NULL
		JOIN projects p ON p.id = c.project_id AND p.deleted_at IS NULL
		JOIN project_members pm ON pm.project_id = c.project_id AND pm.account_id = cm.account_id AND pm.deleted_at IS NULL
		LEFT JOIN LATERAL (
			SELECT m.created_at, a.name AS author_name, m.content_text FROM messages m
			LEFT JOIN accounts a ON a.id = m.author_account_id
			WHERE m.channel_id = c.id AND m.deleted_at IS NULL ORDER BY m.seq DESC LIMIT 1) lm ON TRUE
		WHERE cm.account_id = $1::uuid`
	if clause != "" {
		q += ` AND ` + clause
	}
	if f.UnreadOnly {
		q += ` AND c.last_seq > cm.last_read_seq`
	}
	if f.ProjectID != nil {
		q += ` AND c.project_id = ` + sink.addArg(f.ProjectID.String()) + `::uuid`
	}
	q += ` ORDER BY (c.last_seq > cm.last_read_seq) DESC, lm.created_at DESC NULLS LAST, c.id LIMIT ` + sink.addArg(limit)
	var recs []inboxRecord
	if err := r.db.SelectContext(ctx, &recs, q, sink.args...); err != nil {
		return nil, fmt.Errorf("chat: inbox: %w", err)
	}
	out := make([]chatdom.InboxItem, len(recs))
	for i, rec := range recs {
		out[i] = inboxItem(rec)
	}
	return out, nil
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// MemberProjectIDs implements chatdom.Repository.
func (r *ChatRepository) MemberProjectIDs(ctx context.Context, accountID uuid.UUID) ([]uuid.UUID, error) {
	var ids []string
	err := r.db.SelectContext(ctx, &ids, `SELECT DISTINCT pm.project_id::text FROM project_members pm
		JOIN projects p ON p.id = pm.project_id AND p.deleted_at IS NULL
		WHERE pm.account_id = $1::uuid AND pm.deleted_at IS NULL`, accountID.String())
	if err != nil {
		return nil, fmt.Errorf("chat: member projects: %w", err)
	}
	out := make([]uuid.UUID, len(ids))
	for i, id := range ids {
		out[i] = uuid.MustParse(id)
	}
	return out, nil
}

// MarkAllRead implements chatdom.Repository.
func (r *ChatRepository) MarkAllRead(ctx context.Context, accountID uuid.UUID, projectID *uuid.UUID) error {
	q := `UPDATE channel_members cm SET last_read_seq = c.last_seq
		FROM channels c WHERE c.id = cm.channel_id AND cm.account_id = $1::uuid AND c.last_seq > cm.last_read_seq`
	args := []any{accountID.String()}
	if projectID != nil {
		q += ` AND c.project_id = $2::uuid`
		args = append(args, projectID.String())
	}
	if _, err := r.db.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("chat: mark all read: %w", err)
	}
	// Followed threads are read with their channel.
	tq := `UPDATE thread_follows tf SET last_read_seq = c.last_seq
		FROM messages m JOIN channels c ON c.id = m.channel_id
		WHERE m.id = tf.root_message_id AND tf.account_id = $1::uuid AND c.last_seq > tf.last_read_seq`
	if projectID != nil {
		tq += ` AND c.project_id = $2::uuid`
	}
	if _, err := r.db.ExecContext(ctx, tq, args...); err != nil {
		return fmt.Errorf("chat: mark all threads read: %w", err)
	}
	return nil
}

// FindOrCreateDM implements chatdom.Repository.
func (r *ChatRepository) FindOrCreateDM(ctx context.Context, a, b uuid.UUID) (uuid.UUID, bool, error) {
	lo, hi := a.String(), b.String()
	if hi < lo {
		lo, hi = hi, lo
	}
	key := lo + ":" + hi
	var id string
	var created bool
	err := WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		// The unique index on dm_key makes concurrent starts agree on one row.
		err := tx.GetContext(ctx, &id, `INSERT INTO channels (scope, kind, name, dm_key, created_by)
			VALUES ('workspace', 'dm', '', $1, $2::uuid) ON CONFLICT (dm_key) WHERE dm_key IS NOT NULL DO NOTHING RETURNING id::text`,
			key, a.String())
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if err := tx.GetContext(ctx, &id, `SELECT id::text FROM channels WHERE dm_key = $1`, key); err != nil {
				return fmt.Errorf("chat: find dm: %w", err)
			}
		case err != nil:
			return fmt.Errorf("chat: create dm: %w", err)
		default:
			created = true
		}
		for _, m := range []uuid.UUID{a, b} {
			if _, err := tx.ExecContext(ctx, `INSERT INTO channel_members (channel_id, account_id, last_read_seq)
				SELECT id, $2::uuid, last_seq FROM channels WHERE id = $1::uuid ON CONFLICT DO NOTHING`, id, m.String()); err != nil {
				return fmt.Errorf("chat: add dm member: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return uuid.Nil, false, err
	}
	return uuid.MustParse(id), created, nil
}

// ListDMs implements chatdom.Repository.
func (r *ChatRepository) ListDMs(ctx context.Context, accountID uuid.UUID) ([]*chatdom.Channel, error) {
	sink := &argList{args: []any{accountID.String()}}
	clause, none, err := scopeSQL(ctx, "channel", channelScopeColumns, sink)
	if err != nil {
		return nil, err
	}
	if none {
		return []*chatdom.Channel{}, nil
	}
	q := `SELECT ` + channelCols + channelFrom + `
		LEFT JOIN LATERAL (SELECT m.created_at FROM messages m WHERE m.channel_id = c.id AND m.deleted_at IS NULL
			ORDER BY m.seq DESC LIMIT 1) lm ON TRUE
		WHERE c.kind = 'dm' AND c.project_id IS NULL AND cm.account_id IS NOT NULL AND c.archived_at IS NULL`
	if clause != "" {
		q += ` AND ` + clause
	}
	q += ` ORDER BY COALESCE(lm.created_at, c.created_at) DESC, c.id`
	var recs []channelRecord
	if err := r.db.SelectContext(ctx, &recs, q, sink.args...); err != nil {
		return nil, fmt.Errorf("chat: list dms: %w", err)
	}
	out := make([]*chatdom.Channel, len(recs))
	for i, rec := range recs {
		out[i] = rec.entity()
	}
	return out, nil
}

// AccountKind implements chatdom.Repository.
func (r *ChatRepository) AccountKind(ctx context.Context, id uuid.UUID) (string, bool, error) {
	var kind string
	err := r.db.GetContext(ctx, &kind, `SELECT kind FROM accounts WHERE id = $1::uuid AND deleted_at IS NULL`, id.String())
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("chat: account kind: %w", err)
	}
	return kind, true, nil
}

// likeEscaper escapes the wildcards of a LIKE pattern.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// Directory implements chatdom.Repository.
func (r *ChatRepository) Directory(ctx context.Context, accountID uuid.UUID, query, kind string, limit int) ([]chatdom.DirectoryEntry, error) {
	if limit <= 0 || limit > 50 {
		limit = 25
	}
	var rows []struct {
		ID          string `db:"id"`
		Name        string `db:"name"`
		Kind        string `db:"kind"`
		Description string `db:"description"`
	}
	err := r.db.SelectContext(ctx, &rows, `SELECT a.id::text AS id, a.name, a.kind, a.description FROM accounts a
		WHERE a.deleted_at IS NULL AND a.id <> $1::uuid
		  AND a.name ILIKE $2 ESCAPE '\'
		  AND ($3 = '' OR a.kind = $3)
		  AND (EXISTS (SELECT 1 FROM project_members mine JOIN project_members theirs ON theirs.project_id = mine.project_id
		               WHERE mine.account_id = $1::uuid AND mine.deleted_at IS NULL
		                 AND theirs.account_id = a.id AND theirs.deleted_at IS NULL)
		       OR (a.kind = 'agent' AND EXISTS (SELECT 1 FROM agents ag WHERE ag.id = a.id AND ag.project_id IS NULL)))
		ORDER BY lower(a.name), a.id LIMIT $4`,
		accountID.String(), "%"+likeEscaper.Replace(strings.TrimSpace(query))+"%", kind, limit)
	if err != nil {
		return nil, fmt.Errorf("chat: directory: %w", err)
	}
	out := make([]chatdom.DirectoryEntry, len(rows))
	for i, row := range rows {
		out[i] = chatdom.DirectoryEntry{AccountID: uuid.MustParse(row.ID), Name: row.Name, Kind: row.Kind, Description: row.Description}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Mentions, followed threads and saved messages
// ---------------------------------------------------------------------------

func chatUUIDStrings(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

// pgUUIDArray renders ids as a Postgres array literal for ANY($n::uuid[]).
func pgUUIDArray(ids []uuid.UUID) string { return "{" + joinStrings(chatUUIDStrings(ids)) + "}" }

// RecordMessageEffects implements chatdom.Repository.
func (r *ChatRepository) RecordMessageEffects(ctx context.Context, m *chatdom.Message, mentionIDs []uuid.UUID) error {
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		var mentioned []string
		if len(mentionIDs) > 0 {
			// Only existing accounts, never the author.
			if err := tx.SelectContext(ctx, &mentioned, `INSERT INTO message_mentions (message_id, account_id)
				SELECT $1::uuid, a.id FROM accounts a WHERE a.id = ANY($2::uuid[]) AND a.deleted_at IS NULL AND a.id <> $3::uuid
				ON CONFLICT DO NOTHING RETURNING account_id::text`, m.ID.String(), pgUUIDArray(mentionIDs), m.AuthorID.String()); err != nil {
				return fmt.Errorf("chat: mentions: %w", err)
			}
		}
		// A mentioned project member follows a public channel from this message on,
		// so the mention reaches their Inbox and unread counts.
		if len(mentioned) > 0 {
			if _, err := tx.ExecContext(ctx, `INSERT INTO channel_members (channel_id, account_id, last_read_seq)
				SELECT c.id, a.id, $3 - 1 FROM channels c, unnest($2::uuid[]) a(id)
				WHERE c.id = $1::uuid AND c.project_id IS NOT NULL AND c.kind IN ('public', 'voice')
				  AND EXISTS (SELECT 1 FROM project_members pm WHERE pm.project_id = c.project_id AND pm.account_id = a.id AND pm.deleted_at IS NULL)
				ON CONFLICT DO NOTHING`, m.ChannelID.String(), pgUUIDArray(mustParseUUIDs(mentioned)), m.Seq); err != nil {
				return fmt.Errorf("chat: follow channel: %w", err)
			}
		}
		if m.ThreadRootID == nil {
			return nil
		}
		// A reply: the author follows the thread and has read it; the root's author
		// and anyone mentioned follow it with this reply unread.
		root := m.ThreadRootID.String()
		if _, err := tx.ExecContext(ctx, `INSERT INTO thread_follows (account_id, root_message_id, last_read_seq) VALUES ($1::uuid, $2::uuid, $3)
			ON CONFLICT (account_id, root_message_id) DO UPDATE SET last_read_seq = GREATEST(thread_follows.last_read_seq, EXCLUDED.last_read_seq)`,
			m.AuthorID.String(), root, m.Seq); err != nil {
			return fmt.Errorf("chat: follow thread: %w", err)
		}
		others := append([]string{}, mentioned...)
		if _, err := tx.ExecContext(ctx, `INSERT INTO thread_follows (account_id, root_message_id, last_read_seq)
			SELECT x.id, $1::uuid, $2 - 1 FROM (SELECT author_account_id AS id FROM messages WHERE id = $1::uuid UNION SELECT unnest($3::uuid[])) x
			WHERE x.id <> $4::uuid ON CONFLICT DO NOTHING`, root, m.Seq, pgUUIDArray(mustParseUUIDs(others)), m.AuthorID.String()); err != nil {
			return fmt.Errorf("chat: follow thread: %w", err)
		}
		return nil
	})
}

func mustParseUUIDs(in []string) []uuid.UUID {
	out := make([]uuid.UUID, len(in))
	for i, s := range in {
		out[i] = uuid.MustParse(s)
	}
	return out
}

// SetMentions implements chatdom.Repository.
func (r *ChatRepository) SetMentions(ctx context.Context, messageID, authorID uuid.UUID, mentionIDs []uuid.UUID) error {
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM message_mentions WHERE message_id = $1::uuid AND NOT (account_id = ANY($2::uuid[]))`,
			messageID.String(), pgUUIDArray(mentionIDs)); err != nil {
			return fmt.Errorf("chat: clear mentions: %w", err)
		}
		if len(mentionIDs) == 0 {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO message_mentions (message_id, account_id)
			SELECT $1::uuid, a.id FROM accounts a WHERE a.id = ANY($2::uuid[]) AND a.deleted_at IS NULL AND a.id <> $3::uuid
			ON CONFLICT DO NOTHING`, messageID.String(), pgUUIDArray(mentionIDs), authorID.String()); err != nil {
			return fmt.Errorf("chat: set mentions: %w", err)
		}
		return nil
	})
}

// FollowThread implements chatdom.Repository.
func (r *ChatRepository) FollowThread(ctx context.Context, accountID, channelID, rootID uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `INSERT INTO thread_follows (account_id, root_message_id, last_read_seq)
		SELECT $1::uuid, m.id, COALESCE((SELECT MAX(x.seq) FROM messages x WHERE x.thread_root_id = m.id), m.seq)
		FROM messages m WHERE m.id = $3::uuid AND m.channel_id = $2::uuid AND m.thread_root_id IS NULL AND m.deleted_at IS NULL
		ON CONFLICT DO NOTHING`, accountID.String(), channelID.String(), rootID.String())
	if err != nil {
		return fmt.Errorf("chat: follow thread: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Already following is fine; a root that is not there is not.
		var ok bool
		if err := r.db.GetContext(ctx, &ok, `SELECT EXISTS (SELECT 1 FROM messages WHERE id = $2::uuid AND channel_id = $1::uuid AND thread_root_id IS NULL AND deleted_at IS NULL)`,
			channelID.String(), rootID.String()); err != nil {
			return fmt.Errorf("chat: follow thread: %w", err)
		}
		if !ok {
			return chatdom.ErrThreadRootInvalid
		}
	}
	return nil
}

// UnfollowThread implements chatdom.Repository.
func (r *ChatRepository) UnfollowThread(ctx context.Context, accountID, rootID uuid.UUID) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM thread_follows WHERE account_id = $1::uuid AND root_message_id = $2::uuid`,
		accountID.String(), rootID.String()); err != nil {
		return fmt.Errorf("chat: unfollow thread: %w", err)
	}
	return nil
}

// MarkThreadRead implements chatdom.Repository.
func (r *ChatRepository) MarkThreadRead(ctx context.Context, accountID, channelID, rootID uuid.UUID, seq int64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE thread_follows tf SET last_read_seq = GREATEST(tf.last_read_seq, LEAST($4, c.last_seq))
		FROM messages m JOIN channels c ON c.id = m.channel_id
		WHERE tf.root_message_id = m.id AND m.id = $3::uuid AND m.channel_id = $2::uuid AND tf.account_id = $1::uuid`,
		accountID.String(), channelID.String(), rootID.String(), seq)
	if err != nil {
		return fmt.Errorf("chat: thread read: %w", err)
	}
	return nil
}

// SaveMessage implements chatdom.Repository.
func (r *ChatRepository) SaveMessage(ctx context.Context, accountID, channelID, messageID uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `INSERT INTO saved_messages (account_id, message_id)
		SELECT $1::uuid, m.id FROM messages m WHERE m.id = $3::uuid AND m.channel_id = $2::uuid AND m.deleted_at IS NULL
		ON CONFLICT DO NOTHING`, accountID.String(), channelID.String(), messageID.String())
	if err != nil {
		return fmt.Errorf("chat: save: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var ok bool
		if err := r.db.GetContext(ctx, &ok, `SELECT EXISTS (SELECT 1 FROM messages WHERE id = $2::uuid AND channel_id = $1::uuid AND deleted_at IS NULL)`,
			channelID.String(), messageID.String()); err != nil {
			return fmt.Errorf("chat: save: %w", err)
		}
		if !ok {
			return chatdom.ErrMessageNotFound
		}
	}
	return nil
}

// UnsaveMessage implements chatdom.Repository.
func (r *ChatRepository) UnsaveMessage(ctx context.Context, accountID, messageID uuid.UUID) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM saved_messages WHERE account_id = $1::uuid AND message_id = $2::uuid`,
		accountID.String(), messageID.String()); err != nil {
		return fmt.Errorf("chat: unsave: %w", err)
	}
	return nil
}

// attachFlags sets the caller's Saved and Following flags on msgs.
func (r *ChatRepository) attachFlags(ctx context.Context, msgs []*chatdom.Message, accountID uuid.UUID) error {
	if len(msgs) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(msgs))
	byID := make(map[uuid.UUID]*chatdom.Message, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
		byID[m.ID] = m
	}
	var saved, followed []string
	if err := r.db.SelectContext(ctx, &saved, `SELECT message_id::text FROM saved_messages WHERE account_id = $1::uuid AND message_id = ANY($2::uuid[])`,
		accountID.String(), pgUUIDArray(ids)); err != nil {
		return fmt.Errorf("chat: saved flags: %w", err)
	}
	if err := r.db.SelectContext(ctx, &followed, `SELECT root_message_id::text FROM thread_follows WHERE account_id = $1::uuid AND root_message_id = ANY($2::uuid[])`,
		accountID.String(), pgUUIDArray(ids)); err != nil {
		return fmt.Errorf("chat: follow flags: %w", err)
	}
	for _, id := range saved {
		if m := byID[uuid.MustParse(id)]; m != nil {
			m.Saved = true
		}
	}
	for _, id := range followed {
		if m := byID[uuid.MustParse(id)]; m != nil {
			m.Following = true
		}
	}
	return nil
}

// chatVisibilitySQL is the predicate over the channel alias c that holds for
// the channels the caller may read, from the IAM scopes attached to ctx: per
// project (iam.WithProjectScopes) for project channels, and the root scope
// (iam.WithScope) for direct messages. none means nothing is visible.
func chatVisibilitySQL(ctx context.Context, sink argSink) (clause string, none bool, err error) {
	pc, pnone, err := projectScopesSQL(ctx, "channel", channelScopeColumns, "c.project_id", sink)
	if err != nil {
		return "", false, err
	}
	rc, rnone, err := scopeSQL(ctx, "channel", channelScopeColumns, sink)
	if err != nil {
		return "", false, err
	}
	var parts []string
	if !pnone {
		p := "c.project_id IS NOT NULL"
		if pc != "" {
			p += " AND " + pc
		}
		parts = append(parts, "("+p+")")
	}
	if !rnone {
		p := "c.project_id IS NULL"
		if rc != "" {
			p += " AND " + rc
		}
		parts = append(parts, "("+p+")")
	}
	if len(parts) == 0 {
		return "", true, nil
	}
	return "(" + strings.Join(parts, " OR ") + ")", false, nil
}

type messageRefRecord struct {
	ProjectID    *string   `db:"project_id"`
	ProjectName  *string   `db:"project_name"`
	PeerID       *string   `db:"peer_id"`
	PeerName     *string   `db:"peer_name"`
	PeerKind     *string   `db:"peer_kind"`
	ChannelID    string    `db:"channel_id"`
	ChannelName  string    `db:"channel_name"`
	Kind         string    `db:"kind"`
	MessageID    string    `db:"message_id"`
	Seq          int64     `db:"seq"`
	ThreadRootID *string   `db:"thread_root_id"`
	RootSeq      *int64    `db:"root_seq"`
	AuthorID     string    `db:"author_id"`
	AuthorName   string    `db:"author_name"`
	Preview      string    `db:"preview"`
	RootPreview  string    `db:"root_preview"`
	CreatedAt    time.Time `db:"created_at"`
	ActivityAt   time.Time `db:"activity_at"`
	Unread       bool      `db:"unread"`
	UnreadCount  int64     `db:"unread_count"`
	ReplyCount   int64     `db:"reply_count"`
}

// messageRefFrom is the SELECT list shared by the three lists; the FROM clause
// supplies the aliases m (the message shown), c (its channel), p (its project),
// cm (the caller's membership), a (its author) and the columns named below.
const messageRefSelect = `SELECT c.project_id::text AS project_id, p.name AS project_name,
	peer.id::text AS peer_id, peer.name AS peer_name, peer.kind AS peer_kind,
	c.id::text AS channel_id, c.name AS channel_name, c.kind, m.id::text AS message_id, m.seq,
	m.thread_root_id::text AS thread_root_id, (SELECT rr.seq FROM messages rr WHERE rr.id = m.thread_root_id) AS root_seq,
	m.author_account_id::text AS author_id, COALESCE(a.name, '') AS author_name,
	m.content_text AS preview, ''::text AS root_preview, m.created_at, m.created_at AS activity_at,
	FALSE AS unread, 0::bigint AS unread_count, 0::bigint AS reply_count`

const messageRefJoins = `
	JOIN channels c ON c.id = m.channel_id AND c.archived_at IS NULL
	LEFT JOIN projects p ON p.id = c.project_id
	LEFT JOIN channel_members cm ON cm.channel_id = c.id AND cm.account_id = $1::uuid
	LEFT JOIN accounts a ON a.id = m.author_account_id
	LEFT JOIN LATERAL (SELECT pa.id, pa.name, pa.kind FROM channel_members pm JOIN accounts pa ON pa.id = pm.account_id
		WHERE c.kind = 'dm' AND pm.channel_id = c.id AND pm.account_id <> $1::uuid LIMIT 1) peer ON TRUE`

// the caller must still belong to the project a project channel is in
const messageRefGuard = ` AND (c.project_id IS NULL OR (p.deleted_at IS NULL AND EXISTS (SELECT 1 FROM project_members pmm
	WHERE pmm.project_id = c.project_id AND pmm.account_id = $1::uuid AND pmm.deleted_at IS NULL)))`

func (r *ChatRepository) listRefs(ctx context.Context, accountID uuid.UUID, query, order string, limit int) ([]chatdom.MessageRef, error) {
	return r.listRefsArgs(ctx, accountID, query, order, limit, nil)
}

// listRefsArgs is listRefs for a query with parameters of its own: they are
// $2, $3 … (the caller is $1) and the scope clause and limit follow them.
func (r *ChatRepository) listRefsArgs(ctx context.Context, accountID uuid.UUID, query, order string, limit int, extra []any) ([]chatdom.MessageRef, error) {
	if limit <= 0 || limit > chatdom.MaxMessageRefs {
		limit = chatdom.MaxMessageRefs
	}
	sink := &argList{args: append([]any{accountID.String()}, extra...)}
	clause, none, err := chatVisibilitySQL(ctx, sink)
	if err != nil {
		return nil, err
	}
	if none {
		return nil, nil
	}
	q := query + messageRefGuard
	if clause != "" {
		q += " AND " + clause
	}
	q += " ORDER BY " + order + " LIMIT " + sink.addArg(limit)
	var recs []messageRefRecord
	if err := r.db.SelectContext(ctx, &recs, q, sink.args...); err != nil {
		return nil, fmt.Errorf("chat: message list: %w", err)
	}
	out := make([]chatdom.MessageRef, len(recs))
	for i, rec := range recs {
		ref := chatdom.MessageRef{
			ChannelID: uuid.MustParse(rec.ChannelID), ChannelName: rec.ChannelName, Kind: rec.Kind,
			MessageID: uuid.MustParse(rec.MessageID), Seq: rec.Seq, RootSeq: rec.RootSeq, AuthorID: uuid.MustParse(rec.AuthorID), AuthorName: rec.AuthorName,
			Preview: truncateRunes(rec.Preview, chatdom.PreviewLen), RootPreview: truncateRunes(rec.RootPreview, chatdom.PreviewLen),
			CreatedAt: rec.CreatedAt, ActivityAt: rec.ActivityAt, Unread: rec.Unread, UnreadCount: rec.UnreadCount, ReplyCount: rec.ReplyCount,
		}
		if rec.ProjectID != nil {
			id := uuid.MustParse(*rec.ProjectID)
			ref.ProjectID = &id
		}
		if rec.ProjectName != nil {
			ref.ProjectName = *rec.ProjectName
		}
		if rec.ThreadRootID != nil {
			id := uuid.MustParse(*rec.ThreadRootID)
			ref.ThreadRootID = &id
		}
		if rec.PeerID != nil && rec.PeerName != nil && rec.PeerKind != nil {
			ref.Peer = &chatdom.DMPeer{AccountID: uuid.MustParse(*rec.PeerID), Name: *rec.PeerName, Kind: *rec.PeerKind}
		}
		out[i] = ref
	}
	return out, nil
}

// ListMentions implements chatdom.Repository.
func (r *ChatRepository) ListMentions(ctx context.Context, accountID uuid.UUID, limit int) ([]chatdom.MessageRef, error) {
	q := strings.Replace(messageRefSelect, "FALSE AS unread", "(cm.account_id IS NULL OR m.seq > cm.last_read_seq) AS unread", 1) + `
	FROM message_mentions mm JOIN messages m ON m.id = mm.message_id AND m.deleted_at IS NULL` + messageRefJoins + `
	WHERE mm.account_id = $1::uuid AND m.author_account_id <> $1::uuid`
	return r.listRefs(ctx, accountID, q, "m.created_at DESC, m.id", limit)
}

// ListThreads implements chatdom.Repository.
func (r *ChatRepository) ListThreads(ctx context.Context, accountID uuid.UUID, limit int) ([]chatdom.MessageRef, error) {
	q := `SELECT c.project_id::text AS project_id, p.name AS project_name,
	peer.id::text AS peer_id, peer.name AS peer_name, peer.kind AS peer_kind,
	c.id::text AS channel_id, c.name AS channel_name, c.kind, m.id::text AS message_id, m.seq,
	NULL::text AS thread_root_id, m.seq AS root_seq, COALESCE(last.author_id, m.author_account_id)::text AS author_id,
	COALESCE(last.author_name, a.name, '') AS author_name, COALESCE(last.content_text, m.content_text) AS preview,
	m.content_text AS root_preview, m.created_at, COALESCE(last.created_at, m.created_at) AS activity_at,
	(cnt.unread > 0) AS unread, cnt.unread AS unread_count, cnt.replies AS reply_count
	FROM thread_follows tf JOIN messages m ON m.id = tf.root_message_id AND m.deleted_at IS NULL` + messageRefJoins + `
	LEFT JOIN LATERAL (SELECT x.created_at, x.content_text, x.author_account_id AS author_id, xa.name AS author_name
		FROM messages x LEFT JOIN accounts xa ON xa.id = x.author_account_id
		WHERE x.thread_root_id = m.id AND x.deleted_at IS NULL ORDER BY x.seq DESC LIMIT 1) last ON TRUE
	LEFT JOIN LATERAL (SELECT COUNT(*) FILTER (WHERE y.seq > tf.last_read_seq AND y.author_account_id <> $1::uuid) AS unread, COUNT(*) AS replies
		FROM messages y WHERE y.thread_root_id = m.id AND y.deleted_at IS NULL) cnt ON TRUE
	WHERE tf.account_id = $1::uuid`
	return r.listRefs(ctx, accountID, q, "(cnt.unread > 0) DESC, COALESCE(last.created_at, m.created_at) DESC, m.id", limit)
}

// ListSaved implements chatdom.Repository.
func (r *ChatRepository) ListSaved(ctx context.Context, accountID uuid.UUID, limit int) ([]chatdom.MessageRef, error) {
	q := messageRefSelect + `
	FROM saved_messages sm JOIN messages m ON m.id = sm.message_id AND m.deleted_at IS NULL` + messageRefJoins + `
	WHERE sm.account_id = $1::uuid`
	return r.listRefs(ctx, accountID, q, "sm.created_at DESC, m.id", limit)
}

// SearchMessages implements chatdom.Repository. The text match is a trigram
// ILIKE on messages.content_text, the way document search works.
func (r *ChatRepository) SearchMessages(ctx context.Context, accountID uuid.UUID, query string, projectID *uuid.UUID, limit int) ([]chatdom.MessageRef, error) {
	q := messageRefSelect + `
	FROM messages m` + messageRefJoins + `
	WHERE m.deleted_at IS NULL AND m.content_text ILIKE '%' || $2::text || '%' ESCAPE '\'`
	args := []any{escapeLikePattern(query)}
	if projectID != nil {
		q += " AND c.project_id = $3::uuid"
		args = append(args, projectID.String())
	}
	return r.listRefsArgs(ctx, accountID, q, "m.created_at DESC, m.id", limit, args)
}

// SetStarred implements chatdom.Repository.
func (r *ChatRepository) SetStarred(ctx context.Context, accountID, channelID uuid.UUID, starred bool) error {
	if starred {
		// Starring follows: a member row at the channel head, then the flag.
		if _, err := r.db.ExecContext(ctx, `INSERT INTO channel_members (channel_id, account_id, last_read_seq, starred)
			SELECT id, $2::uuid, last_seq, TRUE FROM channels WHERE id = $1::uuid
			ON CONFLICT (channel_id, account_id) DO UPDATE SET starred = TRUE`, channelID.String(), accountID.String()); err != nil {
			return fmt.Errorf("chat: star: %w", err)
		}
		return nil
	}
	if _, err := r.db.ExecContext(ctx, `UPDATE channel_members SET starred = FALSE WHERE channel_id = $1::uuid AND account_id = $2::uuid`,
		channelID.String(), accountID.String()); err != nil {
		return fmt.Errorf("chat: unstar: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Direct messages with an agent
// ---------------------------------------------------------------------------

type agentLinkRecord struct {
	ChannelID      string  `db:"channel_id"`
	AgentID        string  `db:"agent_id"`
	AccountID      string  `db:"account_id"`
	SessionID      *string `db:"session_id"`
	ForwardedSeq   int64   `db:"forwarded_seq"`
	ConversationID *string `db:"conversation_id"`
	BridgedIndex   int     `db:"bridged_index"`
}

const agentLinkCols = `channel_id::text, agent_id::text, account_id::text, session_id::text, forwarded_seq,
	conversation_id::text, bridged_index`

func (a agentLinkRecord) entity() *chatdom.AgentLink {
	l := &chatdom.AgentLink{
		ChannelID: uuid.MustParse(a.ChannelID), AgentID: uuid.MustParse(a.AgentID),
		AccountID: uuid.MustParse(a.AccountID), ForwardedSeq: a.ForwardedSeq, BridgedIndex: a.BridgedIndex,
	}
	if a.SessionID != nil {
		id := uuid.MustParse(*a.SessionID)
		l.SessionID = &id
	}
	if a.ConversationID != nil {
		id := uuid.MustParse(*a.ConversationID)
		l.ConversationID = &id
	}
	return l
}

// EnsureAgentLink implements chatdom.Repository.
func (r *ChatRepository) EnsureAgentLink(ctx context.Context, channelID, agentID, accountID uuid.UUID) (*chatdom.AgentLink, error) {
	if _, err := r.db.ExecContext(ctx, `INSERT INTO chat_agent_dms (channel_id, agent_id, account_id)
		VALUES ($1::uuid, $2::uuid, $3::uuid) ON CONFLICT (channel_id) DO NOTHING`,
		channelID.String(), agentID.String(), accountID.String()); err != nil {
		return nil, fmt.Errorf("chat: ensure agent link: %w", err)
	}
	var rec agentLinkRecord
	if err := r.db.GetContext(ctx, &rec, `SELECT `+agentLinkCols+` FROM chat_agent_dms WHERE channel_id = $1::uuid`, channelID.String()); err != nil {
		return nil, fmt.Errorf("chat: find agent link: %w", err)
	}
	return rec.entity(), nil
}

// FindAgentLinkBySession implements chatdom.Repository.
func (r *ChatRepository) FindAgentLinkBySession(ctx context.Context, sessionID uuid.UUID) (*chatdom.AgentLink, error) {
	var rec agentLinkRecord
	err := r.db.GetContext(ctx, &rec, `SELECT `+agentLinkCols+` FROM chat_agent_dms WHERE session_id = $1::uuid`, sessionID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("chat: find agent link by session: %w", err)
	}
	return rec.entity(), nil
}

// ClaimForward implements chatdom.Repository.
func (r *ChatRepository) ClaimForward(ctx context.Context, channelID uuid.UUID, from, to int64) (bool, error) {
	res, err := r.db.ExecContext(ctx, `UPDATE chat_agent_dms SET forwarded_seq = $3, updated_at = NOW()
		WHERE channel_id = $1::uuid AND forwarded_seq = $2`, channelID.String(), from, to)
	if err != nil {
		return false, fmt.Errorf("chat: claim forward: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// ReleaseForward implements chatdom.Repository.
func (r *ChatRepository) ReleaseForward(ctx context.Context, channelID uuid.UUID, from, to int64) error {
	if _, err := r.db.ExecContext(ctx, `UPDATE chat_agent_dms SET forwarded_seq = $2, updated_at = NOW()
		WHERE channel_id = $1::uuid AND forwarded_seq = $3`, channelID.String(), from, to); err != nil {
		return fmt.Errorf("chat: release forward: %w", err)
	}
	return nil
}

// MessagesToForward implements chatdom.Repository.
func (r *ChatRepository) MessagesToForward(ctx context.Context, channelID, authorID uuid.UUID, after int64) ([]*chatdom.Message, error) {
	var recs []messageRecord
	if err := r.db.SelectContext(ctx, &recs, `SELECT `+messageCols+` FROM messages m LEFT JOIN accounts a ON a.id = m.author_account_id
		WHERE m.channel_id = $1::uuid AND m.author_account_id = $2::uuid AND m.seq > $3
		  AND m.thread_root_id IS NULL AND m.deleted_at IS NULL ORDER BY m.seq`,
		channelID.String(), authorID.String(), after); err != nil {
		return nil, fmt.Errorf("chat: messages to forward: %w", err)
	}
	out := make([]*chatdom.Message, len(recs))
	for i, rec := range recs {
		out[i] = rec.entity()
	}
	return out, nil
}

// SetAgentRun implements chatdom.Repository.
func (r *ChatRepository) SetAgentRun(ctx context.Context, channelID, sessionID, conversationID uuid.UUID) error {
	if _, err := r.db.ExecContext(ctx, `UPDATE chat_agent_dms SET session_id = $2::uuid,
		bridged_index = CASE WHEN conversation_id IS DISTINCT FROM $3::uuid THEN -1 ELSE bridged_index END,
		conversation_id = $3::uuid, updated_at = NOW() WHERE channel_id = $1::uuid`,
		channelID.String(), sessionID.String(), conversationID.String()); err != nil {
		return fmt.Errorf("chat: set agent run: %w", err)
	}
	return nil
}

// SetBridgedIndex implements chatdom.Repository.
func (r *ChatRepository) SetBridgedIndex(ctx context.Context, channelID uuid.UUID, index int) error {
	if _, err := r.db.ExecContext(ctx, `UPDATE chat_agent_dms SET bridged_index = GREATEST(bridged_index, $2), updated_at = NOW()
		WHERE channel_id = $1::uuid`, channelID.String(), index); err != nil {
		return fmt.Errorf("chat: set bridged index: %w", err)
	}
	return nil
}

// AgentReply implements chatdom.Repository.
func (r *ChatRepository) AgentReply(ctx context.Context, conversationID uuid.UUID, after, upTo int) (string, error) {
	var parts []string
	if err := r.db.SelectContext(ctx, &parts, `SELECT COALESCE(payload->'content'->>'text', '')
		FROM agent_conversation_events
		WHERE conversation_id = $1::uuid AND event_index > $2 AND event_index <= $3 AND event_type = 'agent_message_chunk'
		ORDER BY event_index`, conversationID.String(), after, upTo); err != nil {
		return "", fmt.Errorf("chat: agent reply: %w", err)
	}
	return strings.TrimSpace(strings.Join(parts, "")), nil
}

// ListAgentRuns implements chatdom.Repository. It lists the runs of the link's
// chat session and the runs an @mention of the agent by the same person
// started in a channel, newest first.
func (r *ChatRepository) ListAgentRuns(ctx context.Context, link *chatdom.AgentLink, limit int) ([]chatdom.AgentRun, error) {
	if link == nil {
		return nil, nil
	}
	var session *string
	if link.SessionID != nil {
		s := link.SessionID.String()
		session = &s
	}
	var rows []struct {
		ID          string    `db:"id"`
		Title       string    `db:"title"`
		ChannelName string    `db:"channel_name"`
		Status      string    `db:"status"`
		CreatedAt   time.Time `db:"created_at"`
		UpdatedAt   time.Time `db:"updated_at"`
	}
	if err := r.db.SelectContext(ctx, &rows, `
		SELECT id, title, channel_name, status, created_at, updated_at FROM (
			SELECT ac.id::text AS id, COALESCE(ac.title, '') AS title, ''::text AS channel_name, ac.status, ac.created_at, ac.updated_at
			FROM agent_conversations ac WHERE ac.chat_session_id = $1::uuid AND ac.deleted_at IS NULL
			UNION ALL
			SELECT ac.id::text, COALESCE(ac.title, ''), ch.name, ac.status, ac.created_at, ac.updated_at
			FROM chat_agent_runs r JOIN agent_conversations ac ON ac.id = r.conversation_id AND ac.deleted_at IS NULL
			JOIN channels ch ON ch.id = r.channel_id AND ch.archived_at IS NULL
			WHERE r.agent_id = $2::uuid AND r.requester_id = $3::uuid
			  AND (ch.kind = 'public' OR EXISTS (SELECT 1 FROM channel_members cm WHERE cm.channel_id = ch.id AND cm.account_id = $3::uuid))
			  AND EXISTS (SELECT 1 FROM project_members pm WHERE pm.project_id = ch.project_id AND pm.account_id = $3::uuid AND pm.deleted_at IS NULL)
		) runs ORDER BY created_at DESC, id LIMIT $4`,
		session, link.AgentID.String(), link.AccountID.String(), limit); err != nil {
		return nil, fmt.Errorf("chat: list agent runs: %w", err)
	}
	out := make([]chatdom.AgentRun, len(rows))
	for i, row := range rows {
		out[i] = chatdom.AgentRun{ID: uuid.MustParse(row.ID), Title: row.Title, ChannelName: row.ChannelName, Status: row.Status, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
	}
	return out, nil
}

// ResetAgentRun implements chatdom.Repository: the next message starts a new
// chat session. What was already forwarded stays forwarded.
func (r *ChatRepository) ResetAgentRun(ctx context.Context, channelID uuid.UUID) error {
	if _, err := r.db.ExecContext(ctx, `UPDATE chat_agent_dms SET session_id = NULL, conversation_id = NULL,
		bridged_index = -1, updated_at = NOW() WHERE channel_id = $1::uuid`, channelID.String()); err != nil {
		return fmt.Errorf("chat: reset agent run: %w", err)
	}
	return nil
}

// ProjectMemberID implements chatdom.Repository.
func (r *ChatRepository) ProjectMemberID(ctx context.Context, projectID, accountID uuid.UUID) (uuid.UUID, bool, error) {
	var id string
	err := r.db.GetContext(ctx, &id, `SELECT id::text FROM project_members
		WHERE project_id = $1::uuid AND account_id = $2::uuid AND deleted_at IS NULL`, projectID.String(), accountID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("chat: project member: %w", err)
	}
	return uuid.MustParse(id), true, nil
}

// AgentAccountOfMember implements chatdom.Repository.
func (r *ChatRepository) AgentAccountOfMember(ctx context.Context, projectID, memberID uuid.UUID) (uuid.UUID, bool, error) {
	var id string
	err := r.db.GetContext(ctx, &id, `SELECT pm.account_id::text FROM project_members pm JOIN accounts a ON a.id = pm.account_id
		WHERE pm.id = $1::uuid AND pm.project_id = $2::uuid AND pm.deleted_at IS NULL AND a.kind = 'agent'`, memberID.String(), projectID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("chat: agent of member: %w", err)
	}
	return uuid.MustParse(id), true, nil
}

// CreateAgentRun implements chatdom.Repository.
func (r *ChatRepository) CreateAgentRun(ctx context.Context, a *chatdom.ChannelAgentRun) error {
	if _, err := r.db.ExecContext(ctx, `INSERT INTO chat_agent_runs
		(conversation_id, channel_id, source_message_id, thread_root_id, agent_id, requester_id)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6::uuid) ON CONFLICT (conversation_id) DO NOTHING`,
		a.ConversationID.String(), a.ChannelID.String(), a.SourceMessageID.String(), a.ThreadRootID.String(),
		a.AgentID.String(), a.RequesterID.String()); err != nil {
		return fmt.Errorf("chat: create agent run: %w", err)
	}
	return nil
}

// FindAgentRun implements chatdom.Repository.
func (r *ChatRepository) FindAgentRun(ctx context.Context, conversationID uuid.UUID) (*chatdom.ChannelAgentRun, error) {
	var row struct {
		ConversationID  string `db:"conversation_id"`
		ChannelID       string `db:"channel_id"`
		SourceMessageID string `db:"source_message_id"`
		ThreadRootID    string `db:"thread_root_id"`
		AgentID         string `db:"agent_id"`
		RequesterID     string `db:"requester_id"`
		Posted          bool   `db:"posted"`
		ProjectID       string `db:"project_id"`
	}
	err := r.db.GetContext(ctx, &row, `SELECT r.conversation_id::text, r.channel_id::text, r.source_message_id::text, r.thread_root_id::text,
		r.agent_id::text, r.requester_id::text, r.posted, COALESCE(c.project_id::text, '') AS project_id
		FROM chat_agent_runs r JOIN channels c ON c.id = r.channel_id WHERE r.conversation_id = $1::uuid`, conversationID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("chat: find agent run: %w", err)
	}
	project, _ := uuid.Parse(row.ProjectID)
	return &chatdom.ChannelAgentRun{
		ConversationID: uuid.MustParse(row.ConversationID), ProjectID: project, ChannelID: uuid.MustParse(row.ChannelID),
		SourceMessageID: uuid.MustParse(row.SourceMessageID), ThreadRootID: uuid.MustParse(row.ThreadRootID),
		AgentID: uuid.MustParse(row.AgentID), RequesterID: uuid.MustParse(row.RequesterID), Posted: row.Posted,
	}, nil
}

// agentRunPostLease is how long a claim holds before another delivery may take
// it over: a worker that died between claiming and posting must not lose the
// answer for good.
const agentRunPostLease = "2 minutes"

// ClaimAgentRunPost implements chatdom.Repository.
func (r *ChatRepository) ClaimAgentRunPost(ctx context.Context, conversationID uuid.UUID) (bool, error) {
	res, err := r.db.ExecContext(ctx, `UPDATE chat_agent_runs SET post_claimed_at = NOW()
		WHERE conversation_id = $1::uuid AND NOT posted
		  AND (post_claimed_at IS NULL OR post_claimed_at < NOW() - INTERVAL '`+agentRunPostLease+`')`, conversationID.String())
	if err != nil {
		return false, fmt.Errorf("chat: claim agent run post: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// ReleaseAgentRunPost implements chatdom.Repository.
func (r *ChatRepository) ReleaseAgentRunPost(ctx context.Context, conversationID uuid.UUID) error {
	if _, err := r.db.ExecContext(ctx, `UPDATE chat_agent_runs SET post_claimed_at = NULL WHERE conversation_id = $1::uuid AND NOT posted`, conversationID.String()); err != nil {
		return fmt.Errorf("chat: release agent run post: %w", err)
	}
	return nil
}

// MarkAgentRunPosted implements chatdom.Repository.
func (r *ChatRepository) MarkAgentRunPosted(ctx context.Context, conversationID uuid.UUID) error {
	if _, err := r.db.ExecContext(ctx, `UPDATE chat_agent_runs SET posted = TRUE WHERE conversation_id = $1::uuid`, conversationID.String()); err != nil {
		return fmt.Errorf("chat: mark agent run posted: %w", err)
	}
	return nil
}

// AgentReplyStored implements chatdom.Repository.
func (r *ChatRepository) AgentReplyStored(ctx context.Context, channelID, conversationID uuid.UUID) (bool, error) {
	var ok bool
	if err := r.db.GetContext(ctx, &ok, `SELECT EXISTS (SELECT 1 FROM messages WHERE channel_id = $1::uuid AND run_id = $2::uuid AND deleted_at IS NULL)`,
		channelID.String(), conversationID.String()); err != nil {
		return false, fmt.Errorf("chat: agent reply stored: %w", err)
	}
	return ok, nil
}

// CountRecentAgentRuns implements chatdom.Repository.
func (r *ChatRepository) CountRecentAgentRuns(ctx context.Context, requesterID uuid.UUID, since time.Time) (int, error) {
	var n int
	if err := r.db.GetContext(ctx, &n, `SELECT COUNT(*) FROM chat_agent_runs WHERE requester_id = $1::uuid AND created_at >= $2`,
		requesterID.String(), since); err != nil {
		return 0, fmt.Errorf("chat: count agent runs: %w", err)
	}
	return n, nil
}

// IsGlobalAgent implements chatdom.Repository.
func (r *ChatRepository) IsGlobalAgent(ctx context.Context, accountID uuid.UUID) (bool, error) {
	var ok bool
	if err := r.db.GetContext(ctx, &ok, `SELECT EXISTS (SELECT 1 FROM agents WHERE id = $1::uuid AND project_id IS NULL)`, accountID.String()); err != nil {
		return false, fmt.Errorf("chat: is global agent: %w", err)
	}
	return ok, nil
}
