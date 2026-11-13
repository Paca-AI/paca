import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { renderWithQueries } from "@/test/render-with-queries";
import { LoginSecuritySettings } from "./LoginSecuritySettings";

const http = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn() }));
vi.mock("@/lib/api-client", () => ({ apiClient: { instance: http } }));

const sso = vi.hoisted(() => ({
	providers: [] as { slug: string; display_name: string }[],
}));

beforeEach(() => {
	vi.clearAllMocks();
	sso.providers = [{ slug: "corp", display_name: "Corp SSO" }];
	http.get.mockImplementation(async (url: string) => {
		if (url === "/admin/auth-security") {
			return {
				data: {
					data: {
						privileged_roles: ["*"],
						require_mfa: false,
						ip_allow_list: [],
						sso_only: false,
					},
				},
			};
		}
		if (url === "/auth/sso/providers") {
			return { data: { data: sso.providers } };
		}
		return {
			data: {
				data: {
					items: [
						{ id: "1", name: "ADMIN" },
						{ id: "2", name: "USER" },
					],
					page_size: 100,
					next_cursor: null,
					total_count: 2,
				},
			},
		};
	});
	http.put.mockImplementation(async (_url: string, body: unknown) => ({
		data: { data: body },
	}));
});

describe("LoginSecuritySettings", () => {
	it("saves the protected roles, the MFA requirement and the allow-list together", async () => {
		const user = userEvent.setup();
		renderWithQueries(<LoginSecuritySettings />);

		await user.click(
			await screen.findByRole("checkbox", { name: "All roles" }),
		);
		await user.click(await screen.findByRole("checkbox", { name: "ADMIN" }));
		await user.click(
			screen.getByRole("switch", { name: /require two-step verification/i }),
		);
		await user.type(
			screen.getByLabelText(/allowed ip addresses/i),
			"203.0.113.0/24",
		);
		await user.click(screen.getByRole("button", { name: /save changes/i }));

		await waitFor(() =>
			expect(http.put).toHaveBeenCalledWith("/admin/auth-security", {
				privileged_roles: ["ADMIN"],
				require_mfa: true,
				ip_allow_list: ["203.0.113.0/24"],
				sso_only: false,
			}),
		);
	});

	it("warns that nothing is protected until a role is chosen, and about lockout", async () => {
		const user = userEvent.setup();
		renderWithQueries(<LoginSecuritySettings />);

		await user.click(
			await screen.findByRole("checkbox", { name: "All roles" }),
		);
		await user.type(
			screen.getByLabelText(/allowed ip addresses/i),
			"10.0.0.0/8",
		);

		expect(screen.getByText(/no role is protected yet/i)).toBeInTheDocument();
		expect(screen.getByText(/lock yourself out/i)).toBeInTheDocument();
	});

	it("keeps Save disabled until something changes", async () => {
		renderWithQueries(<LoginSecuritySettings />);
		await screen.findByRole("checkbox", { name: "All roles" });
		expect(
			screen.getByRole("button", { name: /save changes/i }),
		).toBeDisabled();
	});

	it("turns on SSO-only sign-in", async () => {
		const user = userEvent.setup();
		renderWithQueries(<LoginSecuritySettings />);

		await user.click(
			await screen.findByRole("switch", { name: /sso-only sign-in/i }),
		);
		expect(screen.getByText(/sign in as administrator/i)).toBeInTheDocument();
		await user.click(screen.getByRole("button", { name: /save changes/i }));

		await waitFor(() =>
			expect(http.put).toHaveBeenCalledWith("/admin/auth-security", {
				privileged_roles: ["*"],
				require_mfa: false,
				ip_allow_list: [],
				sso_only: true,
			}),
		);
	});

	it("can't turn on SSO-only sign-in without an enabled provider", async () => {
		sso.providers = [];
		renderWithQueries(<LoginSecuritySettings />);

		expect(
			await screen.findByText(/enable an sso provider first/i),
		).toBeInTheDocument();
		expect(
			screen.getByRole("switch", { name: /sso-only sign-in/i }),
		).toHaveAttribute("aria-disabled", "true");
	});
});
