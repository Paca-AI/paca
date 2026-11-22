// Package meetingsvc implements the meeting assistant: its settings, inviting
// it into a call, the routes its worker uses, and proposals.
package meetingsvc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	meetingdom "github.com/Paca-AI/api/internal/domain/meeting"
)

// Timings.
const (
	// ProposalTTL is how long an unreviewed proposal stays open.
	ProposalTTL = 30 * time.Minute
	// WorkerKeyTTL bounds the API key a worker gets for one job; a longer call
	// re-bootstraps.
	WorkerKeyTTL = 6 * time.Hour
	// AgentName is the name the worker registers with LiveKit under.
	AgentName = "paca-meeting"
)

// STTProviders are the transcription providers the worker bundles. "openai"
// also covers any OpenAI-compatible endpoint (self-hosted Whisper) through
// stt_base_url.
var STTProviders = []string{"openai", "deepgram"}

// Room is the LiveKit part of the service.
type Room interface {
	Enabled() bool
	DispatchAgent(ctx context.Context, room, agentName, metadata string) (string, error)
	DeleteDispatch(ctx context.Context, room, dispatchID string) error
	RemoveParticipant(ctx context.Context, room, identity string) error
}

// Crypter decrypts and encrypts the stored secrets (AES-GCM with the platform
// key). A nil Crypter stores and returns plaintext, like the agent service.
type Crypter interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(encoded string) (string, error)
}

// Service implements meetingdom.Service.
type Service struct {
	repo        meetingdom.Repository
	room        Room
	crypt       Crypter
	notifier    meetingdom.Notifier
	workerToken string
	now         func() time.Time
}

// New returns a Service. workerToken is the secret the worker presents; empty
// switches assistants off.
func New(repo meetingdom.Repository, room Room, workerToken string) *Service {
	return &Service{repo: repo, room: room, workerToken: workerToken, now: time.Now}
}

// WithCrypter sets the secret encryptor.
func (s *Service) WithCrypter(c Crypter) *Service { s.crypt = c; return s }

// WithNotifier announces changes to realtime.
func (s *Service) WithNotifier(n meetingdom.Notifier) *Service { s.notifier = n; return s }

var _ meetingdom.Service = (*Service)(nil)

// Enabled implements meetingdom.Service.
func (s *Service) Enabled() bool {
	return s.workerToken != "" && s.room != nil && s.room.Enabled()
}

func (s *Service) encrypt(v string) (string, error) {
	if s.crypt == nil || v == "" {
		return v, nil
	}
	return s.crypt.Encrypt(v)
}

func (s *Service) decrypt(v string) (string, error) {
	if s.crypt == nil || v == "" {
		return v, nil
	}
	return s.crypt.Decrypt(v)
}

func (s *Service) meetingAgent(ctx context.Context, projectID, agentID uuid.UUID) (*meetingdom.AgentInfo, error) {
	info, err := s.repo.AgentInfo(ctx, projectID, agentID)
	if err != nil {
		return nil, err
	}
	if info.Type != agentdom.AgentTypeMeeting {
		return nil, meetingdom.ErrNotMeetingAgent
	}
	return info, nil
}

// GetSettings implements meetingdom.Service.
func (s *Service) GetSettings(ctx context.Context, projectID, agentID uuid.UUID) (*meetingdom.Settings, error) {
	if _, err := s.meetingAgent(ctx, projectID, agentID); err != nil {
		return nil, err
	}
	return s.repo.GetSettings(ctx, agentID)
}

func oneOf(v string, set ...string) bool {
	for _, x := range set {
		if v == x {
			return true
		}
	}
	return false
}

func inRange(v, lo, hi int) bool { return v >= lo && v <= hi }

func cleanList(in []string, maxItems, maxLen int) ([]string, bool) {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		if len(v) > maxLen {
			return nil, false
		}
		seen[v] = true
		out = append(out, v)
	}
	return out, len(out) <= maxItems
}

// UpdateSettings implements meetingdom.Service.
func (s *Service) UpdateSettings(ctx context.Context, projectID, agentID uuid.UUID, p meetingdom.SettingsPatch) (*meetingdom.Settings, error) {
	if _, err := s.meetingAgent(ctx, projectID, agentID); err != nil {
		return nil, err
	}
	cur, err := s.repo.GetSettings(ctx, agentID)
	if err != nil {
		return nil, err
	}
	bad := func(what string) (*meetingdom.Settings, error) {
		return nil, fmt.Errorf("%w: %s", meetingdom.ErrSettingsInvalid, what)
	}
	if p.STTProvider != nil {
		v := strings.TrimSpace(*p.STTProvider)
		if v != "" && !oneOf(v, STTProviders...) {
			return bad("stt_provider")
		}
		cur.STTProvider = v
	}
	if p.STTModel != nil {
		if len(*p.STTModel) > 200 {
			return bad("stt_model")
		}
		cur.STTModel = strings.TrimSpace(*p.STTModel)
	}
	if p.STTBaseURL != nil {
		v := strings.TrimSpace(*p.STTBaseURL)
		if len(v) > 500 || (v != "" && !strings.HasPrefix(v, "http://") && !strings.HasPrefix(v, "https://")) {
			return bad("stt_base_url")
		}
		cur.STTBaseURL = v
	}
	if p.STTAPIKey != nil {
		enc, err := s.encrypt(*p.STTAPIKey)
		if err != nil {
			return nil, fmt.Errorf("meeting: encrypt stt key: %w", err)
		}
		cur.STTAPIKeySecret = enc
	}
	if p.STTLanguages != nil {
		langs, ok := cleanList(*p.STTLanguages, 20, 16)
		if !ok {
			return bad("stt_languages")
		}
		cur.STTLanguages = langs
	}
	if p.Mode != nil {
		if !oneOf(*p.Mode, meetingdom.ModeOnDemand, meetingdom.ModeAutoSuggest, meetingdom.ModeNotesOnly) {
			return bad("mode")
		}
		cur.Mode = *p.Mode
	}
	if p.SuggestIntervalSeconds != nil {
		if !inRange(*p.SuggestIntervalSeconds, 30, 3600) {
			return bad("suggest_interval_seconds")
		}
		cur.SuggestIntervalSeconds = *p.SuggestIntervalSeconds
	}
	if p.ProposalKinds != nil {
		kinds, ok := cleanList(*p.ProposalKinds, len(meetingdom.ProposalKinds), 64)
		if !ok {
			return bad("proposal_kinds")
		}
		for _, k := range kinds {
			if !meetingdom.ValidKind(k) {
				return bad("proposal_kinds")
			}
		}
		cur.ProposalKinds = kinds
	}
	if p.MaxProposalsPerCall != nil {
		if !inRange(*p.MaxProposalsPerCall, 0, 100) {
			return bad("max_proposals_per_call")
		}
		cur.MaxProposalsPerCall = *p.MaxProposalsPerCall
	}
	if p.StoreTranscript != nil {
		cur.StoreTranscript = *p.StoreTranscript
	}
	if p.TranscriptRetentionDays != nil {
		if !inRange(*p.TranscriptRetentionDays, 1, 3650) {
			return bad("transcript_retention_days")
		}
		cur.TranscriptRetentionDays = *p.TranscriptRetentionDays
	}
	if p.Announcement != nil {
		if len(*p.Announcement) > 500 {
			return bad("announcement")
		}
		cur.Announcement = strings.TrimSpace(*p.Announcement)
	}
	if p.SummaryTarget != nil {
		if !oneOf(*p.SummaryTarget, meetingdom.SummaryNone, meetingdom.SummaryChannelMessage) {
			return bad("summary_target")
		}
		cur.SummaryTarget = *p.SummaryTarget
	}
	if p.DefaultTarget != nil {
		if !oneOf(*p.DefaultTarget, meetingdom.TargetRequester, meetingdom.TargetHost) {
			return bad("default_target")
		}
		cur.DefaultTarget = *p.DefaultTarget
	}
	if err := s.repo.SaveSettings(ctx, cur); err != nil {
		return nil, err
	}
	return s.repo.GetSettings(ctx, agentID)
}

// ListMeetingAgents implements meetingdom.Service.
func (s *Service) ListMeetingAgents(ctx context.Context, projectID uuid.UUID) ([]*meetingdom.MeetingAgent, error) {
	return s.repo.ListMeetingAgents(ctx, projectID)
}

// ready reports whether the assistant has what it needs to run.
func (s *Service) ready(info *meetingdom.AgentInfo, set *meetingdom.Settings) bool {
	if info.LLMProvider == "" || info.LLMModel == "" || set.STTProvider == "" || set.STTModel == "" {
		return false
	}
	// A hosted provider needs a key; a self-hosted endpoint may not.
	return set.HasSTTKey() || set.STTBaseURL != ""
}

type dispatchMeta struct {
	V       int    `json:"v"`
	Session string `json:"session"`
}

// Invite implements meetingdom.Service. The router has already checked
// calls:invite_agent on the agent.
func (s *Service) Invite(ctx context.Context, projectID, channelID, agentID, invitedBy uuid.UUID) (*meetingdom.AgentSession, error) {
	if !s.Enabled() {
		return nil, meetingdom.ErrAssistantsDisabled
	}
	info, err := s.meetingAgent(ctx, projectID, agentID)
	if err != nil {
		return nil, err
	}
	set, err := s.repo.GetSettings(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if !s.ready(info, set) {
		return nil, meetingdom.ErrAssistantNotConfigured
	}
	callID, _, err := s.repo.OpenCall(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if callID == uuid.Nil {
		return nil, meetingdom.ErrNoOpenCall
	}
	if in, err := s.repo.IsCallParticipant(ctx, callID, invitedBy); err != nil {
		return nil, err
	} else if !in {
		// Only someone in the call may bring an assistant into it.
		return nil, meetingdom.ErrNoOpenCall
	}
	sess := &meetingdom.AgentSession{ID: uuid.New(), CallSessionID: callID, ChannelID: channelID, ProjectID: projectID,
		AgentID: agentID, InvitedBy: &invitedBy, Status: meetingdom.SessionDispatched}
	if err := s.repo.CreateAgentSession(ctx, sess); err != nil {
		return nil, err
	}
	meta, err := json.Marshal(dispatchMeta{V: 1, Session: sess.ID.String()})
	if err != nil {
		return nil, err
	}
	dispatchID, err := s.room.DispatchAgent(ctx, channelID.String(), AgentName, string(meta))
	if err != nil {
		_ = s.repo.SetAgentSessionStatus(ctx, sess.ID, meetingdom.SessionFailed)
		return nil, fmt.Errorf("meeting: dispatch: %w", err)
	}
	if err := s.repo.SetAgentSessionDispatch(ctx, sess.ID, dispatchID); err != nil {
		slog.Warn("meeting: record dispatch", "session_id", sess.ID, "err", err)
	}
	s.announce(ctx, channelID)
	return s.repo.FindAgentSession(ctx, sess.ID)
}

// Remove implements meetingdom.Service.
func (s *Service) Remove(ctx context.Context, projectID, channelID, agentID uuid.UUID) error {
	if !s.Enabled() {
		return meetingdom.ErrAssistantsDisabled
	}
	if _, err := s.meetingAgent(ctx, projectID, agentID); err != nil {
		return err
	}
	sess, err := s.repo.OpenAgentSession(ctx, channelID, agentID)
	if err != nil {
		return err
	}
	room := channelID.String()
	if sess.DispatchID != "" {
		if err := s.room.DeleteDispatch(ctx, room, sess.DispatchID); err != nil {
			slog.Warn("meeting: delete dispatch", "session_id", sess.ID, "err", err)
		}
	}
	if err := s.room.RemoveParticipant(ctx, room, agentID.String()); err != nil {
		slog.Warn("meeting: remove assistant participant", "session_id", sess.ID, "err", err)
	}
	s.end(ctx, sess)
	return nil
}

func (s *Service) end(ctx context.Context, sess *meetingdom.AgentSession) {
	if err := s.repo.SetAgentSessionStatus(ctx, sess.ID, meetingdom.SessionLeft); err != nil {
		slog.Warn("meeting: close agent session", "session_id", sess.ID, "err", err)
	}
	if err := s.repo.RevokeWorkerKeys(ctx, sess.ID); err != nil {
		slog.Warn("meeting: revoke worker key", "session_id", sess.ID, "err", err)
	}
	s.announce(ctx, sess.ChannelID)
}

// ListAssistants implements meetingdom.Service.
func (s *Service) ListAssistants(ctx context.Context, _, channelID uuid.UUID) ([]*meetingdom.AgentSession, error) {
	return s.repo.ListAgentSessions(ctx, channelID)
}

// CallEnded implements meetingdom.Service.
func (s *Service) CallEnded(ctx context.Context, channelID uuid.UUID) {
	callID, _, err := s.repo.OpenCall(ctx, channelID)
	if err != nil || callID == uuid.Nil {
		return
	}
	sessions, err := s.repo.ListAgentSessions(ctx, channelID)
	if err != nil {
		slog.Warn("meeting: list sessions of ended call", "channel_id", channelID, "err", err)
		return
	}
	for _, sess := range sessions {
		if sess.DispatchID != "" && s.room != nil {
			_ = s.room.DeleteDispatch(ctx, channelID.String(), sess.DispatchID)
		}
		if err := s.repo.RevokeWorkerKeys(ctx, sess.ID); err != nil {
			slog.Warn("meeting: revoke worker key", "session_id", sess.ID, "err", err)
		}
	}
	if err := s.repo.EndAgentSessions(ctx, callID); err != nil {
		slog.Warn("meeting: end agent sessions", "call_id", callID, "err", err)
	}
}

func (s *Service) announce(ctx context.Context, channelID uuid.UUID) {
	if s.notifier != nil {
		s.notifier.AssistantChanged(ctx, channelID)
	}
}

// ---------------------------------------------------------------- worker

func (s *Service) authWorker(secret string) error {
	if s.workerToken == "" || subtle.ConstantTimeCompare([]byte(secret), []byte(s.workerToken)) != 1 {
		return meetingdom.ErrWorkerUnauthorized
	}
	return nil
}

func (s *Service) liveSession(ctx context.Context, secret string, id uuid.UUID) (*meetingdom.AgentSession, error) {
	if err := s.authWorker(secret); err != nil {
		return nil, err
	}
	sess, err := s.repo.FindAgentSession(ctx, id)
	if err != nil {
		return nil, err
	}
	if sess.LeftAt != nil {
		return nil, meetingdom.ErrNotInCall
	}
	return sess, nil
}

func newWorkerKey() (plain, hash, prefix string, err error) {
	var b [32]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", "", "", err
	}
	raw := hex.EncodeToString(b[:])
	sum := sha256.Sum256([]byte("paca_" + raw))
	return "paca_" + raw, hex.EncodeToString(sum[:]), raw[:8], nil
}

// Bootstrap implements meetingdom.Service. The decrypted keys leave the API
// here and live only in the job's memory.
func (s *Service) Bootstrap(ctx context.Context, secret string, sessionID uuid.UUID) (*meetingdom.WorkerBootstrap, error) {
	sess, err := s.liveSession(ctx, secret, sessionID)
	if err != nil {
		return nil, err
	}
	info, err := s.meetingAgent(ctx, sess.ProjectID, sess.AgentID)
	if err != nil {
		return nil, err
	}
	set, err := s.repo.GetSettings(ctx, sess.AgentID)
	if err != nil {
		return nil, err
	}
	llmKey, err := s.decrypt(info.LLMKeySecret)
	if err != nil {
		return nil, fmt.Errorf("meeting: decrypt llm key: %w", err)
	}
	sttKey, err := s.decrypt(set.STTAPIKeySecret)
	if err != nil {
		return nil, fmt.Errorf("meeting: decrypt stt key: %w", err)
	}
	if err := s.repo.RevokeWorkerKeys(ctx, sess.ID); err != nil {
		return nil, err
	}
	plain, hash, prefix, err := newWorkerKey()
	if err != nil {
		return nil, err
	}
	exp := s.now().Add(WorkerKeyTTL)
	if err := s.repo.CreateWorkerKey(ctx, sess.AgentID, sess.ID, hash, prefix, exp); err != nil {
		return nil, err
	}
	if err := s.repo.SetAgentSessionStatus(ctx, sess.ID, meetingdom.SessionActive); err != nil {
		return nil, err
	}
	_, host, err := s.repo.OpenCall(ctx, sess.ChannelID)
	if err != nil {
		return nil, err
	}
	s.announce(ctx, sess.ChannelID)
	return &meetingdom.WorkerBootstrap{
		SessionID: sess.ID, ProjectID: sess.ProjectID, ChannelID: sess.ChannelID, CallID: sess.CallSessionID,
		AgentID: sess.AgentID, AgentName: info.Name, RequesterID: sess.InvitedBy, HostID: host,
		Instructions: info.Instructions, LLMProvider: info.LLMProvider, LLMModel: info.LLMModel,
		LLMAPIKey: llmKey, LLMBaseURL: info.LLMBaseURL, Settings: set, STTAPIKey: sttKey,
		APIKey: plain, ExpiresAt: exp,
	}, nil
}

const maxSegmentsPerFlush = 200
const maxSegmentLen = 8000

// AppendTranscript implements meetingdom.Service. A speaker who was never in the
// call is rejected, so a job cannot attribute speech to anyone else; one who has
// left since still counts, or the last turns of every call would be lost.
func (s *Service) AppendTranscript(ctx context.Context, secret string, sessionID uuid.UUID, segs []meetingdom.SegmentInput) error {
	sess, err := s.liveSession(ctx, secret, sessionID)
	if err != nil {
		return err
	}
	if len(segs) > maxSegmentsPerFlush {
		return fmt.Errorf("%w: too many segments", meetingdom.ErrProposalInvalid)
	}
	set, err := s.repo.GetSettings(ctx, sess.AgentID)
	if err != nil {
		return err
	}
	if !set.StoreTranscript {
		// Kept in the job's memory for its own use, never stored.
		return nil
	}
	clean := make([]meetingdom.SegmentInput, 0, len(segs))
	for _, seg := range segs {
		seg.Text = strings.TrimSpace(seg.Text)
		if seg.Text == "" {
			continue
		}
		if len(seg.Text) > maxSegmentLen {
			seg.Text = seg.Text[:maxSegmentLen]
		}
		if in, err := s.repo.WasCallParticipant(ctx, sess.CallSessionID, seg.SpeakerID); err != nil {
			return err
		} else if !in {
			continue
		}
		clean = append(clean, seg)
	}
	return s.repo.AppendSegments(ctx, sess.CallSessionID, sess.ChannelID, set.TranscriptRetentionDays, clean)
}

// Transcript implements meetingdom.Service.
func (s *Service) Transcript(ctx context.Context, secret string, sessionID uuid.UUID, limit int) ([]*meetingdom.Segment, error) {
	sess, err := s.liveSession(ctx, secret, sessionID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 1000 {
		limit = 300
	}
	return s.repo.ListSegments(ctx, sess.CallSessionID, limit)
}

// Finish implements meetingdom.Service.
func (s *Service) Finish(ctx context.Context, secret string, sessionID uuid.UUID) error {
	if err := s.authWorker(secret); err != nil {
		return err
	}
	sess, err := s.repo.FindAgentSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if sess.LeftAt != nil {
		return nil
	}
	s.end(ctx, sess)
	return nil
}

// ------------------------------------------------------------- proposals

// CreateProposal implements meetingdom.Service. The router has already checked
// proposals:write for the agent's own account.
func (s *Service) CreateProposal(ctx context.Context, in meetingdom.NewProposal) (*meetingdom.Proposal, error) {
	if !meetingdom.ValidKind(in.Kind) {
		return nil, fmt.Errorf("%w: unknown kind %q", meetingdom.ErrProposalInvalid, in.Kind)
	}
	payload, err := meetingdom.ValidatePayload(in.Kind, in.Payload)
	if err != nil {
		return nil, err
	}
	info, err := s.meetingAgent(ctx, in.ProjectID, in.AgentID)
	if err != nil {
		return nil, err
	}
	set, err := s.repo.GetSettings(ctx, in.AgentID)
	if err != nil {
		return nil, err
	}
	if set.Mode == meetingdom.ModeNotesOnly || !oneOf(in.Kind, set.ProposalKinds...) {
		return nil, meetingdom.ErrProposalKindDisabled
	}
	sess, err := s.repo.OpenAgentSession(ctx, in.ChannelID, in.AgentID)
	if err != nil {
		return nil, err
	}
	if n, err := s.repo.CountProposals(ctx, sess.CallSessionID, in.AgentID); err != nil {
		return nil, err
	} else if n >= set.MaxProposalsPerCall {
		return nil, meetingdom.ErrProposalLimit
	}
	_, host, err := s.repo.OpenCall(ctx, in.ChannelID)
	if err != nil {
		return nil, err
	}
	target := s.pickTarget(ctx, sess, host, set, in.TargetID)
	if target == uuid.Nil {
		return nil, meetingdom.ErrNoOpenCall
	}
	now := s.now()
	p := &meetingdom.Proposal{ID: uuid.New(), ProjectID: in.ProjectID, ChannelID: &in.ChannelID, CallSessionID: &sess.CallSessionID,
		AgentAccountID: in.AgentID, AgentName: info.Name, TargetAccountID: target, Kind: in.Kind, Payload: payload,
		Status: meetingdom.StatusPending, Result: []byte(`{}`), ExpiresAt: now.Add(ProposalTTL), CreatedAt: now, UpdatedAt: now}
	if err := s.repo.CreateProposal(ctx, p); err != nil {
		return nil, err
	}
	s.announceProposal(ctx, p)
	return p, nil
}

// pickTarget chooses who reviews: the person the assistant named if they are
// in the call, else per the settings, falling back to the other one.
func (s *Service) pickTarget(ctx context.Context, sess *meetingdom.AgentSession, host *uuid.UUID, set *meetingdom.Settings, named *uuid.UUID) uuid.UUID {
	in := func(id *uuid.UUID) bool {
		if id == nil {
			return false
		}
		ok, err := s.repo.IsCallParticipant(ctx, sess.CallSessionID, *id)
		return err == nil && ok
	}
	if in(named) {
		return *named
	}
	order := []*uuid.UUID{sess.InvitedBy, host}
	if set.DefaultTarget == meetingdom.TargetHost {
		order = []*uuid.UUID{host, sess.InvitedBy}
	}
	for _, id := range order {
		if id != nil {
			return *id
		}
	}
	return uuid.Nil
}

func (s *Service) announceProposal(ctx context.Context, p *meetingdom.Proposal) {
	if s.notifier != nil {
		s.notifier.ProposalChanged(ctx, p.ChannelID, p.TargetAccountID, p.ID)
	}
}

// GetProposal implements meetingdom.Service.
func (s *Service) GetProposal(ctx context.Context, projectID, id uuid.UUID) (*meetingdom.Proposal, error) {
	return s.repo.GetProposal(ctx, projectID, id)
}

// ListProposals implements meetingdom.Service.
func (s *Service) ListProposals(ctx context.Context, projectID uuid.UUID, channelID, target *uuid.UUID) ([]*meetingdom.Proposal, error) {
	if _, err := s.repo.ExpireProposals(ctx); err != nil {
		slog.Warn("meeting: expire proposals", "err", err)
	}
	return s.repo.ListProposals(ctx, projectID, channelID, target, 100)
}

func (s *Service) mine(ctx context.Context, projectID, id, by uuid.UUID) (*meetingdom.Proposal, error) {
	p, err := s.repo.GetProposal(ctx, projectID, id)
	if err != nil {
		return nil, err
	}
	// Who may act on the proposal is decided at the route (proposals:resolve
	// with the proposal.target_id condition), not here.
	if p.Status == meetingdom.StatusPending && !p.ExpiresAt.After(s.now()) {
		if ok, err := s.repo.TransitionProposal(ctx, p.ID, []string{meetingdom.StatusPending}, meetingdom.StatusExpired, nil, nil); err != nil {
			return nil, err
		} else if ok {
			s.announceProposal(ctx, p)
		}
		return s.repo.GetProposal(ctx, projectID, id)
	}
	return p, nil
}

// Claim implements meetingdom.Service. Claiming is a compare-and-set, so two
// tabs or two clicks cannot both go on to write; the same person claiming
// again (a reload mid-card) gets the proposal back.
func (s *Service) Claim(ctx context.Context, projectID, id, by uuid.UUID) (*meetingdom.Proposal, error) {
	p, err := s.mine(ctx, projectID, id, by)
	if err != nil {
		return nil, err
	}
	if p.Status == meetingdom.StatusClaimed && p.ResolvedBy != nil && *p.ResolvedBy == by {
		return p, nil
	}
	ok, err := s.repo.TransitionProposal(ctx, id, []string{meetingdom.StatusPending}, meetingdom.StatusClaimed, &by, nil)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, meetingdom.ErrProposalClosed
	}
	p, err = s.repo.GetProposal(ctx, projectID, id)
	if err == nil {
		s.announceProposal(ctx, p)
	}
	return p, err
}

// Resolve implements meetingdom.Service: the person records the outcome of
// what their own browser did (resolved, failed) or dismisses the proposal
// (rejected).
func (s *Service) Resolve(ctx context.Context, projectID, id, by uuid.UUID, status string, result []byte) (*meetingdom.Proposal, error) {
	var from []string
	switch status {
	case meetingdom.StatusResolved, meetingdom.StatusFailed:
		from = []string{meetingdom.StatusClaimed}
	case meetingdom.StatusRejected:
		from = []string{meetingdom.StatusPending, meetingdom.StatusClaimed}
	default:
		return nil, fmt.Errorf("%w: status %q", meetingdom.ErrProposalInvalid, status)
	}
	if len(result) > 16<<10 || (len(result) > 0 && !json.Valid(result)) {
		return nil, fmt.Errorf("%w: result", meetingdom.ErrProposalInvalid)
	}
	p, err := s.mine(ctx, projectID, id, by)
	if err != nil {
		return nil, err
	}
	if p.Status == meetingdom.StatusClaimed && p.ResolvedBy != nil && *p.ResolvedBy != by {
		return nil, meetingdom.ErrProposalClosed
	}
	ok, err := s.repo.TransitionProposal(ctx, id, from, status, &by, result)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, meetingdom.ErrProposalClosed
	}
	p, err = s.repo.GetProposal(ctx, projectID, id)
	if err == nil {
		s.announceProposal(ctx, p)
	}
	return p, err
}

// SweepExpired closes proposals nobody reviewed in time and deletes transcript
// segments past their retention. It is meant for a periodic worker.
func (s *Service) SweepExpired(ctx context.Context) {
	if n, err := s.repo.ExpireProposals(ctx); err != nil {
		slog.Warn("meeting: expire proposals", "err", err)
	} else if n > 0 {
		slog.Info("meeting: proposals expired", "count", n)
	}
	if n, err := s.repo.DeleteExpiredSegments(ctx); err != nil {
		slog.Warn("meeting: delete expired transcript", "err", err)
	} else if n > 0 {
		slog.Info("meeting: transcript segments deleted", "count", n)
	}
}
