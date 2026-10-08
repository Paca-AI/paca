import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockUseQuery } = vi.hoisted(() => ({ mockUseQuery: vi.fn() }));

vi.mock("@tanstack/react-query", async () => {
	const actual = await vi.importActual<typeof import("@tanstack/react-query")>(
		"@tanstack/react-query",
	);
	return { ...actual, useQuery: mockUseQuery };
});

import { useEffectiveActions } from "./use-effective-actions";

function withActions(actions: string[] | undefined, isLoading = false) {
	mockUseQuery.mockReturnValue({ data: actions, isLoading });
}

describe("useEffectiveActions", () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	it("treats * as covering every action", () => {
		withActions(["*"]);
		const { result } = renderHook(() => useEffectiveActions());
		expect(result.current.has("tasks:write")).toBe(true);
		expect(result.current.has("project.settings.task_types:write")).toBe(true);
	});

	it("treats domain:* as covering that domain only", () => {
		withActions(["tasks:*"]);
		const { result } = renderHook(() => useEffectiveActions("p1"));
		expect(result.current.has("tasks:write")).toBe(true);
		expect(result.current.has("docs:read")).toBe(false);
		// "project:*" style wildcards do not reach nested domains.
		withActions(["project:*"]);
		const nested = renderHook(() => useEffectiveActions("p1"));
		expect(nested.result.current.has("project.settings.task_types:write")).toBe(
			false,
		);
	});

	it("matches exact actions and answers hasAny", () => {
		withActions(["tasks:read"]);
		const { result } = renderHook(() => useEffectiveActions("p1"));
		expect(result.current.has("tasks:read")).toBe(true);
		expect(result.current.has("tasks:write")).toBe(false);
		expect(result.current.hasAny(["docs:read", "tasks:read"])).toBe(true);
		expect(result.current.hasAny(["docs:read"])).toBe(false);
	});

	it("denies while loading and exposes isLoading", () => {
		withActions(undefined, true);
		const { result } = renderHook(() => useEffectiveActions("p1"));
		expect(result.current.isLoading).toBe(true);
		expect(result.current.has("tasks:read")).toBe(false);
		expect(result.current.actions).toEqual([]);
	});

	it("queries the project endpoint with a project id and the global one without", () => {
		withActions([]);
		renderHook(() => useEffectiveActions("p1"));
		expect(mockUseQuery.mock.calls[0][0]).toMatchObject({
			queryKey: ["projects", "p1", "members", "me", "permissions"],
			enabled: true,
		});
		renderHook(() => useEffectiveActions());
		expect(mockUseQuery.mock.calls[1][0].queryKey).toEqual([
			"auth",
			"me",
			"permissions",
		]);
	});

	it("does not query a project until its id is known", () => {
		withActions([]);
		renderHook(() => useEffectiveActions(""));
		expect(mockUseQuery.mock.calls[0][0]).toMatchObject({ enabled: false });
	});
});
