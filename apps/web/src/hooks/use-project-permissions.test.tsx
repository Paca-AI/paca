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

import { useProjectPermissions } from "./use-project-permissions";

describe("useProjectPermissions", () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	it("exposes isLoading so callers can distinguish loading from denied", () => {
		// While the permissions request is in flight, data isn't back yet —
		// same shape a confirmed "no permissions" response would have. A
		// caller gating a noPermission render on `hasProjectPermission`
		// alone (without also checking isLoading) would flash
		// NoPermissionState before the real result comes back.
		mockUseQuery.mockReturnValue({
			data: undefined,
			isLoading: true,
		});

		const { result } = renderHook(() => useProjectPermissions("proj-1"));

		expect(result.current.isLoading).toBe(true);
	});

	it("reports isLoading false once the permissions map has loaded", () => {
		mockUseQuery.mockReturnValue({
			data: ["tasks:write"],
			isLoading: false,
		});

		const { result } = renderHook(() => useProjectPermissions("proj-1"));

		expect(result.current.isLoading).toBe(false);
	});

	it("answers hasProjectPermission from the granted actions, wildcards included", () => {
		mockUseQuery.mockReturnValue({
			data: ["tasks:*", "docs:read"],
			isLoading: false,
		});

		const { result } = renderHook(() => useProjectPermissions("proj-1"));

		expect(result.current.hasProjectPermission("tasks:write")).toBe(true);
		expect(result.current.hasProjectPermission("docs:read")).toBe(true);
		expect(result.current.hasProjectPermission("docs:write")).toBe(false);
	});
});
