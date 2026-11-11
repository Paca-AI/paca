package events

import (
	"context"
	"time"
)

// PolicyInvalidator is what the services call after a role or an attachment
// has changed. *iam.Authorizer implements it.
type PolicyInvalidator interface {
	Invalidate(roleIDs ...string)
}

// notifyTimeout bounds the publish; Invalidate has no request context and a
// slow broker must not hold up the service that changed the role.
const notifyTimeout = 2 * time.Second

type iamChangeNotifier struct {
	next PolicyInvalidator
	sink realtimeSink
}

// NotifyIAMChanges wraps the authorizer's invalidator so that every committed
// change to a role or its attachments is also announced on ChannelRealtime as
// TopicIAMChanged. services/realtime drops what it cached about who may see
// what and re-checks its open sockets, so a revoked grant stops delivering at
// the next event instead of when the socket next joins a room.
//
// The authorizer is invalidated first: once the announcement arrives, a check
// asked of the API sees the new policy. The announcement names the roles that
// changed; it says nothing about who holds them, and subscribers must treat
// it as "recheck everyone". A nil sink returns next unchanged.
func NotifyIAMChanges(next PolicyInvalidator, sink realtimeSink) PolicyInvalidator {
	if sink == nil {
		return next
	}
	return iamChangeNotifier{next: next, sink: sink}
}

// epochSink is the part of the publisher that keeps the IAM epoch.
type epochSink interface {
	IncrCounter(ctx context.Context, key string) (int64, error)
}

func (n iamChangeNotifier) Invalidate(roleIDs ...string) {
	n.next.Invalidate(roleIDs...)
	ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
	defer cancel()
	if roleIDs == nil {
		roleIDs = []string{}
	}
	msg := map[string]any{
		"type":    TopicIAMChanged,
		"payload": map[string]any{"role_ids": roleIDs},
	}
	// The epoch goes up before the announcement, so a subscriber that sees the
	// message sees the new value, and one that missed it still sees the value.
	if es, ok := n.sink.(epochSink); ok {
		if epoch, err := es.IncrCounter(ctx, IAMEpochKey); err == nil {
			msg["epoch"] = epoch
		}
	}
	_ = n.sink.Publish(ctx, ChannelRealtime, msg)
}
