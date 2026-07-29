// Package apikeysvc implements services for API key operations.
package apikeysvc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	apikeydom "github.com/Paca-AI/api/internal/domain/apikey"
)

const (
	keyPrefix      = "paca_"
	rawKeyHexLen   = 64 // 32 random bytes → 64 hex chars
	displayPrefLen = 8  // first N hex chars stored for display
	maxNameLen     = 100
)

// Service is the concrete implementation of apikeydom.Service.
type Service struct {
	repo           apikeydom.Repository
	agentKeyHash   [32]byte  // SHA-256 of the configured static agent API key
	agentKeySet    bool      // true when an agent API key has been configured
	agentBotUserID uuid.UUID // user identity returned for the static agent key
}

// New returns a configured API key Service.
func New(repo apikeydom.Repository) *Service {
	return &Service{repo: repo}
}

// WithAgentKey configures a static pre-shared key for the AI agent service.
// When rawKey is presented to Authenticate it is validated in constant time
// without a database lookup, and the request is authenticated as agentUserID.
func (s *Service) WithAgentKey(rawKey string, agentUserID uuid.UUID) *Service {
	s.agentKeyHash = sha256.Sum256([]byte(rawKey))
	s.agentBotUserID = agentUserID
	s.agentKeySet = true
	return s
}

// List returns all non-revoked API keys for the given user.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]*apikeydom.APIKey, error) {
	return s.repo.ListByUserID(ctx, userID)
}

// Create generates a cryptographically random API key, stores its SHA-256
// hash, and returns the key record together with the raw key string.
// The raw key is returned ONLY here and is never persisted.
func (s *Service) Create(ctx context.Context, in apikeydom.CreateInput) (*apikeydom.APIKey, string, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, "", apikeydom.ErrNameInvalid
	}
	if len(name) > maxNameLen {
		return nil, "", apikeydom.ErrNameTooLong
	}

	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		return nil, "", fmt.Errorf("api key svc: generate key: %w", err)
	}
	rawHex := hex.EncodeToString(rawBytes)
	rawKey := keyPrefix + rawHex

	hash := sha256.Sum256([]byte(rawKey))
	keyHash := hex.EncodeToString(hash[:])

	key := &apikeydom.APIKey{
		ID:        uuid.New(),
		UserID:    in.UserID,
		Name:      name,
		KeyPrefix: rawHex[:displayPrefLen],
		ExpiresAt: in.ExpiresAt,
		CreatedAt: time.Now().UTC(),
	}

	if err := s.repo.Create(ctx, key, keyHash); err != nil {
		return nil, "", err
	}
	return key, rawKey, nil
}

// Revoke revokes an API key. Only the owning user may revoke their own key.
func (s *Service) Revoke(ctx context.Context, userID, keyID uuid.UUID) error {
	key, err := s.repo.FindByID(ctx, keyID)
	if err != nil {
		return err
	}
	if key.UserID != userID {
		return apikeydom.ErrForbidden
	}
	return s.repo.Revoke(ctx, keyID)
}

// Authenticate validates a raw API key and returns the matching record.
// If a static agent API key has been configured via WithAgentKey, it is
// checked first in constant time without a database lookup.
// For all other keys a SHA-256 hash lookup is performed against the database.
// A best-effort update of last_used_at is performed; any update error is
// ignored so authentication does not fail due to last_used_at persistence.
func (s *Service) Authenticate(ctx context.Context, rawKey string) (*apikeydom.APIKey, error) {
	// Check static agent key first — constant-time comparison to prevent
	// timing attacks.
	if s.agentKeySet {
		h := sha256.Sum256([]byte(rawKey))
		if subtle.ConstantTimeCompare(h[:], s.agentKeyHash[:]) == 1 {
			return &apikeydom.APIKey{UserID: s.agentBotUserID}, nil
		}
	}

	hash := sha256.Sum256([]byte(rawKey))
	keyHash := hex.EncodeToString(hash[:])

	key, err := s.repo.FindByHash(ctx, keyHash)
	if err != nil {
		return nil, err
	}

	if key.RevokedAt != nil {
		return nil, apikeydom.ErrRevoked
	}
	if key.ExpiresAt != nil && time.Now().After(*key.ExpiresAt) {
		return nil, apikeydom.ErrExpired
	}

	// Best-effort last_used_at update — ignore errors.
	_ = s.repo.UpdateLastUsed(ctx, key.ID, time.Now().UTC())

	return key, nil
}

// IsAgentKey checks if the provided raw key is the static agent API key.
// This is used to validate that X-Agent-ID header can only be used with the agent API key.
func (s *Service) IsAgentKey(_ context.Context, rawKey string) bool {
	if !s.agentKeySet {
		return false
	}
	h := sha256.Sum256([]byte(rawKey))
	return subtle.ConstantTimeCompare(h[:], s.agentKeyHash[:]) == 1
}
