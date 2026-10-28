package gitsvc

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	gitdom "github.com/Paca-AI/api/internal/domain/git"
	"github.com/Paca-AI/api/internal/platform/gitprovider"
)

// AccessInput starts granting the host's shared identity access to one more
// account or organization, on behalf of a project that wants to link a
// repository there.
type AccessInput struct {
	ProjectID  uuid.UUID
	ProviderID string
	Host       string // empty: the provider's first default host
	Owner      string // the account or organization, when known
	// IdentityID names the connection to grant access to. Empty: the project's
	// only connection for the provider and host.
	IdentityID *uuid.UUID
	// AgentID, when set, grants the agent's own identity instead of the host's
	// shared one (the agent must have one).
	AgentID   *uuid.UUID
	ActorID   uuid.UUID
	ReturnURL string // must be on this instance
}

// BeginAccessGrant starts the platform round trip that lets the shared identity
// see another owner's repositories (GitHub: install the App on that account).
// The grant is made by the user on the platform, which enforces who may do it;
// core only remembers who started it and for which project.
func (s *Service) BeginAccessGrant(ctx context.Context, in AccessInput) (*gitprovider.SetupStep, error) {
	if s.publicURL == "" {
		return nil, gitdom.ErrPublicURLRequired
	}
	if in.ReturnURL != "" && !sameOrigin(s.publicURL, in.ReturnURL) {
		return nil, gitdom.ErrInvalidReturnURL
	}
	info, err := s.providers.Describe(ctx, in.ProviderID)
	if err != nil {
		return nil, err
	}
	if !info.Capabilities.AccessGrants {
		return nil, &gitprovider.Error{Code: gitprovider.CodeUnsupported, Message: "this provider does not need access grants"}
	}
	prov, err := s.providers.Provider(ctx, in.ProviderID)
	if err != nil {
		return nil, err
	}
	host := strings.TrimSpace(in.Host)
	if host == "" && len(info.Hosts) > 0 {
		host = info.Hosts[0]
	}
	var ident *gitdom.Identity
	switch {
	case in.AgentID != nil:
		ident, err = s.store.FindAgentIdentity(ctx, *in.AgentID, in.ProviderID)
	case in.IdentityID != nil:
		ident, err = s.visibleIdentity(ctx, in.ProjectID, *in.IdentityID)
		// An explicit connection must be of the platform asked for, as the default path guarantees.
		if err == nil && (ident.ProviderID != in.ProviderID || (strings.TrimSpace(in.Host) != "" && !strings.EqualFold(ident.Host, host))) {
			err = gitdom.ErrNotFound
		}
	default:
		ident, err = s.onlyConnection(ctx, in.ProjectID, in.ProviderID, host)
	}
	if errors.Is(err, gitdom.ErrNotFound) {
		return nil, gitdom.ErrNoIdentity
	}
	if err != nil {
		return nil, err
	}
	step, err := prov.BeginAccessGrant(ctx, gitprovider.BeginAccessRequest{
		Identity: identityRef(ident), Owner: strings.TrimSpace(in.Owner), ReturnURL: in.ReturnURL, CallbackURL: s.publicURL + SetupCallbackPath,
	})
	if err != nil {
		return nil, err
	}
	if step.State == "" {
		return nil, errors.New("git: provider returned no access grant state")
	}
	if err := s.store.CreateAccessSession(ctx, &gitdom.AccessSession{
		State: step.State, ProviderID: in.ProviderID, IdentityID: ident.ID, ProjectID: in.ProjectID, Owner: strings.TrimSpace(in.Owner),
		ReturnURL: in.ReturnURL, CreatedBy: in.ActorID, ExpiresAt: s.now().Add(setupSessionTTL),
	}); err != nil {
		return nil, err
	}
	return &step, nil
}

// AccessCompletion is the outcome of a finished grant.
type AccessCompletion struct {
	Owner     string // the account or organization now covered
	ReturnURL string
}

// CompleteAccessGrant finishes a grant from the platform's redirect. The state
// must be the caller's own, unused, unexpired and started from this project.
func (s *Service) CompleteAccessGrant(ctx context.Context, actorID, projectID uuid.UUID, state string, params map[string]string) (*AccessCompletion, error) {
	sess, err := s.store.FindAccessSession(ctx, state)
	if err != nil {
		if errors.Is(err, gitdom.ErrNotFound) {
			return nil, gitdom.ErrSetupExpired
		}
		return nil, err
	}
	if sess.CreatedBy != actorID || sess.ProjectID != projectID || sess.CompletedAt != nil || !s.now().Before(sess.ExpiresAt) {
		return nil, gitdom.ErrSetupExpired
	}
	ident, err := s.store.FindIdentity(ctx, sess.IdentityID)
	if err != nil {
		return nil, err
	}
	prov, err := s.providers.Provider(ctx, sess.ProviderID)
	if err != nil {
		return nil, err
	}
	grant, err := prov.CompleteAccessGrant(ctx, gitprovider.CompleteAccessRequest{Identity: identityRef(ident), State: state, Params: params})
	if err != nil {
		return nil, err
	}
	if err := s.store.CompleteAccessSession(ctx, state); err != nil {
		return nil, err
	}
	// Credentials minted before the grant could not see the new owner; they are
	// per repository, so nothing cached needs dropping.
	return &AccessCompletion{Owner: grant.Owner, ReturnURL: sess.ReturnURL}, nil
}

// onlyConnection returns the one connection of a provider host the project can
// use, ErrIdentityRequired when there are several and ErrNotFound when none.
func (s *Service) onlyConnection(ctx context.Context, projectID uuid.UUID, providerID, host string) (*gitdom.Identity, error) {
	all, err := s.store.ListProjectIdentities(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var found []*gitdom.Identity
	for _, id := range all {
		if id.ProviderID == providerID && strings.EqualFold(id.Host, host) {
			found = append(found, id)
		}
	}
	switch len(found) {
	case 0:
		return nil, gitdom.ErrNotFound
	case 1:
		return found[0], nil
	}
	return nil, gitdom.ErrIdentityRequired
}
