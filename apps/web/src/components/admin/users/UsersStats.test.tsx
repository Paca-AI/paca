import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { UsersStats } from "./UsersStats";

describe("UsersStats", () => {
	it("shows total user count", () => {
		render(<UsersStats total={5} mustChangePasswordCount={0} />);

		expect(screen.getByText("5")).toBeInTheDocument();
		expect(screen.getByText(/users in system/i)).toBeInTheDocument();
	});

	it("uses singular 'user' when total is 1", () => {
		render(<UsersStats total={1} mustChangePasswordCount={0} />);

		expect(screen.getByText(/user in system/i)).toBeInTheDocument();
		expect(screen.queryByText(/users in system/i)).not.toBeInTheDocument();
	});

	it("hides must-change-password section when count is 0", () => {
		render(<UsersStats total={3} mustChangePasswordCount={0} />);

		expect(screen.queryByText(/must change password/i)).not.toBeInTheDocument();
	});

	it("shows must-change-password count when greater than 0", () => {
		render(<UsersStats total={4} mustChangePasswordCount={2} />);

		expect(screen.getByText("2")).toBeInTheDocument();
		expect(screen.getByText(/must change password/i)).toBeInTheDocument();
	});
});
