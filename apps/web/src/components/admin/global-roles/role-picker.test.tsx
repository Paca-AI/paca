import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const { mockGet } = vi.hoisted(() => ({ mockGet: vi.fn() }));

// Only the states that need the list to be *fetched* (loading, failed) go
// through the client; the rest render from a seeded cache.
vi.mock("@/lib/api-client", () => ({
	apiClient: { instance: { get: mockGet } },
}));

import { makeRole, renderWithQueries } from "@/test/render-with-queries";
import { isFullAccessRole, RolePicker } from "./role-picker";

const USER = makeRole("role-user", "USER", { "tasks.read": true });
const ADMIN = makeRole("role-admin", "ADMIN", {
	"users.read": true,
	"users.write": true,
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
			label="Change role"
			value={null}
			onChange={onChange}
			{...props}
		/>,
		roles,
	);
	return { onChange, ...view };
}

beforeEach(() => vi.clearAllMocks());

describe("RolePicker", () => {
	it("lists every role as a radio in a group with the given label", () => {
		renderPicker();

		const group = screen.getByRole("radiogroup", { name: "Change role" });
		expect(group).toBeInTheDocument();
		expect(screen.getAllByRole("radio")).toHaveLength(3);
		expect(screen.getByRole("radio", { name: "ADMIN" })).toBeInTheDocument();
	});

	it("shows what a role grants, so it is chosen for what it allows", () => {
		renderPicker();

		expect(screen.getByText("tasks.read")).toBeInTheDocument();
		expect(screen.getByText("users.write")).toBeInTheDocument();
		expect(screen.getByText("Full access")).toBeInTheDocument();
	});

	it("folds a long permission list into +N", () => {
		const many = makeRole(
			"role-many",
			"MANY",
			Object.fromEntries(
				["a", "b", "c", "d", "e", "f"].map((k) => [`tasks.${k}`, true]),
			),
		);
		renderPicker({}, { roles: [many] });

		expect(screen.getByText("tasks.a")).toBeInTheDocument();
		expect(screen.queryByText("tasks.f")).not.toBeInTheDocument();
		expect(screen.getByText("+2")).toHaveAttribute("title", "tasks.e, tasks.f");
	});

	it("says so when a role grants nothing", () => {
		renderPicker({}, { roles: [makeRole("role-none", "NONE")] });

		expect(screen.getByText("No permissions assigned")).toBeInTheDocument();
	});

	it("marks the current role (by name) and selects it until another is picked", () => {
		renderPicker({ currentRoleName: "ADMIN" });

		const admin = screen.getByRole("radio", { name: "ADMIN" });
		expect(admin).toBeChecked();
		expect(admin).toHaveAccessibleDescription(/Current/);
		expect(screen.getByRole("radio", { name: "USER" })).not.toBeChecked();
		expect(screen.getAllByText("Current")).toHaveLength(1);
	});

	it("marks the current role by id too", () => {
		renderPicker({ currentRoleId: "role-user" });

		expect(screen.getByRole("radio", { name: "USER" })).toBeChecked();
	});

	it("reports the whole role when one is picked", async () => {
		const { onChange } = renderPicker({ currentRoleName: "USER" });

		await userEvent.click(screen.getByRole("radio", { name: "ADMIN" }));

		expect(onChange).toHaveBeenCalledWith(ADMIN);
	});

	it("shows the picked role as selected instead of the current one", () => {
		renderPicker({ currentRoleName: "USER", value: "role-admin" });

		expect(screen.getByRole("radio", { name: "ADMIN" })).toBeChecked();
		expect(screen.getByRole("radio", { name: "USER" })).not.toBeChecked();
	});

	it("disables every choice when disabled", () => {
		renderPicker({ disabled: true });

		for (const radio of screen.getAllByRole("radio")) {
			expect(radio).toBeDisabled();
		}
	});

	it("says so when there are no roles", () => {
		renderPicker({}, { roles: [] });

		expect(
			screen.getByText("No roles have been created yet."),
		).toBeInTheDocument();
		expect(screen.queryByRole("radio")).not.toBeInTheDocument();
	});

	it("shows placeholders, not choices, while the roles load", () => {
		mockGet.mockReturnValue(new Promise(() => {}));
		renderPicker({}, { roles: null });

		expect(screen.queryByRole("radio")).not.toBeInTheDocument();
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

		await waitFor(() => expect(screen.getAllByRole("radio")).toHaveLength(2));
		expect(screen.queryByRole("alert")).not.toBeInTheDocument();
	});
});

describe("RolePicker — a long list", () => {
	// jsdom has no layout and no scrollIntoView; stub it to see what is asked for.
	const original = Element.prototype.scrollIntoView;
	afterEach(() => {
		Element.prototype.scrollIntoView = original;
	});

	it("brings the role that is selected on arrival into view, so it never sits below the fold", () => {
		const scrollIntoView = vi.fn();
		Element.prototype.scrollIntoView = scrollIntoView;

		renderPicker({ currentRoleName: "SUPER_ADMIN" });

		expect(scrollIntoView).toHaveBeenCalledTimes(1);
		const scrolled = scrollIntoView.mock.contexts[0] as HTMLElement;
		expect(scrolled).toContainElement(
			screen.getByRole("radio", { name: "SUPER_ADMIN" }),
		);
		expect(scrollIntoView).toHaveBeenCalledWith({ block: "nearest" });
	});

	it("does not scroll again when the person picks another role", async () => {
		const scrollIntoView = vi.fn();
		Element.prototype.scrollIntoView = scrollIntoView;
		renderPicker({ currentRoleName: "USER" });
		scrollIntoView.mockClear();

		await userEvent.click(screen.getByRole("radio", { name: "ADMIN" }));

		expect(scrollIntoView).not.toHaveBeenCalled();
	});
});

describe("RolePicker — a custom badge on the current role", () => {
	it("shows the given text instead of 'Current' (a new account's role is its default)", () => {
		renderPicker({ currentRoleName: "USER", currentBadge: "Default" });

		expect(
			screen.getByRole("radio", { name: "USER" }),
		).toHaveAccessibleDescription(/Default/);
		expect(screen.queryByText("Current")).not.toBeInTheDocument();
	});
});

describe("RolePicker — offering 'no role'", () => {
	const none = (onSelect = vi.fn()) => ({
		label: "No global role",
		hint: "This agent has no global permissions.",
		onSelect,
	});

	it("lists 'no role' first, described by its hint, and selects it while nothing is picked", () => {
		renderPicker({ none: none() });

		const radios = screen.getAllByRole("radio");
		expect(radios).toHaveLength(4);
		expect(radios[0]).toHaveAccessibleName("No global role");
		expect(radios[0]).toHaveAccessibleDescription(
			"This agent has no global permissions.",
		);
		expect(radios[0]).toBeChecked();
		for (const radio of radios.slice(1)) expect(radio).not.toBeChecked();
	});

	it("selects a role instead once one is picked", () => {
		renderPicker({ none: none(), value: "role-admin" });

		expect(
			screen.getByRole("radio", { name: "No global role" }),
		).not.toBeChecked();
		expect(screen.getByRole("radio", { name: "ADMIN" })).toBeChecked();
	});

	it("reports 'no role' through onSelect, not onChange", async () => {
		const onSelect = vi.fn();
		const { onChange } = renderPicker({
			none: none(onSelect),
			value: "role-admin",
		});

		await userEvent.click(
			screen.getByRole("radio", { name: "No global role" }),
		);

		expect(onSelect).toHaveBeenCalledTimes(1);
		expect(onChange).not.toHaveBeenCalled();
	});

	it("does not fall back to the current role: with 'no role' on offer, null means none", () => {
		renderPicker({ none: none(), currentRoleName: "USER" });

		expect(screen.getByRole("radio", { name: "No global role" })).toBeChecked();
		expect(screen.getByRole("radio", { name: "USER" })).not.toBeChecked();
	});

	it("disables it with the rest when disabled", () => {
		renderPicker({ none: none(), disabled: true });

		expect(
			screen.getByRole("radio", { name: "No global role" }),
		).toBeDisabled();
	});

	it("offers no 'no role' choice unless asked to", () => {
		renderPicker();

		expect(screen.queryByText("No global role")).not.toBeInTheDocument();
		expect(screen.getAllByRole("radio")).toHaveLength(3);
	});
});

describe("isFullAccessRole", () => {
	it("is true only for a role holding the * wildcard", () => {
		expect(isFullAccessRole(ROOT)).toBe(true);
		expect(isFullAccessRole(ADMIN)).toBe(false);
		expect(isFullAccessRole(makeRole("x", "X", { "*": false }))).toBe(false);
	});
});
