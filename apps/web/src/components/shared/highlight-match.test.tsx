import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { HighlightMatch } from "./highlight-match";

describe("HighlightMatch", () => {
	it("marks each matching word case-insensitively", () => {
		const { container } = render(
			<HighlightMatch text="Alice Smith" query="smi ALI" />,
		);
		const marks = [...container.querySelectorAll("mark")].map(
			(m) => m.textContent,
		);
		expect(marks).toEqual(["Ali", "Smi"]);
		expect(container.textContent).toBe("Alice Smith");
	});

	it("renders plain text for an empty query and treats regex chars literally", () => {
		const a = render(<HighlightMatch text="Bob" query="  " />);
		expect(a.container.querySelector("mark")).toBeNull();
		const b = render(<HighlightMatch text="a.b" query="." />);
		expect(b.container.querySelectorAll("mark")).toHaveLength(1);
	});
});
