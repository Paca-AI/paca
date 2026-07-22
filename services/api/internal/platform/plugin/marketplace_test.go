package plugin

import (
	"context"
	"strings"
	"testing"
)

// TestValidateMarketplacePlugin_RejectsPrivateURLs ensures every optional
// artifact URL is put through the same SSRF guard as the required
// manifest_tar_gz_url. Skills was added after backend/frontend/migrations/mcp
// and was missed in this check — regression test for that gap.
func TestValidateMarketplacePlugin_RejectsPrivateURLs(t *testing.T) {
	const manifestURL = "https://example.com/manifest.tar.gz"
	privateURL := "https://127.0.0.1/artifact.tar.gz"

	cases := map[string]MarketplacePlugin{
		"backend": {Name: "com.paca.test", Version: "1.0.0", Artifacts: MarketplacePluginArtifact{
			ManifestTarGzURL: manifestURL, BackendTarGzURL: privateURL,
		}},
		"frontend": {Name: "com.paca.test", Version: "1.0.0", Artifacts: MarketplacePluginArtifact{
			ManifestTarGzURL: manifestURL, FrontendTarGzURL: privateURL,
		}},
		"migrations": {Name: "com.paca.test", Version: "1.0.0", Artifacts: MarketplacePluginArtifact{
			ManifestTarGzURL: manifestURL, MigrationsTarGzURL: privateURL,
		}},
		"mcp": {Name: "com.paca.test", Version: "1.0.0", Artifacts: MarketplacePluginArtifact{
			ManifestTarGzURL: manifestURL, MCPTarGzURL: privateURL,
		}},
		"skills": {Name: "com.paca.test", Version: "1.0.0", Artifacts: MarketplacePluginArtifact{
			ManifestTarGzURL: manifestURL, SkillsTarGzURL: privateURL,
		}},
	}

	for name, plugin := range cases {
		t.Run(name, func(t *testing.T) {
			err := validateMarketplacePlugin(context.Background(), plugin)
			if err == nil {
				t.Fatalf("expected validation error for private %s URL, got nil", name)
			}
			if !strings.Contains(err.Error(), name) {
				t.Fatalf("expected error to reference %q artifact field, got: %v", name, err)
			}
		})
	}
}

func TestValidateMarketplacePlugin_AllowsPublicSkillsURL(t *testing.T) {
	plugin := MarketplacePlugin{
		Name:    "com.paca.test",
		Version: "1.0.0",
		Artifacts: MarketplacePluginArtifact{
			ManifestTarGzURL: "https://example.com/manifest.tar.gz",
			SkillsTarGzURL:   "https://example.com/skills.tar.gz",
		},
	}
	if err := validateMarketplacePlugin(context.Background(), plugin); err != nil {
		t.Fatalf("expected no error for public skills URL, got: %v", err)
	}
}
