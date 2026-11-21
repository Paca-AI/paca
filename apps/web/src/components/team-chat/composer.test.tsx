import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import "@/i18n";
import { Composer } from "./composer";

vi.mock("@/hooks/use-project-permissions", () => ({
	useProjectPermissions: () => ({
		hasProjectPermission: () => false,
		isLoading: false,
	}),
}));
vi.mock("@/lib/mention-api", () => ({
	searchMentionableMembers: vi.fn(async () => []),
	searchMentionableTasks: vi.fn(async () => []),
	searchMentionableDocuments: vi.fn(async () => []),
}));

const draft = (text: string) =>
	JSON.stringify([
		{ type: "paragraph", content: [{ type: "text", text, styles: {} }] },
	]);

function box(id: string) {
	return (
		<QueryClientProvider client={new QueryClient()}>
			<Composer draftId={id} placeholder="Message" onSend={async () => {}} />
		</QueryClientProvider>
	);
}

describe("Composer drafts", () => {
	beforeEach(() => {
		localStorage.clear();
		localStorage.setItem("paca:chat-draft:thread:a", draft("alpha draft"));
		localStorage.setItem("paca:chat-draft:thread:b", draft("beta draft"));
	});

	it("shows the new conversation's draft, not the previous one's, when the draft id changes", async () => {
		const { rerender, container } = render(box("thread:a"));
		await waitFor(() => expect(container.textContent).toContain("alpha draft"));
		rerender(box("thread:b"));
		await waitFor(() => expect(container.textContent).toContain("beta draft"));
		expect(container.textContent).not.toContain("alpha draft");
		expect(screen.getByRole("button", { name: /send/i })).toBeEnabled();
	});
});
