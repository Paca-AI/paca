import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { MembersFilters } from "./MembersFilters";

function setup(
	over: Partial<React.ComponentProps<typeof MembersFilters>> = {},
) {
	const props = {
		search: "",
		roles: [
			{ name: "Admin", count: 2 },
			{ name: "Member", count: 5 },
			{ name: "Viewer", count: 0 },
		],
		totalCount: 7,
		selectedRole: "",
		shownCount: 7,
		onSearchChange: vi.fn(),
		onRoleChange: vi.fn(),
		onClear: vi.fn(),
		...over,
	};
	render(<MembersFilters {...props} />);
	return props;
}

describe("MembersFilters", () => {
	it("shows per-role counts and omits roles nobody holds", () => {
		setup();
		expect(
			screen.getByRole("button", { name: /^All\s*7$/ }),
		).toBeInTheDocument();
		expect(
			screen.getByRole("button", { name: /^Admin\s*2$/ }),
		).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: /Viewer/ }),
		).not.toBeInTheDocument();
	});

	it("reports role selection", async () => {
		const user = userEvent.setup();
		const props = setup();
		await user.click(screen.getByRole("button", { name: /^Admin/ }));
		expect(props.onRoleChange).toHaveBeenCalledWith("Admin");
	});

	it("clears search via X and Escape", async () => {
		const user = userEvent.setup();
		const props = setup({ search: "ali" });
		await user.click(screen.getByRole("button", { name: /clear search/i }));
		expect(props.onSearchChange).toHaveBeenCalledWith("");
		vi.mocked(props.onSearchChange).mockClear();
		await user.type(screen.getByRole("searchbox"), "{Escape}");
		expect(props.onSearchChange).toHaveBeenCalledWith("");
	});

	it("shows 'Showing x of y' with a clear action only while filtering", async () => {
		const user = userEvent.setup();
		const props = setup({ search: "ali", shownCount: 1 });
		expect(screen.getByText("Showing 1 of 7")).toBeInTheDocument();
		await user.click(screen.getByRole("button", { name: "Clear filters" }));
		expect(props.onClear).toHaveBeenCalled();
	});

	it("hides the summary when no filter is active", () => {
		setup();
		expect(screen.queryByText(/Showing/)).not.toBeInTheDocument();
	});
});
