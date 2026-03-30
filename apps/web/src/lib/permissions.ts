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

export function expandWildcardPermissions(
	source: PermissionMap | undefined,
	knownPermissions: PermissionDefinition[],
): PermissionMap {
	if (!source) return {};

	const expanded: PermissionMap = {};
	const hasGlobalWildcard = source["*"] === true;

	for (const permission of knownPermissions) {
		const domainWildcard = `${permission.domain}.*`;
		expanded[permission.key] =
			hasGlobalWildcard ||
			source[domainWildcard] === true ||
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

	const permissionsByDomain = new Map<string, PermissionDefinition[]>();
	for (const permission of knownPermissions) {
		const existing = permissionsByDomain.get(permission.domain) ?? [];
		existing.push(permission);
		permissionsByDomain.set(permission.domain, existing);
	}

	const normalized: PermissionMap = {};
	for (const [domain, domainPermissions] of permissionsByDomain) {
		const enabledPermissions = domainPermissions.filter(
			(permission) => source[permission.key] === true,
		);
		if (enabledPermissions.length === 0) continue;

		if (enabledPermissions.length === domainPermissions.length) {
			normalized[`${domain}.*`] = true;
			continue;
		}

		for (const permission of enabledPermissions) {
			normalized[permission.key] = true;
		}
	}

	return normalized;
}
