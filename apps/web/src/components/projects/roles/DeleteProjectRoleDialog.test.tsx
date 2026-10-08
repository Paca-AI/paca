import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockDeleteRole } = vi.hoisted(() => ({
	mockDeleteRole: vi.fn(),
}));

vi.mock("@/lib/role-api", () => ({
	deleteRole: mockDeleteRole,
	projectRolesQueryOptions: (projectId: string) => ({
		queryKey: ["projects", projectId, "roles"],
	}),
}));

import type { Role } from "@/lib/role-api";
import { DeleteProjectRoleDialog } from "./DeleteProjectRoleDialog";

// ── Helpers ───────────────────────────────────────────────────────────────────

function makeQueryClient() {
	return new QueryClient({
		defaultOptions: {
			mutations: { retry: false },
			queries: { retry: false, gcTime: 0 },
		},
	});
}

function Wrapper({ children }: { children: ReactNode }) {
	return (
		<QueryClientProvider client={makeQueryClient()}>
			{children}
		</QueryClientProvider>
	);
}

const testRole: Role = {
	id: "r1",
	project_id: "p1",
	name: "DEVELOPER",
	description: "",
	policy: { statements: [] },
	is_system: false,
	is_default: false,
	attachment_count: 0,
	created_at: "2026-01-01T00:00:00.000Z",
	updated_at: "2026-01-01T00:00:00.000Z",
};

function renderDialog(
	overrides: {
		open?: boolean;
		role?: Role;
		onOpenChange?: (open: boolean) => void;
	} = {},
) {
	const onOpenChange = overrides.onOpenChange ?? vi.fn();
	render(
		<Wrapper>
			<DeleteProjectRoleDialog
				open={overrides.open ?? true}
				onOpenChange={onOpenChange}
				projectId="p1"
				role={overrides.role ?? testRole}
			/>
		</Wrapper>,
	);
	return { onOpenChange };
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe("DeleteProjectRoleDialog", () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	it("renders the role name in the confirmation text", () => {
		renderDialog();
		expect(screen.getByText("DEVELOPER")).toBeInTheDocument();
	});

	it("renders the dialog title and description", () => {
		renderDialog();
		expect(
			screen.getByRole("heading", { name: "Delete role" }),
		).toBeInTheDocument();
		expect(screen.getByText(/cannot be undone/i)).toBeInTheDocument();
	});

	it("does not render content when closed", () => {
		renderDialog({ open: false });
		expect(screen.queryByText("Delete role")).not.toBeInTheDocument();
	});

	it("deletes the role inside the project", async () => {
		mockDeleteRole.mockResolvedValue(undefined);
		renderDialog();

		await userEvent.click(screen.getByRole("button", { name: /delete role/i }));

		expect(mockDeleteRole).toHaveBeenCalledWith("r1", "p1");
	});

	it("calls onOpenChange(false) after successful deletion", async () => {
		mockDeleteRole.mockResolvedValue(undefined);
		const { onOpenChange } = renderDialog();

		await userEvent.click(screen.getByRole("button", { name: /delete role/i }));

		await waitFor(() => {
			expect(onOpenChange).toHaveBeenCalledWith(false);
		});
	});

	it("shows an error when the role no longer exists", async () => {
		mockDeleteRole.mockRejectedValue({
			response: { data: { error_code: "ROLE_NOT_FOUND" } },
		});
		renderDialog();

		await userEvent.click(screen.getByRole("button", { name: /delete role/i }));

		await waitFor(() => {
			expect(
				screen.getByText("This role no longer exists."),
			).toBeInTheDocument();
		});
	});

	it("shows a forbidden error when user lacks permission", async () => {
		mockDeleteRole.mockRejectedValue({
			response: { data: { error_code: "FORBIDDEN" } },
		});
		renderDialog();

		await userEvent.click(screen.getByRole("button", { name: /delete role/i }));

		await waitFor(() => {
			expect(screen.getByText(/don't have permission/i)).toBeInTheDocument();
		});
	});

	it("shows a generic error message for unexpected errors", async () => {
		mockDeleteRole.mockRejectedValue(new Error("Network failure"));
		renderDialog();

		await userEvent.click(screen.getByRole("button", { name: /delete role/i }));

		await waitFor(() => {
			expect(
				screen.getByText("Something went wrong. Try again."),
			).toBeInTheDocument();
		});
	});

	it("does not call onOpenChange(false) when the mutation fails", async () => {
		mockDeleteRole.mockRejectedValue(new Error("Oops"));
		const { onOpenChange } = renderDialog();

		await waitFor(() => undefined);
		await userEvent.click(screen.getByRole("button", { name: /delete role/i }));

		await waitFor(() => {
			expect(
				screen.getByText("Something went wrong. Try again."),
			).toBeInTheDocument();
		});
		expect(onOpenChange).not.toHaveBeenCalledWith(false);
	});
});
