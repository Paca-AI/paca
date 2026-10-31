// Package mfadom holds the multi-factor-authentication and login-restriction
// domain: TOTP enrollments, recovery codes, per-account and instance-level
// IP/CIDR allow-lists, and the policy that makes both mandatory for
// privileged accounts.
package mfadom

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Domain sentinel errors.
var (
	// ErrNotFound means the user has no enrollment.
	ErrNotFound = errors.New("mfa: enrollment not found")
	// ErrRequired means the account has MFA enabled and the login carried no
	// second factor. The client should prompt for a code and retry.
	ErrRequired = errors.New("mfa: second factor required")
	// ErrInvalidCode means the supplied TOTP or recovery code is wrong,
	// expired, or already used.
	ErrInvalidCode = errors.New("mfa: invalid code")
	// ErrSetupRequired means policy requires this account to use MFA but it
	// has not enrolled yet.
	ErrSetupRequired = errors.New("mfa: enrollment required by policy")
	// ErrIPNotAllowed means the login came from outside an allow-list.
	ErrIPNotAllowed = errors.New("mfa: login not allowed from this address")
	// ErrSetupNotRequired means password-only enrollment was asked for an
	// account that policy does not oblige to use MFA. Such an account enrolls
	// from a signed-in session instead.
	ErrSetupNotRequired = errors.New("mfa: enrollment is not required for this account")
	// ErrInvalidCIDR means an allow-list entry is not an IP or CIDR.
	ErrInvalidCIDR = errors.New("mfa: invalid ip or cidr")
	// ErrAlreadyEnabled means enrollment was started on an enrolled account.
	ErrAlreadyEnabled = errors.New("mfa: already enabled")
	// ErrNotEnabled means the operation needs an enabled enrollment.
	ErrNotEnabled = errors.New("mfa: not enabled")
	// ErrLocked means too many wrong codes were entered for the account; no
	// second factor is accepted again until the lockout expires.
	ErrLocked = errors.New("too many failed attempts")
	// ErrSelfLockout means a policy change would block the admin making it.
	ErrSelfLockout = errors.New("mfa: policy would block the current address")
	// ErrUnavailable means the server has no ENCRYPTION_KEY, so TOTP
	// secrets cannot be stored.
	ErrUnavailable = errors.New("mfa: unavailable (ENCRYPTION_KEY not configured)")
)

// LockedError is ErrLocked plus how long the lockout has left.
type LockedError struct{ RetryAfter time.Duration }

func (e *LockedError) Error() string { return ErrLocked.Error() }

// Is makes errors.Is(err, ErrLocked) true.
func (e *LockedError) Is(target error) bool { return target == ErrLocked }

// Enrollment is a user's TOTP enrollment. Enabled=false is pending: the
// secret was issued but no valid code has confirmed it yet.
type Enrollment struct {
	UserID uuid.UUID
	// SecretEnc is the base32 TOTP secret, encrypted at rest.
	SecretEnc string
	Enabled   bool
	// RecoveryCodes are SHA-256 hex hashes of the unused recovery codes.
	RecoveryCodes []string
	// LastUsedStep is the newest TOTP time step already accepted.
	LastUsedStep int64
	// LockedUntil, when in the future, rejects every second-factor attempt.
	LockedUntil *time.Time
	UpdatedAt   time.Time
}

// AllRoles is the sole entry of Policy.PrivilegedRoles that applies the
// policy to every account, whatever its roles.
const AllRoles = "*"

// Policy is the instance-wide login policy. The zero value restricts no one.
type Policy struct {
	// PrivilegedRoles names the platform roles the policy applies to; the
	// single entry AllRoles means every account. Empty means nobody.
	PrivilegedRoles []string
	// RequireMFA demands an enrolled second factor of privileged accounts.
	RequireMFA bool
	// IPAllowList, when non-empty, limits password login of privileged
	// accounts to these IPs/CIDRs.
	IPAllowList []string
}

// Store is the persistence contract.
type Store interface {
	GetEnrollment(ctx context.Context, userID uuid.UUID) (*Enrollment, error)
	// SaveEnrollment upserts the enrollment (secret, enabled, codes).
	SaveEnrollment(ctx context.Context, e *Enrollment) error
	DeleteEnrollment(ctx context.Context, userID uuid.UUID) error
	// AdvanceStep atomically records step as used, returning false when a
	// step >= it was already accepted (a replay).
	AdvanceStep(ctx context.Context, userID uuid.UUID, step int64) (bool, error)
	// ConsumeRecoveryCode atomically removes one recovery-code hash,
	// returning false when it was not present.
	ConsumeRecoveryCode(ctx context.Context, userID uuid.UUID, hash string) (bool, error)

	// ReplaceRecoveryCodes swaps only the recovery-code hashes, leaving the
	// replay step and lockout state untouched so it can't undo a concurrent
	// login.
	ReplaceRecoveryCodes(ctx context.Context, userID uuid.UUID, hashes []string) error
	// RegisterFailure counts a wrong code; the maxAttempts-th consecutive
	// failure locks the account for lockout and resets the counter.
	RegisterFailure(ctx context.Context, userID uuid.UUID, maxAttempts int, lockout time.Duration) error
	// ClearFailures resets the failure counter and any lockout.
	ClearFailures(ctx context.Context, userID uuid.UUID) error

	GetUserCIDRs(ctx context.Context, userID uuid.UUID) ([]string, error)
	SetUserCIDRs(ctx context.Context, userID uuid.UUID, cidrs []string) error

	GetPolicy(ctx context.Context) (*Policy, error)
	SavePolicy(ctx context.Context, p *Policy, updatedBy *uuid.UUID) error
}
