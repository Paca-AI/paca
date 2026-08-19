package userdom

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// PasswordSetToken is a single-use token that lets a user set their password
// via a link (e.g. one delivered by a plugin reacting to user.created)
// without ever transmitting the password itself. Only TokenHash is
// persisted — the raw token exists only transiently, in the caller that
// issues it.
type PasswordSetToken struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	TokenHash string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

// PasswordSetTokenRepository persists password-set tokens.
type PasswordSetTokenRepository interface {
	Create(ctx context.Context, t *PasswordSetToken) error
	// FindActiveByTokenHash returns the token row for hash if it exists, is
	// unused, and has not expired; otherwise ErrPasswordSetTokenInvalid.
	FindActiveByTokenHash(ctx context.Context, hash string) (*PasswordSetToken, error)
	// MarkUsed atomically claims the token for redemption, succeeding only
	// if it had not already been claimed. The bool reports whether this
	// call won the claim; false means another redemption already used it.
	MarkUsed(ctx context.Context, id uuid.UUID) (bool, error)
}
