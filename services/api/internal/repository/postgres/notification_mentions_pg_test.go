package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
)

// The mention queries run against a real schema: the joins that resolve a
// task's sprint and status at read time, the type/project/agent filters, the
// per-task unread counts and the task_reads upsert.
func TestNotificationRepo_MentionsInContext_PG(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	ctx := context.Background()
	repo := NewNotificationRepository(db)

	p := fx.project()
	recipient, actor := fx.user(false), fx.user(false)
	actorMember := fx.member(p, actor, false)
	fx.member(p, recipient, false)
	agent := fx.agent(&p, false)
	agentMember := fx.member(p, agent, false)

	sprint, status := uuid.New(), uuid.New()
	pgExec(t, db, `INSERT INTO sprints (id, project_id, name) VALUES ($1,$2,'Sprint 12')`, sprint, p)
	pgExec(t, db, `INSERT INTO task_statuses (id, project_id, name, color, position, category) VALUES ($1,$2,'In progress','#f90',0,'inprogress')`, status, p)
	inSprint, backlog := uuid.New(), uuid.New()
	pgExec(t, db, `INSERT INTO tasks (id, project_id, task_number, title, sprint_id, status_id) VALUES ($1,$2,1,'In sprint',$3,$4)`, inSprint, p, sprint, status)
	pgExec(t, db, `INSERT INTO tasks (id, project_id, task_number, title) VALUES ($1,$2,2,'In backlog')`, backlog, p)

	comment := uuid.New()
	mention := func(task uuid.UUID, by uuid.UUID, src notificationdom.MentionSource, excerpt string, at time.Time) *notificationdom.Notification {
		n := &notificationdom.Notification{
			ID: uuid.New(), RecipientAccountID: recipient, ActorMemberID: &by,
			Type: notificationdom.NotificationTypeMentioned, TaskID: &task, ProjectID: p,
			MentionSource: src, Excerpt: excerpt, CreatedAt: at,
		}
		if src == notificationdom.MentionSourceComment {
			n.CommentID = &comment
		}
		if err := repo.Create(ctx, n); err != nil {
			t.Fatalf("create: %v", err)
		}
		return n
	}
	now := time.Now().UTC()
	m1 := mention(inSprint, actorMember, notificationdom.MentionSourceComment, "hey @Lee", now.Add(-3*time.Minute))
	mention(backlog, agentMember, notificationdom.MentionSourceDescription, "", now.Add(-2*time.Minute))
	assigned := &notificationdom.Notification{
		ID: uuid.New(), RecipientAccountID: recipient, Type: notificationdom.NotificationTypeAssigned,
		TaskID: &inSprint, ProjectID: p, CreatedAt: now.Add(-time.Minute),
	}
	if err := repo.Create(ctx, assigned); err != nil {
		t.Fatal(err)
	}

	// Read-time context: sprint, status, comment link, excerpt.
	all, _, err := repo.ListForUser(ctx, recipient, 20, nil, notificationdom.ListFilter{})
	if err != nil || len(all) != 3 {
		t.Fatalf("list: %v (%d)", err, len(all))
	}
	var got *notificationdom.Notification
	for _, n := range all {
		if n.ID == m1.ID {
			got = n
		}
	}
	if got == nil || got.SprintName != "Sprint 12" || got.SprintID == nil || *got.SprintID != sprint ||
		got.TaskStatusName != "In progress" || got.TaskStatusColor != "#f90" ||
		got.CommentID == nil || *got.CommentID != comment || got.Excerpt != "hey @Lee" ||
		got.MentionSource != notificationdom.MentionSourceComment {
		t.Fatalf("mention context wrong: %+v", got)
	}
	for _, n := range all {
		if n.TaskID != nil && *n.TaskID == backlog && (n.SprintID != nil || n.SprintName != "") {
			t.Fatalf("backlog task must have no sprint: %+v", n)
		}
	}

	// Filters.
	byType, _, _ := repo.ListForUser(ctx, recipient, 20, nil, notificationdom.ListFilter{Types: []notificationdom.NotificationType{notificationdom.NotificationTypeMentioned}})
	if len(byType) != 2 {
		t.Fatalf("type filter: want 2, got %d", len(byType))
	}
	byAgent, _, _ := repo.ListForUser(ctx, recipient, 20, nil, notificationdom.ListFilter{AgentActor: true})
	if len(byAgent) != 1 || byAgent[0].MentionSource != notificationdom.MentionSourceDescription {
		t.Fatalf("agent filter: %+v", byAgent)
	}
	other := uuid.New()
	byProject, _, _ := repo.ListForUser(ctx, recipient, 20, nil, notificationdom.ListFilter{ProjectID: &other})
	if len(byProject) != 0 {
		t.Fatalf("project filter: want 0, got %d", len(byProject))
	}

	// Unread mentions per task, then read by opening one task.
	unread, err := repo.UnreadMentionsByTask(ctx, recipient, nil)
	if err != nil || len(unread) != 2 {
		t.Fatalf("unread mentions: %v %+v", err, unread)
	}
	for _, u := range unread {
		if u.TaskID == inSprint && (u.SprintID == nil || *u.SprintID != sprint) {
			t.Fatalf("sprint id missing: %+v", u)
		}
	}
	if err := repo.MarkTaskMentionsRead(ctx, recipient, inSprint); err != nil {
		t.Fatal(err)
	}
	unread, _ = repo.UnreadMentionsByTask(ctx, recipient, &p)
	if len(unread) != 1 || unread[0].TaskID != backlog {
		t.Fatalf("after read: %+v", unread)
	}

	// task_reads: first touch returns nil, the next returns the first time, and
	// the stored time never moves backwards.
	t1 := now.Truncate(time.Microsecond) // Postgres keeps microseconds
	prev, err := repo.TouchTaskRead(ctx, recipient, inSprint, t1)
	if err != nil || prev != nil {
		t.Fatalf("first touch: %v %v", prev, err)
	}
	prev, err = repo.TouchTaskRead(ctx, recipient, inSprint, t1.Add(-time.Hour))
	if err != nil || prev == nil || !prev.Equal(t1) {
		t.Fatalf("second touch: %v %v", prev, err)
	}
	seen, _ := repo.TaskLastSeen(ctx, recipient, inSprint)
	if seen == nil || !seen.Equal(t1) {
		t.Fatalf("last seen moved backwards: %v", seen)
	}
}
