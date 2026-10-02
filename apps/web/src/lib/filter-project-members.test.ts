import { describe, expect, it } from "vitest";

import { filterProjectMembers } from "./filter-project-members";
import type { ProjectMember } from "./project-api";

const m = (over: Partial<ProjectMember>): ProjectMember => ({
	id: over.username ?? "x",
	project_id: "p",
	user_id: over.username ?? "x",
	project_role_id: "r",
	username: "u",
	full_name: "",
	role_name: "Member",
	description: "",
	...over,
});

const members = [
	m({ username: "alice", full_name: "Alice Smith", role_name: "Admin" }),
	m({ username: "bob", full_name: "Bob Jones" }),
	m({
		username: "bot-1",
		member_type: "agent",
		agent_name: "Reviewer",
		agent_handle: "rev",
		role_name: "Agent",
	}),
];

const names = (list: ProjectMember[]) => list.map((x) => x.username);

describe("filterProjectMembers", () => {
	it("returns everyone for an empty filter", () => {
		expect(filterProjectMembers(members, "  ", "")).toHaveLength(3);
	});

	it("requires every word, case-insensitively, in any order", () => {
		expect(names(filterProjectMembers(members, "SMITH alice", ""))).toEqual([
			"alice",
		]);
		expect(filterProjectMembers(members, "alice jones", "")).toEqual([]);
	});

	it("matches agent name and handle", () => {
		expect(names(filterProjectMembers(members, "review", ""))).toEqual([
			"bot-1",
		]);
		expect(names(filterProjectMembers(members, "rev", ""))).toEqual(["bot-1"]);
	});

	it("filters by exact role and combines with search", () => {
		expect(names(filterProjectMembers(members, "", "Admin"))).toEqual([
			"alice",
		]);
		expect(filterProjectMembers(members, "bob", "Admin")).toEqual([]);
	});
});
