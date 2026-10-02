import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { SearchableList, type SearchableListItem } from "./searchable-list";

function makeItems(
	n: number,
	onSelect: (i: number) => void,
	selected = -1,
): SearchableListItem[] {
	return Array.from({ length: n }, (_, i) => ({
		key: String(i),
		label: `Person ${i}`,
		group: i < n / 2 ? "First" : "Second",
		selected: i === selected,
		onSelect: () => onSelect(i),
		content: <span>{`Person ${i}`}</span>,
	}));
}

describe("SearchableList", () => {
	it("hides the search box for short lists", () => {
		render(<SearchableList items={makeItems(3, vi.fn())} />);
		expect(screen.queryByRole("textbox")).toBeNull();
	});

	it("filters as you type and Enter picks the first match", async () => {
		const onSelect = vi.fn();
		render(<SearchableList items={makeItems(12, onSelect)} />);
		const input = screen.getByRole("textbox");
		expect(input).toHaveFocus();
		await userEvent.type(input, "person 7{Enter}");
		expect(screen.queryByText("Person 3")).toBeNull();
		expect(onSelect).toHaveBeenCalledExactlyOnceWith(7);
	});

	it("Enter does nothing before a query is typed", async () => {
		const onSelect = vi.fn();
		render(<SearchableList items={makeItems(12, onSelect)} />);
		await userEvent.type(screen.getByRole("textbox"), "{Enter}");
		expect(onSelect).not.toHaveBeenCalled();
	});

	it("Enter never acts on an already-selected row", async () => {
		const onSelect = vi.fn();
		render(<SearchableList items={makeItems(12, onSelect, 7)} />);
		await userEvent.type(screen.getByRole("textbox"), "person 7{Enter}");
		expect(onSelect).not.toHaveBeenCalled();
	});

	it("keeps group headings for the filtered result and hides pinned rows", async () => {
		render(
			<SearchableList
				items={makeItems(12, vi.fn())}
				pinned={<button type="button">Unassigned</button>}
			/>,
		);
		expect(screen.getByText("Unassigned")).toBeInTheDocument();
		await userEvent.type(screen.getByRole("textbox"), "person 9");
		expect(screen.getByText("Second")).toBeInTheDocument();
		expect(screen.queryByText("First")).toBeNull();
		expect(screen.queryByText("Unassigned")).toBeNull();
	});
});
