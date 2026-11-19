import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { MemberDirectory } from "./member-directory";
import * as projectApi from "./project-api";

const member = (over: Partial<projectApi.ProjectMember>) =>
	({
		id: "m1",
		project_id: "p",
		user_id: "u1",
		roles: [],
		username: "alice",
		full_name: "Alice",
		...over,
	}) as projectApi.ProjectMember;

const page = (items: projectApi.ProjectMember[]) => ({
	items,
	page_size: 100,
	next_cursor: null,
	total_count: items.length,
});

describe("MemberDirectory", () => {
	beforeEach(() => vi.useFakeTimers());
	afterEach(() => {
		vi.useRealTimers();
		vi.restoreAllMocks();
	});

	it("batches ids asked for together into one request and resolves both id kinds", async () => {
		const spy = vi
			.spyOn(projectApi, "listProjectMembers")
			.mockResolvedValue(page([member({})]));
		const dir = new MemberDirectory("p");

		expect(dir.get("m1")).toBeUndefined();
		expect(dir.get("u1")).toBeUndefined();
		expect(dir.get("m1")).toBeUndefined(); // no duplicate request while pending
		await vi.runAllTimersAsync();

		expect(spy).toHaveBeenCalledTimes(1);
		expect(spy.mock.calls[0][1]?.ids).toEqual(["m1", "u1"]);
		expect(dir.get("m1")?.username).toBe("alice");
		expect(dir.get("u1")?.username).toBe("alice");
	});

	it("caches ids that are not members and does not ask again", async () => {
		const spy = vi
			.spyOn(projectApi, "listProjectMembers")
			.mockResolvedValue(page([]));
		const dir = new MemberDirectory("p");
		dir.get("gone");
		await vi.runAllTimersAsync();
		expect(dir.get("gone")).toBeUndefined();
		await vi.runAllTimersAsync();
		expect(spy).toHaveBeenCalledTimes(1);
	});

	it("notifies subscribers when a batch lands", async () => {
		vi.spyOn(projectApi, "listProjectMembers").mockResolvedValue(
			page([member({})]),
		);
		const dir = new MemberDirectory("p");
		const listener = vi.fn();
		dir.subscribe(listener);
		dir.get("m1");
		await vi.runAllTimersAsync();
		expect(listener).toHaveBeenCalled();
	});

	it("retries failed lookups only after the retry delay", async () => {
		const spy = vi
			.spyOn(projectApi, "listProjectMembers")
			.mockRejectedValueOnce(new Error("boom"))
			.mockResolvedValue(page([member({})]));
		const dir = new MemberDirectory("p");
		dir.get("m1");
		await vi.runAllTimersAsync();
		dir.get("m1");
		await vi.runAllTimersAsync();
		expect(spy).toHaveBeenCalledTimes(1);

		vi.advanceTimersByTime(16_000);
		dir.get("m1");
		await vi.runAllTimersAsync();
		expect(spy).toHaveBeenCalledTimes(2);
		expect(dir.get("m1")?.username).toBe("alice");
	});

	it("refetches in-use ids after invalidate", async () => {
		const spy = vi
			.spyOn(projectApi, "listProjectMembers")
			.mockResolvedValue(page([member({})]));
		const dir = new MemberDirectory("p");
		dir.get("m1");
		await vi.runAllTimersAsync();
		dir.invalidate();
		dir.get("m1");
		await vi.runAllTimersAsync();
		expect(spy).toHaveBeenCalledTimes(2);
	});

	it("does not notify or swap the record when a seed only carries a re-signed avatar URL", () => {
		const signed = (sig: string) =>
			member({
				avatar_url: `http://h/avatars/u1/full.png?X-Amz-Date=${sig}&X-Amz-Signature=${sig}`,
			});
		const dir = new MemberDirectory("p");
		const listener = vi.fn();
		dir.subscribe(listener);

		dir.seed([signed("aaa")]);
		expect(listener).toHaveBeenCalledTimes(1);
		const first = dir.get("m1");

		// Every page of tasks re-signs the same avatar.
		dir.seed([signed("bbb")]);
		expect(listener).toHaveBeenCalledTimes(1);
		expect(dir.get("m1")).toBe(first);

		// A real change still goes through.
		dir.seed([{ ...signed("ccc"), full_name: "Alice B" }]);
		expect(listener).toHaveBeenCalledTimes(2);
		expect(dir.get("m1")?.full_name).toBe("Alice B");
	});

	it("takes a re-signed avatar URL once the kept one is old", () => {
		vi.setSystemTime(new Date("2026-01-01T00:00:00Z"));
		const dir = new MemberDirectory("p");
		const url = (sig: string) =>
			member({ avatar_url: `http://h/a.png?X-Amz-Signature=${sig}` });
		dir.seed([url("old")]);

		vi.setSystemTime(new Date("2026-01-01T00:30:00Z"));
		dir.seed([url("new")]);
		expect(dir.get("m1")?.avatar_url).toContain("Signature=new");
	});
});
