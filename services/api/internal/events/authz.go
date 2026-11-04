package events

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

// Authz says what IAM must allow before a subscriber may see an event: the
// action and the resource it is checked on, in the same terms a route gate
// uses. It is metadata about the event, not a decision — services/realtime
// asks the API whether a given account holds Action on Resource, so the
// answer always comes from the IAM engine and honours entity-level Deny
// statements and conditions.
type Authz struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
}

// AuthzFor returns the authorization an event needs before it is shown to a
// client, or false when realtime has no project-scoped delivery for the topic
// (identity-routed events such as notification.*, agent.* conversation
// events published by services/ai-agent, and topics only the activity log
// reads). An explicit Event.Authz wins over the derived one.
//
// The topic prefix picks the action (the same permission the matching REST
// read routes are gated on, see router.go) and the entity picks the resource:
// an event about one task is checked on project/<p>/task/<id>, so a Deny on
// that task applies. An event with no single subject, or about an entity IAM
// has no resource kind for (doc folders, automation graph edits that name no
// automation), is checked on the project, which is what a collection-level
// route does.
func AuthzFor(e Event) (Authz, bool) {
	if e.Authz != nil {
		return *e.Authz, true
	}
	if e.ProjectID == uuid.Nil {
		return Authz{}, false
	}
	project := "project/" + e.ProjectID.String()
	// child is the entity's resource when the event is about one entity of
	// the expected type, otherwise the project.
	child := func(kind string, want EntityType, id *uuid.UUID) string {
		if e.EntityType != want || id == nil || *id == uuid.Nil {
			return project
		}
		return project + "/" + kind + "/" + id.String()
	}
	task := func() string {
		if e.TaskID != nil && *e.TaskID != uuid.Nil {
			return project + "/task/" + e.TaskID.String()
		}
		return child("task", EntityTask, e.EntityID)
	}

	t := e.Topic
	switch {
	case t == "agent.session.started":
		// A task activity ("an agent began a session on this task") whose
		// payload names the conversation; what it shows is the conversation's,
		// so it is checked on the conversation, not the task.
		if id := payloadString(e.Payload, "conversation_id"); id != "" {
			return Authz{string(iam.ActionConversationsRead), project + "/conversation/" + id}, true
		}
		return Authz{}, false
	case t == "agent.acp_bridge.status":
		// No conversation content; a project-wide read like the task boards
		// it refreshes.
		return Authz{string(iam.ActionTasksRead), project}, true
	case strings.HasPrefix(t, "task."), strings.HasPrefix(t, "git."),
		t == "workflow.assigned", t == "automation.applied":
		return Authz{string(iam.ActionTasksRead), task()}, true
	case strings.HasPrefix(t, "doc.folder."):
		return Authz{string(iam.ActionDocsRead), project}, true
	case strings.HasPrefix(t, "doc."):
		return Authz{string(iam.ActionDocsRead), child("doc", EntityDoc, e.EntityID)}, true
	case strings.HasPrefix(t, "workflow."), strings.HasPrefix(t, "automation."):
		return Authz{string(iam.ActionWorkflowsRead), child("workflow", EntityAutomation, e.EntityID)}, true
	case strings.HasPrefix(t, "sprint."):
		return Authz{string(iam.ActionSprintsRead), child("sprint", EntitySprint, e.EntityID)}, true
	case strings.HasPrefix(t, "view."):
		// Views have their own action and resource kind: the view routes are
		// gated on views:read, not sprints:read.
		return Authz{string(iam.ActionViewsRead), child("view", EntityView, e.EntityID)}, true
	case strings.HasPrefix(t, "environment."):
		return Authz{string(iam.ActionEnvironmentsRead), child("environment", EntityEnvironment, e.EntityID)}, true
	}
	return Authz{}, false
}

// realtimeSink is the part of the publisher PublishRealtime needs.
type realtimeSink interface {
	Publish(ctx context.Context, channel string, payload any) error
}

// PublishRealtime sends an event to ChannelRealtime only, for the events that
// are deliberately not part of the activity log (view reorders, automation
// node drags, environment status changes). It builds the same envelope as
// Fanout, so every realtime message carries its authorization the same way.
// Errors are swallowed for the reason given on Fanout.
func PublishRealtime(ctx context.Context, sink realtimeSink, e Event) {
	if sink == nil {
		return
	}
	_ = sink.Publish(ctx, ChannelRealtime, realtimeMessage(e))
}

// realtimeMessage is the ChannelRealtime envelope: "type" and "payload" as
// they have always been, plus "authz" when the topic has one.
func realtimeMessage(e Event) map[string]any {
	msg := map[string]any{
		"type":    e.Topic,
		"payload": e.Payload,
	}
	if a, ok := AuthzFor(e); ok {
		msg["authz"] = a
	}
	return msg
}

// payloadString reads one string field of an event payload that is a map or
// any JSON-marshalable struct; "" when absent or not a string.
func payloadString(payload any, key string) string {
	if m, ok := payload.(map[string]any); ok {
		v, _ := m[key].(string)
		return v
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}
