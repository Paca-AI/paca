import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/lib/admin-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/admin-api")>("@/lib/admin-api");
	return { ...actual, deleteGlobalRole: vi.fn() };
});

import { deleteGlobalRole, globalRolesQueryOptions } from "@/lib/admin-api";
import { makeRole, renderWithQueries } from "@/test/render-with-queries";
import { DeleteRoleDialog } from "./DeleteRoleDialog";

const ADMIN = makeRole("role-admin", "ADMIN", { "users.read": true });

function renderDialog(onOpenChange = vi.fn()) {
	const view = renderWithQueries(
		<DeleteRoleDialog role={ADMIN} open onOpenChange={onOpenChange} />,
		{ roles: [ADMIN] },
	);
	return { onOpenChange, ...view };
}

const deleteButton = () => screen.getByRole("button", { name: "Delete role" });

beforeEach(() => {
	vi.resetAllMocks();
	vi.mocked(deleteGlobalRole).mockResolvedValue(undefined);
});

describe("DeleteRoleDialog", () => {
	it("deletes the role, refreshes the roles, and closes", async () => {
		const { onOpenChange, client } = renderDialog();
		const invalidate = vi.spyOn(client, "invalidateQueries");

		await userEvent.click(deleteButton());

		await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
		expect(deleteGlobalRole).toHaveBeenCalledWith("role-admin");
		expect(invalidate).toHaveBeenCalledWith({
			queryKey: globalRolesQueryOptions.queryKey,
		});
	});

	it.each([
		[
			"GLOBAL_ROLE_IS_DEFAULT",
			"The default role can't be deleted. Make another role the default first.",
		],
		[
			"GLOBAL_ROLE_HAS_ASSIGNED_USERS",
			"This role cannot be deleted because it is still assigned to one or more users.",
		],
		["GLOBAL_ROLE_NOT_FOUND", "This role no longer exists."],
		["FORBIDDEN", "You don't have permission to delete this role."],
	])("explains a %s refusal and stays open", async (code, message) => {
		vi.mocked(deleteGlobalRole).mockRejectedValue({
			response: { data: { error_code: code } },
		});
		const { onOpenChange } = renderDialog();

		await userEvent.click(deleteButton());

		expect(await screen.findByText(message)).toBeInTheDocument();
		expect(onOpenChange).not.toHaveBeenCalled();
	});
});
