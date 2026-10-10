package plugindom

import (
	"errors"
	"strings"
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

// TestPluginRouteMiddlewares_SurvivesManifestRoundTrip is a regression test
// for a bug where PluginRoute.Middlewares carried a `json:"middlewares,omitempty"`
// tag: an explicitly-empty slice ("public route, no middleware") and a nil
// slice ("not declared, apply the host's default policy") are meaningfully
// different to PluginHandler.routeMiddlewares, but `omitempty` made a
// marshal of the former indistinguishable from the latter — dropping the key
// entirely, same as it would for nil. Every request replays this exact
// marshal/unmarshal (ProxyRequest re-fetches the manifest via
// PluginService.ListPlugins on each call), so a route declared with an
// explicit empty middleware list silently reverted to the default policy
// after its first round trip through JSONB storage.
func TestPluginRouteMiddlewares_SurvivesManifestRoundTrip(t *testing.T) {
	tests := []struct {
		name        string
		middlewares []PluginRouteMiddleware
		wantNil     bool
	}{
		{
			name:        "nil middlewares stays nil",
			middlewares: nil,
			wantNil:     true,
		},
		{
			name:        "explicit empty middlewares stays a non-nil empty slice",
			middlewares: []PluginRouteMiddleware{},
			wantNil:     false,
		},
		{
			name:        "populated middlewares round-trips",
			middlewares: []PluginRouteMiddleware{{Name: "authn"}},
			wantNil:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := PluginManifest{
				ID: "com.paca.example",
				Backend: &BackendManifest{
					Routes: []PluginRoute{{Method: "GET", Path: "/hello", Middlewares: tt.middlewares}},
				},
			}

			data, err := m.MarshalManifest()
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			got, err := UnmarshalManifest(data)
			if err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			route := got.Backend.Routes[0]
			if tt.wantNil && route.Middlewares != nil {
				t.Fatalf("expected nil middlewares after round trip, got %#v", route.Middlewares)
			}
			if !tt.wantNil && route.Middlewares == nil {
				t.Fatalf("expected non-nil middlewares after round trip, got nil")
			}
			if !tt.wantNil && len(route.Middlewares) != len(tt.middlewares) {
				t.Fatalf("expected %d middlewares after round trip, got %d", len(tt.middlewares), len(route.Middlewares))
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

func TestValidatePluginName(t *testing.T) {
	for _, ok := range []string{"com.paca.checklist", "com.paca.time-logging", "test.plugin", "alpha", "a_b.c-d"} {
		if err := ValidatePluginName(ok); err != nil {
			t.Errorf("ValidatePluginName(%q) = %v, want nil", ok, err)
		}
	}
	long := strings.Repeat("a", 129)
	for _, bad := range []string{"", "..", ".", "../../tmp/x", "a/b", `a\b`, "/abs", "a..b", ".a", "a.", "a b", "a;b", long} {
		if err := ValidatePluginName(bad); err == nil {
			t.Errorf("ValidatePluginName(%q) = nil, want error", bad)
		}
	}
}

func TestValidate_RouteMiddlewares(t *testing.T) {
	withMW := func(mw PluginRouteMiddleware) PluginManifest {
		return PluginManifest{ID: "com.paca.time-logging", Backend: &BackendManifest{Routes: []PluginRoute{
			{Method: "GET", Path: "/x", Middlewares: []PluginRouteMiddleware{{Name: "authn"}, mw}},
		}}}
	}
	tests := []struct {
		name    string
		mw      PluginRouteMiddleware
		wantErr string
	}{
		{"requireActions builtin", PluginRouteMiddleware{Name: "requireActions", Actions: []string{"tasks:read"}}, ""},
		{"requireActions plugin action", PluginRouteMiddleware{Name: "requireActions", Scope: "project", Actions: []string{"time_logging:manage_all"}}, ""},
		{"requireActions dotted domain", PluginRouteMiddleware{Name: "requireActions", Actions: []string{"project.settings.task_types:write"}}, ""},
		{"legacy requirePermissions rejected", PluginRouteMiddleware{Name: "requirePermissions", Permissions: []string{"tasks.read"}}, "requirePermissions is no longer supported; use requireActions with IAM actions"},
		{"legacy key in requireActions rejected", PluginRouteMiddleware{Name: "requireActions", Actions: []string{"tasks.read"}}, "<domain>:<verb>"},
		{"wildcard rejected", PluginRouteMiddleware{Name: "requireActions", Actions: []string{"tasks:*"}}, "<domain>:<verb>"},
		{"empty actions rejected", PluginRouteMiddleware{Name: "requireActions"}, "at least one action"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := withMW(tt.mw).Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
	if err := withMW(PluginRouteMiddleware{Name: "requirePermissions"}).Validate(); !errors.Is(err, ErrRequirePermissionsUnsupported) {
		t.Fatalf("want ErrRequirePermissionsUnsupported, got %v", err)
	}
}

func TestManifestCustomPermissionsAreActions(t *testing.T) {
	base := PluginManifest{ID: "com.paca.time-logging"}
	cases := []struct {
		name string
		keys []string
		ok   bool
	}{
		{"own namespace", []string{"time_logging:manage_all", "time_logging:log"}, true},
		{"none", nil, true},
		{"legacy dotted key", []string{"time_logging.manage_all"}, false},
		{"someone else's namespace", []string{"tasks:write"}, false},
		{"a wildcard", []string{"time_logging:*"}, false},
		{"duplicate", []string{"time_logging:log", "time_logging:log"}, false},
	}
	for _, c := range cases {
		m := base
		for _, k := range c.keys {
			m.CustomPermissions = append(m.CustomPermissions, CustomPermission{Key: k, Label: k})
		}
		if err := m.Validate(); (err == nil) != c.ok {
			t.Errorf("%s: err = %v, want ok=%v", c.name, err, c.ok)
		}
		if c.ok && len(m.Actions()) != len(c.keys) {
			t.Errorf("%s: Actions() = %v", c.name, m.Actions())
		}
	}
}

func TestManifestJSONUsesRequirePermissions(t *testing.T) {
	cases := []struct {
		name, raw string
		want      bool
	}{
		{"legacy", `{"backend":{"routes":[{"middlewares":[{"name":"authn"},{"name":"RequirePermissions"}]}]}}`, true},
		{"actions", `{"backend":{"routes":[{"middlewares":[{"name":"requireActions"}]}]}}`, false},
		{"no backend", `{"id":"x"}`, false},
		{"no middlewares", `{"backend":{"routes":[{"method":"GET"}]}}`, false},
		{"not json", `nope`, false},
	}
	for _, c := range cases {
		if got := ManifestJSONUsesRequirePermissions([]byte(c.raw)); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestValidate_RejectsMalformedRequiredPermission(t *testing.T) {
	m := PluginManifest{ID: "com.paca.smtp", Frontend: &FrontendManifest{
		NavItems: []NavItem{{Scope: "admin", Slug: "smtp", RequiredPermission: "settings.write"}},
	}}
	if err := m.Validate(); err == nil {
		t.Fatal("want error for requiredPermission without a domain:verb form")
	}
	m.Frontend.NavItems[0].RequiredPermission = "settings:write"
	if err := m.Validate(); err != nil {
		t.Fatalf("settings:write should be valid: %v", err)
	}
}
