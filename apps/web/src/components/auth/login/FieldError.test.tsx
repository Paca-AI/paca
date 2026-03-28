import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { FieldError } from "./FieldError";

describe("FieldError", () => {
	it("does not render when field is untouched", () => {
		render(<FieldError isTouched={false} error="Username is required" />);

		expect(screen.queryByRole("alert")).toBeNull();
	});

	it("does not render when there is no error", () => {
		render(<FieldError isTouched={true} error={undefined} />);

		expect(screen.queryByRole("alert")).toBeNull();
	});

	it("renders alert text when touched and invalid", () => {
		render(<FieldError isTouched={true} error="Username is required" />);

		expect(screen.getByRole("alert")).toHaveTextContent("Username is required");
	});
});
