import { describe, expect, it } from "vitest";

import { ssoLoginUrl } from "./sso-api";

describe("ssoLoginUrl", () => {
	it("points at the provider's login route with the remember-me choice", () => {
		expect(ssoLoginUrl("corp", true)).toBe(
			"/api/v1/auth/sso/corp/login?remember_me=true",
		);
		expect(ssoLoginUrl("corp", false)).toBe(
			"/api/v1/auth/sso/corp/login?remember_me=false",
		);
	});

	it("encodes the post-sign-in redirect", () => {
		expect(ssoLoginUrl("corp", false, "/projects/1?tab=board")).toBe(
			"/api/v1/auth/sso/corp/login?remember_me=false&redirect=%2Fprojects%2F1%3Ftab%3Dboard",
		);
	});
});
