package notificationsvc

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	userdom "github.com/Paca-AI/api/internal/domain/user"
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

// ListForUser mirrors the real repository's keyset-pagination contract
// (ordered created_at DESC, id DESC; cursorAfter excludes everything at or
// after that boundary; hasMore reports whether more rows remain) so tests
// can exercise pagination against something more than always returning
// everything at once.
func (r *fakeNotificationRepo) ListForUser(_ context.Context, userID uuid.UUID, limit int, cursorAfter *string) ([]*notificationdom.Notification, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var all []*notificationdom.Notification
	for _, n := range r.data {
		if n.RecipientUserID == userID {
			cp := *n
			all = append(all, &cp)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if !all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].CreatedAt.After(all[j].CreatedAt)
		}
		return all[i].ID.String() > all[j].ID.String()
	})

	if cursorAfter != nil {
		cur, err := notificationdom.DecodeNotificationCursor(*cursorAfter)
		if err != nil {
			return nil, false, fmt.Errorf("%w: %s", notificationdom.ErrInvalidCursor, err)
		}
		idx := 0
		for idx < len(all) {
			n := all[idx]
			if n.CreatedAt.Before(cur.CreatedAt) ||
				(n.CreatedAt.Equal(cur.CreatedAt) && n.ID.String() < cur.ID) {
				break
			}
			idx++
		}
		all = all[idx:]
	}

	if limit <= 0 {
		limit = 50
	}
	hasMore := len(all) > limit
	if hasMore {
		all = all[:limit]
	}
	return all, hasMore, nil
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
	mu      sync.RWMutex
	byID    map[uuid.UUID]*projectdom.ProjectMember
	byProj  map[uuid.UUID][]*projectdom.ProjectMember
	byUser  map[[2]uuid.UUID]*projectdom.ProjectMember
	byAgent map[[2]uuid.UUID]*projectdom.ProjectMember
}

type userProjectKey = [2]uuid.UUID

func newFakeMemberRepo() *fakeMemberRepo {
	return &fakeMemberRepo{
		byID:    make(map[uuid.UUID]*projectdom.ProjectMember),
		byProj:  make(map[uuid.UUID][]*projectdom.ProjectMember),
		byUser:  make(map[[2]uuid.UUID]*projectdom.ProjectMember),
		byAgent: make(map[[2]uuid.UUID]*projectdom.ProjectMember),
	}
}

func (r *fakeMemberRepo) add(m *projectdom.ProjectMember) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *m
	r.byID[m.ID] = &cp
	r.byProj[m.ProjectID] = append(r.byProj[m.ProjectID], &cp)
	if m.AgentID != nil {
		r.byAgent[userProjectKey{*m.AgentID, m.ProjectID}] = &cp
	} else {
		r.byUser[userProjectKey{m.UserID, m.ProjectID}] = &cp
	}
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

func (r *fakeMemberRepo) FindMemberByActor(ctx context.Context, projectID, actorID uuid.UUID, agentID *uuid.UUID) (*projectdom.ProjectMember, error) {
	if agentID != nil {
		r.mu.RLock()
		m, ok := r.byAgent[userProjectKey{*agentID, projectID}]
		r.mu.RUnlock()
		if !ok {
			return nil, fmt.Errorf("member not found for agent %s project %s", *agentID, projectID)
		}
		cp := *m
		return &cp, nil
	}
	return r.FindMemberByUserProject(ctx, actorID, projectID)
}

func (r *fakeMemberRepo) ListMembers(_ context.Context, projectID uuid.UUID) ([]*projectdom.ProjectMember, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byProj[projectID], nil
}

// fakeUserRepo is a spy userLookup: it records every ID passed to FindByID
// so tests can assert whether a lookup happened at all (e.g. to prove a
// membership check short-circuited before ever resolving the mentioned
// user's name/email).
type fakeUserRepo struct {
	mu    sync.Mutex
	byID  map[uuid.UUID]*userdom.User
	calls []uuid.UUID
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{byID: make(map[uuid.UUID]*userdom.User)}
}

func (r *fakeUserRepo) add(u *userdom.User) {
	r.byID[u.ID] = u
}

func (r *fakeUserRepo) FindByID(_ context.Context, id uuid.UUID) (*userdom.User, error) {
	r.mu.Lock()
	r.calls = append(r.calls, id)
	r.mu.Unlock()
	u, ok := r.byID[id]
	if !ok {
		return nil, userdom.ErrNotFound
	}
	return u, nil
}

func (r *fakeUserRepo) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
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

// TestNotifyAssigned_AgentActor_ResolvesAgentMember guards the case a human
// user reported: when an AI agent (not a human) assigns a task, the
// notification's ActorMemberID must resolve to the agent's own project
// member record — so the notification list can show the agent's name and
// avatar — rather than trying (and failing) to resolve a human member from
// the agent-authenticated request's underlying actor user ID.
func TestNotifyAssigned_AgentActor_ResolvesAgentMember(t *testing.T) {
	ctx := context.Background()
	repo := newFakeNotificationRepo()
	members := newFakeMemberRepo()
	svc := New(repo, members, nil)

	projectID := uuid.New()
	agentID := uuid.New()
	agentMemberID := uuid.New()
	assigneeUserID := uuid.New()
	assigneeMemberID := uuid.New()

	members.add(&projectdom.ProjectMember{
		ID: agentMemberID, ProjectID: projectID, MemberType: "agent", AgentID: &agentID,
	})
	members.add(&projectdom.ProjectMember{
		ID: assigneeMemberID, ProjectID: projectID, UserID: assigneeUserID, Username: "assignee",
	})

	taskID := uuid.New()
	err := svc.NotifyAssigned(ctx, notificationdom.NotifyAssignedInput{
		TaskID:              taskID,
		ProjectID:           projectID,
		NewAssigneeMemberID: assigneeMemberID,
		// A pure agent-authenticated request has no meaningful human actor
		// user ID to resolve against — ActorAgentID is what identifies the
		// actor here.
		ActorUserID:  uuid.New(),
		ActorAgentID: &agentID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	n := findNotification(repo, assigneeUserID, notificationdom.NotificationTypeAssigned)
	if n == nil {
		t.Fatal("expected to find assigned notification")
	}
	if n.ActorMemberID == nil || *n.ActorMemberID != agentMemberID {
		t.Errorf("expected ActorMemberID=%s (the agent's member record), got %v", agentMemberID, n.ActorMemberID)
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

// --- Cursor pagination -------------------------------------------------------

func addTestNotification(repo *fakeNotificationRepo, recipientID uuid.UUID, createdAt time.Time) *notificationdom.Notification {
	n := &notificationdom.Notification{
		ID:              uuid.New(),
		RecipientUserID: recipientID,
		Type:            notificationdom.NotificationTypeMentioned,
		CreatedAt:       createdAt,
	}
	repo.data[n.ID] = n
	return n
}

// TestListNotifications_Pagination walks a full set of notifications two
// pages at a time via the cursor ListNotifications returns, and checks that
// every notification is seen exactly once, newest first, with no
// duplicates or gaps across the page boundary — the core guarantee cursor
// pagination has to hold.
func TestListNotifications_Pagination(t *testing.T) {
	ctx := context.Background()
	repo := newFakeNotificationRepo()
	members := newFakeMemberRepo()
	svc := New(repo, members, nil)

	recipientID := uuid.New()
	const total = 5
	base := time.Now().Truncate(time.Millisecond)
	var created []*notificationdom.Notification
	for i := 0; i < total; i++ {
		// Distinct, strictly increasing timestamps so newest-first order is
		// unambiguous — the i'th notification is older than the (i+1)'th.
		created = append(created, addTestNotification(repo, recipientID, base.Add(time.Duration(i)*time.Second)))
	}

	const pageSize = 2
	var seen []*notificationdom.Notification
	var cursor *string
	for pages := 0; ; pages++ {
		if pages > total {
			t.Fatalf("pagination did not terminate after %d pages", pages)
		}
		page, hasMore, err := svc.ListNotifications(ctx, recipientID, pageSize, cursor)
		if err != nil {
			t.Fatalf("unexpected error on page %d: %v", pages, err)
		}
		seen = append(seen, page...)
		if !hasMore {
			if len(page) == 0 && pages == 0 {
				t.Fatal("expected at least one page of results")
			}
			break
		}
		if len(page) != pageSize {
			t.Fatalf("expected full page of %d, got %d", pageSize, len(page))
		}
		s := notificationdom.EncodeNotificationCursor(page[len(page)-1])
		cursor = &s
	}

	if len(seen) != total {
		t.Fatalf("expected %d total notifications across all pages, got %d", total, len(seen))
	}
	seenIDs := make(map[uuid.UUID]int)
	for _, n := range seen {
		seenIDs[n.ID]++
	}
	for _, n := range created {
		if seenIDs[n.ID] != 1 {
			t.Errorf("notification %s seen %d times across pages, expected exactly 1", n.ID, seenIDs[n.ID])
		}
	}
	for i := 1; i < len(seen); i++ {
		if seen[i-1].CreatedAt.Before(seen[i].CreatedAt) {
			t.Errorf("expected newest-first order, but item %d (created %v) is older than item %d (created %v)",
				i-1, seen[i-1].CreatedAt, i, seen[i].CreatedAt)
		}
	}
}

// TestListNotifications_LastPageHasNoNextCursor guards the handler-facing
// contract: once every notification has been returned, hasMore is false so
// the handler doesn't hand back a next_cursor that would just 404/empty on
// the next request.
func TestListNotifications_LastPageHasNoNextCursor(t *testing.T) {
	ctx := context.Background()
	repo := newFakeNotificationRepo()
	members := newFakeMemberRepo()
	svc := New(repo, members, nil)

	recipientID := uuid.New()
	addTestNotification(repo, recipientID, time.Now())

	page, hasMore, err := svc.ListNotifications(ctx, recipientID, 20, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(page))
	}
	if hasMore {
		t.Error("expected hasMore=false when every notification fits on one page")
	}
}

// TestListNotifications_InvalidCursorErrors guards against a malformed
// client-supplied cursor being silently ignored (which would look to the
// caller like pagination just quietly restarted from the top).
func TestListNotifications_InvalidCursorErrors(t *testing.T) {
	ctx := context.Background()
	repo := newFakeNotificationRepo()
	members := newFakeMemberRepo()
	svc := New(repo, members, nil)

	bad := "not-a-valid-cursor"
	_, _, err := svc.ListNotifications(ctx, uuid.New(), 20, &bad)
	if err == nil {
		t.Fatal("expected error for invalid cursor, got nil")
	}
}

// --- NotifyDocMentioned / NotifyTaskDescriptionMentioned --------------------
//
// mentionedUserID for both of these comes straight from client-supplied
// BlockNote content, not a server-restricted picker, so a project-membership
// check is the only thing standing between "any editor" and "leak an
// arbitrary user's name/email to a subscribing plugin". These tests use a
// nil publisher (as the rest of this file's Svc instances do), which makes
// publishNotificationEvent a no-op before it would otherwise resolve the
// *recipient* — so a passing membership check is observed via exactly one
// fakeUserRepo.FindByID call (resolveUserName resolving the *actor*'s name),
// and a failing one via zero calls, proving the check runs before any lookup
// that could leak the mentioned user's identity.

func TestNotifyDocMentioned_NonMemberIgnored(t *testing.T) {
	ctx := context.Background()
	repo := newFakeNotificationRepo()
	members := newFakeMemberRepo()
	users := newFakeUserRepo()
	svc := New(repo, members, nil).WithEventPublishing(users, "https://paca.example")

	projectID := uuid.New()
	actorUserID := uuid.New()
	strangerID := uuid.New() // never added as a member of projectID

	svc.NotifyDocMentioned(ctx, strangerID, actorUserID, projectID, uuid.New())

	if got := users.callCount(); got != 0 {
		t.Errorf("expected no user lookup for a mention naming a non-member, got %d calls", got)
	}
}

func TestNotifyDocMentioned_MemberNotified(t *testing.T) {
	ctx := context.Background()
	repo := newFakeNotificationRepo()
	members := newFakeMemberRepo()
	users := newFakeUserRepo()
	svc := New(repo, members, nil).WithEventPublishing(users, "https://paca.example")

	projectID := uuid.New()
	actorUserID := uuid.New()
	mentionedUserID := uuid.New()
	members.add(&projectdom.ProjectMember{ID: uuid.New(), ProjectID: projectID, UserID: mentionedUserID, Username: "alice"})

	svc.NotifyDocMentioned(ctx, mentionedUserID, actorUserID, projectID, uuid.New())

	if got := users.callCount(); got != 1 {
		t.Errorf("expected the actor-name lookup to proceed for a mentioned project member, got %d calls", got)
	}
}

func TestNotifyDocMentioned_SelfMentionSuppressed(t *testing.T) {
	ctx := context.Background()
	repo := newFakeNotificationRepo()
	members := newFakeMemberRepo()
	users := newFakeUserRepo()
	svc := New(repo, members, nil).WithEventPublishing(users, "https://paca.example")

	projectID := uuid.New()
	userID := uuid.New()
	members.add(&projectdom.ProjectMember{ID: uuid.New(), ProjectID: projectID, UserID: userID, Username: "myself"})

	svc.NotifyDocMentioned(ctx, userID, userID, projectID, uuid.New())

	if got := users.callCount(); got != 0 {
		t.Errorf("expected self-mention to short-circuit before any lookup, got %d calls", got)
	}
}

func TestNotifyTaskDescriptionMentioned_NonMemberIgnored(t *testing.T) {
	ctx := context.Background()
	repo := newFakeNotificationRepo()
	members := newFakeMemberRepo()
	users := newFakeUserRepo()
	svc := New(repo, members, nil).WithEventPublishing(users, "https://paca.example")

	projectID := uuid.New()
	actorUserID := uuid.New()
	strangerID := uuid.New() // never added as a member of projectID

	svc.NotifyTaskDescriptionMentioned(ctx, strangerID, actorUserID, projectID, uuid.New())

	if got := users.callCount(); got != 0 {
		t.Errorf("expected no user lookup for a mention naming a non-member, got %d calls", got)
	}
}

func TestNotifyTaskDescriptionMentioned_MemberNotified(t *testing.T) {
	ctx := context.Background()
	repo := newFakeNotificationRepo()
	members := newFakeMemberRepo()
	users := newFakeUserRepo()
	svc := New(repo, members, nil).WithEventPublishing(users, "https://paca.example")

	projectID := uuid.New()
	actorUserID := uuid.New()
	mentionedUserID := uuid.New()
	members.add(&projectdom.ProjectMember{ID: uuid.New(), ProjectID: projectID, UserID: mentionedUserID, Username: "alice"})

	svc.NotifyTaskDescriptionMentioned(ctx, mentionedUserID, actorUserID, projectID, uuid.New())

	if got := users.callCount(); got != 1 {
		t.Errorf("expected the actor-name lookup to proceed for a mentioned project member, got %d calls", got)
	}
}
