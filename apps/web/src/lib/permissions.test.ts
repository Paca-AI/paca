import { describe, expect, it } from "vitest";

import {
	expandWildcardPermissions,
	hasAnyPermission,
	hasPermission,
	normalizePermissionsToWildcards,
	type PermissionDefinition,
} from "./permissions";

const knownPermissions: PermissionDefinition[] = [
	{ key: "users.read", domain: "users" },
	{ key: "users.manage", domain: "users" },
	{ key: "projects.read", domain: "projects" },
	{ key: "projects.write", domain: "projects" },
];

describe("permissions", () => {
	it("hasPermission supports exact, domain wildcard, and global wildcard", () => {
		expect(hasPermission(["users.read"], "users.read")).toBe(true);
		expect(hasPermission(["users.*"], "users.manage")).toBe(true);
		expect(hasPermission(["*"], "projects.write")).toBe(true);
		expect(hasPermission(["users.read"], "projects.read")).toBe(false);
		expect(hasPermission(["users.read"], "invalid-format")).toBe(false);
	});

	it("hasAnyPermission returns true if any required permission is granted", () => {
		expect(
			hasAnyPermission(["projects.*"], ["users.read", "projects.write"]),
		).toBe(true);
		expect(hasAnyPermission(["users.read"], ["projects.read"])).toBe(false);
	});

	it("expandWildcardPermissions expands explicit, domain wildcard, and global wildcard grants", () => {
		expect(expandWildcardPermissions(undefined, knownPermissions)).toEqual({});

		expect(
			expandWildcardPermissions(
				{ "users.manage": true, "projects.*": true },
				knownPermissions,
			),
		).toEqual({
			"users.read": false,
			"users.manage": true,
			"projects.read": true,
			"projects.write": true,
		});

		expect(expandWildcardPermissions({ "*": true }, knownPermissions)).toEqual({
			"users.read": true,
			"users.manage": true,
			"projects.read": true,
			"projects.write": true,
		});
	});

	it("normalizePermissionsToWildcards compacts fully selected domains and preserves partial domains", () => {
		expect(
			normalizePermissionsToWildcards(
				{
					"users.read": true,
					"users.manage": true,
					"projects.read": true,
				},
				knownPermissions,
			),
		).toEqual({
			"users.*": true,
			"projects.read": true,
		});
	});

	it("normalizePermissionsToWildcards keeps global wildcard as-is", () => {
		expect(
			normalizePermissionsToWildcards({ "*": true }, knownPermissions),
		).toEqual({ "*": true });
	});
});
