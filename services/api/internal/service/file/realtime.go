package filesvc

import (
	"context"

	"github.com/google/uuid"

	filedom "github.com/Paca-AI/api/internal/domain/file"
	"github.com/Paca-AI/api/internal/events"
)

// Changes announced as TopicFileChanged.
const (
	changeCreated  = "created"
	changeUpdated  = "updated"
	changeMoved    = "moved"
	changeTrashed  = "trashed"
	changeRestored = "restored"
	changeDeleted  = "deleted"
)

// realtimeSink is where file changes are announced to live clients.
type realtimeSink interface {
	Publish(ctx context.Context, channel string, payload any) error
}

// WithRealtime sets where file changes are announced. They are realtime-only
// (the activity log has no entry kind for them): another user browsing the
// same folder must follow them. Without a sink nothing is announced.
func (s *Service) WithRealtime(sink realtimeSink) *Service {
	s.realtime = sink
	return s
}

// announce tells live clients that listings of the scope changed. folders are
// the folders whose contents changed (nil is the scope root); a client shows
// one folder at a time, so it refetches only when its own folder is named.
// The payload holds ids only — no names — so it is safe to hand to anyone
// allowed to read the scope. A project's event is checked as files:read on the
// project (events.AuthzFor); the general scope is routed by services/realtime.
func (s *Service) announce(ctx context.Context, scope filedom.Scope, change string, n *filedom.Node, folders ...*uuid.UUID) {
	if s.realtime == nil || n == nil {
		return
	}
	payload := map[string]any{
		"change":  change,
		"scope":   "general",
		"node_id": n.ID.String(),
		"kind":    string(n.Kind),
	}
	// root marks the scope's top level, whose listing has no folder id.
	ids := make([]string, 0, len(folders))
	root := false
	seen := make(map[uuid.UUID]struct{}, len(folders))
	for _, f := range folders {
		if f == nil {
			root = true
			continue
		}
		if _, dup := seen[*f]; dup {
			continue
		}
		seen[*f] = struct{}{}
		ids = append(ids, f.String())
	}
	payload["folder_ids"] = ids
	payload["root"] = root

	e := events.Event{Topic: events.TopicFileChanged, Payload: payload, SkipPluginEvents: true}
	if scope.ProjectID != nil {
		payload["scope"] = "project"
		payload["project_id"] = scope.ProjectID.String()
		e.ProjectID = *scope.ProjectID
	}
	id := n.ID
	e.EntityID = &id
	events.PublishRealtime(ctx, s.realtime, e)
}
