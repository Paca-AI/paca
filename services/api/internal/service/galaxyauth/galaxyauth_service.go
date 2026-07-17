// Package galaxyauth resolves identities asserted by the trusted Vortex
// identity provider (ADR-038) to local user accounts: OIDC SSO logins
// (find-by-sub → link-by-email → optional JIT provisioning) and, separately,
// RS256 bearer tokens presented by platform agents.
package galaxyauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	globalroledom "github.com/Paca-AI/api/internal/domain/globalrole"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// ErrUserNotProvisioned indicates the asserted identity has no local account
// and auto-provisioning is not allowed on this path.
var ErrUserNotProvisioned = errors.New("galaxyauth: no local user for asserted identity")

// maxUsernameAttempts bounds the "-2", "-3", … suffix search when a JIT
// username collides with existing accounts.
const maxUsernameAttempts = 50

// UserStore is the persistence contract galaxyauth needs.  It is implemented
// by the postgres UserRepository.
type UserStore interface {
	FindByOIDCSub(ctx context.Context, sub string) (*userdom.User, error)
	FindByEmail(ctx context.Context, email string) (*userdom.User, error)
	FindByUsername(ctx context.Context, username string) (*userdom.User, error)
	Create(ctx context.Context, u *userdom.User) error
	LinkOIDC(ctx context.Context, userID uuid.UUID, sub, email string) error
}

// RoleFinder resolves a global role by name; implemented by the postgres
// GlobalRoleRepository.
type RoleFinder interface {
	FindByName(ctx context.Context, name string) (*globalroledom.GlobalRole, error)
}

// Identity carries the claims extracted from a verified ID token.
type Identity struct {
	Subject           string
	Email             string
	Name              string
	PreferredUsername string
}

// Service maps Vortex identities to local users.
type Service struct {
	users       UserStore
	roles       RoleFinder
	autoCreate  bool
	defaultRole string
	log         *slog.Logger
}

// New returns a configured galaxyauth Service.  autoCreate and defaultRole
// only apply to the OIDC SSO login path (never to bearer tokens).
func New(users UserStore, roles RoleFinder, autoCreate bool, defaultRole string, log *slog.Logger) *Service {
	return &Service{
		users:       users,
		roles:       roles,
		autoCreate:  autoCreate,
		defaultRole: defaultRole,
		log:         log,
	}
}

// ResolveOIDCUser maps a verified OIDC identity to a local user:
//  1. an account already linked to the subject wins;
//  2. otherwise an active account with the same email is linked to the
//     subject;
//  3. otherwise a new account is JIT-provisioned when auto-create is enabled,
//     with an unusable random password and the configured default global role.
func (s *Service) ResolveOIDCUser(ctx context.Context, id Identity) (*userdom.User, error) {
	if id.Subject == "" {
		return nil, fmt.Errorf("galaxyauth: id token has no subject")
	}

	u, err := s.users.FindByOIDCSub(ctx, id.Subject)
	if err == nil {
		return u, nil
	}
	if !errors.Is(err, userdom.ErrNotFound) {
		return nil, err
	}

	if id.Email != "" {
		u, err = s.users.FindByEmail(ctx, id.Email)
		if err == nil {
			if err := s.users.LinkOIDC(ctx, u.ID, id.Subject, id.Email); err != nil {
				return nil, err
			}
			s.log.Info("galaxyauth: linked existing user to OIDC subject", "user_id", u.ID, "username", u.Username)
			return u, nil
		}
		if !errors.Is(err, userdom.ErrNotFound) {
			return nil, err
		}
	}

	if !s.autoCreate {
		return nil, ErrUserNotProvisioned
	}
	return s.jitCreateUser(ctx, id)
}

func (s *Service) jitCreateUser(ctx context.Context, id Identity) (*userdom.User, error) {
	role, err := s.roles.FindByName(ctx, s.defaultRole)
	if err != nil {
		return nil, fmt.Errorf("galaxyauth: resolve default role %q: %w", s.defaultRole, err)
	}

	username, err := s.uniqueUsername(ctx, usernameBase(id))
	if err != nil {
		return nil, err
	}

	// The account authenticates via SSO only: hash a throwaway random secret
	// so password login can never match anything a human could know.
	randomSecret := make([]byte, 32)
	if _, err := rand.Read(randomSecret); err != nil {
		return nil, fmt.Errorf("galaxyauth: generate password: %w", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(hex.EncodeToString(randomSecret)), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("galaxyauth: hash password: %w", err)
	}

	fullName := id.Name
	if fullName == "" {
		fullName = username
	}

	now := time.Now()
	u := &userdom.User{
		ID:                 uuid.New(),
		Username:           username,
		PasswordHash:       string(hash),
		FullName:           fullName,
		MustChangePassword: false,
		RoleID:             role.ID,
		Role:               role.Name,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := s.users.Create(ctx, u); err != nil {
		return nil, fmt.Errorf("galaxyauth: create user: %w", err)
	}
	if err := s.users.LinkOIDC(ctx, u.ID, id.Subject, id.Email); err != nil {
		return nil, fmt.Errorf("galaxyauth: link created user: %w", err)
	}

	s.log.Info("galaxyauth: JIT-created user from OIDC login", "user_id", u.ID, "username", username, "role", role.Name)
	return u, nil
}

// uniqueUsername returns base or base-2, base-3, … — the first candidate not
// taken by an active account.
func (s *Service) uniqueUsername(ctx context.Context, base string) (string, error) {
	for i := 1; i <= maxUsernameAttempts; i++ {
		candidate := base
		if i > 1 {
			candidate = fmt.Sprintf("%s-%d", base, i)
		}
		_, err := s.users.FindByUsername(ctx, candidate)
		if errors.Is(err, userdom.ErrNotFound) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("galaxyauth: could not find a free username for %q", base)
}

// usernameBase derives a login-friendly username from the identity claims:
// preferred_username first, then the email local part, then the subject.
func usernameBase(id Identity) string {
	for _, candidate := range []string{id.PreferredUsername, emailLocalPart(id.Email)} {
		if v := sanitizeUsername(candidate); v != "" {
			return v
		}
	}
	if v := sanitizeUsername(id.Subject); v != "" {
		return "vortex-" + v
	}
	return "vortex-user"
}

func emailLocalPart(email string) string {
	local, _, _ := strings.Cut(email, "@")
	return local
}

// sanitizeUsername lowercases and keeps [a-z0-9._-], trimming leading/
// trailing separators and padding very short results to the platform minimum.
func sanitizeUsername(v string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(v) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), "._-")
	if out == "" {
		return ""
	}
	if len(out) > 64 {
		out = out[:64]
	}
	for len(out) < 3 {
		out += "0"
	}
	return out
}
