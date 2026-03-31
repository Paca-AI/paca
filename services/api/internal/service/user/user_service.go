// Package usersvc implements the user use-case service.
package usersvc

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	globalroledom "github.com/paca/api/internal/domain/globalrole"
	userdom "github.com/paca/api/internal/domain/user"
	"github.com/paca/api/internal/platform/authz"
	"golang.org/x/crypto/bcrypt"
)

// GlobalPermissionReader resolves global permissions for a user.
type GlobalPermissionReader interface {
	ListGlobalPermissions(ctx context.Context, userID uuid.UUID) ([]authz.Permission, error)
}

// RoleByNameFinder looks up a global role by its unique name.
type RoleByNameFinder interface {
	FindByName(ctx context.Context, name string) (*globalroledom.GlobalRole, error)
}

// Service is the concrete implementation of domain/user.Service.
type Service struct {
	repo                   userdom.Repository
	globalPermissionReader GlobalPermissionReader
	roleRepo               RoleByNameFinder
}

// New returns a configured user Service.
// Pass optional GlobalPermissionReader and RoleByNameFinder as variadic args.
func New(repo userdom.Repository, opts ...any) *Service {
	s := &Service{repo: repo}
	for _, opt := range opts {
		switch v := opt.(type) {
		case GlobalPermissionReader:
			s.globalPermissionReader = v
		case RoleByNameFinder:
			s.roleRepo = v
		}
	}
	return s
}

// GetByID returns a user by primary key.
func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (*userdom.User, error) {
	return s.repo.FindByID(ctx, id)
}

// List returns a page of users and the total count.
func (s *Service) List(ctx context.Context, page, pageSize int) ([]*userdom.User, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize
	return s.repo.List(ctx, offset, pageSize)
}

// ListGlobalPermissions returns effective global permissions for the user.
func (s *Service) ListGlobalPermissions(ctx context.Context, id uuid.UUID) ([]string, error) {
	u, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}
	for _, p := range authz.LegacyPermissionsForRole(u.Role) {
		seen[string(p)] = struct{}{}
	}

	if s.globalPermissionReader != nil {
		perms, err := s.globalPermissionReader.ListGlobalPermissions(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, p := range perms {
			seen[string(p)] = struct{}{}
		}
	}

	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)

	return out, nil
}

// Create registers a new user with a hashed password.
// If Role is provided, it is resolved to a RoleID via roleRepo.
func (s *Service) Create(ctx context.Context, in userdom.CreateInput) (*userdom.User, error) {
	// Check username uniqueness.
	_, err := s.repo.FindByUsername(ctx, in.Username)
	if err == nil {
		return nil, userdom.ErrUsernameTaken
	}
	if !errors.Is(err, userdom.ErrNotFound) {
		return nil, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("user svc: hash password: %w", err)
	}

	roleName := in.Role
	if roleName == "" {
		roleName = userdom.RoleUser
	}

	var roleID uuid.UUID
	if s.roleRepo != nil {
		r, err := s.roleRepo.FindByName(ctx, roleName)
		if err != nil {
			if errors.Is(err, globalroledom.ErrNotFound) {
				return nil, fmt.Errorf("user svc: create: role %q not found: %w", roleName, err)
			}
			return nil, fmt.Errorf("user svc: create: lookup role: %w", err)
		}
		roleID = r.ID
	}

	now := time.Now()
	u := &userdom.User{
		ID:                 uuid.New(),
		Username:           in.Username,
		PasswordHash:       string(hash),
		FullName:           in.FullName,
		RoleID:             roleID,
		Role:               roleName,
		MustChangePassword: in.MustChangePassword,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	if err := s.repo.Create(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// UpdateProfile applies self-service profile changes (e.g. display name).
func (s *Service) UpdateProfile(ctx context.Context, id uuid.UUID, in userdom.UpdateProfileInput) (*userdom.User, error) {
	u, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	u.FullName = in.FullName
	u.UpdatedAt = time.Now()

	if err := s.repo.Update(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// AdminUpdate applies admin-level changes to any user account.
// If Role is provided, it is resolved to a RoleID via roleRepo.
func (s *Service) AdminUpdate(ctx context.Context, id uuid.UUID, in userdom.AdminUpdateInput) (*userdom.User, error) {
	u, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if in.FullName != "" {
		u.FullName = in.FullName
	}
	if in.Role != "" {
		if s.roleRepo != nil {
			r, err := s.roleRepo.FindByName(ctx, in.Role)
			if err != nil {
				if errors.Is(err, globalroledom.ErrNotFound) {
					return nil, fmt.Errorf("user svc: admin update: role %q not found", in.Role)
				}
				return nil, fmt.Errorf("user svc: admin update: lookup role: %w", err)
			}
			u.RoleID = r.ID
		}
		u.Role = in.Role
	}
	u.UpdatedAt = time.Now()

	if err := s.repo.Update(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// ResetPassword replaces a user's password with a new bcrypt hash.
func (s *Service) ResetPassword(ctx context.Context, id uuid.UUID, newPassword string) error {
	u, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("user svc: reset password: hash: %w", err)
	}

	u.PasswordHash = string(hash)
	u.MustChangePassword = true // admin reset — force the user to set a new password
	u.UpdatedAt = time.Now()

	return s.repo.Update(ctx, u)
}

// ChangeMyPassword lets a user change their own password. It verifies
// currentPassword, replaces the hash, and clears MustChangePassword.
func (s *Service) ChangeMyPassword(ctx context.Context, id uuid.UUID, currentPassword, newPassword string) error {
	u, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(currentPassword)); err != nil {
		return userdom.ErrInvalidCurrentPassword
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("user svc: change password: hash: %w", err)
	}

	u.PasswordHash = string(hash)
	u.MustChangePassword = false
	u.UpdatedAt = time.Now()

	return s.repo.Update(ctx, u)
}

// Delete soft-deletes a user.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.Delete(ctx, id)
}
