import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { makeRole } from "@/test/render-with-queries";
import {
	enrichRoles,
	RoleBadge,
	RoleBadgeList,
	roleKind,
	toRoleBadgeData,
} from "./role-badge";

const names = (...ns: string[]) => ns.map((name) => ({ id: name, name }));

describe("roleKind", () => {
	it("lets the strongest fact about a role decide its kind", () => {
		expect(roleKind({ name: "a", full_access: true, is_system: true })).toBe(
			"fullAccess",
		);
		expect(roleKind({ name: "a", is_system: true, is_default: true })).toBe(
			"system",
		);
		expect(roleKind({ name: "a", is_default: true })).toBe("default");
		expect(roleKind({ name: "a" })).toBe("custom");
	});
});

describe("toRoleBadgeData / enrichRoles", () => {
	it("flags a role holding the * wildcard as full access", () => {
		expect(
			toRoleBadgeData(makeRole("r", "ROOT", { "*": true })).full_access,
		).toBe(true);
		expect(
			toRoleBadgeData(makeRole("r", "USER", { "tasks:read": true }))
				.full_access,
		).toBe(false);
	});

	it("adds what the roles list knows to a summary, and leaves unknown roles plain", () => {
		const root = {
			...makeRole("r1", "ROOT", { "*": true }),
			description: "Everything",
		};
		const [known, unknown] = enrichRoles(
			names("r1", "r2").map((r) =>
				r.id === "r1" ? { id: "r1", name: "ROOT" } : r,
			),
			[root],
		);

		expect(known).toMatchObject({
			name: "ROOT",
			description: "Everything",
			full_access: true,
		});
		expect(unknown).toEqual({ id: "r2", name: "r2" });
	});
});

describe("RoleBadge", () => {
	it("shows the name and the kind it is", () => {
		const { container } = render(
			<RoleBadge
				role={{ name: "ADMIN", is_system: true }}
				interactive={false}
			/>,
		);

		expect(screen.getByText("ADMIN")).toBeInTheDocument();
		expect(container.querySelector("[data-kind]")).toHaveAttribute(
			"data-kind",
			"system",
		);
	});

	it("truncates a long name instead of growing", () => {
		render(
			<RoleBadge
				role={{ name: "A_VERY_LONG_ROLE_NAME_INDEED" }}
				interactive={false}
			/>,
		);

		expect(screen.getByText("A_VERY_LONG_ROLE_NAME_INDEED")).toHaveClass(
			"truncate",
		);
	});

	it("shows the description in a tooltip on hover", async () => {
		render(
			<RoleBadge
				role={{
					name: "EDITOR",
					description: "Can edit tasks",
					is_default: true,
				}}
			/>,
		);

		await userEvent.hover(screen.getByText("EDITOR"));

		expect(await screen.findByText("Can edit tasks")).toBeInTheDocument();
		expect(screen.getByText("Default")).toBeInTheDocument();
	});
});

describe("RoleBadgeList", () => {
	it("says so when there are no roles", () => {
		render(<RoleBadgeList roles={[]} />);

		expect(screen.getByText("No roles")).toBeInTheDocument();
	});

	it("shows every role up to the limit, with no overflow", () => {
		render(<RoleBadgeList roles={names("A", "B")} max={2} />);

		expect(screen.getByText("A")).toBeInTheDocument();
		expect(screen.getByText("B")).toBeInTheDocument();
		expect(screen.queryByText(/^\+/)).not.toBeInTheDocument();
	});

	it("folds the rest into +N, and a popover lists all of them", async () => {
		render(<RoleBadgeList roles={names("A", "B", "C", "D")} max={2} />);

		expect(screen.queryByText("C")).not.toBeInTheDocument();
		const more = screen.getByRole("button", { name: "Show more roles (+2)" });
		expect(more).toHaveTextContent("+2");

		await userEvent.click(more);

		expect(await screen.findByText("All roles (4)")).toBeInTheDocument();
		expect(screen.getByText("C")).toBeInTheDocument();
		expect(screen.getByText("D")).toBeInTheDocument();
	});

	it("opens the overflow from the keyboard", async () => {
		render(<RoleBadgeList roles={names("A", "B", "C")} max={1} />);

		await userEvent.tab();
		expect(
			screen.getByRole("button", { name: /Show more roles/ }),
		).toHaveFocus();
		await userEvent.keyboard("{Enter}");

		expect(await screen.findByText("All roles (3)")).toBeInTheDocument();
	});

	it("renders the overflow as plain text when not interactive (inside a button)", () => {
		render(
			<RoleBadgeList
				roles={names("A", "B", "C")}
				max={1}
				interactive={false}
			/>,
		);

		expect(screen.queryByRole("button")).not.toBeInTheDocument();
		expect(screen.getByText("+2")).toHaveAttribute("title", "B, C");
	});
});
