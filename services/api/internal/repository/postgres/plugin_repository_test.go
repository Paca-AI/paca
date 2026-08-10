package postgres

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
)

// TestPluginToModel_RoundTrip_PreservesMCPManifestFields guards against a
// regression class this package has already hit once: Create/Update
// re-marshal the *typed* PluginManifest struct into the DB's JSONB column
// rather than storing the raw plugin.json bytes, so any manifest field not
// mirrored in the Go struct is silently dropped on install/update instead of
// erroring. ToolContextHooks was added to MCPManifest specifically to fix
// one such drop; this test exercises the exact conversion path
// (pluginToModel -> pluginFromModel) Create/Update/FindByID all go through,
// so a future field added to the wrong place fails a test instead of
// silently vanishing in production.
func TestPluginToModel_RoundTrip_PreservesMCPManifestFields(t *testing.T) {
	original := &plugindom.Plugin{
		ID:      uuid.New(),
		Name:    "com.paca.github",
		Version: "1.0.0",
		Manifest: plugindom.PluginManifest{
			ID:      "com.paca.github",
			Version: "1.0.0",
			MCP: &plugindom.MCPManifest{
				RemoteEntryURL:   "https://example.com/entry.js",
				ToolContextHooks: []string{"get_task", "list_tasks"},
			},
		},
		Enabled:     true,
		InstalledAt: time.Now().UTC().Truncate(time.Second),
		UpdatedAt:   time.Now().UTC().Truncate(time.Second),
	}

	model, err := pluginToModel(original)
	if err != nil {
		t.Fatalf("pluginToModel: %v", err)
	}

	roundTripped, err := pluginFromModel(model)
	if err != nil {
		t.Fatalf("pluginFromModel: %v", err)
	}

	if roundTripped.Manifest.MCP == nil {
		t.Fatal("MCP manifest was dropped on round trip")
	}
	if roundTripped.Manifest.MCP.RemoteEntryURL != original.Manifest.MCP.RemoteEntryURL {
		t.Errorf("RemoteEntryURL = %q, want %q",
			roundTripped.Manifest.MCP.RemoteEntryURL, original.Manifest.MCP.RemoteEntryURL)
	}
	if !slices.Equal(roundTripped.Manifest.MCP.ToolContextHooks, original.Manifest.MCP.ToolContextHooks) {
		t.Errorf("ToolContextHooks = %v, want %v",
			roundTripped.Manifest.MCP.ToolContextHooks, original.Manifest.MCP.ToolContextHooks)
	}
}

// TestPluginToModel_RoundTrip_NilMCPManifest ensures a manifest with no mcp
// block at all (most plugins) round-trips to nil rather than a zero-value
// struct, since callers branch on `Manifest.MCP == nil`.
func TestPluginToModel_RoundTrip_NilMCPManifest(t *testing.T) {
	original := &plugindom.Plugin{
		ID:      uuid.New(),
		Name:    "com.paca.no-mcp",
		Version: "1.0.0",
		Manifest: plugindom.PluginManifest{
			ID:      "com.paca.no-mcp",
			Version: "1.0.0",
		},
		InstalledAt: time.Now().UTC().Truncate(time.Second),
		UpdatedAt:   time.Now().UTC().Truncate(time.Second),
	}

	model, err := pluginToModel(original)
	if err != nil {
		t.Fatalf("pluginToModel: %v", err)
	}

	roundTripped, err := pluginFromModel(model)
	if err != nil {
		t.Fatalf("pluginFromModel: %v", err)
	}

	if roundTripped.Manifest.MCP != nil {
		t.Errorf("MCP = %+v, want nil", roundTripped.Manifest.MCP)
	}
}
