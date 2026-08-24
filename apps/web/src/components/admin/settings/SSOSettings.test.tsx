import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
	data: {
		source: "environment" as const,
		enabled: true,
		issuer_url: "https://id.example.com",
		client_id: "paca",
		client_secret_configured: true,
		scopes: ["openid", "profile", "email"],
		redirect_url: "https://paca.example.com/api/v1/auth/oidc/callback",
		display_name: "Company SSO",
		username_claim: "preferred_username",
		local_login_enabled: true,
		encrypted_secret_storage_available: true,
	},
	mutate: vi.fn(),
	setQueryData: vi.fn(),
	mutationFn: null as null | (() => Promise<unknown>),
	update: vi.fn(),
}));

vi.mock("@tanstack/react-query", async () => {
	const actual = await vi.importActual<typeof import("@tanstack/react-query")>(
		"@tanstack/react-query",
	);
	return {
		...actual,
		useQuery: () => ({ data: mocks.data }),
		useQueryClient: () => ({ setQueryData: mocks.setQueryData }),
		useMutation: (options: { mutationFn: () => Promise<unknown> }) => {
			mocks.mutationFn = options.mutationFn;
			return { mutate: mocks.mutate, isPending: false };
		},
	};
});

vi.mock("@/lib/sso-settings-api", async () => {
	const actual = await vi.importActual<typeof import("@/lib/sso-settings-api")>(
		"@/lib/sso-settings-api",
	);
	return { ...actual, updateSSOSettings: mocks.update };
});

import { SSOSettings } from "./SSOSettings";

describe("SSOSettings", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mocks.mutationFn = null;
		mocks.data.encrypted_secret_storage_available = true;
	});

	it("shows effective values without placing the stored secret in the form", () => {
		render(<SSOSettings />);

		expect(screen.getByLabelText(/issuer url/i)).toHaveValue(
			"https://id.example.com",
		);
		expect(screen.getByLabelText(/client secret/i)).toHaveValue("");
		expect(screen.getByText(/secret is configured/i)).toBeInTheDocument();
	});

	it("omits a blank secret when saving", async () => {
		mocks.update.mockResolvedValue(mocks.data);
		render(<SSOSettings />);

		await userEvent.type(screen.getByLabelText(/display name/i), " updated");
		await userEvent.click(screen.getByRole("button", { name: /save/i }));
		expect(mocks.mutate).toHaveBeenCalledOnce();

		await mocks.mutationFn?.();
		expect(mocks.update).toHaveBeenCalledWith(
			expect.not.objectContaining({ client_secret: expect.anything() }),
		);
	});

	it("prevents database-backed saves when encrypted storage is unavailable", () => {
		mocks.data.encrypted_secret_storage_available = false;
		render(<SSOSettings />);

		expect(screen.getByRole("button", { name: /save/i })).toBeDisabled();
		expect(
			screen.getByText(/encrypted secret storage is unavailable/i),
		).toBeInTheDocument();
	});
});
