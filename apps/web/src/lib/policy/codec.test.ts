import { describe, expect, it } from "vitest";

import {
	actionsToPolicy,
	allowedActionsOf,
	PLATFORM_RESOURCES,
	policyToActions,
} from "./codec";
import type { Policy, RoleScope } from "./types";

describe("actionsToPolicy", () => {
	it("writes each platform action on the roots it is checked on", () => {
		const p = actionsToPolicy(["users:read", "roles:*"], "platform");
		expect(p.statements).toEqual([
			{
				effect: "Allow",
				actions: ["users:read"],
				resources: ["user", "user/*"],
			},
			{
				effect: "Allow",
				actions: ["roles:*"],
				resources: ["role", "role/*"],
			},
		]);
	});
	it("puts a plugin action on the plugin resources only", () => {
		const p = actionsToPolicy(["dashboard:view"], "platform");
		expect(p.statements).toEqual([
			{ effect: "Allow", actions: ["dashboard:view"], resources: ["plugin/*"] },
		]);
		expect(policyToActions(p, "platform")).toEqual({
			ok: true,
			actions: ["dashboard:view"],
		});
	});
	it("still reads a platform policy written on every root", () => {
		const legacy: Policy = {
			statements: [
				{
					effect: "Allow",
					actions: ["users:read", "dashboard:view"],
					resources: [...PLATFORM_RESOURCES],
				},
			],
		};
		expect(policyToActions(legacy, "platform")).toEqual({
			ok: true,
			actions: ["users:read", "dashboard:view"],
		});
	});
	it("writes project/* for a project role", () => {
		const p = actionsToPolicy(["tasks:read"], "project");
		expect(p.statements[0].resources).toEqual(["project/*"]);
	});
	it("writes the project's own resources for a role owned by a project", () => {
		const p = actionsToPolicy(["tasks:read"], "project", "p1");
		expect(p.statements[0].resources).toEqual(["project/p1/*"]);
	});

	it("full access on a platform role is * on *", () => {
		const p = actionsToPolicy(["*", "users:read"], "platform");
		expect(p.statements).toEqual([
			{ effect: "Allow", actions: ["*"], resources: ["*"] },
		]);
	});
	it("no actions is no statements, not an empty Allow", () => {
		expect(actionsToPolicy([], "project").statements).toEqual([]);
	});
	it("removes duplicate actions", () => {
		expect(
			actionsToPolicy(["a:b", "a:b"], "project").statements[0].actions,
		).toEqual(["a:b"]);
	});
});

describe("policyToActions round trip", () => {
	const cases: [string, string[], RoleScope][] = [
		["platform subset", ["users:read", "projects:*"], "platform"],
		["platform full access", ["*"], "platform"],
		[
			"project subset",
			["tasks:read", "docs:*", "project.settings.task_types:write"],
			"project",
		],
		["project full access", ["*"], "project"],
		["empty", [], "project"],
	];
	it.each(cases)("%s", (_name, actions, scope) => {
		const policy = actionsToPolicy(actions, scope);
		const back = policyToActions(policy, scope);
		expect(back).toEqual({ ok: true, actions });
		// and the other way: re-encoding what we read gives the same document
		if (back.ok) expect(actionsToPolicy(back.actions, scope)).toEqual(policy);
	});
});

describe("policyToActions refuses what the checkboxes cannot express", () => {
	const allow = (extra: Record<string, unknown> = {}) => ({
		effect: "Allow" as const,
		actions: ["tasks:read"],
		resources: ["project/*"],
		...extra,
	});
	const reject: [string, Policy][] = [
		["a Deny", { statements: [{ ...allow(), effect: "Deny" }] }],
		[
			"a condition",
			{
				statements: [
					allow({ conditions: { StringEquals: { "task.sprint_id": "x" } } }),
				],
			},
		],
		[
			"other resources",
			{ statements: [allow({ resources: ["project/p1/task/*"] })] },
		],
		[
			"resources of the wrong scope",
			{ statements: [allow({ resources: [...PLATFORM_RESOURCES] })] },
		],
	];
	it.each(reject)("%s", (_name, policy) => {
		const r = policyToActions(policy, "project");
		expect(r.ok).toBe(false);
		if (!r.ok) expect(r.reason).not.toBe("");
	});

	it("accepts reordered resources and ignores a sid", () => {
		const p: Policy = {
			statements: [
				{
					sid: "Anything",
					effect: "Allow",
					actions: ["users:read"],
					resources: [...PLATFORM_RESOURCES].reverse(),
				},
			],
		};
		expect(policyToActions(p, "platform")).toEqual({
			ok: true,
			actions: ["users:read"],
		});
	});

	it("shows several plain Allow statements as the union of their actions", () => {
		const p: Policy = {
			statements: [
				allow(),
				allow({ actions: ["docs:read"] }),
				allow({ actions: ["tasks:read", "sprints:read"] }),
			],
		};
		expect(policyToActions(p, "project")).toEqual({
			ok: true,
			actions: ["tasks:read", "docs:read", "sprints:read"],
		});
	});

	it("shows a role converted from the old model, which names its own project", () => {
		const p: Policy = {
			statements: [
				allow({ actions: ["projects:read"], resources: ["project/p1"] }),
				allow({
					actions: ["tasks:read", "tasks:write"],
					resources: ["project/p1/*"],
				}),
				allow({
					actions: ["roles:read"],
					resources: ["project/p1/role/*"],
				}),
			],
		};
		expect(policyToActions(p, "project")).toEqual({
			ok: true,
			actions: ["projects:read", "tasks:read", "tasks:write", "roles:read"],
		});
	});

	it("shows a converted project-role template, whose roles statement names project/*/role/*", () => {
		const p: Policy = {
			statements: [
				allow({
					sid: "Migrated",
					actions: ["tasks:read", "projects:read"],
					resources: ["project/*"],
				}),
				allow({
					sid: "MigratedRoles",
					actions: ["roles:read"],
					resources: ["project/*/role/*"],
				}),
			],
		};
		expect(policyToActions(p, "project")).toEqual({
			ok: true,
			actions: ["tasks:read", "projects:read", "roles:read"],
		});
	});

	it("does not widen a kind-specific resource for a non-roles action", () => {
		const p: Policy = {
			statements: [
				allow({ actions: ["tasks:read"], resources: ["project/p1/role/*"] }),
			],
		};
		expect(policyToActions(p, "project").ok).toBe(false);
	});

	it("for a project's role, accepts only that project's resources", () => {
		const ok: Policy = {
			statements: [allow({ resources: ["project/p1/*"] })],
		};
		expect(policyToActions(ok, "project", "p1").ok).toBe(true);
		const otherProject: Policy = {
			statements: [allow({ resources: ["project/p2/*"] })],
		};
		expect(policyToActions(otherProject, "project", "p1").ok).toBe(false);
		const wildcard: Policy = { statements: [allow()] }; // project/*
		expect(policyToActions(wildcard, "project", "p1").ok).toBe(false);
		// Without a project (a template) any project form is shown.
		expect(policyToActions(wildcard, "project").ok).toBe(true);
	});

	it("treats empty conditions as none", () => {
		const p: Policy = { statements: [allow({ conditions: {} })] };
		expect(policyToActions(p, "project").ok).toBe(true);
	});
});

describe("allowedActionsOf", () => {
	it("unions the actions of Allow statements and skips Deny", () => {
		expect(
			allowedActionsOf({
				statements: [
					{ effect: "Allow", actions: ["a:x", "b:y"], resources: ["*"] },
					{ effect: "Allow", actions: ["a:x"], resources: ["*"] },
					{ effect: "Deny", actions: ["c:z"], resources: ["*"] },
				],
			}).sort(),
		).toEqual(["a:x", "b:y"]);
	});
});
