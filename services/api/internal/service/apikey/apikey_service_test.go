package apikeysvc_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

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
