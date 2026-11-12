package events

import "context"

// CollabSocketHeader is the request header a browser stamps on its mutations
// with the id of its realtime socket. The collaborative-editing hub uses it to
// recognise a save made by an editor of a live room (its own content coming
// back, which must not be merged again) and tell it apart from a write by
// anyone else, including an agent using the same account's API key, which
// must be merged. It travels on the realtime envelope only (collab_socket):
// it is not part of the event's payload, the activity log or the change log.
const CollabSocketHeader = "X-Collab-Socket"

type collabSocketKey struct{}

// WithCollabSocket returns ctx carrying the request's realtime socket id, so
// the realtime messages the request produces are stamped with it. An invalid
// id is dropped: like a client op id, it can only ever suppress an echo.
func WithCollabSocket(ctx context.Context, id string) context.Context {
	if !ValidClientOp(id) {
		return ctx
	}
	return context.WithValue(ctx, collabSocketKey{}, id)
}

// CollabSocket returns the realtime socket id carried by ctx, or "".
func CollabSocket(ctx context.Context) string {
	id, _ := ctx.Value(collabSocketKey{}).(string)
	return id
}
