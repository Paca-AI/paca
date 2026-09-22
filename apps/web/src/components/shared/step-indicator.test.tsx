import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { StepIndicator } from "./step-indicator";

/** The dots are decoration (aria-hidden); the label is what is announced. */
const dots = (container: HTMLElement) =>
	Array.from(
		container.querySelectorAll<HTMLElement>('[aria-hidden="true"] > span'),
	);

describe("StepIndicator", () => {
	it("shows the label it is given", () => {
		render(<StepIndicator step={2} total={3} label="2 / 3" />);

		expect(screen.getByText("2 / 3")).toBeInTheDocument();
	});

	it("draws one dot per step and fills those up to the current step", () => {
		const { container } = render(
			<StepIndicator step={2} total={3} label="2 / 3" />,
		);

		const filled = dots(container).map((dot) =>
			dot.classList.contains("bg-primary"),
		);
		expect(filled).toEqual([true, true, false]);
	});

	it("works for any number of steps", () => {
		const { container } = render(
			<StepIndicator step={1} total={2} label="1 / 2" />,
		);

		expect(dots(container)).toHaveLength(2);
	});

	it("hides the dots from assistive technology", () => {
		const { container } = render(
			<StepIndicator step={1} total={3} label="1 / 3" />,
		);

		expect(container.querySelector('[aria-hidden="true"]')).not.toBeNull();
	});
});
