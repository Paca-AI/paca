package events_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/Paca-AI/api/internal/events"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

// notRealtimeTopics are the topics in topics.go that never reach a project
// room: auth/user lifecycle, agent triggers and conversation events (the
// latter are routed by services/realtime on their own conversation check),
// notifications (routed to the recipient's own room), and the stream-only
// commands. A new topic must be added here or get an AuthzFor rule — the test
// fails until it is one of the two, so a topic cannot silently skip IAM.
var notRealtimeTopics = map[string]string{
	"iam.changed":         "handled by services/realtime itself, never delivered",
	"user.created":        "auth lifecycle",
	"user.deleted":        "auth lifecycle",
	"user.password_reset": "auth lifecycle",
	"auth.login":          "auth lifecycle",
	"auth.logout":         "auth lifecycle",

	"notification.assigned":                   "plugin stream; recipient-routed",
	"notification.mentioned":                  "plugin stream; recipient-routed",
	"notification.doc_mentioned":              "plugin stream; recipient-routed",
	"notification.task_description_mentioned": "plugin stream; recipient-routed",
	"notification.created":                    "routed to the recipient's own room",

	"agent.task_assigned":          "agent trigger stream",
	"agent.comment_mention":        "agent trigger stream",
	"agent.chat_message":           "agent trigger stream",
	"agent.description_write":      "agent trigger stream",
	"agent.automation_message":     "agent trigger stream",
	"agent.stop":                   "agent trigger stream",
	"agent.pause":                  "agent trigger stream",
	"agent.heartbeat":              "agent trigger stream",
	"agent.conversation.started":   "published by ai-agent; checked per conversation",
	"agent.conversation.finished":  "published by ai-agent; checked per conversation",
	"agent.conversation.failed":    "published by ai-agent; checked per conversation",
	"agent.conversation.paused":    "published by ai-agent; checked per conversation",
	"agent.conversation.resumed":   "published by ai-agent; checked per conversation",
	"agent.conversation.stopped":   "published by ai-agent; checked per conversation",
	"agent.thinking":               "published by ai-agent; checked per conversation",
	"agent.action":                 "published by ai-agent; checked per conversation",
	"agent.observation":            "published by ai-agent; checked per conversation",
	"agent.message":                "published by ai-agent; checked per conversation",
	"automation.api_trigger.fired": "webhook stream",
	"project.export.requested":     "export stream",
	"environment.create":           "environment command stream",
	"environment.start":            "environment command stream",
	"environment.stop":             "environment command stream",
}

// topicConstants returns the value of every Topic* constant in topics.go.
func topicConstants(t *testing.T) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "topics.go", nil, 0)
	if err != nil {
		t.Fatalf("parse topics.go: %v", err)
	}
	var out []string
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, name := range vs.Names {
				if !strings.HasPrefix(name.Name, "Topic") || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				v, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("unquote %s: %v", name.Name, err)
				}
				out = append(out, v)
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("found no Topic constants in topics.go")
	}
	return out
}

// TestEveryTopicIsClassified fails when a topic is added without deciding
// what IAM must allow before a client sees it. The decision is either an
// AuthzFor rule or an entry in notRealtimeTopics.
func TestEveryTopicIsClassified(t *testing.T) {
	project := uuid.New()
	entity := uuid.New()
	topics := topicConstants(t)
	// Activity types owned by the domains reach realtime through the same
	// fanout and share the prefix rules.
	for _, a := range []string{
		string(taskdom.ActivityTypeTaskCreated), string(taskdom.ActivityTypeTaskUpdated),
		string(taskdom.ActivityTypeTaskDeleted), string(taskdom.ActivityTypeAttachmentAdded),
		string(taskdom.ActivityTypeAttachmentRemoved), string(taskdom.ActivityTypeTaskLinkAdded),
		string(taskdom.ActivityTypeTaskLinkRemoved), string(taskdom.ActivityTypeAutomationApplied),
		string(docdom.ActivityTypeDocCreated), string(docdom.ActivityTypeDocUpdated),
		string(docdom.ActivityTypeDocDeleted), string(docdom.ActivityTypeDocMoved),
		string(docdom.ActivityTypeFolderCreated), string(docdom.ActivityTypeFolderUpdated),
		string(docdom.ActivityTypeFolderDeleted),
		"sprint.created", "sprint.updated", "sprint.deleted", "sprint.completed",
		"workflow.assigned",
	} {
		if !slices.Contains(topics, a) {
			topics = append(topics, a)
		}
	}

	for _, topic := range topics {
		if _, skipped := notRealtimeTopics[topic]; skipped {
			continue
		}
		if _, ok := events.AuthzFor(events.Event{Topic: topic, ProjectID: project, EntityID: &entity}); !ok {
			t.Errorf("%s has no AuthzFor rule and is not in notRealtimeTopics: decide what IAM must allow before a client sees it", topic)
		}
	}
	for topic := range notRealtimeTopics {
		if !slices.Contains(topics, topic) {
			t.Errorf("notRealtimeTopics lists %s, which is not a topic any more", topic)
		}
	}
}

// TestAuthzFor_ActionsAndResourcesAreValid pins that every derived descriptor
// names a registered action and a resource the engine can parse back to the
// event's project.
func TestAuthzFor_ActionsAndResourcesAreValid(t *testing.T) {
	project, entity := uuid.New(), uuid.New()
	registered := map[string]bool{}
	for _, a := range iam.BuiltinActions() {
		registered[string(a)] = true
	}

	cases := []struct {
		name       string
		ev         events.Event
		wantAction iam.Action
		wantRes    string
	}{
		{"task entity", events.Event{Topic: "task.updated", EntityType: events.EntityTask, EntityID: &entity}, iam.ActionTasksRead, "project/%s/task/%s"},
		{"task via TaskID", events.Event{Topic: "git.task.updated", TaskID: &entity}, iam.ActionTasksRead, "project/%s/task/%s"},
		{"git.task.updated is task scoped", events.Event{Topic: "git.task.updated", TaskID: &entity}, iam.ActionTasksRead, "project/%s/task/%s"},
		{"automation.applied is task scoped", events.Event{Topic: "automation.applied", TaskID: &entity}, iam.ActionTasksRead, "project/%s/task/%s"},
		{"doc", events.Event{Topic: "doc.updated", EntityType: events.EntityDoc, EntityID: &entity}, iam.ActionDocsRead, "project/%s/doc/%s"},
		{"doc folder has no IAM kind", events.Event{Topic: "doc.folder.created", EntityType: events.EntityDoc, EntityID: &entity}, iam.ActionDocsRead, "project/%s"},
		{"sprint", events.Event{Topic: "sprint.completed", EntityType: events.EntitySprint, EntityID: &entity}, iam.ActionSprintsRead, "project/%s/sprint/%s"},
		{"view uses views:read", events.Event{Topic: "view.updated", EntityType: events.EntityView, EntityID: &entity}, iam.ActionViewsRead, "project/%s/view/%s"},
		{"view reorder names no view", events.Event{Topic: "view.reordered", EntityType: events.EntityView}, iam.ActionViewsRead, "project/%s"},
		{"automation graph", events.Event{Topic: "automation.node.updated", EntityType: events.EntityAutomation, EntityID: &entity}, iam.ActionWorkflowsRead, "project/%s/workflow/%s"},
		{"environment", events.Event{Topic: "environment.status_changed", EntityType: events.EntityEnvironment, EntityID: &entity}, iam.ActionEnvironmentsRead, "project/%s/environment/%s"},
		{"agent session started is a conversation event", events.Event{Topic: "agent.session.started", EntityType: events.EntityTask, TaskID: &entity, Payload: map[string]any{"conversation_id": "c1"}}, iam.ActionConversationsRead, "project/%s/conversation/c1"},
		{"entity of the wrong type falls back to project", events.Event{Topic: "task.updated", EntityType: events.EntityDoc, EntityID: &entity}, iam.ActionTasksRead, "project/%s"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.ev.ProjectID = project
			got, ok := events.AuthzFor(c.ev)
			if !ok {
				t.Fatal("no descriptor")
			}
			if got.Action != string(c.wantAction) {
				t.Errorf("action = %q, want %q", got.Action, c.wantAction)
			}
			want := c.wantRes
			switch strings.Count(want, "%s") {
			case 1:
				want = strings.Replace(want, "%s", project.String(), 1)
			case 2:
				want = strings.Replace(strings.Replace(want, "%s", project.String(), 1), "%s", entity.String(), 1)
			}
			if got.Resource != want {
				t.Errorf("resource = %q, want %q", got.Resource, want)
			}
			if !registered[got.Action] {
				t.Errorf("action %q is not a registered built-in action", got.Action)
			}
			if _, _, p := iam.ParseResource(got.Resource); p != project.String() {
				t.Errorf("resource %q does not parse back to project %s", got.Resource, project)
			}
		})
	}
}

func TestAuthzFor_NotRealtimeAndOverride(t *testing.T) {
	project := uuid.New()
	if _, ok := events.AuthzFor(events.Event{Topic: "notification.created", ProjectID: project}); ok {
		t.Error("notification.created is routed to the recipient, not to a project room")
	}
	if _, ok := events.AuthzFor(events.Event{Topic: "task.updated"}); ok {
		t.Error("an event with no project has nothing to authorize against")
	}
	over := events.Authz{Action: "tasks:read", Resource: "project/x/task/y"}
	got, ok := events.AuthzFor(events.Event{Topic: "sprint.created", ProjectID: project, Authz: &over})
	if !ok || got != over {
		t.Errorf("explicit Authz not honoured: %+v %v", got, ok)
	}
}

type captureSink struct{ realtime []any }

func (c *captureSink) Publish(_ context.Context, channel string, payload any) error {
	if channel == events.ChannelRealtime {
		c.realtime = append(c.realtime, payload)
	}
	return nil
}

func (*captureSink) AppendFlat(context.Context, string, map[string]any) error { return nil }

func TestRealtimeEnvelopeCarriesAuthz(t *testing.T) {
	project, task := uuid.New(), uuid.New()
	sink := &captureSink{}
	events.FanoutTo(context.Background(), sink, events.Event{
		Topic: "task.updated", Payload: map[string]any{"id": task.String()},
		ProjectID: project, EntityType: events.EntityTask, EntityID: &task,
	})
	events.PublishRealtime(context.Background(), sink, events.Event{
		Topic: "notification.created", Payload: map[string]any{}, ProjectID: project,
	})
	if len(sink.realtime) != 2 {
		t.Fatalf("published %d realtime messages, want 2", len(sink.realtime))
	}
	first := sink.realtime[0].(map[string]any)
	want := events.Authz{Action: "tasks:read", Resource: "project/" + project.String() + "/task/" + task.String()}
	if first["authz"] != want {
		t.Errorf("authz = %+v, want %+v", first["authz"], want)
	}
	if _, has := sink.realtime[1].(map[string]any)["authz"]; has {
		t.Error("a recipient-routed notification must not carry a project authz")
	}
}

func TestNotifyIAMChanges_InvalidatesThenAnnounces(t *testing.T) {
	var order []string
	inner := invalidatorFunc(func(ids ...string) { order = append(order, "invalidate:"+strings.Join(ids, ",")) })
	sink := &captureSink{}
	events.NotifyIAMChanges(inner, sink).Invalidate("r1", "r2")

	if len(order) != 1 || order[0] != "invalidate:r1,r2" {
		t.Fatalf("authorizer not invalidated first: %v", order)
	}
	if len(sink.realtime) != 1 {
		t.Fatalf("published %d messages, want 1", len(sink.realtime))
	}
	msg := sink.realtime[0].(map[string]any)
	if msg["type"] != events.TopicIAMChanged {
		t.Errorf("type = %v, want %s", msg["type"], events.TopicIAMChanged)
	}
	if ids := msg["payload"].(map[string]any)["role_ids"].([]string); len(ids) != 2 {
		t.Errorf("role_ids = %v", ids)
	}
	if _, has := msg["authz"]; has {
		t.Error("iam.changed is a control message and must carry no authz")
	}

	if got := events.NotifyIAMChanges(inner, nil); got == nil {
		t.Error("a nil sink must return the inner invalidator")
	}
}

type invalidatorFunc func(...string)

func (f invalidatorFunc) Invalidate(ids ...string) { f(ids...) }
