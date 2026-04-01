import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { UsersHeader } from "./UsersHeader";

describe("UsersHeader", () => {
	it("renders title and description", () => {
		render(<UsersHeader canWrite={false} onCreate={vi.fn()} />);

		expect(
			screen.getByRole("heading", { name: /user management/i }),
		).toBeInTheDocument();
		expect(
			screen.getByText(/view and manage user accounts/i),
		).toBeInTheDocument();
	});

	it("shows New User button when canWrite is true", () => {
		render(<UsersHeader canWrite={true} onCreate={vi.fn()} />);

		expect(
			screen.getByRole("button", { name: /new user/i }),
		).toBeInTheDocument();
	});

	it("hides New User button when canWrite is false", () => {
		render(<UsersHeader canWrite={false} onCreate={vi.fn()} />);

		expect(
			screen.queryByRole("button", { name: /new user/i }),
		).not.toBeInTheDocument();
	});

	it("calls onCreate when New User button is clicked", async () => {
		const onCreate = vi.fn();
		render(<UsersHeader canWrite={true} onCreate={onCreate} />);

		await userEvent.click(screen.getByRole("button", { name: /new user/i }));

		expect(onCreate).toHaveBeenCalledTimes(1);
	});
});
