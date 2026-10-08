import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockPermissions, mockProjectPermissions } = vi.hoisted(() => ({
	mockPermissions: vi.fn(),
	mockProjectPermissions: vi.fn(),
}));

vi.mock("@/hooks/use-permissions", () => ({
	usePermissions: mockPermissions,
}));
vi.mock("@/hooks/use-project-permissions", () => ({
	useProjectPermissions: mockProjectPermissions,
}));

import { useCanAssignProjectRole } from "./use-can-assign-project-role";

function setup(opts: {
	global?: string[];
	project?: string[];
	loading?: boolean;
}) {
	mockPermissions.mockReturnValue({
		hasPermission: (a: string) => (opts.global ?? []).includes(a),
		isLoading: opts.loading ?? false,
	});
	mockProjectPermissions.mockReturnValue({
		hasProjectPermission: (a: string) => (opts.project ?? []).includes(a),
		isLoading: false,
	});
	return renderHook(() => useCanAssignProjectRole("proj-1")).result.current;
}

describe("useCanAssignProjectRole", () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	it("is false without roles:assign, even with members:write", () => {
		expect(setup({ project: ["project.members:write"] }).canAssignRoles).toBe(
			false,
		);
	});

	it("is true with roles:assign in the project", () => {
		expect(setup({ project: ["roles:assign"] }).canAssignRoles).toBe(true);
	});

	it("is true with workspace-wide roles:assign", () => {
		expect(setup({ global: ["roles:assign"] }).canAssignRoles).toBe(true);
	});

	it("asks the project it was given", () => {
		setup({});
		expect(mockProjectPermissions).toHaveBeenCalledWith("proj-1");
	});

	it("reports loading while either source is loading", () => {
		expect(setup({ loading: true }).isLoading).toBe(true);
		expect(setup({}).isLoading).toBe(false);
	});
});
