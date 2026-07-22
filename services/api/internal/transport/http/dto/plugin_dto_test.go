package dto_test

import (
	"testing"

	pluginrt "github.com/Paca-AI/api/internal/platform/plugin"
	"github.com/Paca-AI/api/internal/transport/http/dto"
)

// TestMarketplacePluginListResponseFromCatalog_ArtifactsRoundtrip verifies
// every optional artifact URL, including skills_tar_gz_url, survives the
// mapping from the marketplace catalog model to the API response DTO.
func TestMarketplacePluginListResponseFromCatalog_ArtifactsRoundtrip(t *testing.T) {
	catalog := &pluginrt.MarketplaceCatalog{
		Plugins: []pluginrt.MarketplacePlugin{
			{
				Name:    "com.paca.test",
				Version: "1.0.0",
				Artifacts: pluginrt.MarketplacePluginArtifact{
					BackendTarGzURL:    "https://example.com/backend.tar.gz",
					FrontendTarGzURL:   "https://example.com/frontend.tar.gz",
					MigrationsTarGzURL: "https://example.com/migrations.tar.gz",
					ManifestTarGzURL:   "https://example.com/manifest.tar.gz",
					MCPTarGzURL:        "https://example.com/mcp.tar.gz",
					SkillsTarGzURL:     "https://example.com/skills.tar.gz",
				},
			},
		},
	}

	resp := dto.MarketplacePluginListResponseFromCatalog(catalog)

	if len(resp.Plugins) != 1 {
		t.Fatalf("expected 1 plugin in response, got %d", len(resp.Plugins))
	}

	got := resp.Plugins[0].Artifacts
	want := dto.MarketplacePluginArtifactResponse{
		BackendTarGzURL:    "https://example.com/backend.tar.gz",
		FrontendTarGzURL:   "https://example.com/frontend.tar.gz",
		MigrationsTarGzURL: "https://example.com/migrations.tar.gz",
		ManifestTarGzURL:   "https://example.com/manifest.tar.gz",
		MCPTarGzURL:        "https://example.com/mcp.tar.gz",
		SkillsTarGzURL:     "https://example.com/skills.tar.gz",
	}

	if got.BackendTarGzURL != want.BackendTarGzURL ||
		got.FrontendTarGzURL != want.FrontendTarGzURL ||
		got.MigrationsTarGzURL != want.MigrationsTarGzURL ||
		got.ManifestTarGzURL != want.ManifestTarGzURL ||
		got.MCPTarGzURL != want.MCPTarGzURL ||
		got.SkillsTarGzURL != want.SkillsTarGzURL {
		t.Fatalf("artifact URLs did not round-trip through the DTO mapping:\ngot:  %+v\nwant: %+v", got, want)
	}
}
