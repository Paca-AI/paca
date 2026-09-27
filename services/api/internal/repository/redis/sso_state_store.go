package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	ssodom "github.com/Paca-AI/api/internal/domain/sso"
)

const ssoStatePrefix = "sso:state:"

// SSOStateStore keeps each in-flight SSO sign-in attempt in Redis between
// the redirect to the provider and its callback.
type SSOStateStore struct {
	client *redis.Client
	ttl    time.Duration
}

// NewSSOStateStore returns an SSOStateStore whose attempts expire after ttl
// — how long a user may spend at the provider's sign-in page.
func NewSSOStateStore(client *redis.Client, ttl time.Duration) *SSOStateStore {
	return &SSOStateStore{client: client, ttl: ttl}
}

// Put stores a under state.
func (s *SSOStateStore) Put(ctx context.Context, state string, a *ssodom.AuthAttempt) error {
	b, err := json.Marshal(a)
	if err != nil {
		return fmt.Errorf("sso state: encode: %w", err)
	}
	if err := s.client.Set(ctx, ssoStatePrefix+state, b, s.ttl).Err(); err != nil {
		return fmt.Errorf("sso state: put: %w", err)
	}
	return nil
}

// Take atomically reads and deletes the attempt (GETDEL), so a callback URL
// can complete at most one sign-in. Returns nil, nil when absent/expired.
func (s *SSOStateStore) Take(ctx context.Context, state string) (*ssodom.AuthAttempt, error) {
	b, err := s.client.GetDel(ctx, ssoStatePrefix+state).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sso state: take: %w", err)
	}
	var a ssodom.AuthAttempt
	if err := json.Unmarshal(b, &a); err != nil {
		return nil, fmt.Errorf("sso state: decode: %w", err)
	}
	return &a, nil
}
