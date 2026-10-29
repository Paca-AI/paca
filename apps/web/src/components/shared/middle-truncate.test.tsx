import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { MiddleTruncate } from "./middle-truncate";

describe("MiddleTruncate", () => {
	it("leaves short names intact", () => {
		render(<MiddleTruncate text="Ada" />);
		expect(screen.getByTitle("Ada")).toHaveTextContent("Ada");
		expect(screen.getAllByText("Ada")).toHaveLength(1);
	});

	it("keeps the full accessible name while reserving the distinguishing suffix", () => {
		const name = "backend-refactor-agent-v2";
		render(
			<button type="button">
				<MiddleTruncate text={name} />
			</button>,
		);
		expect(screen.getByRole("button", { name })).toBeInTheDocument();
		expect(screen.getByTitle(name)).toBeInTheDocument();
		expect(screen.getByText("agent-v2")).toBeVisible();
	});

	it.each([
		"e\u0301",
		"👩‍💻",
		"🇨🇦",
	])("does not split the grapheme %s at the suffix boundary", (grapheme) => {
		const name = `worker-${grapheme.repeat(9)}`;
		const { container } = render(<MiddleTruncate text={name} />);
		const visibleParts = container.querySelectorAll('[aria-hidden="true"]');
		expect(visibleParts[1]).toHaveTextContent(grapheme.repeat(8));
		expect(Array.from(visibleParts, (part) => part.textContent).join("")).toBe(
			name,
		);
	});
});
