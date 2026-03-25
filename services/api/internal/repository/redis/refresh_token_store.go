// Package redis provides Redis-backed implementations for token management.
package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	refreshUsedPrefix   = "refresh:used:"
	refreshFamilyPrefix = "refresh:family:"
)

// RefreshTokenStore manages refresh-token rotation state in Redis.
// It tracks per-token first-use timestamps and maintains per-family revocation
// flags, enabling reuse detection with a configurable grace period.
type RefreshTokenStore struct {
	client *redis.Client
}

// NewRefreshTokenStore returns a RefreshTokenStore backed by the given Redis client.
func NewRefreshTokenStore(client *redis.Client) *RefreshTokenStore {
	return &RefreshTokenStore{client: client}
}

// RecordFirstUse atomically marks jti as used and returns nil for the first
// caller.  Subsequent callers receive a pointer to the time of the first use,
// allowing the service to enforce the grace period before revoking the family.
//
// The key expires after ttl to keep Redis memory bounded.
func (s *RefreshTokenStore) RecordFirstUse(ctx context.Context, jti string, ttl time.Duration) (*time.Time, error) {
	key := refreshUsedPrefix + jti
	nowUnix := time.Now().UnixMilli()

	// SET key value PX ms NX — only sets if key does not exist.
	ok, err := s.client.SetArgs(ctx, key, nowUnix, redis.SetArgs{
		Mode: "NX",
		TTL:  ttl,
	}).Result()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("refresh store: record use %q: %w", jti, err)
	}
	if ok == "OK" {
		// First use — key was just created.
		return nil, nil
	}

	// Key already existed — retrieve the original timestamp.
	val, err := s.client.Get(ctx, key).Int64()
	if err != nil {
		return nil, fmt.Errorf("refresh store: get use time %q: %w", jti, err)
	}
	t := time.UnixMilli(val)
	return &t, nil
}

// RevokeFamily marks the entire token family as revoked until ttl elapses.
// All subsequent refresh attempts for any token in the family will be rejected.
func (s *RefreshTokenStore) RevokeFamily(ctx context.Context, familyID string, ttl time.Duration) error {
	key := refreshFamilyPrefix + familyID + ":revoked"
	if err := s.client.Set(ctx, key, 1, ttl).Err(); err != nil {
		return fmt.Errorf("refresh store: revoke family %q: %w", familyID, err)
	}
	return nil
}

// IsFamilyRevoked returns true when the family has been revoked.
func (s *RefreshTokenStore) IsFamilyRevoked(ctx context.Context, familyID string) (bool, error) {
	key := refreshFamilyPrefix + familyID + ":revoked"
	err := s.client.Get(ctx, key).Err()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("refresh store: check family %q: %w", familyID, err)
	}
	return true, nil
}
