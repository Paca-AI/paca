// Tests for the LoginFormPanel's login entry-point switching: the SSO button
// appears only when the instance's /auth/config advertises OIDC, and the
// local password form disappears entirely on SSO-only instances.

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LoginFormPanel } from "./LoginFormPanel";

// The panel reads branding + auth config through hooks; stub both so the
// component renders in isolation. vi.hoisted keeps the mock fn available to
// the hoisted vi.mock factory below.
const { getAuthConfigMock } = vi.hoisted(() => ({
	getAuthConfigMock: vi.fn(),
}));

vi.mock("@/hooks/use-branding", () => ({
	useBranding: vi.fn(() => undefined),
}));

vi.mock("@/lib/auth-api", async (importOriginal) => {
	const original = await importOriginal<typeof import("@/lib/auth-api")>();
	return {
		...original,
		authConfigQueryOptions: {
			queryKey: ["auth", "config"],
			queryFn: getAuthConfigMock,
		},
	};
});

function renderPanel() {
	const client = new QueryClient({
		defaultOptions: { queries: { retry: false } },
	});
	return render(
		<QueryClientProvider client={client}>
			<LoginFormPanel />
		</QueryClientProvider>,
	);
}

describe("LoginFormPanel login entry points", () => {
	beforeEach(() => {
		getAuthConfigMock.mockReset();
	});

	it("renders only the local form by default (config loading / SSO off)", async () => {
		getAuthConfigMock.mockResolvedValue({
			local_login_enabled: true,
			oidc: { enabled: false, display_name: "" },
		});
		renderPanel();

		expect(
			await screen.findByPlaceholderText(/enter your username/i),
		).toBeInTheDocument();
		expect(
			screen.queryByRole("link", { name: /continue with/i }),
		).not.toBeInTheDocument();
	});

	it("renders the SSO button when OIDC is enabled", async () => {
		getAuthConfigMock.mockResolvedValue({
			local_login_enabled: true,
			oidc: { enabled: true, display_name: "Company SSO" },
		});
		renderPanel();

		const sso = await screen.findByRole("link", {
			name: /continue with Company SSO/i,
		});
		expect(sso).toHaveAttribute("href", "/api/v1/auth/oidc/login");
		// Local form still available alongside SSO.
		expect(
			screen.getByPlaceholderText(/enter your username/i),
		).toBeInTheDocument();
	});

	it("hides the local password form on SSO-only instances", async () => {
		getAuthConfigMock.mockResolvedValue({
			local_login_enabled: false,
			oidc: { enabled: true, display_name: "Company SSO" },
		});
		renderPanel();

		expect(
			await screen.findByRole("link", { name: /continue with Company SSO/i }),
		).toBeInTheDocument();
		expect(
			screen.queryByPlaceholderText(/enter your username/i),
		).not.toBeInTheDocument();
		expect(screen.queryByLabelText(/^password$/i)).not.toBeInTheDocument();
	});
});
