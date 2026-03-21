// Package events defines domain event publishing abstractions.
package events

import "context"

// Publisher is the application-level contract for publishing domain events.
type Publisher interface {
	Publish(ctx context.Context, topic string, payload any) error
}
