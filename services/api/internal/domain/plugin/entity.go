// Package plugindom defines the plugin aggregate and its domain contracts.
// Each installed plugin has a manifest that describes its routes, extension
// points, and event subscriptions.  System-wide extension settings managed
// by the super admin control ordering and visibility for all users.
package plugindom

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	"github.com/Paca-AI/api/internal/platform/bundledskills"
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
	// MinCoreVersion is the minimum Paca (host) version required to install or
	// upgrade to this manifest, as a strict "X.Y.Z" (or "vX.Y.Z") semver
	// string. The host refuses to install/enable a manifest whose
	// MinCoreVersion is newer than the running build (see CheckMinCoreVersion).
	// Empty means the plugin has no minimum host version requirement.
	MinCoreVersion string `json:"minCoreVersion,omitempty"`
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
	// Automation holds automation-graph node types the plugin contributes.
	Automation *AutomationManifest `json:"automation,omitempty"`
	// Permissions lists the host function scopes the plugin requires.
	Permissions []string `json:"permissions,omitempty"`
	// CustomPermissions lists the IAM actions the plugin declares (e.g.
	// "time_logging:manage_all"). Declared actions become checkable via
	// requireActions, are accepted in role policies, and appear in the role
	// editor so admins can grant them.
	CustomPermissions []CustomPermission `json:"customPermissions,omitempty"`
}

// CustomPermission describes a permission key a plugin declares beyond the
// host's built-in permission set. Grants for these keys live in role
// policies (roles.policy) like any other action, so no schema change is
// needed to persist them — only to know they exist so the role editor can
// expose them and plugin route/backend checks can reference them by name.
type CustomPermission struct {
	// Key is the IAM action the permission is, e.g. "time_logging:manage_all":
	// "<domain>:<verb>" with the domain the plugin's own namespace (see
	// pluginKeyNamespace), so it cannot collide with a built-in action or
	// another plugin's.
	Key string `json:"key"`
	// Label is the human-readable name shown in the role editor.
	Label string `json:"label"`
	// Description explains what the permission grants.
	Description string `json:"description,omitempty"`
	// Scope determines which role editor the permission appears in:
	// "project" (per-project roles), "global" (global roles) or "both" (an
	// action checked inside a project and across projects). Defaults to
	// "project" when omitted.
	Scope string `json:"scope,omitempty"`
}

// skillNamePattern mirrors the AgentSkills name convention enforced by the
// OpenHands SDK (openhands/sdk/skills/utils.py's SKILL_NAME_PATTERN):
// lowercase alphanumeric segments joined by single hyphens.
// pluginNamePattern restricts plugin names to dot-separated segments of
// letters, digits, '_' and '-' (e.g. "com.paca.time-logging"). Names are used
// as directory names on disk and as Postgres schema names, so path separators
// and ".." must never be accepted.
var pluginNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*(\.[A-Za-z0-9][A-Za-z0-9_-]*)*$`)

// maxPluginNameLen bounds plugin names (also the Postgres identifier limit
// leaves headroom for the schema prefix).
const maxPluginNameLen = 128

// ValidatePluginName reports whether name is a safe reverse-DNS plugin id.
func ValidatePluginName(name string) error {
	if name == "" {
		return fmt.Errorf("plugin name is required")
	}
	if len(name) > maxPluginNameLen {
		return fmt.Errorf("plugin name must be at most %d characters", maxPluginNameLen)
	}
	if !pluginNamePattern.MatchString(name) {
		return fmt.Errorf("plugin name %q is invalid: use dot-separated segments of letters, digits, '_' and '-'", name)
	}
	return nil
}

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
	if m.MinCoreVersion != "" {
		if _, err := ParseSemver(m.MinCoreVersion); err != nil {
			return fmt.Errorf("minCoreVersion: %w", err)
		}
	}
	namespace := pluginKeyNamespace(m.ID)
	prefix := namespace + ":"
	seen := map[string]bool{}
	for _, perm := range m.CustomPermissions {
		if perm.Key == "" {
			return fmt.Errorf("customPermissions: key is required")
		}
		if !strings.HasPrefix(perm.Key, prefix) || !actionPattern.MatchString(perm.Key) {
			return fmt.Errorf("customPermissions: key %q must be an action of the form %s<verb> (the plugin's own namespace %q)", perm.Key, prefix, namespace)
		}
		if seen[perm.Key] {
			return fmt.Errorf("customPermissions: key %q is declared twice", perm.Key)
		}
		seen[perm.Key] = true
		if perm.Scope != "" && perm.Scope != "project" && perm.Scope != "global" && perm.Scope != "both" {
			return fmt.Errorf("customPermissions: key %q has invalid scope %q", perm.Key, perm.Scope)
		}
	}
	if err := m.validateRouteMiddlewares(); err != nil {
		return err
	}
	if err := m.validateRequiredPermissions(); err != nil {
		return err
	}
	if m.Skills != nil {
		if err := m.Skills.validate(); err != nil {
			return err
		}
	}
	if m.Automation != nil {
		if err := m.Automation.validate(m.ID); err != nil {
			return err
		}
	}
	return nil
}

// Actions returns the IAM actions the manifest declares (customPermissions).
func (m PluginManifest) Actions() []string {
	out := make([]string, 0, len(m.CustomPermissions))
	for _, p := range m.CustomPermissions {
		out = append(out, p.Key)
	}
	return out
}

// validateRequiredPermissions rejects a nav item or extension point whose
// requiredPermission is not an action of the form "<domain>:<verb>" (a
// malformed one, such as "settings.write", is never granted to anyone, so the
// page would be locked for every user, "*" holders included).
func (m PluginManifest) validateRequiredPermissions() error {
	if m.Frontend == nil {
		return nil
	}
	check := func(what, perm string) error {
		if perm != "" && !actionPattern.MatchString(perm) {
			return fmt.Errorf("%s: requiredPermission %q must have the form <domain>:<verb>", what, perm)
		}
		return nil
	}
	for _, n := range m.Frontend.NavItems {
		if err := check("navItems "+n.Slug, n.RequiredPermission); err != nil {
			return err
		}
	}
	for _, e := range m.Frontend.ExtensionPoints {
		if err := check("extensionPoints "+e.Component, e.RequiredPermission); err != nil {
			return err
		}
	}
	return nil
}

// validateRouteMiddlewares rejects the legacy requirePermissions middleware
// and malformed requireActions actions ("<domain>:<verb>", no wildcard).
func (m PluginManifest) validateRouteMiddlewares() error {
	if m.Backend == nil {
		return nil
	}
	for _, route := range m.Backend.Routes {
		for _, mw := range route.Middlewares {
			switch strings.ToLower(strings.TrimSpace(mw.Name)) {
			case "requirepermissions":
				return fmt.Errorf("route %s %s: %w", route.Method, route.Path, ErrRequirePermissionsUnsupported)
			case "requireactions":
				if len(mw.Actions) == 0 {
					return fmt.Errorf("route %s %s: requireActions needs at least one action", route.Method, route.Path)
				}
				for _, a := range mw.Actions {
					if !actionPattern.MatchString(a) {
						return fmt.Errorf("route %s %s: requireActions action %q must have the form <domain>:<verb>", route.Method, route.Path, a)
					}
				}
			}
		}
	}
	return nil
}

// actionPattern matches an IAM action: a dotted domain, ":", a verb.
var actionPattern = regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_]+)*:[a-z0-9_]+$`)

// AutomationManifest declares the automation-graph node types a plugin
// contributes: Trigger, Condition, and Action nodes that appear in the
// canvas node palette alongside the built-in types, dispatched through the
// same WASM bridge as HandleRequest/HandleEvent (see Runtime.EvaluateCondition
// and Runtime.RunAction) — no new execution path, just two new exported
// function contracts a plugin's WASM module can implement.
type AutomationManifest struct {
	Triggers   []AutomationNodeManifest `json:"triggers,omitempty"`
	Conditions []AutomationNodeManifest `json:"conditions,omitempty"`
	Actions    []AutomationNodeManifest `json:"actions,omitempty"`
}

// AutomationNodeManifest describes one plugin-contributed node type.
type AutomationNodeManifest struct {
	// Type is the node's stable identifier, reverse-DNS namespaced under the
	// plugin's own ID (e.g. "com.paca.github.pr_merged") — this doubles as
	// the node's own collision-proof registry key, so no separate ID field
	// is needed.
	Type string `json:"type"`
	// Label is the human-readable name shown in the node palette.
	Label string `json:"label"`
	// ConfigSchema is a JSON Schema describing the node's config shape, used
	// by the frontend's generic config-form renderer. Plugins wanting a
	// richer custom UI can additionally register a component at the
	// "automation.node.config" frontend extension point, keyed by Type.
	ConfigSchema json.RawMessage `json:"configSchema,omitempty"`
	// EventTopic is the event topic that sources this trigger — only
	// meaningful for entries in AutomationManifest.Triggers. The engine
	// matches on topic alone (no WASM call needed); a plugin wanting
	// per-instance refinement beyond "this topic fired" contributes a
	// Condition node instead, placed right after the trigger in the graph.
	EventTopic string `json:"eventTopic,omitempty"`
}

// validate enforces that every declared node Type is namespaced under the
// plugin's own ID, mirroring CustomPermission's namespacing rule — a node
// type string is its own registry key, so collisions here are exactly as
// dangerous as a CustomPermission key collision.
func (a *AutomationManifest) validate(pluginID string) error {
	namespace := pluginKeyNamespace(pluginID)
	prefix := namespace + "."
	checkType := func(kind, nodeType string) error {
		if nodeType == "" {
			return fmt.Errorf("automation.%s: type is required", kind)
		}
		if !strings.HasPrefix(nodeType, prefix) {
			return fmt.Errorf("automation.%s: type %q must be namespaced under %q (expected prefix %q)", kind, nodeType, pluginID, prefix)
		}
		return nil
	}
	for _, t := range a.Triggers {
		if err := checkType("triggers", t.Type); err != nil {
			return err
		}
		if t.EventTopic == "" {
			return fmt.Errorf("automation.triggers: %q requires an eventTopic", t.Type)
		}
	}
	for _, c := range a.Conditions {
		if err := checkType("conditions", c.Type); err != nil {
			return err
		}
	}
	for _, act := range a.Actions {
		if err := checkType("actions", act.Type); err != nil {
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
	// ToolContextHooks lists core tool IDs (e.g. "get_task") this plugin's
	// getToolContext can contribute to. The MCP server only calls into a
	// plugin for a given tool if that tool ID is declared here — this is
	// what lets the host skip plugins that have no interest in a given
	// tool call instead of invoking every loaded plugin on every call.
	ToolContextHooks []string `json:"toolContextHooks,omitempty"`
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
// agentdom.ReservedSkillNames) or one of Paca's own bundled skill names (see
// bundledskills.IsBuiltinName) — the latter would otherwise let a plugin
// silently shadow a bundled skill users already trust, since neither the
// GET /api/v1/skills merge nor scripts/install-paca-skills.sh's by-name file
// writes dedupe against that list.
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
		if bundledskills.IsBuiltinName(name) {
			return fmt.Errorf("skills: name %q collides with a bundled Paca skill", name)
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
	// RequiredPermission is the IAM action (built-in or one from this
	// plugin's own CustomPermissions) the caller must hold to see and access
	// this nav item's page, checked against the caller's effective actions
	// for Scope "admin" (platform) or "project" (the project). If
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
	//
	// Deliberately no `omitempty`: this field's nil-vs-empty-slice distinction
	// is load-bearing (see PluginHandler.routeMiddlewares), and `omitempty`
	// would drop an explicit empty array during the JSON marshal this struct
	// goes through for JSONB storage, turning it back into nil (default
	// policy, now auth-required) on the next load.
	Middlewares []PluginRouteMiddleware `json:"middlewares"`
}

// PluginRouteMiddleware describes one middleware stage to enforce before the
// plugin handler is invoked.
type PluginRouteMiddleware struct {
	// Name is the middleware identifier. Supported values:
	// authn, optionalAuthn, requireFreshPassword, requireJWTAuth,
	// requireActions. The legacy requirePermissions is rejected (see
	// ErrRequirePermissionsUnsupported).
	Name string `json:"name"`
	// Scope is used by requireActions: global | project.
	Scope string `json:"scope,omitempty"`
	// ProjectParam is the route param name for project scope resolution.
	// Defaults to "projectId".
	ProjectParam string `json:"projectParam,omitempty"`
	// Actions is used by requireActions: IAM actions ("<domain>:<verb>",
	// built-in or plugin-declared, e.g. ["tasks:read"]) the caller must ALL
	// be allowed.
	Actions []string `json:"actions,omitempty"`
	// Permissions is the legacy requirePermissions key list. It is only
	// decoded so Validate can reject a manifest that still uses it.
	Permissions []string `json:"permissions,omitempty"`
}

// ErrRequirePermissionsUnsupported is returned for a manifest route that
// still declares the legacy requirePermissions middleware.
var ErrRequirePermissionsUnsupported = errors.New("requirePermissions is no longer supported; use requireActions with IAM actions")

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
	// RequiredPermission is the IAM action (built-in or one from this
	// plugin's own CustomPermissions) the caller must hold for this
	// registration to render, mirroring NavItem.RequiredPermission. Checked
	// against the caller's project effective actions when the enclosing
	// extension point is project-scoped (e.g. "project.settings.tab"). If
	// omitted, the registration is reachable by anyone who can already reach
	// the enclosing host page — this is the default for every extension
	// point today, since the field is new and no host-side renderer enforced
	// it before.
	RequiredPermission string `json:"requiredPermission,omitempty"`
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

// ManifestJSONUsesRequirePermissions reports whether a plugin.json document
// declares the retired requirePermissions route middleware. An installed
// plugin whose package still does is built for the old permission model: its
// stored manifest was converted to requireActions by migration 000065, but a
// reinstall or upgrade of that build is rejected until its author publishes a
// version that uses requireActions. Unreadable JSON reports false.
func ManifestJSONUsesRequirePermissions(raw []byte) bool {
	var doc struct {
		Backend struct {
			Routes []struct {
				Middlewares []struct {
					Name string `json:"name"`
				} `json:"middlewares"`
			} `json:"routes"`
		} `json:"backend"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return false
	}
	for _, route := range doc.Backend.Routes {
		for _, mw := range route.Middlewares {
			if strings.EqualFold(mw.Name, "requirePermissions") {
				return true
			}
		}
	}
	return false
}
