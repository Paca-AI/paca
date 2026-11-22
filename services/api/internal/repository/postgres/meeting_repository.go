package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	meetingdom "github.com/Paca-AI/api/internal/domain/meeting"
)

// MeetingRepository implements meetingdom.Repository on Postgres.
type MeetingRepository struct{ db *sqlx.DB }

// NewMeetingRepository returns a MeetingRepository.
func NewMeetingRepository(db *sqlx.DB) *MeetingRepository { return &MeetingRepository{db: db} }

var _ meetingdom.Repository = (*MeetingRepository)(nil)

func optUUID(s *string) *uuid.UUID {
	if s == nil {
		return nil
	}
	id, err := uuid.Parse(*s)
	if err != nil {
		return nil
	}
	return &id
}

func uuidStrPtr(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}

// AgentInfo implements meetingdom.Repository: the agent must be live and
// visible in the project (its own, or a global agent invited into it).
func (r *MeetingRepository) AgentInfo(ctx context.Context, projectID, agentID uuid.UUID) (*meetingdom.AgentInfo, error) {
	var rec struct {
		Name   string `db:"name"`
		Type   string `db:"agent_type"`
		Prompt string `db:"system_prompt"`
		LLMP   string `db:"llm_provider"`
		LLMM   string `db:"llm_model"`
		LLMK   string `db:"llm_api_key_secret"`
		LLMU   string `db:"llm_base_url"`
	}
	err := r.db.GetContext(ctx, &rec, `
		SELECT COALESCE(ac.name, '') AS name, a.agent_type, a.system_prompt, a.llm_provider, a.llm_model,
		       a.llm_api_key_secret, a.llm_base_url
		FROM agents a JOIN accounts ac ON ac.id = a.id AND ac.deleted_at IS NULL
		WHERE a.id = $1::uuid
		  AND (a.project_id = $2::uuid OR EXISTS (SELECT 1 FROM project_members pm
		        WHERE pm.account_id = a.id AND pm.project_id = $2::uuid AND pm.deleted_at IS NULL))`,
		agentID.String(), projectID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, agentdom.ErrAgentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("meeting: agent info: %w", err)
	}
	return &meetingdom.AgentInfo{ID: agentID, Name: rec.Name, Type: rec.Type, Instructions: rec.Prompt,
		LLMProvider: rec.LLMP, LLMModel: rec.LLMM, LLMKeySecret: rec.LLMK, LLMBaseURL: rec.LLMU}, nil
}

type meetingSettingsRecord struct {
	AgentID                 string    `db:"agent_id"`
	STTProvider             string    `db:"stt_provider"`
	STTModel                string    `db:"stt_model"`
	STTAPIKeySecret         string    `db:"stt_api_key_secret"`
	STTBaseURL              string    `db:"stt_base_url"`
	STTLanguages            []byte    `db:"stt_languages"`
	Mode                    string    `db:"mode"`
	SuggestIntervalSeconds  int       `db:"suggest_interval_seconds"`
	ProposalKinds           []byte    `db:"proposal_kinds"`
	MaxProposalsPerCall     int       `db:"max_proposals_per_call"`
	StoreTranscript         bool      `db:"store_transcript"`
	TranscriptRetentionDays int       `db:"transcript_retention_days"`
	Announcement            string    `db:"announcement"`
	SummaryTarget           string    `db:"summary_target"`
	DefaultTarget           string    `db:"default_target"`
	UpdatedAt               time.Time `db:"updated_at"`
}

// GetSettings implements meetingdom.Repository; an agent with no row gets the
// defaults.
func (r *MeetingRepository) GetSettings(ctx context.Context, agentID uuid.UUID) (*meetingdom.Settings, error) {
	var rec meetingSettingsRecord
	err := r.db.GetContext(ctx, &rec, `SELECT agent_id, stt_provider, stt_model, stt_api_key_secret, stt_base_url,
		to_jsonb(stt_languages) AS stt_languages, mode, suggest_interval_seconds, to_jsonb(proposal_kinds) AS proposal_kinds,
		max_proposals_per_call, store_transcript,
		transcript_retention_days, announcement, summary_target, default_target, updated_at
		FROM agent_meeting_settings WHERE agent_id = $1::uuid`, agentID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return meetingdom.DefaultSettings(agentID), nil
	}
	if err != nil {
		return nil, fmt.Errorf("meeting: get settings: %w", err)
	}
	var langs, kinds []string
	if err := json.Unmarshal(rec.STTLanguages, &langs); err != nil || langs == nil {
		langs = []string{}
	}
	if err := json.Unmarshal(rec.ProposalKinds, &kinds); err != nil || kinds == nil {
		kinds = []string{}
	}
	return &meetingdom.Settings{
		AgentID: agentID, STTProvider: rec.STTProvider, STTModel: rec.STTModel, STTAPIKeySecret: rec.STTAPIKeySecret,
		STTBaseURL: rec.STTBaseURL, STTLanguages: langs, Mode: rec.Mode, SuggestIntervalSeconds: rec.SuggestIntervalSeconds,
		ProposalKinds: kinds, MaxProposalsPerCall: rec.MaxProposalsPerCall,
		StoreTranscript: rec.StoreTranscript, TranscriptRetentionDays: rec.TranscriptRetentionDays,
		Announcement: rec.Announcement, SummaryTarget: rec.SummaryTarget, DefaultTarget: rec.DefaultTarget, UpdatedAt: rec.UpdatedAt,
	}, nil
}

// SaveSettings implements meetingdom.Repository.
func (r *MeetingRepository) SaveSettings(ctx context.Context, s *meetingdom.Settings) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO agent_meeting_settings (agent_id, stt_provider, stt_model, stt_api_key_secret, stt_base_url,
			stt_languages, mode, suggest_interval_seconds, proposal_kinds, max_proposals_per_call, store_transcript,
			transcript_retention_days, announcement, summary_target, default_target, updated_at)
		VALUES ($1::uuid,$2,$3,$4,$5,ARRAY(SELECT jsonb_array_elements_text($6::jsonb)),$7,$8,
			ARRAY(SELECT jsonb_array_elements_text($9::jsonb)),$10,$11,$12,$13,$14,$15,NOW())
		ON CONFLICT (agent_id) DO UPDATE SET stt_provider=EXCLUDED.stt_provider, stt_model=EXCLUDED.stt_model,
			stt_api_key_secret=EXCLUDED.stt_api_key_secret, stt_base_url=EXCLUDED.stt_base_url,
			stt_languages=EXCLUDED.stt_languages, mode=EXCLUDED.mode, suggest_interval_seconds=EXCLUDED.suggest_interval_seconds,
			proposal_kinds=EXCLUDED.proposal_kinds, max_proposals_per_call=EXCLUDED.max_proposals_per_call,
			store_transcript=EXCLUDED.store_transcript, transcript_retention_days=EXCLUDED.transcript_retention_days,
			announcement=EXCLUDED.announcement, summary_target=EXCLUDED.summary_target,
			default_target=EXCLUDED.default_target, updated_at=NOW()`,
		s.AgentID.String(), s.STTProvider, s.STTModel, s.STTAPIKeySecret, s.STTBaseURL,
		jsonList(s.STTLanguages), s.Mode, s.SuggestIntervalSeconds, jsonList(s.ProposalKinds),
		s.MaxProposalsPerCall, s.StoreTranscript, s.TranscriptRetentionDays, s.Announcement, s.SummaryTarget, s.DefaultTarget)
	if err != nil {
		return fmt.Errorf("meeting: save settings: %w", err)
	}
	return nil
}

// jsonList encodes a string list as JSON text, which the queries turn back into
// a text[] (and which needs no array support from the driver).
func jsonList(v []string) string {
	if v == nil {
		v = []string{}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// OpenCall implements meetingdom.Repository.
func (r *MeetingRepository) OpenCall(ctx context.Context, channelID uuid.UUID) (uuid.UUID, *uuid.UUID, error) {
	var rec struct {
		ID        string  `db:"id"`
		StartedBy *string `db:"started_by"`
	}
	err := r.db.GetContext(ctx, &rec, `SELECT id, started_by FROM call_sessions WHERE channel_id = $1::uuid AND ended_at IS NULL`, channelID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, nil, nil
	}
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("meeting: open call: %w", err)
	}
	id, _ := uuid.Parse(rec.ID)
	return id, optUUID(rec.StartedBy), nil
}

// ChannelProject implements meetingdom.Repository.
func (r *MeetingRepository) ChannelProject(ctx context.Context, channelID uuid.UUID) (uuid.UUID, bool, error) {
	var p *string
	err := r.db.GetContext(ctx, &p, `SELECT project_id::text FROM channels WHERE id = $1::uuid`, channelID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("meeting: channel project: %w", err)
	}
	if p == nil {
		return uuid.Nil, true, nil
	}
	id, _ := uuid.Parse(*p)
	return id, true, nil
}

// IsCallParticipant implements meetingdom.Repository.
func (r *MeetingRepository) IsCallParticipant(ctx context.Context, callID, accountID uuid.UUID) (bool, error) {
	var ok bool
	err := r.db.GetContext(ctx, &ok, `SELECT EXISTS (SELECT 1 FROM call_participants
		WHERE session_id = $1::uuid AND account_id = $2::uuid AND left_at IS NULL)`, callID.String(), accountID.String())
	if err != nil {
		return false, fmt.Errorf("meeting: is participant: %w", err)
	}
	return ok, nil
}

// WasCallParticipant implements meetingdom.Repository.
func (r *MeetingRepository) WasCallParticipant(ctx context.Context, callID, accountID uuid.UUID) (bool, error) {
	var ok bool
	err := r.db.GetContext(ctx, &ok, `SELECT EXISTS (SELECT 1 FROM call_participants
		WHERE session_id = $1::uuid AND account_id = $2::uuid)`, callID.String(), accountID.String())
	if err != nil {
		return false, fmt.Errorf("meeting: was participant: %w", err)
	}
	return ok, nil
}

const agentSessionCols = `s.id, s.call_session_id, cs.channel_id, COALESCE(c.project_id::text, '') AS project_id, s.agent_id,
	COALESCE(ac.name, '') AS agent_name, s.invited_by, s.dispatch_id, s.status, s.joined_at, s.left_at,
	COALESCE(ms.mode, 'notes_only') AS mode, COALESCE(ms.summary_target, 'none') AS summary_target, COALESCE(ms.store_transcript, TRUE) AS store_transcript,
	COALESCE(ms.transcript_retention_days, 30) AS retention_days, COALESCE(ms.announcement, '') AS announcement`

const agentSessionFrom = `call_agent_sessions s
	JOIN call_sessions cs ON cs.id = s.call_session_id
	JOIN channels c ON c.id = cs.channel_id
	JOIN accounts ac ON ac.id = s.agent_id
	LEFT JOIN agent_meeting_settings ms ON ms.agent_id = s.agent_id`

type agentSessionRecord struct {
	ID            string     `db:"id"`
	CallSessionID string     `db:"call_session_id"`
	ChannelID     string     `db:"channel_id"`
	ProjectID     string     `db:"project_id"`
	AgentID       string     `db:"agent_id"`
	AgentName     string     `db:"agent_name"`
	InvitedBy     *string    `db:"invited_by"`
	DispatchID    string     `db:"dispatch_id"`
	Status        string     `db:"status"`
	JoinedAt      time.Time  `db:"joined_at"`
	LeftAt        *time.Time `db:"left_at"`
	Mode          string     `db:"mode"`
	Summary       string     `db:"summary_target"`
	Store         bool       `db:"store_transcript"`
	Retention     int        `db:"retention_days"`
	Announcement  string     `db:"announcement"`
}

func (rec agentSessionRecord) entity() *meetingdom.AgentSession {
	id, _ := uuid.Parse(rec.ID)
	call, _ := uuid.Parse(rec.CallSessionID)
	ch, _ := uuid.Parse(rec.ChannelID)
	proj, _ := uuid.Parse(rec.ProjectID)
	agent, _ := uuid.Parse(rec.AgentID)
	return &meetingdom.AgentSession{ID: id, CallSessionID: call, ChannelID: ch, ProjectID: proj, AgentID: agent,
		AgentName: rec.AgentName, InvitedBy: optUUID(rec.InvitedBy), DispatchID: rec.DispatchID, Status: rec.Status,
		JoinedAt: rec.JoinedAt, LeftAt: rec.LeftAt, Mode: rec.Mode, SummaryTarget: rec.Summary, StoreTranscript: rec.Store,
		RetentionDays: rec.Retention, Announcement: rec.Announcement}
}

// CreateAgentSession implements meetingdom.Repository; a second open session of
// the same assistant in the same call is ErrAlreadyInCall.
func (r *MeetingRepository) CreateAgentSession(ctx context.Context, s *meetingdom.AgentSession) error {
	res, err := r.db.ExecContext(ctx, `INSERT INTO call_agent_sessions (id, call_session_id, agent_id, invited_by, status)
		VALUES ($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5)
		ON CONFLICT (call_session_id, agent_id) WHERE left_at IS NULL DO NOTHING`,
		s.ID.String(), s.CallSessionID.String(), s.AgentID.String(), uuidStrPtr(s.InvitedBy), meetingdom.SessionDispatched)
	if err != nil {
		return fmt.Errorf("meeting: create agent session: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return meetingdom.ErrAlreadyInCall
	}
	return nil
}

// SetAgentSessionDispatch implements meetingdom.Repository.
func (r *MeetingRepository) SetAgentSessionDispatch(ctx context.Context, id uuid.UUID, dispatchID string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE call_agent_sessions SET dispatch_id = $2 WHERE id = $1::uuid`, id.String(), dispatchID)
	return err
}

// SetAgentSessionStatus implements meetingdom.Repository. "left" and "failed"
// close the session.
func (r *MeetingRepository) SetAgentSessionStatus(ctx context.Context, id uuid.UUID, status string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE call_agent_sessions SET status = $2,
		left_at = CASE WHEN $2 IN ('left','failed') THEN COALESCE(left_at, NOW()) ELSE left_at END
		WHERE id = $1::uuid AND left_at IS NULL`, id.String(), status)
	return err
}

// FindAgentSession implements meetingdom.Repository.
func (r *MeetingRepository) FindAgentSession(ctx context.Context, id uuid.UUID) (*meetingdom.AgentSession, error) {
	var rec agentSessionRecord
	err := r.db.GetContext(ctx, &rec, `SELECT `+agentSessionCols+` FROM `+agentSessionFrom+` WHERE s.id = $1::uuid`, id.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, meetingdom.ErrNotInCall
	}
	if err != nil {
		return nil, fmt.Errorf("meeting: find agent session: %w", err)
	}
	return rec.entity(), nil
}

// OpenAgentSession implements meetingdom.Repository.
func (r *MeetingRepository) OpenAgentSession(ctx context.Context, channelID, agentID uuid.UUID) (*meetingdom.AgentSession, error) {
	var rec agentSessionRecord
	err := r.db.GetContext(ctx, &rec, `SELECT `+agentSessionCols+` FROM `+agentSessionFrom+`
		WHERE cs.channel_id = $1::uuid AND cs.ended_at IS NULL AND s.agent_id = $2::uuid AND s.left_at IS NULL`,
		channelID.String(), agentID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, meetingdom.ErrNotInCall
	}
	if err != nil {
		return nil, fmt.Errorf("meeting: open agent session: %w", err)
	}
	return rec.entity(), nil
}

// ListAgentSessions implements meetingdom.Repository.
func (r *MeetingRepository) ListAgentSessions(ctx context.Context, channelID uuid.UUID) ([]*meetingdom.AgentSession, error) {
	var recs []agentSessionRecord
	err := r.db.SelectContext(ctx, &recs, `SELECT `+agentSessionCols+` FROM `+agentSessionFrom+`
		WHERE cs.channel_id = $1::uuid AND cs.ended_at IS NULL AND s.left_at IS NULL ORDER BY s.joined_at`, channelID.String())
	if err != nil {
		return nil, fmt.Errorf("meeting: list agent sessions: %w", err)
	}
	out := make([]*meetingdom.AgentSession, len(recs))
	for i, rec := range recs {
		out[i] = rec.entity()
	}
	return out, nil
}

// EndAgentSessions implements meetingdom.Repository.
func (r *MeetingRepository) EndAgentSessions(ctx context.Context, callID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `UPDATE call_agent_sessions SET status = 'left', left_at = NOW()
		WHERE call_session_id = $1::uuid AND left_at IS NULL`, callID.String())
	return err
}

// ListMeetingAgents implements meetingdom.Repository.
func (r *MeetingRepository) ListMeetingAgents(ctx context.Context, projectID uuid.UUID) ([]*meetingdom.MeetingAgent, error) {
	var recs []struct {
		ID        string `db:"id"`
		Name      string `db:"name"`
		Handle    string `db:"handle"`
		Desc      string `db:"description"`
		Mode      string `db:"mode"`
		Summary   string `db:"summary_target"`
		Store     bool   `db:"store_transcript"`
		Retention int    `db:"retention_days"`
	}
	err := r.db.SelectContext(ctx, &recs, `
		SELECT a.id, COALESCE(ac.name, '') AS name, a.handle, COALESCE(ac.description, '') AS description,
		       COALESCE(ms.mode, 'notes_only') AS mode, COALESCE(ms.summary_target, 'none') AS summary_target, COALESCE(ms.store_transcript, TRUE) AS store_transcript,
		       COALESCE(ms.transcript_retention_days, 30) AS retention_days
		FROM agents a JOIN accounts ac ON ac.id = a.id AND ac.deleted_at IS NULL
		LEFT JOIN agent_meeting_settings ms ON ms.agent_id = a.id
		WHERE a.agent_type = 'meeting'
		  AND (a.project_id = $1::uuid OR EXISTS (SELECT 1 FROM project_members pm
		        WHERE pm.account_id = a.id AND pm.project_id = $1::uuid AND pm.deleted_at IS NULL))
		ORDER BY ac.name`, projectID.String())
	if err != nil {
		return nil, fmt.Errorf("meeting: list meeting agents: %w", err)
	}
	out := make([]*meetingdom.MeetingAgent, len(recs))
	for i, rec := range recs {
		id, _ := uuid.Parse(rec.ID)
		out[i] = &meetingdom.MeetingAgent{ID: id, Name: rec.Name, Handle: rec.Handle, Description: rec.Desc,
			Mode: rec.Mode, SummaryTarget: rec.Summary, StoreTranscript: rec.Store, RetentionDays: rec.Retention}
	}
	return out, nil
}

func workerKeyName(sessionID uuid.UUID) string { return "meeting-agent:" + sessionID.String() }

// CreateWorkerKey implements meetingdom.Repository.
func (r *MeetingRepository) CreateWorkerKey(ctx context.Context, agentID, sessionID uuid.UUID, hash, prefix string, expires time.Time) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO api_keys (account_id, name, key_prefix, key_hash, expires_at)
		VALUES ($1::uuid, $2, $3, $4, $5)`, agentID.String(), workerKeyName(sessionID), prefix, hash, expires)
	if err != nil {
		return fmt.Errorf("meeting: create worker key: %w", err)
	}
	return nil
}

// RevokeWorkerKeys implements meetingdom.Repository.
func (r *MeetingRepository) RevokeWorkerKeys(ctx context.Context, sessionID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `UPDATE api_keys SET revoked_at = NOW() WHERE name = $1 AND revoked_at IS NULL`, workerKeyName(sessionID))
	return err
}

// AppendSegments implements meetingdom.Repository.
func (r *MeetingRepository) AppendSegments(ctx context.Context, callID, channelID uuid.UUID, retentionDays int, segs []meetingdom.SegmentInput) error {
	if len(segs) == 0 {
		return nil
	}
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		for _, s := range segs {
			spoken := s.SpokenAt
			if spoken.IsZero() {
				spoken = time.Now()
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO call_transcript_segments
				(call_session_id, channel_id, speaker_id, text, spoken_at, expires_at)
				VALUES ($1::uuid,$2::uuid,
				        (SELECT id FROM accounts WHERE id = $3::uuid), $4, $5, $5::timestamptz + make_interval(days => $6::int))`,
				callID.String(), channelID.String(), s.SpeakerID.String(), s.Text, spoken, retentionDays); err != nil {
				return fmt.Errorf("meeting: append segment: %w", err)
			}
		}
		return nil
	})
}

// ListSegments implements meetingdom.Repository.
func (r *MeetingRepository) ListSegments(ctx context.Context, callID uuid.UUID, limit int) ([]*meetingdom.Segment, error) {
	var recs []struct {
		ID      string    `db:"id"`
		Speaker *string   `db:"speaker_id"`
		Text    string    `db:"text"`
		Spoken  time.Time `db:"spoken_at"`
	}
	err := r.db.SelectContext(ctx, &recs, `SELECT id, speaker_id, text, spoken_at FROM (
			SELECT id, speaker_id, text, spoken_at FROM call_transcript_segments
			WHERE call_session_id = $1::uuid ORDER BY spoken_at DESC LIMIT $2) t ORDER BY spoken_at`, callID.String(), limit)
	if err != nil {
		return nil, fmt.Errorf("meeting: list segments: %w", err)
	}
	out := make([]*meetingdom.Segment, len(recs))
	for i, rec := range recs {
		id, _ := uuid.Parse(rec.ID)
		out[i] = &meetingdom.Segment{ID: id, CallSessionID: callID, SpeakerID: optUUID(rec.Speaker), Text: rec.Text, SpokenAt: rec.Spoken}
	}
	return out, nil
}

// DeleteExpiredSegments implements meetingdom.Repository.
func (r *MeetingRepository) DeleteExpiredSegments(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM call_transcript_segments WHERE expires_at < NOW()`)
	if err != nil {
		return 0, fmt.Errorf("meeting: delete expired segments: %w", err)
	}
	return res.RowsAffected()
}

// ListMeetings implements meetingdom.Repository.
func (r *MeetingRepository) ListMeetings(ctx context.Context, channelID uuid.UUID, limit int) ([]*meetingdom.MeetingSummary, error) {
	var recs []struct {
		ID       string     `db:"id"`
		Started  time.Time  `db:"started_at"`
		Ended    *time.Time `db:"ended_at"`
		Segments int        `db:"segments"`
		Speakers []byte     `db:"speakers"`
	}
	err := r.db.SelectContext(ctx, &recs, `
		SELECT cs.id, cs.started_at, cs.ended_at, t.segments, t.speakers
		FROM call_sessions cs
		JOIN LATERAL (
			SELECT count(*) AS segments,
			       COALESCE((SELECT jsonb_agg(n ORDER BY first_at) FROM (
			            SELECT COALESCE(ac.name, '') AS n, min(s2.spoken_at) AS first_at
			            FROM call_transcript_segments s2 LEFT JOIN accounts ac ON ac.id = s2.speaker_id
			            WHERE s2.call_session_id = cs.id GROUP BY ac.name) x), '[]'::jsonb) AS speakers
			FROM call_transcript_segments s WHERE s.call_session_id = cs.id) t ON TRUE
		WHERE cs.channel_id = $1::uuid
		  AND (t.segments > 0 OR EXISTS (SELECT 1 FROM call_recordings r WHERE r.call_session_id = cs.id AND r.status = 'complete'))
		ORDER BY cs.started_at DESC LIMIT $2`, channelID.String(), limit)
	if err != nil {
		return nil, fmt.Errorf("meeting: list meetings: %w", err)
	}
	out := make([]*meetingdom.MeetingSummary, len(recs))
	for i, rec := range recs {
		id, _ := uuid.Parse(rec.ID)
		var speakers []string
		_ = json.Unmarshal(rec.Speakers, &speakers)
		out[i] = &meetingdom.MeetingSummary{CallID: id, StartedAt: rec.Started, EndedAt: rec.Ended, Segments: rec.Segments, Speakers: speakers}
	}
	return out, nil
}

// ListNamedSegments implements meetingdom.Repository.
func (r *MeetingRepository) ListNamedSegments(ctx context.Context, channelID, callID uuid.UUID, limit int) ([]*meetingdom.NamedSegment, error) {
	var ok bool
	if err := r.db.GetContext(ctx, &ok, `SELECT EXISTS (SELECT 1 FROM call_sessions WHERE id = $1::uuid AND channel_id = $2::uuid)`,
		callID.String(), channelID.String()); err != nil {
		return nil, fmt.Errorf("meeting: check call: %w", err)
	}
	if !ok {
		return nil, meetingdom.ErrMeetingNotFound
	}
	var recs []struct {
		Speaker *string   `db:"speaker_id"`
		Name    string    `db:"name"`
		Text    string    `db:"text"`
		Spoken  time.Time `db:"spoken_at"`
	}
	err := r.db.SelectContext(ctx, &recs, `SELECT s.speaker_id, COALESCE(ac.name, '') AS name, s.text, s.spoken_at
		FROM call_transcript_segments s LEFT JOIN accounts ac ON ac.id = s.speaker_id
		WHERE s.call_session_id = $1::uuid ORDER BY s.spoken_at, s.created_at LIMIT $2`, callID.String(), limit)
	if err != nil {
		return nil, fmt.Errorf("meeting: list named segments: %w", err)
	}
	out := make([]*meetingdom.NamedSegment, len(recs))
	for i, rec := range recs {
		out[i] = &meetingdom.NamedSegment{SpeakerID: optUUID(rec.Speaker), SpeakerName: rec.Name, Text: rec.Text, SpokenAt: rec.Spoken}
	}
	return out, nil
}

// CountProposals implements meetingdom.Repository.
func (r *MeetingRepository) CountProposals(ctx context.Context, callID, agentID uuid.UUID) (int, error) {
	var n int
	err := r.db.GetContext(ctx, &n, `SELECT count(*) FROM agent_proposals WHERE call_session_id = $1::uuid AND agent_account_id = $2::uuid`,
		callID.String(), agentID.String())
	return n, err
}

const proposalCols = `p.id, p.project_id, p.channel_id, p.call_session_id, p.agent_account_id, COALESCE(ac.name, '') AS agent_name,
	p.target_account_id, p.kind, p.payload, p.status, p.result, p.resolved_by, p.expires_at, p.created_at, p.updated_at`

const proposalFrom = `agent_proposals p JOIN accounts ac ON ac.id = p.agent_account_id`

type proposalRecord struct {
	ID         string    `db:"id"`
	ProjectID  string    `db:"project_id"`
	ChannelID  *string   `db:"channel_id"`
	CallID     *string   `db:"call_session_id"`
	AgentID    string    `db:"agent_account_id"`
	AgentName  string    `db:"agent_name"`
	TargetID   string    `db:"target_account_id"`
	Kind       string    `db:"kind"`
	Payload    []byte    `db:"payload"`
	Status     string    `db:"status"`
	Result     []byte    `db:"result"`
	ResolvedBy *string   `db:"resolved_by"`
	ExpiresAt  time.Time `db:"expires_at"`
	CreatedAt  time.Time `db:"created_at"`
	UpdatedAt  time.Time `db:"updated_at"`
}

func (rec proposalRecord) entity() *meetingdom.Proposal {
	id, _ := uuid.Parse(rec.ID)
	proj, _ := uuid.Parse(rec.ProjectID)
	agent, _ := uuid.Parse(rec.AgentID)
	target, _ := uuid.Parse(rec.TargetID)
	return &meetingdom.Proposal{ID: id, ProjectID: proj, ChannelID: optUUID(rec.ChannelID), CallSessionID: optUUID(rec.CallID),
		AgentAccountID: agent, AgentName: rec.AgentName, TargetAccountID: target, Kind: rec.Kind, Payload: rec.Payload,
		Status: rec.Status, Result: rec.Result, ResolvedBy: optUUID(rec.ResolvedBy), ExpiresAt: rec.ExpiresAt,
		CreatedAt: rec.CreatedAt, UpdatedAt: rec.UpdatedAt}
}

// CreateProposal implements meetingdom.Repository.
func (r *MeetingRepository) CreateProposal(ctx context.Context, p *meetingdom.Proposal) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO agent_proposals (id, project_id, channel_id, call_session_id, agent_account_id,
		target_account_id, kind, payload, status, expires_at, created_at, updated_at)
		VALUES ($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7,$8::jsonb,$9,$10,$11,$11)`,
		p.ID.String(), p.ProjectID.String(), uuidStrPtr(p.ChannelID), uuidStrPtr(p.CallSessionID), p.AgentAccountID.String(),
		p.TargetAccountID.String(), p.Kind, string(p.Payload), p.Status, p.ExpiresAt, p.CreatedAt)
	if err != nil {
		return fmt.Errorf("meeting: create proposal: %w", err)
	}
	return nil
}

// GetProposal implements meetingdom.Repository.
func (r *MeetingRepository) GetProposal(ctx context.Context, projectID, id uuid.UUID) (*meetingdom.Proposal, error) {
	var rec proposalRecord
	err := r.db.GetContext(ctx, &rec, `SELECT `+proposalCols+` FROM `+proposalFrom+`
		WHERE p.id = $1::uuid AND p.project_id = $2::uuid`, id.String(), projectID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, meetingdom.ErrProposalNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("meeting: get proposal: %w", err)
	}
	return rec.entity(), nil
}

// ListProposals implements meetingdom.Repository.
func (r *MeetingRepository) ListProposals(ctx context.Context, projectID uuid.UUID, channelID, target *uuid.UUID, limit int) ([]*meetingdom.Proposal, error) {
	var recs []proposalRecord
	err := r.db.SelectContext(ctx, &recs, `SELECT `+proposalCols+` FROM `+proposalFrom+`
		WHERE p.project_id = $1::uuid
		  AND ($2::uuid IS NULL OR p.channel_id = $2::uuid)
		  AND ($3::uuid IS NULL OR p.target_account_id = $3::uuid)
		  AND NOT (p.status = 'pending' AND p.expires_at < NOW())
		ORDER BY p.created_at DESC LIMIT $4`, projectID.String(), uuidStrPtr(channelID), uuidStrPtr(target), limit)
	if err != nil {
		return nil, fmt.Errorf("meeting: list proposals: %w", err)
	}
	out := make([]*meetingdom.Proposal, len(recs))
	for i, rec := range recs {
		out[i] = rec.entity()
	}
	return out, nil
}

// TransitionProposal implements meetingdom.Repository.
func (r *MeetingRepository) TransitionProposal(ctx context.Context, id uuid.UUID, from []string, to string, by *uuid.UUID, result []byte) (bool, error) {
	if result == nil {
		result = []byte(`{}`)
	}
	res, err := r.db.ExecContext(ctx, `UPDATE agent_proposals SET status = $3,
		resolved_by = COALESCE($4::uuid, resolved_by),
		result = CASE WHEN $5::text = '{}' THEN result ELSE $5::jsonb END, updated_at = NOW()
		WHERE id = $1::uuid AND status IN (SELECT jsonb_array_elements_text($2::jsonb))
		  AND ($4::uuid IS NULL OR resolved_by IS NULL OR resolved_by = $4::uuid)`,
		id.String(), jsonList(from), to, uuidStrPtr(by), string(result))
	if err != nil {
		return false, fmt.Errorf("meeting: transition proposal: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// ExpireProposals implements meetingdom.Repository.
func (r *MeetingRepository) ExpireProposals(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx, `UPDATE agent_proposals SET status = 'expired', updated_at = NOW()
		WHERE status IN ('pending') AND expires_at < NOW()`)
	if err != nil {
		return 0, fmt.Errorf("meeting: expire proposals: %w", err)
	}
	return res.RowsAffected()
}
