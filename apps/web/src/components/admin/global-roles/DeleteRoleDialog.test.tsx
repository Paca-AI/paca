import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/lib/role-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/role-api")>("@/lib/role-api");
	return { ...actual, deleteRole: vi.fn() };
});

import { deleteRole, platformRolesQueryOptions } from "@/lib/role-api";
import { makeRole, renderWithQueries } from "@/test/render-with-queries";
import { DeleteRoleDialog } from "./DeleteRoleDialog";

const ADMIN = makeRole("role-admin", "ADMIN", { "users:read": true });

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
	vi.mocked(deleteRole).mockResolvedValue(undefined);
});

describe("DeleteRoleDialog", () => {
	it("deletes the role, refreshes the roles, and closes", async () => {
		const { onOpenChange, client } = renderDialog();
		const invalidate = vi.spyOn(client, "invalidateQueries");

		await userEvent.click(deleteButton());

		await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
		expect(deleteRole).toHaveBeenCalledWith("role-admin");
		expect(invalidate).toHaveBeenCalledWith({
			queryKey: platformRolesQueryOptions.queryKey,
		});
	});

	it.each([
		[
			"ROLE_IS_DEFAULT",
			"The default role can't be deleted. Make another role the default first.",
		],
		[
			"ROLE_LAST_FULL_ACCESS",
			"This would leave nobody with full access. Keep at least one full-access role assigned.",
		],
		["ROLE_IS_SYSTEM", "This is a built-in role and can't be changed."],
		["ROLE_NOT_FOUND", "This role no longer exists."],
		["FORBIDDEN", "You don't have permission to do this."],
	])("explains a %s refusal and stays open", async (code, message) => {
		vi.mocked(deleteRole).mockRejectedValue({
			response: { data: { error_code: code } },
		});
		const { onOpenChange } = renderDialog();

		await userEvent.click(deleteButton());

		expect(await screen.findByText(message)).toBeInTheDocument();
		expect(onOpenChange).not.toHaveBeenCalled();
	});
});
