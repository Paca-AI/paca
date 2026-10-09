import { describe, expect, it } from "vitest";

import {
	actionDomain,
	dedupeActions,
	expandGranted,
	hasAction,
	hasAnyAction,
	matchAction,
	normalizeToWildcards,
} from "./actions";

describe("matchAction", () => {
	it.each([
		["*", "tasks:write", true],
		["tasks:*", "tasks:write", true],
		["tasks:*", "sprints:write", false],
		["tasks:write", "tasks:write", true],
		["tasks:write", "tasks:read", false],
		// a dotted domain is its own domain: "project:*" is not "project.settings:*"
		["project:*", "project.settings.task_types:write", false],
		[
			"project.settings.task_types:*",
			"project.settings.task_types:write",
			true,
		],
		["task:*", "tasks:write", false],
	])("matchAction(%s, %s) = %s", (pattern, action, want) => {
		expect(matchAction(pattern, action)).toBe(want);
	});
});

describe("hasAction / hasAnyAction", () => {
	it("is false for an empty grant", () => {
		expect(hasAction([], "tasks:read")).toBe(false);
	});
	it("honours wildcards in the granted set", () => {
		expect(hasAction(["users:read", "tasks:*"], "tasks:write")).toBe(true);
		expect(hasAction(["*"], "anything:at_all")).toBe(true);
	});
	it("hasAnyAction needs one of them", () => {
		expect(hasAnyAction(["tasks:read"], ["sprints:read", "tasks:read"])).toBe(
			true,
		);
		expect(hasAnyAction(["tasks:read"], ["sprints:read"])).toBe(false);
	});
});

describe("dedupeActions", () => {
	it("collapses to * when * is granted", () => {
		expect(dedupeActions(["tasks:read", "*"])).toEqual(["*"]);
	});
	it("drops actions implied by a domain wildcard", () => {
		expect(dedupeActions(["tasks:read", "tasks:*", "docs:read"])).toEqual([
			"tasks:*",
			"docs:read",
		]);
	});
	it("keeps a nested domain's actions under a shorter wildcard", () => {
		expect(
			dedupeActions(["project:*", "project.settings.task_types:write"]),
		).toEqual(["project:*", "project.settings.task_types:write"]);
	});
	it("removes duplicates", () => {
		expect(dedupeActions(["tasks:read", "tasks:read"])).toEqual(["tasks:read"]);
	});
});

describe("normalizeToWildcards / expandGranted", () => {
	const known = ["tasks:read", "tasks:write", "docs:read", "docs:write"];
	it("uses a domain wildcard when every action of the domain is checked", () => {
		expect(
			normalizeToWildcards(["tasks:read", "tasks:write", "docs:read"], known),
		).toEqual(["tasks:*", "docs:read"]);
	});
	it("keeps individual actions when the domain is partial", () => {
		expect(normalizeToWildcards(["tasks:read"], known)).toEqual(["tasks:read"]);
	});
	it("keeps checked actions the catalogue does not know", () => {
		expect(normalizeToWildcards(["tasks:read", "weird:thing"], known)).toEqual([
			"tasks:read",
			"weird:thing",
		]);
	});
	it("returns * for *", () => {
		expect(normalizeToWildcards(["*", "tasks:read"], known)).toEqual(["*"]);
	});
	it("round-trips through expandGranted", () => {
		const checked = ["tasks:read", "tasks:write"];
		const granted = normalizeToWildcards(checked, known);
		const expanded = expandGranted(granted, known);
		expect(Object.keys(expanded).filter((k) => expanded[k])).toEqual(checked);
	});
});

describe("actionDomain", () => {
	it("splits at the colon", () => {
		expect(actionDomain("tasks:read")).toBe("tasks");
		expect(actionDomain("project.settings.task_types:write")).toBe(
			"project.settings.task_types",
		);
	});
});
