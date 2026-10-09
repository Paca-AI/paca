import { useCallback, useEffect, useRef, useState } from "react";

import {
	expandWildcardPermissions,
	normalizePermissionsToWildcards,
	type PermissionMap,
} from "@/lib/permissions";
import {
	actionsToPolicy,
	matchAction,
	type Policy,
	type PolicyIssue,
	policyToActions,
	type RoleScope,
} from "@/lib/policy";
import { validatePolicy } from "@/lib/role-api";

export type EditorMode = "simple" | "advanced";

/** Why the simple view cannot show a policy, as a translation key under the
 *  `roles` namespace's editor.reasons. */
export type BlockedReason =
	| "deny"
	| "conditions"
	| "resources"
	| "unknownActions"
	| "invalidJson";

const REASON_BY_MESSAGE: Record<string, BlockedReason> = {
	"The role has a Deny statement.": "deny",
	"The role has conditions.": "conditions",
	"The role applies to specific resources.": "resources",
};

const VALIDATE_DEBOUNCE_MS = 400;

export function prettyPolicy(policy: Policy): string {
	return JSON.stringify(policy, null, 2);
}

/** Actions a policy grants that the simple view's catalogue cannot show: a
 *  checkbox for them would not exist, so saving from the simple view would
 *  silently drop them. "*" and "<domain>:*" are fine when they cover at least
 *  one listed permission. */
function unlistedActions(actions: string[], known: string[]): string[] {
	return actions.filter((a) => {
		if (a === "*") return false;
		if (known.includes(a)) return false;
		return !known.some((k) => matchAction(a, k));
	});
}

interface Init {
	/** True while the checked set is a role's stored "*": saving it unchanged
	 *  must write "*" again, not today's enumerated wildcards, so a permission
	 *  added later is still covered. The first toggle ends it. */
	fullAccess: boolean;
	mode: EditorMode;
	checked: PermissionMap;
	json: string;
	blocked: BlockedReason | null;
}

function initialState(
	policy: Policy,
	scope: RoleScope,
	known: string[],
	projectId?: string,
): Init {
	const json = prettyPolicy(policy);
	const r = policyToActions(policy, scope, projectId);
	if (!r.ok) {
		return {
			fullAccess: false,
			mode: "advanced",
			checked: {},
			json,
			blocked: REASON_BY_MESSAGE[r.reason] ?? "resources",
		};
	}
	if (unlistedActions(r.actions, known).length > 0) {
		return {
			fullAccess: false,
			mode: "advanced",
			checked: {},
			json,
			blocked: "unknownActions",
		};
	}
	return {
		fullAccess: r.actions.includes("*"),
		mode: "simple",
		checked: expandWildcardPermissions(
			Object.fromEntries(r.actions.map((a) => [a, true])),
			known.map((key) => ({ key, domain: "" })),
		),
		json,
		blocked: null,
	};
}

export interface PolicyEditor {
	mode: EditorMode;
	checked: PermissionMap;
	/** The role is stored as "*" and nothing has been toggled yet. */
	fullAccess: boolean;
	json: string;
	/** Set when the simple view cannot show the current policy. */
	blocked: BlockedReason | null;
	/** Problems found by the server in the JSON being edited. */
	issues: PolicyIssue[];
	validating: boolean;
	/** A syntax error in the JSON, or null. */
	parseError: string | null;
	toggle: (key: string, checked: boolean) => void;
	/** Sets several permissions at once (a group's select all / none). */
	setMany: (keys: string[], checked: boolean) => void;
	setJson: (json: string) => void;
	/** Switches view; leaving advanced for simple only works when the JSON is
	 *  expressible, otherwise it stays and `blocked` says why. */
	setMode: (mode: EditorMode) => void;
	/** The policy to save, or null when the JSON does not parse. */
	buildPolicy: () => Policy | null;
	reset: (policy: Policy) => void;
}

/**
 * State of a role's permission editor: the simple view (a switch per known
 * action) and the advanced view (the policy as JSON) edit one policy. Going
 * from simple to advanced writes the checked actions as a policy; going back
 * only works when the JSON is something the switches express, and the JSON is
 * never rewritten or dropped by a failed switch back.
 */
export function usePolicyEditor(
	policy: Policy,
	scope: RoleScope,
	known: string[],
	projectId: string | undefined,
	active: boolean,
): PolicyEditor {
	const [state, setState] = useState<Init>(() =>
		initialState(policy, scope, known, projectId),
	);
	const [issues, setIssues] = useState<PolicyIssue[]>([]);
	const [validating, setValidating] = useState(false);
	const [parseError, setParseError] = useState<string | null>(null);
	const dirty = useRef(false);

	const knownKey = known.join("|");
	const policyKey = JSON.stringify(policy);
	// biome-ignore lint/correctness/useExhaustiveDependencies: known/policy are tracked by their keys so a fresh array with the same content does not reset the editor.
	useEffect(() => {
		if (!active) return;
		// Plugin permissions load after the dialog opens; re-derive from the
		// stored policy until the user has touched something.
		if (dirty.current) return;
		setState(initialState(policy, scope, known, projectId));
		setIssues([]);
		setParseError(null);
	}, [active, policyKey, knownKey, scope, projectId]);

	const simplePolicy = useCallback(
		(checked: PermissionMap, fullAccess: boolean): Policy => {
			if (fullAccess) return actionsToPolicy(["*"], scope, projectId);
			const norm = normalizePermissionsToWildcards(
				checked,
				known.map((key) => ({ key, domain: "" })),
			);
			return actionsToPolicy(Object.keys(norm), scope, projectId);
		},
		[known, scope, projectId],
	);

	const parse = useCallback((json: string): Policy | null => {
		try {
			const v = JSON.parse(json) as unknown;
			if (v === null || typeof v !== "object" || Array.isArray(v)) return null;
			const p = v as Policy;
			if (!Array.isArray(p.statements)) return null;
			return p;
		} catch {
			return null;
		}
	}, []);

	// Validate the JSON with the server (debounced) while editing it.
	const json = state.json;
	const mode = state.mode;
	useEffect(() => {
		if (!active || mode !== "advanced") return;
		const parsed = parse(json);
		if (!parsed) {
			setIssues([]);
			setValidating(false);
			return;
		}
		let cancelled = false;
		setValidating(true);
		const timer = setTimeout(() => {
			validatePolicy(parsed, projectId)
				.then((r) => {
					if (!cancelled) setIssues(r.valid ? [] : r.issues);
				})
				.catch(() => {
					if (!cancelled) setIssues([]);
				})
				.finally(() => {
					if (!cancelled) setValidating(false);
				});
		}, VALIDATE_DEBOUNCE_MS);
		return () => {
			cancelled = true;
			clearTimeout(timer);
		};
	}, [active, mode, json, projectId, parse]);

	const toggle = (key: string, checked: boolean) => {
		dirty.current = true;
		setState((s) => ({
			...s,
			fullAccess: false,
			checked: { ...s.checked, [key]: checked },
		}));
	};

	const setMany = (keys: string[], checked: boolean) => {
		dirty.current = true;
		setState((s) => ({
			...s,
			fullAccess: false,
			checked: {
				...s.checked,
				...Object.fromEntries(keys.map((key) => [key, checked])),
			},
		}));
	};

	const setJson = (next: string) => {
		dirty.current = true;
		setParseError(parse(next) ? null : errorOf(next));
		setState((s) => ({ ...s, json: next, blocked: null }));
	};

	const setMode = (next: EditorMode) => {
		dirty.current = true;
		if (next === state.mode) return;
		if (next === "advanced") {
			setState((s) => ({
				...s,
				mode: "advanced",
				json: prettyPolicy(simplePolicy(s.checked, s.fullAccess)),
				blocked: null,
			}));
			setParseError(null);
			return;
		}
		const parsed = parse(state.json);
		if (!parsed) {
			setParseError(errorOf(state.json));
			setState((s) => ({ ...s, blocked: "invalidJson" }));
			return;
		}
		const init = initialState(parsed, scope, known, projectId);
		if (init.mode === "advanced") {
			setState((s) => ({ ...s, blocked: init.blocked }));
			return;
		}
		setState((s) => ({
			...s,
			mode: "simple",
			checked: init.checked,
			fullAccess: init.fullAccess,
			blocked: null,
		}));
	};

	const buildPolicy = (): Policy | null =>
		state.mode === "simple"
			? simplePolicy(state.checked, state.fullAccess)
			: parse(state.json);

	const reset = (next: Policy) => {
		dirty.current = false;
		setState(initialState(next, scope, known, projectId));
		setIssues([]);
		setParseError(null);
	};

	return {
		mode: state.mode,
		checked: state.checked,
		fullAccess: state.fullAccess,
		json: state.json,
		blocked: state.blocked,
		issues,
		validating,
		parseError,
		toggle,
		setMany,
		setJson,
		setMode,
		buildPolicy,
		reset,
	};
}

function errorOf(json: string): string {
	try {
		JSON.parse(json);
		return "expected an object with a statements list";
	} catch (e) {
		return e instanceof Error ? e.message : String(e);
	}
}
