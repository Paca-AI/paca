// Package agentturnsvc implements the session-first, owner-private project
// chat application service.
package agentturnsvc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	"github.com/Paca-AI/api/internal/platform/authz"
)

const (
	defaultTurnTimeout = 30 * time.Minute
	maxTurnDeadline    = 24 * time.Hour
	maxSessionTitle    = 200
)

type permissionChecker interface {
	HasPermissions(ctx context.Context, userID uuid.UUID, projectID *uuid.UUID, legacyRole string, required ...authz.Permission) (bool, error)
}

type projectAgentFinder interface {
	FindVisibleAgentInProject(ctx context.Context, projectID, agentID uuid.UUID) (*agentdom.Agent, error)
}

// Service coordinates authorization and durable project chat turn workflows.
type Service struct {
	repo       agentdom.TurnRepository
	agents     projectAgentFinder
	authorizer permissionChecker
	now        func() time.Time
}

// New constructs an authoritative project chat service.
func New(repo agentdom.TurnRepository, agents projectAgentFinder, authorizer permissionChecker) *Service {
	return &Service{repo: repo, agents: agents, authorizer: authorizer, now: time.Now}
}

// ListChatSessions returns owner-scoped project chat sessions.
func (s *Service) ListChatSessions(ctx context.Context, filter agentdom.ChatSessionListFilter, actor agentdom.ChatActor) ([]*agentdom.ChatSessionSummary, bool, error) {
	if err := s.authorize(ctx, filter.ProjectID, actor, authz.PermissionAgentsRead); err != nil {
		return nil, false, err
	}
	filter.MemberID = actor.MemberID
	return s.repo.ListOwnerChatSessions(ctx, filter)
}

// GetChatSession returns one owner-scoped project chat session.
func (s *Service) GetChatSession(ctx context.Context, projectID, sessionID uuid.UUID, actor agentdom.ChatActor) (*agentdom.AgentChatSession, error) {
	if err := s.authorizeOwnerRead(ctx, projectID, actor); err != nil {
		return nil, err
	}
	return s.repo.GetOwnerChatSession(ctx, projectID, sessionID, actor.MemberID)
}

// GetChatTurn returns one owner-scoped turn and its immutable execution data.
func (s *Service) GetChatTurn(ctx context.Context, projectID, sessionID, turnID uuid.UUID, actor agentdom.ChatActor) (*agentdom.TurnBundle, error) {
	if err := s.authorizeOwnerRead(ctx, projectID, actor); err != nil {
		return nil, err
	}
	bundle, err := s.repo.GetOwnerTurn(ctx, projectID, turnID, actor.MemberID)
	if err != nil {
		return nil, err
	}
	if bundle.Turn == nil || bundle.Turn.SessionID == nil || *bundle.Turn.SessionID != sessionID {
		return nil, agentdom.ErrTurnNotFound
	}
	return bundle, nil
}

// ListChatTurns returns paginated turns for an owner-scoped session.
func (s *Service) ListChatTurns(ctx context.Context, projectID, sessionID uuid.UUID, actor agentdom.ChatActor, limit int, beforeIndex *int) ([]*agentdom.TurnBundle, bool, error) {
	if err := s.authorizeOwnerRead(ctx, projectID, actor); err != nil {
		return nil, false, err
	}
	return s.repo.ListOwnerSessionTurns(ctx, projectID, sessionID, actor.MemberID, limit, beforeIndex)
}

// ListChatTurnEvents returns paginated durable events for an owner-scoped turn.
func (s *Service) ListChatTurnEvents(ctx context.Context, filter agentdom.TurnEventListFilter, actor agentdom.ChatActor) ([]*agentdom.AgentConversationEvent, bool, error) {
	if err := s.authorizeOwnerRead(ctx, filter.ProjectID, actor); err != nil {
		return nil, false, err
	}
	filter.MemberID = actor.MemberID
	return s.repo.ListOwnerTurnEvents(ctx, filter)
}

// ListLegacyChatExecutions returns read-only pre-turn chat history.
func (s *Service) ListLegacyChatExecutions(ctx context.Context, filter agentdom.LegacyExecutionListFilter, actor agentdom.ChatActor) ([]agentdom.LegacyChatExecution, bool, error) {
	if err := s.authorizeOwnerRead(ctx, filter.ProjectID, actor); err != nil {
		return nil, false, err
	}
	filter.MemberID = actor.MemberID
	return s.repo.ListOwnerSessionLegacyExecutions(ctx, filter)
}

// ListChatContextSources returns the live selection for the session's next turn.
func (s *Service) ListChatContextSources(ctx context.Context, projectID, sessionID uuid.UUID, actor agentdom.ChatActor) ([]agentdom.SessionContextSource, error) {
	if err := s.authorizeOwnerRead(ctx, projectID, actor); err != nil {
		return nil, err
	}
	return s.repo.ListSessionContextSources(ctx, projectID, sessionID, actor.MemberID)
}

// ReplaceChatContextSources replaces the authorized next-turn context selection.
func (s *Service) ReplaceChatContextSources(ctx context.Context, in agentdom.ReplaceChatContextInput) ([]agentdom.SessionContextSource, error) {
	sources, err := buildSessionSources(in.ProjectID, in.SessionID, in.Actor.MemberID, in.Sources, s.now().UTC())
	if err != nil {
		return nil, err
	}
	// Selection authorization is deliberately separate from per-turn snapshot
	// authorization. A later turn never trusts this earlier decision.
	if err := s.authorizeContext(ctx, in.ProjectID, in.Actor, sessionSourcesRequireTasksRead(sources)); err != nil {
		return nil, err
	}
	if _, err := s.repo.GetOwnerChatSession(ctx, in.ProjectID, in.SessionID, in.Actor.MemberID); err != nil {
		return nil, err
	}
	// Resolve canonical sources now so an invalid or foreign reference is not
	// persisted as a seemingly valid future selection. The content is discarded.
	if _, err := s.repo.ResolveContextItems(ctx, in.ProjectID, in.Actor.MemberID, uuid.New(), sources); err != nil {
		return nil, err
	}
	return s.repo.ReplaceSessionContextSources(ctx, in.ProjectID, in.SessionID, in.Actor.MemberID,
		in.Actor.UserID, in.Actor.LegacyRole, sources)
}

// CreateProjectChat creates a session and its first immutable turn.
func (s *Service) CreateProjectChat(ctx context.Context, in agentdom.CreateProjectChatInput) (*agentdom.TurnBundle, bool, error) {
	if err := validateActor(in.Actor); err != nil {
		return nil, false, err
	}
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	message, title, err := validateChatCommand(in.Message, in.Title, in.IdempotencyKey)
	if err != nil {
		return nil, false, err
	}
	// This is the source-selection authorization point.
	if err := s.authorizeContext(ctx, in.ProjectID, in.Actor, contextRefsRequireTasksRead(in.ContextSources)); err != nil {
		return nil, false, err
	}
	now := s.now().UTC()
	sessionID := uuid.New()
	turnID := uuid.New()
	snapshotID := uuid.New()
	sources, err := buildSessionSources(in.ProjectID, sessionID, in.Actor.MemberID, in.ContextSources, now)
	if err != nil {
		return nil, false, err
	}
	commandHash, err := projectChatCommandSHA(true, nil, in.ProjectID, in.AgentID, in.Actor.MemberID,
		message, title, in.ContextSources, in.DeadlineAt)
	if err != nil {
		return nil, false, err
	}
	existing, err := s.repo.GetOwnerCreatedChatByRequest(ctx, in.ProjectID, in.Actor.MemberID, in.IdempotencyKey)
	if err == nil {
		if existing == nil || existing.Turn == nil || existing.Turn.CommandSHA256 != commandHash {
			return nil, false, agentdom.ErrIdempotencyConflict
		}
		return existing, true, nil
	}
	if !errors.Is(err, agentdom.ErrTurnNotFound) {
		return nil, false, err
	}
	agent, err := s.agents.FindVisibleAgentInProject(ctx, in.ProjectID, in.AgentID)
	if err != nil {
		return nil, false, err
	}
	requestedDeadline, defaultTimeout, err := deadlineSpec(now, in.DeadlineAt, agent.TimeoutMinutes)
	if err != nil {
		return nil, false, err
	}
	// Re-authorize immediately before resolving canonical source data and
	// building the immutable snapshot. Selection-time grants are not reused.
	if err := s.authorizeContext(ctx, in.ProjectID, in.Actor, sessionSourcesRequireTasksRead(sources)); err != nil {
		return nil, false, err
	}
	items, err := s.repo.ResolveContextItems(ctx, in.ProjectID, in.Actor.MemberID, snapshotID, sources)
	if err != nil {
		return nil, false, err
	}
	snapshot, err := makeSnapshot(snapshotID, turnID, items, now)
	if err != nil {
		return nil, false, err
	}
	bundleInput := newCreateInput(in, agent, message, title, sessionID, turnID, snapshot, sources,
		requestedDeadline, defaultTimeout, now)
	return s.repo.CreateSessionTurn(ctx, bundleInput)
}

// AppendProjectChatTurn creates a follow-up turn from the current live context.
func (s *Service) AppendProjectChatTurn(ctx context.Context, in agentdom.AppendProjectChatTurnInput) (*agentdom.TurnBundle, bool, error) {
	if err := validateActor(in.Actor); err != nil {
		return nil, false, err
	}
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	message, _, err := validateChatCommand(in.Message, nil, in.IdempotencyKey)
	if err != nil {
		return nil, false, err
	}
	if err := s.authorizeOwnerRead(ctx, in.ProjectID, in.Actor); err != nil {
		return nil, false, err
	}
	session, err := s.repo.GetOwnerChatSession(ctx, in.ProjectID, in.SessionID, in.Actor.MemberID)
	if err != nil {
		return nil, false, err
	}
	now := s.now().UTC()
	commandHash, err := projectChatCommandSHA(false, &in.SessionID, in.ProjectID, session.AgentID,
		in.Actor.MemberID, message, nil, nil, in.DeadlineAt)
	if err != nil {
		return nil, false, err
	}
	existing, err := s.repo.GetOwnerSessionTurnByIdempotency(ctx, in.ProjectID, in.SessionID,
		in.Actor.MemberID, in.IdempotencyKey)
	if err == nil {
		if existing == nil || existing.Turn == nil || existing.Snapshot == nil {
			return nil, false, agentdom.ErrTurnNotFound
		}
		if err := s.authorizeContext(ctx, in.ProjectID, in.Actor, snapshotItemsRequireTasksRead(existing.Snapshot.Items)); err != nil {
			return nil, false, err
		}
		if existing.Turn.CommandSHA256 != commandHash {
			return nil, false, agentdom.ErrIdempotencyConflict
		}
		return existing, true, nil
	}
	if !errors.Is(err, agentdom.ErrTurnNotFound) {
		return nil, false, err
	}
	agent, err := s.agents.FindVisibleAgentInProject(ctx, in.ProjectID, session.AgentID)
	if err != nil {
		return nil, false, err
	}
	requestedDeadline, defaultTimeout, err := deadlineSpec(now, in.DeadlineAt, agent.TimeoutMinutes)
	if err != nil {
		return nil, false, err
	}
	sources, err := s.repo.ListSessionContextSources(ctx, in.ProjectID, in.SessionID, in.Actor.MemberID)
	if err != nil {
		return nil, false, err
	}
	// This is the per-turn snapshot authorization point. The permission result
	// from ReplaceChatContextSources is intentionally not cached or reused.
	if err := s.authorizeContext(ctx, in.ProjectID, in.Actor, sessionSourcesRequireTasksRead(sources)); err != nil {
		return nil, false, err
	}
	if len(sources) >= agentdom.MaxContextSources {
		// One immutable source slot is reserved for the conversation's own
		// prior stable turns, so supplemental selections may use at most the
		// remaining slots.
		return nil, false, agentdom.ErrContextSnapshotTooLarge
	}

	latest, _, err := s.repo.ListOwnerSessionTurns(ctx, in.ProjectID, in.SessionID, in.Actor.MemberID, 1, nil)
	if err != nil {
		return nil, false, err
	}
	if len(latest) != 1 || latest[0].Conversation == nil {
		return nil, false, agentdom.ErrTurnNotFound
	}
	turnID := uuid.New()
	snapshotID := uuid.New()
	snapshotSources := make([]agentdom.SessionContextSource, 0, len(sources)+1)
	snapshotSources = append(snapshotSources, agentdom.SessionContextSource{
		ID: uuid.New(), SessionID: in.SessionID, ProjectID: in.ProjectID,
		SourceType: agentdom.ContextSourceSession, SourceID: in.SessionID,
		Ordinal: 0, SelectedByMemberID: in.Actor.MemberID, CreatedAt: now,
	})
	for _, source := range sources {
		copy := source
		copy.Ordinal++
		snapshotSources = append(snapshotSources, copy)
	}
	items, err := s.repo.ResolveContextItems(ctx, in.ProjectID, in.Actor.MemberID, snapshotID, snapshotSources)
	if err != nil {
		return nil, false, err
	}
	snapshot, err := makeSnapshot(snapshotID, turnID, items, now)
	if err != nil {
		return nil, false, err
	}
	return s.repo.AppendSessionTurn(ctx, newAppendInput(in, session, agent, latest[0], message,
		snapshot, requestedDeadline, defaultTimeout, now))
}

// StopProjectChatTurn cancels a queued or running turn for its session owner.
func (s *Service) StopProjectChatTurn(ctx context.Context, in agentdom.StopProjectChatTurnInput) (*agentdom.TurnResult, error) {
	if err := validateActor(in.Actor); err != nil {
		return nil, err
	}
	if in.SessionID == uuid.Nil || in.TurnID == uuid.Nil {
		return nil, agentdom.ErrProjectChatInvalid
	}
	if err := s.authorizeOwnerRead(ctx, in.ProjectID, in.Actor); err != nil {
		return nil, err
	}
	return s.repo.StopOwnerTurn(ctx, agentdom.StopOwnerTurnInput{
		ProjectID: in.ProjectID, SessionID: in.SessionID, TurnID: in.TurnID,
		MemberID: in.Actor.MemberID, UserID: in.Actor.UserID, LegacyRole: in.Actor.LegacyRole,
	})
}

// PrepareProjectConclusion freezes an agent-generated write-back proposal.
func (s *Service) PrepareProjectConclusion(ctx context.Context, in agentdom.PrepareProjectConclusionInput) (*agentdom.ConclusionPreparation, bool, error) {
	if err := validateActor(in.Actor); err != nil {
		return nil, false, err
	}
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if in.SourceTurnID == uuid.Nil || in.TargetTaskID == uuid.Nil ||
		in.Kind != agentdom.ConclusionPublished || in.RelatedPublicationID != nil ||
		in.IdempotencyKey == "" || len(in.IdempotencyKey) > 200 || in.ExpiresAt.IsZero() {
		return nil, false, agentdom.ErrProjectChatInvalid
	}
	if err := s.authorize(ctx, in.ProjectID, in.Actor,
		authz.PermissionAgentsRead, authz.PermissionTasksRead, authz.PermissionTasksWrite); err != nil {
		return nil, false, err
	}
	if _, err := s.repo.GetOwnerTurn(ctx, in.ProjectID, in.SourceTurnID, in.Actor.MemberID); err != nil {
		return nil, false, err
	}
	return s.repo.PrepareConclusion(ctx, agentdom.PrepareConclusionInput{
		ID: uuid.New(), ProjectID: in.ProjectID, SourceTurnID: in.SourceTurnID,
		TargetTaskID: in.TargetTaskID, PreparedByUserID: in.Actor.UserID,
		PreparedByMemberID: in.Actor.MemberID, LegacyRole: in.Actor.LegacyRole, Kind: in.Kind,
		RelatedPublicationID: in.RelatedPublicationID, UpdateDescription: in.UpdateDescription,
		IdempotencyKey: in.IdempotencyKey, ExpiresAt: in.ExpiresAt,
	})
}

// ConfirmProjectConclusion atomically publishes a frozen write-back proposal.
func (s *Service) ConfirmProjectConclusion(ctx context.Context, in agentdom.ConfirmProjectConclusionInput) (*agentdom.ConclusionPublicationView, bool, error) {
	if err := validateActor(in.Actor); err != nil {
		return nil, false, err
	}
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if in.PreparationID == uuid.Nil || in.ExpectedVersion < 1 || len(in.ExpectedSHA256) != 64 ||
		in.IdempotencyKey == "" || len(in.IdempotencyKey) > 200 {
		return nil, false, agentdom.ErrProjectChatInvalid
	}
	prep, err := s.repo.GetOwnerConclusionPreparation(ctx, in.ProjectID, in.PreparationID, in.Actor.MemberID, in.Actor.UserID)
	if err != nil {
		return nil, false, err
	}
	// Confirmation is a distinct authorization point. Revocation after prepare
	// must stop publication even though the summary is already frozen.
	if err := s.authorize(ctx, in.ProjectID, in.Actor,
		authz.PermissionAgentsRead, authz.PermissionTasksRead, authz.PermissionTasksWrite); err != nil {
		return nil, false, err
	}
	if prep.ProjectID != in.ProjectID {
		return nil, false, agentdom.ErrConclusionNotFound
	}
	source, err := s.repo.GetOwnerTurn(ctx, in.ProjectID, prep.SourceTurnID, in.Actor.MemberID)
	if err != nil || source.Session == nil {
		if err != nil {
			return nil, false, err
		}
		return nil, false, agentdom.ErrTurnNotFound
	}
	publication, replayed, err := s.repo.ConfirmConclusion(ctx, agentdom.ConfirmConclusionInput{
		PreparationID: in.PreparationID, ProjectID: in.ProjectID,
		PublishedByUserID: in.Actor.UserID, PublishedByMemberID: in.Actor.MemberID,
		LegacyRole:      in.Actor.LegacyRole,
		ExpectedVersion: in.ExpectedVersion, ExpectedSHA256: in.ExpectedSHA256,
		IdempotencyKey: in.IdempotencyKey,
	})
	if err != nil {
		return nil, false, err
	}
	sessionID := source.Session.ID
	turnID := publication.SourceTurnID
	return &agentdom.ConclusionPublicationView{
		Publication: *publication, SourceAccessible: true,
		SourceSessionID: &sessionID, SourceTurnID: &turnID,
	}, replayed, nil
}

// ListProjectTaskConclusions returns viewer-safe publications for a task.
func (s *Service) ListProjectTaskConclusions(ctx context.Context, filter agentdom.ConclusionPublicationListFilter, actor agentdom.ChatActor) ([]agentdom.ConclusionPublicationView, bool, error) {
	// The task route has already enforced the task's public-or-tasks.read
	// audience. An authenticated project member is resolved here only so the
	// private source link can receive its separate agents.read + owner check.
	if actor.UserID != uuid.Nil {
		filter.ViewerMemberID = actor.MemberID
	}
	views, hasMore, err := s.repo.ListTaskConclusionPublications(ctx, filter)
	if err != nil {
		return nil, false, err
	}
	sourceAllowed := false
	if actor.UserID != uuid.Nil {
		sourceAllowed, err = s.hasPermissions(ctx, filter.ProjectID, actor, authz.PermissionAgentsRead)
		if err != nil {
			return nil, false, err
		}
	}
	if !sourceAllowed {
		for index := range views {
			views[index].SourceAccessible = false
			views[index].SourceSessionID = nil
			views[index].SourceTurnID = nil
		}
	}
	return views, hasMore, nil
}

func (s *Service) authorizeOwnerRead(ctx context.Context, projectID uuid.UUID, actor agentdom.ChatActor) error {
	if err := validateActor(actor); err != nil {
		return agentdom.ErrChatSessionNotFound
	}
	allowed, err := s.hasPermissions(ctx, projectID, actor, authz.PermissionAgentsRead)
	if err != nil {
		return err
	}
	if !allowed {
		// Owner-private known IDs remain indistinguishable from missing IDs.
		return agentdom.ErrChatSessionNotFound
	}
	return nil
}

func (s *Service) authorizeContext(ctx context.Context, projectID uuid.UUID, actor agentdom.ChatActor, requiresTasksRead bool) error {
	required := []authz.Permission{authz.PermissionAgentsRead}
	if requiresTasksRead {
		required = append(required, authz.PermissionTasksRead)
	}
	return s.authorize(ctx, projectID, actor, required...)
}

func contextRefsRequireTasksRead(sources []agentdom.ContextSourceRef) bool {
	for _, source := range sources {
		if source.Type == agentdom.ContextSourceTask {
			return true
		}
	}
	return false
}

func sessionSourcesRequireTasksRead(sources []agentdom.SessionContextSource) bool {
	for _, source := range sources {
		if source.SourceType == agentdom.ContextSourceTask {
			return true
		}
	}
	return false
}

func snapshotItemsRequireTasksRead(items []agentdom.TurnContextItem) bool {
	for _, item := range items {
		if item.SourceType == agentdom.ContextSourceTask {
			return true
		}
	}
	return false
}

func (s *Service) authorize(ctx context.Context, projectID uuid.UUID, actor agentdom.ChatActor, required ...authz.Permission) error {
	if err := validateActor(actor); err != nil {
		return err
	}
	allowed, err := s.hasPermissions(ctx, projectID, actor, required...)
	if err != nil {
		return err
	}
	if !allowed {
		return agentdom.ErrProjectChatForbidden
	}
	return nil
}

func (s *Service) hasPermissions(ctx context.Context, projectID uuid.UUID, actor agentdom.ChatActor, required ...authz.Permission) (bool, error) {
	if s.authorizer == nil {
		return false, errors.New("agent project chat: authorizer is not configured")
	}
	return s.authorizer.HasPermissions(ctx, actor.UserID, &projectID, actor.LegacyRole, required...)
}

func validateActor(actor agentdom.ChatActor) error {
	if actor.UserID == uuid.Nil || actor.MemberID == uuid.Nil {
		return agentdom.ErrProjectChatForbidden
	}
	return nil
}

func validateChatCommand(message string, title *string, idempotencyKey string) (string, *string, error) {
	message = strings.TrimSpace(message)
	if message == "" || len([]byte(message)) > agentdom.MaxTurnInputBytes {
		return "", nil, fmt.Errorf("%w: invalid message", agentdom.ErrProjectChatInvalid)
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" || len(idempotencyKey) > 200 {
		return "", nil, fmt.Errorf("%w: invalid idempotency key", agentdom.ErrProjectChatInvalid)
	}
	if title != nil {
		trimmed := strings.TrimSpace(*title)
		if len([]byte(trimmed)) > maxSessionTitle {
			return "", nil, fmt.Errorf("%w: title is too long", agentdom.ErrProjectChatInvalid)
		}
		if trimmed == "" {
			title = nil
		} else {
			title = &trimmed
		}
	}
	return message, title, nil
}

func buildSessionSources(projectID, sessionID, memberID uuid.UUID, refs []agentdom.ContextSourceRef, now time.Time) ([]agentdom.SessionContextSource, error) {
	if len(refs) > agentdom.MaxContextSources {
		return nil, agentdom.ErrContextSnapshotTooLarge
	}
	seen := make(map[string]struct{}, len(refs))
	sources := make([]agentdom.SessionContextSource, 0, len(refs))
	for index, ref := range refs {
		if ref.ID == uuid.Nil || ref.Type != agentdom.ContextSourceTask &&
			ref.Type != agentdom.ContextSourceSession && ref.Type != agentdom.ContextSourceRun {
			return nil, agentdom.ErrContextSourceForbidden
		}
		if ref.Type == agentdom.ContextSourceSession && ref.ID == sessionID {
			return nil, agentdom.ErrContextSourceForbidden
		}
		key := string(ref.Type) + ":" + ref.ID.String()
		if _, duplicate := seen[key]; duplicate {
			return nil, agentdom.ErrContextSourceForbidden
		}
		seen[key] = struct{}{}
		sources = append(sources, agentdom.SessionContextSource{
			ID: uuid.New(), SessionID: sessionID, ProjectID: projectID,
			SourceType: ref.Type, SourceID: ref.ID, Ordinal: index,
			SelectedByMemberID: memberID, CreatedAt: now,
		})
	}
	return sources, nil
}

func makeSnapshot(snapshotID, turnID uuid.UUID, items []agentdom.TurnContextItem, now time.Time) (agentdom.TurnContextSnapshot, error) {
	return agentdom.CanonicalizeContextSnapshot(agentdom.TurnContextSnapshot{
		ID: snapshotID, TurnID: turnID, SchemaVersion: 1,
		CreatedAt: now, Items: items,
	})
}

func deadlineSpec(now time.Time, requested *time.Time, timeoutMinutes int) (*time.Time, time.Duration, error) {
	if requested != nil {
		deadline := requested.UTC()
		if !deadline.After(now) || deadline.After(now.Add(maxTurnDeadline)) {
			return nil, 0, fmt.Errorf("%w: invalid deadline", agentdom.ErrProjectChatInvalid)
		}
		return &deadline, 0, nil
	}
	timeout := defaultTurnTimeout
	if timeoutMinutes > 0 {
		timeout = time.Duration(timeoutMinutes) * time.Minute
		if timeout > maxTurnDeadline {
			timeout = maxTurnDeadline
		}
	}
	return nil, timeout, nil
}

func projectChatCommandSHA(newSession bool, sessionID *uuid.UUID, projectID uuid.UUID,
	agentID, memberID uuid.UUID, message string, title *string,
	sources []agentdom.ContextSourceRef, requestedDeadline *time.Time,
) (string, error) {
	return (agentdom.ProjectChatCommand{
		NewSession: newSession, SessionID: sessionID, ProjectID: projectID,
		AgentID: agentID, RequestedByMemberID: memberID, InputText: message,
		ContextSources:    append([]agentdom.ContextSourceRef(nil), sources...),
		RequestedDeadline: requestedDeadline, Title: title,
	}).SHA256()
}

func turnBackend(agent *agentdom.Agent) agentdom.TurnBackend {
	if agent.AgentType == agentdom.AgentTypeACP {
		return agentdom.TurnBackendACP
	}
	return agentdom.TurnBackendLLM
}

func newCreateInput(in agentdom.CreateProjectChatInput, agent *agentdom.Agent, message string, title *string, sessionID, turnID uuid.UUID, snapshot agentdom.TurnContextSnapshot, sources []agentdom.SessionContextSource, requestedDeadline *time.Time, defaultTimeout time.Duration, now time.Time) agentdom.CreateSessionTurnInput {
	conversationID := uuid.New()
	runID := uuid.New()
	memberID := in.Actor.MemberID
	projectID := in.ProjectID
	return agentdom.CreateSessionTurnInput{
		Session: agentdom.AgentChatSession{
			ID: sessionID, AgentID: agent.ID, ProjectID: projectID, MemberID: memberID,
			Title: title, LastMessageAt: &now, CreatedAt: now, UpdatedAt: now,
		},
		Conversation: agentdom.AgentConversation{
			ID: conversationID, AgentID: agent.ID, ProjectID: projectID,
			TriggerType: "chat_message", ChatSessionID: &sessionID,
			TriggeredByMemberID: &memberID, Status: string(agentdom.ConversationStatusQueued),
			CreatedAt: now, UpdatedAt: now,
		},
		Turn: agentdom.AgentTurn{
			ID: turnID, SessionID: &sessionID, ConversationID: conversationID,
			ProjectID: &projectID, AgentID: agent.ID, RequestedByMemberID: &memberID,
			TurnIndex: 1, InputText: message, Status: agentdom.TurnStatusQueued,
			IdempotencyKey: in.IdempotencyKey, ToolPolicy: agentdom.PrivateChatToolPolicy(),
			DeadlineAt: requestedDeadline, CreatedAt: now, UpdatedAt: now,
		},
		Run: agentdom.TurnRun{
			ID: runID, TurnID: turnID, ConversationID: conversationID,
			Backend: turnBackend(agent), Attempt: 1, Status: agentdom.TurnStatusQueued,
			CreatedAt: now, UpdatedAt: now,
		},
		Snapshot: snapshot, SelectedSources: sources, ClientRequestID: in.IdempotencyKey,
		AuthorizedUserID: in.Actor.UserID, LegacyRole: in.Actor.LegacyRole,
		RequestedDeadline: requestedDeadline, DefaultTimeout: defaultTimeout,
	}
}

func newAppendInput(in agentdom.AppendProjectChatTurnInput, session *agentdom.AgentChatSession, agent *agentdom.Agent, latest *agentdom.TurnBundle, message string, snapshot agentdom.TurnContextSnapshot, requestedDeadline *time.Time, defaultTimeout time.Duration, now time.Time) agentdom.AppendSessionTurnInput {
	backend := turnBackend(agent)
	reuse := backend == agentdom.TurnBackendACP && latest.Conversation.Status == string(agentdom.ConversationStatusPaused)
	conversation := *latest.Conversation
	if !reuse {
		conversation = agentdom.AgentConversation{
			ID: uuid.New(), AgentID: agent.ID, ProjectID: in.ProjectID,
			TriggerType: "chat_message", ChatSessionID: &in.SessionID,
			TriggeredByMemberID: &in.Actor.MemberID, Status: string(agentdom.ConversationStatusQueued),
			CreatedAt: now, UpdatedAt: now,
		}
	}
	turnID := snapshot.TurnID
	return agentdom.AppendSessionTurnInput{
		SessionID: in.SessionID, ProjectID: in.ProjectID, MemberID: in.Actor.MemberID,
		Conversation: conversation, ReuseConversation: reuse,
		Turn: agentdom.AgentTurn{
			ID: turnID, SessionID: &in.SessionID, ConversationID: conversation.ID,
			ProjectID: &in.ProjectID, AgentID: session.AgentID,
			RequestedByMemberID: &in.Actor.MemberID, InputText: message,
			Status: agentdom.TurnStatusQueued, IdempotencyKey: in.IdempotencyKey,
			ToolPolicy: agentdom.PrivateChatToolPolicy(), DeadlineAt: requestedDeadline,
			CreatedAt: now, UpdatedAt: now,
		},
		Run: agentdom.TurnRun{
			ID: uuid.New(), TurnID: turnID, ConversationID: conversation.ID,
			Backend: backend, Attempt: 1, Status: agentdom.TurnStatusQueued,
			CreatedAt: now, UpdatedAt: now,
		},
		Snapshot: snapshot, AuthorizedUserID: in.Actor.UserID, LegacyRole: in.Actor.LegacyRole,
		RequestedDeadline: requestedDeadline, DefaultTimeout: defaultTimeout,
	}
}

var _ agentdom.ProjectChatService = (*Service)(nil)
