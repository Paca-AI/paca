import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { DisabledDeleteButton } from "@/components/shared/disabled-delete-button";

describe("DisabledDeleteButton", () => {
	const reason = "The default role can't be deleted.";

	it("is a disabled button named by its label", () => {
		render(<DisabledDeleteButton label="Delete role" reason={reason} />);

		expect(screen.getByRole("button", { name: "Delete role" })).toBeDisabled();
	});

	it("says why, to the mouse and to assistive technology", () => {
		render(<DisabledDeleteButton label="Delete role" reason={reason} />);
		const button = screen.getByRole("button", { name: "Delete role" });

		// A disabled button has no hover of its own, so the wrapper carries the tooltip…
		expect(button.closest("span[title]")).toHaveAttribute("title", reason);
		// …and the same reason is read out with the button.
		expect(button).toHaveAccessibleDescription(reason);
	});
});
