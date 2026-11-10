import { render, waitFor } from "@testing-library/react";
import { StrictMode } from "react";
import { describe, expect, it, vi } from "vitest";
import {
	type CollabSession,
	CollabSession as Session,
} from "@/lib/collab-session";
import { DocEditor } from "./doc-editor";

vi.mock("@/lib/socket-client", () => ({ connectSocket: vi.fn() }));

const sock = {
	connected: true,
	on: () => sock,
	off: () => sock,
	emit: () => true,
	timeout: () => sock,
};

let session: CollabSession;
vi.mock("@/hooks/use-collab-session", () => ({
	useCollabSession: () => ({ phase: "ready", session, status: "ready" }),
}));
vi.mock("@/hooks/use-theme-mode", () => ({
	useThemeMode: () => ({ resolvedMode: "light" }),
}));

const blocks = [
	{
		type: "paragraph",
		props: {},
		content: [{ type: "text", text: "Stored content", styles: {} }],
		children: [],
	},
];

describe("DocEditor collaboration seeding", () => {
	it("writes the stored content into the shared doc under StrictMode", async () => {
		session = new Session(
			sock as never,
			{ projectId: "p", kind: "doc", id: "d" },
			{ name: "A", color: "#fff" },
		);
		session.seed = true;
		render(
			<StrictMode>
				<DocEditor content={blocks} editable projectId="p" docId="d" />
			</StrictMode>,
		);
		await waitFor(
			() => expect(session.fragment.toString()).toContain("Stored content"),
			{ timeout: 3000 },
		);
	});
});
