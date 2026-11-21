package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
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
func (r *CallRepository) ParticipantJoined(ctx context.Context, channelID, accountID uuid.UUID) error {
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		var sid string
		if err := tx.GetContext(ctx, &sid, `INSERT INTO call_sessions (channel_id, started_by) VALUES ($1::uuid, $2::uuid)
			ON CONFLICT (channel_id) WHERE ended_at IS NULL DO UPDATE SET channel_id = call_sessions.channel_id
			RETURNING id`, channelID.String(), accountID.String()); err != nil {
			return fmt.Errorf("calls: open session: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO call_participants (session_id, account_id) VALUES ($1::uuid, $2::uuid)
			ON CONFLICT (session_id, account_id) WHERE left_at IS NULL DO NOTHING`, sid, accountID.String()); err != nil {
			return fmt.Errorf("calls: participant joined: %w", err)
		}
		return nil
	})
}

// ParticipantLeft implements calldom.Repository.
func (r *CallRepository) ParticipantLeft(ctx context.Context, channelID, accountID uuid.UUID) error {
	if _, err := r.db.ExecContext(ctx, `UPDATE call_participants SET left_at = NOW()
		WHERE account_id = $2::uuid AND left_at IS NULL
		  AND session_id IN (SELECT id FROM call_sessions WHERE channel_id = $1::uuid AND ended_at IS NULL)`,
		channelID.String(), accountID.String()); err != nil {
		return fmt.Errorf("calls: participant left: %w", err)
	}
	return nil
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
