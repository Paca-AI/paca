package gitsvc

import (
	"context"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/platform/gitprovider"
)

// DefaultAvailablePageSize is how many repositories a page of the picker asks
// the provider for when the caller does not say.
const DefaultAvailablePageSize = 30

// AvailableRepositories is one page of the repositories a connection can see.
type AvailableRepositories struct {
	Items      []gitprovider.Repository
	NextCursor string
	// Supported is false when the provider cannot list repositories; the caller
	// falls back to asking for a path or URL.
	Supported bool
}

// ListAvailableRepositories pages the repositories a connection sees on the
// platform, filtered by query, for the picker in the link form. The connection
// must be one the project can use (the workspace's or its own); any other is
// reported as not found. A provider without a repository listing is not an
// error: the page comes back empty and unsupported.
func (s *Service) ListAvailableRepositories(ctx context.Context, projectID, identityID uuid.UUID, query, cursor string) (*AvailableRepositories, error) {
	ident, err := s.visibleIdentity(ctx, projectID, identityID)
	if err != nil {
		return nil, err
	}
	prov, err := s.providers.Provider(ctx, ident.ProviderID)
	if err != nil {
		return nil, err
	}
	page, err := prov.ListRepositories(ctx, gitprovider.ListReposRequest{
		Identity: identityRef(ident), Query: query, Cursor: cursor, PageSize: DefaultAvailablePageSize,
	})
	if err != nil {
		if gitprovider.IsCode(err, gitprovider.CodeUnsupported) {
			return &AvailableRepositories{Items: []gitprovider.Repository{}}, nil
		}
		return nil, err
	}
	if page.Items == nil {
		page.Items = []gitprovider.Repository{}
	}
	return &AvailableRepositories{Items: page.Items, NextCursor: page.NextCursor, Supported: true}, nil
}
