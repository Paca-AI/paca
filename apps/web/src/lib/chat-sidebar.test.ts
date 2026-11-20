import { beforeEach, describe, expect, it } from "vitest";
import type { InboxItem } from "./chat-api";
import {
	DEFAULT_SIDEBAR_STATE,
	FLAT_LIMIT,
	groupByProject,
	limitRows,
	loadSidebarState,
	SIDEBAR_STORAGE_KEY,
	sortRows,
	splitRows,
	totals,
} from "./chat-sidebar";

const row = (o: Partial<InboxItem>): InboxItem => ({
	project_id: "p1",
	project_name: "Apollo",
	peer: null,
	channel_id: o.channel_name ?? "c",
	channel_name: "general",
	kind: "public",
	last_seq: 0,
	last_read_seq: 0,
	unread_count: 0,
	last_message_at: null,
	last_author: "",
	preview: "",
	mention_count: 0,
	starred: false,
	...o,
});

describe("sortRows", () => {
	it("puts mentions first, then unread, then the rest by name", () => {
		const out = sortRows([
			row({ channel_name: "zeta" }),
			row({ channel_name: "alpha" }),
			row({ channel_name: "mid", unread_count: 2 }),
			row({ channel_name: "ping", mention_count: 1, unread_count: 1 }),
		]);
		expect(out.map((r) => r.channel_name)).toEqual([
			"ping",
			"mid",
			"alpha",
			"zeta",
		]);
	});

	it("names a direct message by its person", () => {
		const out = sortRows([
			row({
				channel_name: "",
				peer: { account_id: "b", name: "Bob", kind: "user" },
			}),
			row({
				channel_name: "",
				peer: { account_id: "a", name: "Ann", kind: "agent" },
			}),
		]);
		expect(out.map((r) => r.peer?.name)).toEqual(["Ann", "Bob"]);
	});
});

describe("splitRows", () => {
	it("moves starred rows, channels and direct messages alike, out of the other sections", () => {
		const dm = { account_id: "u", name: "Mina", kind: "user" as const };
		const out = splitRows([
			row({ channel_name: "a", starred: true }),
			row({ channel_name: "b" }),
			row({ channel_name: "", peer: dm, starred: true }),
			row({ channel_name: "", peer: dm }),
		]);
		expect(out.starred).toHaveLength(2);
		expect(out.channels.map((r) => r.channel_name)).toEqual(["b"]);
		expect(out.dms).toHaveLength(1);
	});
});

describe("limitRows", () => {
	const many = Array.from({ length: 12 }, (_, i) =>
		row({ channel_name: `c${String(i).padStart(2, "0")}` }),
	);

	it("shows unread rows always and read ones up to the limit", () => {
		const rows = [
			row({ channel_name: "u1", unread_count: 1 }),
			...many,
			row({ channel_name: "u2", mention_count: 1 }),
		];
		const { shown, hidden } = limitRows(rows, false);
		expect(
			shown.filter((r) => r.unread_count > 0 || r.mention_count > 0),
		).toHaveLength(2);
		expect(shown).toHaveLength(2 + FLAT_LIMIT);
		expect(hidden).toBe(12 - FLAT_LIMIT);
	});

	it("shows everything on request", () => {
		expect(limitRows(many, true)).toEqual({ shown: many, hidden: 0 });
	});
});

describe("groupByProject", () => {
	it("puts the current project first", () => {
		const groups = groupByProject(
			[
				row({ project_id: "a", project_name: "Apollo", unread_count: 5 }),
				row({ project_id: "b", project_name: "Borealis" }),
			],
			"b",
		);
		expect(groups.map((g) => g.name)).toEqual(["Borealis", "Apollo"]);
	});

	it("groups channels, unread groups first, then by name, and skips direct messages", () => {
		const groups = groupByProject([
			row({ project_id: "b", project_name: "Borealis", channel_name: "x" }),
			row({ project_id: "a", project_name: "Apollo", channel_name: "y" }),
			row({
				project_id: "c",
				project_name: "Cirrus",
				channel_name: "z",
				unread_count: 3,
			}),
			row({
				project_id: "c",
				project_name: "Cirrus",
				channel_name: "w",
				mention_count: 1,
			}),
			row({ project_id: null, project_name: "", channel_name: "dm" }),
		]);
		expect(groups.map((g) => g.name)).toEqual(["Cirrus", "Apollo", "Borealis"]);
		expect(groups[0]).toMatchObject({ unread: 3, mentions: 1 });
		expect(groups[0]?.rows).toHaveLength(2);
	});
});

describe("totals", () => {
	it("adds unread and mentions", () => {
		expect(
			totals([
				row({ unread_count: 2, mention_count: 1 }),
				row({ unread_count: 3 }),
			]),
		).toEqual({ unread: 5, mentions: 1 });
	});
});

describe("loadSidebarState", () => {
	beforeEach(() => localStorage.clear());

	it("defaults when nothing is stored or the stored value is broken", () => {
		expect(loadSidebarState()).toEqual(DEFAULT_SIDEBAR_STATE);
		localStorage.setItem(SIDEBAR_STORAGE_KEY, "{nope");
		expect(loadSidebarState()).toEqual(DEFAULT_SIDEBAR_STATE);
	});

	it("restores what was saved and fills in what is missing", () => {
		localStorage.setItem(
			SIDEBAR_STORAGE_KEY,
			JSON.stringify({
				open: { starred: false },
				closedProjects: { p1: true },
			}),
		);
		const s = loadSidebarState();
		expect(s.open).toEqual({
			chat: true,
			starred: false,
			dms: true,
		});
		expect(s.closedProjects).toEqual({ p1: true });
	});

	it("ignores settings of the removed flat view", () => {
		localStorage.setItem(
			SIDEBAR_STORAGE_KEY,
			JSON.stringify({
				mode: "flat",
				unreadOnly: true,
				open: { channels: false },
			}),
		);
		expect(loadSidebarState().open).toEqual(DEFAULT_SIDEBAR_STATE.open);
	});
});
