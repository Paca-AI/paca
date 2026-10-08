import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockGet } = vi.hoisted(() => ({ mockGet: vi.fn() }));

// Only the states that need the list to be *fetched* (loading, failed) go
// through the client; the rest render from a seeded cache.
vi.mock("@/lib/api-client", () => ({
	apiClient: { instance: { get: mockGet } },
}));

import { makeRole, renderWithQueries } from "@/test/render-with-queries";
import { openRoleSelect } from "@/test/role-select";
import { isFullAccessRole, RolePicker } from "./role-picker";

const USER = makeRole("role-user", "USER", { "tasks:read": true });
const ADMIN = makeRole("role-admin", "ADMIN", {
	"users:read": true,
	"users:write": true,
});
const ROOT = makeRole("role-root", "SUPER_ADMIN", { "*": true });

function renderPicker(
	props: Partial<React.ComponentProps<typeof RolePicker>> = {},
	roles: Parameters<typeof renderWithQueries>[1] = {
		roles: [USER, ADMIN, ROOT],
	},
) {
	const onChange = vi.fn();
	const view = renderWithQueries(
		<RolePicker
			label="Change roles"
			values={[]}
			onChange={onChange}
			{...props}
		/>,
		roles,
	);
	return { onChange, ...view };
}

beforeEach(() => vi.clearAllMocks());

const selected = (name: string) =>
	screen.getByRole("option", { name }).getAttribute("aria-selected") === "true";

describe("RolePicker", () => {
	it("shows a field that opens a labelled list with every role as an option", async () => {
		renderPicker();

		expect(
			screen.getByRole("combobox", { name: "Change roles" }),
		).toBeInTheDocument();
		// Nothing is listed until the field is opened.
		expect(screen.queryByRole("option")).not.toBeInTheDocument();

		const list = await openRoleSelect("Change roles");

		expect(list).toHaveAccessibleName("Change roles");
		expect(list).toHaveAttribute("aria-multiselectable", "true");
		expect(screen.getAllByRole("option")).toHaveLength(3);
		expect(screen.getByRole("option", { name: "ADMIN" })).toBeInTheDocument();
	});

	it("shows what a role grants, so it is chosen for what it allows", async () => {
		renderPicker();
		await openRoleSelect();

		expect(screen.getByText("tasks:read")).toBeInTheDocument();
		expect(screen.getByText("users:write")).toBeInTheDocument();
		expect(screen.getByText("Full access")).toBeInTheDocument();
	});

	it("shows a role's description in place of the permission glance", async () => {
		const described = {
			...makeRole("role-d", "DESCRIBED", { "tasks:read": true }),
			description: "Reads tasks and nothing else",
		};
		renderPicker({}, { roles: [described] });
		await openRoleSelect();

		expect(
			screen.getByText("Reads tasks and nothing else"),
		).toBeInTheDocument();
		expect(screen.queryByText("tasks:read")).not.toBeInTheDocument();
	});

	it("folds a long permission list down to the first few", async () => {
		const many = makeRole(
			"role-many",
			"MANY",
			Object.fromEntries(
				["a", "b", "c", "d", "e", "f"].map((k) => [`tasks:${k}`, true]),
			),
		);
		renderPicker({}, { roles: [many] });
		await openRoleSelect();

		expect(screen.getByText("tasks:a")).toBeInTheDocument();
		expect(screen.queryByText("tasks:f")).not.toBeInTheDocument();
		expect(screen.getByText("+3")).toHaveAttribute(
			"title",
			"tasks:d, tasks:e, tasks:f",
		);
	});

	it("says so when a role grants nothing", async () => {
		renderPicker({}, { roles: [makeRole("role-none", "NONE")] });
		await openRoleSelect();

		expect(screen.getByText("No permissions assigned")).toBeInTheDocument();
	});

	it("selects the picked roles, and several can be selected at once", async () => {
		renderPicker({ values: ["role-user", "role-admin"] });
		await openRoleSelect();

		expect(selected("USER")).toBe(true);
		expect(selected("ADMIN")).toBe(true);
		expect(selected("SUPER_ADMIN")).toBe(false);
	});

	it("shows the picked roles as badges on the field, and says so when none", () => {
		const { unmount } = renderPicker({ values: ["role-user", "role-admin"] });
		const field = screen.getByRole("combobox", { name: "Change roles" });
		expect(field).toHaveTextContent("USER");
		expect(field).toHaveTextContent("ADMIN");
		unmount();

		renderPicker();
		expect(
			screen.getByRole("combobox", { name: "Change roles" }),
		).toHaveTextContent("Select roles");
	});

	it("marks the roles held today", async () => {
		renderPicker({
			values: ["role-admin"],
			currentRoleIds: ["role-admin", "role-user"],
		});
		await openRoleSelect();

		expect(
			screen.getByRole("option", { name: "ADMIN" }),
		).toHaveAccessibleDescription(/Current/);
		expect(
			screen.getByRole("option", { name: "USER" }),
		).toHaveAccessibleDescription(/Current/);
		expect(screen.getAllByText("Current")).toHaveLength(2);
	});

	it("reports the new set of ids, and the roles, when one is added", async () => {
		const { onChange } = renderPicker({ values: ["role-user"] });
		await openRoleSelect();

		await userEvent.click(screen.getByRole("option", { name: "ADMIN" }));

		expect(onChange).toHaveBeenCalledWith(
			["role-user", "role-admin"],
			[USER, ADMIN],
		);
	});

	it("reports the set without a role that is unpicked", async () => {
		const { onChange } = renderPicker({ values: ["role-user", "role-admin"] });
		await openRoleSelect();

		await userEvent.click(screen.getByRole("option", { name: "USER" }));

		expect(onChange).toHaveBeenCalledWith(["role-admin"], [ADMIN]);
	});

	it("allows picking none at all", async () => {
		const { onChange } = renderPicker({ values: ["role-user"] });
		await openRoleSelect();

		await userEvent.click(screen.getByRole("option", { name: "USER" }));

		expect(onChange).toHaveBeenCalledWith([], []);
	});

	it("disables the field when disabled", () => {
		renderPicker({ disabled: true });

		expect(
			screen.getByRole("combobox", { name: "Change roles" }),
		).toBeDisabled();
	});

	it("says so when there are no roles", () => {
		renderPicker({}, { roles: [] });

		expect(
			screen.getByText("No roles have been created yet."),
		).toBeInTheDocument();
		expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
	});

	it("shows a placeholder, not choices, while the roles load", () => {
		mockGet.mockReturnValue(new Promise(() => {}));
		renderPicker({}, { roles: null });

		expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
		expect(screen.queryByRole("alert")).not.toBeInTheDocument();
	});

	it("offers a retry when the roles fail to load, and recovers", async () => {
		mockGet.mockRejectedValueOnce(new Error("network"));
		mockGet.mockResolvedValueOnce({ data: { data: [USER, ADMIN] } });
		renderPicker({}, { roles: null });

		expect(await screen.findByRole("alert")).toHaveTextContent(
			"Couldn't load the roles.",
		);

		await userEvent.click(screen.getByRole("button", { name: "Try again" }));

		await openRoleSelect();
		await waitFor(() => expect(screen.getAllByRole("option")).toHaveLength(2));
		expect(screen.queryByRole("alert")).not.toBeInTheDocument();
	});
});

describe("RolePicker: the default role", () => {
	const DEFAULT_USER = makeRole(
		"role-user",
		"USER",
		{ "tasks:read": true },
		{ isDefault: true },
	);

	it("marks the role new accounts start with as the default", async () => {
		renderPicker({}, { roles: [DEFAULT_USER, ADMIN, ROOT] });
		await openRoleSelect();

		expect(
			screen.getByRole("option", { name: "USER" }),
		).toHaveAccessibleDescription(/Default/);
		expect(
			screen.getByRole("option", { name: "ADMIN" }),
		).not.toHaveAccessibleDescription(/Default/);
	});

	it("keeps the default apart from the roles held", async () => {
		renderPicker(
			{ currentRoleIds: ["role-admin"] },
			{ roles: [DEFAULT_USER, ADMIN, ROOT] },
		);
		await openRoleSelect();

		const user = screen.getByRole("option", { name: "USER" });
		expect(user).toHaveAccessibleDescription(/Default/);
		expect(user).not.toHaveAccessibleDescription(/Current/);
		const admin = screen.getByRole("option", { name: "ADMIN" });
		expect(admin).toHaveAccessibleDescription(/Current/);
		expect(admin).not.toHaveAccessibleDescription(/Default/);
	});

	it("marks nothing when no role is the default", async () => {
		renderPicker();
		await openRoleSelect();

		expect(screen.queryByText("Default")).not.toBeInTheDocument();
	});
});

describe("RolePicker: a long list", () => {
	it("keeps the search box focused when the list opens", async () => {
		renderPicker();
		await openRoleSelect();

		await waitFor(() =>
			expect(
				screen.getByRole("combobox", { name: "Search roles" }),
			).toHaveFocus(),
		);
	});
});

describe("isFullAccessRole", () => {
	it("is true only for a role holding the * wildcard", () => {
		expect(isFullAccessRole(ROOT)).toBe(true);
		expect(isFullAccessRole(ADMIN)).toBe(false);
		expect(isFullAccessRole(makeRole("x", "X", { "*": false }))).toBe(false);
	});
});
