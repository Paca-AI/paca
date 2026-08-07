package apikeysvc_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	apikeydom "github.com/Paca-AI/api/internal/domain/apikey"
	apikeysvc "github.com/Paca-AI/api/internal/service/apikey"
)

// ---------------------------------------------------------------------------
// stub repository
// ---------------------------------------------------------------------------

type stubRepo struct {
	findByID       func(ctx context.Context, id uuid.UUID) (*apikeydom.APIKey, error)
	findByHash     func(ctx context.Context, hash string) (*apikeydom.APIKey, error)
	listByUserID   func(ctx context.Context, userID uuid.UUID) ([]*apikeydom.APIKey, error)
	create         func(ctx context.Context, key *apikeydom.APIKey, keyHash string) error
	revoke         func(ctx context.Context, id uuid.UUID) error
	updateLastUsed func(ctx context.Context, id uuid.UUID, at time.Time) error
}

func (r *stubRepo) FindByID(ctx context.Context, id uuid.UUID) (*apikeydom.APIKey, error) {
	if r.findByID != nil {
		return r.findByID(ctx, id)
	}
	return nil, apikeydom.ErrNotFound
}
func (r *stubRepo) FindByHash(ctx context.Context, hash string) (*apikeydom.APIKey, error) {
	if r.findByHash != nil {
		return r.findByHash(ctx, hash)
	}
	return nil, apikeydom.ErrNotFound
}
func (r *stubRepo) ListByUserID(ctx context.Context, userID uuid.UUID) ([]*apikeydom.APIKey, error) {
	if r.listByUserID != nil {
		return r.listByUserID(ctx, userID)
	}
	return nil, nil
}
func (r *stubRepo) Create(ctx context.Context, key *apikeydom.APIKey, keyHash string) error {
	if r.create != nil {
		return r.create(ctx, key, keyHash)
	}
	return nil
}
func (r *stubRepo) Revoke(ctx context.Context, id uuid.UUID) error {
	if r.revoke != nil {
		return r.revoke(ctx, id)
	}
	return nil
}
func (r *stubRepo) UpdateLastUsed(ctx context.Context, id uuid.UUID, at time.Time) error {
	if r.updateLastUsed != nil {
		return r.updateLastUsed(ctx, id, at)
	}
	return nil
}

// verify *apikeysvc.Service satisfies the domain interface.
var _ apikeydom.Service = (*apikeysvc.Service)(nil)

// ---------------------------------------------------------------------------
// Create
// ---------------------------------------------------------------------------

func TestCreate_GeneratesKey(t *testing.T) {
	svc := apikeysvc.New(&stubRepo{})

	key, rawKey, err := svc.Create(context.Background(), apikeydom.CreateInput{
		UserID: uuid.New(),
		Name:   "CI token",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key == nil {
		t.Fatal("expected non-nil key")
	}
	if !strings.HasPrefix(rawKey, "paca_") {
		t.Errorf("raw key should start with 'paca_', got %q", rawKey[:10])
	}
	if len(rawKey) != len("paca_")+64 {
		t.Errorf("raw key length: want %d, got %d", len("paca_")+64, len(rawKey))
	}
	if key.KeyPrefix != rawKey[len("paca_"):len("paca_")+8] {
		t.Errorf("key prefix mismatch: stored %q, want %q", key.KeyPrefix, rawKey[5:13])
	}
}

func TestCreate_EmptyNameReturnsError(t *testing.T) {
	svc := apikeysvc.New(&stubRepo{})

	_, _, err := svc.Create(context.Background(), apikeydom.CreateInput{
		UserID: uuid.New(),
		Name:   "   ",
	})
	if !errors.Is(err, apikeydom.ErrNameInvalid) {
		t.Errorf("expected ErrNameInvalid, got %v", err)
	}
}

func TestCreate_NameTooLongReturnsError(t *testing.T) {
	svc := apikeysvc.New(&stubRepo{})

	_, _, err := svc.Create(context.Background(), apikeydom.CreateInput{
		UserID: uuid.New(),
		Name:   strings.Repeat("a", 101),
	})
	if !errors.Is(err, apikeydom.ErrNameTooLong) {
		t.Errorf("expected ErrNameTooLong, got %v", err)
	}
}

func TestCreate_PropagatesRepoError(t *testing.T) {
	repoErr := errors.New("db error")
	svc := apikeysvc.New(&stubRepo{
		create: func(_ context.Context, _ *apikeydom.APIKey, _ string) error {
			return repoErr
		},
	})

	_, _, err := svc.Create(context.Background(), apikeydom.CreateInput{
		UserID: uuid.New(),
		Name:   "token",
	})
	if !errors.Is(err, repoErr) {
		t.Errorf("expected repo error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Revoke
// ---------------------------------------------------------------------------

func TestRevoke_OwnerCanRevoke(t *testing.T) {
	userID := uuid.New()
	keyID := uuid.New()

	var revokedID uuid.UUID
	svc := apikeysvc.New(&stubRepo{
		findByID: func(_ context.Context, id uuid.UUID) (*apikeydom.APIKey, error) {
			return &apikeydom.APIKey{ID: id, UserID: userID}, nil
		},
		revoke: func(_ context.Context, id uuid.UUID) error {
			revokedID = id
			return nil
		},
	})

	if err := svc.Revoke(context.Background(), userID, keyID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if revokedID != keyID {
		t.Errorf("expected key %v to be revoked, got %v", keyID, revokedID)
	}
}

func TestRevoke_NonOwnerForbidden(t *testing.T) {
	ownerID := uuid.New()
	callerID := uuid.New()
	keyID := uuid.New()

	svc := apikeysvc.New(&stubRepo{
		findByID: func(_ context.Context, id uuid.UUID) (*apikeydom.APIKey, error) {
			return &apikeydom.APIKey{ID: id, UserID: ownerID}, nil
		},
	})

	err := svc.Revoke(context.Background(), callerID, keyID)
	if !errors.Is(err, apikeydom.ErrForbidden) {
		t.Errorf("expected ErrForbidden, got %v", err)
	}
}

func TestRevoke_NotFound(t *testing.T) {
	svc := apikeysvc.New(&stubRepo{})
	err := svc.Revoke(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, apikeydom.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Authenticate
// ---------------------------------------------------------------------------

func TestAuthenticate_ValidKey(t *testing.T) {
	// Create a key so we know the hash.
	var capturedHash string
	storedKey := &apikeydom.APIKey{ID: uuid.New(), UserID: uuid.New(), Name: "test"}

	svc := apikeysvc.New(&stubRepo{
		create: func(_ context.Context, key *apikeydom.APIKey, keyHash string) error {
			capturedHash = keyHash
			*storedKey = *key
			return nil
		},
		findByHash: func(_ context.Context, hash string) (*apikeydom.APIKey, error) {
			if hash == capturedHash {
				return storedKey, nil
			}
			return nil, apikeydom.ErrNotFound
		},
	})

	_, rawKey, err := svc.Create(context.Background(), apikeydom.CreateInput{
		UserID: storedKey.UserID,
		Name:   "test",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	result, err := svc.Authenticate(context.Background(), rawKey)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if result.ID != storedKey.ID {
		t.Errorf("expected key ID %v, got %v", storedKey.ID, result.ID)
	}
}

func TestAuthenticate_RevokedKey(t *testing.T) {
	now := time.Now()
	svc := apikeysvc.New(&stubRepo{
		findByHash: func(_ context.Context, _ string) (*apikeydom.APIKey, error) {
			return &apikeydom.APIKey{RevokedAt: &now}, nil
		},
	})
	_, err := svc.Authenticate(context.Background(), "paca_"+"a"+strings.Repeat("b", 63))
	if !errors.Is(err, apikeydom.ErrRevoked) {
		t.Errorf("expected ErrRevoked, got %v", err)
	}
}

func TestAuthenticate_ExpiredKey(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	svc := apikeysvc.New(&stubRepo{
		findByHash: func(_ context.Context, _ string) (*apikeydom.APIKey, error) {
			return &apikeydom.APIKey{ExpiresAt: &past}, nil
		},
	})
	_, err := svc.Authenticate(context.Background(), "paca_"+"a"+strings.Repeat("b", 63))
	if !errors.Is(err, apikeydom.ErrExpired) {
		t.Errorf("expected ErrExpired, got %v", err)
	}
}

func TestAuthenticate_UnknownKey(t *testing.T) {
	svc := apikeysvc.New(&stubRepo{})
	_, err := svc.Authenticate(context.Background(), "paca_"+"a"+strings.Repeat("b", 63))
	if !errors.Is(err, apikeydom.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// stubAgentIdentityStore satisfies apikeysvc.AgentIdentityStore for the
// per-agent MCP key fallback tests below.
type stubAgentIdentityStore struct {
	findAgentByMCPAPIKeyHash func(ctx context.Context, hash string) (*agentdom.Agent, error)
}

func (s *stubAgentIdentityStore) FindAgentByID(_ context.Context, agentID uuid.UUID) (*agentdom.Agent, error) {
	return &agentdom.Agent{ID: agentID}, nil
}

func (s *stubAgentIdentityStore) HasActiveGlobalChatSession(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return true, nil
}

func (s *stubAgentIdentityStore) FindAgentByMCPAPIKeyHash(ctx context.Context, hash string) (*agentdom.Agent, error) {
	if s.findAgentByMCPAPIKeyHash != nil {
		return s.findAgentByMCPAPIKeyHash(ctx, hash)
	}
	return nil, agentdom.ErrAgentNotFound
}

func TestAuthenticate_AgentMCPKey_ResolvesAgentID(t *testing.T) {
	agentID := uuid.New()
	svc := apikeysvc.New(&stubRepo{}).
		WithAgentIdentityStore(&stubAgentIdentityStore{
			findAgentByMCPAPIKeyHash: func(context.Context, string) (*agentdom.Agent, error) {
				return &agentdom.Agent{ID: agentID}, nil
			},
		})

	result, err := svc.Authenticate(context.Background(), "some-agent-mcp-key")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if result.AgentID == nil || *result.AgentID != agentID {
		t.Fatalf("expected AgentID %v, got %v", agentID, result.AgentID)
	}
}

func TestAuthenticate_PersonalKeyTakesPrecedenceOverAgentStore(t *testing.T) {
	// A hash that matches a personal key must never fall through to the
	// agent-identity store — the two lookups only differ in what an
	// attacker-unknown raw key happens to hash to.
	personalKey := &apikeydom.APIKey{ID: uuid.New(), UserID: uuid.New()}
	agentStoreCalled := false
	svc := apikeysvc.New(&stubRepo{
		findByHash: func(context.Context, string) (*apikeydom.APIKey, error) {
			return personalKey, nil
		},
	}).WithAgentIdentityStore(&stubAgentIdentityStore{
		findAgentByMCPAPIKeyHash: func(context.Context, string) (*agentdom.Agent, error) {
			agentStoreCalled = true
			return &agentdom.Agent{ID: uuid.New()}, nil
		},
	})

	result, err := svc.Authenticate(context.Background(), "paca_"+"a"+strings.Repeat("b", 63))
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if result.AgentID != nil {
		t.Errorf("expected no AgentID for a personal key match, got %v", result.AgentID)
	}
	if agentStoreCalled {
		t.Error("agent identity store should not be consulted once a personal key matches")
	}
}

func TestAuthenticate_UnknownKey_AgentStoreConfigured(t *testing.T) {
	// A key that matches neither a personal key nor any agent's MCP key
	// must still fail closed with ErrNotFound.
	svc := apikeysvc.New(&stubRepo{}).WithAgentIdentityStore(&stubAgentIdentityStore{})
	_, err := svc.Authenticate(context.Background(), "paca_"+"a"+strings.Repeat("b", 63))
	if !errors.Is(err, apikeydom.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// TestAuthenticate_AgentStoreDBError_Propagates covers a transient failure
// in the MCP-key fallback lookup (e.g. the database is unreachable): it must
// surface as-is, not be masked as an ordinary "invalid API key" ErrNotFound —
// otherwise an infrastructure outage looks identical to a bad credential.
func TestAuthenticate_AgentStoreDBError_Propagates(t *testing.T) {
	dbErr := errors.New("connection refused")
	svc := apikeysvc.New(&stubRepo{}).WithAgentIdentityStore(&stubAgentIdentityStore{
		findAgentByMCPAPIKeyHash: func(context.Context, string) (*agentdom.Agent, error) {
			return nil, dbErr
		},
	})

	_, err := svc.Authenticate(context.Background(), "paca_"+"a"+strings.Repeat("b", 63))
	if !errors.Is(err, dbErr) {
		t.Errorf("expected the underlying DB error to propagate, got %v", err)
	}
	if errors.Is(err, apikeydom.ErrNotFound) {
		t.Error("a transient lookup failure must not be reported as ErrNotFound")
	}
}
