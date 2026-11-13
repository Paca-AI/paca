package mfasvc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	mfadom "github.com/Paca-AI/api/internal/domain/mfa"
	userdom "github.com/Paca-AI/api/internal/domain/user"
)

const (
	// ceremonyTTL is how long a browser has to answer a passkey prompt.
	ceremonyTTL     = 5 * time.Minute
	maxPasskeyName  = 64
	maxPasskeyCount = 20
)

// waUser adapts an account and its passkeys to webauthn.User.
type waUser struct {
	u     *userdom.User
	creds []webauthn.Credential
}

func (w waUser) WebAuthnID() []byte                         { return w.u.ID[:] }
func (w waUser) WebAuthnName() string                       { return w.u.Username }
func (w waUser) WebAuthnDisplayName() string                { return w.u.Username }
func (w waUser) WebAuthnCredentials() []webauthn.Credential { return w.creds }

// RelyingParty identifies this deployment to authenticators. A passkey is
// bound to RPID, so changing it orphans every registered passkey.
type RelyingParty struct {
	ID      string
	Name    string
	Origins []string
}

// RelyingPartyFromURL derives the relying party from the deployment's public
// URL: the host is the RP ID and the URL's origin the only accepted origin.
// rpID, when set, overrides the host (a registrable parent domain, to share
// passkeys across subdomains). An empty publicURL returns false.
func RelyingPartyFromURL(publicURL, rpID, name string) (RelyingParty, bool) {
	u, err := url.Parse(strings.TrimSpace(publicURL))
	if err != nil || u.Scheme == "" || u.Hostname() == "" {
		return RelyingParty{}, false
	}
	if rpID == "" {
		rpID = u.Hostname()
	}
	return RelyingParty{ID: rpID, Name: name, Origins: []string{u.Scheme + "://" + u.Host}}, true
}

// WithPasskeyStore tells the service where passkeys are stored without
// enabling WebAuthn. Install it even when no relying party is configured: an
// account that registered a passkey keeps owing a second factor then, rather
// than silently falling back to its password alone, so a lost PUBLIC_URL
// can't downgrade it.
func (s *Service) WithPasskeyStore(store mfadom.PasskeyStore) *Service {
	s.passkeys = store
	return s
}

// WithPasskeys enables WebAuthn passkeys as a second factor. Without it the
// passkey methods return ErrPasskeyUnavailable.
func (s *Service) WithPasskeys(store mfadom.PasskeyStore, ceremonies mfadom.CeremonyStore, rp RelyingParty) (*Service, error) {
	wa, err := webauthn.New(&webauthn.Config{
		RPID:          rp.ID,
		RPDisplayName: rp.Name,
		RPOrigins:     rp.Origins,
		// Ask authenticators to verify the user (fingerprint, face, PIN) when
		// they can, without refusing security keys that can't.
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			UserVerification: protocol.VerificationPreferred,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("mfa: webauthn config: %w", err)
	}
	s.passkeys, s.ceremonies, s.wa = store, ceremonies, wa
	return s, nil
}

// PasskeysAvailable reports whether passkeys are configured.
func (s *Service) PasskeysAvailable() bool { return s.wa != nil }

func (s *Service) loadUser(ctx context.Context, u *userdom.User) (waUser, []mfadom.Passkey, error) {
	if s.wa == nil {
		return waUser{}, nil, mfadom.ErrPasskeyUnavailable
	}
	list, err := s.passkeys.ListPasskeys(ctx, u.ID)
	if err != nil {
		return waUser{}, nil, err
	}
	creds := make([]webauthn.Credential, 0, len(list))
	for _, p := range list {
		var c webauthn.Credential
		if err := json.Unmarshal(p.Credential, &c); err != nil {
			// One unreadable record must not take down every ceremony (and
			// with it sign-in); the passkey just can't be used.
			slog.Warn("mfa: skipping unreadable passkey", "passkey", p.ID, "error", err)
			continue
		}
		creds = append(creds, c)
	}
	return waUser{u: u, creds: creds}, list, nil
}

// hasPasskeys reports whether u has a registered passkey, whether or not
// WebAuthn is currently enabled. A deployment that loses its relying party
// (PUBLIC_URL unset) therefore fails closed for such an account: it still owes
// a second factor it cannot produce, and signs in only once passkeys are
// configured again, through TOTP if it has one, or after an admin MFA reset.
func (s *Service) hasPasskeys(ctx context.Context, userID uuid.UUID) (bool, error) {
	if s.passkeys == nil {
		return false, nil
	}
	return s.passkeys.HasPasskeys(ctx, userID)
}

// Methods lists the second factors u can answer a login challenge with.
func (s *Service) Methods(ctx context.Context, u *userdom.User) ([]string, error) {
	var out []string
	enr, err := s.store.GetEnrollment(ctx, u.ID)
	if err != nil && !errors.Is(err, mfadom.ErrNotFound) {
		return nil, err
	}
	if enr != nil && enr.Enabled {
		out = append(out, mfadom.MethodTOTP)
	}
	// Only advertised when it can be answered; see hasPasskeys.
	if s.wa != nil {
		has, err := s.hasPasskeys(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		if has {
			out = append(out, mfadom.MethodPasskey)
		}
	}
	return out, nil
}

// ListPasskeys returns u's passkeys.
func (s *Service) ListPasskeys(ctx context.Context, u *userdom.User) ([]mfadom.Passkey, error) {
	if s.wa == nil {
		return nil, mfadom.ErrPasskeyUnavailable
	}
	return s.passkeys.ListPasskeys(ctx, u.ID)
}

// BeginPasskeyRegistration returns the options to hand to
// navigator.credentials.create and parks the challenge server-side.
func (s *Service) BeginPasskeyRegistration(ctx context.Context, u *userdom.User) (json.RawMessage, error) {
	wu, list, err := s.loadUser(ctx, u)
	if err != nil {
		return nil, err
	}
	if len(list) >= maxPasskeyCount {
		return nil, mfadom.ErrPasskeyLimit
	}
	exclude := make([]protocol.CredentialDescriptor, 0, len(wu.creds))
	for _, c := range wu.creds {
		exclude = append(exclude, c.Descriptor())
	}
	creation, session, err := s.wa.BeginRegistration(wu,
		webauthn.WithExclusions(exclude),
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementPreferred),
	)
	if err != nil {
		return nil, fmt.Errorf("mfa: begin passkey registration: %w", err)
	}
	if err := s.putCeremony(ctx, "reg:"+u.ID.String(), session); err != nil {
		return nil, err
	}
	return json.Marshal(creation)
}

// FinishPasskeyRegistration verifies the authenticator's response to the
// options BeginPasskeyRegistration issued and stores the new passkey.
func (s *Service) FinishPasskeyRegistration(ctx context.Context, u *userdom.User, name string, response []byte) (*mfadom.Passkey, error) {
	wu, _, err := s.loadUser(ctx, u)
	if err != nil {
		return nil, err
	}
	session, err := s.takeCeremony(ctx, "reg:"+u.ID.String(), mfadom.ErrPasskeyInvalid)
	if err != nil {
		return nil, err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(response)
	if err != nil {
		return nil, mfadom.ErrPasskeyInvalid
	}
	cred, err := s.wa.CreateCredential(wu, *session, parsed)
	if err != nil {
		return nil, mfadom.ErrPasskeyInvalid
	}
	raw, err := json.Marshal(cred)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Passkey"
	}
	if r := []rune(name); len(r) > maxPasskeyName {
		name = string(r[:maxPasskeyName])
	}
	p := &mfadom.Passkey{
		ID: uuid.New(), UserID: u.ID, Name: name,
		CredentialID: cred.ID, Credential: raw, CreatedAt: s.now(),
	}
	if err := s.passkeys.AddPasskey(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// BeginPasskeyVerification returns the WebAuthn request options for proving,
// from a signed-in session, that the user holds one of their passkeys.
func (s *Service) BeginPasskeyVerification(ctx context.Context, u *userdom.User) (json.RawMessage, error) {
	if s.wa == nil {
		return nil, mfadom.ErrPasskeyUnavailable
	}
	// Signed in already, so "no passkey" is a 404-style answer, not the 401
	// a login gives (which would trigger a token refresh).
	if has, err := s.hasPasskeys(ctx, u.ID); err != nil {
		return nil, err
	} else if !has {
		return nil, mfadom.ErrPasskeyNotFound
	}
	// Removing a second factor needs more than a tap on a stolen, unlocked key.
	return s.beginAssertion(ctx, u, "verify:"+u.ID.String(), webauthn.WithUserVerification(protocol.VerificationRequired))
}

// DeletePasskey removes one of u's passkeys, on the strength of an assertion
// from any of u's passkeys (itself included) over the options
// BeginPasskeyVerification issued. A session alone can't remove a second
// factor, the same bar turning TOTP off sets with a code.
func (s *Service) DeletePasskey(ctx context.Context, u *userdom.User, id uuid.UUID, response []byte) error {
	if s.wa == nil {
		return mfadom.ErrPasskeyUnavailable
	}
	if err := s.verifyAssertion(ctx, u, "verify:"+u.ID.String(), response, mfadom.ErrPasskeyInvalid); err != nil {
		return err
	}
	return s.passkeys.DeletePasskey(ctx, u.ID, id)
}

// BeginPasskeyLogin returns the options to hand to navigator.credentials.get
// for the login challenge ceremonyID (the challenge token's id).
func (s *Service) BeginPasskeyLogin(ctx context.Context, u *userdom.User, ceremonyID string) (json.RawMessage, error) {
	return s.beginAssertion(ctx, u, "login:"+ceremonyID)
}

// VerifyPasskeyLogin checks the authenticator's assertion for ceremonyID,
// enforcing SSO-only mode and the IP allow-lists first, and records the new
// sign counter. The passkey is the second factor of a password sign-in, so
// SSO-only mode applies to it as it does to CheckLogin.
//
// A wrong assertion is not counted toward the TOTP lockout: it is a signature
// over a fresh server challenge, so there is nothing to guess.
func (s *Service) VerifyPasskeyLogin(ctx context.Context, u *userdom.User, ceremonyID string, response []byte, clientIP string) error {
	policy, err := s.store.GetPolicy(ctx)
	if err != nil {
		return err
	}
	if err := passwordAllowed(policy, u); err != nil {
		return err
	}
	if err := s.checkIP(ctx, u, policy, hasAny(u.RoleNames(), policy.PrivilegedRoles), clientIP); err != nil {
		return err
	}
	if err := s.verifyAssertion(ctx, u, "login:"+ceremonyID, response, mfadom.ErrInvalidCode); err != nil {
		return err
	}
	logSSOOnlyPasswordSignIn(policy, u, clientIP)
	return nil
}

// beginAssertion parks a fresh challenge under key and returns the request
// options for u's passkeys.
func (s *Service) beginAssertion(ctx context.Context, u *userdom.User, key string, opts ...webauthn.LoginOption) (json.RawMessage, error) {
	wu, _, err := s.loadUser(ctx, u)
	if err != nil {
		return nil, err
	}
	if len(wu.creds) == 0 {
		return nil, mfadom.ErrInvalidCode
	}
	assertion, session, err := s.wa.BeginLogin(wu, opts...)
	if err != nil {
		return nil, fmt.Errorf("mfa: begin passkey assertion: %w", err)
	}
	if err := s.putCeremony(ctx, key, session); err != nil {
		return nil, err
	}
	return json.Marshal(assertion)
}

// verifyAssertion consumes the challenge under key and validates the
// authenticator's response against u's passkeys, recording the new sign
// counter. Every failure is invalid, which differs by caller: a login answers
// 401, a signed-in request must not (a 401 would trigger a token refresh).
func (s *Service) verifyAssertion(ctx context.Context, u *userdom.User, key string, response []byte, invalid error) error {
	wu, list, err := s.loadUser(ctx, u)
	if err != nil {
		return err
	}
	session, err := s.takeCeremony(ctx, key, invalid)
	if err != nil {
		return err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(response)
	if err != nil {
		return invalid
	}
	cred, err := s.wa.ValidateLogin(wu, *session, parsed)
	if err != nil {
		return invalid
	}
	if cred.Authenticator.CloneWarning {
		// The counter went backwards: the credential was likely copied.
		return invalid
	}
	for _, p := range list {
		if string(p.CredentialID) != string(cred.ID) {
			continue
		}
		raw, err := json.Marshal(cred)
		if err != nil {
			return err
		}
		return s.passkeys.UpdatePasskeyCredential(ctx, p.ID, raw, s.now())
	}
	return invalid
}

// putCeremony parks session under key. Keys are per account (or per login
// challenge), not per ceremony: a second begin replaces the first, so two open
// tabs race and the loser's finish is refused as PASSKEY_INVALID.
func (s *Service) putCeremony(ctx context.Context, key string, session *webauthn.SessionData) error {
	b, err := json.Marshal(session)
	if err != nil {
		return err
	}
	return s.ceremonies.Put(ctx, key, b, ceremonyTTL)
}

// takeCeremony consumes the parked challenge; an absent one (expired, already
// used, or never begun) is an invalid attempt.
func (s *Service) takeCeremony(ctx context.Context, key string, invalid error) (*webauthn.SessionData, error) {
	b, err := s.ceremonies.Take(ctx, key)
	if err != nil {
		return nil, err
	}
	if b == nil {
		return nil, invalid
	}
	var session webauthn.SessionData
	if err := json.Unmarshal(b, &session); err != nil {
		return nil, invalid
	}
	return &session, nil
}
