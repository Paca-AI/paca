// Package usersvc implements the user use-case service.
package usersvc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	globalroledom "github.com/Paca-AI/api/internal/domain/globalrole"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/events"
	"github.com/Paca-AI/api/internal/platform/authz"
	"github.com/Paca-AI/api/internal/platform/messaging"
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
	avatarSvc              attachmentdom.AvatarService
	tokenRepo              userdom.PasswordSetTokenRepository
	publisher              *messaging.Publisher
}

// ErrRoleResolverRequired indicates a missing role resolver dependency when a
// mutating path requires Role -> RoleID resolution.
var ErrRoleResolverRequired = errors.New("user svc: role resolver required")

// ErrAvatarServiceRequired indicates a missing AvatarService dependency when
// an avatar-upload path is invoked.
var ErrAvatarServiceRequired = errors.New("user svc: avatar service required")

// ErrPasswordSetTokenRepoRequired indicates a missing token repository
// dependency when a password-set-token path is invoked.
var ErrPasswordSetTokenRepoRequired = errors.New("user svc: password set token repository required")

// passwordSetTokenTTL bounds how long an issued password-set link stays
// redeemable.
const passwordSetTokenTTL = 24 * time.Hour

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

// WithAvatarService configures avatar upload support.
func (s *Service) WithAvatarService(svc attachmentdom.AvatarService) *Service {
	s.avatarSvc = svc
	return s
}

// WithPasswordSetTokenRepo configures password-set-token issuance/validation.
func (s *Service) WithPasswordSetTokenRepo(repo userdom.PasswordSetTokenRepository) *Service {
	s.tokenRepo = repo
	return s
}

// WithEventPublishing configures publishing a user.created event to the
// plugin event stream for every new user — what, if anything, a plugin does
// with that event is up to the plugin. publisher may be nil, in which case
// the event is skipped silently, matching this codebase's existing
// convention for optional event publishers (see tasksvc.ActivitySvc).
func (s *Service) WithEventPublishing(publisher *messaging.Publisher) *Service {
	s.publisher = publisher
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

// CountUsers returns the total count of users without paginating rows.
func (s *Service) CountUsers(ctx context.Context) (int64, error) {
	return s.repo.CountUsers(ctx)
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
	// Check username uniqueness among active users only; a soft-deleted
	// user's username is freed up for reuse.
	_, err := s.repo.FindByUsername(ctx, in.Username)
	if err == nil {
		return nil, userdom.ErrUsernameTaken
	}
	if !errors.Is(err, userdom.ErrNotFound) {
		return nil, err
	}

	if in.Email != "" {
		if err := s.checkEmailAvailable(ctx, in.Email); err != nil {
			return nil, err
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("user svc: hash password: %w", err)
	}

	roleName := in.Role
	if roleName == "" {
		roleName = userdom.RoleUser
	}
	if s.roleRepo == nil {
		return nil, ErrRoleResolverRequired
	}

	r, err := s.roleRepo.FindByName(ctx, roleName)
	if err != nil {
		if errors.Is(err, globalroledom.ErrNotFound) {
			return nil, globalroledom.ErrNotFound
		}
		return nil, fmt.Errorf("user svc: create: lookup role: %w", err)
	}
	roleID := r.ID

	now := time.Now()
	u := &userdom.User{
		ID:                   uuid.New(),
		Username:             in.Username,
		PasswordHash:         string(hash),
		FullName:             in.FullName,
		RoleID:               roleID,
		Role:                 roleName,
		MustChangePassword:   in.MustChangePassword,
		PasswordLoginEnabled: true,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	if in.Email != "" {
		u.Email = &in.Email
	}

	if err := s.repo.Create(ctx, u); err != nil {
		return nil, err
	}

	s.publishUserIdentityEvent(ctx, events.TopicUserCreated, u)

	return u, nil
}

// publishUserIdentityEvent publishes topic to the plugin event stream,
// carrying only denormalized identity fields (username, full name, email)
// so a subscribing plugin doesn't need direct DB access to resolve u's
// account. Used for both TopicUserCreated (new account) and
// TopicUserPasswordReset (an admin reset an existing account's password) —
// both leave the account in the same must-change-password state and are
// meant to trigger the same downstream plugin behavior (e.g. emailing a
// "set your password" link). Fired unconditionally — paca has no visibility
// into what a given plugin does with the event, and a future plugin may
// have nothing to do with email at all. Deliberately absent: a
// password-set token or invite link. A token is a bearer credential
// (whoever holds it can set that user's password), so it is never embedded
// in an event payload fanned out to every subscriber — a plugin that
// decides, having received this event, that it wants to deliver an invite
// link calls back into the host on demand via paca.password_set_token_issue
// (see platform/plugin.registerPasswordSetTokenFunction), gated by its own
// manifest permission, to mint one scoped to exactly this user_id.
// Best-effort: failures here must not fail the caller's own operation, so
// errors are swallowed after logging is left to the caller's discretion
// (there is none here — this mirrors ActivitySvc.publishCreated's existing
// "errors silently swallowed" convention for optional publishers).
func (s *Service) publishUserIdentityEvent(ctx context.Context, topic string, u *userdom.User) {
	if s.publisher == nil {
		return
	}
	payload := map[string]any{
		"user_id":   u.ID,
		"username":  u.Username,
		"full_name": u.FullName,
	}
	if u.Email != nil && *u.Email != "" {
		payload["email"] = *u.Email
	}
	_ = s.publisher.Append(ctx, events.StreamPluginEvents, topic, payload)
}

// UpdateProfile applies self-service profile changes (e.g. display name).
func (s *Service) UpdateProfile(ctx context.Context, id uuid.UUID, in userdom.UpdateProfileInput) (*userdom.User, error) {
	u, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	u.FullName = in.FullName
	if in.Email != "" && (u.Email == nil || *u.Email != in.Email) {
		if err := s.checkEmailAvailable(ctx, in.Email); err != nil {
			return nil, err
		}
		u.Email = &in.Email
	}
	u.UpdatedAt = time.Now()

	if err := s.repo.Update(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// checkEmailAvailable returns userdom.ErrEmailTaken if email is already in
// use by another active user. Same uniqueness pre-check as username: catches
// a duplicate email before insert/update so it maps to a 409, instead of
// surfacing the raw uni_users_email_active constraint violation as an
// unhandled 500.
func (s *Service) checkEmailAvailable(ctx context.Context, email string) error {
	_, err := s.repo.FindByEmail(ctx, email)
	if err == nil {
		return userdom.ErrEmailTaken
	}
	if !errors.Is(err, userdom.ErrNotFound) {
		return err
	}
	return nil
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
		if s.roleRepo == nil {
			return nil, ErrRoleResolverRequired
		}

		r, err := s.roleRepo.FindByName(ctx, in.Role)
		if err != nil {
			if errors.Is(err, globalroledom.ErrNotFound) {
				// propagate domain-typed not-found error so presenter can map to a 4xx
				return nil, globalroledom.ErrNotFound
			}
			return nil, fmt.Errorf("user svc: admin update: lookup role: %w", err)
		}
		u.RoleID = r.ID
		u.Role = in.Role
	}
	if in.Email != "" && (u.Email == nil || *u.Email != in.Email) {
		if err := s.checkEmailAvailable(ctx, in.Email); err != nil {
			return nil, err
		}
		u.Email = &in.Email
	}
	u.UpdatedAt = time.Now()

	if err := s.repo.Update(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// ResetPassword replaces a user's password with a new bcrypt hash and
// publishes a user.password_reset plugin event (see
// publishUserIdentityEvent) so a subscribing plugin can, for example, email
// the user a "set a new password" link — the same mechanism and mandatory
// treatment as the welcome email a plugin sends on user.created.
func (s *Service) ResetPassword(ctx context.Context, id uuid.UUID, newPassword string) error {
	u, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if !u.PasswordLoginEnabled {
		// SSO-only account: a password reset would silently open a second
		// (password) login path. Fail closed.
		return userdom.ErrPasswordLoginDisabled
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("user svc: reset password: hash: %w", err)
	}

	u.PasswordHash = string(hash)
	u.MustChangePassword = true // admin reset — force the user to set a new password
	u.UpdatedAt = time.Now()

	if err := s.repo.Update(ctx, u); err != nil {
		return err
	}

	s.publishUserIdentityEvent(ctx, events.TopicUserPasswordReset, u)
	return nil
}

// ChangeMyPassword lets a user change their own password. It verifies
// currentPassword, replaces the hash, and clears MustChangePassword.
func (s *Service) ChangeMyPassword(ctx context.Context, id uuid.UUID, currentPassword, newPassword string) error {
	u, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if !u.PasswordLoginEnabled {
		// SSO-only account: there is no local password to change. Fail closed.
		return userdom.ErrPasswordLoginDisabled
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

// IssuePasswordSetToken creates a single-use token letting userID set their
// password via an emailed link. Only the token's SHA-256 hash is persisted;
// the raw token is returned here and nowhere else. Checking userID exists
// up front, rather than letting a bad ID surface as the token insert's FK
// violation, keeps the error a clean userdom.ErrNotFound instead of a raw
// repository/driver error string — this is called directly by a plugin (see
// platform/plugin.registerPasswordSetTokenFunction), which forwards err's
// message back to the plugin, so it must never carry internal DB detail.
func (s *Service) IssuePasswordSetToken(ctx context.Context, userID uuid.UUID) (string, time.Time, error) {
	if s.tokenRepo == nil {
		return "", time.Time{}, ErrPasswordSetTokenRepoRequired
	}

	u, err := s.repo.FindByID(ctx, userID)
	if err != nil {
		return "", time.Time{}, err
	}
	if !u.PasswordLoginEnabled {
		// SSO-only account: no password to set via an emailed link. Fail closed.
		return "", time.Time{}, userdom.ErrPasswordLoginDisabled
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, fmt.Errorf("user svc: issue password set token: %w", err)
	}
	rawToken := base64.RawURLEncoding.EncodeToString(raw)
	expiresAt := time.Now().Add(passwordSetTokenTTL)

	t := &userdom.PasswordSetToken{
		ID:        uuid.New(),
		UserID:    userID,
		TokenHash: hashPasswordSetToken(rawToken),
		ExpiresAt: expiresAt,
		CreatedAt: time.Now(),
	}
	if err := s.tokenRepo.Create(ctx, t); err != nil {
		return "", time.Time{}, err
	}
	return rawToken, expiresAt, nil
}

// SetPasswordWithToken validates rawToken and, if active, sets the
// associated account's password and marks the token used. Only usable while
// the account is still in the must-change-password state (set by Create's
// admin-invite flow and ResetPassword alike, cleared once a password is
// actually set by any means) — this closes the window where a token that
// was never marked used (e.g. the recipient instead logged in with their
// temporary password and changed it through the normal UI) would otherwise
// stay silently redeemable until its 24h expiry, letting anyone who later
// gets hold of the old invite link hijack the account without ever knowing
// its current password.
func (s *Service) SetPasswordWithToken(ctx context.Context, rawToken, newPassword string) error {
	if s.tokenRepo == nil {
		return ErrPasswordSetTokenRepoRequired
	}

	t, err := s.tokenRepo.FindActiveByTokenHash(ctx, hashPasswordSetToken(rawToken))
	if err != nil {
		return err
	}

	u, err := s.repo.FindByID(ctx, t.UserID)
	if err != nil {
		return err
	}
	if !u.MustChangePassword {
		// Same generic error as an unknown/expired/used token — deliberately
		// not distinguished, so a caller can't use response differences to
		// probe an account's password-change state.
		return userdom.ErrPasswordSetTokenInvalid
	}
	if !u.PasswordLoginEnabled {
		// SSO-only account: no local password to set. Fail closed.
		return userdom.ErrPasswordLoginDisabled
	}

	// Claim the token atomically before writing the password: this is what
	// makes redemption single-use under concurrent requests. If two
	// requests race on the same token, only one MarkUsed call wins the
	// claim, so at most one of them goes on to change the password.
	claimed, err := s.tokenRepo.MarkUsed(ctx, t.ID)
	if err != nil {
		return err
	}
	if !claimed {
		return userdom.ErrPasswordSetTokenInvalid
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("user svc: set password with token: hash: %w", err)
	}
	u.PasswordHash = string(hash)
	u.MustChangePassword = false
	u.UpdatedAt = time.Now()
	return s.repo.Update(ctx, u)
}

// hashPasswordSetToken derives the storage/lookup hash for a raw
// password-set token. SHA-256 (not bcrypt) is deliberate: the token is
// already a high-entropy random value, not a user-chosen secret, so a fast
// hash is sufficient and lets FindActiveByTokenHash look it up directly by
// an indexed equality match instead of scanning and comparing every live
// token with a slow KDF.
func hashPasswordSetToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// Delete soft-deletes a user.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.Delete(ctx, id)
}

// InitiateAvatarUpload starts an avatar upload for the user's own profile picture.
func (s *Service) InitiateAvatarUpload(ctx context.Context, userID uuid.UUID, fileName, contentType string, fileSize int64) (*attachmentdom.UploadSession, error) {
	if s.avatarSvc == nil {
		return nil, ErrAvatarServiceRequired
	}
	return s.avatarSvc.InitiateAvatarUpload(ctx, attachmentdom.AvatarUploadInput{
		OwnerKind:   attachmentdom.AvatarOwnerUser,
		OwnerID:     userID,
		FileName:    fileName,
		ContentType: contentType,
		FileSize:    fileSize,
		UploadedBy:  userID,
	})
}

// CompleteAvatarUpload finishes an avatar upload, replacing any previous avatar.
func (s *Service) CompleteAvatarUpload(ctx context.Context, userID, fileID uuid.UUID) (*userdom.User, error) {
	if s.avatarSvc == nil {
		return nil, ErrAvatarServiceRequired
	}
	u, err := s.repo.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	keys, err := s.avatarSvc.CompleteAvatarUpload(ctx, attachmentdom.AvatarCompleteInput{
		OwnerKind: attachmentdom.AvatarOwnerUser,
		OwnerID:   userID,
		FileID:    fileID,
	})
	if err != nil {
		return nil, err
	}

	oldKey, oldThumbKey := u.AvatarKey, u.AvatarThumbKey
	u.AvatarKey = &keys.Key
	u.AvatarThumbKey = &keys.ThumbKey
	u.UpdatedAt = time.Now()
	if err := s.repo.Update(ctx, u); err != nil {
		return nil, err
	}

	s.avatarSvc.DeleteAvatarObjects(ctx, oldKey, oldThumbKey)
	return u, nil
}

// RemoveAvatar clears the user's avatar, deleting the underlying objects.
func (s *Service) RemoveAvatar(ctx context.Context, userID uuid.UUID) (*userdom.User, error) {
	if s.avatarSvc == nil {
		return nil, ErrAvatarServiceRequired
	}
	u, err := s.repo.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	oldKey, oldThumbKey := u.AvatarKey, u.AvatarThumbKey
	if oldKey == nil && oldThumbKey == nil {
		return u, nil
	}
	u.AvatarKey = nil
	u.AvatarThumbKey = nil
	u.UpdatedAt = time.Now()
	if err := s.repo.Update(ctx, u); err != nil {
		return nil, err
	}

	s.avatarSvc.DeleteAvatarObjects(ctx, oldKey, oldThumbKey)
	return u, nil
}
