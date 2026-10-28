import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const h = vi.hoisted(() => ({
	get: vi.fn(),
	post: vi.fn(),
	del: vi.fn(),
	follow: vi.fn(),
	global: new Set<string>(),
	project: new Set<string>(),
}));

// The query options inside git-api call its own helpers, which a module mock
// cannot replace, so the HTTP client is mocked instead.
vi.mock("@/lib/api-client", () => ({
	apiClient: { instance: { get: h.get, post: h.post, delete: h.del } },
}));
vi.mock("@/lib/git-api", async (orig) => ({
	...(await orig<typeof import("@/lib/git-api")>()),
	followGitStep: h.follow,
}));
vi.mock("@/hooks/use-permissions", () => ({
	usePermissions: () => ({ hasPermission: (a: string) => h.global.has(a) }),
}));
vi.mock("@/hooks/use-project-permissions", () => ({
	useProjectPermissions: () => ({
		hasProjectPermission: (a: string) => h.project.has(a),
	}),
}));

import { AgentGitTab } from "./AgentGitTab";

const P = "p1";
const A = "a1";
const provider = (over: object = {}) => ({
	id: "com.paca.github",
	display_name: "GitHub",
	hosts: ["github.com"],
	self_hosted_hosts: false,
	terms: {
		change_request: "Pull request",
		change_request_plural: "Pull requests",
		repository: "Repository",
	},
	capabilities: { multiple_identities: true, access_grants: true },
	...over,
});
const identity = {
	id: "i1",
	provider_id: "com.paca.github",
	host: "github.com",
	kind: "app",
	scope: "agent",
	handle: "paca-dev",
	display_name: "Paca Dev",
	expires_at: null,
	created_at: "2026-01-01T00:00:00Z",
};

let providers: unknown[] = [provider()];
let identities: unknown[] = [];
let providersFail = false;

function renderTab(canWrite = true) {
	const client = new QueryClient({
		defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
	});
	return render(
		<QueryClientProvider client={client}>
			<AgentGitTab projectId={P} agentId={A} canWrite={canWrite} />
		</QueryClientProvider>,
	);
}

describe("AgentGitTab", () => {
	beforeEach(() => {
		vi.resetAllMocks();
		window.sessionStorage.clear();
		h.global = new Set(["git.identities:manage"]);
		h.project = new Set(["git:write"]);
		providers = [provider()];
		identities = [];
		providersFail = false;
		h.follow.mockReturnValue(true);
		h.get.mockImplementation(async (url: string) => {
			if (url.endsWith("/git/providers")) {
				if (providersFail)
					throw {
						response: { status: 403, data: { error_code: "FORBIDDEN" } },
					};
				return { data: { data: providers } };
			}
			if (url.endsWith("/git/identity")) return { data: { data: identities } };
			throw new Error(`unexpected GET ${url}`);
		});
	});

	it("says the agent uses the shared identity until it gets its own", async () => {
		renderTab();
		expect(
			await screen.findByText("Uses the shared identity"),
		).toBeInTheDocument();
		expect(
			screen.getByRole("button", { name: "Create identity" }),
		).toBeInTheDocument();
		expect(
			screen.queryByLabelText(/Install on another account/i),
		).not.toBeInTheDocument();
	});

	it("creates the agent's own identity through the agent's endpoint, returning to this tab", async () => {
		const user = userEvent.setup();
		const step = {
			kind: "form_post",
			url: "https://github.com/settings/apps/new",
			fields: { manifest: "{}" },
			state: "s1",
		};
		h.post.mockResolvedValue({ data: { data: step } });
		renderTab();
		await user.click(
			await screen.findByRole("button", { name: "Create identity" }),
		);
		await user.click(
			screen.getByRole("button", { name: /continue to github\.com/i }),
		);

		await waitFor(() => expect(h.follow).toHaveBeenCalledWith(step));
		expect(h.post).toHaveBeenCalledWith(
			`/projects/${P}/agents/${A}/git/identity/setup`,
			{
				provider_id: "com.paca.github",
				host: "github.com",
				return_url: `${window.location.origin}/projects/${P}/agents/${A}#git`,
				params: {},
			},
		);
	});

	it("does not let someone without git.identities:manage create one", async () => {
		h.global = new Set();
		renderTab();
		expect(
			await screen.findByText(/Ask a workspace administrator/i),
		).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: "Create identity" }),
		).not.toBeInTheDocument();
	});

	it("tells a platform with one identity per host that the shared identity is used", async () => {
		providers = [
			provider({
				capabilities: { multiple_identities: false, access_grants: false },
			}),
		];
		renderTab();
		expect(
			await screen.findByText(/one identity per host/i),
		).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: "Create identity" }),
		).not.toBeInTheDocument();
	});

	it("shows the agent's own identity", async () => {
		identities = [identity];
		renderTab();
		expect(await screen.findByText("@paca-dev")).toBeInTheDocument();
		expect(screen.getByText("Own identity")).toBeInTheDocument();
		expect(
			screen.queryByText("Uses the shared identity"),
		).not.toBeInTheDocument();
	});

	it("installs the agent's identity on an account and remembers that this is an access grant", async () => {
		const user = userEvent.setup();
		identities = [identity];
		h.post.mockResolvedValue({
			data: {
				data: {
					kind: "redirect",
					url: "https://github.com/apps/x/installations/new",
					state: "ag9",
				},
			},
		});
		renderTab();
		await user.type(
			await screen.findByLabelText(/Install on another account/i),
			"  acme  ",
		);
		await user.click(screen.getByRole("button", { name: "Install" }));

		await waitFor(() => expect(h.follow).toHaveBeenCalled());
		expect(h.post).toHaveBeenCalledWith(
			`/projects/${P}/agents/${A}/git/identity/access/begin`,
			{
				provider_id: "com.paca.github",
				owner: "acme",
				return_url: `${window.location.origin}/projects/${P}/agents/${A}#git`,
			},
		);
		expect(
			JSON.parse(window.sessionStorage.getItem("paca.git.flow.ag9") ?? "{}"),
		).toEqual({ kind: "access", projectId: P });
	});

	it("hides the install form unless the user may install (agents:write and git:write)", async () => {
		identities = [identity];
		h.project = new Set();
		const { unmount } = renderTab();
		expect(await screen.findByText("@paca-dev")).toBeInTheDocument();
		expect(
			screen.queryByLabelText(/Install on another account/i),
		).not.toBeInTheDocument();
		unmount();

		h.project = new Set(["git:write"]);
		renderTab(false); // no agents:write
		expect(await screen.findByText("@paca-dev")).toBeInTheDocument();
		expect(
			screen.queryByLabelText(/Install on another account/i),
		).not.toBeInTheDocument();
	});

	it("reports why an install could not start", async () => {
		const user = userEvent.setup();
		identities = [identity];
		h.post.mockRejectedValue({
			response: { status: 409, data: { error_code: "GIT_NO_IDENTITY" } },
		});
		renderTab();
		await user.click(await screen.findByRole("button", { name: "Install" }));
		expect(await screen.findByRole("alert")).toHaveTextContent(
			/No connection is available/i,
		);
		expect(h.follow).not.toHaveBeenCalled();
	});

	it("removes the agent's identity after confirmation, asking for that provider", async () => {
		const user = userEvent.setup();
		identities = [identity];
		h.del.mockImplementation(async () => {
			identities = [];
		});
		renderTab();
		await user.click(await screen.findByRole("button", { name: "Remove" }));
		expect(h.del).not.toHaveBeenCalled();
		await user.click(
			await screen.findByRole("button", { name: "Remove connection" }),
		);
		await waitFor(() =>
			expect(h.del).toHaveBeenCalledWith(
				`/projects/${P}/agents/${A}/git/identity`,
				{ params: { provider_id: "com.paca.github" } },
			),
		);
	});

	it("offers no removal without git.identities:manage", async () => {
		identities = [identity];
		h.global = new Set();
		renderTab();
		expect(await screen.findByText("@paca-dev")).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: "Remove" }),
		).not.toBeInTheDocument();
	});

	it("says so when the providers cannot be loaded", async () => {
		providersFail = true;
		renderTab();
		expect(await screen.findByRole("alert")).toHaveTextContent(
			/could not be loaded/i,
		);
	});

	it("says so when no provider is installed", async () => {
		providers = [];
		renderTab();
		expect(
			await screen.findByText(/No Git platform available/i),
		).toBeInTheDocument();
	});
});
