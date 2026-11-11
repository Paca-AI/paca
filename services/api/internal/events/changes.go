package events

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/platform/messaging"
)

// MaxSnapshotBytes bounds Event.Snapshot in the change log; a larger one is
// left out and clients refetch the entity instead.
const MaxSnapshotBytes = 64 << 10

// ChangeSink is implemented by a sink that keeps the per-project change log
// (*messaging.Publisher). A sink without it simply has no log: Fanout and
// PublishRealtime behave exactly as they did before the log existed.
type ChangeSink interface {
	AppendChange(ctx context.Context, projectID string, fields map[string]any) (messaging.ChangeRef, error)
}

// changeOp names what an event did to its entity, derived from the topic's
// last segment ("task.updated" → "update"). A topic that fits none is
// "event": it is still logged, and readers fall back on the topic.
func changeOp(topic string) string {
	last := topic
	if i := strings.LastIndex(topic, "."); i >= 0 {
		last = topic[i+1:]
	}
	switch last {
	case "created", "added":
		return "create"
	case "deleted", "removed":
		return "delete"
	case "moved", "reordered", "task_moved":
		return "move"
	case "updated", "changed", "completed", "activated", "deactivated",
		"default_set", "role_changed", "status_changed", "applied", "assigned":
		return "update"
	}
	return "event"
}

// changeFields renders the event as a change-log entry: the stream fields
// the activity log already gets, plus an idempotency id, the operation, a
// timestamp and the authorization a reader must check before showing it.
// false when the event is not part of the log: it has no project, or no
// project-room delivery (AuthzFor), such as notifications and agent triggers.
func (e Event) changeFields() (map[string]any, bool) {
	authz, ok := AuthzFor(e)
	if !ok {
		return nil, false
	}
	fields, ok := e.streamFields()
	if !ok {
		return nil, false
	}
	az, err := json.Marshal(authz)
	if err != nil {
		return nil, false
	}
	if e.Snapshot != nil {
		if body, err := json.Marshal(e.Snapshot); err == nil && len(body) <= MaxSnapshotBytes {
			fields["entity"] = string(body)
		}
	}
	if e.ClientOpID != "" {
		fields["client_op_id"] = e.ClientOpID
	}
	if id := e.markedEntity(); id != "" {
		fields[messaging.MarkField] = id
		fields[messaging.MarkVersionField] = e.loggedVersion(fields)
	}
	fields["id"] = uuid.NewString()
	fields["op"] = changeOp(e.Topic)
	fields["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	fields["authz"] = string(az)
	return fields, true
}

// wholeEntityTopics are the topics that stand for the entity as a whole: the
// state after one of them is the state of the entity. Logging one is what
// tells the change reconciler a database change was announced; a comment or a
// link is not, because it says nothing about the task row itself.
var wholeEntityTopics = map[string]bool{
	"task.created": true, "task.updated": true, "task.deleted": true,
	"sprint.created": true, "sprint.updated": true, "sprint.completed": true, "sprint.deleted": true,
	"doc.created": true, "doc.updated": true, "doc.deleted": true, "doc.moved": true,
}

// markedEntity is the id of the entity a whole-entity event is about, or "".
func (e Event) markedEntity() string {
	if !wholeEntityTopics[e.Topic] {
		return ""
	}
	switch {
	case e.TaskID != nil && *e.TaskID != uuid.Nil:
		return e.TaskID.String()
	case e.EntityID != nil && *e.EntityID != uuid.Nil:
		return e.EntityID.String()
	}
	return ""
}

// loggedVersion is the row version the change's state has: that of its
// snapshot, DeletedVersion for a delete (nothing follows it), else unknown.
func (e Event) loggedVersion(fields map[string]any) int64 {
	if strings.HasSuffix(e.Topic, ".deleted") {
		return messaging.DeletedVersion
	}
	body, _ := fields["entity"].(string)
	if body == "" {
		return messaging.UnknownVersion
	}
	var v struct {
		Version *int64 `json:"version"`
	}
	if json.Unmarshal([]byte(body), &v) != nil || v.Version == nil {
		return messaging.UnknownVersion
	}
	return *v.Version
}

// recordChange appends e to its project's change log when the sink keeps one
// and returns what it was given. Failure is non-fatal, like every publish
// here: the legacy realtime message still goes out, without a seq.
func recordChange(ctx context.Context, sink any, e Event) (messaging.ChangeRef, bool) {
	cs, ok := sink.(ChangeSink)
	if !ok || e.ProjectID == uuid.Nil {
		return messaging.ChangeRef{}, false
	}
	fields, ok := e.changeFields()
	if !ok {
		return messaging.ChangeRef{}, false
	}
	ref, err := cs.AppendChange(ctx, e.ProjectID.String(), fields)
	if err != nil {
		return messaging.ChangeRef{}, false
	}
	return ref, true
}
