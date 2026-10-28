package gitsvc

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	gitdom "github.com/Paca-AI/api/internal/domain/git"
	"github.com/Paca-AI/api/internal/platform/gitprovider"
)

// Repository webhooks. A connection of a kind whose platform delivers events
// per repository (a personal access token on GitHub) has no app-level webhook
// like a GitHub App: the provider registers one on each repository it is linked
// to, pointing at the connection's own endpoint. The provider holds the signing
// secret (chosen by the user when the connection was created) and verifies the
// deliveries; core only says where and when.

func hasWebhook(r *gitdom.Repository) bool {
	return len(r.WebhookRef) > 2 // "{}" and "null" are empty
}

// needsRepoWebhook reports whether events for repositories linked through the
// connection are delivered per repository.
func (s *Service) needsRepoWebhook(ctx context.Context, ident *gitdom.Identity) (bool, gitprovider.Info) {
	info, err := s.providers.Describe(ctx, ident.ProviderID)
	if err != nil {
		return false, info
	}
	profile := info.Capabilities.Profile(gitprovider.IdentityKind(ident.Kind))
	return profile.WebhookScope == gitprovider.WebhookRepo, info
}

// webhookURL is the public URL the connection's deliveries go to; empty when
// the connection has no endpoint or the instance has no public URL.
func (s *Service) webhookURL(ctx context.Context, ident *gitdom.Identity) string {
	if s.publicURL == "" {
		return ""
	}
	ep, err := s.store.FindWebhookEndpointForIdentity(ctx, ident.ID)
	if err != nil {
		return ""
	}
	return s.publicURL + WebhookPathPrefix + ep.Key
}

// ensureWebhook registers the repository's webhook with the provider and
// records it. It returns nil when the connection needs none (nothing to do) or
// when one is in place, and an error saying why not otherwise.
func (s *Service) ensureWebhook(ctx context.Context, r *gitdom.Repository, ident *gitdom.Identity) error {
	need, info := s.needsRepoWebhook(ctx, ident)
	if !need {
		return nil
	}
	if !info.Capabilities.WebhookManagement {
		return gitdom.ErrUnsupported
	}
	url := s.webhookURL(ctx, ident)
	if url == "" {
		return &gitprovider.Error{Code: gitprovider.CodeUnsupported, Message: "this instance has no public URL, so the platform cannot reach it"}
	}
	prov, err := s.providers.Provider(ctx, r.ProviderID)
	if err != nil {
		return err
	}
	ref, err := prov.EnsureWebhook(ctx, gitprovider.EnsureWebhookRequest{
		Target: gitprovider.Target{Identity: identityRef(ident), Repo: repoRef(r)}, URL: url,
	})
	s.audit(ctx, r, nil, "webhook.register", "", err)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(ref)
	if err != nil {
		return err
	}
	if err := s.store.SetRepositoryWebhook(ctx, r.ID, raw); err != nil {
		return err
	}
	r.WebhookRef = raw
	return nil
}

// registerWebhook is ensureWebhook for linking: failing is not failing to link,
// the repository is listed with the URL to register by hand.
func (s *Service) registerWebhook(ctx context.Context, r *gitdom.Repository, ident *gitdom.Identity) {
	_ = s.ensureWebhook(ctx, r, ident)
}

// RegisterWebhook retries registering a linked repository's webhook, for when
// the first attempt failed (a token without the right to manage webhooks) and
// the user has since fixed it. The error says why it still cannot be.
func (s *Service) RegisterWebhook(ctx context.Context, projectID, repositoryID uuid.UUID) (*gitdom.Repository, error) {
	repo, err := s.store.FindRepository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	if repo.ProjectID != projectID {
		return nil, gitdom.ErrNotFound
	}
	if repo.IdentityID == nil {
		return nil, gitdom.ErrNoIdentity
	}
	ident, err := s.store.FindIdentity(ctx, *repo.IdentityID)
	if err != nil {
		return nil, gitdom.ErrNoIdentity
	}
	if need, _ := s.needsRepoWebhook(ctx, ident); !need {
		return nil, gitdom.ErrUnsupported
	}
	if err := s.ensureWebhook(ctx, repo, ident); err != nil {
		return nil, err
	}
	return repo, nil
}

// unregisterWebhook removes the webhook of a repository that was just unlinked
// (or is about to be rebound to another connection), unless another project still links it through the same connection (they share
// one webhook). Best effort: a webhook that stays only sends events nobody
// routes.
func (s *Service) unregisterWebhook(ctx context.Context, r *gitdom.Repository) {
	if !hasWebhook(r) || r.IdentityID == nil {
		return
	}
	others, err := s.store.FindLinkedRepositories(ctx, r.ProviderID, r.Host, r.FullPath)
	if err != nil {
		return
	}
	for _, o := range others {
		if o.ID != r.ID && o.IdentityID != nil && *o.IdentityID == *r.IdentityID {
			return
		}
	}
	ident, err := s.store.FindIdentity(ctx, *r.IdentityID)
	if err != nil {
		return
	}
	prov, err := s.providers.Provider(ctx, r.ProviderID)
	if err != nil {
		return
	}
	var ref gitprovider.WebhookRef
	if json.Unmarshal(r.WebhookRef, &ref) != nil || ref.ID == "" {
		return
	}
	err = prov.RemoveWebhook(ctx, gitprovider.RemoveWebhookRequest{
		Target: gitprovider.Target{Identity: identityRef(ident), Repo: repoRef(r)}, Webhook: ref,
	})
	s.audit(ctx, r, nil, "webhook.remove", "", err)
}

// removeConnectionWebhooks removes the webhooks registered through a connection
// that is about to be forgotten: afterwards nothing could, as removing one takes
// the connection's credential. Best effort.
func (s *Service) removeConnectionWebhooks(ctx context.Context, ident *gitdom.Identity) {
	repos, err := s.store.ListIdentityRepositories(ctx, ident.ID)
	if err != nil {
		return
	}
	prov, err := s.providers.Provider(ctx, ident.ProviderID)
	if err != nil {
		return
	}
	for _, r := range repos {
		var ref gitprovider.WebhookRef
		if !hasWebhook(r) || json.Unmarshal(r.WebhookRef, &ref) != nil || ref.ID == "" {
			continue
		}
		err := prov.RemoveWebhook(ctx, gitprovider.RemoveWebhookRequest{
			Target: gitprovider.Target{Identity: identityRef(ident), Repo: repoRef(r)}, Webhook: ref,
		})
		s.audit(ctx, r, nil, "webhook.remove", "", err)
	}
}

// webhookNeed is what ListRepositories learns once per connection.
type webhookNeed struct {
	need bool
	url  string
}
