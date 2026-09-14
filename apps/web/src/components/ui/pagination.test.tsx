import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { Pagination } from "./pagination";

describe("Pagination", () => {
	it("renders nothing for a single page", () => {
		const { container } = render(
			<Pagination page={1} totalPages={1} onPageChange={vi.fn()} />,
		);

		expect(container).toBeEmptyDOMElement();
	});

	it("renders nothing when there are no pages", () => {
		const { container } = render(
			<Pagination page={1} totalPages={0} onPageChange={vi.fn()} />,
		);

		expect(container).toBeEmptyDOMElement();
	});

	it("shows the current page and total", () => {
		render(<Pagination page={2} totalPages={5} onPageChange={vi.fn()} />);

		expect(screen.getByText("Page 2 of 5")).toBeInTheDocument();
	});

	it("renders every page number when the range is short", () => {
		render(<Pagination page={1} totalPages={5} onPageChange={vi.fn()} />);

		for (const n of [1, 2, 3, 4, 5]) {
			expect(
				screen.getByRole("button", { name: `Page ${n}` }),
			).toBeInTheDocument();
		}
	});

	it("collapses a long range with ellipses around the current page", () => {
		render(<Pagination page={10} totalPages={20} onPageChange={vi.fn()} />);

		expect(screen.getByRole("button", { name: "Page 1" })).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Page 20" })).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Page 9" })).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Page 11" })).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: "Page 5" }),
		).not.toBeInTheDocument();
	});

	it("disables Previous on the first page and Next on the last page", () => {
		const { rerender } = render(
			<Pagination page={1} totalPages={3} onPageChange={vi.fn()} />,
		);
		expect(
			screen.getByRole("button", { name: "Previous page" }),
		).toBeDisabled();
		expect(screen.getByRole("button", { name: "Next page" })).toBeEnabled();

		rerender(<Pagination page={3} totalPages={3} onPageChange={vi.fn()} />);
		expect(screen.getByRole("button", { name: "Previous page" })).toBeEnabled();
		expect(screen.getByRole("button", { name: "Next page" })).toBeDisabled();
	});

	it("calls onPageChange with the target page when a page number is clicked", async () => {
		const onPageChange = vi.fn();
		render(<Pagination page={1} totalPages={5} onPageChange={onPageChange} />);

		await userEvent.click(screen.getByRole("button", { name: "Page 3" }));

		expect(onPageChange).toHaveBeenCalledWith(3);
	});

	it("calls onPageChange with page - 1 / page + 1 for Previous/Next", async () => {
		const onPageChange = vi.fn();
		render(<Pagination page={2} totalPages={5} onPageChange={onPageChange} />);

		await userEvent.click(
			screen.getByRole("button", { name: "Previous page" }),
		);
		expect(onPageChange).toHaveBeenCalledWith(1);

		await userEvent.click(screen.getByRole("button", { name: "Next page" }));
		expect(onPageChange).toHaveBeenCalledWith(3);
	});

	it("marks the current page button with aria-current", () => {
		render(<Pagination page={2} totalPages={5} onPageChange={vi.fn()} />);

		expect(screen.getByRole("button", { name: "Page 2" })).toHaveAttribute(
			"aria-current",
			"page",
		);
		expect(screen.getByRole("button", { name: "Page 3" })).not.toHaveAttribute(
			"aria-current",
		);
	});
});
