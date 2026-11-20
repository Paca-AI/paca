import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Document } from "@/lib/doc-api";

const h = vi.hoisted(() => ({
	get: vi.fn(),
	post: vi.fn(),
	actions: new Set<string>(),
}));

vi.mock("@/lib/api-client", () => ({
	apiClient: { instance: { get: h.get, post: h.post } },
}));
vi.mock("@/hooks/use-project-permissions", () => ({
	useProjectPermissions: () => ({
		hasProjectPermission: (a: string) => h.actions.has(a),
		isLoading: false,
	}),
}));
// The editor itself is the docs route's, covered by its own tests.
vi.mock("@/components/projects/docs/document-pane", () => ({
	DocumentPane: ({
		docId,
		headerSlot,
	}: {
		docId: string;
		headerSlot?: HTMLElement | null;
	}) => (
		<div data-testid="document-pane" data-header-slot={String(!!headerSlot)}>
			{docId}
		</div>
	),
}));

import { NoteView } from "./note-view";

const note = (id: string, position = 0): Document => ({
	id,
	view_id: "v1",
	title: "Untitled",
	content: null,
	position,
	created_at: "2026-11-01T00:00:00Z",
	updated_at: "2026-11-01T00:00:00Z",
	source_locked: false,
});

let notes: Document[] = [];
const envelope = (data: unknown) => ({ data: { data } });

const slot = document.createElement("div");

function renderView() {
	const client = new QueryClient({
		defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
	});
	return render(
		<QueryClientProvider client={client}>
			<NoteView projectId="p1" viewId="v1" headerSlot={slot} />
		</QueryClientProvider>,
	);
}

beforeEach(() => {
	h.actions = new Set(["docs:read", "docs:write"]);
	notes = [];
	h.get.mockReset();
	h.post.mockReset();
	h.get.mockImplementation(async () =>
		envelope({ items: notes, next_cursor: null }),
	);
	h.post.mockImplementation(async (_url: string, body: { title: string }) => {
		const created = note("n1");
		notes = [created];
		return envelope({ ...created, title: body.title });
	});
});

describe("NoteView", () => {
	it("opens the view's document without a title, and shows no notes list", async () => {
		notes = [note("n1")];
		renderView();

		const pane = await screen.findByTestId("document-pane");
		expect(pane).toHaveTextContent("n1");
		expect(pane).toHaveAttribute("data-header-slot", "true");
		expect(h.get).toHaveBeenCalledWith("/projects/p1/docs", {
			params: { page_size: 200, view_id: "v1" },
		});
		expect(screen.queryByRole("complementary")).toBeNull();
		expect(screen.queryByRole("button", { name: "New note" })).toBeNull();
		expect(h.post).not.toHaveBeenCalled();
	});

	it("shows only the first document when the view holds several", async () => {
		notes = [note("n1"), note("n2", 1)];
		renderView();

		expect(await screen.findByTestId("document-pane")).toHaveTextContent("n1");
		expect(screen.getAllByTestId("document-pane")).toHaveLength(1);
	});

	it("creates the document once when an empty view is opened", async () => {
		renderView();

		expect(await screen.findByTestId("document-pane")).toHaveTextContent("n1");
		expect(h.post).toHaveBeenCalledTimes(1);
		expect(h.post).toHaveBeenCalledWith("/projects/p1/docs", {
			title: "Untitled",
			view_id: "v1",
		});
	});

	it("lets a failed creation be retried", async () => {
		const user = userEvent.setup();
		h.post.mockRejectedValueOnce(new Error("boom"));
		renderView();

		expect(await screen.findByRole("alert")).toHaveTextContent(
			"Couldn't create the document.",
		);
		await user.click(screen.getByRole("button", { name: "Try again" }));
		expect(await screen.findByTestId("document-pane")).toHaveTextContent("n1");
		expect(h.post).toHaveBeenCalledTimes(2);
	});

	it("shows the document someone else created first", async () => {
		h.post.mockImplementationOnce(async () => {
			notes = [note("n9")];
			throw {
				isAxiosError: true,
				response: {
					status: 409,
					data: { error_code: "DOC_NOTE_VIEW_TAKEN", error: "taken" },
				},
			};
		});
		renderView();

		expect(await screen.findByTestId("document-pane")).toHaveTextContent("n9");
		expect(screen.queryByRole("alert")).toBeNull();
		expect(h.post).toHaveBeenCalledTimes(1);
	});

	it("creates nothing and says so in an empty view without docs:write", async () => {
		h.actions = new Set(["docs:read"]);
		renderView();

		expect(
			await screen.findByText("This view has no document yet."),
		).toBeInTheDocument();
		await waitFor(() => expect(h.get).toHaveBeenCalled());
		expect(h.post).not.toHaveBeenCalled();
	});

	it("says so, and loads nothing, without docs:read", async () => {
		h.actions = new Set();
		renderView();

		expect(
			await screen.findByText("You can't see this document"),
		).toBeInTheDocument();
		expect(h.get).not.toHaveBeenCalled();
		expect(h.post).not.toHaveBeenCalled();
	});
});
