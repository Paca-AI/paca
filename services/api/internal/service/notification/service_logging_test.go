package notificationsvc

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
)

// captureLogs routes the default slog logger into a buffer for the test. Not
// safe with t.Parallel, which these tests do not use.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

type failingPublisher struct{}

func (failingPublisher) Append(context.Context, string, string, any) error {
	return errors.New("valkey down")
}
func (failingPublisher) Publish(context.Context, string, any) error {
	return errors.New("valkey down")
}

func TestNotifyMentioned_CreateFailureIsLogged(t *testing.T) {
	noRetryDelay(t)
	logs := captureLogs(t)
	failingRepo := &errCreateRepo{newFakeNotificationRepo()}
	members := newFakeMemberRepo()
	svc := New(failingRepo, members, nil)

	projectID, actorAccountID, actorMemberID := uuid.New(), uuid.New(), uuid.New()
	userA := uuid.New()
	members.add(&projectdom.ProjectMember{ID: actorMemberID, ProjectID: projectID, UserID: actorAccountID, Username: "commenter"})
	members.add(&projectdom.ProjectMember{ID: uuid.New(), ProjectID: projectID, UserID: userA, Username: "alice"})

	if err := svc.NotifyMentioned(context.Background(), notificationdom.NotifyMentionedInput{
		TaskID: uuid.New(), ProjectID: projectID, CommentText: "@alice hi",
		ActorMemberID: actorMemberID, ActorID: actorAccountID,
	}); err == nil {
		t.Fatal("expected the failed insert to be returned")
	}
	if !strings.Contains(logs.String(), "mention notification not created") {
		t.Fatalf("expected a warning for the lost mention, got logs: %q", logs.String())
	}
}

func TestNotifyAssigned_PublishFailuresAreLoggedNotReturned(t *testing.T) {
	logs := captureLogs(t)
	repo := newFakeNotificationRepo()
	members := newFakeMemberRepo()
	svc := New(repo, members, failingPublisher{}).WithEventPublishing(newFakeUserRepo(), "https://paca.test")

	projectID, actorAccountID, assigneeAccountID, assigneeMemberID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	members.add(&projectdom.ProjectMember{ID: assigneeMemberID, ProjectID: projectID, UserID: assigneeAccountID, AccountID: assigneeAccountID, Username: "bob"})

	if err := svc.NotifyAssigned(context.Background(), notificationdom.NotifyAssignedInput{
		TaskID: uuid.New(), ProjectID: projectID, NewAssigneeMemberID: assigneeMemberID, ActorID: actorAccountID,
	}); err != nil {
		t.Fatalf("a publish failure must not fail the notification, got %v", err)
	}
	if countNotifications(repo, assigneeAccountID) != 1 {
		t.Fatal("the in-app notification must still be stored")
	}
	if !strings.Contains(logs.String(), "notification realtime publish failed") {
		t.Fatalf("expected a realtime publish warning, got logs: %q", logs.String())
	}
}

// noRetryDelay removes the backoff between insert retries so a test that
// forces failures doesn't sleep.
func noRetryDelay(t *testing.T) {
	t.Helper()
	prev := createRetryDelays
	createRetryDelays = []time.Duration{0, 0}
	t.Cleanup(func() { createRetryDelays = prev })
}

// flakyCreateRepo fails the first `failures` inserts, then stores normally.
type flakyCreateRepo struct {
	*fakeNotificationRepo
	failures int
	attempts int
}

func (r *flakyCreateRepo) Create(ctx context.Context, n *notificationdom.Notification) error {
	r.attempts++
	if r.attempts <= r.failures {
		return errors.New("connection reset")
	}
	return r.fakeNotificationRepo.Create(ctx, n)
}

func mentionFixture(repo notificationdom.Repository) (*Svc, notificationdom.NotifyMentionedInput, uuid.UUID) {
	members := newFakeMemberRepo()
	svc := New(repo, members, nil)
	projectID, actorAccountID, actorMemberID, userA := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	members.add(&projectdom.ProjectMember{ID: actorMemberID, ProjectID: projectID, UserID: actorAccountID, Username: "commenter"})
	members.add(&projectdom.ProjectMember{ID: uuid.New(), ProjectID: projectID, UserID: userA, Username: "alice"})
	return svc, notificationdom.NotifyMentionedInput{
		TaskID: uuid.New(), ProjectID: projectID, CommentText: "@alice hi",
		ActorMemberID: actorMemberID, ActorID: actorAccountID,
	}, userA
}

func TestNotifyMentioned_RetriesTransientCreateFailure(t *testing.T) {
	noRetryDelay(t)
	base := newFakeNotificationRepo()
	repo := &flakyCreateRepo{fakeNotificationRepo: base, failures: 2}
	svc, in, alice := mentionFixture(repo)

	if err := svc.NotifyMentioned(context.Background(), in); err != nil {
		t.Fatalf("a transient failure must be retried to success, got %v", err)
	}
	if countNotifications(base, alice) != 1 {
		t.Fatal("the mention must be stored after the retries")
	}
	if repo.attempts != 3 {
		t.Fatalf("expected 3 insert attempts, got %d", repo.attempts)
	}
}

func TestNotifyMentioned_GivesUpAfterRetries(t *testing.T) {
	noRetryDelay(t)
	repo := &flakyCreateRepo{fakeNotificationRepo: newFakeNotificationRepo(), failures: 100}
	svc, in, _ := mentionFixture(repo)

	if err := svc.NotifyMentioned(context.Background(), in); err == nil {
		t.Fatal("a persistent failure must be returned")
	}
	if repo.attempts != len(createRetryDelays)+1 {
		t.Fatalf("expected %d attempts, got %d", len(createRetryDelays)+1, repo.attempts)
	}
}

// switchPublisher succeeds or fails on Publish as configured.
type switchPublisher struct{ publishErr error }

func (p switchPublisher) Append(context.Context, string, string, any) error { return nil }
func (p switchPublisher) Publish(context.Context, string, any) error        { return p.publishErr }

func assignOne(t *testing.T, repo *fakeNotificationRepo, pub eventPublisher) {
	t.Helper()
	members := newFakeMemberRepo()
	svc := New(repo, members, pub)
	projectID, actor, assignee, assigneeMember := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	members.add(&projectdom.ProjectMember{ID: assigneeMember, ProjectID: projectID, UserID: assignee, AccountID: assignee, Username: "bob"})
	if err := svc.NotifyAssigned(context.Background(), notificationdom.NotifyAssignedInput{
		TaskID: uuid.New(), ProjectID: projectID, NewAssigneeMemberID: assigneeMember, ActorID: actor,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPublishCreated_MarksRealtimeDeliverySentOnSuccess(t *testing.T) {
	repo := newFakeNotificationRepo()
	assignOne(t, repo, switchPublisher{})
	if len(repo.delivered) != 1 {
		t.Fatalf("a successful publish must mark the delivery sent, got %d marks", len(repo.delivered))
	}
}

func TestPublishCreated_LeavesDeliveryPendingWhenPublishFails(t *testing.T) {
	repo := newFakeNotificationRepo()
	assignOne(t, repo, switchPublisher{publishErr: errors.New("valkey down")})
	if len(repo.delivered) != 0 {
		t.Fatal("a failed publish must leave the delivery pending for the dispatcher")
	}
}
