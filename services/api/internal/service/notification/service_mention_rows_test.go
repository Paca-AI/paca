package notificationsvc

import (
	"context"
	"testing"

	"github.com/google/uuid"

	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/events"
)

type mentionFx struct {
	repo      *fakeNotificationRepo
	members   *fakeMemberRepo
	pub       *fakePublisher
	users     *fakeUserRepo
	svc       *Svc
	project   uuid.UUID
	actor     uuid.UUID
	actorMem  uuid.UUID
	mentioned uuid.UUID
}

func newMentionFx(t *testing.T) *mentionFx {
	t.Helper()
	f := &mentionFx{
		repo: newFakeNotificationRepo(), members: newFakeMemberRepo(), pub: &fakePublisher{},
		project: uuid.New(), actor: uuid.New(), actorMem: uuid.New(), mentioned: uuid.New(),
	}
	f.users = newFakeUserRepo()
	f.users.byID[f.mentioned] = &userdom.User{ID: f.mentioned, FullName: "Alice"}
	f.svc = New(f.repo, f.members, f.pub).WithEventPublishing(f.users, "https://paca.example")
	f.members.add(&projectdom.ProjectMember{ID: f.actorMem, ProjectID: f.project, UserID: f.actor, AccountID: f.actor, Username: "actor"})
	f.members.add(&projectdom.ProjectMember{ID: uuid.New(), ProjectID: f.project, UserID: f.mentioned, AccountID: f.mentioned, Username: "alice"})
	return f
}

func TestNotifyDocMentioned_CreatesInboxRowAndEvent(t *testing.T) {
	f := newMentionFx(t)
	docID := uuid.New()

	f.svc.NotifyDocMentioned(context.Background(), f.mentioned, f.actor, f.project, docID)

	n := findNotification(f.repo, f.mentioned, notificationdom.NotificationTypeMentioned)
	if n == nil {
		t.Fatal("a doc-content mention must create an in-app notification")
	}
	if n.DocumentID == nil || *n.DocumentID != docID || n.TaskID != nil {
		t.Fatalf("the row must point at the document only, got doc=%v task=%v", n.DocumentID, n.TaskID)
	}
	if n.ActorMemberID == nil || *n.ActorMemberID != f.actorMem {
		t.Fatalf("the row must credit the actor's membership, got %v", n.ActorMemberID)
	}
	if len(f.repo.delivered) != 1 {
		t.Fatalf("the realtime announcement should be marked delivered, got %d", len(f.repo.delivered))
	}
	if ev, ok := f.pub.lastAppend(); !ok || ev.eventType != events.TopicNotificationDocMentioned {
		t.Fatalf("the plugin event must still be published, got %+v", ev)
	}
}

func TestNotifyTaskDescriptionMentioned_CreatesInboxRowAndEvent(t *testing.T) {
	f := newMentionFx(t)
	taskID := uuid.New()

	f.svc.NotifyTaskDescriptionMentioned(context.Background(), f.mentioned, f.actor, f.project, taskID)

	n := findNotification(f.repo, f.mentioned, notificationdom.NotificationTypeMentioned)
	if n == nil {
		t.Fatal("a description mention must create an in-app notification")
	}
	if n.TaskID == nil || *n.TaskID != taskID || n.DocumentID != nil {
		t.Fatalf("the row must point at the task only, got task=%v doc=%v", n.TaskID, n.DocumentID)
	}
	if ev, ok := f.pub.lastAppend(); !ok || ev.eventType != events.TopicNotificationTaskDescriptionMentioned {
		t.Fatalf("the plugin event must still be published, got %+v", ev)
	}
}

func TestContentMentions_NoRowForSelfOrNonMember(t *testing.T) {
	f := newMentionFx(t)
	ctx := context.Background()

	f.svc.NotifyDocMentioned(ctx, f.actor, f.actor, f.project, uuid.New())
	f.svc.NotifyDocMentioned(ctx, uuid.New(), f.actor, f.project, uuid.New()) // not a member
	f.svc.NotifyTaskDescriptionMentioned(ctx, f.actor, f.actor, f.project, uuid.New())
	f.svc.NotifyTaskDescriptionMentioned(ctx, uuid.New(), f.actor, f.project, uuid.New())

	if len(f.repo.data) != 0 {
		t.Fatalf("expected no notifications, got %d", len(f.repo.data))
	}
	if _, ok := f.pub.lastAppend(); ok {
		t.Fatal("expected no plugin events either")
	}
}

func TestContentMentions_AgentMentionStoresNoRow(t *testing.T) {
	f := newMentionFx(t)
	agent := uuid.New()
	f.members.add(&projectdom.ProjectMember{ID: uuid.New(), ProjectID: f.project, UserID: agent, AccountID: agent, Username: "bot", MemberType: "agent"})

	f.svc.NotifyDocMentioned(context.Background(), agent, f.actor, f.project, uuid.New())

	if len(f.repo.data) != 0 {
		t.Fatal("an agent has no inbox: the mention must not store a notification")
	}
}

func TestContentMention_StorageFailureStillPublishesPluginEvent(t *testing.T) {
	noRetryDelay(t)
	f := newMentionFx(t)
	f.svc = New(&errCreateRepo{newFakeNotificationRepo()}, f.members, f.pub).WithEventPublishing(f.users, "https://paca.example")

	f.svc.NotifyDocMentioned(context.Background(), f.mentioned, f.actor, f.project, uuid.New())

	if ev, ok := f.pub.lastAppend(); !ok || ev.eventType != events.TopicNotificationDocMentioned {
		t.Fatalf("a failed insert must not suppress the email event, got %+v", ev)
	}
}
