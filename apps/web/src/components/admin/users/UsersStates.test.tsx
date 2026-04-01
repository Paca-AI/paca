import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import {
	EmptyUsersState,
	UsersErrorState,
	UsersNoPermissionState,
} from "./UsersStates";

describe("EmptyUsersState", () => {
	it("shows empty message", () => {
		render(<EmptyUsersState canWrite={false} onCreate={vi.fn()} />);

		expect(screen.getByText(/no users found/i)).toBeInTheDocument();
		expect(
			screen.getByText(/create your first user to get started/i),
		).toBeInTheDocument();
	});

	it("shows Create user button when canWrite is true", () => {
		render(<EmptyUsersState canWrite={true} onCreate={vi.fn()} />);

		expect(
			screen.getByRole("button", { name: /create user/i }),
		).toBeInTheDocument();
	});

	it("hides Create user button when canWrite is false", () => {
		render(<EmptyUsersState canWrite={false} onCreate={vi.fn()} />);

		expect(
			screen.queryByRole("button", { name: /create user/i }),
		).not.toBeInTheDocument();
	});

	it("calls onCreate when Create user button is clicked", async () => {
		const onCreate = vi.fn();
		render(<EmptyUsersState canWrite={true} onCreate={onCreate} />);

		await userEvent.click(screen.getByRole("button", { name: /create user/i }));

		expect(onCreate).toHaveBeenCalledTimes(1);
	});
});

describe("UsersErrorState", () => {
	it("renders error message", () => {
		render(<UsersErrorState />);

		expect(screen.getByText(/failed to load users/i)).toBeInTheDocument();
		expect(screen.getByText(/please refresh/i)).toBeInTheDocument();
	});
});

describe("UsersNoPermissionState", () => {
	it("renders no-permission message", () => {
		render(<UsersNoPermissionState />);

		expect(
			screen.getByText(/you don't have permission to view users/i),
		).toBeInTheDocument();
	});
});
