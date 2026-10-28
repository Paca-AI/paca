package gitsvc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	gitdom "github.com/Paca-AI/api/internal/domain/git"
	"github.com/Paca-AI/api/internal/platform/gitprovider"
)

const (
	// setupSessionTTL bounds how long a user has to finish a platform round trip.
	setupSessionTTL = time.Hour
	// SetupCallbackPath is the Paca web page the platform redirects the browser
	// to; it passes the parameters to CompleteIdentitySetup.
	SetupCallbackPath = "/git/setup/callback"
	// WebhookPathPrefix is where core receives platform webhooks.
	WebhookPathPrefix = "/api/v1/git/webhooks/"
)

// SetupInput starts creating a bot identity.
type SetupInput struct {
	ProviderID string
	Host       string // empty: the provider's first default host
	Scope      gitdom.IdentityScope
	AgentID    *uuid.UUID // required for ScopeAgent
	ProjectID  *uuid.UUID // required for ScopeProject
	AgentName  string
	ActorID    uuid.UUID
	ReturnURL  string // where the browser lands when setup is finished; must be on this instance
	Params     map[string]string
}

// providerScope is the scope a provider is told about. A project connection is
// a Paca visibility rule; to the platform it is as shared as a workspace one.
func providerScope(s gitdom.IdentityScope) gitprovider.SetupScope {
	if s == gitdom.ScopeAgent {
		return gitprovider.SetupAgent
	}
	return gitprovider.SetupShared
}

func randomKey(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// sameOrigin reports whether candidate is on the same scheme and host as base.
func sameOrigin(base, candidate string) bool {
	b, err1 := url.Parse(base)
	c, err2 := url.Parse(candidate)
	if err1 != nil || err2 != nil || c.Scheme == "" || c.Host == "" {
		return false
	}
	return strings.EqualFold(b.Scheme, c.Scheme) && strings.EqualFold(b.Host, c.Host)
}

// BeginIdentitySetup starts a platform round trip. It reserves the webhook
// endpoint the identity will own, so the URL can be given to the platform
// before the identity exists, and returns the provider's next step for the
// browser (a redirect, a form to POST, or a token form).
func (s *Service) BeginIdentitySetup(ctx context.Context, in SetupInput) (*gitprovider.SetupStep, error) {
	if s.publicURL == "" {
		return nil, gitdom.ErrPublicURLRequired
	}
	if in.ReturnURL != "" && !sameOrigin(s.publicURL, in.ReturnURL) {
		return nil, gitdom.ErrInvalidReturnURL
	}
	switch in.Scope {
	case gitdom.ScopeShared, gitdom.ScopeAgent, gitdom.ScopeProject:
	default:
		return nil, errors.New("git: scope must be shared, project or agent")
	}
	if in.Scope == gitdom.ScopeProject && in.ProjectID == nil {
		return nil, errors.New("git: a project is required for a project connection")
	}
	if in.Scope == gitdom.ScopeAgent && in.AgentID == nil {
		return nil, errors.New("git: an agent is required for an agent identity")
	}
	info, err := s.providers.Describe(ctx, in.ProviderID)
	if err != nil {
		return nil, err
	}
	prov, err := s.providers.Provider(ctx, in.ProviderID)
	if err != nil {
		return nil, err
	}
	host := strings.TrimSpace(in.Host)
	if host == "" && len(info.Hosts) > 0 {
		host = info.Hosts[0]
	}

	// Workspace and project connections may be several per host (two Apps, an
	// App and a token); an agent has at most one identity per provider.
	if in.Scope == gitdom.ScopeAgent {
		if _, err := s.store.FindAgentIdentity(ctx, *in.AgentID, in.ProviderID); err == nil {
			return nil, gitdom.ErrIdentityExists
		} else if !errors.Is(err, gitdom.ErrNotFound) {
			return nil, err
		}
	}

	key, err := randomKey(24)
	if err != nil {
		return nil, err
	}
	if err := s.store.CreateWebhookEndpoint(ctx, &gitdom.WebhookEndpoint{Key: key, ProviderID: in.ProviderID}); err != nil {
		return nil, err
	}
	req := gitprovider.BeginSetupRequest{
		Scope: providerScope(in.Scope), Host: host, AgentName: in.AgentName,
		ReturnURL: in.ReturnURL, CallbackURL: s.publicURL + SetupCallbackPath,
		WebhookURL: s.publicURL + WebhookPathPrefix + key, Params: in.Params,
	}
	if in.AgentID != nil {
		req.AgentID = in.AgentID.String()
	}
	step, err := prov.BeginIdentitySetup(ctx, req)
	if err != nil {
		_ = s.store.DeleteWebhookEndpoint(ctx, key) // nothing was created on the platform
		return nil, err
	}
	sess := &gitdom.SetupSession{
		State: step.State, ProviderID: in.ProviderID, Host: host, Scope: in.Scope, AgentID: in.AgentID, ProjectID: in.ProjectID,
		EndpointKey: key, ReturnURL: in.ReturnURL, CreatedBy: in.ActorID, ExpiresAt: s.now().Add(setupSessionTTL),
	}
	if step.Kind == gitprovider.StepDone {
		// The provider needed no round trip (for example a pre-issued token).
		if step.Identity == nil {
			_ = s.store.DeleteWebhookEndpoint(ctx, key)
			return nil, errors.New("git: provider finished setup without an identity")
		}
		if _, err := s.finishIdentity(ctx, sess, *step.Identity, in.ActorID); err != nil {
			return nil, err
		}
		return &step, nil
	}
	if step.State == "" {
		_ = s.store.DeleteWebhookEndpoint(ctx, key)
		return nil, errors.New("git: provider returned no setup state")
	}
	if err := s.store.CreateSetupSession(ctx, sess); err != nil {
		_ = s.store.DeleteWebhookEndpoint(ctx, key)
		return nil, err
	}
	return &step, nil
}

// SetupCompletion is the outcome of one leg of identity setup.
type SetupCompletion struct {
	Identity  *gitdom.Identity
	Next      *gitprovider.SetupStep // a further browser step (GitHub: install the app); nil when finished
	ReturnURL string
}

// CompleteIdentitySetup finishes one leg with the parameters the platform (or
// the user's pasted token) supplied. Only the user who began the flow can
// complete it, and a finished or expired state is useless.
//
// projectID is the project the route was reached through, nil for the workspace
// route; the session must have been started for the same place, so a flow begun
// as a workspace connection cannot be finished through a project's gate or the
// other way round.
func (s *Service) CompleteIdentitySetup(ctx context.Context, actorID uuid.UUID, projectID *uuid.UUID, state string, params map[string]string) (*SetupCompletion, error) {
	sess, err := s.store.FindSetupSession(ctx, state)
	if err != nil {
		if errors.Is(err, gitdom.ErrNotFound) {
			return nil, gitdom.ErrSetupExpired
		}
		return nil, err
	}
	if sess.CreatedBy != actorID || sess.CompletedAt != nil || !s.now().Before(sess.ExpiresAt) || !sameProject(sess.ProjectID, projectID) {
		return nil, gitdom.ErrSetupExpired
	}
	prov, err := s.providers.Provider(ctx, sess.ProviderID)
	if err != nil {
		return nil, err
	}
	ident, err := prov.CompleteIdentitySetup(ctx, gitprovider.CompleteSetupRequest{
		Scope: providerScope(sess.Scope), Host: sess.Host, State: state, Params: params,
	})
	if err != nil {
		return nil, err
	}
	return s.finishIdentity(ctx, sess, ident, actorID)
}

func sameProject(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

var errIdentityCollision = errors.New("git: the provider reported an identity that belongs to another connection")

// sameConnection reports (as an error when not) whether a stored identity is the
// one this setup session is creating: same scope, project and host, and for an
// agent the identity already mapped to that agent.
func (s *Service) sameConnection(ctx context.Context, stored *gitdom.Identity, sess *gitdom.SetupSession, ident gitprovider.Identity) error {
	host := ident.Host
	if host == "" {
		host = sess.Host
	}
	if stored.Scope != sess.Scope || !sameProject(stored.ProjectID, sess.ProjectID) || !strings.EqualFold(stored.Host, host) {
		return errIdentityCollision
	}
	if sess.Scope == gitdom.ScopeAgent && sess.AgentID != nil {
		mapped, err := s.store.FindAgentIdentity(ctx, *sess.AgentID, sess.ProviderID)
		if errors.Is(err, gitdom.ErrNotFound) || (err == nil && mapped.ID != stored.ID) {
			return errIdentityCollision
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// finishIdentity records the identity the provider reports, binds the pending
// webhook endpoint to it and, for an agent, maps it. It is idempotent: the
// provider reports the same identity on every leg.
func (s *Service) finishIdentity(ctx context.Context, sess *gitdom.SetupSession, ident gitprovider.Identity, actorID uuid.UUID) (*SetupCompletion, error) {
	stored, err := s.store.FindIdentityByProviderRef(ctx, sess.ProviderID, ident.Ref.ID)
	if err == nil {
		// IdentityRef.ID is unique per connection, so a match belongs to this
		// setup. One that does not (another scope, project, host or agent) means
		// the provider reused an id: never merge two connections into one.
		if cerr := s.sameConnection(ctx, stored, sess, ident); cerr != nil {
			return nil, cerr
		}
	}
	if errors.Is(err, gitdom.ErrNotFound) {
		host := ident.Host
		if host == "" {
			host = sess.Host
		}
		stored = &gitdom.Identity{
			ProviderID: sess.ProviderID, Host: host, ProviderIdentityID: ident.Ref.ID, Kind: string(ident.Kind), Scope: sess.Scope, ProjectID: sess.ProjectID,
			Handle: ident.Ref.Handle, DisplayName: ident.DisplayName, ProviderRef: ident.Ref.ProviderRef, CreatedBy: &actorID,
		}
		if !ident.ExpiresAt.IsZero() {
			t := ident.ExpiresAt
			stored.ExpiresAt = &t
		}
		if err := s.store.CreateIdentity(ctx, stored); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if err := s.store.BindWebhookEndpoint(ctx, sess.EndpointKey, stored.ID); err != nil {
		return nil, err
	}
	if sess.Scope == gitdom.ScopeAgent && sess.AgentID != nil {
		if err := s.store.SetAgentIdentity(ctx, *sess.AgentID, sess.ProviderID, stored.ID); err != nil {
			return nil, err
		}
	}
	out := &SetupCompletion{Identity: stored, ReturnURL: sess.ReturnURL}
	if ident.Next != nil {
		out.Next = ident.Next
		return out, nil
	}
	if sess.State != "" {
		if err := s.store.CompleteSetupSession(ctx, sess.State); err != nil && !errors.Is(err, gitdom.ErrNotFound) {
			return nil, err
		}
	}
	return out, nil
}

// ListProviders describes the enabled Git providers.
func (s *Service) ListProviders(ctx context.Context) ([]gitprovider.Info, error) {
	return s.providers.List(ctx)
}

// ListIdentities returns the shared identities of every enabled provider.
func (s *Service) ListIdentities(ctx context.Context) ([]*gitdom.Identity, error) {
	infos, err := s.providers.List(ctx)
	if err != nil {
		return nil, err
	}
	var out []*gitdom.Identity
	for _, info := range infos {
		ids, err := s.store.ListSharedIdentities(ctx, info.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, ids...)
	}
	return out, nil
}

// ListConnections returns the connections a project can use: the workspace's
// and its own, of every enabled provider.
func (s *Service) ListConnections(ctx context.Context, projectID uuid.UUID) ([]*gitdom.Identity, error) {
	ids, err := s.store.ListProjectIdentities(ctx, projectID)
	if err != nil {
		return nil, err
	}
	enabled, err := s.enabledProviders(ctx)
	if err != nil {
		return nil, err
	}
	out := ids[:0:0]
	var all []uuid.UUID
	for _, id := range ids {
		if enabled[id.ProviderID] {
			out = append(out, id)
			all = append(all, id.ID)
		}
	}
	counts, err := s.store.CountRepositories(ctx, all)
	if err != nil {
		return nil, err
	}
	for _, id := range out {
		id.RepositoryCount = counts[id.ID]
	}
	return out, nil
}

func (s *Service) enabledProviders(ctx context.Context) (map[string]bool, error) {
	infos, err := s.providers.List(ctx)
	if err != nil {
		return nil, err
	}
	m := make(map[string]bool, len(infos))
	for _, i := range infos {
		m[i.ID] = true
	}
	return m, nil
}

// DeleteProjectConnection forgets a connection that belongs to one project. A
// workspace connection is not the project's to delete: it answers not found.
func (s *Service) DeleteProjectConnection(ctx context.Context, projectID, id uuid.UUID) error {
	ident, err := s.store.FindIdentity(ctx, id)
	if err != nil {
		return err
	}
	if ident.Scope != gitdom.ScopeProject || ident.ProjectID == nil || *ident.ProjectID != projectID {
		return gitdom.ErrNotFound
	}
	return s.DeleteIdentity(ctx, id)
}

// PurgeProjectConnections forgets the connections that belong to a project that
// is being deleted, so their credentials and repository webhooks do not outlive
// it. Every connection is tried; the first error is returned.
func (s *Service) PurgeProjectConnections(ctx context.Context, projectID uuid.UUID) error {
	ids, err := s.store.ListProjectIdentities(ctx, projectID)
	if err != nil {
		return err
	}
	var first error
	for _, id := range ids {
		if id.Scope != gitdom.ScopeProject || id.ProjectID == nil || *id.ProjectID != projectID {
			continue
		}
		if err := s.DeleteIdentity(ctx, id.ID); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// visibleIdentity returns the connection with the given id when the project can use it.
func (s *Service) visibleIdentity(ctx context.Context, projectID, id uuid.UUID) (*gitdom.Identity, error) {
	ident, err := s.store.FindIdentity(ctx, id)
	if err != nil {
		return nil, err
	}
	switch {
	case ident.Scope == gitdom.ScopeShared:
	case ident.Scope == gitdom.ScopeProject && ident.ProjectID != nil && *ident.ProjectID == projectID:
	default:
		return nil, gitdom.ErrNotFound // another project's, or an agent's
	}
	return ident, nil
}

// ListAgentIdentities returns the agent's own identity for each provider that has one.
func (s *Service) ListAgentIdentities(ctx context.Context, agentID uuid.UUID) ([]*gitdom.Identity, error) {
	infos, err := s.providers.List(ctx)
	if err != nil {
		return nil, err
	}
	var out []*gitdom.Identity
	for _, info := range infos {
		id, err := s.store.FindAgentIdentity(ctx, agentID, info.ID)
		if errors.Is(err, gitdom.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

// DeleteIdentity forgets an identity: the provider drops its credentials, then
// the record (and with it the webhook endpoint) goes. Repositories that used it
// lose their credential: they stay linked but cannot be used until they are
// linked again through another connection. An unavailable
// provider plugin does not block cleanup.
func (s *Service) DeleteIdentity(ctx context.Context, id uuid.UUID) error {
	ident, err := s.store.FindIdentity(ctx, id)
	if err != nil {
		return err
	}
	s.removeConnectionWebhooks(ctx, ident)
	prov, err := s.providers.Provider(ctx, ident.ProviderID)
	switch {
	case err == nil:
		derr := prov.DeleteIdentity(ctx, gitprovider.DeleteIdentityRequest{Identity: identityRef(ident)})
		if derr != nil && !gitprovider.IsCode(derr, gitprovider.CodeUnsupported) && !gitprovider.IsCode(derr, gitprovider.CodeNotFound) {
			return derr
		}
	case errors.Is(err, gitdom.ErrProviderUnavailable):
	default:
		return err
	}
	return s.store.DeleteIdentity(ctx, id)
}

// RemoveAgentIdentity unmaps and deletes an agent's own identity.
func (s *Service) RemoveAgentIdentity(ctx context.Context, agentID uuid.UUID, providerID string) error {
	ident, err := s.store.FindAgentIdentity(ctx, agentID, providerID)
	if err != nil {
		return err
	}
	if err := s.store.DeleteAgentIdentity(ctx, agentID, providerID); err != nil {
		return err
	}
	if ident.Scope == gitdom.ScopeAgent {
		return s.DeleteIdentity(ctx, ident.ID)
	}
	return nil
}
