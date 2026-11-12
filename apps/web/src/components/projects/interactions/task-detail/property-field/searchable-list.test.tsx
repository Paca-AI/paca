import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { SearchableList } from "./searchable-list";

const item = (key: string) => ({
	key,
	label: key,
	onSelect: vi.fn(),
	content: <span>{key}</span>,
});

describe("SearchableList remote mode", () => {
	const remote = (over = {}) => ({
		query: "",
		onQueryChange: vi.fn(),
		hasMore: false,
		isLoadingMore: false,
		onLoadMore: vi.fn(),
		...over,
	});

	it("always shows the search box and reports typing instead of filtering locally", async () => {
		const r = remote();
		render(<SearchableList items={[item("alice"), item("bob")]} remote={r} />);
		await userEvent.type(screen.getByRole("combobox"), "z");
		expect(r.onQueryChange).toHaveBeenCalledWith("z");
		// Not filtered locally: the server owns the results.
		expect(screen.getByText("alice")).toBeInTheDocument();
		expect(screen.getByText("bob")).toBeInTheDocument();
	});

	it("offers load-more when the server has another page", async () => {
		const r = remote({ hasMore: true });
		render(<SearchableList items={[item("alice")]} remote={r} />);
		await userEvent.click(screen.getByRole("button", { name: /load more/i }));
		expect(r.onLoadMore).toHaveBeenCalledTimes(1);
	});

	it("hides pinned rows while a query is active", () => {
		render(
			<SearchableList
				items={[item("alice")]}
				pinned={<span>Unassigned</span>}
				remote={remote({ query: "al" })}
			/>,
		);
		expect(screen.queryByText("Unassigned")).not.toBeInTheDocument();
	});
});
