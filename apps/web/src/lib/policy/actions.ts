/**
 * IAM action matching, mirroring the server's iam.MatchAction: an action is
 * "<domain>:<verb>" (the domain may contain dots, e.g.
 * "project.settings.task_types:write"); a grant is "*", "<domain>:*" or the
 * action itself. "project:*" does not cover "project.settings:write".
 */
export function matchAction(pattern: string, action: string): boolean {
	if (pattern === "*") return true;
	if (pattern.endsWith(":*")) {
		return action.startsWith(pattern.slice(0, -1));
	}
	return pattern === action;
}

/** Whether any of the granted actions covers the required one. */
export function hasAction(
	granted: readonly string[],
	required: string,
): boolean {
	return granted.some((g) => matchAction(g, required));
}

/** Whether any of the required actions is covered. */
export function hasAnyAction(
	granted: readonly string[],
	required: readonly string[],
): boolean {
	return required.some((r) => hasAction(granted, r));
}

/** The domain of an action: "tasks" for "tasks:read". */
export function actionDomain(action: string): string {
	const i = action.indexOf(":");
	return i === -1 ? action : action.slice(0, i);
}

/** Drops every granted action already implied by another granted pattern
 *  ("tasks:read" once "tasks:*" is granted; everything once "*" is). */
export function dedupeActions(granted: readonly string[]): string[] {
	if (granted.includes("*")) return ["*"];
	const unique = [...new Set(granted)];
	return unique.filter(
		(a) =>
			a.endsWith(":*") ||
			!unique.some((other) => other !== a && matchAction(other, a)),
	);
}

/**
 * Collapses a checked set into wildcards where a whole domain is checked:
 * when every known action of a domain is in `checked`, the domain's
 * "<domain>:*" is used instead. `known` is the catalogue the dialog offers.
 * Anything not in `known` that is checked stays as it is.
 */
export function normalizeToWildcards(
	checked: readonly string[],
	known: readonly string[],
): string[] {
	const set = new Set(checked);
	if (set.has("*")) return ["*"];
	const byDomain = new Map<string, string[]>();
	for (const a of known) {
		const d = actionDomain(a);
		byDomain.set(d, [...(byDomain.get(d) ?? []), a]);
	}
	const out: string[] = [];
	const covered = new Set<string>();
	for (const [domain, actions] of byDomain) {
		if (actions.length > 0 && actions.every((a) => set.has(a))) {
			out.push(`${domain}:*`);
			for (const a of actions) covered.add(a);
		}
	}
	for (const a of checked) if (!covered.has(a)) out.push(a);
	return out;
}

/** Marks which of the known actions the granted set covers, so a checkbox
 *  reads as checked under exactly the rule that grants access. */
export function expandGranted(
	granted: readonly string[],
	known: readonly string[],
): Record<string, boolean> {
	const out: Record<string, boolean> = {};
	for (const a of known) out[a] = hasAction(granted, a);
	return out;
}
