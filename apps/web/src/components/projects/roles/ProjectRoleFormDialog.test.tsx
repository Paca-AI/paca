import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockCreateProjectRole, mockUpdateProjectRole } = vi.hoisted(() => ({
	mockCreateProjectRole: vi.fn(),
	mockUpdateProjectRole: vi.fn(),
}));

vi.mock("@/lib/project-api", () => ({
	createProjectRole: mockCreateProjectRole,
	updateProjectRole: mockUpdateProjectRole,
	projectRolesQueryOptions: (projectId: string) => ({
		queryKey: ["projects", projectId, "roles"],
	}),
}));

import type { ProjectRole } from "@/lib/project-api";
import { ProjectRoleFormDialog } from "./ProjectRoleFormDialog";

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

const existingRole: ProjectRole = {
	id: "r1",
	project_id: "p1",
	role_name: "DEVELOPER",
	permissions: { "tasks.*": true },
	created_at: "2026-01-01T00:00:00.000Z",
	updated_at: "2026-01-01T00:00:00.000Z",
};

function renderCreate(onOpenChange = vi.fn()) {
	render(
		<Wrapper>
			<ProjectRoleFormDialog projectId="p1" open onOpenChange={onOpenChange} />
		</Wrapper>,
	);
	return { onOpenChange };
}

function renderEdit(role: ProjectRole = existingRole, onOpenChange = vi.fn()) {
	render(
		<Wrapper>
			<ProjectRoleFormDialog
				projectId="p1"
				open
				role={role}
				onOpenChange={onOpenChange}
			/>
		</Wrapper>,
	);
	return { onOpenChange };
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe("ProjectRoleFormDialog", () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	// ── Create mode ────────────────────────────────────────────────────────────

	describe("create mode", () => {
		it("shows 'New Role' as the title", () => {
			renderCreate();
			expect(screen.getByText("New Role")).toBeInTheDocument();
		});

		it("renders an empty role name input", () => {
			renderCreate();
			const input = screen.getByLabelText(/role name/i);
			expect(input).toHaveValue("");
		});

		it("disables the submit button when the name is empty", () => {
			renderCreate();
			expect(
				screen.getByRole("button", { name: /create role/i }),
			).toBeDisabled();
		});

		it("enables the submit button once a name is typed", async () => {
			renderCreate();
			await userEvent.type(screen.getByLabelText(/role name/i), "REVIEWER");
			expect(
				screen.getByRole("button", { name: /create role/i }),
			).toBeEnabled();
		});

		it("calls createProjectRole with the role name on submit", async () => {
			mockCreateProjectRole.mockResolvedValue(existingRole);
			renderCreate();

			await userEvent.type(screen.getByLabelText(/role name/i), "REVIEWER");
			await userEvent.click(
				screen.getByRole("button", { name: /create role/i }),
			);

			await waitFor(() => {
				expect(mockCreateProjectRole).toHaveBeenCalledWith(
					"p1",
					expect.objectContaining({ role_name: "REVIEWER" }),
				);
			});
		});

		it("calls onOpenChange(false) after successful creation", async () => {
			mockCreateProjectRole.mockResolvedValue(existingRole);
			const { onOpenChange } = renderCreate();

			await userEvent.type(screen.getByLabelText(/role name/i), "REVIEWER");
			await userEvent.click(
				screen.getByRole("button", { name: /create role/i }),
			);

			await waitFor(() => {
				expect(onOpenChange).toHaveBeenCalledWith(false);
			});
		});

		it("does not call updateProjectRole in create mode", async () => {
			mockCreateProjectRole.mockResolvedValue(existingRole);
			renderCreate();

			await userEvent.type(screen.getByLabelText(/role name/i), "REVIEWER");
			await userEvent.click(
				screen.getByRole("button", { name: /create role/i }),
			);

			await waitFor(() => {
				expect(mockCreateProjectRole).toHaveBeenCalled();
			});
			expect(mockUpdateProjectRole).not.toHaveBeenCalled();
		});
	});

	// ── Edit mode ──────────────────────────────────────────────────────────────

	describe("edit mode", () => {
		it("shows 'Edit Role' as the title", () => {
			renderEdit();
			expect(screen.getByText("Edit Role")).toBeInTheDocument();
		});

		it("pre-fills the role name input with the existing role name", () => {
			renderEdit();
			expect(screen.getByLabelText(/role name/i)).toHaveValue("DEVELOPER");
		});

		it("submit button says 'Save changes'", () => {
			renderEdit();
			expect(
				screen.getByRole("button", { name: /save changes/i }),
			).toBeInTheDocument();
		});

		it("calls updateProjectRole with the project id, role id, and updated name", async () => {
			mockUpdateProjectRole.mockResolvedValue({
				...existingRole,
				role_name: "LEAD",
			});
			renderEdit();

			const input = screen.getByLabelText(/role name/i);
			await userEvent.clear(input);
			await userEvent.type(input, "LEAD");
			await userEvent.click(
				screen.getByRole("button", { name: /save changes/i }),
			);

			await waitFor(() => {
				expect(mockUpdateProjectRole).toHaveBeenCalledWith(
					"p1",
					"r1",
					expect.objectContaining({ role_name: "LEAD" }),
				);
			});
		});

		it("calls onOpenChange(false) after successful update", async () => {
			mockUpdateProjectRole.mockResolvedValue(existingRole);
			const { onOpenChange } = renderEdit();

			await userEvent.click(
				screen.getByRole("button", { name: /save changes/i }),
			);

			await waitFor(() => {
				expect(onOpenChange).toHaveBeenCalledWith(false);
			});
		});

		it("does not call createProjectRole in edit mode", async () => {
			mockUpdateProjectRole.mockResolvedValue(existingRole);
			renderEdit();

			await userEvent.click(
				screen.getByRole("button", { name: /save changes/i }),
			);

			await waitFor(() => {
				expect(mockUpdateProjectRole).toHaveBeenCalled();
			});
			expect(mockCreateProjectRole).not.toHaveBeenCalled();
		});
	});

	// ── Error handling ─────────────────────────────────────────────────────────

	describe("error handling", () => {
		it("shows an inline field error when role name is already taken", async () => {
			mockCreateProjectRole.mockRejectedValue({
				response: { data: { error_code: "PROJECT_ROLE_NAME_TAKEN" } },
			});
			renderCreate();

			await userEvent.type(screen.getByLabelText(/role name/i), "DEVELOPER");
			await userEvent.click(
				screen.getByRole("button", { name: /create role/i }),
			);

			await waitFor(() => {
				expect(
					screen.getByText(/role with this name already exists/i),
				).toBeInTheDocument();
			});
		});

		it("shows an inline field error when role name is invalid", async () => {
			mockCreateProjectRole.mockRejectedValue({
				response: { data: { error_code: "PROJECT_ROLE_NAME_INVALID" } },
			});
			renderCreate();

			await userEvent.type(screen.getByLabelText(/role name/i), "bad-name!!");
			await userEvent.click(
				screen.getByRole("button", { name: /create role/i }),
			);

			await waitFor(() => {
				expect(
					screen.getByText(/uppercase letters, numbers, and underscores/i),
				).toBeInTheDocument();
			});
		});

		it("shows a general error for forbidden action", async () => {
			mockCreateProjectRole.mockRejectedValue({
				response: { data: { error_code: "FORBIDDEN" } },
			});
			renderCreate();

			await userEvent.type(screen.getByLabelText(/role name/i), "NEW_ROLE");
			await userEvent.click(
				screen.getByRole("button", { name: /create role/i }),
			);

			await waitFor(() => {
				expect(screen.getByText(/don't have permission/i)).toBeInTheDocument();
			});
		});

		it("shows a general error for internal server errors", async () => {
			mockCreateProjectRole.mockRejectedValue({
				response: { data: { error_code: "INTERNAL_ERROR" } },
			});
			renderCreate();

			await userEvent.type(screen.getByLabelText(/role name/i), "NEW_ROLE");
			await userEvent.click(
				screen.getByRole("button", { name: /create role/i }),
			);

			await waitFor(() => {
				expect(
					screen.getByText(/something went wrong on the server/i),
				).toBeInTheDocument();
			});
		});

		it("clears the name error when the user starts typing again after a validation error", async () => {
			mockCreateProjectRole.mockRejectedValue({
				response: { data: { error_code: "PROJECT_ROLE_NAME_TAKEN" } },
			});
			renderCreate();

			await userEvent.type(screen.getByLabelText(/role name/i), "DEVELOPER");
			await userEvent.click(
				screen.getByRole("button", { name: /create role/i }),
			);

			await waitFor(() => {
				expect(
					screen.getByText(/role with this name already exists/i),
				).toBeInTheDocument();
			});

			await userEvent.type(screen.getByLabelText(/role name/i), "X");
			expect(
				screen.queryByText(/role with this name already exists/i),
			).not.toBeInTheDocument();
		});

		it("does not call onOpenChange(false) when creation fails", async () => {
			mockCreateProjectRole.mockRejectedValue(new Error("Server down"));
			const { onOpenChange } = renderCreate();

			await userEvent.type(screen.getByLabelText(/role name/i), "NEW_ROLE");
			await userEvent.click(
				screen.getByRole("button", { name: /create role/i }),
			);

			await waitFor(() => {
				expect(screen.getByText("Server down")).toBeInTheDocument();
			});
			expect(onOpenChange).not.toHaveBeenCalledWith(false);
		});
	});

	// ── Permissions ────────────────────────────────────────────────────────────

	describe("permissions", () => {
		it("renders permission toggles for all known permission groups", () => {
			renderCreate();
			// Each group label should be visible
			expect(screen.getByText("Project")).toBeInTheDocument();
			expect(screen.getByText("Members")).toBeInTheDocument();
			expect(screen.getByText("Roles")).toBeInTheDocument();
			expect(screen.getByText("Tasks")).toBeInTheDocument();
			expect(screen.getByText("Sprints")).toBeInTheDocument();
		});

		it("pre-selects permissions from the existing role and sends them in the update payload", async () => {
			// existingRole has "tasks.*": true; both task permissions should be normalised
			// back to the wildcard and forwarded to the API on save.
			mockUpdateProjectRole.mockResolvedValue(existingRole);
			renderEdit();

			await userEvent.click(
				screen.getByRole("button", { name: /save changes/i }),
			);

			await waitFor(() => {
				const payload = mockUpdateProjectRole.mock.calls[0][2] as {
					permissions: Record<string, boolean>;
				};
				// normalizePermissionsToWildcards should compress tasks.read + tasks.write → tasks.*
				expect(payload.permissions?.["tasks.*"]).toBe(true);
			});
		});

		it("sends an empty permissions object when no permissions are toggled", async () => {
			mockCreateProjectRole.mockResolvedValue(existingRole);
			renderCreate();

			await userEvent.type(screen.getByLabelText(/role name/i), "VIEWER");
			// no toggles changed — all permissions off by default
			await userEvent.click(
				screen.getByRole("button", { name: /create role/i }),
			);

			await waitFor(() => {
				const payload = mockCreateProjectRole.mock.calls[0][1] as {
					permissions: Record<string, boolean>;
				};
				expect(Object.keys(payload.permissions ?? {})).toHaveLength(0);
			});
		});
	});
});
