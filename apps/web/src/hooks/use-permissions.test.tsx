import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockUseQuery } = vi.hoisted(() => ({
	mockUseQuery: vi.fn(),
}));

vi.mock("@tanstack/react-query", async () => {
	const actual = await vi.importActual<typeof import("@tanstack/react-query")>(
		"@tanstack/react-query",
	);

	return {
		...actual,
		useQuery: mockUseQuery,
	};
});

import { usePermissions } from "./use-permissions";

describe("usePermissions", () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	it("returns query data and loading state", () => {
		mockUseQuery.mockReturnValue({
			data: ["users:read"],
			isLoading: true,
		});

		const { result } = renderHook(() => usePermissions());

		expect(result.current.permissions).toEqual(["users:read"]);
		expect(result.current.isLoading).toBe(true);
	});

	it("defaults permissions to empty array when data is undefined", () => {
		mockUseQuery.mockReturnValue({
			data: undefined,
			isLoading: false,
		});

		const { result } = renderHook(() => usePermissions());

		expect(result.current.permissions).toEqual([]);
		expect(result.current.isLoading).toBe(false);
	});

	it("answers hasPermission from the granted actions, wildcards included", () => {
		mockUseQuery.mockReturnValue({
			data: ["projects:*"],
			isLoading: false,
		});

		const { result } = renderHook(() => usePermissions());

		expect(result.current.hasPermission("projects:create")).toBe(true);
		expect(result.current.hasPermission("users:read")).toBe(false);
	});

	it("answers hasAnyPermission from the granted actions", () => {
		mockUseQuery.mockReturnValue({
			data: ["projects:create"],
			isLoading: false,
		});

		const { result } = renderHook(() => usePermissions());

		expect(
			result.current.hasAnyPermission(["projects:create", "projects:delete"]),
		).toBe(true);
		expect(result.current.hasAnyPermission(["projects:delete"])).toBe(false);
	});
});
