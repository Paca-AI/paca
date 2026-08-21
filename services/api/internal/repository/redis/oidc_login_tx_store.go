package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const oidcLoginTxPrefix = "oidc:login-tx:"

// OIDCLoginTxStore persists OIDC login transactions (an opaque JSON payload
// holding the nonce and PKCE verifier) in Redis/Valkey, keyed by the random
// state value. Save overwrites any previous value for the same key; Consume
// is single-use (GETDEL), and every key carries a TTL so abandoned logins
// never leak state.
type OIDCLoginTxStore struct {
	client *redis.Client
}

// NewOIDCLoginTxStore returns an OIDCLoginTxStore backed by the given client.
func NewOIDCLoginTxStore(client *redis.Client) *OIDCLoginTxStore {
	return &OIDCLoginTxStore{client: client}
}

// Save stores payload under state with the given TTL.
func (s *OIDCLoginTxStore) Save(ctx context.Context, state string, payload []byte, ttl time.Duration) error {
	if state == "" {
		return fmt.Errorf("oidc login tx store: empty state")
	}
	if err := s.client.Set(ctx, oidcLoginTxPrefix+state, payload, ttl).Err(); err != nil {
		return fmt.Errorf("oidc login tx store: save: %w", err)
	}
	return nil
}

// Consume atomically deletes and returns the payload stored under state.
// A nil payload with nil error means no live transaction exists — unknown,
// expired, and already-used states are deliberately indistinguishable. A
// second call with the same state therefore also returns (nil, nil), which
// is what makes replaying a stolen callback URL useless.
func (s *OIDCLoginTxStore) Consume(ctx context.Context, state string) ([]byte, error) {
	payload, err := s.client.GetDel(ctx, oidcLoginTxPrefix+state).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("oidc login tx store: consume: %w", err)
	}
	return payload, nil
}
