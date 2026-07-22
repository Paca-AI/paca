import { queryOptions } from "@tanstack/react-query";
import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

// ── Shapes ────────────────────────────────────────────────────────────────────

export interface PluginRoute {
	method: string;
	path: string;
}

export interface BackendManifest {
	routes?: PluginRoute[];
	eventSubscriptions?: string[];
}

export interface ExtensionPointRegistration {
	point: string;
	component: string;
	label?: string;
	order?: number;
}

export interface PluginNavItem {
	scope: "project" | "admin";
	slug: string;
	label: string;
	component: string;
	icon?: string;
	/**
	 * Permission key (built-in, or one of this plugin's own
	 * `customPermissions`) required to see and access this nav item's page.
	 * Checked against the caller's global permissions for `scope: "admin"`
	 * or their project permissions for `scope: "project"`. If omitted, the
	 * page is reachable by anyone who can reach the enclosing sidebar
	 * section (all project members, or all admins).
	 */
	requiredPermission?: string;
}

export interface FrontendManifest {
	remoteEntryUrl?: string;
	extensionPoints?: ExtensionPointRegistration[];
	navItems?: PluginNavItem[];
}

export interface PluginManifest {
	id: string;
	displayName: string;
	description?: string;
	version: string;
	backend?: BackendManifest;
	frontend?: FrontendManifest;
	permissions?: string[];
	customPermissions?: PluginCustomPermission[];
}

/**
 * A permission key a plugin declares beyond the host's built-in permission
 * set (e.g. "time_logging.manage_all"). The host stores grants for these in
 * the same JSONB permission map as built-in permissions, so declaring one
 * here only makes it discoverable — it shows up in the project/global role
 * editor and can be checked via requirePermissions or the plugin backend's
 * CheckPermission host function.
 */
export interface PluginCustomPermission {
	key: string;
	label: string;
	description?: string;
	/** Which role editor this permission appears in. Defaults to "project". */
	scope?: "project" | "global";
}

export interface Plugin {
	id: string;
	name: string;
	version: string;
	manifest: PluginManifest;
	extension_settings?: PluginExtensionSetting[];
	enabled: boolean;
	installed_at: string;
	updated_at: string;
}

export interface PluginExtensionSetting {
	id: string;
	plugin_id: string;
	extension_point: string;
	settings: {
		hidden: boolean;
		order?: number;
	};
	updated_at: string;
}

// ── API ───────────────────────────────────────────────────────────────────────

export async function listPlugins(): Promise<Plugin[]> {
	const { data } =
		await apiClient.instance.get<SuccessEnvelope<{ plugins: Plugin[] }>>(
			"/plugins",
		);
	return data.data.plugins;
}

export interface MarketplacePluginArtifacts {
	backend_tar_gz_url?: string;
	frontend_tar_gz_url?: string;
	migrations_tar_gz_url?: string;
	manifest_tar_gz_url: string;
	mcp_tar_gz_url?: string;
	skills_tar_gz_url?: string;
}

export interface MarketplacePlugin {
	name: string;
	display_name: string;
	description: string;
	version: string;
	avatar_url?: string;
	repository_url?: string;
	artifacts: MarketplacePluginArtifacts;
}

export async function listMarketplacePlugins(): Promise<MarketplacePlugin[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ plugins: MarketplacePlugin[] }>
	>("/admin/plugins/marketplace");
	return data.data.plugins;
}

export async function installMarketplacePlugin(payload: {
	name: string;
	enabled?: boolean;
}): Promise<Plugin> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<Plugin>>(
		"/admin/plugins/marketplace/install",
		payload,
	);
	return data.data;
}

export async function uninstallPlugin(pluginId: string): Promise<void> {
	await apiClient.instance.delete(`/admin/plugins/${pluginId}`);
}

export async function upgradePlugin(pluginId: string): Promise<Plugin> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<Plugin>>(
		`/admin/plugins/${pluginId}/upgrade`,
	);
	return data.data;
}

export async function updatePluginExtensionSetting(payload: {
	plugin_id: string;
	extension_point: string;
	settings: { hidden: boolean; order?: number };
}): Promise<PluginExtensionSetting> {
	const { data } = await apiClient.instance.patch<
		SuccessEnvelope<PluginExtensionSetting>
	>("/admin/plugin-extension-settings", payload);
	return data.data;
}

// ── Query options ─────────────────────────────────────────────────────────────

export const pluginsQueryOptions = queryOptions({
	queryKey: ["plugins"],
	queryFn: listPlugins,
	staleTime: 5 * 60 * 1000, // 5 min — plugins don't change often
});

export const marketplacePluginsQueryOptions = queryOptions({
	queryKey: ["plugins", "marketplace"],
	queryFn: listMarketplacePlugins,
	staleTime: 60 * 1000,
	retry: false,
});

// ── Registry helpers ──────────────────────────────────────────────────────────

export type ExtensionPointId =
	| "sidebar.general.section"
	| "sidebar.project.section"
	| "task.detail.section"
	| "project.settings.tab"
	| "view"
	| "project.page"
	| "admin.page";

export interface PluginRegistration {
	pluginUUID: string; // The database UUID for API calls
	pluginId: string; // The reverse-DNS identifier (e.g., "com.paca.checklist")
	pluginName: string;
	/** Per-registration display label from the manifest; falls back to component name. */
	label: string;
	remoteEntryUrl: string;
	component: string;
	order: number;
	hidden?: boolean;
}

/** Build a Map<ExtensionPointId, PluginRegistration[]> from the plugins list. */
export function buildRegistryMap(
	plugins: Plugin[],
): Map<ExtensionPointId, PluginRegistration[]> {
	const map = new Map<ExtensionPointId, PluginRegistration[]>();

	for (const plugin of plugins) {
		if (!plugin.enabled) continue;
		const ext = plugin.manifest.frontend?.extensionPoints;
		const remoteEntryUrl = plugin.manifest.frontend?.remoteEntryUrl;
		if (!ext || !remoteEntryUrl) continue;
		const settingsByPoint = new Map(
			(plugin.extension_settings ?? []).map((s) => [s.extension_point, s]),
		);

		for (const reg of ext) {
			const point = reg.point as ExtensionPointId;
			const setting = settingsByPoint.get(reg.point);
			const settingOrder = setting?.settings.order;
			const order =
				typeof settingOrder === "number" && settingOrder > 0
					? settingOrder
					: (reg.order ?? 0);
			const regs = map.get(point) ?? [];
			regs.push({
				pluginUUID: plugin.id, // UUID for API calls
				pluginId: plugin.manifest.id, // reverse-DNS for display/keys
				pluginName: plugin.manifest.displayName,
				label: reg.label ?? reg.component,
				remoteEntryUrl,
				component: reg.component,
				order,
				hidden: setting?.settings.hidden ?? false,
			});
			map.set(point, regs);
		}
	}

	// Sort each point's registrations by order
	for (const [, regs] of map) {
		regs.sort((a, b) => a.order - b.order);
	}

	return map;
}

// ── Nav item helpers ──────────────────────────────────────────────────────────

export interface PluginNavRegistration {
	pluginId: string; // reverse-DNS identifier, e.g. "com.paca.time-logging"
	pluginName: string;
	scope: "project" | "admin";
	slug: string;
	label: string;
	icon?: string;
	/** See `PluginNavItem.requiredPermission`. */
	requiredPermission?: string;
	/** The `project.page` / `admin.page` extension point registration this nav item routes to. */
	registration: PluginRegistration;
}

/**
 * Build the list of sidebar nav items contributed by enabled plugins for the
 * given scope ("project" or "admin"). Each nav item must reference a
 * component also registered at the matching `project.page`/`admin.page`
 * extension point — nav items without a matching registration are skipped.
 */
export function buildNavItems(
	plugins: Plugin[],
	scope: "project" | "admin",
): PluginNavRegistration[] {
	const point: ExtensionPointId =
		scope === "project" ? "project.page" : "admin.page";
	const registryMap = buildRegistryMap(plugins);
	const pageRegs = registryMap.get(point) ?? [];

	const items: PluginNavRegistration[] = [];
	for (const plugin of plugins) {
		if (!plugin.enabled) continue;
		const navItems = plugin.manifest.frontend?.navItems ?? [];
		for (const nav of navItems) {
			if (nav.scope !== scope) continue;
			const registration = pageRegs.find(
				(r) =>
					r.pluginId === plugin.manifest.id && r.component === nav.component,
			);
			if (!registration) continue;
			items.push({
				pluginId: plugin.manifest.id,
				pluginName: plugin.manifest.displayName,
				scope: nav.scope,
				slug: nav.slug,
				label: nav.label,
				icon: nav.icon,
				requiredPermission: nav.requiredPermission,
				registration,
			});
		}
	}
	return items;
}

// ── Custom permission helpers ─────────────────────────────────────────────────

/**
 * Collect plugin-declared custom permissions for a given scope ("project" or
 * "global") from all enabled plugins. Used to extend the built-in
 * PROJECT_KNOWN_PERMISSIONS / KNOWN_PERMISSIONS sets shown in the role
 * editors so admins can grant plugin permissions to roles.
 */
export function collectPluginCustomPermissions(
	plugins: Plugin[],
	scope: "project" | "global",
): Array<PluginCustomPermission & { pluginId: string; pluginName: string }> {
	const result: Array<
		PluginCustomPermission & { pluginId: string; pluginName: string }
	> = [];
	for (const plugin of plugins) {
		if (!plugin.enabled) continue;
		for (const perm of plugin.manifest.customPermissions ?? []) {
			if ((perm.scope ?? "project") !== scope) continue;
			result.push({
				...perm,
				pluginId: plugin.manifest.id,
				pluginName: plugin.manifest.displayName,
			});
		}
	}
	return result;
}

export interface PluginKnownPermission {
	key: string;
	labelKey: string;
	descriptionKey: string;
	domain: string;
	/**
	 * When set, the role editor renders this literal string instead of
	 * running `labelKey` through i18n. Used for plugin-declared custom
	 * permissions, whose label text comes from the plugin manifest (a
	 * plugin has no access to the host's i18n catalog) rather than a
	 * translation key.
	 */
	rawLabel?: string;
	/** Same as `rawLabel` but for the description text. */
	rawDescription?: string;
}

/**
 * Convert plugin-declared custom permissions (as returned by
 * `collectPluginCustomPermissions`) into role-editor-ready entries under the
 * synthetic "plugins" domain, shared by both the project and global role
 * editors. Label/description come straight from the plugin manifest
 * (`rawLabel`/`rawDescription`) since plugins can't contribute host i18n keys.
 */
export function toPluginKnownPermissions(
	pluginPermissions: Array<PluginCustomPermission & { pluginName: string }>,
): PluginKnownPermission[] {
	return pluginPermissions.map((perm) => ({
		key: perm.key,
		labelKey: "",
		descriptionKey: "",
		domain: "plugins",
		rawLabel: perm.label,
		rawDescription: perm.description
			? `${perm.description} (${perm.pluginName})`
			: perm.pluginName,
	}));
}
