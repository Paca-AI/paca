import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockGet, mockPatch } = vi.hoisted(() => ({
	mockGet: vi.fn(),
	mockPatch: vi.fn(),
}));

vi.mock("./api-client", () => ({
	apiClient: {
		instance: {
			get: mockGet,
			patch: mockPatch,
		},
	},
}));

import {
	getSSOSettings,
	ssoSettingsQueryOptions,
	updateSSOSettings,
} from "./sso-settings-api";

const settings = {
	source: "environment" as const,
	enabled: true,
	issuer_url: "https://id.example.com",
	client_id: "paca",
	client_secret_configured: true,
	scopes: ["openid", "profile"],
	redirect_url: "https://paca.example.com/api/v1/auth/oidc/callback",
	display_name: "Company SSO",
	username_claim: "preferred_username",
	local_login_enabled: true,
	encrypted_secret_storage_available: true,
};

describe("SSO settings API", () => {
	beforeEach(() => {
		mockGet.mockReset();
		mockPatch.mockReset();
	});

	it("loads the non-secret admin configuration", async () => {
		mockGet.mockResolvedValue({ data: { data: settings } });

		await expect(getSSOSettings()).resolves.toEqual(settings);
		expect(mockGet).toHaveBeenCalledWith("/admin/settings/sso");
		expect(ssoSettingsQueryOptions.queryKey).toEqual([
			"admin",
			"settings",
			"sso",
		]);
	});

	it("sends the write-only secret only in the update payload", async () => {
		const updated = { ...settings, source: "database" as const };
		mockPatch.mockResolvedValue({ data: { data: updated } });
		const payload = {
			enabled: true,
			issuer_url: settings.issuer_url,
			client_id: settings.client_id,
			client_secret: "replacement",
			scopes: settings.scopes,
			redirect_url: settings.redirect_url,
			display_name: settings.display_name,
			username_claim: settings.username_claim,
			local_login_enabled: true,
		};

		await expect(updateSSOSettings(payload)).resolves.toEqual(updated);
		expect(mockPatch).toHaveBeenCalledWith("/admin/settings/sso", payload);
		expect(updated).not.toHaveProperty("client_secret");
	});
});
