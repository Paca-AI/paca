import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const h = vi.hoisted(() => ({
	get: vi.fn(),
	post: vi.fn(),
	del: vi.fn(),
	put: vi.fn(),
	patch: vi.fn(),
	follow: vi.fn(),
}));

vi.mock("@tanstack/react-router", async () => {
	const React = await import("react");
	return {
		Link: ({
			to,
			children,
			...rest
		}: {
			to: string;
			children?: React.ReactNode;
		}) => React.createElement("a", { href: to, ...rest }, children),
	};
});

// The query options inside git-api call its own helpers, which a module mock
// cannot replace, so the HTTP client is mocked instead.
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
	apiClient: {
		instance: {
			get: h.get,
			post: h.post,
			delete: h.del,
			put: h.put,
			patch: h.patch,
		},
	},
}));
vi.mock("@/lib/git-api", async (orig) => ({
	...(await orig<typeof import("@/lib/git-api")>()),
	followGitStep: h.follow,
}));

import { GitSettings } from "./GitSettings";

const PROJECT = "p1";
const github = {
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
const connection = {
	id: "i1",
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
const repo = {
	id: "r1",
	provider_id: "com.paca.github",
	host: "github.com",
	full_path: "acme/web",
	default_branch: "main",
	clone_url: "https://github.com/acme/web.git",
	description: "",
	identity_id: "i1",
	created_at: "2026-01-01T00:00:00Z",
};

let providers: unknown[] = [github];
let repos: unknown[] = [];
let connections: unknown[] = [connection];
let settings: Record<string, unknown> = {};
const agents = [
	{ id: "a1", name: "Dev", handle: "dev" },
	{ id: "a2", name: "Reviewer", handle: "reviewer" },
];

function apiFail(code: string, details?: Record<string, string>) {
	return Promise.reject({
		response: {
			status: 409,
			data: { error_code: code, error_details: details },
		},
	});
}

function renderPanel(canWrite = true) {
	const client = new QueryClient({
		defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
	});
	return render(
		<QueryClientProvider client={client}>
			<GitSettings projectId={PROJECT} canWrite={canWrite} />
		</QueryClientProvider>,
	);
}

describe("GitSettings", () => {
	beforeEach(() => {
		vi.resetAllMocks();
		window.sessionStorage.clear();
		providers = [github];
		repos = [];
		connections = [connection];
		perms.workspace = true;
		perms.project = true;
		settings = {
			default_responder_agent_id: null,
			paused: false,
			min_actor_permission: "write",
		};
		h.put.mockImplementation(async (_url: string, body: unknown) => ({
			data: { data: body },
		}));
		h.follow.mockReturnValue(true);
		h.get.mockImplementation(async (url: string) => {
			if (url.endsWith("/git/providers")) return { data: { data: providers } };
			if (url.endsWith("/git/repositories")) return { data: { data: repos } };
			if (url.endsWith("/git/connections"))
				return { data: { data: connections } };
			if (url.endsWith("/git/settings")) return { data: { data: settings } };
			if (url.endsWith("/agents"))
				return {
					data: { data: { items: agents, page_size: 50, next_cursor: null } },
				};
			throw new Error(`unexpected GET ${url}`);
		});
	});

	it("asks for a connection before repositories can be linked", async () => {
		connections = [];
		renderPanel();
		expect(
			await screen.findByText(
				"Add a connection first, then link repositories through it.",
			),
		).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: "Link" }),
		).not.toBeInTheDocument();
		// Whoever may add one is offered the way to do it.
		expect(
			screen.getAllByRole("button", { name: "Add GitHub" }).length,
		).toBeGreaterThan(0);
	});

	it("tells someone who cannot add connections to ask for one", async () => {
		connections = [];
		perms.workspace = false;
		perms.project = false;
		renderPanel();
		expect(
			await screen.findByText(/Ask someone who can manage Git connections/i),
		).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: "Add GitHub" }),
		).not.toBeInTheDocument();
	});

	it("lists the project's connections, shared and its own", async () => {
		connections = [
			connection,
			{
				...connection,
				id: "i2",
				kind: "token",
				scope: "project",
				project_id: PROJECT,
				handle: "ci-bot",
				display_name: "CI bot",
			},
		];
		renderPanel();
		expect(await screen.findByText("Paca")).toBeInTheDocument();
		expect(screen.getByText("CI bot")).toBeInTheDocument();
		expect(screen.getByText("All projects")).toBeInTheDocument();
		expect(screen.getByText("This project")).toBeInTheDocument();
	});

	it("lets the user choose among several connections when linking", async () => {
		const user = userEvent.setup();
		connections = [
			connection,
			{
				...connection,
				id: "i2",
				scope: "project",
				project_id: PROJECT,
				handle: "second",
				display_name: "Second org",
			},
		];
		h.post.mockImplementation(async () => {
			repos = [repo];
			return { data: { data: repo } };
		});
		renderPanel();
		await user.selectOptions(await screen.findByLabelText("Connection"), "i2");
		await user.type(screen.getByLabelText("Repository"), "acme/web");
		await user.click(screen.getByRole("button", { name: "Link" }));
		await waitFor(() =>
			expect(h.post).toHaveBeenCalledWith(
				`/projects/${PROJECT}/git/repositories`,
				{
					provider_id: "com.paca.github",
					path_or_url: "acme/web",
					identity_id: "i2",
				},
			),
		);
	});

	it("offers to reconnect a repository whose connection was deleted", async () => {
		const user = userEvent.setup();
		repos = [{ ...repo, identity_id: null }];
		h.post.mockImplementation(async () => {
			repos = [repo];
			return { data: { data: repo } };
		});
		renderPanel();
		expect(
			await screen.findByText("This repository has no connection"),
		).toBeInTheDocument();
		await user.click(screen.getByRole("button", { name: "Reconnect" }));
		await waitFor(() =>
			expect(h.post).toHaveBeenCalledWith(
				`/projects/${PROJECT}/git/repositories/r1/reconnect`,
				{},
			),
		);
		await waitFor(() =>
			expect(
				screen.queryByText("This repository has no connection"),
			).toBeNull(),
		);
	});

	it("explains when no connection can see the repository", async () => {
		const user = userEvent.setup();
		repos = [{ ...repo, identity_id: null }];
		h.post.mockImplementation(() => apiFail("GIT_REPOSITORY_NOT_VISIBLE"));
		renderPanel();
		await user.click(await screen.findByRole("button", { name: "Reconnect" }));
		expect(await screen.findByRole("alert")).toHaveTextContent(
			/None of the project's connections can see this repository/,
		);
	});

	it("shows what the platform said under the generic message, as plain text", async () => {
		const user = userEvent.setup();
		repos = [{ ...repo, identity_id: null }];
		const said = "The token covers only acme/ui. <b>Add</b> one for acme/api.";
		h.post.mockImplementation(() =>
			apiFail("GIT_REPOSITORY_NOT_VISIBLE", { provider_message: said }),
		);
		renderPanel();
		await user.click(await screen.findByRole("button", { name: "Reconnect" }));
		const alert = await screen.findByRole("alert");
		expect(alert).toHaveTextContent(/None of the project's connections/);
		expect(alert).toHaveTextContent(`Details from the platform: ${said}`);
		expect(alert.querySelector("b")).toBeNull();
	});

	it("shows why the webhook still could not be registered", async () => {
		const user = userEvent.setup();
		repos = [
			{
				...repo,
				webhook_url: "https://paca.example.com/api/v1/git/webhooks/k",
			},
		];
		h.post.mockImplementation(() =>
			apiFail("GIT_PROVIDER_REJECTED", {
				provider_message: "Needs the Maintainer role",
			}),
		);
		renderPanel();
		await user.click(await screen.findByRole("button", { name: "Try again" }));
		const alert = await screen.findByRole("alert");
		expect(alert).toHaveTextContent("Still couldn't register the webhook.");
		expect(alert).toHaveTextContent(
			"Details from the platform: Needs the Maintainer role",
		);
		// The platform's reason replaces the guess about permissions.
		expect(screen.queryByText(/most common cause/i)).toBeNull();
	});

	it("invites registering a webhook that Paca never attempted, without guessing at a cause", async () => {
		const user = userEvent.setup();
		repos = [
			{
				...repo,
				webhook_url: "https://paca.example.com/api/v1/git/webhooks/k",
				webhook_attempted: false,
			},
		];
		h.post.mockImplementation(() => apiFail("GIT_PROVIDER_REJECTED"));
		renderPanel();
		expect(
			await screen.findByText(
				/hasn't registered a webhook on this repository yet/i,
			),
		).toBeInTheDocument();
		expect(screen.queryByText(/couldn't register a webhook/i)).toBeNull();
		expect(screen.queryByText(/most common cause/i)).toBeNull();
		expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
		await user.click(screen.getByRole("button", { name: "Register webhook" }));
		// Once it has been tried and refused, it reads as a failure.
		await screen.findByRole("alert");
		expect(screen.getByText(/most common cause/i)).toBeInTheDocument();
		expect(
			screen.getByRole("button", { name: "Try again" }),
		).toBeInTheDocument();
	});

	it("keeps the usual wording for a webhook that was attempted and failed", async () => {
		repos = [
			{
				...repo,
				webhook_url: "https://paca.example.com/api/v1/git/webhooks/k",
				webhook_attempted: true,
			},
		];
		renderPanel();
		expect(
			await screen.findByText(
				/couldn't register a webhook on this repository/i,
			),
		).toBeInTheDocument();
		expect(screen.getByText(/most common cause/i)).toBeInTheDocument();
		expect(
			screen.getByRole("button", { name: "Try again" }),
		).toBeInTheDocument();
	});

	it("keeps the permission hint before a try and when the platform said nothing", async () => {
		const user = userEvent.setup();
		repos = [
			{
				...repo,
				webhook_url: "https://paca.example.com/api/v1/git/webhooks/k",
			},
		];
		h.post.mockImplementation(() => apiFail("GIT_PROVIDER_REJECTED"));
		renderPanel();
		expect(await screen.findByText(/most common cause/i)).toBeInTheDocument();
		await user.click(screen.getByRole("button", { name: "Try again" }));
		await screen.findByRole("alert");
		expect(screen.getByText(/most common cause/i)).toBeInTheDocument();
	});

	it("shows no details line when the platform said nothing", async () => {
		const user = userEvent.setup();
		repos = [{ ...repo, identity_id: null }];
		h.post.mockImplementation(() => apiFail("GIT_REPOSITORY_NOT_VISIBLE"));
		renderPanel();
		await user.click(await screen.findByRole("button", { name: "Reconnect" }));
		expect(await screen.findByRole("alert")).not.toHaveTextContent(
			/Details from the platform/,
		);
	});

	it("lets the user choose the connection to reconnect through, and asks to add one when there is none", async () => {
		const user = userEvent.setup();
		repos = [{ ...repo, identity_id: null }];
		connections = [
			connection,
			{ ...connection, id: "i2", display_name: "Second org", handle: "second" },
		];
		h.post.mockResolvedValue({ data: { data: repo } });
		const { unmount } = renderPanel();
		await user.selectOptions(await screen.findByLabelText("Connection"), "i2");
		await user.click(screen.getByRole("button", { name: "Reconnect" }));
		await waitFor(() =>
			expect(h.post).toHaveBeenCalledWith(
				`/projects/${PROJECT}/git/repositories/r1/reconnect`,
				{ identity_id: "i2" },
			),
		);
		unmount();

		connections = [];
		renderPanel();
		expect(
			await screen.findByText(/Add a connection that can see it first/i),
		).toBeInTheDocument();
		expect(screen.queryByRole("button", { name: "Reconnect" })).toBeNull();
	});

	it("says when Paca could not register a webhook, and retries", async () => {
		const user = userEvent.setup();
		repos = [
			{
				...repo,
				webhook_url: "https://paca.example.com/api/v1/git/webhooks/k",
			},
		];
		h.post.mockImplementation(async () => {
			repos = [repo];
			return { data: { data: repo } };
		});
		renderPanel();
		expect(
			await screen.findByText(/aren't reaching Paca/i),
		).toBeInTheDocument();
		// Paca creates the webhook itself: nothing to copy and paste by hand.
		expect(screen.queryByText(/paca\.example\.com/)).toBeNull();
		await user.click(screen.getByRole("button", { name: "Try again" }));
		await waitFor(() =>
			expect(h.post).toHaveBeenCalledWith(
				`/projects/${PROJECT}/git/repositories/r1/webhook`,
			),
		);
		await waitFor(() =>
			expect(screen.queryByText(/aren't reaching Paca/i)).toBeNull(),
		);
	});

	it("lists linked repositories", async () => {
		repos = [repo];
		renderPanel();
		expect(await screen.findByText("acme/web")).toBeInTheDocument();
		expect(screen.getByText("main")).toBeInTheDocument();
	});

	it("links a repository and shows it", async () => {
		const user = userEvent.setup();
		h.post.mockImplementation(async () => {
			repos = [repo];
			return { data: { data: repo } };
		});
		renderPanel();
		await user.type(
			await screen.findByLabelText("Repository"),
			"https://github.com/acme/web",
		);
		await user.click(screen.getByRole("button", { name: "Link" }));
		expect(await screen.findByText("acme/web")).toBeInTheDocument();
		expect(h.post).toHaveBeenCalledWith(
			`/projects/${PROJECT}/git/repositories`,
			{
				provider_id: "com.paca.github",
				path_or_url: "https://github.com/acme/web",
			},
		);
	});

	it("offers to install the app on the owner when the identity cannot see the repository", async () => {
		const user = userEvent.setup();
		h.post.mockImplementation((url: string) => {
			if (url.endsWith("/repositories"))
				return apiFail("GIT_ACCESS_NOT_GRANTED");
			return Promise.resolve({
				data: {
					data: {
						kind: "redirect",
						url: "https://github.com/apps/x/installations/new",
						state: "g1",
					},
				},
			});
		});
		renderPanel();
		await user.type(
			await screen.findByLabelText("Repository"),
			"git@github.com:other-org/lib.git",
		);
		await user.click(screen.getByRole("button", { name: "Link" }));

		expect(await screen.findByRole("alert")).toHaveTextContent(
			/can't see this repository/i,
		);
		await user.click(
			screen.getByRole("button", { name: "Install on other-org" }),
		);

		await waitFor(() => expect(h.follow).toHaveBeenCalled());
		expect(h.post).toHaveBeenLastCalledWith(
			`/projects/${PROJECT}/git/access/begin`,
			{
				provider_id: "com.paca.github",
				owner: "other-org",
				identity_id: "i1",
				return_url: `${window.location.origin}/projects/${PROJECT}/settings#git`,
			},
		);
		// The browser remembers which flow this state belongs to, and what was being linked.
		expect(
			JSON.parse(window.sessionStorage.getItem("paca.git.flow.g1") ?? "{}"),
		).toEqual({ kind: "access", projectId: PROJECT });
		expect(
			JSON.parse(
				window.sessionStorage.getItem(`paca.git.pendingLink.${PROJECT}`) ??
					"{}",
			),
		).toEqual({
			input: "git@github.com:other-org/lib.git",
			providerId: "com.paca.github",
		});
	});

	it("lets the platform pick the account when the owner cannot be told", async () => {
		const user = userEvent.setup();
		h.post.mockImplementation(() => apiFail("GIT_ACCESS_NOT_GRANTED"));
		renderPanel();
		await user.type(await screen.findByLabelText("Repository"), "web");
		await user.click(screen.getByRole("button", { name: "Link" }));
		expect(
			await screen.findByRole("button", { name: "Choose where to install" }),
		).toBeInTheDocument();
	});

	it("picks up where the user left off after returning from the platform", async () => {
		window.sessionStorage.setItem(
			`paca.git.pendingLink.${PROJECT}`,
			JSON.stringify({ input: "acme/web", providerId: "com.paca.github" }),
		);
		renderPanel();
		expect(await screen.findByDisplayValue("acme/web")).toBeInTheDocument();
		expect(screen.getByRole("status")).toHaveTextContent("acme/web");
		expect(
			window.sessionStorage.getItem(`paca.git.pendingLink.${PROJECT}`),
		).toBeNull(); // shown once
	});

	it("sends the description typed when linking", async () => {
		const user = userEvent.setup();
		h.post.mockImplementation(async () => ({ data: { data: repo } }));
		renderPanel();
		await user.type(await screen.findByLabelText("Repository"), "acme/web");
		await user.type(
			screen.getByLabelText("Description (optional)"),
			"  Web client ",
		);
		await user.click(screen.getByRole("button", { name: "Link" }));
		await waitFor(() =>
			expect(h.post).toHaveBeenCalledWith(
				`/projects/${PROJECT}/git/repositories`,
				{
					provider_id: "com.paca.github",
					path_or_url: "acme/web",
					description: "Web client",
				},
			),
		);
	});

	it("edits a repository's description in place", async () => {
		const user = userEvent.setup();
		repos = [{ ...repo, description: "Web client" }];
		h.patch.mockImplementation(async (_url: string, body: unknown) => {
			repos = [{ ...repo, ...(body as object) }];
			return { data: { data: repos[0] } };
		});
		renderPanel();
		expect(await screen.findByText("Web client")).toBeInTheDocument();
		await user.click(screen.getByRole("button", { name: "Edit description" }));
		const field = screen.getByLabelText("Repository description");
		await user.clear(field);
		await user.type(field, "Customer API");
		await user.click(screen.getByRole("button", { name: "Save description" }));
		await waitFor(() =>
			expect(h.patch).toHaveBeenCalledWith(
				`/projects/${PROJECT}/git/repositories/r1`,
				{ description: "Customer API" },
			),
		);
		expect(await screen.findByText("Customer API")).toBeInTheDocument();
	});

	it("explains a description that is too long", async () => {
		const user = userEvent.setup();
		repos = [repo];
		h.patch.mockImplementationOnce(() => apiFail("GIT_DESCRIPTION_TOO_LONG"));
		renderPanel();
		await user.click(
			await screen.findByRole("button", { name: "Add a description" }),
		);
		await user.type(screen.getByLabelText("Repository description"), "x");
		await user.click(screen.getByRole("button", { name: "Save description" }));
		expect(await screen.findByRole("alert")).toHaveTextContent(/too long/i);
	});

	it("does not offer to edit the description without git:write", async () => {
		repos = [{ ...repo, description: "Web client" }];
		renderPanel(false);
		expect(await screen.findByText("Web client")).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: "Edit description" }),
		).not.toBeInTheDocument();
	});

	it("explains the other ways linking can fail", async () => {
		const user = userEvent.setup();
		for (const [code, text] of [
			["GIT_NO_IDENTITY", /No connection is available/i],
			["GIT_ALREADY_LINKED", /already linked/i],
			["GIT_PROVIDER_UNAVAILABLE", /plugin is unavailable/i],
		] as const) {
			h.post.mockImplementationOnce(() => apiFail(code));
			const { unmount } = renderPanel();
			await user.type(await screen.findByLabelText("Repository"), "acme/web");
			await user.click(screen.getByRole("button", { name: "Link" }));
			expect(await screen.findByRole("alert")).toHaveTextContent(text);
			expect(
				screen.queryByRole("button", { name: /Install on/ }),
			).not.toBeInTheDocument();
			unmount();
		}
	});

	it("unlinks only after confirmation", async () => {
		const user = userEvent.setup();
		repos = [repo];
		h.del.mockImplementation(async () => {
			repos = [];
		});
		renderPanel();
		await user.click(await screen.findByRole("button", { name: "Unlink" }));
		expect(h.del).not.toHaveBeenCalled();
		await user.click(screen.getByRole("button", { name: "Confirm unlink" }));
		await waitFor(() =>
			expect(h.del).toHaveBeenCalledWith(
				`/projects/${PROJECT}/git/repositories/r1`,
			),
		);
	});

	it("is read-only without git:write", async () => {
		repos = [repo];
		renderPanel(false);
		expect(await screen.findByText("acme/web")).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: "Unlink" }),
		).not.toBeInTheDocument();
		expect(screen.queryByLabelText("Repository")).not.toBeInTheDocument();
	});

	it("says so when no provider is installed", async () => {
		providers = [];
		renderPanel();
		expect(
			await screen.findByText(/No Git provider is installed/i),
		).toBeInTheDocument();
	});

	it("shows only the next step to a project with nothing set up", async () => {
		renderPanel();
		expect(
			await screen.findByText("No repositories linked"),
		).toBeInTheDocument();
		expect(screen.getByLabelText("Repository")).toBeInTheDocument();
		expect(screen.queryByText("Get started")).toBeNull();
		expect(screen.queryByText("Using it")).toBeNull();
	});

	it("shows the bot to mention and how to use it once a repository is linked", async () => {
		repos = [{ ...repo, identity_handle: "paca-bot" }];
		settings = { ...settings, default_responder_agent_id: "a1" };
		renderPanel();
		expect((await screen.findAllByText("@paca-bot")).length).toBeGreaterThan(0);
		expect(screen.getByText("Using it")).toBeInTheDocument();
	});

	it("saves a changed responder, access bar and pause switch", async () => {
		const user = userEvent.setup();
		repos = [repo];
		renderPanel();
		const responder = await screen.findByLabelText(/default responder/i);
		await screen.findByRole("option", { name: "Reviewer" });
		const save = screen.getByRole("button", { name: /^save$/i });
		expect(save).toBeDisabled();

		await user.selectOptions(responder, "a2");
		await user.selectOptions(
			screen.getByLabelText(/minimum access/i),
			"triage",
		);
		await user.click(screen.getByRole("switch", { name: /pause automation/i }));
		expect(save).toBeEnabled();
		await user.click(save);

		await waitFor(() =>
			expect(h.put).toHaveBeenCalledWith(`/projects/${PROJECT}/git/settings`, {
				default_responder_agent_id: "a2",
				paused: true,
				min_actor_permission: "triage",
			}),
		);
		expect(await screen.findByText("Saved.")).toBeInTheDocument();
	});

	it("explains a refusal when saving the automation", async () => {
		h.put.mockRejectedValue({
			response: { status: 404, data: { error_code: "GIT_NOT_FOUND" } },
		});
		const user = userEvent.setup();
		repos = [repo];
		renderPanel();
		await screen.findByRole("option", { name: "Dev" });
		await user.selectOptions(screen.getByLabelText(/default responder/i), "a1");
		await user.click(screen.getByRole("button", { name: /^save$/i }));
		expect(await screen.findByRole("alert")).toBeInTheDocument();
	});

	it("holds the automation back until a repository is linked", async () => {
		renderPanel();
		expect(
			await screen.findByText(/Link a repository first/i),
		).toBeInTheDocument();
		expect(await screen.findByLabelText(/default responder/i)).toBeDisabled();
	});

	it("shows the automation but cannot change it without git:write", async () => {
		repos = [repo];
		renderPanel(false);
		expect(await screen.findByLabelText(/default responder/i)).toBeDisabled();
		expect(screen.getByLabelText(/minimum access/i)).toBeDisabled();
		expect(
			screen.getByRole("switch", { name: /pause automation/i }),
		).toHaveAttribute("aria-disabled", "true");
	});
});
