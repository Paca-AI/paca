import type { Policy, RoleScope, Statement } from "./types";

export const POLICY_VERSION = "2026-10-01";

/** The resources a platform role's checkboxes apply to: the workspace-level
 *  roots, as the built-in ADMIN and USER roles use them. */
export const PLATFORM_RESOURCES = [
	"user",
	"user/*",
	"role",
	"role/*",
	"plugin",
	"plugin/*",
	"settings",
	"sso",
	"agent",
	"agent/*",
	"project",
] as const;

/** A project role is attached to one project and so only ever acts inside it:
 *  "project/*" covers the project and everything in it. */
export const PROJECT_RESOURCES = ["project/*"] as const;

/** The platform roots a built-in workspace action is checked on, by domain
 *  (mirrors the server's iam.PlatformRootFor). */
const PLATFORM_ROOTS: Record<string, readonly string[]> = {
	users: ["user", "user/*"],
	roles: ["role", "role/*"],
	plugins: ["plugin", "plugin/*"],
	settings: ["settings"],
	"settings.sso": ["sso"],
	agents: ["agent", "agent/*"],
	projects: ["project"],
};

/** Where a workspace-wide check of one action lands: its platform root, or
 *  for a plugin's own action (no root) the plugin resources. */
function platformRootsOf(action: string): readonly string[] {
	const i = action.indexOf(":");
	const domain = i === -1 ? action : action.slice(0, i);
	return PLATFORM_ROOTS[domain] ?? ["plugin/*"];
}

/** The resources a set of platform actions is checked on, in the canonical
 *  PLATFORM_RESOURCES order. */
function platformResourcesOf(actions: readonly string[]): string[] {
	const roots = new Set(actions.flatMap((a) => platformRootsOf(a)));
	const canonical: readonly string[] = PLATFORM_RESOURCES;
	return [
		...canonical.filter((r) => roots.has(r)),
		...[...roots].filter((r) => !canonical.includes(r)),
	];
}

function resourcesFor(
	scope: RoleScope,
	actions: readonly string[],
	projectId?: string,
): string[] {
	if (scope === "platform") {
		// Full access to everything, including resources outside the roots.
		return actions.length === 1 && actions[0] === "*"
			? ["*"]
			: platformResourcesOf(actions);
	}
	// A project's own role only ever names its own project, which the API
	// enforces. Without a project (a template) it is "project/*".
	return projectId ? [`project/${projectId}/*`] : [...PROJECT_RESOURCES];
}

/** The policy for a set of checked actions: one Allow statement, or none. */
export function actionsToPolicy(
	actions: readonly string[],
	scope: RoleScope,
	projectId?: string,
): Policy {
	const unique = [...new Set(actions)];
	if (unique.length === 0) {
		return { version: POLICY_VERSION, statements: [] };
	}
	const list = unique.includes("*") ? ["*"] : unique;
	// A platform action applies only where it is checked, so actions sharing
	// the same resources share one statement and the rest get their own.
	const groups = new Map<string, string[]>();
	if (scope === "platform" && list[0] !== "*") {
		for (const a of list) {
			const key = platformResourcesOf([a]).join("\n");
			groups.set(key, [...(groups.get(key) ?? []), a]);
		}
	} else {
		groups.set("", list);
	}
	return {
		version: POLICY_VERSION,
		statements: [...groups.values()].map((acts) => ({
			effect: "Allow" as const,
			actions: acts,
			resources: resourcesFor(scope, acts, projectId),
		})),
	};
}

export type PolicyToActions =
	| { ok: true; actions: string[] }
	| { ok: false; reason: string };

function sameSet(a: readonly string[], b: readonly string[]): boolean {
	return (
		a.length === b.length &&
		new Set(a).size === new Set(b).size &&
		a.every((x) => b.includes(x))
	);
}

function isSimple(st: Statement): boolean {
	return !st.conditions || Object.keys(st.conditions).length === 0;
}

const escapeRe = (v: string) => v.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

/**
 * Whether one statement's resources mean, for these actions, the same thing the
 * checkboxes write. Platform roles must list exactly the platform roots. A
 * project role names its own project: `project/<id>` or `project/<id>/*`
 * (a template, with no project, names any project or `project/*`).
 * `project/<id>/role/*` counts too when every action is a roles action,
 * because those only ever act on role resources, so the wider
 * `project/<id>/*` grants nothing more.
 */
function resourcesAreCanonical(
	scope: RoleScope,
	actions: readonly string[],
	resources: readonly string[],
	projectId?: string,
): boolean {
	if (scope === "platform") {
		// The resources the actions are checked on, or the full set of roots
		// the checkboxes used to write for every platform action.
		return (
			sameSet(resources, resourcesFor(scope, actions)) ||
			sameSet(resources, PLATFORM_RESOURCES)
		);
	}
	if (resources.length === 0) return false;
	const seg = projectId ? escapeRe(projectId) : "[^/]+";
	const root = new RegExp(`^project/${seg}$`);
	const all = new RegExp(`^project/${seg}/\\*$`);
	const roles = new RegExp(`^project/${seg}/role/\\*$`);
	const onlyRolesActions =
		actions.length > 0 && actions.every((a) => a.startsWith("roles:"));
	return resources.every(
		(r) => root.test(r) || all.test(r) || (onlyRolesActions && roles.test(r)),
	);
}

/**
 * The checked actions a policy stands for, when the checkbox view can show it
 * without changing its meaning: Allow statements with no conditions whose
 * resources are the ones `actionsToPolicy` writes (or, for a project role, the
 * project's own id, see resourcesAreCanonical). Several such statements are
 * the union of their actions. Anything else (a Deny, a condition, other
 * resources) is reported as not representable, so the caller keeps the JSON
 * as it is and never rewrites it from the checkboxes.
 */
export function policyToActions(
	policy: Policy,
	scope: RoleScope,
	projectId?: string,
): PolicyToActions {
	const sts = policy.statements ?? [];
	const actions = new Set<string>();
	for (const st of sts) {
		if (st.effect !== "Allow") {
			return { ok: false, reason: "The role has a Deny statement." };
		}
		if (!isSimple(st)) {
			return { ok: false, reason: "The role has conditions." };
		}
		const acts = st.actions ?? [];
		if (!resourcesAreCanonical(scope, acts, st.resources ?? [], projectId)) {
			return {
				ok: false,
				reason: "The role applies to specific resources.",
			};
		}
		for (const a of acts) actions.add(a);
	}
	return { ok: true, actions: [...actions] };
}

/**
 * Whether a policy is a project-role template: it has statements and every
 * resource it names has a wildcard in the project position (`project/*`,
 * `project/*\/task/*`). Such a role has no owning project and is attached per
 * project; roles converted from the old project roles look like this. It must
 * be edited with the project permission catalogue, not the workspace one. A
 * role naming specific projects (`project/<id>/*`) is not a template.
 */
export function isProjectTemplate(policy: Policy): boolean {
	const sts = policy.statements ?? [];
	return (
		sts.length > 0 &&
		sts.every(
			(st) =>
				(st.resources ?? []).length > 0 &&
				(st.resources ?? []).every(
					(r) => r === "project/*" || r.startsWith("project/*/"),
				),
		)
	);
}

/** Every action a policy allows by an Allow statement, ignoring resources,
 *  conditions and Deny statements. Used for summaries only, never to decide
 *  access. */
export function allowedActionsOf(policy: Policy): string[] {
	const out = new Set<string>();
	for (const st of policy.statements ?? []) {
		if (st.effect !== "Allow") continue;
		for (const a of st.actions ?? []) out.add(a);
	}
	return [...out];
}
