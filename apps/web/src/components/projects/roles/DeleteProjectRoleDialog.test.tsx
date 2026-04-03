import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockDeleteProjectRole } = vi.hoisted(() => ({
	mockDeleteProjectRole: vi.fn(),
}));

vi.mock("@/lib/project-api", () => ({
	deleteProjectRole: mockDeleteProjectRole,
	projectRolesQueryOptions: (projectId: string) => ({
		queryKey: ["projects", projectId, "roles"],
	}),
}));

import type { ProjectRole } from "@/lib/project-api";
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

const testRole: ProjectRole = {
	id: "r1",
	project_id: "p1",
	role_name: "DEVELOPER",
	permissions: {},
	created_at: "2026-01-01T00:00:00.000Z",
	updated_at: "2026-01-01T00:00:00.000Z",
};

function renderDialog(
	overrides: {
		open?: boolean;
		role?: ProjectRole;
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

	it("calls deleteProjectRole with the correct project and role ids", async () => {
		mockDeleteProjectRole.mockResolvedValue(undefined);
		renderDialog();

		await userEvent.click(screen.getByRole("button", { name: /delete role/i }));

		expect(mockDeleteProjectRole).toHaveBeenCalledWith("p1", "r1");
	});

	it("calls onOpenChange(false) after successful deletion", async () => {
		mockDeleteProjectRole.mockResolvedValue(undefined);
		const { onOpenChange } = renderDialog();

		await userEvent.click(screen.getByRole("button", { name: /delete role/i }));

		await waitFor(() => {
			expect(onOpenChange).toHaveBeenCalledWith(false);
		});
	});

	it("shows an error when role is still assigned to members", async () => {
		mockDeleteProjectRole.mockRejectedValue({
			response: { data: { error_code: "PROJECT_ROLE_HAS_MEMBERS" } },
		});
		renderDialog();

		await userEvent.click(screen.getByRole("button", { name: /delete role/i }));

		await waitFor(() => {
			expect(
				screen.getByText(/still assigned to one or more members/i),
			).toBeInTheDocument();
		});
	});

	it("shows an error when the role no longer exists", async () => {
		mockDeleteProjectRole.mockRejectedValue({
			response: { data: { error_code: "PROJECT_ROLE_NOT_FOUND" } },
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
		mockDeleteProjectRole.mockRejectedValue({
			response: { data: { error_code: "FORBIDDEN" } },
		});
		renderDialog();

		await userEvent.click(screen.getByRole("button", { name: /delete role/i }));

		await waitFor(() => {
			expect(
				screen.getByText(/don't have permission to delete/i),
			).toBeInTheDocument();
		});
	});

	it("shows a generic error message for unexpected errors", async () => {
		mockDeleteProjectRole.mockRejectedValue(new Error("Network failure"));
		renderDialog();

		await userEvent.click(screen.getByRole("button", { name: /delete role/i }));

		await waitFor(() => {
			expect(screen.getByText("Network failure")).toBeInTheDocument();
		});
	});

	it("does not call onOpenChange(false) when the mutation fails", async () => {
		mockDeleteProjectRole.mockRejectedValue(new Error("Oops"));
		const { onOpenChange } = renderDialog();

		await userEvent.click(screen.getByRole("button", { name: /delete role/i }));

		await waitFor(() => {
			expect(screen.getByText("Oops")).toBeInTheDocument();
		});
		expect(onOpenChange).not.toHaveBeenCalledWith(false);
	});
});
