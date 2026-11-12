import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { LoginFormPanel } from "./LoginFormPanel";

const mocks = vi.hoisted(() => ({
	ssoOnly: false,
	providers: [{ slug: "corp", display_name: "Corp SSO" }] as {
		slug: string;
		display_name: string;
	}[],
}));
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => vi.fn() }));
vi.mock("@/lib/api-client", () => ({
	apiClient: {
		instance: {
			get: vi.fn(async (url: string) => {
				if (url === "/auth/login-options") {
					return { data: { data: { sso_only: mocks.ssoOnly } } };
				}
				if (url === "/auth/sso/providers") {
					return { data: { data: mocks.providers } };
				}
				return { data: { data: {} } };
			}),
		},
	},
}));

function renderPanel(props: Parameters<typeof LoginFormPanel>[0] = {}) {
	const client = new QueryClient({
		defaultOptions: { queries: { retry: false } },
	});
	return render(
		<QueryClientProvider client={client}>
			<LoginFormPanel {...props} />
		</QueryClientProvider>,
	);
}

beforeEach(() => {
	mocks.ssoOnly = false;
	mocks.providers = [{ slug: "corp", display_name: "Corp SSO" }];
});

describe("LoginFormPanel", () => {
	it("offers the password form and SSO side by side by default", async () => {
		renderPanel();
		expect(
			await screen.findByRole("link", { name: /continue with corp sso/i }),
		).toBeInTheDocument();
		expect(screen.getByLabelText(/username/i)).toBeInTheDocument();
		expect(
			screen.queryByRole("link", { name: /sign in as administrator/i }),
		).toBeNull();
	});

	it("offers only SSO when the instance is SSO-only, with a link to the administrator sign-in", async () => {
		mocks.ssoOnly = true;
		renderPanel({ redirectTo: "/projects" });

		const admin = await screen.findByRole("link", {
			name: /sign in as administrator/i,
		});
		expect(admin).toHaveAttribute("href", "/?admin=1&redirect=%2Fprojects");
		expect(
			screen.getByRole("link", { name: /continue with corp sso/i }),
		).toBeInTheDocument();
		expect(screen.queryByLabelText(/username/i)).toBeNull();
		expect(screen.queryByLabelText(/password/i)).toBeNull();
		// Remember-me still applies to the SSO session.
		expect(
			screen.getByRole("switch", { name: /keep me signed in/i }),
		).toBeInTheDocument();
	});

	it("shows the password form on the administrator sign-in, with a way back", async () => {
		mocks.ssoOnly = true;
		renderPanel({ adminSignIn: true });

		const back = await screen.findByRole("link", {
			name: /back to single sign-on/i,
		});
		expect(back).toHaveAttribute("href", "/");
		expect(screen.getByLabelText(/username/i)).toBeInTheDocument();
		expect(
			screen.getByText(/only the default administrator account/i),
		).toBeInTheDocument();
	});

	it("says so when the instance is SSO-only but no provider is available", async () => {
		mocks.ssoOnly = true;
		mocks.providers = [];
		renderPanel();

		expect(
			await screen.findByText(/no single sign-on option is available/i),
		).toBeInTheDocument();
		expect(
			screen.getByRole("link", { name: /sign in as administrator/i }),
		).toBeInTheDocument();
	});
});
