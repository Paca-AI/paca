// Package apikeydom provides domain entities for API key management.
package apikeydom

import (
	"time"

	"github.com/google/uuid"
)

// APIKey represents a user-created API key used for programmatic authentication.
// The raw key value is never stored; only key_hash (SHA-256 hex) and key_prefix
// (first 8 hex chars) are persisted.
type APIKey struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	Name       string
	KeyPrefix  string
	LastUsedAt *time.Time
	ExpiresAt  *time.Time
	CreatedAt  time.Time
	RevokedAt  *time.Time
	// AgentID is set only for the synthetic record Authenticate returns when
	// the presented key is a specific agent's own MCP API key (as opposed
	// to a human's personal key, or the shared static agent key) — never
	// persisted in the api_keys table. Its presence tells the authn
	// middleware which agent the key already proves identity for, with no
	// separate X-Agent-ID header claim needed or trusted.
	AgentID *uuid.UUID
}

// IsActive reports whether the key can be used for authentication.
func (k *APIKey) IsActive() bool {
	if k.RevokedAt != nil {
		return false
	}
	if k.ExpiresAt != nil && time.Now().After(*k.ExpiresAt) {
		return false
	}
	return true
}
