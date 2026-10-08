import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { makeRole } from "@/test/render-with-queries";
import { RoleSelectPanel } from "./role-select";

const OWNER = {
	...makeRole("r-owner", "OWNER", { "*": true }),
	description: "Does anything in the project",
	is_system: true,
};
const EDITOR = {
	...makeRole("r-editor", "EDITOR", { "tasks:write": true }),
	description: "Edits tasks",
};
const VIEWER = {
	...makeRole("r-viewer", "VIEWER", { "tasks:read": true }),
	is_default: true,
};
const ROLES = [OWNER, EDITOR, VIEWER];

function renderPanel(
	props: Partial<React.ComponentProps<typeof RoleSelectPanel>> = {},
) {
	const onChange = vi.fn();
	render(
		<RoleSelectPanel
			label="Roles"
			roles={ROLES}
			values={[]}
			onChange={onChange}
			{...props}
		/>,
	);
	return { onChange };
}

const search = () => screen.getByRole("combobox", { name: "Search roles" });

describe("RoleSelectPanel", () => {
	it("shows each role's name, description and flags", () => {
		renderPanel({ currentRoleIds: ["r-editor"] });

		const owner = screen.getByRole("option", { name: "OWNER" });
		expect(owner).toHaveAccessibleDescription(/Full access/);
		expect(owner).toHaveAccessibleDescription(/Built-in/);
		expect(owner).toHaveAccessibleDescription(/Does anything in the project/);
		expect(
			screen.getByRole("option", { name: "VIEWER" }),
		).toHaveAccessibleDescription(/Default/);
		expect(
			screen.getByRole("option", { name: "EDITOR" }),
		).toHaveAccessibleDescription(/Current/);
	});

	it("filters by name as you type", async () => {
		renderPanel();

		await userEvent.type(search(), "edit");

		expect(screen.getAllByRole("option")).toHaveLength(1);
		expect(screen.getByRole("option", { name: "EDITOR" })).toBeInTheDocument();
	});

	it("filters by description too", async () => {
		renderPanel();

		await userEvent.type(search(), "anything");

		expect(screen.getAllByRole("option")).toHaveLength(1);
		expect(screen.getByRole("option", { name: "OWNER" })).toBeInTheDocument();
	});

	it("says so when nothing matches, and the clear button restores the list", async () => {
		renderPanel();

		await userEvent.type(search(), "zzz");

		expect(screen.queryByRole("option")).not.toBeInTheDocument();
		expect(screen.getByRole("status")).toHaveTextContent(
			"No roles match “zzz”",
		);

		await userEvent.click(screen.getByRole("button", { name: "Clear search" }));

		expect(screen.getAllByRole("option")).toHaveLength(3);
	});

	it("marks selected roles and counts them", () => {
		renderPanel({ values: ["r-owner", "r-viewer"] });

		expect(screen.getByRole("option", { name: "OWNER" })).toHaveAttribute(
			"aria-selected",
			"true",
		);
		expect(screen.getByRole("option", { name: "EDITOR" })).toHaveAttribute(
			"aria-selected",
			"false",
		);
		expect(screen.getByText("Selected: 2")).toBeInTheDocument();
	});

	it("adds a role in list order, and removes one, on click", async () => {
		const { onChange } = renderPanel({ values: ["r-owner"] });

		await userEvent.click(screen.getByRole("option", { name: "EDITOR" }));
		expect(onChange).toHaveBeenLastCalledWith(
			["r-owner", "r-editor"],
			[OWNER, EDITOR],
		);

		await userEvent.click(screen.getByRole("option", { name: "OWNER" }));
		expect(onChange).toHaveBeenLastCalledWith([], []);
	});

	it("clears the whole selection with Clear", async () => {
		const { onChange } = renderPanel({ values: ["r-owner", "r-editor"] });

		await userEvent.click(screen.getByRole("button", { name: "Clear" }));

		expect(onChange).toHaveBeenCalledWith([], []);
	});

	it("offers no Clear when nothing is selected", () => {
		renderPanel();

		expect(
			screen.queryByRole("button", { name: "Clear" }),
		).not.toBeInTheDocument();
	});

	describe("keyboard", () => {
		it("moves with the arrow keys and toggles with Enter, keeping focus in the search box", async () => {
			const { onChange } = renderPanel();
			await userEvent.click(search());

			expect(search()).toHaveAttribute(
				"aria-activedescendant",
				screen.getByRole("option", { name: "OWNER" }).id,
			);
			await userEvent.keyboard("{ArrowDown}");
			expect(search()).toHaveAttribute(
				"aria-activedescendant",
				screen.getByRole("option", { name: "EDITOR" }).id,
			);
			await userEvent.keyboard("{Enter}");

			expect(onChange).toHaveBeenCalledWith(["r-editor"], [EDITOR]);
			expect(search()).toHaveFocus();
		});

		it("wraps around at both ends", async () => {
			renderPanel();
			await userEvent.click(search());

			await userEvent.keyboard("{ArrowUp}");
			expect(search()).toHaveAttribute(
				"aria-activedescendant",
				screen.getByRole("option", { name: "VIEWER" }).id,
			);
			await userEvent.keyboard("{ArrowDown}");
			expect(search()).toHaveAttribute(
				"aria-activedescendant",
				screen.getByRole("option", { name: "OWNER" }).id,
			);
		});

		it("Enter picks the first match after filtering", async () => {
			const { onChange } = renderPanel();

			await userEvent.type(search(), "view{Enter}");

			expect(onChange).toHaveBeenCalledWith(["r-viewer"], [VIEWER]);
		});

		it("Escape clears the search first", async () => {
			renderPanel();
			await userEvent.type(search(), "edit");

			await userEvent.keyboard("{Escape}");

			expect(search()).toHaveValue("");
			expect(screen.getAllByRole("option")).toHaveLength(3);
		});
	});

	describe("at least one role (minSelected)", () => {
		it("won't unpick the last role, says why, and hides Clear", async () => {
			const { onChange } = renderPanel({
				values: ["r-editor"],
				minSelected: 1,
			});

			const editor = screen.getByRole("option", { name: "EDITOR" });
			expect(editor).toHaveAttribute("aria-disabled", "true");
			expect(
				screen.getByText(/At least one role is required/),
			).toBeInTheDocument();
			expect(
				screen.queryByRole("button", { name: "Clear" }),
			).not.toBeInTheDocument();

			await userEvent.click(editor);

			expect(onChange).not.toHaveBeenCalled();
		});

		it("still lets another role be added, and then either be removed", async () => {
			const { onChange } = renderPanel({
				values: ["r-editor", "r-viewer"],
				minSelected: 1,
			});

			expect(
				screen.getByRole("option", { name: "EDITOR" }),
			).not.toHaveAttribute("aria-disabled");
			await userEvent.click(screen.getByRole("option", { name: "EDITOR" }));

			expect(onChange).toHaveBeenCalledWith(["r-viewer"], [VIEWER]);
		});

		it("also refuses the last role from the keyboard", async () => {
			const { onChange } = renderPanel({ values: ["r-owner"], minSelected: 1 });

			await userEvent.type(search(), "owner{Enter}");

			expect(onChange).not.toHaveBeenCalled();
		});
	});

	it("changes nothing while disabled", async () => {
		const { onChange } = renderPanel({ disabled: true });

		await userEvent.click(screen.getByRole("option", { name: "EDITOR" }));
		await userEvent.type(search(), "{Enter}");

		expect(onChange).not.toHaveBeenCalled();
	});
});
