import {
	dedupeActions,
	expandGranted,
	hasAction,
	hasAnyAction,
	normalizeToWildcards,
} from "./policy/actions";

/** A permission the role editor can offer: an IAM action ("tasks:read") and
 *  the UI group it is listed under. */
export interface PermissionDefinition {
	key: string;
	domain: string;
}

/** The checkbox state of a role editor: action -> checked. */
export type PermissionMap = Record<string, boolean>;

/** Whether the granted IAM actions cover the required one. Wildcards are "*"
 *  and "<domain>:*" (see matchAction); "project:*" does not cover the nested
 *  "project.settings.task_types:write". */
export function hasPermission(
	grantedPermissions: string[],
	requiredPermission: string,
): boolean {
	return hasAction(grantedPermissions, requiredPermission);
}

export function hasAnyPermission(
	grantedPermissions: string[],
	requiredPermissions: string[],
): boolean {
	return hasAnyAction(grantedPermissions, requiredPermissions);
}

/** Drops any granted action already implied by another granted pattern, so a
 *  role's actions read as a clean list wherever they are displayed. */
export function dedupeGrantedPermissions(
	grantedPermissions: string[],
): string[] {
	return dedupeActions(grantedPermissions);
}

/** Marks which known permissions a set of granted actions covers, so a
 *  checkbox reads as checked under exactly the rule that grants access. */
export function expandWildcardPermissions(
	source: PermissionMap | undefined,
	knownPermissions: PermissionDefinition[],
): PermissionMap {
	if (!source) return {};
	const granted = Object.keys(source).filter((key) => source[key] === true);
	return expandGranted(
		granted,
		knownPermissions.map((p) => p.key),
	);
}

/** The compact action list for a checkbox state: a domain whose every known
 *  permission is checked becomes "<domain>:*"; "*" stands alone. */
export function normalizePermissionsToWildcards(
	source: PermissionMap,
	knownPermissions: PermissionDefinition[],
): PermissionMap {
	if (source["*"] === true) return { "*": true };
	const checked = Object.keys(source).filter((key) => source[key] === true);
	const out: PermissionMap = {};
	for (const action of normalizeToWildcards(
		checked,
		knownPermissions.map((p) => p.key),
	)) {
		out[action] = true;
	}
	return out;
}
