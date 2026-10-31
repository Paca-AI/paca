// Package mfasvc implements TOTP enrollment, second-factor verification and
// IP/CIDR login restrictions.
package mfasvc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	mfadom "github.com/Paca-AI/api/internal/domain/mfa"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/platform/secret"
	"github.com/Paca-AI/api/internal/platform/totp"
)

const (
	recoveryCodeCount = 8
	// recoveryAlphabet omits look-alike characters (0/o, 1/l/i).
	recoveryAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"
)

// Service is the concrete MFA / login-restriction service.
type Service struct {
	store  mfadom.Store
	enc    *secret.Encryptor
	issuer string
	now    func() time.Time
}

// New returns a Service. enc may be nil (ENCRYPTION_KEY unset): existing
// installs keep working, but enrollment is refused with ErrUnavailable.
func New(store mfadom.Store, enc *secret.Encryptor, issuer string) *Service {
	if issuer == "" {
		issuer = "Paca"
	}
	return &Service{store: store, enc: enc, issuer: issuer, now: time.Now}
}

// CheckLogin runs after the password has been verified. It enforces, in order,
// the IP allow-lists and the second factor. otp may be a TOTP or recovery code.
func (s *Service) CheckLogin(ctx context.Context, u *userdom.User, otp, clientIP string) error {
	policy, err := s.store.GetPolicy(ctx)
	if err != nil {
		return err
	}
	privileged := hasAny(u.RoleNames(), policy.PrivilegedRoles)

	userCIDRs, err := s.store.GetUserCIDRs(ctx, u.ID)
	if err != nil {
		return err
	}
	if len(userCIDRs) > 0 && !ipAllowed(clientIP, userCIDRs) {
		return mfadom.ErrIPNotAllowed
	}
	if privileged && len(policy.IPAllowList) > 0 && !ipAllowed(clientIP, policy.IPAllowList) {
		return mfadom.ErrIPNotAllowed
	}

	enr, err := s.store.GetEnrollment(ctx, u.ID)
	if err != nil && !errors.Is(err, mfadom.ErrNotFound) {
		return err
	}
	if enr != nil && enr.Enabled {
		if strings.TrimSpace(otp) == "" {
			return mfadom.ErrRequired
		}
		return s.verifySecondFactor(ctx, enr, otp)
	}
	if privileged && policy.RequireMFA {
		return mfadom.ErrSetupRequired
	}
	return nil
}

func (s *Service) verifySecondFactor(ctx context.Context, enr *mfadom.Enrollment, code string) error {
	if s.enc == nil {
		return mfadom.ErrUnavailable
	}
	clean := strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(clean) == totp.Digits && isDigits(clean) {
		secretB32, err := s.enc.Decrypt(enr.SecretEnc)
		if err != nil {
			return fmt.Errorf("mfa: decrypt secret: %w", err)
		}
		step, ok := totp.Verify(secretB32, clean, s.now())
		if !ok {
			return mfadom.ErrInvalidCode
		}
		fresh, err := s.store.AdvanceStep(ctx, enr.UserID, step)
		if err != nil {
			return err
		}
		if !fresh {
			return mfadom.ErrInvalidCode // replay
		}
		return nil
	}
	used, err := s.store.ConsumeRecoveryCode(ctx, enr.UserID, hashRecovery(clean))
	if err != nil {
		return err
	}
	if !used {
		return mfadom.ErrInvalidCode
	}
	return nil
}

// SetupRequired reports whether policy obliges u to use MFA but u has not
// enrolled: the one case in which enrollment may be started from a password
// alone, because the account cannot sign in to do it any other way.
func (s *Service) SetupRequired(ctx context.Context, u *userdom.User) (bool, error) {
	policy, err := s.store.GetPolicy(ctx)
	if err != nil {
		return false, err
	}
	if !policy.RequireMFA || !hasAny(u.RoleNames(), policy.PrivilegedRoles) {
		return false, nil
	}
	enr, err := s.store.GetEnrollment(ctx, u.ID)
	if errors.Is(err, mfadom.ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return !enr.Enabled, nil
}

// Begin starts (or restarts, while still pending) enrollment, returning the
// shared secret and the otpauth:// URI to render as a QR code.
func (s *Service) Begin(ctx context.Context, u *userdom.User) (secretB32, uri string, err error) {
	if s.enc == nil {
		return "", "", mfadom.ErrUnavailable
	}
	if enr, gerr := s.store.GetEnrollment(ctx, u.ID); gerr == nil && enr.Enabled {
		return "", "", mfadom.ErrAlreadyEnabled
	} else if gerr != nil && !errors.Is(gerr, mfadom.ErrNotFound) {
		return "", "", gerr
	}
	secretB32, err = totp.GenerateSecret()
	if err != nil {
		return "", "", err
	}
	encSecret, err := s.enc.Encrypt(secretB32)
	if err != nil {
		return "", "", err
	}
	if err := s.store.SaveEnrollment(ctx, &mfadom.Enrollment{UserID: u.ID, SecretEnc: encSecret}); err != nil {
		return "", "", err
	}
	return secretB32, totp.ProvisioningURI(s.issuer, u.Username, secretB32), nil
}

// Confirm activates a pending enrollment once code proves the user's device
// holds the secret, and returns the one-time-visible recovery codes.
func (s *Service) Confirm(ctx context.Context, u *userdom.User, code string) ([]string, error) {
	if s.enc == nil {
		return nil, mfadom.ErrUnavailable
	}
	enr, err := s.store.GetEnrollment(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	if enr.Enabled {
		return nil, mfadom.ErrAlreadyEnabled
	}
	secretB32, err := s.enc.Decrypt(enr.SecretEnc)
	if err != nil {
		return nil, fmt.Errorf("mfa: decrypt secret: %w", err)
	}
	step, ok := totp.Verify(secretB32, code, s.now())
	if !ok {
		return nil, mfadom.ErrInvalidCode
	}
	plain, hashes, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	enr.Enabled, enr.RecoveryCodes, enr.LastUsedStep = true, hashes, step
	if err := s.store.SaveEnrollment(ctx, enr); err != nil {
		return nil, err
	}
	return plain, nil
}

// RegenerateRecoveryCodes replaces all recovery codes after a valid second
// factor, returning the new ones.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, u *userdom.User, code string) ([]string, error) {
	enr, err := s.store.GetEnrollment(ctx, u.ID)
	if err != nil || !enr.Enabled {
		return nil, mfadom.ErrNotEnabled
	}
	if err := s.verifySecondFactor(ctx, enr, code); err != nil {
		return nil, err
	}
	plain, hashes, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	// Re-read: verifySecondFactor may have advanced the step or used a code.
	if enr, err = s.store.GetEnrollment(ctx, u.ID); err != nil {
		return nil, err
	}
	enr.RecoveryCodes = hashes
	if err := s.store.SaveEnrollment(ctx, enr); err != nil {
		return nil, err
	}
	return plain, nil
}

// Disable removes the user's enrollment after a valid second factor.
func (s *Service) Disable(ctx context.Context, u *userdom.User, code string) error {
	enr, err := s.store.GetEnrollment(ctx, u.ID)
	if err != nil || !enr.Enabled {
		return mfadom.ErrNotEnabled
	}
	if err := s.verifySecondFactor(ctx, enr, code); err != nil {
		return err
	}
	return s.store.DeleteEnrollment(ctx, u.ID)
}

// Reset removes a user's enrollment without a second factor — the admin
// recovery path for a user who lost both device and recovery codes.
func (s *Service) Reset(ctx context.Context, userID uuid.UUID) error {
	return s.store.DeleteEnrollment(ctx, userID)
}

// Status reports whether MFA is enabled and how many recovery codes remain.
func (s *Service) Status(ctx context.Context, u *userdom.User) (enabled bool, recoveryLeft int, err error) {
	enr, err := s.store.GetEnrollment(ctx, u.ID)
	if errors.Is(err, mfadom.ErrNotFound) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	return enr.Enabled, len(enr.RecoveryCodes), nil
}

// GetPolicy returns the instance policy.
func (s *Service) GetPolicy(ctx context.Context) (*mfadom.Policy, error) {
	return s.store.GetPolicy(ctx)
}

// SetPolicy validates and saves the instance policy.
func (s *Service) SetPolicy(ctx context.Context, p *mfadom.Policy, updatedBy *uuid.UUID) error {
	cidrs, err := NormalizeCIDRs(p.IPAllowList)
	if err != nil {
		return err
	}
	p.IPAllowList = cidrs
	if slices.Contains(p.PrivilegedRoles, mfadom.AllRoles) {
		p.PrivilegedRoles = []string{mfadom.AllRoles}
	}
	return s.store.SavePolicy(ctx, p, updatedBy)
}

// SetUserCIDRs validates and saves a per-account allow-list.
func (s *Service) SetUserCIDRs(ctx context.Context, userID uuid.UUID, cidrs []string) error {
	norm, err := NormalizeCIDRs(cidrs)
	if err != nil {
		return err
	}
	return s.store.SetUserCIDRs(ctx, userID, norm)
}

// GetUserCIDRs returns a per-account allow-list.
func (s *Service) GetUserCIDRs(ctx context.Context, userID uuid.UUID) ([]string, error) {
	return s.store.GetUserCIDRs(ctx, userID)
}

// NormalizeCIDRs trims entries, turns bare IPs into /32 or /128 prefixes, and
// rejects anything that is neither.
func NormalizeCIDRs(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	for _, raw := range in {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if !strings.Contains(raw, "/") {
			addr, err := netip.ParseAddr(raw)
			if err != nil {
				return nil, fmt.Errorf("%w: %q", mfadom.ErrInvalidCIDR, raw)
			}
			raw = netip.PrefixFrom(addr.Unmap(), addr.Unmap().BitLen()).String()
		} else if _, err := netip.ParsePrefix(raw); err != nil {
			return nil, fmt.Errorf("%w: %q", mfadom.ErrInvalidCIDR, raw)
		}
		out = append(out, raw)
	}
	return out, nil
}

func ipAllowed(clientIP string, cidrs []string) bool {
	ip := net.ParseIP(strings.TrimSpace(clientIP))
	if ip == nil {
		return false
	}
	for _, c := range cidrs {
		if _, n, err := net.ParseCIDR(c); err == nil && n.Contains(ip) {
			return true
		}
	}
	return false
}

func hasAny(have, want []string) bool {
	if slices.Contains(want, mfadom.AllRoles) {
		return true
	}
	for _, w := range want {
		for _, h := range have {
			if strings.EqualFold(w, h) {
				return true
			}
		}
	}
	return false
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func hashRecovery(code string) string {
	norm := strings.ToLower(strings.NewReplacer("-", "", " ", "").Replace(code))
	sum := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(sum[:])
}

// newRecoveryCodes returns the plaintext codes (xxxxx-xxxxx) and their hashes.
func newRecoveryCodes() (plain, hashes []string, err error) {
	// Bytes at or above this are discarded: 256 is not a multiple of the
	// alphabet size, so taking them modulo it would favour the first letters.
	const unbiasedLimit = 256 - 256%len(recoveryAlphabet)
	for i := 0; i < recoveryCodeCount; i++ {
		buf := make([]byte, 0, 10)
		for len(buf) < 10 {
			chunk := make([]byte, 16)
			if _, err := rand.Read(chunk); err != nil {
				return nil, nil, fmt.Errorf("mfa: recovery code: %w", err)
			}
			for _, b := range chunk {
				if int(b) < unbiasedLimit && len(buf) < 10 {
					buf = append(buf, recoveryAlphabet[int(b)%len(recoveryAlphabet)])
				}
			}
		}
		code := string(buf[:5]) + "-" + string(buf[5:])
		plain = append(plain, code)
		hashes = append(hashes, hashRecovery(code))
	}
	return plain, hashes, nil
}
