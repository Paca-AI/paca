import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const h = vi.hoisted(() => ({
	get: vi.fn(),
	post: vi.fn(),
	del: vi.fn(),
	follow: vi.fn(),
}));
const perms = vi.hoisted(() => ({ workspace: true, project: true }));

vi.mock("@/hooks/use-permissions", () => ({
	usePermissions: () => ({
		hasPermission: (a: string) =>
			a === "git.identities:manage" ? perms.workspace : false,
	}),
}));
vi.mock("@/hooks/use-effective-actions", () => ({
	useEffectiveActions: () => ({
		has: (a: string) =>
			a === "git.connections:manage" || a === "git:write"
				? perms.project
				: false,
	}),
}));
vi.mock("@/lib/api-client", () => ({
	apiClient: { instance: { get: h.get, post: h.post, delete: h.del } },
}));
vi.mock("@/lib/git-api", async (orig) => ({
	...(await orig<typeof import("@/lib/git-api")>()),
	followGitStep: h.follow,
}));

import type { GitProvider } from "@/lib/git-api";
import { GitConnections } from "./GitConnections";

const PROJECT = "p1";
const github: GitProvider = {
	id: "com.paca.github",
	display_name: "GitHub",
	hosts: ["github.com"],
	self_hosted_hosts: false,
	terms: {
		change_request: "Pull request",
		change_request_plural: "Pull requests",
		repository: "Repository",
	},
	capabilities: {
		multiple_identities: true,
		access_grants: true,
		identity_kinds: ["app", "token"],
		kinds: [
			{ kind: "app", access_grants: true },
			{ kind: "token", access_grants: false },
		],
	},
};
const gitlab: GitProvider = {
	...github,
	id: "com.paca.gitlab",
	display_name: "GitLab",
	hosts: ["gitlab.com"],
	self_hosted_hosts: true,
	capabilities: {
		multiple_identities: true,
		access_grants: false,
		identity_kinds: ["bot_user"],
	},
};
const shared = {
	id: "w1",
	provider_id: "com.paca.github",
	host: "github.com",
	kind: "app",
	scope: "shared",
	project_id: null,
	handle: "paca-bot",
	display_name: "Paca",
	expires_at: null,
	created_at: "2026-01-01T00:00:00Z",
};
const own = {
	...shared,
	id: "p-1",
	kind: "token",
	scope: "project",
	project_id: PROJECT,
	handle: "ci-bot",
	display_name: "CI bot",
};

let connections: unknown[] = [];

function renderConnections(providers: GitProvider[] = [github]) {
	const client = new QueryClient({
		defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
	});
	return render(
		<QueryClientProvider client={client}>
			<GitConnections projectId={PROJECT} providers={providers} />
		</QueryClientProvider>,
	);
}

describe("GitConnections", () => {
	beforeEach(() => {
		vi.resetAllMocks();
		window.sessionStorage.clear();
		perms.workspace = true;
		perms.project = true;
		connections = [shared, own];
		h.follow.mockReturnValue(true);
		h.get.mockImplementation(async (url: string) => {
			if (url.endsWith("/git/connections"))
				return { data: { data: connections } };
			throw new Error(`unexpected GET ${url}`);
		});
	});

	it("removes a project's own connection through the project route", async () => {
		const user = userEvent.setup();
		h.del.mockResolvedValue({});
		renderConnections();
		const row = (await screen.findByText("CI bot")).closest("li");
		if (!row) throw new Error("row not found");
		await user.click(within(row).getByRole("button", { name: "Remove" }));
		await user.click(
			await screen.findByRole("button", { name: "Remove connection" }),
		);
		await waitFor(() =>
			expect(h.del).toHaveBeenCalledWith(
				`/projects/${PROJECT}/git/connections/p-1`,
			),
		);
	});

	it("warns which repositories lose their credential before removing a connection", async () => {
		const user = userEvent.setup();
		connections = [{ ...shared, repository_count: 3 }, own];
		h.get.mockImplementation(async (url: string) => {
			if (url.endsWith("/git/connections"))
				return { data: { data: connections } };
			if (url.endsWith("/git/repositories"))
				return {
					data: {
						data: [
							{ id: "r1", full_path: "acme/web", identity_id: "w1" },
							{ id: "r2", full_path: "acme/api", identity_id: "p-1" },
						],
					},
				};
			throw new Error(`unexpected GET ${url}`);
		});
		renderConnections();
		const row = (await screen.findByText("Paca")).closest("li");
		if (!row) throw new Error("row not found");
		await user.click(within(row).getByRole("button", { name: "Remove" }));
		const note = await screen.findByRole("note");
		expect(note).toHaveTextContent("Repositories that use it (3):");
		expect(note).toHaveTextContent("acme/web");
		expect(note).not.toHaveTextContent("acme/api"); // another connection's
		expect(note).toHaveTextContent("Plus 2 in other projects.");
		expect(note).toHaveTextContent(/Reconnect each one/);
	});

	it("shows no warning for a connection nothing uses", async () => {
		const user = userEvent.setup();
		renderConnections();
		const row = (await screen.findByText("CI bot")).closest("li");
		if (!row) throw new Error("row not found");
		await user.click(within(row).getByRole("button", { name: "Remove" }));
		await screen.findByRole("button", { name: "Remove connection" });
		expect(screen.queryByRole("note")).toBeNull();
	});

	it("removes a shared connection through the workspace route", async () => {
		const user = userEvent.setup();
		h.del.mockResolvedValue({});
		renderConnections();
		const row = (await screen.findByText("Paca")).closest("li");
		if (!row) throw new Error("row not found");
		await user.click(within(row).getByRole("button", { name: "Remove" }));
		await user.click(
			await screen.findByRole("button", { name: "Remove connection" }),
		);
		await waitFor(() =>
			expect(h.del).toHaveBeenCalledWith("/git/identities/w1"),
		);
	});

	it("offers removal only for what the user may manage", async () => {
		perms.workspace = false; // may manage the project's own, not the shared one
		renderConnections();
		const shared = (await screen.findByText("Paca")).closest("li");
		const own = screen.getByText("CI bot").closest("li");
		if (!shared || !own) throw new Error("rows not found");
		expect(within(shared).queryByRole("button", { name: "Remove" })).toBeNull();
		expect(within(own).getByRole("button", { name: "Remove" })).toBeEnabled();
	});

	it("adds a project-only access token: kind and scope go to the project route", async () => {
		const user = userEvent.setup();
		h.post.mockImplementation(async (url: string) => {
			if (url.endsWith("/connections/setup"))
				return {
					data: {
						data: {
							kind: "token_form",
							state: "st1",
							prompt: "Paste a token",
							form: [
								{ name: "token", label: "Token", secret: true, required: true },
							],
						},
					},
				};
			return { data: { data: { identity: own, next: null, done: true } } };
		});
		renderConnections();
		await user.click(await screen.findByRole("button", { name: "Add GitHub" }));
		await user.click(screen.getByRole("radio", { name: /Access token/ }));
		expect(
			screen.getByRole("radio", { name: /This project only/ }),
		).toBeChecked();
		await user.click(screen.getByRole("button", { name: "Next" }));

		await waitFor(() =>
			expect(h.post).toHaveBeenCalledWith(
				`/projects/${PROJECT}/git/connections/setup`,
				expect.objectContaining({
					provider_id: "com.paca.github",
					host: "github.com",
					params: { kind: "token" },
				}),
			),
		);
		await user.type(await screen.findByLabelText("Token"), "ghp_secret");
		await user.click(screen.getByRole("button", { name: "Save" }));
		await waitFor(() =>
			expect(h.post).toHaveBeenLastCalledWith(
				`/projects/${PROJECT}/git/connections/setup/complete`,
				{ state: "st1", params: { token: "ghp_secret" } },
			),
		);
	});

	it("adds a workspace-wide App through the workspace route and remembers the flow", async () => {
		const user = userEvent.setup();
		h.post.mockResolvedValue({
			data: {
				data: {
					kind: "form_post",
					url: "https://github.com/settings/apps/new",
					state: "g9",
				},
			},
		});
		renderConnections();
		await user.click(await screen.findByRole("button", { name: "Add GitHub" }));
		await user.click(screen.getByRole("radio", { name: /All projects/ }));
		await user.click(
			screen.getByRole("button", { name: /Continue to github.com/ }),
		);
		await waitFor(() => expect(h.follow).toHaveBeenCalled());
		expect(h.post).toHaveBeenCalledWith(
			"/git/identities/setup",
			expect.objectContaining({ params: { kind: "app" } }),
		);
		expect(window.sessionStorage.getItem("paca.git.flow.g9")).toBe(
			JSON.stringify({ kind: "identity" }),
		);
	});

	it("remembers a project connection's flow so the callback completes the right one", async () => {
		const user = userEvent.setup();
		h.post.mockResolvedValue({
			data: {
				data: { kind: "form_post", url: "https://github.com/x", state: "g8" },
			},
		});
		renderConnections();
		await user.click(await screen.findByRole("button", { name: "Add GitHub" }));
		await user.click(
			screen.getByRole("button", { name: /Continue to github.com/ }),
		);
		await waitFor(() => expect(h.follow).toHaveBeenCalled());
		expect(
			JSON.parse(window.sessionStorage.getItem("paca.git.flow.g8") ?? "{}"),
		).toEqual({
			kind: "project-identity",
			projectId: PROJECT,
		});
	});

	it("disables the scopes the user may not use", async () => {
		perms.workspace = false;
		const user = userEvent.setup();
		renderConnections();
		await user.click(await screen.findByRole("button", { name: "Add GitHub" }));
		expect(screen.getByRole("radio", { name: /All projects/ })).toBeDisabled();
		expect(
			screen.getByRole("radio", { name: /This project only/ }),
		).toBeEnabled();
	});

	it("asks only what a single-kind provider needs", async () => {
		const user = userEvent.setup();
		connections = [];
		renderConnections([gitlab]);
		await user.click(await screen.findByRole("button", { name: "Add GitLab" }));
		expect(screen.queryByRole("radio", { name: /Access token/ })).toBeNull(); // no kind to pick
		expect(screen.getByLabelText("Host")).toHaveValue("gitlab.com"); // self-hosted
		expect(screen.queryByLabelText(/App name/i)).toBeNull(); // not an App
	});
});
