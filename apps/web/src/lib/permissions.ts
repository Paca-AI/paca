export interface PermissionDefinition {
	key: string;
	domain: string;
}

export type PermissionMap = Record<string, boolean>;

/**
 * Mirrors the Go backend's own matcher (internal/platform/authz/authorizer.go's
 * hasPermission): every granted key ending in ".*" is tried as a prefix
 * against requiredPermission, not just one wildcard derived from
 * requiredPermission's *immediate* parent. That distinction matters now
 * that some domains nest a wildcard below the top level —
 * "project.settings.*" must cover "project.settings.task_statuses.read",
 * but checking only the immediate-parent candidate
 * ("project.settings.task_statuses.*") never looks at "project.settings.*"
 * at all, so a role granted just the broader wildcard read as having none
 * of its narrower permissions.
 */
export function hasPermission(
	grantedPermissions: string[],
	requiredPermission: string,
): boolean {
	if (grantedPermissions.includes("*")) return true;
	if (grantedPermissions.includes(requiredPermission)) return true;

	return grantedPermissions.some((granted) => {
		if (!granted.endsWith(".*")) return false;
		const prefix = granted.slice(0, -1); // strip the trailing "*", keep the dot
		return requiredPermission.startsWith(prefix);
	});
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
	if (grantedPermissions.includes("*")) return ["*"];

	return grantedPermissions.filter((key) => {
		if (key.endsWith(".*")) return true;
		// A key is redundant once *any* other granted wildcard's prefix
		// covers it — not just the one derived from its immediate parent
		// (see hasPermission's doc comment for why a single derived
		// candidate misses a broader wildcard like "project.settings.*"
		// covering "project.settings.task_statuses.read").
		return !grantedPermissions.some(
			(other) => other !== key && hasPermission([other], key),
		);
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

	// Delegates to hasPermission so a role-editor checkbox reads as checked
	// under exactly the same rule that actually grants access — including a
	// wildcard nested above a permission's immediate parent (e.g. a role
	// with just "project.settings.*" checks every project.settings.* box,
	// not only ones matching "project.settings.<same-immediate-parent>.*").
	const granted = Object.keys(source).filter((key) => source[key] === true);

	const expanded: PermissionMap = {};
	for (const permission of knownPermissions) {
		expanded[permission.key] = hasPermission(granted, permission.key);
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
