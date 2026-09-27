import { describe, expect, it } from "vitest";

import { slugify } from "./SsoProviderDialog";

describe("slugify", () => {
	it("derives a URL-safe slug from a display name", () => {
		expect(slugify("Google Workspace")).toBe("google-workspace");
		expect(slugify("  Microsoft Entra ID! ")).toBe("microsoft-entra-id");
		expect(slugify("Okta (prod)")).toBe("okta-prod");
	});

	it("drops characters that can't appear in a slug", () => {
		expect(slugify("Café SSO")).toBe("cafe-sso");
		expect(slugify("日本語")).toBe("");
	});

	it("caps the length without a trailing dash", () => {
		const slug = slugify(`${"a".repeat(39)} b`);
		expect(slug.length).toBeLessThanOrEqual(40);
		expect(slug.endsWith("-")).toBe(false);
	});
});
