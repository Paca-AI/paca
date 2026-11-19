import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Document } from "@/lib/doc-api";

const h = vi.hoisted(() => ({
	get: vi.fn(),
	post: vi.fn(),
	patch: vi.fn(),
	del: vi.fn(),
	actions: new Set<string>(),
}));

vi.mock("@/lib/api-client", () => ({
	apiClient: {
		instance: { get: h.get, post: h.post, patch: h.patch, delete: h.del },
	},
}));
vi.mock("@/hooks/use-project-permissions", () => ({
	useProjectPermissions: () => ({
		hasProjectPermission: (a: string) => h.actions.has(a),
		isLoading: false,
	}),
}));
// The editor itself is the docs route's, covered by its own tests.
vi.mock("@/components/projects/docs/document-pane", () => ({
	DocumentPane: ({ docId }: { docId: string }) => (
		<div data-testid="document-pane">{docId}</div>
	),
}));

import { moveNote, NoteView } from "./note-view";

const note = (id: string, title: string, position = 0): Document => ({
	id,
	view_id: "v1",
	title,
	content: null,
	position,
	created_at: "2026-11-01T00:00:00Z",
	updated_at: "2026-11-01T00:00:00Z",
	source_locked: false,
});

let notes: Document[] = [];
const envelope = (data: unknown) => ({ data: { data } });

function renderView() {
	const client = new QueryClient({
		defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
	});
	return render(
		<QueryClientProvider client={client}>
			<NoteView projectId="p1" viewId="v1" />
		</QueryClientProvider>,
	);
}

beforeEach(() => {
	localStorage.clear();
	h.actions = new Set(["docs:read", "docs:write"]);
	notes = [note("n1", "Sprint goals"), note("n2", "Retro"), note("n3", "Demo")];
	h.get.mockReset();
	h.post.mockReset();
	h.patch.mockReset();
	h.del.mockReset();
	h.get.mockImplementation(async () =>
		envelope({ items: notes, next_cursor: null }),
	);
	h.patch.mockImplementation(async (_url: string, body: Partial<Document>) =>
		envelope(body),
	);
	h.del.mockResolvedValue({});
});

async function openActions(
	user: ReturnType<typeof userEvent.setup>,
	title: string,
) {
	await user.click(
		await screen.findByRole("button", { name: `Actions for ${title}` }),
	);
}

describe("moveNote", () => {
	it("swaps a note with its neighbour and ignores moves past the ends", () => {
		const list = [note("a", "A"), note("b", "B"), note("c", "C")];
		expect(moveNote(list, 0, 1).map((n) => n.id)).toEqual(["b", "a", "c"]);
		expect(moveNote(list, 2, -1).map((n) => n.id)).toEqual(["a", "c", "b"]);
		expect(moveNote(list, 0, -1)).toBe(list);
		expect(moveNote(list, 2, 1)).toBe(list);
	});
});

describe("NoteView", () => {
	it("lists the view's notes and opens the first, then the one clicked", async () => {
		const user = userEvent.setup();
		renderView();

		expect(await screen.findByTestId("document-pane")).toHaveTextContent("n1");
		expect(h.get).toHaveBeenCalledWith("/projects/p1/docs", {
			params: { page_size: 200, view_id: "v1" },
		});
		const list = screen.getByRole("complementary", { name: "Notes" });
		expect(
			within(list)
				.getAllByRole("button")
				.map((b) => b.textContent)
				.filter((t) => t),
		).toEqual(["New note", "Sprint goals", "Retro", "Demo"]);

		await user.click(screen.getByRole("button", { name: "Retro" }));
		expect(screen.getByTestId("document-pane")).toHaveTextContent("n2");
		// The open note is remembered for the next visit.
		expect(localStorage.getItem("paca:note-view:v1:active")).toBe("n2");
	});

	it("creates a note in the view and lets it be named straight away", async () => {
		const user = userEvent.setup();
		h.post.mockImplementation(async (_url: string, body: { title: string }) => {
			const created = note("n4", body.title);
			notes = [...notes, created];
			return envelope(created);
		});
		renderView();

		await user.click(await screen.findByRole("button", { name: "New note" }));
		expect(h.post).toHaveBeenCalledWith("/projects/p1/docs", {
			title: "Untitled",
			view_id: "v1",
		});
		const input = await screen.findByRole("textbox", { name: "Note title" });
		expect(screen.getByTestId("document-pane")).toHaveTextContent("n4");

		await user.clear(input);
		await user.type(input, "Release checklist{Enter}");
		await waitFor(() =>
			expect(h.patch).toHaveBeenCalledWith("/projects/p1/docs/n4", {
				title: "Release checklist",
			}),
		);
		expect(h.patch).toHaveBeenCalledTimes(1);
	});

	it("keeps the typed title when a rename is refused", async () => {
		const user = userEvent.setup();
		h.patch.mockRejectedValueOnce({
			isAxiosError: true,
			response: {
				status: 400,
				data: { error_code: "DOC_TITLE_INVALID", error: "bad title" },
			},
		});
		renderView();

		await openActions(user, "Retro");
		await user.click(await screen.findByRole("menuitem", { name: "Rename" }));
		const input = await screen.findByRole("textbox", { name: "Note title" });
		await user.clear(input);
		await user.type(input, "Q1/Q2{Enter}");

		expect(await screen.findByRole("alert")).toHaveTextContent(
			"A title can't be empty or contain “/”.",
		);
		expect(input).toHaveValue("Q1/Q2");
		await user.clear(input);
		await user.type(input, "Q1 and Q2{Enter}");
		await waitFor(() =>
			expect(h.patch).toHaveBeenLastCalledWith("/projects/p1/docs/n2", {
				title: "Q1 and Q2",
			}),
		);
	});

	it("reorders by renumbering the notes whose position changes", async () => {
		const user = userEvent.setup();
		renderView();

		await openActions(user, "Sprint goals");
		await user.click(
			await screen.findByRole("menuitem", { name: "Move down" }),
		);

		await waitFor(() => expect(h.patch).toHaveBeenCalledTimes(2));
		// New order: Retro (0, unchanged), Sprint goals (1), Demo (2).
		expect(h.patch).toHaveBeenCalledWith("/projects/p1/docs/n1", {
			position: 1,
		});
		expect(h.patch).toHaveBeenCalledWith("/projects/p1/docs/n3", {
			position: 2,
		});
	});

	it("compares a second move with the positions the first one saved", async () => {
		const user = userEvent.setup();
		// The list is not refetched in between: every PATCH stays pending.
		let release = () => {};
		const pending = new Promise<void>((resolve) => {
			release = resolve;
		});
		h.patch.mockImplementation(
			async (_url: string, body: Partial<Document>) => {
				await pending;
				return envelope(body);
			},
		);
		renderView();

		await openActions(user, "Sprint goals");
		await user.click(
			await screen.findByRole("menuitem", { name: "Move down" }),
		);
		// Retro 0, Sprint goals 1, Demo 2. Now Retro moves down.
		await openActions(user, "Retro");
		await user.click(
			await screen.findByRole("menuitem", { name: "Move down" }),
		);
		release();

		// Sprint goals 0, Retro 1, Demo stays 2: Retro and Sprint goals are
		// saved again, Demo is not.
		await waitFor(() => expect(h.patch).toHaveBeenCalledTimes(4));
		expect(h.patch.mock.calls.slice(2)).toEqual(
			expect.arrayContaining([
				["/projects/p1/docs/n1", { position: 0 }],
				["/projects/p1/docs/n2", { position: 1 }],
			]),
		);
	});

	it("disables moves past either end", async () => {
		const user = userEvent.setup();
		renderView();

		await openActions(user, "Sprint goals");
		expect(
			await screen.findByRole("menuitem", { name: "Move up" }),
		).toHaveAttribute("aria-disabled", "true");
	});

	it("deletes a note after confirming", async () => {
		const user = userEvent.setup();
		renderView();

		await openActions(user, "Retro");
		await user.click(await screen.findByRole("menuitem", { name: "Delete" }));
		const dialog = await screen.findByRole("dialog");
		expect(dialog).toHaveTextContent("“Retro” will be deleted for everyone.");
		notes = notes.filter((n) => n.id !== "n2");
		await user.click(within(dialog).getByRole("button", { name: "Delete" }));

		await waitFor(() =>
			expect(h.del).toHaveBeenCalledWith("/projects/p1/docs/n2"),
		);
		await waitFor(() =>
			expect(screen.queryByRole("button", { name: "Retro" })).toBeNull(),
		);
	});

	it("is read-only without docs:write", async () => {
		h.actions = new Set(["docs:read"]);
		renderView();

		expect(await screen.findByRole("button", { name: "Retro" })).toBeVisible();
		expect(screen.queryByRole("button", { name: "New note" })).toBeNull();
		expect(screen.queryByRole("button", { name: /^Actions for/ })).toBeNull();
	});

	it("says so, and loads nothing, without docs:read", async () => {
		h.actions = new Set();
		renderView();

		expect(
			await screen.findByText("You can't see these notes"),
		).toBeInTheDocument();
		expect(h.get).not.toHaveBeenCalled();
	});

	it("invites writing the first note in an empty view", async () => {
		notes = [];
		renderView();

		expect(await screen.findByText("No notes yet")).toBeInTheDocument();
		expect(screen.getAllByRole("button", { name: "New note" })).toHaveLength(2);
		expect(screen.queryByTestId("document-pane")).toBeNull();
	});
});
