// Package redis provides a Redis-backed token blacklist for JWT revocation.
package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const keyPrefix = "token:revoked:"

// TokenBlacklist stores revoked JWT IDs in Redis.
type TokenBlacklist struct {
	client *redis.Client
}

// NewTokenBlacklist returns a TokenBlacklist backed by the given Redis client.
func NewTokenBlacklist(client *redis.Client) *TokenBlacklist {
	return &TokenBlacklist{client: client}
}

// Revoke marks jti as revoked until ttl elapses.
func (b *TokenBlacklist) Revoke(ctx context.Context, jti string, ttl time.Duration) error {
	if err := b.client.Set(ctx, keyPrefix+jti, 1, ttl).Err(); err != nil {
		return fmt.Errorf("blacklist: revoke %q: %w", jti, err)
	}
	return nil
}

// IsRevoked returns true if jti has been revoked.
func (b *TokenBlacklist) IsRevoked(ctx context.Context, jti string) (bool, error) {
	err := b.client.Get(ctx, keyPrefix+jti).Err()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("blacklist: check %q: %w", jti, err)
	}
	return true, nil
}
