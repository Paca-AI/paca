import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { UsersFilters } from "./UsersFilters";

function setup(over: Partial<React.ComponentProps<typeof UsersFilters>> = {}) {
	const props = {
		roles: [],
		selectedRole: "",
		isFetching: false,
		isFiltering: false,
		resultCount: 0,
		onSearchChange: vi.fn(),
		onRoleChange: vi.fn(),
		onClear: vi.fn(),
		...over,
	};
	render(<UsersFilters {...props} />);
	return props;
}

describe("UsersFilters", () => {
	it("debounces search input into a single onSearchChange call", async () => {
		const user = userEvent.setup();
		const props = setup();

		await user.type(screen.getByRole("searchbox"), "ali");
		expect(props.onSearchChange).not.toHaveBeenCalled();
		await waitFor(() => expect(props.onSearchChange).toHaveBeenCalledTimes(1));
		expect(props.onSearchChange).toHaveBeenCalledWith("ali");
	});

	it("clears the search with the X button and with Escape", async () => {
		const user = userEvent.setup();
		const props = setup();
		const box = screen.getByRole("searchbox");

		await user.type(box, "ali");
		await user.click(screen.getByRole("button", { name: /clear search/i }));
		expect(box).toHaveValue("");
		await waitFor(() => expect(props.onSearchChange).toHaveBeenCalledWith(""));

		await user.type(box, "bo{Escape}");
		expect(box).toHaveValue("");
	});

	it("shows a spinner instead of the clear button while fetching", async () => {
		const user = userEvent.setup();
		setup({ isFetching: true });
		await user.type(screen.getByRole("searchbox"), "a");
		expect(
			screen.queryByRole("button", { name: /clear search/i }),
		).not.toBeInTheDocument();
	});

	it("shows the result count and a clear-all action only while filtering", async () => {
		const user = userEvent.setup();
		const props = setup({ isFiltering: true, resultCount: 7 });

		expect(screen.getByText("Results: 7")).toBeInTheDocument();
		await user.click(screen.getByRole("button", { name: "Clear filters" }));
		expect(props.onClear).toHaveBeenCalled();
	});

	it("hides the result row when not filtering and the role menu without roles", () => {
		setup();
		expect(screen.queryByText(/Results:/)).not.toBeInTheDocument();
		expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
	});

	it("renders the role menu when roles exist", () => {
		setup({ roles: ["ADMIN", "USER"] });
		expect(screen.getByRole("combobox")).toBeInTheDocument();
	});
});
