package galaxyauth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	globalroledom "github.com/Paca-AI/api/internal/domain/globalrole"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// in-memory fakes
// ---------------------------------------------------------------------------

type memUserStore struct {
	users   []*userdom.User
	oidcSub map[uuid.UUID]string
	emails  map[uuid.UUID]string
}

func newMemUserStore() *memUserStore {
	return &memUserStore{oidcSub: map[uuid.UUID]string{}, emails: map[uuid.UUID]string{}}
}

func (m *memUserStore) FindByOIDCSub(_ context.Context, sub string) (*userdom.User, error) {
	for _, u := range m.users {
		if m.oidcSub[u.ID] == sub {
			return u, nil
		}
	}
	return nil, userdom.ErrNotFound
}

func (m *memUserStore) FindByEmail(_ context.Context, email string) (*userdom.User, error) {
	for _, u := range m.users {
		if m.emails[u.ID] == email {
			return u, nil
		}
	}
	return nil, userdom.ErrNotFound
}

func (m *memUserStore) FindByUsername(_ context.Context, username string) (*userdom.User, error) {
	for _, u := range m.users {
		if u.Username == username {
			return u, nil
		}
	}
	return nil, userdom.ErrNotFound
}

func (m *memUserStore) Create(_ context.Context, u *userdom.User) error {
	m.users = append(m.users, u)
	return nil
}

func (m *memUserStore) LinkOIDC(_ context.Context, userID uuid.UUID, sub, email string) error {
	m.oidcSub[userID] = sub
	if email != "" {
		if _, ok := m.emails[userID]; !ok {
			m.emails[userID] = email
		}
	}
	return nil
}

type memRoleFinder struct {
	roles map[string]*globalroledom.GlobalRole
}

func (m *memRoleFinder) FindByName(_ context.Context, name string) (*globalroledom.GlobalRole, error) {
	if r, ok := m.roles[name]; ok {
		return r, nil
	}
	return nil, globalroledom.ErrNotFound
}

func newTestService(store *memUserStore, autoCreate bool) *Service {
	roles := &memRoleFinder{roles: map[string]*globalroledom.GlobalRole{
		"USER": {ID: uuid.New(), Name: "USER", CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}}
	return New(store, roles, autoCreate, "USER", slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func TestResolveOIDCUserPrefersLinkedSubject(t *testing.T) {
	store := newMemUserStore()
	existing := &userdom.User{ID: uuid.New(), Username: "cao"}
	store.users = append(store.users, existing)
	store.oidcSub[existing.ID] = "sub-1"

	svc := newTestService(store, true)
	u, err := svc.ResolveOIDCUser(context.Background(), Identity{Subject: "sub-1", Email: "other@example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.ID != existing.ID {
		t.Fatalf("expected linked user, got %s", u.Username)
	}
}

func TestResolveOIDCUserLinksByEmail(t *testing.T) {
	store := newMemUserStore()
	existing := &userdom.User{ID: uuid.New(), Username: "cao"}
	store.users = append(store.users, existing)
	store.emails[existing.ID] = "cao@example.com"

	svc := newTestService(store, true)
	u, err := svc.ResolveOIDCUser(context.Background(), Identity{Subject: "sub-9", Email: "cao@example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.ID != existing.ID {
		t.Fatal("expected existing user matched by email")
	}
	if store.oidcSub[existing.ID] != "sub-9" {
		t.Fatal("expected oidc_sub to be linked onto the email-matched user")
	}
	if len(store.users) != 1 {
		t.Fatal("no new user should be created when email matches")
	}
}

func TestResolveOIDCUserJITCreatesWithUniqueUsername(t *testing.T) {
	store := newMemUserStore()
	store.users = append(store.users, &userdom.User{ID: uuid.New(), Username: "cao.phan"})

	svc := newTestService(store, true)
	u, err := svc.ResolveOIDCUser(context.Background(), Identity{
		Subject:           "sub-42",
		Email:             "cao.phan@example.com",
		Name:              "Cao Phan",
		PreferredUsername: "Cao.Phan",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.Username != "cao.phan-2" {
		t.Fatalf("expected uniquified username cao.phan-2, got %q", u.Username)
	}
	if u.MustChangePassword {
		t.Fatal("JIT users must not be forced to change password")
	}
	if u.Role != "USER" {
		t.Fatalf("expected default role USER, got %q", u.Role)
	}
	if u.PasswordHash == "" {
		t.Fatal("JIT user must have an (unusable) password hash")
	}
	if store.oidcSub[u.ID] != "sub-42" {
		t.Fatal("JIT user must be linked to the OIDC subject")
	}
	if store.emails[u.ID] != "cao.phan@example.com" {
		t.Fatal("JIT user must carry the asserted email")
	}
}

func TestResolveOIDCUserFallsBackToEmailLocalPart(t *testing.T) {
	svc := newTestService(newMemUserStore(), true)
	u, err := svc.ResolveOIDCUser(context.Background(), Identity{Subject: "sub-7", Email: "An.Binh+x@corp.vn"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.Username != "an.binhx" {
		t.Fatalf("expected sanitized email local part, got %q", u.Username)
	}
}

func TestResolveOIDCUserRespectsAutoCreateOff(t *testing.T) {
	svc := newTestService(newMemUserStore(), false)
	_, err := svc.ResolveOIDCUser(context.Background(), Identity{Subject: "sub-1", Email: "x@example.com"})
	if !errors.Is(err, ErrUserNotProvisioned) {
		t.Fatalf("expected ErrUserNotProvisioned, got %v", err)
	}
}

// Galaxy ADR-038 user-directory sync — SSO-login safety: an account
// pre-provisioned by the directory reconciler (email + oidc_sub already set,
// random password) must resolve to the SAME row on first OIDC login. The
// subject match short-circuits before the email/JIT paths, so no duplicate is
// created even with auto-create enabled.
func TestResolveOIDCUserReusesDirectorySyncedAccount(t *testing.T) {
	store := newMemUserStore()
	synced := &userdom.User{
		ID:       uuid.New(),
		Username: "cao.phan",
		Email:    "cao.phan@galaxy.example",
		OIDCSub:  "vortex-uuid-1",
	}
	store.users = append(store.users, synced)
	store.oidcSub[synced.ID] = "vortex-uuid-1"
	store.emails[synced.ID] = "cao.phan@galaxy.example"

	svc := newTestService(store, true)
	u, err := svc.ResolveOIDCUser(context.Background(), Identity{
		Subject: "vortex-uuid-1",
		Email:   "cao.phan@galaxy.example",
		Name:    "Cao Phan",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.ID != synced.ID {
		t.Fatalf("expected the directory-synced row to be reused, got %s", u.ID)
	}
	if len(store.users) != 1 {
		t.Fatalf("no duplicate may be JIT-created for a synced account, have %d users", len(store.users))
	}
}
