package sso

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	ssodom "github.com/Paca-AI/api/internal/domain/sso"
	userdom "github.com/Paca-AI/api/internal/domain/user"
)

// Claims are the identity claims SSO sign-in reads from an ID token (or the
// userinfo response).
type Claims struct {
	Subject           string   `json:"sub"`
	Email             string   `json:"email"`
	EmailVerified     flexBool `json:"email_verified"`
	Name              string   `json:"name"`
	GivenName         string   `json:"given_name"`
	FamilyName        string   `json:"family_name"`
	PreferredUsername string   `json:"preferred_username"`
}

// merge fills c's empty fields from o.
func (c *Claims) merge(o Claims) {
	if c.Email == "" {
		c.Email, c.EmailVerified = o.Email, o.EmailVerified
	}
	if c.Name == "" {
		c.Name = o.Name
	}
	if c.GivenName == "" {
		c.GivenName = o.GivenName
	}
	if c.FamilyName == "" {
		c.FamilyName = o.FamilyName
	}
	if c.PreferredUsername == "" {
		c.PreferredUsername = o.PreferredUsername
	}
}

// flexBool accepts both true and "true": some providers send email_verified
// as a string.
type flexBool bool

func (b *flexBool) UnmarshalJSON(data []byte) error {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	switch t := v.(type) {
	case bool:
		*b = flexBool(t)
	case string:
		parsed, _ := strconv.ParseBool(t)
		*b = flexBool(parsed)
	}
	return nil
}

// verifiedEmail returns the lowercased email when the provider asserted it as
// verified, else "". An unverified email is never used for linking,
// provisioning or the domain allow-list: anyone can type any address into
// many providers' profile forms.
func (c Claims) verifiedEmail() string {
	if !c.EmailVerified {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(c.Email))
}

// ResolveUser maps a verified identity from p to the Paca account it signs
// in as, in order:
//
//  1. the account already linked to (p, subject);
//  2. with p.LinkByEmail, the active account with the same verified email —
//     linked now;
//  3. with p.AutoProvision, a new account with the default global role —
//     linked now.
//
// A linked account that has since been deleted does not sign in and is not
// re-provisioned: deleting a user must keep them out.
func (s *Service) ResolveUser(ctx context.Context, p *ssodom.Provider, c Claims) (*userdom.User, error) {
	if c.Subject == "" {
		return nil, ssodom.ErrExchangeFailed
	}
	email := c.verifiedEmail()
	if !emailDomainAllowed(email, p.AllowedDomains) {
		return nil, ssodom.ErrEmailNotAllowed
	}
	var emailPtr *string
	if email != "" {
		emailPtr = &email
	}

	ident, err := s.repo.FindIdentity(ctx, p.ID, c.Subject)
	if err != nil {
		return nil, err
	}
	if ident != nil {
		return s.signInLinked(ctx, ident, emailPtr)
	}

	var u *userdom.User
	provisioned := false
	if email != "" {
		existing, err := s.users.FindByEmail(ctx, email)
		switch {
		case err == nil && p.LinkByEmail:
			u = existing
		case err == nil:
			// An account already owns this email; creating a second one
			// would fail on the unique email index anyway, and silently
			// provisioning an email-less duplicate would confuse everyone.
			return nil, ssodom.ErrAccountExists
		case !errors.Is(err, userdom.ErrNotFound):
			return nil, err
		}
	}
	if u == nil {
		if !p.AutoProvision {
			return nil, ssodom.ErrNoAccount
		}
		if u, err = s.provision(ctx, c, email); err != nil {
			return nil, err
		}
		provisioned = true
	}

	now := time.Now().UTC()
	if err := s.repo.CreateIdentity(ctx, &ssodom.Identity{
		ID:          uuid.New(),
		UserID:      u.ID,
		ProviderID:  p.ID,
		Subject:     c.Subject,
		Email:       emailPtr,
		CreatedAt:   now,
		LastLoginAt: now,
	}); errors.Is(err, ssodom.ErrIdentityExists) {
		// A concurrent first sign-in for the same subject linked it first
		// (both missed FindIdentity above). Sign in as whatever it linked.
		ident, err := s.repo.FindIdentity(ctx, p.ID, c.Subject)
		if err != nil {
			return nil, err
		}
		if ident == nil {
			return nil, ssodom.ErrNoAccount
		}
		// Drop the account this attempt provisioned only if the winner linked
		// a different one. With LinkByEmail the winner may have adopted this
		// very account (found by the email provision just set), and deleting
		// it would lock that user out.
		if provisioned && ident.UserID != u.ID {
			if err := s.users.Delete(ctx, u.ID); err != nil {
				s.log.Warn("sso: remove duplicate provisioned user", "user_id", u.ID, "error", err)
			}
		}
		return s.signInLinked(ctx, ident, emailPtr)
	} else if err != nil {
		return nil, err
	}
	return u, nil
}

// signInLinked returns the active account ident links to and records the
// sign-in. A linked account that has since been deleted does not sign in.
func (s *Service) signInLinked(ctx context.Context, ident *ssodom.Identity, email *string) (*userdom.User, error) {
	u, err := s.users.FindByID(ctx, ident.UserID)
	if errors.Is(err, userdom.ErrNotFound) {
		return nil, ssodom.ErrNoAccount
	}
	if err != nil {
		return nil, err
	}
	if err := s.repo.TouchIdentity(ctx, ident.ID, email); err != nil {
		s.log.Warn("sso: record identity sign-in", "error", err)
	}
	return u, nil
}

func emailDomainAllowed(email string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	at := strings.LastIndexByte(email, '@')
	if at < 0 {
		return false
	}
	return slices.Contains(allowed, email[at+1:])
}

// provision creates the account for a first-time SSO user. The password is
// random and never shown, so the account signs in only through SSO until an
// admin resets it.
func (s *Service) provision(ctx context.Context, c Claims, email string) (*userdom.User, error) {
	password, err := randomToken()
	if err != nil {
		return nil, err
	}
	fullName := strings.TrimSpace(c.Name)
	if fullName == "" {
		fullName = strings.TrimSpace(c.GivenName + " " + c.FamilyName)
	}
	base := usernameBase(c, email)
	if fullName == "" {
		fullName = base
	}

	// Pick the first free username: base, base2, base3… then a random
	// suffix. Create re-checks, so a lost race just moves on to the next.
	for i := 1; i <= 20; i++ {
		candidate := base
		if i > 1 {
			candidate = withSuffix(base, strconv.Itoa(i))
		}
		if i == 20 {
			candidate = withSuffix(base, randomSuffix())
		}
		if _, err := s.users.FindByUsername(ctx, candidate); err == nil {
			continue
		} else if !errors.Is(err, userdom.ErrNotFound) {
			return nil, err
		}
		u, err := s.creator.Create(ctx, userdom.CreateInput{
			Username: candidate,
			Password: password,
			FullName: fullName,
			Email:    email,
		})
		if errors.Is(err, userdom.ErrUsernameTaken) {
			continue
		}
		if errors.Is(err, userdom.ErrEmailTaken) {
			return nil, ssodom.ErrAccountExists
		}
		if err != nil {
			return nil, fmt.Errorf("sso svc: provision user: %w", err)
		}
		return u, nil
	}
	return nil, fmt.Errorf("sso svc: provision user: no free username for %q", base)
}

const maxUsernameLen = 32

var usernameInvalidRe = regexp.MustCompile(`[^a-z0-9._-]+`)

// usernameBase derives a username from the identity: preferred_username,
// else the email's local part, else "user". Lowercased, restricted to
// [a-z0-9._-], 3–32 characters.
func usernameBase(c Claims, email string) string {
	raw := c.PreferredUsername
	if at := strings.IndexByte(raw, '@'); at >= 0 {
		raw = raw[:at]
	}
	if strings.TrimSpace(raw) == "" && email != "" {
		raw, _, _ = strings.Cut(email, "@")
	}
	v := usernameInvalidRe.ReplaceAllString(strings.ToLower(raw), "-")
	v = strings.Trim(v, "-._")
	if len(v) > maxUsernameLen {
		v = strings.Trim(v[:maxUsernameLen], "-._")
	}
	if len(v) < 3 {
		v = "user"
	}
	return v
}

func withSuffix(base, suffix string) string {
	if len(suffix) >= maxUsernameLen {
		return suffix[:maxUsernameLen]
	}
	if len(base)+len(suffix) > maxUsernameLen {
		base = base[:maxUsernameLen-len(suffix)]
	}
	return base + suffix
}

func randomSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "-" + strings.ToLower(base64.RawURLEncoding.EncodeToString(b)[:6])
}

func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
