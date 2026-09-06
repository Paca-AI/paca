import { describe, expect, it } from "vitest";

import {
	dedupeGrantedPermissions,
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

	it("hasPermission supports multi-segment domain wildcard", () => {
		expect(hasPermission(["project.members.*"], "project.members.write")).toBe(
			true,
		);
		expect(hasPermission(["project.members.*"], "project.members.read")).toBe(
			true,
		);
		expect(hasPermission(["project.*"], "project.members.write")).toBe(false);
		expect(hasPermission(["project.roles.*"], "project.members.write")).toBe(
			false,
		);
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

	it("expandWildcardPermissions matches plugin-declared permissions by their own key prefix, not the synthetic UI domain", () => {
		// Plugin-declared permissions all share the UI domain "plugins" (see
		// toPluginKnownPermissions), but the wildcard a role actually stores is
		// keyed by the permission's own namespace (e.g. "time_logging.*") —
		// domain-based matching would look for a non-existent "plugins.*".
		const pluginPermissions: PermissionDefinition[] = [
			{ key: "time_logging.view_all", domain: "plugins" },
			{ key: "time_logging.manage_all", domain: "plugins" },
			{ key: "checklist.manage", domain: "plugins" },
		];

		expect(
			expandWildcardPermissions({ "time_logging.*": true }, pluginPermissions),
		).toEqual({
			"time_logging.view_all": true,
			"time_logging.manage_all": true,
			"checklist.manage": false,
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

	it("dedupeGrantedPermissions drops a key already implied by a granted domain wildcard", () => {
		// Reproduces a role whose stored permissions carry both
		// "environments.*" and the individual keys it already covers (data
		// debt from a wildcard merged onto an existing grant — see
		// 000044_add_environment_permissions.sql) — the display should show
		// only the wildcard.
		expect(
			dedupeGrantedPermissions([
				"environments.*",
				"environments.read",
				"environments.write",
				"environments.connect",
				"tasks.read",
			]),
		).toEqual(["environments.*", "tasks.read"]);
	});

	it("dedupeGrantedPermissions supports multi-segment domains and leaves ungoverned keys alone", () => {
		expect(
			dedupeGrantedPermissions(["project.members.*", "project.members.read"]),
		).toEqual(["project.members.*"]);
		expect(dedupeGrantedPermissions(["tasks.read", "docs.write"])).toEqual([
			"tasks.read",
			"docs.write",
		]);
	});

	it("dedupeGrantedPermissions collapses everything to the global wildcard alone", () => {
		expect(dedupeGrantedPermissions(["*", "users.read", "projects.*"])).toEqual(
			["*"],
		);
	});
});
