import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { Notification } from "@/lib/notification-api";
import {
	groupNotifications,
	HighlightedExcerpt,
	NotificationCard,
} from "./notification-card";

function make(overrides: Partial<Notification> = {}): Notification {
	return {
		id: "n1",
		type: "mentioned",
		actor_full_name: "Mina Park",
		actor_username: "mina",
		task_id: "t1",
		task_title: "Fix token refresh",
		task_number: 142,
		project_id: "p1",
		project_name: "Apollo",
		document_id: null,
		document_title: "",
		conversation_id: null,
		conversation_title: "",
		message: "",
		comment_id: "c1",
		mention_source: "comment",
		excerpt: "Hey @Lee can you look at this?",
		task_status_name: "In progress",
		task_status_color: "",
		sprint_id: "s1",
		sprint_name: "Sprint 12",
		read_at: null,
		created_at: "2026-01-01T00:00:00Z",
		...overrides,
	};
}

describe("groupNotifications", () => {
	it("collapses adjacent mentions on one task and keeps the rest apart", () => {
		const list = [
			make({ id: "a" }),
			make({ id: "b" }),
			make({ id: "c", type: "assigned", mention_source: "" }),
			make({ id: "d" }),
		];
		const groups = groupNotifications(list);
		expect(groups.map((g) => g.items.map((n) => n.id))).toEqual([
			["a", "b"],
			["c"],
			["d"],
		]);
	});
});

describe("NotificationCard", () => {
	it("shows project, sprint, task, status and the excerpt for a mention", async () => {
		const onOpen = vi.fn();
		const n = make();
		render(
			<NotificationCard group={{ head: n, items: [n] }} onOpen={onOpen} />,
		);

		expect(screen.getByText("Mina Park mentioned you")).toBeInTheDocument();
		expect(screen.getByText("Apollo")).toBeInTheDocument();
		expect(screen.getByText("Sprint 12")).toBeInTheDocument();
		expect(screen.getByText("#142 Fix token refresh")).toBeInTheDocument();
		expect(screen.getByText("In progress")).toBeInTheDocument();
		expect(screen.getByText("@Lee").tagName).toBe("MARK");

		await userEvent.click(screen.getByRole("button"));
		expect(onOpen).toHaveBeenCalledWith(n);
	});

	it("says Backlog when the task has no sprint and groups with a count", () => {
		const a = make({ id: "a", sprint_id: null, sprint_name: "" });
		const b = make({ id: "b", sprint_id: null, sprint_name: "" });
		render(
			<NotificationCard group={{ head: a, items: [a, b] }} onOpen={() => {}} />,
		);
		expect(screen.getByText("Backlog")).toBeInTheDocument();
		expect(
			screen.getByText("2 mentions in #142 — Fix token refresh"),
		).toBeInTheDocument();
	});
});

describe("HighlightedExcerpt", () => {
	it("marks every @mention and leaves the rest as text", () => {
		const { container } = render(
			<HighlightedExcerpt text="@a hi @b and e@mail" />,
		);
		expect(
			[...container.querySelectorAll("mark")].map((m) => m.textContent),
		).toEqual(["@a", "@b", "@mail"]);
	});
});
