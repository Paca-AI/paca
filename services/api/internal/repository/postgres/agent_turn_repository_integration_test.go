package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
)

// This test is intentionally opt-in because it exercises PostgreSQL triggers,
// row locks and transactions that a SQL mock cannot model. CI and local
// verification set PACA_TEST_DATABASE_URL to a freshly migrated disposable DB.
func TestAgentTurnRepositoryLifecycle(t *testing.T) {
	dsn := os.Getenv("PACA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PACA_TEST_DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", dsn)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	ids := seedAgentTurnTestScope(t, ctx, db)
	repo := NewAgentTurnRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	bundleInput := newSessionTurnInput(t, ids, now, "request-1", "first question")

	bundle, replayed, err := repo.CreateSessionTurn(ctx, bundleInput)
	if err != nil {
		t.Fatalf("create session turn: %v", err)
	}
	if replayed || bundle.Turn.Status != agentdom.TurnStatusQueued || bundle.Turn.TurnIndex != 1 {
		t.Fatalf("unexpected first turn: replayed=%v turn=%+v", replayed, bundle.Turn)
	}
	if bundle.Turn.DeadlineAt == nil || bundle.Turn.DeadlineAt.Before(time.Now().UTC().Add(29*time.Minute)) {
		t.Fatalf("default deadline was not materialized transactionally: %v", bundle.Turn.DeadlineAt)
	}

	replayedBundle, replayed, err := repo.CreateSessionTurn(ctx, bundleInput)
	if err != nil || !replayed || replayedBundle.Turn.ID != bundle.Turn.ID {
		t.Fatalf("replay first turn: replayed=%v err=%v", replayed, err)
	}
	freshRetry := newSessionTurnInput(t, ids, now.Add(time.Second), "request-1", "first question")
	freshRetryBundle, replayed, err := repo.CreateSessionTurn(ctx, freshRetry)
	if err != nil || !replayed || freshRetryBundle.Session.ID != bundle.Session.ID || freshRetryBundle.Turn.ID != bundle.Turn.ID {
		t.Fatalf("fresh command replay: bundle=%+v replayed=%v err=%v", freshRetryBundle, replayed, err)
	}
	conflictInput := bundleInput
	conflictInput.Turn.InputText = "different payload"
	if _, _, err := repo.CreateSessionTurn(ctx, conflictInput); !errors.Is(err, agentdom.ErrIdempotencyConflict) {
		t.Fatalf("expected idempotency payload conflict, got %v", err)
	}

	appendInput := newAppendTurnInput(t, ids, bundle.Session.ID, now.Add(time.Second), "turn-request-2", "second question")
	if _, _, err := repo.AppendSessionTurn(ctx, appendInput); !errors.Is(err, agentdom.ErrTurnBusy) {
		t.Fatalf("expected active turn conflict, got %v", err)
	}

	claimed, err := repo.ClaimTurnRun(ctx, agentdom.ClaimTurnRunInput{
		TurnID: bundle.Turn.ID, WorkerID: "integration-worker", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("claim turn run: %v", err)
	}
	if claimed.Bundle.Turn.Status != agentdom.TurnStatusRunning {
		t.Fatalf("claimed turn status = %s", claimed.Bundle.Turn.Status)
	}
	replayedClaim, err := repo.ClaimTurnRun(ctx, agentdom.ClaimTurnRunInput{
		TurnID: bundle.Turn.ID, WorkerID: "integration-worker", LeaseDuration: time.Minute,
	})
	if err != nil || replayedClaim.ClaimToken != claimed.ClaimToken || replayedClaim.Bundle.Run.ID != claimed.Bundle.Run.ID {
		t.Fatalf("replay claim after response loss: claim=%+v err=%v", replayedClaim, err)
	}
	if _, err := repo.ClaimTurnRun(ctx, agentdom.ClaimTurnRunInput{
		TurnID: bundle.Turn.ID, WorkerID: "other-worker", LeaseDuration: time.Minute,
	}); !errors.Is(err, agentdom.ErrTurnBusy) {
		t.Fatalf("expected live lease conflict, got %v", err)
	}

	stable := "stable private answer"
	eventID := uuid.New()
	stableCreatedAt := now.Add(2 * time.Second)
	if _, err := repo.AppendTurnEvent(ctx, agentdom.AppendTurnEventInput{
		ID: eventID, TurnID: bundle.Turn.ID, RunID: bundle.Run.ID,
		ClaimToken: claimed.ClaimToken, TurnSequence: 0,
		EventType: agentdom.StableOutputEventType, EventSource: "agent",
		Payload: []byte(`{"text":"stable private answer"}`), CreatedAt: stableCreatedAt,
	}); err != nil {
		t.Fatalf("insert stable output event: %v", err)
	}
	if _, err := repo.ClaimTurnRun(ctx, agentdom.ClaimTurnRunInput{
		TurnID: bundle.Turn.ID, WorkerID: "integration-worker", LeaseDuration: time.Minute,
	}); !errors.Is(err, agentdom.ErrTurnBusy) {
		t.Fatalf("expected replay after durable event to wait for a new attempt, got %v", err)
	}
	result, err := repo.FinalizeTurn(ctx, agentdom.FinalizeTurnInput{
		RunID:              bundle.Run.ID,
		ClaimToken:         claimed.ClaimToken,
		TerminalStatus:     agentdom.TurnStatusSucceeded,
		StableOutputEvent:  &eventID,
		GeneratedByAgentID: ids.agentID,
		Disposition:        agentdom.RuntimeRetired,
		FinalEventSequence: intPointer(0),
	})
	if err != nil || result.TerminalStatus != agentdom.TurnStatusSucceeded {
		t.Fatalf("finalize successful turn: result=%+v err=%v", result, err)
	}
	if _, err := repo.FinalizeTurn(ctx, agentdom.FinalizeTurnInput{
		RunID:              bundle.Run.ID,
		ClaimToken:         claimed.ClaimToken,
		TerminalStatus:     agentdom.TurnStatusSucceeded,
		StableOutputEvent:  &eventID,
		GeneratedByAgentID: ids.agentID,
		Disposition:        agentdom.RuntimeRetired,
		FinalEventSequence: intPointer(0),
	}); err != nil {
		t.Fatalf("replay finalization: %v", err)
	}
	if _, err := repo.AppendTurnEvent(ctx, agentdom.AppendTurnEventInput{
		ID: eventID, TurnID: bundle.Turn.ID, RunID: bundle.Run.ID,
		ClaimToken: claimed.ClaimToken, TurnSequence: 0,
		EventType: agentdom.StableOutputEventType, EventSource: "agent",
		Payload: []byte(`{"text":"stable private answer"}`), CreatedAt: stableCreatedAt,
	}); err != nil {
		t.Fatalf("replay stable event after terminal status ACK loss: %v", err)
	}

	appended, replayed, err := repo.AppendSessionTurn(ctx, appendInput)
	if err != nil || replayed || appended.Turn.TurnIndex != 2 {
		t.Fatalf("append second turn: bundle=%+v replayed=%v err=%v", appended, replayed, err)
	}
	unrelatedTaskID := uuid.New()
	if _, err := db.ExecContext(ctx, `INSERT INTO tasks
		(id,project_id,task_number,title,reporter_id) VALUES ($1,$2,2,'Unrelated task',$3)`,
		unrelatedTaskID, ids.projectID, ids.memberID); err != nil {
		t.Fatalf("insert unrelated task: %v", err)
	}
	if _, _, err := repo.PrepareConclusion(ctx, agentdom.PrepareConclusionInput{
		ID: uuid.New(), ProjectID: ids.projectID, SourceTurnID: bundle.Turn.ID,
		TargetTaskID: unrelatedTaskID, PreparedByUserID: ids.userID, PreparedByMemberID: ids.memberID,
		Kind: agentdom.ConclusionPublished, IdempotencyKey: "prepare-unrelated",
		ExpiresAt: now.Add(time.Hour),
	}); !errors.Is(err, agentdom.ErrTurnResultNotPublishable) {
		t.Fatalf("same-project task outside the source snapshot was publishable: %v", err)
	}

	prep, replayed, err := repo.PrepareConclusion(ctx, agentdom.PrepareConclusionInput{
		ID:                 uuid.New(),
		ProjectID:          ids.projectID,
		SourceTurnID:       bundle.Turn.ID,
		TargetTaskID:       ids.taskID,
		PreparedByUserID:   ids.userID,
		PreparedByMemberID: ids.memberID,
		Kind:               agentdom.ConclusionPublished,
		UpdateDescription:  true,
		IdempotencyKey:     "prepare-1",
		ExpiresAt:          now.Add(time.Hour),
	})
	if err != nil || replayed || prep.Summary != stable || !prep.IsFrozen || !prep.UpdateDescription {
		t.Fatalf("prepare conclusion: prep=%+v replayed=%v err=%v", prep, replayed, err)
	}
	publication, replayed, err := repo.ConfirmConclusion(ctx, agentdom.ConfirmConclusionInput{
		PreparationID:       prep.ID,
		ProjectID:           ids.projectID,
		PublishedByUserID:   ids.userID,
		PublishedByMemberID: ids.memberID,
		ExpectedVersion:     prep.SummaryVersion,
		ExpectedSHA256:      prep.SummarySHA256,
		IdempotencyKey:      "confirm-1",
	})
	if err != nil || replayed || publication.Summary != stable || !publication.DescriptionUpdated {
		t.Fatalf("confirm conclusion: publication=%+v replayed=%v err=%v", publication, replayed, err)
	}
	var updatedDescription json.RawMessage
	if err := db.GetContext(ctx, &updatedDescription, `SELECT description FROM tasks WHERE id=$1`, ids.taskID); err != nil ||
		!strings.Contains(string(updatedDescription), "stable private answer") {
		t.Fatalf("description proposal was not applied atomically: description=%s err=%v", updatedDescription, err)
	}
	var descriptionActivityCount int
	if err := db.GetContext(ctx, &descriptionActivityCount, `SELECT COUNT(*) FROM task_activities
		WHERE task_id=$1 AND activity_type='task.updated'
		  AND content->>'conclusion_publication_id'=$2`, ids.taskID, publication.ID.String()); err != nil || descriptionActivityCount != 1 {
		t.Fatalf("description audit activity count=%d err=%v", descriptionActivityCount, err)
	}
	var descriptionConclusionActivityCount int
	if err := db.GetContext(ctx, &descriptionConclusionActivityCount, `SELECT COUNT(*) FROM task_activities
		WHERE task_id=$1 AND activity_type LIKE 'agent.conclusion.%'
		  AND content->>'publication_id'=$2`, ids.taskID, publication.ID.String()); err != nil || descriptionConclusionActivityCount != 0 {
		t.Fatalf("description writeback created a duplicate conclusion activity: count=%d err=%v",
			descriptionConclusionActivityCount, err)
	}
	if _, _, err := repo.ConfirmConclusion(ctx, agentdom.ConfirmConclusionInput{
		PreparationID: prep.ID, ProjectID: ids.projectID,
		PublishedByUserID: ids.userID, PublishedByMemberID: ids.memberID,
		ExpectedVersion: prep.SummaryVersion, ExpectedSHA256: prep.SummarySHA256,
		IdempotencyKey: "confirm-new-alias",
	}); !errors.Is(err, agentdom.ErrIdempotencyConflict) {
		t.Fatalf("confirmed preparation accepted an unpersisted key alias: %v", err)
	}
	legacyConversationID := uuid.New()
	if _, err := db.ExecContext(ctx, `INSERT INTO agent_conversations
		(id,agent_id,project_id,trigger_type,chat_session_id,triggered_by_member_id,status,created_at,updated_at)
		VALUES ($1,$2,$3,'chat_message',$4,$5,'finished',$6,$6)`, legacyConversationID,
		ids.agentID, ids.projectID, bundle.Session.ID, ids.memberID, now.Add(-time.Hour)); err != nil {
		t.Fatalf("insert legacy chat execution: %v", err)
	}

	sessions, hasMore, err := repo.ListOwnerChatSessions(ctx, agentdom.ChatSessionListFilter{
		ProjectID: ids.projectID, MemberID: ids.memberID, Limit: 20,
	})
	if err != nil || hasMore || len(sessions) != 1 || sessions[0].Session.ID != bundle.Session.ID ||
		sessions[0].LatestTurn == nil || sessions[0].LatestTurn.ID != appended.Turn.ID || !sessions[0].HasLegacyExecutions {
		t.Fatalf("session-first history: sessions=%+v hasMore=%v err=%v", sessions, hasMore, err)
	}
	legacyExecutions, hasMoreLegacy, err := repo.ListOwnerSessionLegacyExecutions(ctx, agentdom.LegacyExecutionListFilter{
		ProjectID: ids.projectID, SessionID: bundle.Session.ID, MemberID: ids.memberID, Limit: 30,
	})
	if err != nil || hasMoreLegacy || len(legacyExecutions) != 1 || legacyExecutions[0].ConversationID != legacyConversationID {
		t.Fatalf("legacy execution compatibility: executions=%+v hasMore=%v err=%v", legacyExecutions, hasMoreLegacy, err)
	}
	foreignSessions, _, err := repo.ListOwnerChatSessions(ctx, agentdom.ChatSessionListFilter{
		ProjectID: ids.projectID, MemberID: uuid.New(), Limit: 20,
	})
	if err != nil || len(foreignSessions) != 0 {
		t.Fatalf("foreign owner history leaked: sessions=%+v err=%v", foreignSessions, err)
	}
	turns, hasMore, err := repo.ListOwnerSessionTurns(ctx, ids.projectID, bundle.Session.ID, ids.memberID, 20, nil)
	if err != nil || hasMore || len(turns) != 2 || turns[0].Turn.ID != appended.Turn.ID || turns[1].Turn.ID != bundle.Turn.ID {
		t.Fatalf("session turns: turns=%+v hasMore=%v err=%v", turns, hasMore, err)
	}
	turnPage, hasMore, err := repo.ListOwnerSessionTurns(ctx, ids.projectID, bundle.Session.ID, ids.memberID, 1, nil)
	if err != nil || !hasMore || len(turnPage) != 1 {
		t.Fatalf("first turn page: turns=%+v hasMore=%v err=%v", turnPage, hasMore, err)
	}
	nextBefore := turnPage[0].Turn.TurnIndex
	olderTurnPage, hasMore, err := repo.ListOwnerSessionTurns(ctx, ids.projectID, bundle.Session.ID, ids.memberID, 1, &nextBefore)
	if err != nil || hasMore || len(olderTurnPage) != 1 || olderTurnPage[0].Turn.ID == turnPage[0].Turn.ID {
		t.Fatalf("second turn page overlapped: turns=%+v hasMore=%v err=%v", olderTurnPage, hasMore, err)
	}
	if _, err := repo.GetOwnerTurn(ctx, ids.projectID, bundle.Turn.ID, uuid.New()); !errors.Is(err, agentdom.ErrTurnNotFound) {
		t.Fatalf("foreign turn lookup error = %v", err)
	}
	events, _, err := repo.ListOwnerTurnEvents(ctx, agentdom.TurnEventListFilter{
		ProjectID: ids.projectID, TurnID: bundle.Turn.ID, MemberID: ids.memberID,
	})
	if err != nil || len(events) != 1 || events[0].ID != eventID {
		t.Fatalf("owner turn events: events=%+v err=%v", events, err)
	}
	selected, err := repo.ReplaceSessionContextSources(ctx, ids.projectID, bundle.Session.ID, ids.memberID,
		ids.userID, "", []agentdom.SessionContextSource{{
			SourceType: agentdom.ContextSourceTask, SourceID: ids.taskID,
		}})
	if err != nil || len(selected) != 1 || selected[0].SessionID != bundle.Session.ID {
		t.Fatalf("replace context sources: sources=%+v err=%v", selected, err)
	}
	resolved, err := repo.ResolveContextItems(ctx, ids.projectID, ids.memberID, uuid.New(), selected)
	if err != nil || len(resolved) != 1 || resolved[0].SourceID != ids.taskID ||
		!strings.Contains(resolved[0].RenderedText, "UNTRUSTED CONTEXT") {
		t.Fatalf("resolve context items: items=%+v err=%v", resolved, err)
	}
	loadedPreparation, err := repo.GetOwnerConclusionPreparation(ctx, ids.projectID, prep.ID, ids.memberID, ids.userID)
	if err != nil || loadedPreparation.ID != prep.ID {
		t.Fatalf("owner preparation: preparation=%+v err=%v", loadedPreparation, err)
	}
	if _, err := repo.GetOwnerConclusionPreparation(ctx, ids.projectID, prep.ID, uuid.New(), ids.userID); !errors.Is(err, agentdom.ErrConclusionNotFound) {
		t.Fatalf("foreign preparation lookup error = %v", err)
	}
	ownerViews, _, err := repo.ListTaskConclusionPublications(ctx, agentdom.ConclusionPublicationListFilter{
		ProjectID: ids.projectID, TaskID: ids.taskID, ViewerMemberID: ids.memberID,
	})
	if err != nil || len(ownerViews) != 1 || !ownerViews[0].SourceAccessible ||
		ownerViews[0].SourceSessionID == nil || ownerViews[0].SourceTurnID == nil {
		t.Fatalf("owner publication view: views=%+v err=%v", ownerViews, err)
	}
	readerViews, _, err := repo.ListTaskConclusionPublications(ctx, agentdom.ConclusionPublicationListFilter{
		ProjectID: ids.projectID, TaskID: ids.taskID, ViewerMemberID: uuid.New(),
	})
	if err != nil || len(readerViews) != 1 || readerViews[0].SourceAccessible ||
		readerViews[0].SourceSessionID != nil || readerViews[0].SourceTurnID != nil {
		t.Fatalf("non-owner publication source leaked: views=%+v err=%v", readerViews, err)
	}

	var activity struct {
		ActivityType string          `db:"activity_type"`
		Content      json.RawMessage `db:"content"`
	}
	if err := db.GetContext(ctx, &activity, `SELECT activity_type, content
		FROM task_activities
		WHERE task_id=$1 AND content->>'conclusion_publication_id'=$2`, ids.taskID, publication.ID.String()); err != nil {
		t.Fatalf("load description writeback activity: %v", err)
	}
	if activity.ActivityType != "task.updated" {
		t.Fatalf("description writeback used the wrong activity projection: type=%s content=%s",
			activity.ActivityType, activity.Content)
	}
	if _, err := db.ExecContext(ctx, `UPDATE agents SET deleted_at=NOW() WHERE id=$1`, ids.agentID); err != nil {
		t.Fatalf("soft-delete session agent: %v", err)
	}
	deletedAgentSessions, _, err := repo.ListOwnerChatSessions(ctx, agentdom.ChatSessionListFilter{
		ProjectID: ids.projectID, MemberID: ids.memberID, Limit: 20,
	})
	if err != nil || len(deletedAgentSessions) != 1 || deletedAgentSessions[0].Session.ID != bundle.Session.ID {
		t.Fatalf("soft-deleted agent hid persistent chat history: sessions=%+v err=%v", deletedAgentSessions, err)
	}

	var legacyCount int
	if err := db.GetContext(ctx, &legacyCount, `SELECT COUNT(*) FROM task_activities
		WHERE activity_type='agent.session.finished'`); err != nil || legacyCount != 0 {
		t.Fatalf("legacy automatic activity count=%d err=%v", legacyCount, err)
	}
}

func TestAgentConclusionSummaryOnlyCreatesConclusionActivity(t *testing.T) {
	db := openAgentTurnTestDB(t)
	ctx := context.Background()
	ids := seedAgentTurnTestScope(t, ctx, db)
	repo := NewAgentTurnRepository(db)
	source := createSucceededTurn(t, ctx, repo, ids, "summary-only", agentdom.RuntimeRetired)
	now := time.Now().UTC()
	preparation, replayed, err := repo.PrepareConclusion(ctx, agentdom.PrepareConclusionInput{
		ID: uuid.New(), ProjectID: ids.projectID, SourceTurnID: source.Turn.ID,
		TargetTaskID: ids.taskID, PreparedByUserID: ids.userID, PreparedByMemberID: ids.memberID,
		Kind:           agentdom.ConclusionPublished,
		IdempotencyKey: "summary-only-prepare", ExpiresAt: now.Add(30 * time.Minute),
	})
	if err != nil || replayed {
		t.Fatalf("prepare summary-only conclusion: replayed=%v err=%v", replayed, err)
	}
	concurrentPreparation, replayed, err := repo.PrepareConclusion(ctx, agentdom.PrepareConclusionInput{
		ID: uuid.New(), ProjectID: ids.projectID, SourceTurnID: source.Turn.ID,
		TargetTaskID: ids.taskID, PreparedByUserID: ids.userID, PreparedByMemberID: ids.memberID,
		Kind: agentdom.ConclusionPublished, IdempotencyKey: "summary-only-concurrent-prepare",
		ExpiresAt: now.Add(30 * time.Minute),
	})
	if err != nil || replayed {
		t.Fatalf("prepare concurrent publication: replayed=%v err=%v", replayed, err)
	}
	publication, replayed, err := repo.ConfirmConclusion(ctx, agentdom.ConfirmConclusionInput{
		PreparationID: preparation.ID, ProjectID: ids.projectID,
		PublishedByUserID: ids.userID, PublishedByMemberID: ids.memberID,
		ExpectedVersion: preparation.SummaryVersion, ExpectedSHA256: preparation.SummarySHA256,
		IdempotencyKey: "summary-only-confirm",
	})
	if err != nil || replayed || publication.DescriptionUpdated {
		t.Fatalf("confirm summary-only conclusion: publication=%+v replayed=%v err=%v",
			publication, replayed, err)
	}
	if _, _, err := repo.ConfirmConclusion(ctx, agentdom.ConfirmConclusionInput{
		PreparationID: concurrentPreparation.ID, ProjectID: ids.projectID,
		PublishedByUserID: ids.userID, PublishedByMemberID: ids.memberID,
		ExpectedVersion: concurrentPreparation.SummaryVersion,
		ExpectedSHA256:  concurrentPreparation.SummarySHA256,
		IdempotencyKey:  "summary-only-concurrent-confirm",
	}); !errors.Is(err, agentdom.ErrConclusionConflict) {
		t.Fatalf("concurrent preparation published the same source twice: %v", err)
	}
	if _, _, err := repo.PrepareConclusion(ctx, agentdom.PrepareConclusionInput{
		ID: uuid.New(), ProjectID: ids.projectID, SourceTurnID: source.Turn.ID,
		TargetTaskID: ids.taskID, PreparedByUserID: ids.userID, PreparedByMemberID: ids.memberID,
		Kind: agentdom.ConclusionPublished, IdempotencyKey: "summary-only-duplicate",
		ExpiresAt: now.Add(30 * time.Minute),
	}); !errors.Is(err, agentdom.ErrConclusionConflict) {
		t.Fatalf("published source turn was publishable a second time: %v", err)
	}
	var activity struct {
		ActivityType string          `db:"activity_type"`
		Content      json.RawMessage `db:"content"`
	}
	if err := db.GetContext(ctx, &activity, `SELECT activity_type,content
		FROM task_activities WHERE id=$1`, publication.ID); err != nil {
		t.Fatalf("load summary-only activity: %v", err)
	}
	if activity.ActivityType != "agent.conclusion.published" ||
		stringsContainAny(string(activity.Content), publication.Summary, source.Turn.ID.String(), source.Conversation.ID.String()) {
		t.Fatalf("summary-only activity leaked private data: type=%s content=%s",
			activity.ActivityType, activity.Content)
	}
	var descriptionActivityCount int
	if err := db.GetContext(ctx, &descriptionActivityCount, `SELECT COUNT(*) FROM task_activities
		WHERE task_id=$1 AND activity_type='task.updated'
		  AND content->>'conclusion_publication_id'=$2`, ids.taskID, publication.ID.String()); err != nil || descriptionActivityCount != 0 {
		t.Fatalf("summary-only writeback changed description activity count=%d err=%v",
			descriptionActivityCount, err)
	}
}

func TestAgentConclusionDescriptionConflictIsAtomic(t *testing.T) {
	db := openAgentTurnTestDB(t)
	ctx := context.Background()
	ids := seedAgentTurnTestScope(t, ctx, db)
	repo := NewAgentTurnRepository(db)
	source := createSucceededTurn(t, ctx, repo, ids, "description-conflict", agentdom.RuntimeRetired)
	baseDescription := json.RawMessage(`[{"type":"paragraph","props":{"revision":9007199254740992}}]`)
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET description=$1,updated_at=NOW() WHERE id=$2`, baseDescription, ids.taskID); err != nil {
		t.Fatalf("write baseline description: %v", err)
	}
	preparation, _, err := repo.PrepareConclusion(ctx, agentdom.PrepareConclusionInput{
		ID: uuid.New(), ProjectID: ids.projectID, SourceTurnID: source.Turn.ID,
		TargetTaskID: ids.taskID, PreparedByUserID: ids.userID, PreparedByMemberID: ids.memberID,
		Kind: agentdom.ConclusionPublished, UpdateDescription: true,
		IdempotencyKey: "description-conflict-prepare", ExpiresAt: time.Now().UTC().Add(30 * time.Minute),
	})
	if err != nil {
		t.Fatalf("prepare description proposal: %v", err)
	}
	concurrentDescription := json.RawMessage(`[{"type":"paragraph","props":{"revision":9007199254740993}}]`)
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET description=$1,updated_at=NOW() WHERE id=$2`, concurrentDescription, ids.taskID); err != nil {
		t.Fatalf("write concurrent description: %v", err)
	}
	_, _, err = repo.ConfirmConclusion(ctx, agentdom.ConfirmConclusionInput{
		PreparationID: preparation.ID, ProjectID: ids.projectID,
		PublishedByUserID: ids.userID, PublishedByMemberID: ids.memberID,
		ExpectedVersion: preparation.SummaryVersion, ExpectedSHA256: preparation.SummarySHA256,
		IdempotencyKey: "description-conflict-confirm",
	})
	if !errors.Is(err, agentdom.ErrConclusionConflict) {
		t.Fatalf("concurrent description did not fence confirmation: %v", err)
	}
	var publicationCount, activityCount int
	if err := db.GetContext(ctx, &publicationCount, `SELECT COUNT(*) FROM agent_conclusion_publications WHERE preparation_id=$1`, preparation.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.GetContext(ctx, &activityCount, `SELECT COUNT(*) FROM task_activities
		WHERE task_id=$1 AND activity_type LIKE 'agent.conclusion.%'`, ids.taskID); err != nil {
		t.Fatal(err)
	}
	var persisted json.RawMessage
	if err := db.GetContext(ctx, &persisted, `SELECT description FROM tasks WHERE id=$1`, ids.taskID); err != nil {
		t.Fatal(err)
	}
	if publicationCount != 0 || activityCount != 0 || !strings.Contains(string(persisted), "9007199254740993") {
		t.Fatalf("conflicted confirm was not atomic: publications=%d activities=%d description=%s",
			publicationCount, activityCount, persisted)
	}
}

func TestAgentTurnRepositoryFencesExpiredRunAttempt(t *testing.T) {
	db := openAgentTurnTestDB(t)
	ctx := context.Background()
	ids := seedAgentTurnTestScope(t, ctx, db)
	repo := NewAgentTurnRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)

	bundle, _, err := repo.CreateSessionTurn(ctx, newSessionTurnInput(t, ids, now, "fence-request", "recover me"))
	if err != nil {
		t.Fatalf("create session turn: %v", err)
	}
	oldClaim, err := repo.ClaimTurnRun(ctx, agentdom.ClaimTurnRunInput{
		TurnID: bundle.Turn.ID, WorkerID: "old-worker", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("claim first attempt: %v", err)
	}
	if _, err := repo.AppendTurnEvent(ctx, agentdom.AppendTurnEventInput{
		ID: uuid.New(), TurnID: bundle.Turn.ID, RunID: oldClaim.Bundle.Run.ID,
		ClaimToken: oldClaim.ClaimToken, TurnSequence: 0,
		EventType: "agent.turn.progress", EventSource: "agent",
		Payload: []byte(`{"step":"started"}`), CreatedAt: now,
	}); err != nil {
		t.Fatalf("append first-attempt progress: %v", err)
	}
	var leaseBefore time.Time
	if err := db.GetContext(ctx, &leaseBefore,
		`SELECT lease_expires_at FROM agent_turn_runs WHERE id=$1`, oldClaim.Bundle.Run.ID); err != nil {
		t.Fatalf("load first-attempt lease: %v", err)
	}
	if _, err := repo.ClaimTurnRun(ctx, agentdom.ClaimTurnRunInput{
		TurnID: bundle.Turn.ID, WorkerID: "old-worker", LeaseDuration: 2 * time.Minute,
	}); !errors.Is(err, agentdom.ErrTurnBusy) {
		t.Fatalf("same worker replayed an attempt with durable events: %v", err)
	}
	var leaseAfter time.Time
	if err := db.GetContext(ctx, &leaseAfter,
		`SELECT lease_expires_at FROM agent_turn_runs WHERE id=$1`, oldClaim.Bundle.Run.ID); err != nil {
		t.Fatalf("reload first-attempt lease: %v", err)
	}
	if !leaseAfter.Equal(leaseBefore) {
		t.Fatalf("busy replay changed lease: before=%s after=%s", leaseBefore, leaseAfter)
	}
	for _, forbiddenStatus := range []agentdom.TurnStatus{agentdom.TurnStatusStopped, agentdom.TurnStatusCancelled} {
		if _, err := repo.FinalizeTurn(ctx, agentdom.FinalizeTurnInput{
			RunID: oldClaim.Bundle.Run.ID, ClaimToken: oldClaim.ClaimToken,
			TerminalStatus: forbiddenStatus, GeneratedByAgentID: ids.agentID,
			Disposition: agentdom.RuntimeRetired,
		}); !errors.Is(err, agentdom.ErrTurnEventInvalid) {
			t.Fatalf("runtime forged %s finalization: %v", forbiddenStatus, err)
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE agent_turn_runs
		SET lease_expires_at=NOW()-INTERVAL '1 second' WHERE id=$1`, oldClaim.Bundle.Run.ID); err != nil {
		t.Fatalf("expire first attempt lease: %v", err)
	}
	if _, err := repo.FinalizeTurn(ctx, agentdom.FinalizeTurnInput{
		RunID: oldClaim.Bundle.Run.ID, ClaimToken: oldClaim.ClaimToken,
		TerminalStatus: agentdom.TurnStatusTimedOut, GeneratedByAgentID: ids.agentID,
		Disposition: agentdom.RuntimeRetired,
	}); !errors.Is(err, agentdom.ErrTurnClaimLost) {
		t.Fatalf("expired attempt forged timed_out finalization: %v", err)
	}

	newClaim, err := repo.ClaimTurnRun(ctx, agentdom.ClaimTurnRunInput{
		TurnID: bundle.Turn.ID, WorkerID: "new-worker", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("claim recovered attempt: %v", err)
	}
	if newClaim.Bundle.Run.ID == oldClaim.Bundle.Run.ID || newClaim.Bundle.Run.Attempt != 2 ||
		len(newClaim.Bundle.Runs) != 2 || newClaim.Bundle.Runs[0].Attempt != 1 || newClaim.Bundle.Runs[1].Attempt != 2 {
		t.Fatalf("expected a distinct attempt 2, got old=%s new=%+v", oldClaim.Bundle.Run.ID, newClaim.Bundle.Run)
	}
	if _, err := repo.AppendTurnEvent(ctx, agentdom.AppendTurnEventInput{
		ID: uuid.New(), TurnID: bundle.Turn.ID, RunID: oldClaim.Bundle.Run.ID,
		ClaimToken: oldClaim.ClaimToken, TurnSequence: 0,
		EventType: agentdom.StableOutputEventType, EventSource: "agent",
		Payload: []byte(`{"text":"stale answer"}`), CreatedAt: now,
	}); err == nil {
		t.Fatal("old attempt appended an event after recovery")
	}
	oldEvent := uuid.New()
	if _, err := repo.FinalizeTurn(ctx, agentdom.FinalizeTurnInput{
		RunID: oldClaim.Bundle.Run.ID, ClaimToken: oldClaim.ClaimToken,
		TerminalStatus: agentdom.TurnStatusSucceeded, StableOutputEvent: &oldEvent,
		GeneratedByAgentID: ids.agentID, Disposition: agentdom.RuntimeRetired,
		FinalEventSequence: intPointer(0),
	}); !errors.Is(err, agentdom.ErrTurnClaimLost) {
		t.Fatalf("old attempt finalization error = %v, want claim lost", err)
	}

	eventID := uuid.New()
	if _, err := repo.AppendTurnEvent(ctx, agentdom.AppendTurnEventInput{
		ID: eventID, TurnID: bundle.Turn.ID, RunID: newClaim.Bundle.Run.ID,
		ClaimToken: newClaim.ClaimToken, TurnSequence: 0,
		EventType: agentdom.StableOutputEventType, EventSource: "agent",
		Payload: []byte(`{"text":"recovered answer"}`), CreatedAt: now,
	}); err != nil {
		t.Fatalf("append recovered stable event: %v", err)
	}
	events, _, err := repo.ListOwnerTurnEvents(ctx, agentdom.TurnEventListFilter{
		ProjectID: ids.projectID, TurnID: bundle.Turn.ID, MemberID: ids.memberID, Limit: 20,
	})
	if err != nil || len(events) != 2 ||
		events[0].TurnRunAttempt == nil || *events[0].TurnRunAttempt != 1 ||
		events[1].TurnRunAttempt == nil || *events[1].TurnRunAttempt != 2 {
		t.Fatalf("recovered event attempt audit: events=%+v err=%v", events, err)
	}
	if _, err := repo.FinalizeTurn(ctx, agentdom.FinalizeTurnInput{
		RunID: newClaim.Bundle.Run.ID, ClaimToken: newClaim.ClaimToken,
		TerminalStatus: agentdom.TurnStatusSucceeded, StableOutputEvent: &eventID,
		GeneratedByAgentID: ids.agentID, Disposition: agentdom.RuntimeRetired,
		FinalEventSequence: intPointer(0),
	}); err != nil {
		t.Fatalf("finalize recovered attempt: %v", err)
	}
}

func TestAgentTurnRepositoryRejectsTaskBoundPrivateConversation(t *testing.T) {
	db := openAgentTurnTestDB(t)
	ctx := context.Background()
	ids := seedAgentTurnTestScope(t, ctx, db)
	input := newSessionTurnInput(t, ids, time.Now().UTC(), "private-task-request", "private question")
	input.Conversation.TaskID = &ids.taskID

	if _, _, err := NewAgentTurnRepository(db).CreateSessionTurn(ctx, input); err == nil {
		t.Fatal("private chat conversation with task_id was accepted")
	}
	var count int
	if err := db.GetContext(ctx, &count, `SELECT COUNT(*) FROM agent_chat_sessions WHERE id=$1`, input.Session.ID); err != nil || count != 0 {
		t.Fatalf("rejected private chat left partial state: count=%d err=%v", count, err)
	}
}

func TestAgentTurnRepositoryOwnerStopIsAtomicAndFenced(t *testing.T) {
	db := openAgentTurnTestDB(t)
	ctx := context.Background()
	ids := seedAgentTurnTestScope(t, ctx, db)
	repo := NewAgentTurnRepository(db)
	now := time.Now().UTC()

	queued, _, err := repo.CreateSessionTurn(ctx, newSessionTurnInput(t, ids, now, "cancel-queued", "wait"))
	if err != nil {
		t.Fatal(err)
	}
	queuedResult, err := repo.StopOwnerTurn(ctx, agentdom.StopOwnerTurnInput{
		ProjectID: ids.projectID, SessionID: queued.Session.ID, TurnID: queued.Turn.ID,
		MemberID: ids.memberID, UserID: ids.userID, LegacyRole: "USER",
	})
	if err != nil || queuedResult.TerminalStatus != agentdom.TurnStatusCancelled {
		t.Fatalf("cancel queued turn: result=%+v err=%v", queuedResult, err)
	}
	replayed, err := repo.StopOwnerTurn(ctx, agentdom.StopOwnerTurnInput{
		ProjectID: ids.projectID, SessionID: queued.Session.ID, TurnID: queued.Turn.ID,
		MemberID: ids.memberID, UserID: ids.userID, LegacyRole: "USER",
	})
	if err != nil || replayed.RunID != queuedResult.RunID || replayed.TerminalStatus != agentdom.TurnStatusCancelled {
		t.Fatalf("replay queued cancellation: result=%+v err=%v", replayed, err)
	}

	running, _, err := repo.CreateSessionTurn(ctx, newSessionTurnInput(t, ids, now.Add(time.Second), "stop-running", "run"))
	if err != nil {
		t.Fatal(err)
	}
	claim, err := repo.ClaimTurnRun(ctx, agentdom.ClaimTurnRunInput{
		TurnID: running.Turn.ID, WorkerID: "stop-test-worker", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	runningResult, err := repo.StopOwnerTurn(ctx, agentdom.StopOwnerTurnInput{
		ProjectID: ids.projectID, SessionID: running.Session.ID, TurnID: running.Turn.ID,
		MemberID: ids.memberID, UserID: ids.userID, LegacyRole: "USER",
	})
	if err != nil || runningResult.TerminalStatus != agentdom.TurnStatusStopped {
		t.Fatalf("stop running turn: result=%+v err=%v", runningResult, err)
	}
	if _, err := repo.RenewTurnRunLease(ctx, agentdom.RenewTurnRunLeaseInput{
		RunID: claim.Bundle.Run.ID, ClaimToken: claim.ClaimToken, LeaseDuration: time.Minute,
	}); !errors.Is(err, agentdom.ErrTurnClaimLost) {
		t.Fatalf("stopped turn renewed stale claim: %v", err)
	}
	if _, err := repo.AppendTurnEvent(ctx, agentdom.AppendTurnEventInput{
		ID: uuid.New(), TurnID: running.Turn.ID, RunID: claim.Bundle.Run.ID,
		ClaimToken: claim.ClaimToken, TurnSequence: 0, EventType: "message_chunk",
		EventSource: "agent", Payload: []byte(`{"text":"late"}`), CreatedAt: now,
	}); !errors.Is(err, agentdom.ErrTurnClaimLost) {
		t.Fatalf("stopped turn accepted late event: %v", err)
	}
	assertTerminalTurn(t, ctx, db, running.Turn.ID, "stopped")
}

func TestAgentOutboxClaimFencing(t *testing.T) {
	db := openAgentTurnTestDB(t)
	ctx := context.Background()
	repo := NewAgentTurnRepository(db)
	if _, err := db.ExecContext(ctx, `DELETE FROM agent_outbox_events WHERE event_type='test.event'`); err != nil {
		t.Fatalf("clear prior test outbox events: %v", err)
	}
	eventID := uuid.New()
	if _, err := db.ExecContext(ctx, `INSERT INTO agent_outbox_events
		(id,aggregate_type,aggregate_id,event_type,payload,idempotency_key,available_at,created_at)
		VALUES ($1,'test',$2,'test.event','{}',$3,'2000-01-01','2000-01-01')`,
		eventID, uuid.New(), "outbox-fence-"+eventID.String()); err != nil {
		t.Fatalf("insert outbox event: %v", err)
	}

	first, err := repo.ClaimOutbox(ctx, "worker-one", 1, time.Minute)
	if err != nil || len(first) != 1 || first[0].ID != eventID || first[0].LockToken == nil {
		t.Fatalf("first outbox claim: events=%+v err=%v", first, err)
	}
	firstToken := *first[0].LockToken
	if _, err := db.ExecContext(ctx, `UPDATE agent_outbox_events
		SET lock_expires_at=NOW()-INTERVAL '1 second' WHERE id=$1`, eventID); err != nil {
		t.Fatalf("expire outbox claim: %v", err)
	}
	second, err := repo.ClaimOutbox(ctx, "worker-two", 1, time.Minute)
	if err != nil || len(second) != 1 || second[0].ID != eventID || second[0].LockToken == nil {
		t.Fatalf("second outbox claim: events=%+v err=%v", second, err)
	}
	if *second[0].LockToken == firstToken {
		t.Fatal("outbox reclaim reused the old fencing token")
	}
	if err := repo.MarkOutboxPublished(ctx, eventID, firstToken, time.Now().UTC()); err == nil {
		t.Fatal("stale outbox claim published the reclaimed event")
	}
	if err := repo.MarkOutboxPublished(ctx, eventID, *second[0].LockToken, time.Now().UTC()); err != nil {
		t.Fatalf("current outbox claim publish: %v", err)
	}
}

func TestAgentTurnRepositoryExpiresDueTurns(t *testing.T) {
	db := openAgentTurnTestDB(t)
	ctx := context.Background()
	ids := seedAgentTurnTestScope(t, ctx, db)
	repo := NewAgentTurnRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)

	queuedInput := newSessionTurnInput(t, ids, now, "queued-deadline", "queued timeout")
	queuedDeadline := now.Add(-time.Second)
	queuedInput.Turn.DeadlineAt = &queuedDeadline
	queued, _, err := repo.CreateSessionTurn(ctx, queuedInput)
	if err != nil {
		t.Fatalf("create queued deadline turn: %v", err)
	}
	if _, err := repo.ClaimTurnRun(ctx, agentdom.ClaimTurnRunInput{
		TurnID: queued.Turn.ID, WorkerID: "late-worker", LeaseDuration: time.Minute,
	}); !errors.Is(err, agentdom.ErrTurnDeadlineExceeded) {
		t.Fatalf("claim expired queued turn error = %v", err)
	}
	assertTimedOutTurn(t, ctx, db, queued.Turn.ID)
	if _, _, err := repo.AppendSessionTurn(ctx, newAppendTurnInput(
		t, ids, queued.Session.ID, now.Add(time.Second), "after-queued-timeout", "next turn",
	)); err != nil {
		t.Fatalf("append after queued timeout: %v", err)
	}

	runningInput := newSessionTurnInput(t, ids, now, "running-deadline", "running timeout")
	runningDeadline := time.Now().UTC().Add(250 * time.Millisecond)
	runningInput.Turn.DeadlineAt = &runningDeadline
	running, _, err := repo.CreateSessionTurn(ctx, runningInput)
	if err != nil {
		t.Fatalf("create running deadline turn: %v", err)
	}
	claim, err := repo.ClaimTurnRun(ctx, agentdom.ClaimTurnRunInput{
		TurnID: running.Turn.ID, WorkerID: "lost-worker", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("claim running deadline turn: %v", err)
	}
	if claim.Bundle.Run.LeaseExpiresAt == nil || claim.Bundle.Run.LeaseExpiresAt.After(runningDeadline.Add(time.Millisecond)) {
		t.Fatalf("claim lease %v exceeded turn deadline %v", claim.Bundle.Run.LeaseExpiresAt, runningDeadline)
	}
	time.Sleep(300 * time.Millisecond)
	if _, err := repo.AppendTurnEvent(ctx, agentdom.AppendTurnEventInput{
		ID: uuid.New(), TurnID: running.Turn.ID, RunID: claim.Bundle.Run.ID,
		ClaimToken: claim.ClaimToken, TurnSequence: 0,
		EventType: agentdom.StableOutputEventType, EventSource: "agent",
		Payload: []byte(`{"text":"too late"}`), CreatedAt: time.Now().UTC(),
	}); err == nil {
		t.Fatal("event was accepted after the turn deadline")
	}
	if _, err := db.ExecContext(ctx, `UPDATE project_members SET deleted_at=NOW()
		WHERE id=$1 OR agent_id=$2`, ids.memberID, ids.agentID); err != nil {
		t.Fatalf("revoke turn participants before expiry: %v", err)
	}
	if _, err := repo.RenewTurnRunLease(ctx, agentdom.RenewTurnRunLeaseInput{
		RunID: claim.Bundle.Run.ID, ClaimToken: claim.ClaimToken, LeaseDuration: time.Minute,
	}); !errors.Is(err, agentdom.ErrTurnDeadlineExceeded) {
		t.Fatalf("deadline+revocation renew error = %v, want deadline exceeded", err)
	}
	if _, err := repo.ExpireDueTurns(ctx, 100); err != nil {
		t.Fatalf("expire due turns after participant revocation: %v", err)
	}
	assertTimedOutTurn(t, ctx, db, running.Turn.ID)

	revokedIDs := seedAgentTurnTestScope(t, ctx, db)
	revokedInput := newSessionTurnInput(t, revokedIDs, now, "revoked-queued-deadline", "queued timeout")
	revokedDeadline := now.Add(-time.Second)
	revokedInput.Turn.DeadlineAt = &revokedDeadline
	revoked, _, err := repo.CreateSessionTurn(ctx, revokedInput)
	if err != nil {
		t.Fatalf("create revoked queued deadline turn: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE project_members SET deleted_at=NOW()
		WHERE id=$1 OR agent_id=$2`, revokedIDs.memberID, revokedIDs.agentID); err != nil {
		t.Fatalf("revoke queued deadline participants: %v", err)
	}
	if _, err := repo.ClaimTurnRun(ctx, agentdom.ClaimTurnRunInput{
		TurnID: revoked.Turn.ID, WorkerID: "late-revoked-worker", LeaseDuration: time.Minute,
	}); !errors.Is(err, agentdom.ErrTurnDeadlineExceeded) {
		t.Fatalf("deadline+revocation claim error = %v, want deadline exceeded", err)
	}
	assertTimedOutTurn(t, ctx, db, revoked.Turn.ID)
}

func TestAgentTurnExecutionAuthorizationLocksAgentMembershipAgainstConcurrentRevocation(t *testing.T) {
	db := openAgentTurnTestDB(t)
	ctx := context.Background()
	ids := seedAgentTurnTestScope(t, ctx, db)
	repo := NewAgentTurnRepository(db)
	bundle, _, err := repo.CreateSessionTurn(ctx, newSessionTurnInput(
		t, ids, time.Now().UTC(), "agent-auth-lock", "lock agent authorization"))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var turn agentTurnRecord
	if err := tx.GetContext(ctx, &turn, `SELECT `+turnColumns+` FROM agent_turns WHERE id=$1`, bundle.Turn.ID); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := authorizeTurnExecutionTx(ctx, tx, turn); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	revoked := make(chan error, 1)
	go func() {
		_, updateErr := db.ExecContext(ctx, `UPDATE project_members SET deleted_at=NOW() WHERE agent_id=$1`, ids.agentID)
		revoked <- updateErr
	}()
	select {
	case err := <-revoked:
		_ = tx.Rollback()
		t.Fatalf("agent membership revocation bypassed authorization lock: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-revoked; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimTurnRun(ctx, agentdom.ClaimTurnRunInput{
		TurnID: bundle.Turn.ID, WorkerID: "revoked-agent-worker", LeaseDuration: time.Minute,
	}); !errors.Is(err, agentdom.ErrTurnAuthorizationRevoked) {
		t.Fatalf("claim after serialized agent revocation = %v", err)
	}
}

func TestAgentTurnExecutionAuthorizationUsesAgentThenMembershipLockOrder(t *testing.T) {
	db := openAgentTurnTestDB(t)
	ctx := context.Background()
	ids := seedAgentTurnTestScope(t, ctx, db)
	repo := NewAgentTurnRepository(db)
	bundle, _, err := repo.CreateSessionTurn(ctx, newSessionTurnInput(
		t, ids, time.Now().UTC(), "agent-auth-lock-order", "serialize agent deletion"))
	if err != nil {
		t.Fatal(err)
	}

	deleteTx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deleteTx.ExecContext(ctx, `UPDATE agents SET deleted_at=NOW() WHERE id=$1`, ids.agentID); err != nil {
		_ = deleteTx.Rollback()
		t.Fatal(err)
	}

	claimed := make(chan error, 1)
	go func() {
		_, claimErr := repo.ClaimTurnRun(ctx, agentdom.ClaimTurnRunInput{
			TurnID: bundle.Turn.ID, WorkerID: "delete-interleaving-worker", LeaseDuration: time.Minute,
		})
		claimed <- claimErr
	}()
	select {
	case err := <-claimed:
		_ = deleteTx.Rollback()
		t.Fatalf("claim did not wait for the already-locked agent row: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	// The claim is waiting on the agent row and therefore cannot hold the
	// membership row. Deletion can take the second lock without deadlocking.
	if _, err := deleteTx.ExecContext(ctx, `UPDATE project_members SET deleted_at=NOW()
		WHERE project_id=$1 AND agent_id=$2 AND member_type='agent'`, ids.projectID, ids.agentID); err != nil {
		_ = deleteTx.Rollback()
		t.Fatalf("delete membership after locking agent: %v", err)
	}
	if err := deleteTx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-claimed; !errors.Is(err, agentdom.ErrTurnAuthorizationRevoked) {
		t.Fatalf("claim serialized after agent deletion = %v, want authorization revoked", err)
	}
}

func TestAgentTurnRepositoryReusableConversationContract(t *testing.T) {
	db := openAgentTurnTestDB(t)
	ctx := context.Background()
	ids := seedAgentTurnTestScope(t, ctx, db)
	repo := NewAgentTurnRepository(db)
	first := createSucceededTurn(t, ctx, repo, ids, "reusable-first", agentdom.RuntimeReusable)

	identityMismatch := newAppendTurnInput(t, ids, first.Session.ID, time.Now().UTC(), "reuse-identity-mismatch", "continue")
	identityMismatch.ReuseConversation = true
	identityMismatch.Conversation = *first.Conversation
	identityMismatch.Conversation.ID = uuid.New()
	identityMismatch.Turn.ConversationID = first.Conversation.ID
	identityMismatch.Run.ConversationID = first.Conversation.ID
	if _, _, err := repo.AppendSessionTurn(ctx, identityMismatch); err == nil {
		t.Fatal("conversation/turn identity mismatch was accepted")
	}

	backendMismatch := newAppendTurnInput(t, ids, first.Session.ID, time.Now().UTC(), "reuse-backend-mismatch", "continue")
	backendMismatch.ReuseConversation = true
	backendMismatch.Conversation = *first.Conversation
	backendMismatch.Turn.ConversationID = first.Conversation.ID
	backendMismatch.Run.ConversationID = first.Conversation.ID
	backendMismatch.Run.Backend = agentdom.TurnBackendACP
	if _, _, err := repo.AppendSessionTurn(ctx, backendMismatch); !errors.Is(err, agentdom.ErrConversationNotFound) {
		t.Fatalf("backend mismatch error = %v", err)
	}

	reuse := newAppendTurnInput(t, ids, first.Session.ID, time.Now().UTC(), "reuse-success", "continue")
	reuse.ReuseConversation = true
	reuse.Conversation = *first.Conversation
	reuse.Turn.ConversationID = first.Conversation.ID
	reuse.Run.ConversationID = first.Conversation.ID
	appended, replayed, err := repo.AppendSessionTurn(ctx, reuse)
	if err != nil || replayed || appended.Conversation.ID != first.Conversation.ID || appended.Turn.TurnIndex != 2 {
		t.Fatalf("reuse conversation: bundle=%+v replayed=%v err=%v", appended, replayed, err)
	}
}

func TestAgentConclusionIdempotencyAndLeafConcurrency(t *testing.T) {
	db := openAgentTurnTestDB(t)
	ctx := context.Background()
	ids := seedAgentTurnTestScope(t, ctx, db)
	repo := NewAgentTurnRepository(db)
	source := createSucceededTurn(t, ctx, repo, ids, "conclusion-source", agentdom.RuntimeRetired)

	prepareInput := agentdom.PrepareConclusionInput{
		ID: uuid.New(), ProjectID: ids.projectID, SourceTurnID: source.Turn.ID,
		TargetTaskID: ids.taskID, PreparedByUserID: ids.userID, PreparedByMemberID: ids.memberID,
		Kind: agentdom.ConclusionPublished, IdempotencyKey: "prepare-concurrent",
		ExpiresAt: time.Now().UTC().Add(30 * time.Minute).Truncate(time.Microsecond),
	}
	type prepareOutcome struct {
		preparation *agentdom.ConclusionPreparation
		replayed    bool
		err         error
	}
	prepareStart := make(chan struct{})
	prepareResults := make(chan prepareOutcome, 2)
	for range 2 {
		go func() {
			<-prepareStart
			preparation, replayed, err := repo.PrepareConclusion(ctx, prepareInput)
			prepareResults <- prepareOutcome{preparation, replayed, err}
		}()
	}
	close(prepareStart)
	var rootPreparation *agentdom.ConclusionPreparation
	prepareReplays := 0
	for range 2 {
		outcome := <-prepareResults
		if outcome.err != nil {
			t.Fatalf("concurrent prepare: %v", outcome.err)
		}
		if rootPreparation == nil {
			rootPreparation = outcome.preparation
		} else if outcome.preparation.ID != rootPreparation.ID {
			t.Fatalf("concurrent prepare produced different IDs: %s %s", rootPreparation.ID, outcome.preparation.ID)
		}
		if outcome.replayed {
			prepareReplays++
		}
	}
	if prepareReplays != 1 {
		t.Fatalf("concurrent prepare replay count = %d", prepareReplays)
	}

	confirmInput := agentdom.ConfirmConclusionInput{
		PreparationID: rootPreparation.ID, ProjectID: ids.projectID,
		PublishedByUserID: ids.userID, PublishedByMemberID: ids.memberID,
		ExpectedVersion: rootPreparation.SummaryVersion, ExpectedSHA256: rootPreparation.SummarySHA256,
		IdempotencyKey: "confirm-concurrent",
	}
	type confirmOutcome struct {
		publication *agentdom.ConclusionPublication
		replayed    bool
		err         error
	}
	confirmStart := make(chan struct{})
	confirmResults := make(chan confirmOutcome, 2)
	for range 2 {
		go func() {
			<-confirmStart
			publication, replayed, err := repo.ConfirmConclusion(ctx, confirmInput)
			confirmResults <- confirmOutcome{publication, replayed, err}
		}()
	}
	close(confirmStart)
	var rootPublication *agentdom.ConclusionPublication
	confirmReplays := 0
	for range 2 {
		outcome := <-confirmResults
		if outcome.err != nil {
			t.Fatalf("concurrent confirm: %v", outcome.err)
		}
		if rootPublication == nil {
			rootPublication = outcome.publication
		} else if outcome.publication.ID != rootPublication.ID {
			t.Fatalf("concurrent confirm produced different IDs: %s %s", rootPublication.ID, outcome.publication.ID)
		}
		if outcome.replayed {
			confirmReplays++
		}
	}
	if confirmReplays != 1 {
		t.Fatalf("concurrent confirm replay count = %d", confirmReplays)
	}

	type preparedChild struct {
		input       agentdom.PrepareConclusionInput
		preparation *agentdom.ConclusionPreparation
	}
	prepareChild := func(kind agentdom.ConclusionKind, key string) preparedChild {
		t.Helper()
		input := agentdom.PrepareConclusionInput{
			ID: uuid.New(), ProjectID: ids.projectID, SourceTurnID: source.Turn.ID,
			TargetTaskID: ids.taskID, PreparedByUserID: ids.userID, PreparedByMemberID: ids.memberID,
			Kind: kind, RelatedPublicationID: &rootPublication.ID, IdempotencyKey: key,
			ExpiresAt: time.Now().UTC().Add(30 * time.Minute).Truncate(time.Microsecond),
		}
		preparation, _, err := repo.PrepareConclusion(ctx, input)
		if err != nil {
			t.Fatalf("prepare %s: %v", kind, err)
		}
		return preparedChild{input: input, preparation: preparation}
	}
	revision := prepareChild(agentdom.ConclusionRevised, "prepare-revision")
	withdrawal := prepareChild(agentdom.ConclusionWithdrawn, "prepare-withdrawal")
	children := []preparedChild{revision, withdrawal}
	leafStart := make(chan struct{})
	leafResults := make(chan error, 2)
	for index, child := range children {
		index, child := index, child
		go func() {
			<-leafStart
			_, _, err := repo.ConfirmConclusion(ctx, agentdom.ConfirmConclusionInput{
				PreparationID: child.preparation.ID, ProjectID: ids.projectID,
				PublishedByUserID: ids.userID, PublishedByMemberID: ids.memberID,
				ExpectedVersion: child.preparation.SummaryVersion, ExpectedSHA256: child.preparation.SummarySHA256,
				IdempotencyKey: "confirm-child-" + string(rune('a'+index)),
			})
			leafResults <- err
		}()
	}
	close(leafStart)
	leafSuccesses, leafConflicts := 0, 0
	for range 2 {
		err := <-leafResults
		switch {
		case err == nil:
			leafSuccesses++
		case errors.Is(err, agentdom.ErrConclusionConflict):
			leafConflicts++
		default:
			t.Fatalf("unexpected leaf confirmation error: %v", err)
		}
	}
	if leafSuccesses != 1 || leafConflicts != 1 {
		t.Fatalf("leaf race successes=%d conflicts=%d", leafSuccesses, leafConflicts)
	}
	for _, child := range children {
		replayedPreparation, replayed, err := repo.PrepareConclusion(ctx, child.input)
		if err != nil || !replayed || replayedPreparation.ID != child.preparation.ID {
			t.Fatalf("replay prepared child after parent superseded: preparation=%+v replayed=%v err=%v",
				replayedPreparation, replayed, err)
		}
	}
	publicationPage, hasMore, err := repo.ListTaskConclusionPublications(ctx, agentdom.ConclusionPublicationListFilter{
		ProjectID: ids.projectID, TaskID: ids.taskID, ViewerMemberID: ids.memberID, Limit: 1,
	})
	if err != nil || !hasMore || len(publicationPage) != 1 {
		t.Fatalf("first publication page: publications=%+v hasMore=%v err=%v", publicationPage, hasMore, err)
	}
	publicationCursorTime := publicationPage[0].Publication.CreatedAt
	publicationCursorID := publicationPage[0].Publication.ID
	nextPublicationPage, _, err := repo.ListTaskConclusionPublications(ctx, agentdom.ConclusionPublicationListFilter{
		ProjectID: ids.projectID, TaskID: ids.taskID, ViewerMemberID: ids.memberID, Limit: 10,
		CursorTime: &publicationCursorTime, CursorID: &publicationCursorID,
	})
	if err != nil || len(nextPublicationPage) != 1 || nextPublicationPage[0].Publication.ID == publicationCursorID {
		t.Fatalf("publication pages overlapped: publications=%+v err=%v", nextPublicationPage, err)
	}
}

func TestAgentTurnEventPaginationHasStableNonOverlappingCursor(t *testing.T) {
	db := openAgentTurnTestDB(t)
	ctx := context.Background()
	ids := seedAgentTurnTestScope(t, ctx, db)
	repo := NewAgentTurnRepository(db)
	bundle, _, err := repo.CreateSessionTurn(ctx, newSessionTurnInput(
		t, ids, time.Now().UTC(), "event-page", "paginate events",
	))
	if err != nil {
		t.Fatal(err)
	}
	claim, err := repo.ClaimTurnRun(ctx, agentdom.ClaimTurnRunInput{
		TurnID: bundle.Turn.ID, WorkerID: "event-page-worker", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	for sequence, eventType := range []string{"agent.turn.progress", agentdom.StableOutputEventType} {
		payload := json.RawMessage(`{"step":"working"}`)
		if sequence == 1 {
			payload = json.RawMessage(`{"text":"stable page answer"}`)
		}
		if _, err := repo.AppendTurnEvent(ctx, agentdom.AppendTurnEventInput{
			ID: uuid.New(), TurnID: bundle.Turn.ID, RunID: bundle.Run.ID,
			ClaimToken: claim.ClaimToken, TurnSequence: sequence,
			EventType: eventType, EventSource: "agent", Payload: payload, CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("append event %d: %v", sequence, err)
		}
	}
	page, hasMore, err := repo.ListOwnerTurnEvents(ctx, agentdom.TurnEventListFilter{
		ProjectID: ids.projectID, TurnID: bundle.Turn.ID, MemberID: ids.memberID, Limit: 1,
	})
	if err != nil || !hasMore || len(page) != 1 || page[0].TurnSequence == nil {
		t.Fatalf("first event page: events=%+v hasMore=%v err=%v", page, hasMore, err)
	}
	cursor := &agentdom.TurnEventCursor{
		EventIndex: page[0].EventIndex, ID: page[0].ID,
	}
	next, hasMore, err := repo.ListOwnerTurnEvents(ctx, agentdom.TurnEventListFilter{
		ProjectID: ids.projectID, TurnID: bundle.Turn.ID, MemberID: ids.memberID,
		Limit: 1, Cursor: cursor,
	})
	if err != nil || hasMore || len(next) != 1 || next[0].ID == page[0].ID {
		t.Fatalf("second event page overlapped: events=%+v hasMore=%v err=%v", next, hasMore, err)
	}
}

func TestAgentConclusionSummaryVersionsAreSerializedAcrossKeys(t *testing.T) {
	db := openAgentTurnTestDB(t)
	ctx := context.Background()
	ids := seedAgentTurnTestScope(t, ctx, db)
	repo := NewAgentTurnRepository(db)
	source := createSucceededTurn(t, ctx, repo, ids, "version-source", agentdom.RuntimeRetired)

	start := make(chan struct{})
	results := make(chan *agentdom.ConclusionPreparation, 2)
	errs := make(chan error, 2)
	for index := range 2 {
		index := index
		go func() {
			<-start
			preparation, _, err := repo.PrepareConclusion(ctx, agentdom.PrepareConclusionInput{
				ID: uuid.New(), ProjectID: ids.projectID, SourceTurnID: source.Turn.ID,
				TargetTaskID: ids.taskID, PreparedByUserID: ids.userID, PreparedByMemberID: ids.memberID,
				Kind: agentdom.ConclusionPublished, IdempotencyKey: fmt.Sprintf("version-key-%d", index),
				ExpiresAt: time.Now().UTC().Add(30 * time.Minute).Truncate(time.Microsecond),
			})
			if err != nil {
				errs <- err
				return
			}
			results <- preparation
		}()
	}
	close(start)
	versions := map[int]struct{}{}
	for range 2 {
		select {
		case err := <-errs:
			t.Fatalf("concurrent version allocation: %v", err)
		case preparation := <-results:
			versions[preparation.SummaryVersion] = struct{}{}
		}
	}
	if len(versions) != 2 {
		t.Fatalf("concurrent preparations reused a summary version: %v", versions)
	}
}

func TestAgentProjectChatTransactionsRecheckRevokedPermissions(t *testing.T) {
	db := openAgentTurnTestDB(t)
	ctx := context.Background()
	ids := seedAgentTurnTestScope(t, ctx, db)
	repo := NewAgentTurnRepository(db)
	var roleID, globalRoleID uuid.UUID
	if err := db.GetContext(ctx, &roleID, `SELECT project_role_id FROM project_members WHERE id=$1`, ids.memberID); err != nil {
		t.Fatal(err)
	}
	if err := db.GetContext(ctx, &globalRoleID, `SELECT role_id FROM users WHERE id=$1`, ids.userID); err != nil {
		t.Fatal(err)
	}
	setPermissions := func(value string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `UPDATE global_roles SET permissions='{}'::jsonb WHERE id=$1`, globalRoleID); err != nil {
			t.Fatalf("clear global permissions: %v", err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE project_roles SET permissions=$1::jsonb WHERE id=$2`, value, roleID); err != nil {
			t.Fatalf("set project permissions: %v", err)
		}
	}

	input := newSessionTurnInput(t, ids, time.Now().UTC(), "snapshot-revoked", "private question")
	source := agentdom.SessionContextSource{
		ID: uuid.New(), SessionID: input.Session.ID, ProjectID: ids.projectID,
		SourceType: agentdom.ContextSourceTask, SourceID: ids.taskID,
		Ordinal: 0, SelectedByMemberID: ids.memberID, CreatedAt: time.Now().UTC(),
	}
	items, err := repo.ResolveContextItems(ctx, ids.projectID, ids.memberID, input.Snapshot.ID, []agentdom.SessionContextSource{source})
	if err != nil {
		t.Fatal(err)
	}
	input.SelectedSources = []agentdom.SessionContextSource{source}
	input.Snapshot.Items = items
	setPermissions(`{"agents.read":true}`)
	if _, _, err := repo.CreateSessionTurn(ctx, input); !errors.Is(err, agentdom.ErrProjectChatForbidden) {
		t.Fatalf("snapshot transaction ignored revoked tasks.read: %v", err)
	}

	setPermissions(`{"*":true}`)
	succeeded := createSucceededTurn(t, ctx, repo, ids, "authorization-source", agentdom.RuntimeRetired)
	if _, err := repo.ReplaceSessionContextSources(ctx, ids.projectID, succeeded.Session.ID,
		ids.memberID, ids.userID, "", []agentdom.SessionContextSource{{
			SourceType: agentdom.ContextSourceTask, SourceID: ids.taskID,
		}}); err != nil {
		t.Fatalf("select source before revocation: %v", err)
	}
	setPermissions(`{"agents.read":true}`)
	if _, err := repo.ReplaceSessionContextSources(ctx, ids.projectID, succeeded.Session.ID,
		ids.memberID, ids.userID, "", []agentdom.SessionContextSource{{
			SourceType: agentdom.ContextSourceTask, SourceID: ids.taskID,
		}}); !errors.Is(err, agentdom.ErrProjectChatForbidden) {
		t.Fatalf("selection transaction ignored revoked tasks.read: %v", err)
	}

	setPermissions(`{"*":true}`)
	preparation, _, err := repo.PrepareConclusion(ctx, agentdom.PrepareConclusionInput{
		ID: uuid.New(), ProjectID: ids.projectID, SourceTurnID: succeeded.Turn.ID,
		TargetTaskID: ids.taskID, PreparedByUserID: ids.userID, PreparedByMemberID: ids.memberID,
		Kind: agentdom.ConclusionPublished, IdempotencyKey: "authorization-prepare",
		ExpiresAt: time.Now().UTC().Add(30 * time.Minute),
	})
	if err != nil {
		t.Fatalf("prepare before revocation: %v", err)
	}
	setPermissions(`{"agents.read":true,"tasks.read":true}`)
	if _, _, err := repo.ConfirmConclusion(ctx, agentdom.ConfirmConclusionInput{
		PreparationID: preparation.ID, ProjectID: ids.projectID,
		PublishedByUserID: ids.userID, PublishedByMemberID: ids.memberID,
		ExpectedVersion: preparation.SummaryVersion, ExpectedSHA256: preparation.SummarySHA256,
		IdempotencyKey: "authorization-confirm",
	}); !errors.Is(err, agentdom.ErrProjectChatForbidden) {
		t.Fatalf("confirm transaction ignored revoked tasks.write: %v", err)
	}
}

func TestAgentTurnExecutionRechecksAuthorizationAtClaimAndRenew(t *testing.T) {
	db := openAgentTurnTestDB(t)
	ctx := context.Background()

	t.Run("claim terminalizes queued turn after human access is revoked", func(t *testing.T) {
		ids := seedAgentTurnTestScope(t, ctx, db)
		repo := NewAgentTurnRepository(db)
		bundle, _, err := repo.CreateSessionTurn(ctx, newSessionTurnInput(
			t, ids, time.Now().UTC(), "claim-revoked", "private snapshot",
		))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE project_members SET deleted_at=NOW() WHERE id=$1`, ids.memberID); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.ClaimTurnRun(ctx, agentdom.ClaimTurnRunInput{
			TurnID: bundle.Turn.ID, WorkerID: "revoked-claim", LeaseDuration: time.Minute,
		}); !errors.Is(err, agentdom.ErrTurnAuthorizationRevoked) {
			t.Fatalf("claim after revocation = %v", err)
		}
		assertTerminalTurn(t, ctx, db, bundle.Turn.ID, string(agentdom.TurnStatusCancelled))
		var controls int
		if err := db.GetContext(ctx, &controls, `SELECT COUNT(*) FROM agent_outbox_events
			WHERE aggregate_id=$1 AND event_type='agent.turn.control.requested'`, bundle.Turn.ID); err != nil {
			t.Fatal(err)
		}
		if controls != 0 {
			t.Fatalf("queued revoked turn emitted %d stop controls", controls)
		}
	})

	t.Run("renew terminalizes running turn and emits fenced control after agent removal", func(t *testing.T) {
		ids := seedAgentTurnTestScope(t, ctx, db)
		repo := NewAgentTurnRepository(db)
		bundle, _, err := repo.CreateSessionTurn(ctx, newSessionTurnInput(
			t, ids, time.Now().UTC(), "renew-revoked", "private snapshot",
		))
		if err != nil {
			t.Fatal(err)
		}
		claim, err := repo.ClaimTurnRun(ctx, agentdom.ClaimTurnRunInput{
			TurnID: bundle.Turn.ID, WorkerID: "revoked-renew", LeaseDuration: time.Minute,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE project_members SET deleted_at=NOW()
			WHERE project_id=$1 AND agent_id=$2`, ids.projectID, ids.agentID); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.RenewTurnRunLease(ctx, agentdom.RenewTurnRunLeaseInput{
			RunID: claim.Bundle.Run.ID, ClaimToken: claim.ClaimToken, LeaseDuration: time.Minute,
		}); !errors.Is(err, agentdom.ErrTurnAuthorizationRevoked) {
			t.Fatalf("renew after revocation = %v", err)
		}
		assertTerminalTurn(t, ctx, db, bundle.Turn.ID, string(agentdom.TurnStatusStopped))
		var controls int
		if err := db.GetContext(ctx, &controls, `SELECT COUNT(*) FROM agent_outbox_events
			WHERE aggregate_id=$1 AND event_type='agent.turn.control.requested'`, bundle.Turn.ID); err != nil {
			t.Fatal(err)
		}
		if controls != 1 {
			t.Fatalf("running revoked turn emitted %d stop controls, want 1", controls)
		}
	})
}

func TestAgentContextPermissionsDependOnSourceType(t *testing.T) {
	db := openAgentTurnTestDB(t)
	ctx := context.Background()
	ids := seedAgentTurnTestScope(t, ctx, db)
	repo := NewAgentTurnRepository(db)
	now := time.Now().UTC()
	source, _, err := repo.CreateSessionTurn(ctx, newSessionTurnInput(t, ids, now, "source-permission", "source"))
	if err != nil {
		t.Fatal(err)
	}
	target, _, err := repo.CreateSessionTurn(ctx, newSessionTurnInput(t, ids, now.Add(time.Millisecond), "target-permission", "target"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE global_roles role SET permissions='{}'
		FROM users u WHERE u.role_id=role.id AND u.id=$1`, ids.userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE project_roles role SET permissions='{"agents.read":true}'
		FROM project_members member
		WHERE member.project_role_id=role.id AND member.id=$1`, ids.memberID); err != nil {
		t.Fatal(err)
	}
	sources := []agentdom.SessionContextSource{
		{SourceType: agentdom.ContextSourceSession, SourceID: source.Session.ID},
		{SourceType: agentdom.ContextSourceRun, SourceID: source.Run.ID},
	}
	selected, err := repo.ReplaceSessionContextSources(ctx, ids.projectID, target.Session.ID,
		ids.memberID, ids.userID, "USER", sources)
	if err != nil || len(selected) != 2 {
		t.Fatalf("owner session/run sources required unrelated tasks.read: selected=%+v err=%v", selected, err)
	}
	_, err = repo.ReplaceSessionContextSources(ctx, ids.projectID, target.Session.ID,
		ids.memberID, ids.userID, "USER", []agentdom.SessionContextSource{{
			SourceType: agentdom.ContextSourceTask, SourceID: ids.taskID,
		}})
	if !errors.Is(err, agentdom.ErrProjectChatForbidden) {
		t.Fatalf("task source without tasks.read error=%v", err)
	}
}

func TestAgentTurnFinalizeReplayRejectsDifferentError(t *testing.T) {
	db := openAgentTurnTestDB(t)
	ctx := context.Background()
	ids := seedAgentTurnTestScope(t, ctx, db)
	repo := NewAgentTurnRepository(db)
	bundle, _, err := repo.CreateSessionTurn(ctx, newSessionTurnInput(
		t, ids, time.Now().UTC(), "failed-replay", "fail",
	))
	if err != nil {
		t.Fatal(err)
	}
	claim, err := repo.ClaimTurnRun(ctx, agentdom.ClaimTurnRunInput{
		TurnID: bundle.Turn.ID, WorkerID: "failure-worker", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	code, message := "provider_error", "first failure"
	input := agentdom.FinalizeTurnInput{
		RunID: claim.Bundle.Run.ID, ClaimToken: claim.ClaimToken,
		TerminalStatus: agentdom.TurnStatusFailed, GeneratedByAgentID: ids.agentID,
		ErrorCode: &code, ErrorMessage: &message, Disposition: agentdom.RuntimeRetired,
	}
	if _, err := repo.FinalizeTurn(ctx, input); err != nil {
		t.Fatalf("finalize failure: %v", err)
	}
	different := "different failure"
	input.ErrorMessage = &different
	if _, err := repo.FinalizeTurn(ctx, input); !errors.Is(err, agentdom.ErrIdempotencyConflict) {
		t.Fatalf("different failure replay error = %v", err)
	}
}

func assertTimedOutTurn(t *testing.T, ctx context.Context, db *sqlx.DB, turnID uuid.UUID) {
	t.Helper()
	var row struct {
		TurnStatus   string `db:"turn_status"`
		RunStatus    string `db:"run_status"`
		ResultStatus string `db:"result_status"`
		OutboxCount  int    `db:"outbox_count"`
	}
	if err := db.GetContext(ctx, &row, `SELECT turn.status AS turn_status,run.status AS run_status,
		result.terminal_status AS result_status,
		(SELECT COUNT(*) FROM agent_outbox_events outbox
		 WHERE outbox.aggregate_id=turn.id AND outbox.event_type='agent.turn.finished') AS outbox_count
		FROM agent_turns turn
		JOIN agent_turn_runs run ON run.turn_id=turn.id
		JOIN agent_turn_results result ON result.turn_id=turn.id
		WHERE turn.id=$1 ORDER BY run.attempt DESC LIMIT 1`, turnID); err != nil {
		t.Fatalf("load timed out turn: %v", err)
	}
	if row.TurnStatus != "timed_out" || row.RunStatus != "timed_out" ||
		row.ResultStatus != "timed_out" || row.OutboxCount != 1 {
		t.Fatalf("incomplete timeout terminalization: %+v", row)
	}
}

func assertTerminalTurn(t *testing.T, ctx context.Context, db *sqlx.DB, turnID uuid.UUID, status string) {
	t.Helper()
	var row struct {
		TurnStatus   string `db:"turn_status"`
		RunStatus    string `db:"run_status"`
		ResultStatus string `db:"result_status"`
		OutboxCount  int    `db:"outbox_count"`
	}
	if err := db.GetContext(ctx, &row, `SELECT turn.status AS turn_status,run.status AS run_status,
		result.terminal_status AS result_status,
		(SELECT COUNT(*) FROM agent_outbox_events outbox
		 WHERE outbox.aggregate_id=turn.id AND outbox.event_type='agent.turn.finished') AS outbox_count
		FROM agent_turns turn
		JOIN agent_turn_runs run ON run.turn_id=turn.id
		JOIN agent_turn_results result ON result.turn_id=turn.id
		WHERE turn.id=$1 ORDER BY run.attempt DESC LIMIT 1`, turnID); err != nil {
		t.Fatalf("load terminal turn: %v", err)
	}
	if row.TurnStatus != status || row.RunStatus != status || row.ResultStatus != status || row.OutboxCount != 1 {
		t.Fatalf("incomplete %s terminalization: %+v", status, row)
	}
}

func openAgentTurnTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	dsn := os.Getenv("PACA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PACA_TEST_DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", dsn)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

type agentTurnTestIDs struct {
	projectID uuid.UUID
	userID    uuid.UUID
	memberID  uuid.UUID
	agentID   uuid.UUID
	taskID    uuid.UUID
}

func seedAgentTurnTestScope(t *testing.T, ctx context.Context, db *sqlx.DB) agentTurnTestIDs {
	t.Helper()
	ids := agentTurnTestIDs{
		projectID: uuid.New(), userID: uuid.New(), memberID: uuid.New(),
		agentID: uuid.New(), taskID: uuid.New(),
	}
	roleID := uuid.New()
	projectRoleID := uuid.New()
	agentMemberID := uuid.New()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO global_roles (id,name,permissions) VALUES ($1,$2,'{"*":true}')`, []any{roleID, "turn-test-" + roleID.String()}},
		{`INSERT INTO users (id,username,password_hash,full_name,role_id) VALUES ($1,$2,'x','Turn Test',$3)`, []any{ids.userID, "turn-test-" + ids.userID.String(), roleID}},
		{`INSERT INTO projects (id,name,task_id_prefix,created_by) VALUES ($1,'Turn Test','TURN',$2)`, []any{ids.projectID, ids.userID}},
		{`INSERT INTO project_roles (id,project_id,role_name,permissions) VALUES ($1,$2,'owner','{"*":true}')`, []any{projectRoleID, ids.projectID}},
		{`INSERT INTO project_members (id,project_id,user_id,project_role_id,member_type) VALUES ($1,$2,$3,$4,'human')`, []any{ids.memberID, ids.projectID, ids.userID, projectRoleID}},
		{`INSERT INTO agents (id,project_id,name,handle,llm_provider,llm_model,llm_api_key_secret,llm_base_url,created_by) VALUES ($1,$2,'Turn Agent',$3,'openai','test','secret','',$4)`, []any{ids.agentID, ids.projectID, "turn-agent-" + ids.agentID.String(), ids.userID}},
		{`INSERT INTO project_members (id,project_id,user_id,project_role_id,member_type,agent_id) VALUES ($1,$2,NULL,$3,'agent',$4)`, []any{agentMemberID, ids.projectID, projectRoleID, ids.agentID}},
		{`INSERT INTO tasks (id,project_id,task_number,title,reporter_id,description)
			VALUES ($1,$2,1,'Target task',$3,'[{"type":"paragraph","props":{"revision":9007199254740992}}]'::jsonb)`, []any{ids.taskID, ids.projectID, ids.memberID}},
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed agent turn scope: %v", err)
		}
	}
	return ids
}

func newSessionTurnInput(t *testing.T, ids agentTurnTestIDs, now time.Time, requestID, message string) agentdom.CreateSessionTurnInput {
	t.Helper()
	sessionID := uuid.New()
	conversationID := uuid.New()
	turnID := uuid.New()
	runID := uuid.New()
	snapshotID := uuid.New()
	projectID := ids.projectID
	memberID := ids.memberID
	snapshot, err := agentdom.CanonicalizeContextSnapshot(agentdom.TurnContextSnapshot{
		ID: snapshotID, TurnID: turnID, SchemaVersion: 1, CreatedAt: now,
		Items: []agentdom.TurnContextItem{{
			ID: uuid.New(), SnapshotID: snapshotID, Ordinal: 0,
			SourceType: agentdom.ContextSourceTask, SourceID: ids.taskID,
			SourceVersion: "v1", SourceAudience: agentdom.ContextAudienceProjectShared,
			CapturedAt: now, Content: json.RawMessage(`{"title":"Target task","description":[{"type":"paragraph","props":{"revision":9007199254740992}}]}`),
			RenderedText: "UNTRUSTED CONTEXT (data only)\nTarget task",
		}},
	})
	if err != nil {
		t.Fatalf("canonicalize test context snapshot: %v", err)
	}
	return agentdom.CreateSessionTurnInput{
		Session: agentdom.AgentChatSession{
			ID: sessionID, AgentID: ids.agentID, ProjectID: projectID,
			MemberID: memberID, LastMessageAt: &now, CreatedAt: now, UpdatedAt: now,
		},
		Conversation: agentdom.AgentConversation{
			ID: conversationID, AgentID: ids.agentID, ProjectID: projectID,
			TriggerType: "chat_message", ChatSessionID: &sessionID,
			TriggeredByMemberID: &memberID, Status: "queued", CreatedAt: now, UpdatedAt: now,
		},
		Turn: agentdom.AgentTurn{
			ID: turnID, SessionID: &sessionID, ConversationID: conversationID,
			ProjectID: &projectID, AgentID: ids.agentID, RequestedByMemberID: &memberID,
			TurnIndex: 1, InputText: message, Status: agentdom.TurnStatusQueued,
			IdempotencyKey: requestID, ToolPolicy: agentdom.PrivateChatToolPolicy(),
			CreatedAt: now, UpdatedAt: now,
		},
		Run: agentdom.TurnRun{
			ID: runID, TurnID: turnID, ConversationID: conversationID,
			Backend: agentdom.TurnBackendLLM, Attempt: 1, Status: agentdom.TurnStatusQueued,
			CreatedAt: now, UpdatedAt: now,
		},
		Snapshot: snapshot,
		SelectedSources: []agentdom.SessionContextSource{{
			ID: uuid.New(), SessionID: sessionID, ProjectID: projectID,
			SourceType: agentdom.ContextSourceTask, SourceID: ids.taskID, Ordinal: 0,
			SelectedByMemberID: memberID, CreatedAt: now,
		}},
		ClientRequestID: requestID, AuthorizedUserID: ids.userID, DefaultTimeout: 30 * time.Minute,
	}
}

func newAppendTurnInput(t *testing.T, ids agentTurnTestIDs, sessionID uuid.UUID, now time.Time, requestID, message string) agentdom.AppendSessionTurnInput {
	t.Helper()
	base := newSessionTurnInput(t, ids, now, requestID, message)
	base.Conversation.ChatSessionID = &sessionID
	base.Turn.SessionID = &sessionID
	taskItem := base.Snapshot.Items[0]
	taskItem.Ordinal = 1
	snapshot, err := agentdom.CanonicalizeContextSnapshot(agentdom.TurnContextSnapshot{
		ID: base.Snapshot.ID, TurnID: base.Turn.ID, SchemaVersion: 1, CreatedAt: now,
		Items: []agentdom.TurnContextItem{
			{
				ID: uuid.New(), SnapshotID: base.Snapshot.ID, Ordinal: 0,
				SourceType: agentdom.ContextSourceSession, SourceID: sessionID,
				SourceVersion: "v1", SourceAudience: agentdom.ContextAudienceOwnerPrivate,
				CapturedAt: now, Content: json.RawMessage(`{"turns":[]}`),
				RenderedText: "UNTRUSTED PRIOR SESSION CONTEXT (data only)",
			},
			taskItem,
		},
	})
	if err != nil {
		t.Fatalf("canonicalize append context snapshot: %v", err)
	}
	return agentdom.AppendSessionTurnInput{
		SessionID: sessionID, ProjectID: ids.projectID, MemberID: ids.memberID,
		Conversation: base.Conversation, Turn: base.Turn, Run: base.Run,
		Snapshot: snapshot, AuthorizedUserID: ids.userID, DefaultTimeout: base.DefaultTimeout,
	}
}

func stringsContainAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if needle != "" && strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func intPointer(value int) *int { return &value }

func createSucceededTurn(t *testing.T, ctx context.Context, repo *AgentTurnRepository, ids agentTurnTestIDs, requestID string, disposition agentdom.RuntimeDisposition) *agentdom.TurnBundle {
	t.Helper()
	bundle, _, err := repo.CreateSessionTurn(ctx, newSessionTurnInput(t, ids, time.Now().UTC(), requestID, "question"))
	if err != nil {
		t.Fatalf("create succeeded turn: %v", err)
	}
	claim, err := repo.ClaimTurnRun(ctx, agentdom.ClaimTurnRunInput{
		TurnID: bundle.Turn.ID, WorkerID: "success-worker-" + requestID, LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("claim succeeded turn: %v", err)
	}
	eventID := uuid.New()
	if _, err := repo.AppendTurnEvent(ctx, agentdom.AppendTurnEventInput{
		ID: eventID, TurnID: bundle.Turn.ID, RunID: claim.Bundle.Run.ID,
		ClaimToken: claim.ClaimToken, TurnSequence: 0,
		EventType: agentdom.StableOutputEventType, EventSource: "agent",
		Payload: []byte(`{"text":"stable conclusion"}`), CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("append succeeded turn event: %v", err)
	}
	if _, err := repo.FinalizeTurn(ctx, agentdom.FinalizeTurnInput{
		RunID: claim.Bundle.Run.ID, ClaimToken: claim.ClaimToken,
		TerminalStatus: agentdom.TurnStatusSucceeded, StableOutputEvent: &eventID,
		GeneratedByAgentID: ids.agentID, Disposition: disposition,
		FinalEventSequence: intPointer(0),
	}); err != nil {
		t.Fatalf("finalize succeeded turn: %v", err)
	}
	return bundle
}
