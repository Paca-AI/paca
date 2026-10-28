import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const {
	mockComplete,
	mockCompleteProject,
	mockCompleteAccess,
	mockFollow,
	mockBeginAgentAccess,
} = vi.hoisted(() => ({
	mockBeginAgentAccess: vi.fn(),
	mockComplete: vi.fn(),
	mockCompleteProject: vi.fn(),
	mockCompleteAccess: vi.fn(),
	mockFollow: vi.fn(),
}));

vi.mock("@/lib/git-api", async (orig) => ({
	...(await orig<typeof import("@/lib/git-api")>()),
	completeGitSetup: mockComplete,
	completeProjectGitSetup: mockCompleteProject,
	completeGitAccess: mockCompleteAccess,
	beginAgentGitAccess: mockBeginAgentAccess,
	followGitStep: mockFollow,
}));
vi.mock("@tanstack/react-router", () => ({
	Link: ({ to, children, ...rest }: { to: string; children?: ReactNode }) => (
		<a href={to} {...rest}>
			{children}
		</a>
	),
}));

import { rememberAgentInstall, rememberGitFlow } from "@/lib/git-api";
import { GitSetupCallback, parseCallback } from "./GitSetupCallback";

function renderCallback(search: string) {
	const client = new QueryClient({
		defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
	});
	return render(
		<QueryClientProvider client={client}>
			<GitSetupCallback search={search} />
		</QueryClientProvider>,
	);
}

const completion = (over: object = {}) => ({
	identity: { id: "i1" },
	next: null,
	return_url: "https://paca.example.com/admin/git",
	done: true,
	...over,
});

// Each test uses its own query string: the component deliberately remembers
// the outcome per URL so a remount cannot spend a one-time code twice.
let n = 0;
const uniq = (qs: string) => `${qs}&t=${++n}`;

describe("parseCallback", () => {
	it("separates the state from the platform's parameters", () => {
		expect(parseCallback("?code=abc&state=st&setup_action=install")).toEqual({
			state: "st",
			params: { code: "abc", setup_action: "install" },
		});
	});

	it("keeps values exactly as sent", () => {
		// A router would parse "1e5" and "007" as numbers and corrupt the code.
		const { params } = parseCallback("?state=s&code=1e5&installation_id=007");
		expect(params).toEqual({ code: "1e5", installation_id: "007" });
	});

	it("reports a missing state", () => {
		expect(parseCallback("?code=abc").state).toBe("");
	});
});

describe("GitSetupCallback", () => {
	beforeEach(() => {
		window.sessionStorage.clear();
		mockCompleteAccess.mockReset();
		mockComplete.mockReset();
		mockFollow.mockReset();
		mockBeginAgentAccess.mockReset();
		mockFollow.mockReturnValue(true);
	});

	it("completes the setup with the raw parameters and follows the next step", async () => {
		const next = {
			kind: "redirect",
			url: "https://github.com/apps/x/installations/new",
		};
		mockComplete.mockResolvedValue(completion({ next, done: false }));
		const qs = uniq("?code=1e5&state=st1");
		renderCallback(qs);

		await waitFor(() => expect(mockFollow).toHaveBeenCalledWith(next));
		expect(mockComplete).toHaveBeenCalledWith("st1", {
			code: "1e5",
			t: expect.any(String),
		});
	});

	it("sends one-time parameters only once even if the page mounts twice", async () => {
		mockComplete.mockResolvedValue(
			completion({ next: { kind: "redirect", url: "u" }, done: false }),
		);
		const qs = uniq("?code=c&state=st2");
		const first = renderCallback(qs);
		first.unmount();
		renderCallback(qs);
		await waitFor(() => expect(mockFollow).toHaveBeenCalled());
		expect(mockComplete).toHaveBeenCalledTimes(1);
	});

	it("returns to where setup started when it is finished", async () => {
		const replace = vi.fn();
		const original = window.location;
		Object.defineProperty(window, "location", {
			configurable: true,
			value: { ...original, origin: original.origin, replace },
		});
		try {
			mockComplete.mockResolvedValue(
				completion({
					return_url: `${window.location.origin}/admin/git?done=1`,
				}),
			);
			renderCallback(uniq("?installation_id=9&state=st3"));
			await waitFor(() =>
				expect(replace).toHaveBeenCalledWith("/admin/git?done=1"),
			);
			expect(mockFollow).not.toHaveBeenCalled();
		} finally {
			Object.defineProperty(window, "location", {
				configurable: true,
				value: original,
			});
		}
	});

	it("explains a rejected setup and offers a way back", async () => {
		mockComplete.mockRejectedValue({
			response: { data: { error_code: "GIT_SETUP_INVALID" } },
		});
		renderCallback(uniq("?code=c&state=st4"));
		expect(await screen.findByRole("alert")).toHaveTextContent(
			/invalid, expired or already used/i,
		);
		const assign = vi.fn();
		const original = window.location;
		Object.defineProperty(window, "location", {
			configurable: true,
			value: { ...original, assign },
		});
		try {
			screen.getByRole("button", { name: /back to git settings/i }).click();
			expect(assign).toHaveBeenCalledWith("/home");
		} finally {
			Object.defineProperty(window, "location", {
				configurable: true,
				value: original,
			});
		}
	});

	it("completes a project's own connection through the project route and goes back there on failure", async () => {
		rememberGitFlow("st-proj", { kind: "project-identity", projectId: "p9" });
		mockCompleteProject.mockRejectedValue({
			response: { data: { error_code: "GIT_SETUP_INVALID" } },
		});
		renderCallback(uniq("?code=c&state=st-proj"));
		expect(await screen.findByRole("alert")).toBeInTheDocument();
		expect(mockCompleteProject).toHaveBeenCalledWith("p9", "st-proj", {
			code: "c",
			t: expect.any(String),
		});
		expect(mockComplete).not.toHaveBeenCalled();

		const assign = vi.fn();
		const original = window.location;
		Object.defineProperty(window, "location", {
			configurable: true,
			value: { ...original, assign },
		});
		try {
			screen.getByRole("button", { name: /back to git settings/i }).click();
			expect(assign).toHaveBeenCalledWith("/projects/p9/settings#git");
		} finally {
			Object.defineProperty(window, "location", {
				configurable: true,
				value: original,
			});
		}
	});

	it("does not call the API without a state", async () => {
		renderCallback(uniq("?code=c"));
		expect(await screen.findByRole("alert")).toBeInTheDocument();
		expect(mockComplete).not.toHaveBeenCalled();
	});

	it("shows an error when the provider's next step cannot be followed", async () => {
		mockFollow.mockReturnValue(false);
		mockComplete.mockResolvedValue(
			completion({ next: { kind: "done" }, done: false }),
		);
		renderCallback(uniq("?code=c&state=st5"));
		expect(await screen.findByRole("alert")).toHaveTextContent(
			/something went wrong/i,
		);
	});

	it("completes a project's access grant when that is what the browser started", async () => {
		const replace = vi.fn();
		const original = window.location;
		Object.defineProperty(window, "location", {
			configurable: true,
			value: { ...original, origin: original.origin, replace },
		});
		try {
			mockCompleteAccess.mockResolvedValue({
				owner: "acme",
				return_url: `${window.location.origin}/projects/p1/settings#git`,
			});
			rememberGitFlow("ga1", { kind: "access", projectId: "p1" });
			renderCallback(uniq("?installation_id=9&setup_action=install&state=ga1"));

			await waitFor(() =>
				expect(replace).toHaveBeenCalledWith("/projects/p1/settings#git"),
			);
			expect(mockCompleteAccess).toHaveBeenCalledWith("p1", "ga1", {
				installation_id: "9",
				setup_action: "install",
				t: expect.any(String),
			});
			expect(mockComplete).not.toHaveBeenCalled();
			expect(mockFollow).not.toHaveBeenCalled();
			expect(window.sessionStorage.getItem("paca.git.flow.ga1")).toBeNull();
		} finally {
			Object.defineProperty(window, "location", {
				configurable: true,
				value: original,
			});
		}
	});

	it("reports a failed access grant", async () => {
		mockCompleteAccess.mockRejectedValue({
			response: { data: { error_code: "GIT_PROVIDER_REJECTED" } },
		});
		rememberGitFlow("ga2", { kind: "access", projectId: "p1" });
		renderCallback(uniq("?installation_id=9&state=ga2"));
		expect(await screen.findByRole("alert")).toHaveTextContent(
			/refused the request/i,
		);
	});

	it("falls back to identity setup when it has no note of the flow", async () => {
		mockComplete.mockResolvedValue(
			completion({ next: { kind: "redirect", url: "u" }, done: false }),
		);
		renderCallback(uniq("?code=c&state=unknown-flow"));
		await waitFor(() => expect(mockComplete).toHaveBeenCalled());
		expect(mockCompleteAccess).not.toHaveBeenCalled();
	});

	describe("an agent's App", () => {
		const agentCompletion = () =>
			completion({
				identity: { id: "i2", scope: "agent", provider_id: "com.paca.github" },
				return_url: "https://paca.example.com/projects/p1/agents/a1#git",
			});

		it("goes straight on to installing it once it is created", async () => {
			rememberAgentInstall({ projectId: "p1", agentId: "a1" });
			mockComplete.mockResolvedValue(agentCompletion());
			mockBeginAgentAccess.mockResolvedValue({
				kind: "redirect",
				url: "https://github.com/apps/x/installations/new",
				state: "acc1",
			});
			renderCallback(uniq("?code=c&state=ag1"));
			await waitFor(() => expect(mockFollow).toHaveBeenCalled());
			expect(mockBeginAgentAccess).toHaveBeenCalledWith("p1", "a1", {
				provider_id: "com.paca.github",
				owner: "",
				return_url: "https://paca.example.com/projects/p1/agents/a1#git",
			});
			// The install's own return trip completes as an access grant.
			expect(window.sessionStorage.getItem("paca.git.flow.acc1")).toContain(
				"access",
			);
			expect(window.sessionStorage.getItem("paca.git.agentInstall")).toBeNull();
		});

		it("returns to the agent when the install cannot start", async () => {
			const replace = vi.fn();
			const original = window.location;
			Object.defineProperty(window, "location", {
				configurable: true,
				value: { ...original, origin: "https://paca.example.com", replace },
			});
			try {
				rememberAgentInstall({ projectId: "p1", agentId: "a1" });
				mockComplete.mockResolvedValue(agentCompletion());
				mockBeginAgentAccess.mockRejectedValue({ response: { status: 403 } });
				renderCallback(uniq("?code=c&state=ag2"));
				await waitFor(() =>
					expect(replace).toHaveBeenCalledWith("/projects/p1/agents/a1#git"),
				);
				expect(mockFollow).not.toHaveBeenCalled();
			} finally {
				Object.defineProperty(window, "location", {
					configurable: true,
					value: original,
				});
			}
		});

		it("does not install for a shared identity, even with a stale note", async () => {
			rememberAgentInstall({ projectId: "p1", agentId: "a1" });
			mockComplete.mockResolvedValue(completion());
			renderCallback(uniq("?code=c&state=sh1"));
			await waitFor(() => expect(mockComplete).toHaveBeenCalled());
			expect(mockBeginAgentAccess).not.toHaveBeenCalled();
		});
	});
});
