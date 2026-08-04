// Tests for agent-activity-filters.tsx

import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { AgentActivityFilters as AgentActivityFiltersState } from "@/lib/agent-api";
import { AgentActivityFilters } from "./agent-activity-filters";

describe("AgentActivityFilters", () => {
	it("debounces search input before calling onFiltersChange", async () => {
		const onFiltersChange = vi.fn();
		render(
			<AgentActivityFilters filters={{}} onFiltersChange={onFiltersChange} />,
		);

		fireEvent.change(screen.getByPlaceholderText(/search activity/i), {
			target: { value: "renamed" },
		});

		expect(onFiltersChange).not.toHaveBeenCalled();

		await waitFor(() => {
			expect(onFiltersChange).toHaveBeenCalledWith({ search: "renamed" });
		});
	});

	it("toggling a source type checkbox adds it to the filters", async () => {
		const onFiltersChange = vi.fn();
		render(
			<AgentActivityFilters filters={{}} onFiltersChange={onFiltersChange} />,
		);

		fireEvent.click(screen.getByRole("button", { name: /filters/i }));
		fireEvent.click(await screen.findByText("Task"));

		expect(onFiltersChange).toHaveBeenCalledWith({ sourceTypes: ["task"] });
	});

	it("clear-all resets the search input and calls onFiltersChange with no filters", async () => {
		const onFiltersChange = vi.fn();
		const filters: AgentActivityFiltersState = { search: "renamed" };
		render(
			<AgentActivityFilters
				filters={filters}
				onFiltersChange={onFiltersChange}
			/>,
		);

		const searchInput = screen.getByPlaceholderText(
			/search activity/i,
		) as HTMLInputElement;
		expect(searchInput.value).toBe("renamed");

		fireEvent.click(screen.getByRole("button", { name: /clear filters/i }));

		expect(onFiltersChange).toHaveBeenCalledWith({});
		expect(searchInput.value).toBe("");
	});
});
