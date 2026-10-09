import { dedupeGrantedPermissions } from "@/lib/permissions";
import { allowedActionsOf, type Policy } from "@/lib/policy";

export function permissionBadgeClass(key: string): string {
	const domain = key.split(":")[0];
	if (domain === "roles") {
		return "bg-primary/10 text-primary border-primary/20 dark:bg-primary/20";
	}
	if (domain === "users") {
		return "bg-amber-50 text-amber-700 border-amber-200 dark:bg-amber-900/20 dark:text-amber-400 dark:border-amber-700/30";
	}
	return "bg-muted text-muted-foreground border-border";
}

/** The actions a role's Allow statements grant, with those a wildcard already
 *  covers folded away ("*" stands alone). Resources, conditions and Deny
 *  statements are ignored: this is a glance, never a decision. */
export function activePermissions(policy: Policy): string[] {
	return dedupeGrantedPermissions(allowedActionsOf(policy));
}

/** Whether the role has features a glance can't show: a Deny or a condition. */
export function hasAdvancedStatements(policy: Policy): boolean {
	return (policy.statements ?? []).some(
		(s) =>
			s.effect === "Deny" ||
			(s.conditions && Object.keys(s.conditions).length > 0),
	);
}

/** A role holding the `*` wildcard can do everything. */
export function isFullAccessRole(role: { policy: Policy }): boolean {
	return activePermissions(role.policy).includes("*");
}
