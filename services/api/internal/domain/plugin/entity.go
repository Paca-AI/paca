// Package plugindom defines the plugin aggregate and its domain contracts.
// Each installed plugin has a manifest that describes its routes, extension
// points, and event subscriptions.  System-wide extension settings managed
// by the super admin control ordering and visibility for all users.
package plugindom

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
)

// Plugin represents one installed plugin in the registry.
type Plugin struct {
	ID          uuid.UUID
	Name        string         // reverse-DNS id, e.g. "com.paca.checklist"
	Version     string         // semver, e.g. "1.0.0"
	Manifest    PluginManifest // parsed plugin.json
	Enabled     bool
	InstalledAt time.Time
	UpdatedAt   time.Time
}

// PluginManifest is the structured content of a plugin's plugin.json file.
// Fields not used by the core runtime are stored as-is in the JSONB column.
type PluginManifest struct {
	// ID is the reverse-DNS plugin identifier (must match Plugin.Name).
	ID string `json:"id"`
	// DisplayName is the human-readable name shown in the UI.
	DisplayName string `json:"displayName"`
	// Description is a short description of the plugin.
	Description string `json:"description,omitempty"`
	// Version is the semver version of the plugin.
	Version string `json:"version"`
	// Capabilities lists the plugin's capabilities (e.g., "repository" for VCS plugins).
	Capabilities []string `json:"capabilities,omitempty"`
	// Backend holds backend-specific manifest settings.
	Backend *BackendManifest `json:"backend,omitempty"`
	// Frontend holds frontend-specific manifest settings.
	Frontend *FrontendManifest `json:"frontend,omitempty"`
	// MCP holds MCP-server-specific manifest settings.
	MCP *MCPManifest `json:"mcp,omitempty"`
	// Skills holds Agent-Skills-specific manifest settings.
	Skills *SkillsManifest `json:"skills,omitempty"`
	// Permissions lists the host function scopes the plugin requires.
	Permissions []string `json:"permissions,omitempty"`
	// CustomPermissions lists project/global-scoped permission keys the
	// plugin declares. Declared keys become checkable via requirePermissions
	// and appear in the project/global role editor UI so admins can grant
	// them to specific roles (e.g. "time_logging.manage_all").
	CustomPermissions []CustomPermission `json:"customPermissions,omitempty"`
}

// CustomPermission describes a permission key a plugin declares beyond the
// host's built-in permission set. The host stores grants for these keys in
// the same JSONB permission map as built-in permissions (e.g.
// project_roles.permissions), so no schema change is needed to persist them
// — only to know they exist so the role editor can expose them and plugin
// route/backend checks can reference them by name.
type CustomPermission struct {
	// Key is the permission's stable identifier, e.g. "time_logging.manage_all".
	// Must be namespaced under the plugin's domain to avoid collisions with
	// built-in permissions and other plugins.
	Key string `json:"key"`
	// Label is the human-readable name shown in the role editor.
	Label string `json:"label"`
	// Description explains what the permission grants.
	Description string `json:"description,omitempty"`
	// Scope determines which role editor the permission appears in:
	// "project" (per-project roles) or "global" (global roles). Defaults to
	// "project" when omitted.
	Scope string `json:"scope,omitempty"`
}

// skillNamePattern mirrors the AgentSkills name convention enforced by the
// OpenHands SDK (openhands/sdk/skills/utils.py's SKILL_NAME_PATTERN):
// lowercase alphanumeric segments joined by single hyphens.
var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// pluginKeyNamespace derives the required custom-permission key prefix from a
// plugin's reverse-DNS ID: its last dot-separated segment, snake_cased (e.g.
// "com.paca.time-logging" -> "time_logging").
func pluginKeyNamespace(pluginID string) string {
	parts := strings.Split(pluginID, ".")
	last := parts[len(parts)-1]
	return strings.ReplaceAll(last, "-", "_")
}

// Validate checks manifest invariants that can't be expressed in the JSON
// schema alone. In particular, it enforces that every declared custom
// permission key is namespaced under the plugin's own ID, so a plugin cannot
// declare a key (e.g. "users.write") that collides with a built-in
// permission or another plugin's custom permission.
func (m PluginManifest) Validate() error {
	namespace := pluginKeyNamespace(m.ID)
	prefix := namespace + "."
	for _, perm := range m.CustomPermissions {
		if perm.Key == "" {
			return fmt.Errorf("customPermissions: key is required")
		}
		if !strings.HasPrefix(perm.Key, prefix) {
			return fmt.Errorf("customPermissions: key %q must be namespaced under %q (expected prefix %q)", perm.Key, m.ID, prefix)
		}
		if perm.Scope != "" && perm.Scope != "project" && perm.Scope != "global" {
			return fmt.Errorf("customPermissions: key %q has invalid scope %q", perm.Key, perm.Scope)
		}
	}
	if m.Skills != nil {
		if err := m.Skills.validate(); err != nil {
			return err
		}
	}
	return nil
}

// MCPManifest describes the MCP (Model Context Protocol) side of the plugin.
// When present, the Paca MCP server loads the module at RemoteEntryURL at
// startup and merges the exported tools into the server's tool list.
type MCPManifest struct {
	// RemoteEntryURL is the URL to the plugin's MCP entry module.
	// The module must be a Node.js-compatible ESM bundle that exports a
	// PluginMCPEntry as its default export (see @paca-ai/plugin-sdk-mcp).
	RemoteEntryURL string `json:"remoteEntryUrl"`
}

// SkillsManifest describes the Agent Skills a plugin contributes. When
// present, the skills bundle is extracted from the plugin's install
// artifact to a directory served statically at BaseURL, with each skill at
// <BaseURL>/<name>/SKILL.md (AgentSkills format: YAML frontmatter +
// markdown body) — the same format as services/ai-agent's default skills.
// This mirrors MCPManifest's RemoteEntryURL pattern, adapted for a
// directory of skills instead of a single JS module.
type SkillsManifest struct {
	// BaseURL is the root URL of the plugin's extracted skills bundle.
	BaseURL string `json:"baseUrl"`
	// Names lists the skill directory names available under BaseURL.
	// Declared explicitly because the static file server does not support
	// directory listing.
	Names []string `json:"names"`
}

// validate checks that the skills manifest is well-formed: a base URL and at
// least one uniquely-named skill, each name following the AgentSkills naming
// convention and none colliding with a reserved trigger-skill name (see
// agentdom.ReservedSkillNames).
func (m SkillsManifest) validate() error {
	if strings.TrimSpace(m.BaseURL) == "" {
		return fmt.Errorf("skills: baseUrl is required")
	}
	if len(m.Names) == 0 {
		return fmt.Errorf("skills: at least one name is required")
	}
	seen := make(map[string]bool, len(m.Names))
	for _, name := range m.Names {
		if !skillNamePattern.MatchString(name) {
			return fmt.Errorf("skills: name %q must be lowercase alphanumeric with single hyphens (e.g. \"paca-my-skill\")", name)
		}
		// Every skill in the Paca ecosystem — bundled or plugin-contributed —
		// shares the "paca-" namespace (see docs/plugins/skills-plugin-system.md),
		// distinct from MCP tool naming, which prefixes with the plugin's own
		// name instead to avoid inter-plugin tool collisions.
		if !strings.HasPrefix(name, "paca-") {
			return fmt.Errorf("skills: name %q must start with \"paca-\"", name)
		}
		if agentdom.IsReservedSkillName(name) {
			return fmt.Errorf("skills: name %q is reserved", name)
		}
		if seen[name] {
			return fmt.Errorf("skills: duplicate name %q", name)
		}
		seen[name] = true
	}
	return nil
}

// BackendManifest describes the backend (WASM) side of the plugin.
type BackendManifest struct {
	// Routes is the list of HTTP routes the plugin registers.
	// Each route is mounted at /api/v1/plugins/{pluginId}/{path}.
	// Project-scoped routes should include /projects/:projectId in the path.
	Routes []PluginRoute `json:"routes,omitempty"`
	// EventSubscriptions lists the event topics the plugin subscribes to.
	EventSubscriptions []string `json:"eventSubscriptions,omitempty"`
	// AllowedOutboundDomains is the list of hostnames the plugin is permitted to
	// contact via paca.fetch. Matching is exact, case-insensitive hostname match,
	// except for the literal entry "*" which permits any HTTPS host (still
	// subject to the private/internal IP block). Requests to unlisted domains
	// are rejected.
	AllowedOutboundDomains []string `json:"allowedOutboundDomains,omitempty"`
	// AllowedConfigKeys is the list of host config keys the plugin may read via
	// paca.config_get. Keys not listed here are not exposed to the plugin.
	AllowedConfigKeys []string `json:"allowedConfigKeys,omitempty"`
	// SensitiveFields declares which columns in this plugin's own database
	// schema contain sensitive data, keyed by unqualified table name. When a
	// different plugin's db_query/db_query2 call reads from this plugin's
	// schema (via an explicit schema-qualified table reference), the host
	// replaces these columns' values with "***" in the result. Queries this
	// plugin runs against its own schema are never redacted.
	SensitiveFields map[string][]string `json:"sensitiveFields,omitempty"`
	// RequestedSensitiveFields declares which sensitive fields outside this
	// plugin's own schema it needs read/write access to via
	// db_query/db_query2/db_exec. Entries are "table.column" for a core
	// (platform-owned) field, or "<owningPluginID>:table.column" for a field
	// another plugin declared in its own SensitiveFields. A field is exempt
	// from redaction/write-blocking for this plugin only if it's listed
	// here — everything else stays masked/blocked. This list must be
	// surfaced to admins in the plugin marketplace before install, the same
	// way an OAuth scope or mobile app permission prompt would be.
	RequestedSensitiveFields []string `json:"requestedSensitiveFields,omitempty"`
}

// FrontendManifest describes the frontend (Module Federation) side of the plugin.
type FrontendManifest struct {
	// RemoteEntryURL is the URL to the Module Federation remote entry JS file.
	RemoteEntryURL string `json:"remoteEntryUrl,omitempty"`
	// ExtensionPoints is the list of extension points the plugin registers into.
	ExtensionPoints []ExtensionPointRegistration `json:"extensionPoints,omitempty"`
	// NavItems is the list of sidebar nav items the plugin registers. Each nav
	// item routes to a full-page component registered at the "project.page" or
	// "admin.page" extension point (see NavItem.Point).
	NavItems []NavItem `json:"navItems,omitempty"`
}

// NavItem describes a sidebar navigation entry contributed by the plugin. It
// routes to a full-page plugin component instead of an embedded fragment.
type NavItem struct {
	// Scope determines which sidebar section the item appears in:
	// "project" (per-project sidebar) or "admin" (admin sidebar).
	Scope string `json:"scope"`
	// Slug is the URL segment identifying this page, unique per plugin+scope,
	// e.g. "time-tracking". Combined with the plugin ID to form the route:
	// /projects/:projectId/plugins/:pluginId/:slug or
	// /admin/plugins/:pluginId/:slug.
	Slug string `json:"slug"`
	// Label is the human-readable sidebar link text.
	Label string `json:"label"`
	// Icon is a lucide-react icon name (PascalCase), e.g. "Clock".
	Icon string `json:"icon,omitempty"`
	// Component is the exported React component name from the remote entry,
	// registered at the "project.page" or "admin.page" extension point.
	Component string `json:"component"`
	// Order is the default display order within the sidebar section.
	Order int `json:"order,omitempty"`
	// RequiredPermission is the permission key (built-in or a key from this
	// plugin's own CustomPermissions) the caller must hold to see and access
	// this nav item's page. Checked with the same dot-wildcard semantics as
	// requirePermissions, against the caller's global permission map for
	// Scope "admin" or their project permission map for Scope "project". If
	// omitted, the page is reachable by anyone who can already reach the
	// enclosing sidebar section (all project members, or all admins).
	RequiredPermission string `json:"requiredPermission,omitempty"`
}

// PluginRoute defines a single HTTP route exposed by the plugin backend.
type PluginRoute struct {
	Method string `json:"method"` // GET | POST | PATCH | PUT | DELETE
	Path   string `json:"path"`   // relative path, e.g. "/items", "/items/:id", or "/items/*rest"
	// Public allows anonymous access for this route (no auth middleware).
	// Kept for backward compatibility; equivalent to an empty middleware chain.
	Public bool `json:"public,omitempty"`
	// Middlewares defines host-enforced middleware to apply in order for this
	// route. If omitted (null), the host applies its default policy. An explicit
	// empty array disables all middleware for the route.
	Middlewares []PluginRouteMiddleware `json:"middlewares,omitempty"`
}

// PluginRouteMiddleware describes one middleware stage to enforce before the
// plugin handler is invoked.
type PluginRouteMiddleware struct {
	// Name is the middleware identifier. Supported values:
	// authn, optionalAuthn, requireFreshPassword, requireJWTAuth,
	// requirePermissions.
	Name string `json:"name"`
	// Scope is used by requirePermissions: global | project.
	Scope string `json:"scope,omitempty"`
	// ProjectParam is the route param name for project scope resolution.
	// Defaults to "projectId".
	ProjectParam string `json:"projectParam,omitempty"`
	// Permissions is used by requirePermissions, e.g. ["projects.read"].
	Permissions []string `json:"permissions,omitempty"`
}

// ExtensionPointRegistration describes a frontend component registered into an
// extension point in the host application.
type ExtensionPointRegistration struct {
	// Point is the extension point identifier, e.g. "task.detail.section".
	Point string `json:"point"`
	// Component is the exported React component name from the remote entry.
	Component string `json:"component"`
	// Label is the human-readable name shown where the host lists this
	// registration as a user-facing choice (e.g. the "New view" picker for
	// the "view" extension point). Falls back to Component when omitted.
	Label string `json:"label,omitempty"`
	// Order is the default display order within the extension point.
	Order int `json:"order,omitempty"`
}

// MarshalManifest serialises the manifest to JSON bytes for JSONB storage.
func (m PluginManifest) MarshalManifest() ([]byte, error) {
	return json.Marshal(m)
}

// UnmarshalManifest parses raw JSONB bytes into a PluginManifest.
func UnmarshalManifest(data []byte) (PluginManifest, error) {
	var m PluginManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return PluginManifest{}, err
	}
	return m, nil
}

// PluginExtensionSetting stores system-wide ordering and visibility settings
// for a single extension point of a specific plugin.  These are configured
// by the super admin and apply to all users.
type PluginExtensionSetting struct {
	ID             uuid.UUID
	PluginID       uuid.UUID
	ExtensionPoint string
	Settings       ExtensionSettingData
	UpdatedAt      time.Time
}

// ExtensionSettingData is the structured content stored in the settings JSONB
// column for a plugin_extension_settings row.
type ExtensionSettingData struct {
	// Hidden controls whether the extension point registration is visible to
	// all users.  Defaults to false (visible).
	Hidden bool `json:"hidden"`
	// Order is the admin-chosen display order for this registration.
	// Lower values appear first.  A value of 0 means "use plugin default".
	Order int `json:"order,omitempty"`
}
