import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/lib/admin-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/admin-api")>("@/lib/admin-api");
	return { ...actual, setDefaultGlobalRole: vi.fn() };
});

import { globalRolesQueryOptions, setDefaultGlobalRole } from "@/lib/admin-api";
import { makeRole, renderWithQueries } from "@/test/render-with-queries";
import { SetDefaultRoleDialog } from "./SetDefaultRoleDialog";

const ADMIN = makeRole("role-admin", "ADMIN", { "users.read": true });
const ROOT = makeRole("role-root", "SUPER_ADMIN", { "*": true });

function renderDialog(role = ADMIN, onOpenChange = vi.fn()) {
	const view = renderWithQueries(
		<SetDefaultRoleDialog role={role} open onOpenChange={onOpenChange} />,
		{ roles: [role] },
	);
	return { onOpenChange, ...view };
}

const button = (name: RegExp | string) => screen.getByRole("button", { name });

beforeEach(() => {
	vi.resetAllMocks();
	vi.mocked(setDefaultGlobalRole).mockResolvedValue(ADMIN);
});

describe("SetDefaultRoleDialog", () => {
	it("says what the default role is for, naming the role", () => {
		renderDialog();

		expect(screen.getByText("Set default role")).toBeInTheDocument();
		expect(
			screen.getByText(
				"New users and new global agents will start with the ADMIN role. Accounts that already exist keep the role they have.",
			),
		).toBeInTheDocument();
		expect(screen.queryByText(/grants every permission/i)).toBeNull();
	});

	it("warns when the role would hand every new account full access", () => {
		renderDialog(ROOT);

		expect(screen.getByText(/grants every permission/i)).toBeInTheDocument();
	});

	it("makes the role the default, refreshes the roles, and closes", async () => {
		const { onOpenChange, client } = renderDialog();
		const invalidate = vi.spyOn(client, "invalidateQueries");

		await userEvent.click(button("Set as default"));

		await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
		expect(setDefaultGlobalRole).toHaveBeenCalledWith("role-admin");
		expect(invalidate).toHaveBeenCalledWith({
			queryKey: globalRolesQueryOptions.queryKey,
		});
	});

	it("closes without changing anything on Cancel", async () => {
		const { onOpenChange } = renderDialog();

		await userEvent.click(button("Cancel"));

		expect(onOpenChange).toHaveBeenCalledWith(false);
		expect(setDefaultGlobalRole).not.toHaveBeenCalled();
	});

	it("shows 'Saving…' and locks the button while the request is in flight", async () => {
		vi.mocked(setDefaultGlobalRole).mockReturnValue(new Promise(() => {}));
		renderDialog();

		await userEvent.click(button("Set as default"));

		expect(
			await screen.findByRole("button", { name: /saving/i }),
		).toBeDisabled();
	});

	it.each([
		["GLOBAL_ROLE_NOT_FOUND", "This role no longer exists."],
		["FORBIDDEN", "You don't have permission to change the default role."],
		["INTERNAL_ERROR", "Something went wrong. Please try again."],
	])("explains a %s failure and stays open", async (code, message) => {
		vi.mocked(setDefaultGlobalRole).mockRejectedValue({
			response: { data: { error_code: code } },
		});
		const { onOpenChange } = renderDialog();

		await userEvent.click(button("Set as default"));

		expect(await screen.findByText(message)).toBeInTheDocument();
		expect(onOpenChange).not.toHaveBeenCalled();
		expect(button("Set as default")).toBeEnabled();
	});
});
