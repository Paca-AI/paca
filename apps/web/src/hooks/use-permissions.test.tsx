import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockUseQuery, mockCheckPermission, mockCheckAnyPermission } = vi.hoisted(() => ({
	mockUseQuery: vi.fn(),
	mockCheckPermission: vi.fn(),
	mockCheckAnyPermission: vi.fn(),
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

vi.mock("@/lib/permissions", () => ({
	hasPermission: mockCheckPermission,
	hasAnyPermission: mockCheckAnyPermission,
}));

import { usePermissions } from "./use-permissions";

describe("usePermissions", () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	it("returns query data and loading state", () => {
		mockUseQuery.mockReturnValue({
			data: ["users.read"],
			isLoading: true,
		});

		const { result } = renderHook(() => usePermissions());

		expect(result.current.permissions).toEqual(["users.read"]);
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

	it("delegates hasPermission checks to permissions helper", () => {
		mockUseQuery.mockReturnValue({
			data: ["projects.*"],
			isLoading: false,
		});
		mockCheckPermission.mockReturnValue(true);

		const { result } = renderHook(() => usePermissions());
		const canManage = result.current.hasPermission("projects.manage");

		expect(canManage).toBe(true);
		expect(mockCheckPermission).toHaveBeenCalledWith(
			["projects.*"],
			"projects.manage",
		);
	});

	it("delegates hasAnyPermission checks to permissions helper", () => {
		mockUseQuery.mockReturnValue({
			data: ["projects.create"],
			isLoading: false,
		});
		mockCheckAnyPermission.mockReturnValue(true);

		const { result } = renderHook(() => usePermissions());
		const canAny = result.current.hasAnyPermission([
			"projects.create",
			"projects.delete",
		]);

		expect(canAny).toBe(true);
		expect(mockCheckAnyPermission).toHaveBeenCalledWith(
			["projects.create"],
			["projects.create", "projects.delete"],
		);
	});
});
