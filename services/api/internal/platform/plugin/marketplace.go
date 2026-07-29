package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Paca-AI/api/internal/platform/netguard"
)

var (
	// ErrMarketplacePluginNotFound indicates the plugin name was not found in the catalog.
	ErrMarketplacePluginNotFound = errors.New("marketplace plugin not found")
)

// MarketplaceCatalog is the root payload served by the marketplace repository.
type MarketplaceCatalog struct {
	SchemaVersion int                 `json:"schema_version"`
	Source        string              `json:"source,omitempty"`
	GeneratedAt   string              `json:"generated_at,omitempty"`
	Plugins       []MarketplacePlugin `json:"plugins"`
}

// MarketplacePlugin is one installable plugin entry from the catalog.
type MarketplacePlugin struct {
	Name          string                    `json:"name"`
	DisplayName   string                    `json:"display_name"`
	Description   string                    `json:"description"`
	Version       string                    `json:"version"`
	AvatarURL     string                    `json:"avatar_url,omitempty"`
	RepositoryURL string                    `json:"repository_url,omitempty"`
	Artifacts     MarketplacePluginArtifact `json:"artifacts"`
}

// MarketplacePluginArtifact contains downloadable plugin package URLs.
// All fields are optional; the installer only processes artifacts whose URL is non-empty.
type MarketplacePluginArtifact struct {
	BackendTarGzURL    string `json:"backend_tar_gz_url,omitempty"`
	FrontendTarGzURL   string `json:"frontend_tar_gz_url,omitempty"`
	MigrationsTarGzURL string `json:"migrations_tar_gz_url,omitempty"`
	ManifestTarGzURL   string `json:"manifest_tar_gz_url"`
	MCPTarGzURL        string `json:"mcp_tar_gz_url,omitempty"`
	SkillsTarGzURL     string `json:"skills_tar_gz_url,omitempty"`
}

// MarketplaceClient fetches the plugin catalog from a public URL.
type MarketplaceClient struct {
	catalogURL string
	httpClient *http.Client
}

// NewMarketplaceClient constructs a client for the given catalog URL.
func NewMarketplaceClient(catalogURL string, timeout time.Duration) *MarketplaceClient {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &MarketplaceClient{
		catalogURL: catalogURL,
		httpClient: &http.Client{Timeout: timeout, Transport: netguard.NewSafeHTTPTransport()},
	}
}

// List returns the catalog with basic validation.
func (c *MarketplaceClient) List(ctx context.Context) (*MarketplaceCatalog, error) {
	if strings.TrimSpace(c.catalogURL) == "" {
		return nil, fmt.Errorf("marketplace catalog URL is not configured")
	}

	if err := validateMarketplaceURL(ctx, c.catalogURL); err != nil {
		return nil, fmt.Errorf("marketplace: invalid catalog URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.catalogURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("marketplace: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("marketplace: fetch catalog: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("marketplace: catalog HTTP status %d", resp.StatusCode)
	}

	var catalog MarketplaceCatalog
	if err := json.NewDecoder(resp.Body).Decode(&catalog); err != nil {
		return nil, fmt.Errorf("marketplace: decode catalog: %w", err)
	}

	for i := range catalog.Plugins {
		if err := validateMarketplacePlugin(ctx, catalog.Plugins[i]); err != nil {
			return nil, fmt.Errorf("marketplace: invalid plugin entry %q: %w", catalog.Plugins[i].Name, err)
		}
	}

	return &catalog, nil
}

// FindPlugin returns a single plugin by reverse-DNS name.
func (c *MarketplaceClient) FindPlugin(ctx context.Context, name string) (*MarketplacePlugin, error) {
	catalog, err := c.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range catalog.Plugins {
		if catalog.Plugins[i].Name == name {
			p := catalog.Plugins[i]
			return &p, nil
		}
	}
	return nil, ErrMarketplacePluginNotFound
}

func validateMarketplacePlugin(ctx context.Context, p MarketplacePlugin) error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(p.Version) == "" {
		return fmt.Errorf("version is required")
	}
	if strings.TrimSpace(p.Artifacts.BackendTarGzURL) != "" {
		if err := validateMarketplaceURL(ctx, p.Artifacts.BackendTarGzURL); err != nil {
			return fmt.Errorf("artifacts.backend_tar_gz_url: %w", err)
		}
	}
	if strings.TrimSpace(p.Artifacts.FrontendTarGzURL) != "" {
		if err := validateMarketplaceURL(ctx, p.Artifacts.FrontendTarGzURL); err != nil {
			return fmt.Errorf("artifacts.frontend_tar_gz_url: %w", err)
		}
	}
	if strings.TrimSpace(p.Artifacts.MigrationsTarGzURL) != "" {
		if err := validateMarketplaceURL(ctx, p.Artifacts.MigrationsTarGzURL); err != nil {
			return fmt.Errorf("artifacts.migrations_tar_gz_url: %w", err)
		}
	}
	if strings.TrimSpace(p.Artifacts.ManifestTarGzURL) == "" {
		return fmt.Errorf("artifacts.manifest_tar_gz_url is required")
	}
	if err := validateMarketplaceURL(ctx, p.Artifacts.ManifestTarGzURL); err != nil {
		return fmt.Errorf("artifacts.manifest_tar_gz_url: %w", err)
	}
	if strings.TrimSpace(p.Artifacts.MCPTarGzURL) != "" {
		if err := validateMarketplaceURL(ctx, p.Artifacts.MCPTarGzURL); err != nil {
			return fmt.Errorf("artifacts.mcp_tar_gz_url: %w", err)
		}
	}
	if strings.TrimSpace(p.Artifacts.SkillsTarGzURL) != "" {
		if err := validateMarketplaceURL(ctx, p.Artifacts.SkillsTarGzURL); err != nil {
			return fmt.Errorf("artifacts.skills_tar_gz_url: %w", err)
		}
	}
	return nil
}

// validateMarketplaceURL validates that a URL is safe for marketplace operations.
// It enforces HTTPS and blocks private/internal IP ranges to prevent SSRF attacks.
//
// Note: This validation is susceptible to DNS rebinding attacks where a hostname
// could resolve to a public IP during validation but to a private IP during the
// actual request. For production deployments, consider implementing DNS pinning
// or using a dedicated egress proxy with allowlist-based filtering.
func validateMarketplaceURL(ctx context.Context, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Require HTTPS
	if u.Scheme != "https" {
		return fmt.Errorf("only HTTPS URLs are allowed, got %q", u.Scheme)
	}

	// Extract hostname for IP validation
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL has no hostname")
	}

	// Resolve hostname to IP addresses
	resolver := &net.Resolver{}
	ips, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("failed to resolve hostname: %w", err)
	}

	// Check each resolved IP against private/internal ranges
	for _, ipAddr := range ips {
		if netguard.IsPrivateOrInternalIP(ipAddr.IP) {
			return fmt.Errorf("URL resolves to private/internal IP address: %s", ipAddr.IP.String())
		}
	}

	return nil
}
