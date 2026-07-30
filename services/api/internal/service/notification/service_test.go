package notificationsvc

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
)

type fakeNotificationRepo struct {
	mu   sync.RWMutex
	data map[uuid.UUID]*notificationdom.Notification
}

func newFakeNotificationRepo() *fakeNotificationRepo {
	return &fakeNotificationRepo{data: make(map[uuid.UUID]*notificationdom.Notification)}
}

func (r *fakeNotificationRepo) Create(_ context.Context, n *notificationdom.Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *n
	r.data[n.ID] = &cp
	return nil
}

func (r *fakeNotificationRepo) ListForUser(_ context.Context, userID uuid.UUID, _ int) ([]*notificationdom.Notification, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*notificationdom.Notification
	for _, n := range r.data {
		if n.RecipientUserID == userID {
			cp := *n
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakeNotificationRepo) UnreadCount(_ context.Context, userID uuid.UUID) (int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var count int64
	for _, n := range r.data {
		if n.RecipientUserID == userID && n.ReadAt == nil {
			count++
		}
	}
	return count, nil
}

func (r *fakeNotificationRepo) MarkAsRead(_ context.Context, id, userID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, ok := r.data[id]
	if !ok || n.RecipientUserID != userID {
		return notificationdom.ErrNotificationNotFound
	}
	now := time.Now()
	n.ReadAt = &now
	return nil
}

func (r *fakeNotificationRepo) MarkAllAsRead(_ context.Context, userID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, n := range r.data {
		if n.RecipientUserID == userID && n.ReadAt == nil {
			now := time.Now()
			n.ReadAt = &now
		}
	}
	return nil
}

type fakeMemberRepo struct {
	mu     sync.RWMutex
	byID   map[uuid.UUID]*projectdom.ProjectMember
	byProj map[uuid.UUID][]*projectdom.ProjectMember
	byUser map[[2]uuid.UUID]*projectdom.ProjectMember
}

type userProjectKey = [2]uuid.UUID

func newFakeMemberRepo() *fakeMemberRepo {
	return &fakeMemberRepo{
		byID:   make(map[uuid.UUID]*projectdom.ProjectMember),
		byProj: make(map[uuid.UUID][]*projectdom.ProjectMember),
		byUser: make(map[[2]uuid.UUID]*projectdom.ProjectMember),
	}
}

func (r *fakeMemberRepo) add(m *projectdom.ProjectMember) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *m
	r.byID[m.ID] = &cp
	r.byProj[m.ProjectID] = append(r.byProj[m.ProjectID], &cp)
	r.byUser[userProjectKey{m.UserID, m.ProjectID}] = &cp
}

func (r *fakeMemberRepo) FindMemberByID(_ context.Context, id uuid.UUID) (*projectdom.ProjectMember, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.byID[id]
	if !ok {
		return nil, fmt.Errorf("member not found: %s", id)
	}
	cp := *m
	return &cp, nil
}

func (r *fakeMemberRepo) FindMemberByUserProject(_ context.Context, userID, projectID uuid.UUID) (*projectdom.ProjectMember, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.byUser[userProjectKey{userID, projectID}]
	if !ok {
		return nil, fmt.Errorf("member not found for user %s project %s", userID, projectID)
	}
	cp := *m
	return &cp, nil
}

func (r *fakeMemberRepo) ListMembers(_ context.Context, projectID uuid.UUID) ([]*projectdom.ProjectMember, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byProj[projectID], nil
}

type errCreateRepo struct {
	*fakeNotificationRepo
}

func (r *errCreateRepo) Create(_ context.Context, _ *notificationdom.Notification) error {
	return fmt.Errorf("db write failed")
}

func countNotifications(repo *fakeNotificationRepo, recipientID uuid.UUID) int {
	repo.mu.RLock()
	defer repo.mu.RUnlock()
	var count int
	for _, n := range repo.data {
		if n.RecipientUserID == recipientID {
			count++
		}
	}
	return count
}

func findNotification(repo *fakeNotificationRepo, recipientID uuid.UUID, nType notificationdom.NotificationType) *notificationdom.Notification {
	repo.mu.RLock()
	defer repo.mu.RUnlock()
	for _, n := range repo.data {
		if n.RecipientUserID == recipientID && n.Type == nType {
			return n
		}
	}
	return nil
}

func TestNotifyAssigned_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeNotificationRepo()
	members := newFakeMemberRepo()
	svc := New(repo, members, nil)

	projectID := uuid.New()
	actorUserID := uuid.New()
	assigneeUserID := uuid.New()
	actorMemberID := uuid.New()
	assigneeMemberID := uuid.New()

	members.add(&projectdom.ProjectMember{
		ID: actorMemberID, ProjectID: projectID, UserID: actorUserID, Username: "actor",
	})
	members.add(&projectdom.ProjectMember{
		ID: assigneeMemberID, ProjectID: projectID, UserID: assigneeUserID, Username: "assignee",
	})

	taskID := uuid.New()
	err := svc.NotifyAssigned(ctx, notificationdom.NotifyAssignedInput{
		TaskID:              taskID,
		ProjectID:           projectID,
		NewAssigneeMemberID: assigneeMemberID,
		ActorUserID:         actorUserID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := countNotifications(repo, assigneeUserID); got != 1 {
		t.Fatalf("expected 1 notification for assignee, got %d", got)
	}

	n := findNotification(repo, assigneeUserID, notificationdom.NotificationTypeAssigned)
	if n == nil {
		t.Fatal("expected to find assigned notification")
	}
	if n.RecipientUserID != assigneeUserID {
		t.Errorf("expected RecipientUserID=%s, got %s", assigneeUserID, n.RecipientUserID)
	}
	if n.ActorMemberID == nil || *n.ActorMemberID != actorMemberID {
		t.Errorf("expected ActorMemberID=%s, got %v", actorMemberID, n.ActorMemberID)
	}
	if n.TaskID == nil || *n.TaskID != taskID {
		t.Errorf("expected TaskID=%s, got %v", taskID, n.TaskID)
	}
	if n.ProjectID != projectID {
		t.Errorf("expected ProjectID=%s, got %s", projectID, n.ProjectID)
	}
}

func TestNotifyAssigned_SelfAssignmentSuppressed(t *testing.T) {
	ctx := context.Background()
	repo := newFakeNotificationRepo()
	members := newFakeMemberRepo()
	svc := New(repo, members, nil)

	projectID := uuid.New()
	userID := uuid.New()
	memberID := uuid.New()

	members.add(&projectdom.ProjectMember{
		ID: memberID, ProjectID: projectID, UserID: userID, Username: "self-assigner",
	})

	err := svc.NotifyAssigned(ctx, notificationdom.NotifyAssignedInput{
		TaskID:              uuid.New(),
		ProjectID:           projectID,
		NewAssigneeMemberID: memberID,
		ActorUserID:         userID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := countNotifications(repo, userID); got != 0 {
		t.Errorf("self-assignment should produce 0 notifications, got %d", got)
	}
}

func TestNotifyAssigned_MemberNotFoundSkipsSilently(t *testing.T) {
	ctx := context.Background()
	repo := newFakeNotificationRepo()
	members := newFakeMemberRepo()
	svc := New(repo, members, nil)

	err := svc.NotifyAssigned(ctx, notificationdom.NotifyAssignedInput{
		TaskID:              uuid.New(),
		ProjectID:           uuid.New(),
		NewAssigneeMemberID: uuid.New(),
		ActorUserID:         uuid.New(),
	})
	if err != nil {
		t.Fatalf("expected nil error when member not found, got %v", err)
	}
	if len(repo.data) != 0 {
		t.Errorf("expected 0 notifications, got %d", len(repo.data))
	}
}

func TestNotifyAssigned_ActorNotInProjectStillNotifies(t *testing.T) {
	ctx := context.Background()
	repo := newFakeNotificationRepo()
	members := newFakeMemberRepo()
	svc := New(repo, members, nil)

	projectID := uuid.New()
	assigneeUserID := uuid.New()
	assigneeMemberID := uuid.New()
	actorUserID := uuid.New()

	members.add(&projectdom.ProjectMember{
		ID: assigneeMemberID, ProjectID: projectID, UserID: assigneeUserID, Username: "assignee",
	})

	err := svc.NotifyAssigned(ctx, notificationdom.NotifyAssignedInput{
		TaskID:              uuid.New(),
		ProjectID:           projectID,
		NewAssigneeMemberID: assigneeMemberID,
		ActorUserID:         actorUserID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	n := findNotification(repo, assigneeUserID, notificationdom.NotificationTypeAssigned)
	if n == nil {
		t.Fatal("expected notification to be created even when actor not in project")
	}
	if n.ActorMemberID != nil {
		t.Errorf("expected nil ActorMemberID when actor not in project, got %v", n.ActorMemberID)
	}
}

func TestNotifyAssigned_RepoError(t *testing.T) {
	ctx := context.Background()
	members := newFakeMemberRepo()
	svc := New(&errCreateRepo{newFakeNotificationRepo()}, members, nil)

	projectID := uuid.New()
	assigneeUserID := uuid.New()
	assigneeMemberID := uuid.New()
	actorUserID := uuid.New()

	members.add(&projectdom.ProjectMember{
		ID: assigneeMemberID, ProjectID: projectID, UserID: assigneeUserID, Username: "assignee",
	})

	err := svc.NotifyAssigned(ctx, notificationdom.NotifyAssignedInput{
		TaskID:              uuid.New(),
		ProjectID:           projectID,
		NewAssigneeMemberID: assigneeMemberID,
		ActorUserID:         actorUserID,
	})
	if err == nil {
		t.Fatal("expected error from repo.Create failure")
	}
}

func TestNotifyMentioned_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeNotificationRepo()
	members := newFakeMemberRepo()
	svc := New(repo, members, nil)

	projectID := uuid.New()
	taskID := uuid.New()
	actorUserID := uuid.New()
	actorMemberID := uuid.New()
	userA := uuid.New()
	memberA := uuid.New()
	userB := uuid.New()
	memberB := uuid.New()

	members.add(&projectdom.ProjectMember{ID: actorMemberID, ProjectID: projectID, UserID: actorUserID, Username: "commenter"})
	members.add(&projectdom.ProjectMember{ID: memberA, ProjectID: projectID, UserID: userA, Username: "alice"})
	members.add(&projectdom.ProjectMember{ID: memberB, ProjectID: projectID, UserID: userB, Username: "bob"})

	err := svc.NotifyMentioned(ctx, notificationdom.NotifyMentionedInput{
		TaskID:        taskID,
		ProjectID:     projectID,
		CommentText:   "Hey @alice and @bob check this out",
		ActorMemberID: actorMemberID,
		ActorUserID:   actorUserID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := countNotifications(repo, userA); got != 1 {
		t.Errorf("expected 1 notification for alice, got %d", got)
	}
	if got := countNotifications(repo, userB); got != 1 {
		t.Errorf("expected 1 notification for bob, got %d", got)
	}
	if got := countNotifications(repo, actorUserID); got != 0 {
		t.Errorf("expected 0 notifications for actor, got %d", got)
	}

	nA := findNotification(repo, userA, notificationdom.NotificationTypeMentioned)
	if nA == nil {
		t.Fatal("expected mentioned notification for alice")
	}
	if nA.ActorMemberID == nil || *nA.ActorMemberID != actorMemberID {
		t.Errorf("expected ActorMemberID=%s, got %v", actorMemberID, nA.ActorMemberID)
	}
	if nA.TaskID == nil || *nA.TaskID != taskID {
		t.Errorf("expected TaskID=%s, got %v", taskID, nA.TaskID)
	}
}

func TestNotifyMentioned_SelfMentionSuppressed(t *testing.T) {
	ctx := context.Background()
	repo := newFakeNotificationRepo()
	members := newFakeMemberRepo()
	svc := New(repo, members, nil)

	projectID := uuid.New()
	userID := uuid.New()
	memberID := uuid.New()

	members.add(&projectdom.ProjectMember{ID: memberID, ProjectID: projectID, UserID: userID, Username: "myself"})

	err := svc.NotifyMentioned(ctx, notificationdom.NotifyMentionedInput{
		TaskID:        uuid.New(),
		ProjectID:     projectID,
		CommentText:   "I am talking to @myself",
		ActorMemberID: memberID,
		ActorUserID:   userID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := countNotifications(repo, userID); got != 0 {
		t.Errorf("self-mention should produce 0 notifications, got %d", got)
	}
}

func TestNotifyMentioned_DuplicateMentionsDeduped(t *testing.T) {
	ctx := context.Background()
	repo := newFakeNotificationRepo()
	members := newFakeMemberRepo()
	svc := New(repo, members, nil)

	projectID := uuid.New()
	actorUserID := uuid.New()
	actorMemberID := uuid.New()
	userA := uuid.New()
	memberA := uuid.New()

	members.add(&projectdom.ProjectMember{ID: actorMemberID, ProjectID: projectID, UserID: actorUserID, Username: "commenter"})
	members.add(&projectdom.ProjectMember{ID: memberA, ProjectID: projectID, UserID: userA, Username: "alice"})

	err := svc.NotifyMentioned(ctx, notificationdom.NotifyMentionedInput{
		TaskID:        uuid.New(),
		ProjectID:     projectID,
		CommentText:   "@alice please @alice review this @alice again",
		ActorMemberID: actorMemberID,
		ActorUserID:   actorUserID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := countNotifications(repo, userA); got != 1 {
		t.Errorf("duplicate @mentions should dedupe to 1 notification, got %d", got)
	}
}

func TestNotifyMentioned_NoMentionsInText(t *testing.T) {
	ctx := context.Background()
	repo := newFakeNotificationRepo()
	members := newFakeMemberRepo()
	svc := New(repo, members, nil)

	err := svc.NotifyMentioned(ctx, notificationdom.NotifyMentionedInput{
		TaskID:        uuid.New(),
		ProjectID:     uuid.New(),
		CommentText:   "Just a regular comment with no mentions",
		ActorMemberID: uuid.New(),
		ActorUserID:   uuid.New(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.data) != 0 {
		t.Errorf("expected 0 notifications, got %d", len(repo.data))
	}
}

func TestNotifyMentioned_NonMemberMentionIgnored(t *testing.T) {
	ctx := context.Background()
	repo := newFakeNotificationRepo()
	members := newFakeMemberRepo()
	svc := New(repo, members, nil)

	projectID := uuid.New()
	actorUserID := uuid.New()
	actorMemberID := uuid.New()

	members.add(&projectdom.ProjectMember{ID: actorMemberID, ProjectID: projectID, UserID: actorUserID, Username: "commenter"})

	err := svc.NotifyMentioned(ctx, notificationdom.NotifyMentionedInput{
		TaskID:        uuid.New(),
		ProjectID:     projectID,
		CommentText:   "Hey @nonexistent and @stranger",
		ActorMemberID: actorMemberID,
		ActorUserID:   actorUserID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.data) != 0 {
		t.Errorf("expected 0 notifications for non-member mentions, got %d", len(repo.data))
	}
}

func TestNotifyMentioned_MixedValidAndInvalidMentions(t *testing.T) {
	ctx := context.Background()
	repo := newFakeNotificationRepo()
	members := newFakeMemberRepo()
	svc := New(repo, members, nil)

	projectID := uuid.New()
	actorUserID := uuid.New()
	actorMemberID := uuid.New()
	userA := uuid.New()
	memberA := uuid.New()

	members.add(&projectdom.ProjectMember{ID: actorMemberID, ProjectID: projectID, UserID: actorUserID, Username: "commenter"})
	members.add(&projectdom.ProjectMember{ID: memberA, ProjectID: projectID, UserID: userA, Username: "alice"})

	err := svc.NotifyMentioned(ctx, notificationdom.NotifyMentionedInput{
		TaskID:        uuid.New(),
		ProjectID:     projectID,
		CommentText:   "@alice can you check? cc @unknown @nobody",
		ActorMemberID: actorMemberID,
		ActorUserID:   actorUserID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := countNotifications(repo, userA); got != 1 {
		t.Errorf("expected 1 notification for alice, got %d", got)
	}
	if len(repo.data) != 1 {
		t.Errorf("expected exactly 1 notification total, got %d", len(repo.data))
	}
}

func TestNotifyMentioned_CaseInsensitiveMatch(t *testing.T) {
	ctx := context.Background()
	repo := newFakeNotificationRepo()
	members := newFakeMemberRepo()
	svc := New(repo, members, nil)

	projectID := uuid.New()
	actorUserID := uuid.New()
	actorMemberID := uuid.New()
	userA := uuid.New()
	memberA := uuid.New()

	members.add(&projectdom.ProjectMember{ID: actorMemberID, ProjectID: projectID, UserID: actorUserID, Username: "commenter"})
	members.add(&projectdom.ProjectMember{ID: memberA, ProjectID: projectID, UserID: userA, Username: "Alice"})

	err := svc.NotifyMentioned(ctx, notificationdom.NotifyMentionedInput{
		TaskID:        uuid.New(),
		ProjectID:     projectID,
		CommentText:   "Hey @alice please review",
		ActorMemberID: actorMemberID,
		ActorUserID:   actorUserID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := countNotifications(repo, userA); got != 1 {
		t.Errorf("expected case-insensitive match to produce 1 notification, got %d", got)
	}
}

func TestNotifyMentioned_RepoCreateErrorBestEffort(t *testing.T) {
	ctx := context.Background()
	failingRepo := &errCreateRepo{newFakeNotificationRepo()}
	members := newFakeMemberRepo()
	svc := New(failingRepo, members, nil)

	projectID := uuid.New()
	actorUserID := uuid.New()
	actorMemberID := uuid.New()
	userA := uuid.New()
	memberA := uuid.New()

	members.add(&projectdom.ProjectMember{ID: actorMemberID, ProjectID: projectID, UserID: actorUserID, Username: "commenter"})
	members.add(&projectdom.ProjectMember{ID: memberA, ProjectID: projectID, UserID: userA, Username: "alice"})

	err := svc.NotifyMentioned(ctx, notificationdom.NotifyMentionedInput{
		TaskID:        uuid.New(),
		ProjectID:     projectID,
		CommentText:   "@alice check this",
		ActorMemberID: actorMemberID,
		ActorUserID:   actorUserID,
	})
	if err != nil {
		t.Fatalf("expected nil error (best-effort), got %v", err)
	}
}

func TestNotifyMentioned_NilPublisherNoPanic(t *testing.T) {
	ctx := context.Background()
	repo := newFakeNotificationRepo()
	members := newFakeMemberRepo()
	svc := New(repo, members, nil)

	projectID := uuid.New()
	actorUserID := uuid.New()
	actorMemberID := uuid.New()
	userA := uuid.New()
	memberA := uuid.New()

	members.add(&projectdom.ProjectMember{ID: actorMemberID, ProjectID: projectID, UserID: actorUserID, Username: "commenter"})
	members.add(&projectdom.ProjectMember{ID: memberA, ProjectID: projectID, UserID: userA, Username: "alice"})

	err := svc.NotifyMentioned(ctx, notificationdom.NotifyMentionedInput{
		TaskID:        uuid.New(),
		ProjectID:     projectID,
		CommentText:   "@alice hello",
		ActorMemberID: actorMemberID,
		ActorUserID:   actorUserID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNotifyAssigned_NilPublisherNoPanic(t *testing.T) {
	ctx := context.Background()
	repo := newFakeNotificationRepo()
	members := newFakeMemberRepo()
	svc := New(repo, members, nil)

	projectID := uuid.New()
	actorUserID := uuid.New()
	assigneeUserID := uuid.New()
	actorMemberID := uuid.New()
	assigneeMemberID := uuid.New()

	members.add(&projectdom.ProjectMember{ID: actorMemberID, ProjectID: projectID, UserID: actorUserID, Username: "actor"})
	members.add(&projectdom.ProjectMember{ID: assigneeMemberID, ProjectID: projectID, UserID: assigneeUserID, Username: "assignee"})

	err := svc.NotifyAssigned(ctx, notificationdom.NotifyAssignedInput{
		TaskID:              uuid.New(),
		ProjectID:           projectID,
		NewAssigneeMemberID: assigneeMemberID,
		ActorUserID:         actorUserID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractMentions_NoMentions(t *testing.T) {
	result := extractMentions("just some plain text without any mentions")
	if len(result) != 0 {
		t.Errorf("expected 0 mentions, got %d", len(result))
	}
}

func TestExtractMentions_SingleMention(t *testing.T) {
	result := extractMentions("hello @alice world")
	if len(result) != 1 {
		t.Fatalf("expected 1 mention, got %d", len(result))
	}
	if _, ok := result["alice"]; !ok {
		t.Error("expected 'alice' in mentions")
	}
}

func TestExtractMentions_MultipleMentions(t *testing.T) {
	result := extractMentions("@alice @bob @charlie")
	if len(result) != 3 {
		t.Fatalf("expected 3 unique mentions, got %d", len(result))
	}
	for _, name := range []string{"alice", "bob", "charlie"} {
		if _, ok := result[name]; !ok {
			t.Errorf("expected '%s' in mentions", name)
		}
	}
}

func TestExtractMentions_DuplicateMentionsDeduped(t *testing.T) {
	result := extractMentions("@alice @bob @alice @bob")
	if len(result) != 2 {
		t.Errorf("expected 2 unique mentions, got %d", len(result))
	}
}

func TestExtractMentions_UsernamePatterns(t *testing.T) {
	result := extractMentions("hi @user.name @user-name @user_name @User123")
	if len(result) != 4 {
		t.Fatalf("expected 4 mentions, got %d", len(result))
	}
	for _, name := range []string{"user.name", "user-name", "user_name", "User123"} {
		if _, ok := result[name]; !ok {
			t.Errorf("expected '%s' in mentions", name)
		}
	}
}

func TestExtractMentions_MentionAtEnd(t *testing.T) {
	result := extractMentions("cc @alice")
	if len(result) != 1 {
		t.Errorf("expected 1 mention, got %d", len(result))
	}
	if _, ok := result["alice"]; !ok {
		t.Error("expected 'alice' in mentions")
	}
}

func TestExtractMentions_MentionAtStart(t *testing.T) {
	result := extractMentions("@alice please review")
	if len(result) != 1 {
		t.Errorf("expected 1 mention, got %d", len(result))
	}
	if _, ok := result["alice"]; !ok {
		t.Error("expected 'alice' in mentions")
	}
}

func TestExtractMentions_EmptyText(t *testing.T) {
	result := extractMentions("")
	if len(result) != 0 {
		t.Errorf("expected 0 mentions for empty text, got %d", len(result))
	}
}

func TestExtractMentions_AtSignAlone(t *testing.T) {
	result := extractMentions("@")
	if len(result) != 0 {
		t.Errorf("expected 0 mentions for lone @, got %d", len(result))
	}
}

func TestNotifyMentioned_EmptyCommentText(t *testing.T) {
	ctx := context.Background()
	repo := newFakeNotificationRepo()
	members := newFakeMemberRepo()
	svc := New(repo, members, nil)

	err := svc.NotifyMentioned(ctx, notificationdom.NotifyMentionedInput{
		TaskID:        uuid.New(),
		ProjectID:     uuid.New(),
		CommentText:   "",
		ActorMemberID: uuid.New(),
		ActorUserID:   uuid.New(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.data) != 0 {
		t.Errorf("expected 0 notifications for empty text, got %d", len(repo.data))
	}
}

func TestNotifyMentioned_MembersListErrorSkipsSilently(t *testing.T) {
	ctx := context.Background()
	repo := newFakeNotificationRepo()
	members := newFakeMemberRepo()
	svc := New(repo, members, nil)

	projectID := uuid.New()

	err := svc.NotifyMentioned(ctx, notificationdom.NotifyMentionedInput{
		TaskID:        uuid.New(),
		ProjectID:     projectID,
		CommentText:   "@alice hello",
		ActorMemberID: uuid.New(),
		ActorUserID:   uuid.New(),
	})
	if err != nil {
		t.Fatalf("expected nil error when ListMembers returns empty, got %v", err)
	}
	if len(repo.data) != 0 {
		t.Errorf("expected 0 notifications when project has no members, got %d", len(repo.data))
	}
}
