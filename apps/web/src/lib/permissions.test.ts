import { describe, expect, it } from "vitest";

import {
	dedupeGrantedPermissions,
	expandWildcardPermissions,
	hasAnyPermission,
	hasPermission,
	normalizePermissionsToWildcards,
	type PermissionDefinition,
} from "./permissions";

const known: PermissionDefinition[] = [
	{ key: "users:read", domain: "users" },
	{ key: "users:write", domain: "users" },
	{ key: "projects:read", domain: "projects" },
	{ key: "projects:write", domain: "projects" },
	{ key: "time_logging:manage_all", domain: "plugins" },
	{ key: "time_logging:log", domain: "plugins" },
];

describe("hasPermission", () => {
	it("supports exact, domain wildcard and global wildcard", () => {
		expect(hasPermission(["users:read"], "users:read")).toBe(true);
		expect(hasPermission(["users:*"], "users:write")).toBe(true);
		expect(hasPermission(["*"], "projects:write")).toBe(true);
		expect(hasPermission(["users:read"], "projects:read")).toBe(false);
		expect(hasPermission(["users:read"], "invalid-format")).toBe(false);
	});

	it("treats a dotted domain as its own domain", () => {
		expect(hasPermission(["project.members:*"], "project.members:write")).toBe(
			true,
		);
		expect(hasPermission(["project:*"], "project.members:write")).toBe(false);
		expect(
			hasPermission(
				["project.settings:*"],
				"project.settings.task_types:write",
			),
		).toBe(false);
		expect(
			hasPermission(
				["project.settings.task_types:*"],
				"project.settings.task_types:write",
			),
		).toBe(true);
	});
});

describe("hasAnyPermission", () => {
	it("needs one of the required actions", () => {
		expect(
			hasAnyPermission(["users:read"], ["projects:read", "users:read"]),
		).toBe(true);
		expect(hasAnyPermission(["users:read"], ["projects:read"])).toBe(false);
	});
});

describe("dedupeGrantedPermissions", () => {
	it("collapses to * and drops what a wildcard implies", () => {
		expect(dedupeGrantedPermissions(["users:read", "*"])).toEqual(["*"]);
		expect(
			dedupeGrantedPermissions(["users:read", "users:*", "projects:read"]),
		).toEqual(["users:*", "projects:read"]);
	});
});

describe("expandWildcardPermissions", () => {
	it("checks every known permission a wildcard covers", () => {
		expect(expandWildcardPermissions({ "users:*": true }, known)).toMatchObject(
			{
				"users:read": true,
				"users:write": true,
				"projects:read": false,
			},
		);
	});
	it("ignores false entries and handles undefined", () => {
		expect(
			expandWildcardPermissions({ "users:read": false }, known)["users:read"],
		).toBe(false);
		expect(expandWildcardPermissions(undefined, known)).toEqual({});
	});
});

describe("normalizePermissionsToWildcards", () => {
	it("collapses a whole domain to its wildcard, keyed by the action's own domain", () => {
		expect(
			normalizePermissionsToWildcards(
				{ "users:read": true, "users:write": true, "projects:read": true },
				known,
			),
		).toEqual({ "users:*": true, "projects:read": true });
	});
	it("does not over-grant sibling plugins that share a UI group", () => {
		// Both plugin permissions are in the "plugins" UI group but have their own
		// real domain, so only the plugin whose actions are all checked collapses.
		expect(
			normalizePermissionsToWildcards(
				{ "time_logging:manage_all": true },
				known,
			),
		).toEqual({ "time_logging:manage_all": true });
		expect(
			normalizePermissionsToWildcards(
				{ "time_logging:manage_all": true, "time_logging:log": true },
				known,
			),
		).toEqual({ "time_logging:*": true });
	});
	it("* wins", () => {
		expect(
			normalizePermissionsToWildcards({ "*": true, "users:read": true }, known),
		).toEqual({
			"*": true,
		});
	});
});
