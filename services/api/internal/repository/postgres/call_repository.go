package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"

	calldom "github.com/Paca-AI/api/internal/domain/call"
	chatdom "github.com/Paca-AI/api/internal/domain/chat"
)

// CallRepository implements calldom.Repository on Postgres.
type CallRepository struct{ db *sqlx.DB }

// NewCallRepository returns a CallRepository.
func NewCallRepository(db *sqlx.DB) *CallRepository { return &CallRepository{db: db} }

var _ calldom.Repository = (*CallRepository)(nil)

// FindChannel implements calldom.Repository.
func (r *CallRepository) FindChannel(ctx context.Context, projectID, channelID uuid.UUID) (*calldom.Channel, error) {
	var archived bool
	err := r.db.GetContext(ctx, &archived, `SELECT archived_at IS NOT NULL FROM channels
		WHERE id = $1::uuid AND project_id IS NOT DISTINCT FROM $2::uuid`, channelID.String(), projectArg(projectID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, chatdom.ErrChannelNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("calls: find channel: %w", err)
	}
	return &calldom.Channel{ID: channelID, ProjectID: projectID, Archived: archived}, nil
}

// AccountName implements calldom.Repository.
func (r *CallRepository) AccountName(ctx context.Context, accountID uuid.UUID) (string, string, error) {
	var rec struct {
		Name string `db:"name"`
		Kind string `db:"kind"`
	}
	if err := r.db.GetContext(ctx, &rec, `SELECT COALESCE(name, '') AS name, kind FROM accounts WHERE id = $1::uuid`, accountID.String()); err != nil {
		return "", "", fmt.Errorf("calls: account name: %w", err)
	}
	return rec.Name, rec.Kind, nil
}

// OpenSession implements calldom.Repository.
func (r *CallRepository) OpenSession(ctx context.Context, channelID, startedBy uuid.UUID) (uuid.UUID, error) {
	var id string
	err := r.db.GetContext(ctx, &id, `INSERT INTO call_sessions (channel_id, started_by) VALUES ($1::uuid, $2::uuid)
		ON CONFLICT (channel_id) WHERE ended_at IS NULL DO UPDATE SET channel_id = call_sessions.channel_id
		RETURNING id`, channelID.String(), startedBy.String())
	if err != nil {
		return uuid.Nil, fmt.Errorf("calls: open session: %w", err)
	}
	return uuid.Parse(id)
}

// SetRoomSID implements calldom.Repository.
func (r *CallRepository) SetRoomSID(ctx context.Context, channelID uuid.UUID, sid string) error {
	if _, err := r.db.ExecContext(ctx, `UPDATE call_sessions SET room_sid = $2 WHERE channel_id = $1::uuid AND ended_at IS NULL`,
		channelID.String(), sid); err != nil {
		return fmt.Errorf("calls: set room sid: %w", err)
	}
	return nil
}

// ParticipantJoined implements calldom.Repository.
func (r *CallRepository) ParticipantJoined(ctx context.Context, channelID, accountID uuid.UUID, sid string) error {
	err := WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		var sessionID string
		if err := tx.GetContext(ctx, &sessionID, `INSERT INTO call_sessions (channel_id, started_by) VALUES ($1::uuid, $2::uuid)
			ON CONFLICT (channel_id) WHERE ended_at IS NULL DO UPDATE SET channel_id = call_sessions.channel_id
			RETURNING id`, channelID.String(), accountID.String()); err != nil {
			return fmt.Errorf("calls: open session: %w", err)
		}
		// A second connection of the same account (another tab) replaces the
		// first, so the row follows the newest connection.
		if _, err := tx.ExecContext(ctx, `INSERT INTO call_participants (session_id, account_id, sid) VALUES ($1::uuid, $2::uuid, $3)
			ON CONFLICT (session_id, account_id) WHERE left_at IS NULL DO UPDATE SET sid = EXCLUDED.sid
			WHERE EXCLUDED.sid <> ''`, sessionID, accountID.String(), sid); err != nil {
			return fmt.Errorf("calls: participant joined: %w", err)
		}
		return nil
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		// The channel (or account) is gone: nothing to record, and a retry
		// would fail the same way.
		return nil
	}
	return err
}

// ParticipantLeft implements calldom.Repository.
func (r *CallRepository) ParticipantLeft(ctx context.Context, channelID, accountID uuid.UUID, sid string) error {
	if _, err := r.db.ExecContext(ctx, `UPDATE call_participants SET left_at = NOW()
		WHERE account_id = $2::uuid AND left_at IS NULL AND ($3 = '' OR sid = $3 OR sid = '')
		  AND session_id IN (SELECT id FROM call_sessions WHERE channel_id = $1::uuid AND ended_at IS NULL)`,
		channelID.String(), accountID.String(), sid); err != nil {
		return fmt.Errorf("calls: participant left: %w", err)
	}
	return nil
}

// BanRemoved implements calldom.Repository.
func (r *CallRepository) BanRemoved(ctx context.Context, channelID, accountID uuid.UUID, until time.Time) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM call_removals WHERE expires_at < NOW()`); err != nil {
		return fmt.Errorf("calls: prune removals: %w", err)
	}
	if _, err := r.db.ExecContext(ctx, `INSERT INTO call_removals (channel_id, account_id, expires_at) VALUES ($1::uuid, $2::uuid, $3)
		ON CONFLICT (channel_id, account_id) DO UPDATE SET expires_at = EXCLUDED.expires_at`,
		channelID.String(), accountID.String(), until); err != nil {
		return fmt.Errorf("calls: bar removed: %w", err)
	}
	return nil
}

// IsRemoved implements calldom.Repository.
func (r *CallRepository) IsRemoved(ctx context.Context, channelID, accountID uuid.UUID) (bool, error) {
	var removed bool
	if err := r.db.GetContext(ctx, &removed, `SELECT EXISTS (SELECT 1 FROM call_removals
		WHERE channel_id = $1::uuid AND account_id = $2::uuid AND expires_at > NOW())`,
		channelID.String(), accountID.String()); err != nil {
		return false, fmt.Errorf("calls: is removed: %w", err)
	}
	return removed, nil
}

// EndSession implements calldom.Repository.
func (r *CallRepository) EndSession(ctx context.Context, channelID uuid.UUID) error {
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE call_participants SET left_at = NOW()
			WHERE left_at IS NULL
			  AND session_id IN (SELECT id FROM call_sessions WHERE channel_id = $1::uuid AND ended_at IS NULL)`,
			channelID.String()); err != nil {
			return fmt.Errorf("calls: close participants: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE call_sessions SET ended_at = NOW() WHERE channel_id = $1::uuid AND ended_at IS NULL`,
			channelID.String()); err != nil {
			return fmt.Errorf("calls: end session: %w", err)
		}
		return nil
	})
}

type callParticipantRecord struct {
	ChannelID string    `db:"channel_id"`
	StartedAt time.Time `db:"started_at"`
	AccountID string    `db:"account_id"`
	Name      string    `db:"name"`
	Kind      string    `db:"kind"`
	JoinedAt  time.Time `db:"joined_at"`
}

const callSelect = `SELECT s.channel_id, s.started_at, p.account_id, COALESCE(a.name, '') AS name, a.kind, p.joined_at
	FROM call_sessions s
	JOIN call_participants p ON p.session_id = s.id AND p.left_at IS NULL
	JOIN accounts a ON a.id = p.account_id
	JOIN channels c ON c.id = s.channel_id
	WHERE s.ended_at IS NULL`

func groupCalls(recs []callParticipantRecord) []*calldom.Call {
	byChannel := map[string]*calldom.Call{}
	var out []*calldom.Call
	for _, rec := range recs {
		c := byChannel[rec.ChannelID]
		if c == nil {
			id, _ := uuid.Parse(rec.ChannelID)
			c = &calldom.Call{ChannelID: id, StartedAt: rec.StartedAt, Participants: []calldom.Participant{}}
			byChannel[rec.ChannelID] = c
			out = append(out, c)
		}
		aid, _ := uuid.Parse(rec.AccountID)
		c.Participants = append(c.Participants, calldom.Participant{AccountID: aid, Name: rec.Name, Kind: rec.Kind, JoinedAt: rec.JoinedAt})
	}
	return out
}

// ListCall implements calldom.Repository.
func (r *CallRepository) ListCall(ctx context.Context, channelID uuid.UUID) (*calldom.Call, error) {
	var recs []callParticipantRecord
	if err := r.db.SelectContext(ctx, &recs, callSelect+` AND s.channel_id = $1::uuid ORDER BY p.joined_at`, channelID.String()); err != nil {
		return nil, fmt.Errorf("calls: list call: %w", err)
	}
	cs := groupCalls(recs)
	if len(cs) == 0 {
		return nil, nil
	}
	return cs[0], nil
}

// ListCalls implements calldom.Repository.
func (r *CallRepository) ListCalls(ctx context.Context, projectID uuid.UUID) ([]*calldom.Call, error) {
	sink := &argList{args: []any{projectID.String()}}
	clause, none, err := scopeSQL(ctx, "channel", channelScopeColumns, sink)
	if err != nil {
		return nil, err
	}
	if none {
		return []*calldom.Call{}, nil
	}
	q := callSelect + ` AND c.project_id = $1::uuid AND c.archived_at IS NULL`
	if clause != "" {
		q += ` AND ` + clause
	}
	q += ` ORDER BY s.started_at, p.joined_at`
	var recs []callParticipantRecord
	if err := r.db.SelectContext(ctx, &recs, q, sink.args...); err != nil {
		return nil, fmt.Errorf("calls: list calls: %w", err)
	}
	out := groupCalls(recs)
	if out == nil {
		out = []*calldom.Call{}
	}
	return out, nil
}

// ---------------------------------------------------------------- recordings

const recordingCols = `id, call_session_id, channel_id, egress_id, status, started_by, started_at, ended_at,
	object_key, size_bytes, duration_ms, error`

type recordingRecord struct {
	ID         string     `db:"id"`
	CallID     string     `db:"call_session_id"`
	ChannelID  string     `db:"channel_id"`
	EgressID   string     `db:"egress_id"`
	Status     string     `db:"status"`
	StartedBy  *string    `db:"started_by"`
	StartedAt  time.Time  `db:"started_at"`
	EndedAt    *time.Time `db:"ended_at"`
	ObjectKey  string     `db:"object_key"`
	SizeBytes  int64      `db:"size_bytes"`
	DurationMs int64      `db:"duration_ms"`
	Error      string     `db:"error"`
}

func (rec recordingRecord) entity() *calldom.Recording {
	id, _ := uuid.Parse(rec.ID)
	call, _ := uuid.Parse(rec.CallID)
	ch, _ := uuid.Parse(rec.ChannelID)
	return &calldom.Recording{ID: id, CallID: call, ChannelID: ch, EgressID: rec.EgressID, Status: rec.Status,
		StartedBy: optUUID(rec.StartedBy), StartedAt: rec.StartedAt, EndedAt: rec.EndedAt, ObjectKey: rec.ObjectKey,
		SizeBytes: rec.SizeBytes, DurationMs: rec.DurationMs, Error: rec.Error}
}

// CreateRecording implements calldom.Repository.
func (r *CallRepository) CreateRecording(ctx context.Context, channelID, startedBy uuid.UUID) (*calldom.Recording, error) {
	var rec recordingRecord
	err := r.db.GetContext(ctx, &rec, `
		INSERT INTO call_recordings (call_session_id, channel_id, started_by)
		SELECT cs.id, cs.channel_id, $2::uuid FROM call_sessions cs
		WHERE cs.channel_id = $1::uuid AND cs.ended_at IS NULL
		RETURNING `+recordingCols, channelID.String(), startedBy.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, calldom.ErrNoOpenCall
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return nil, calldom.ErrRecordingActive
	}
	if err != nil {
		return nil, fmt.Errorf("calls: create recording: %w", err)
	}
	return rec.entity(), nil
}

// SetRecordingEgress implements calldom.Repository.
func (r *CallRepository) SetRecordingEgress(ctx context.Context, id uuid.UUID, egressID string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE call_recordings SET egress_id = $2 WHERE id = $1::uuid`, id.String(), egressID)
	return err
}

// FailRecording implements calldom.Repository.
func (r *CallRepository) FailRecording(ctx context.Context, id uuid.UUID, reason string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE call_recordings SET status = 'failed', ended_at = NOW(), error = $2
		WHERE id = $1::uuid AND status IN ('starting', 'recording', 'stopping')`, id.String(), reason)
	return err
}

// UpdateRecordingByEgress implements calldom.Repository. A finished recording
// never goes back to a running state, whatever order the webhooks arrive in.
func (r *CallRepository) UpdateRecordingByEgress(ctx context.Context, egressID, status, objectKey string, sizeBytes, durationMs int64, errMsg string) (uuid.UUID, error) {
	var ch string
	err := r.db.GetContext(ctx, &ch, `
		UPDATE call_recordings SET status = $2,
			ended_at = CASE WHEN $2 IN ('complete', 'failed') THEN COALESCE(ended_at, NOW()) ELSE ended_at END,
			object_key = CASE WHEN $3 <> '' THEN $3 ELSE object_key END,
			size_bytes = CASE WHEN $4 > 0 THEN $4 ELSE size_bytes END,
			duration_ms = CASE WHEN $5 > 0 THEN $5 ELSE duration_ms END,
			error = CASE WHEN $6 <> '' THEN $6 ELSE error END
		WHERE egress_id = $1 AND egress_id <> '' AND status NOT IN ('complete', 'failed')
		RETURNING channel_id::text`, egressID, status, objectKey, sizeBytes, durationMs, errMsg)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, nil
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("calls: update recording: %w", err)
	}
	id, _ := uuid.Parse(ch)
	return id, nil
}

// OpenRecording implements calldom.Repository.
func (r *CallRepository) OpenRecording(ctx context.Context, channelID uuid.UUID) (*calldom.Recording, error) {
	var rec recordingRecord
	err := r.db.GetContext(ctx, &rec, `SELECT `+recordingCols+` FROM call_recordings
		WHERE channel_id = $1::uuid AND status IN ('starting', 'recording', 'stopping')
		ORDER BY started_at DESC LIMIT 1`, channelID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("calls: open recording: %w", err)
	}
	return rec.entity(), nil
}

// ListRecordings implements calldom.Repository.
func (r *CallRepository) ListRecordings(ctx context.Context, channelID uuid.UUID, limit int) ([]*calldom.Recording, error) {
	var recs []recordingRecord
	if err := r.db.SelectContext(ctx, &recs, `SELECT `+recordingCols+` FROM call_recordings
		WHERE channel_id = $1::uuid ORDER BY started_at DESC LIMIT $2`, channelID.String(), limit); err != nil {
		return nil, fmt.Errorf("calls: list recordings: %w", err)
	}
	out := make([]*calldom.Recording, len(recs))
	for i, rec := range recs {
		out[i] = rec.entity()
	}
	return out, nil
}

// FindRecording implements calldom.Repository.
func (r *CallRepository) FindRecording(ctx context.Context, channelID, id uuid.UUID) (*calldom.Recording, error) {
	var rec recordingRecord
	err := r.db.GetContext(ctx, &rec, `SELECT `+recordingCols+` FROM call_recordings
		WHERE id = $1::uuid AND channel_id = $2::uuid`, id.String(), channelID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, calldom.ErrRecordingNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("calls: find recording: %w", err)
	}
	return rec.entity(), nil
}
