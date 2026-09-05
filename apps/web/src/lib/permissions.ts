export interface PermissionDefinition {
	key: string;
	domain: string;
}

export type PermissionMap = Record<string, boolean>;

export function hasPermission(
	grantedPermissions: string[],
	requiredPermission: string,
): boolean {
	if (grantedPermissions.includes("*")) return true;
	if (grantedPermissions.includes(requiredPermission)) return true;

	const lastDotIndex = requiredPermission.lastIndexOf(".");
	if (lastDotIndex === -1) return false;

	const prefix = requiredPermission.slice(0, lastDotIndex);
	return grantedPermissions.includes(`${prefix}.*`);
}

export function hasAnyPermission(
	grantedPermissions: string[],
	requiredPermissions: string[],
): boolean {
	return requiredPermissions.some((permission) =>
		hasPermission(grantedPermissions, permission),
	);
}

/** Drops any granted key already implied by another granted wildcard in the
 * same set — e.g. "environments.read" is redundant once "environments.*" is
 * also granted, and everything but "*" itself is redundant once "*" is
 * granted. A role's raw permission map can end up with both (see
 * 000044_add_environment_permissions.sql's doc comment for how a wildcard
 * merged onto an existing individual grant produces exactly this), which is
 * harmless for `hasPermission` above but reads as a doubled-up, confusing
 * list wherever a role's permissions are displayed verbatim (e.g.
 * RolesSettings.tsx's badge list) — this is what that display should filter
 * through instead of rendering `grantedPermissions` as-is. */
export function dedupeGrantedPermissions(
	grantedPermissions: string[],
): string[] {
	const granted = new Set(grantedPermissions);
	if (granted.has("*")) return ["*"];

	return grantedPermissions.filter((key) => {
		if (key.endsWith(".*")) return true;
		return !granted.has(`${keyPrefix(key)}.*`);
	});
}

/** The part of a permission key before its last dot, e.g. "time_logging" for
 * "time_logging.view_all". This is the namespace a `.write`-style wildcard
 * grant actually covers — NOT `PermissionDefinition.domain`, which is only a
 * UI-grouping label. For built-in permissions the two happen to coincide,
 * but plugin-declared permissions all share the synthetic "plugins" domain
 * while each has its own real key prefix, so domain-based wildcard checks
 * silently fail for them. */
function keyPrefix(key: string): string {
	const lastDotIndex = key.lastIndexOf(".");
	return lastDotIndex === -1 ? key : key.slice(0, lastDotIndex);
}

export function expandWildcardPermissions(
	source: PermissionMap | undefined,
	knownPermissions: PermissionDefinition[],
): PermissionMap {
	if (!source) return {};

	const expanded: PermissionMap = {};
	const hasGlobalWildcard = source["*"] === true;

	for (const permission of knownPermissions) {
		const prefixWildcard = `${keyPrefix(permission.key)}.*`;
		expanded[permission.key] =
			hasGlobalWildcard ||
			source[prefixWildcard] === true ||
			source[permission.key] === true;
	}

	return expanded;
}

export function normalizePermissionsToWildcards(
	source: PermissionMap,
	knownPermissions: PermissionDefinition[],
): PermissionMap {
	if (source["*"] === true) {
		return { "*": true };
	}

	// Group by each permission's own key prefix (see `keyPrefix`), NOT by
	// `domain` — collapsing by the UI-grouping label would produce a bogus
	// "plugins.*" key that (a) doesn't match any real permission check
	// (hasPermission only understands `${realPrefix}.*`) and (b) would
	// over-grant every other plugin's permissions sharing that UI group.
	const permissionsByPrefix = new Map<string, PermissionDefinition[]>();
	for (const permission of knownPermissions) {
		const prefix = keyPrefix(permission.key);
		const existing = permissionsByPrefix.get(prefix) ?? [];
		existing.push(permission);
		permissionsByPrefix.set(prefix, existing);
	}

	const normalized: PermissionMap = {};
	for (const [prefix, prefixPermissions] of permissionsByPrefix) {
		const enabledPermissions = prefixPermissions.filter(
			(permission) => source[permission.key] === true,
		);
		if (enabledPermissions.length === 0) continue;

		if (enabledPermissions.length === prefixPermissions.length) {
			normalized[`${prefix}.*`] = true;
			continue;
		}

		for (const permission of enabledPermissions) {
			normalized[permission.key] = true;
		}
	}

	return normalized;
}
