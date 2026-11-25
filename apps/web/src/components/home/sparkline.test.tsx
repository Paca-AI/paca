import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Sparkline, sparklinePoints, weekDelta } from "./sparkline";

describe("sparklinePoints", () => {
	it("spans the full width and maps max to the top, min to the bottom", () => {
		const points = sparklinePoints([0, 5, 10]);
		expect(points.map(([x]) => x)).toEqual([0, 50, 100]);
		expect(points[0][1]).toBeGreaterThan(points[2][1]);
		expect(points[0][1]).toBe(29);
		expect(points[2][1]).toBe(3);
	});

	it("draws a flat series across the middle", () => {
		expect(sparklinePoints([4, 4, 4]).map(([, y]) => y)).toEqual([16, 16, 16]);
	});
});

describe("weekDelta", () => {
	it("compares the last value with the one seven days earlier", () => {
		expect(weekDelta([1, 2, 3, 4, 5, 6, 7, 8, 20])).toBe(18);
		expect(weekDelta([9, 9, 9, 9, 9, 9, 9, 2])).toBe(-7);
	});

	it("returns null when there is less than a week of data", () => {
		expect(weekDelta([1, 2, 3, 4, 5, 6, 7])).toBeNull();
		expect(weekDelta([])).toBeNull();
	});
});

describe("Sparkline", () => {
	it("renders a labelled chart", () => {
		render(<Sparkline values={[1, 3, 2]} label="Open tasks, last 30 days" />);
		expect(
			screen.getByRole("img", { name: "Open tasks, last 30 days" }),
		).toBeInTheDocument();
	});

	it("renders nothing for fewer than two points", () => {
		const { container } = render(<Sparkline values={[1]} label="x" />);
		expect(container).toBeEmptyDOMElement();
	});
});
