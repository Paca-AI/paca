package plugindom

import (
	"testing"

	"github.com/Paca-AI/api/internal/platform/bundledskills"
)

func TestPluginManifestValidate_MinCoreVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		wantErr bool
	}{
		{name: "empty is valid", version: ""},
		{name: "valid semver", version: "1.2.3"},
		{name: "valid semver with v prefix", version: "v1.2.3"},
		{name: "missing patch", version: "1.2", wantErr: true},
		{name: "pre-release rejected", version: "1.2.3-beta.1", wantErr: true},
		{name: "build metadata rejected", version: "1.2.3+001", wantErr: true},
		{name: "non-numeric component", version: "1.x.3", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := PluginManifest{ID: "com.paca.example", MinCoreVersion: tt.version}
			err := m.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestPluginManifestCheckMinCoreVersion(t *testing.T) {
	tests := []struct {
		name           string
		minCoreVersion string
		hostVersion    string
		wantErr        bool
	}{
		{name: "no minimum declared", minCoreVersion: "", hostVersion: "1.0.0"},
		{name: "host newer than minimum", minCoreVersion: "1.0.0", hostVersion: "1.2.0"},
		{name: "host equals minimum", minCoreVersion: "1.2.0", hostVersion: "1.2.0"},
		{name: "host older than minimum", minCoreVersion: "1.2.0", hostVersion: "1.1.9", wantErr: true},
		{name: "host version has v prefix", minCoreVersion: "1.2.0", hostVersion: "v1.2.0"},
		{name: "host version has pre-release suffix", minCoreVersion: "1.2.0", hostVersion: "v1.2.0-evup.1"},
		{name: "unparseable host version (dev build) never blocks", minCoreVersion: "99.0.0", hostVersion: "dev"},
		{name: "empty host version never blocks", minCoreVersion: "99.0.0", hostVersion: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := PluginManifest{ID: "com.paca.example", MinCoreVersion: tt.minCoreVersion}
			err := m.CheckMinCoreVersion(tt.hostVersion)
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestCompareSemver covers the shared strict-semver comparator now used by
// both PluginManifest validation/MinCoreVersion checks and the marketplace
// upgrade handler's downgrade/no-op guard, so the two call sites can't drift
// out of sync on what counts as newer/older/equal.
func TestCompareSemver(t *testing.T) {
	tests := []struct {
		name     string
		a, b     string
		wantSign int // -1, 0, or 1
		wantErr  bool
	}{
		{name: "equal", a: "1.2.3", b: "1.2.3", wantSign: 0},
		{name: "equal with v prefix", a: "v1.2.3", b: "1.2.3", wantSign: 0},
		{name: "a greater by patch", a: "1.2.4", b: "1.2.3", wantSign: 1},
		{name: "a less by minor", a: "1.1.9", b: "1.2.0", wantSign: -1},
		{name: "a greater by major", a: "2.0.0", b: "1.9.9", wantSign: 1},
		{name: "invalid a", a: "1.2", b: "1.2.3", wantErr: true},
		{name: "invalid b", a: "1.2.3", b: "1.2.x", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmp, err := CompareSemver(tt.a, tt.b)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			switch {
			case tt.wantSign > 0 && cmp <= 0:
				t.Errorf("expected positive result, got %d", cmp)
			case tt.wantSign < 0 && cmp >= 0:
				t.Errorf("expected negative result, got %d", cmp)
			case tt.wantSign == 0 && cmp != 0:
				t.Errorf("expected 0, got %d", cmp)
			}
		})
	}
}

func TestPluginManifestValidate_Skills(t *testing.T) {
	base := func(skills *SkillsManifest) PluginManifest {
		return PluginManifest{
			ID:     "com.paca.example",
			Skills: skills,
		}
	}

	tests := []struct {
		name    string
		skills  *SkillsManifest
		wantErr bool
	}{
		{
			name:   "nil skills is valid",
			skills: nil,
		},
		{
			name: "valid skills block",
			skills: &SkillsManifest{
				BaseURL: "/plugins-skills/com.paca.example",
				Names:   []string{"paca-pr-review", "paca-changelog"},
			},
		},
		{
			name: "missing base url",
			skills: &SkillsManifest{
				Names: []string{"paca-pr-review"},
			},
			wantErr: true,
		},
		{
			name: "no names",
			skills: &SkillsManifest{
				BaseURL: "/plugins-skills/com.paca.example",
			},
			wantErr: true,
		},
		{
			name: "duplicate name",
			skills: &SkillsManifest{
				BaseURL: "/plugins-skills/com.paca.example",
				Names:   []string{"paca-pr-review", "paca-pr-review"},
			},
			wantErr: true,
		},
		{
			name: "invalid name pattern",
			skills: &SkillsManifest{
				BaseURL: "/plugins-skills/com.paca.example",
				Names:   []string{"PR_Review"},
			},
			wantErr: true,
		},
		{
			name: "missing paca- prefix",
			skills: &SkillsManifest{
				BaseURL: "/plugins-skills/com.paca.example",
				Names:   []string{"pr-review"},
			},
			wantErr: true,
		},
		{
			name: "reserved trigger name",
			skills: &SkillsManifest{
				BaseURL: "/plugins-skills/com.paca.example",
				Names:   []string{"paca-trigger-chat"},
			},
			wantErr: true,
		},
		{
			// A plugin declaring one of Paca's own bundled skill names (e.g.
			// "paca-setup") would otherwise silently shadow it — see
			// bundledskills.IsBuiltinName.
			name: "collides with a bundled skill name",
			skills: &SkillsManifest{
				BaseURL: "/plugins-skills/com.paca.example",
				Names:   []string{bundledskills.List(bundledskills.TargetCLI)[0].Name},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := base(tt.skills).Validate()
			if tt.wantErr && err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}
