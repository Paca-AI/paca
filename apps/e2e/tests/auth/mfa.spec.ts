// spec: features/auth/mfa.feature
// seed: tests/seed.spec.ts
//
// Two-step verification, recovery codes, IP allow-lists and the
// require-MFA policy. Every scenario works on throwaway users under its own
// prefix. The instance login policy is shared, so the policy scenarios only
// ever protect a throwaway role (never "All roles") and put the default back
// afterwards - other specs' users are not subject to it.

import {
	type APIRequestContext,
	expect,
	type Page,
	test,
} from "@playwright/test";
import {
	API_URL,
	allowPolicy,
	authRequest,
	BASE_URL,
	cleanupGlobalRolesByPrefix,
	cleanupUsersByPrefix,
	createGlobalRole,
	createUserWithPassword,
	newRunId,
	RESTRICTED_PASSWORD,
	signIn,
} from "../helpers/e2e-api";
import {
	DEFAULT_POLICY,
	enrollMfa,
	errorCode,
	freshCode,
	type PlaywrightLike,
	putPolicy,
	rawLogin,
	restorePolicy,
	setUserCidrs,
	signInWithMfa,
	verifyMfa,
} from "../helpers/mfa";

const PREFIX = "E2E_MFA_";
const ROLE_PREFIX = "E2E_MFA_ROLE_";
const RUN_ID = newRunId();
let counter = 0;

function uniqueName(label: string): string {
	counter += 1;
	return `${PREFIX}${label}_${RUN_ID}_${counter}`;
}

test.beforeEach(async ({ request }) => {
	await cleanupUsersByPrefix(request, PREFIX);
	await cleanupGlobalRolesByPrefix(request, ROLE_PREFIX);
});

test.afterEach(async ({ request }) => {
	await restorePolicy(request);
	await cleanupUsersByPrefix(request, PREFIX);
	await cleanupGlobalRolesByPrefix(request, ROLE_PREFIX);
});

/** Creates a user and returns an API context signed in as them (no MFA yet). */
async function newUser(
	request: APIRequestContext,
	playwright: PlaywrightLike,
	label: string,
	roles?: string[],
) {
	const username = uniqueName(label);
	await authRequest(request);
	const id = await createUserWithPassword(request, playwright, {
		username,
		fullName: username,
		roles,
	});
	return { username, id };
}

test.describe("Enrolling through the API", () => {
	test("enrollment must be confirmed with a valid code before it takes effect", async ({
		request,
		playwright,
	}) => {
		const { username } = await newUser(request, playwright, "ENROLL");
		const { ctx } = await rawLogin(playwright, username);

		const status = await ctx.get(`${API_URL}/users/me/mfa`);
		expect((await status.json()).data.enabled).toBe(false);

		const begin = await ctx.post(`${API_URL}/users/me/mfa/enroll`);
		expect(begin.ok()).toBeTruthy();
		const data = (await begin.json()).data;
		expect(data.uri).toContain("otpauth://totp/");
		expect(data.secret).toMatch(/^[A-Z2-7]+$/);

		// Not confirmed yet, so a password-only sign-in still works.
		const stillPlain = await rawLogin(playwright, username);
		expect(
			(await stillPlain.response.json()).data.mfa_required,
		).toBeUndefined();
		await stillPlain.ctx.dispose();

		const wrong = await ctx.post(`${API_URL}/users/me/mfa/confirm`, {
			data: { code: "000000" },
		});
		expect(wrong.status()).toBe(401);
		expect(await errorCode(wrong)).toBe("AUTH_MFA_INVALID_CODE");

		const ok = await ctx.post(`${API_URL}/users/me/mfa/confirm`, {
			data: { code: await freshCode(data.secret) },
		});
		expect(ok.ok()).toBeTruthy();
		const codes = (await ok.json()).data.recovery_codes as string[];
		expect(codes).toHaveLength(8);
		expect(new Set(codes).size).toBe(8);

		const after = await ctx.get(`${API_URL}/users/me/mfa`);
		const body = (await after.json()).data;
		expect(body.enabled).toBe(true);
		expect(body.recovery_codes_left).toBe(8);

		// Already enabled: a second enrollment is refused.
		const again = await ctx.post(`${API_URL}/users/me/mfa/enroll`);
		expect(again.status()).toBe(409);
		await ctx.dispose();
	});
});

test.describe("Signing in with a second factor", () => {
	test("a correct password alone yields a challenge, not a session", async ({
		request,
		playwright,
	}) => {
		const { username } = await newUser(request, playwright, "CHALLENGE");
		const setup = await rawLogin(playwright, username);
		const mfa = await enrollMfa(setup.ctx);
		await setup.ctx.dispose();

		const { ctx, response } = await rawLogin(playwright, username);
		expect(response.ok()).toBeTruthy();
		const body = (await response.json()).data;
		expect(body.mfa_required).toBe(true);
		expect(typeof body.mfa_challenge).toBe("string");
		// No cookies, so nothing is authenticated yet.
		expect((await ctx.get(`${API_URL}/users/me`)).status()).toBe(401);

		const bad = await verifyMfa(ctx, body.mfa_challenge, "000000");
		expect(bad.status()).toBe(401);
		expect(await errorCode(bad)).toBe("AUTH_MFA_INVALID_CODE");

		const good = await verifyMfa(
			ctx,
			body.mfa_challenge,
			await freshCode(mfa.secret),
		);
		expect(good.ok()).toBeTruthy();
		expect((await ctx.get(`${API_URL}/users/me`)).ok()).toBeTruthy();
		await ctx.dispose();
	});

	test("a challenge token cannot be used as a session or reused", async ({
		request,
		playwright,
	}) => {
		const { username } = await newUser(request, playwright, "TOKEN");
		const setup = await rawLogin(playwright, username);
		const mfa = await enrollMfa(setup.ctx);
		await setup.ctx.dispose();

		const { ctx, response } = await rawLogin(playwright, username);
		const challenge = (await response.json()).data.mfa_challenge as string;

		// As a bearer credential it is rejected by request auth.
		const asBearer = await ctx.get(`${API_URL}/users/me`, {
			headers: { Authorization: `Bearer ${challenge}` },
		});
		expect(asBearer.status()).toBe(401);

		const first = await verifyMfa(ctx, challenge, await freshCode(mfa.secret));
		expect(first.ok()).toBeTruthy();
		// The same challenge cannot start a second session.
		const other = await playwright.request.newContext();
		const reuse = await verifyMfa(
			other,
			challenge,
			await freshCode(mfa.secret),
		);
		expect(reuse.status()).toBe(401);
		await other.dispose();
		await ctx.dispose();
	});

	test("a code that was already used is rejected", async ({
		request,
		playwright,
	}) => {
		const { username } = await newUser(request, playwright, "REPLAY");
		const setup = await rawLogin(playwright, username);
		const mfa = await enrollMfa(setup.ctx);
		await setup.ctx.dispose();

		const code = await freshCode(mfa.secret);
		const a = await rawLogin(playwright, username);
		const first = await verifyMfa(
			a.ctx,
			(await a.response.json()).data.mfa_challenge,
			code,
		);
		expect(first.ok()).toBeTruthy();

		const b = await rawLogin(playwright, username);
		const replay = await verifyMfa(
			b.ctx,
			(await b.response.json()).data.mfa_challenge,
			code,
		);
		expect(replay.status()).toBe(401);
		expect(await errorCode(replay)).toBe("AUTH_MFA_INVALID_CODE");
		await a.ctx.dispose();
		await b.ctx.dispose();
	});

	test("a recovery code works once", async ({ request, playwright }) => {
		const { username } = await newUser(request, playwright, "RECOVERY");
		const setup = await rawLogin(playwright, username);
		const mfa = await enrollMfa(setup.ctx);
		const [recovery] = mfa.recoveryCodes;

		const a = await rawLogin(playwright, username);
		const ok = await verifyMfa(
			a.ctx,
			(await a.response.json()).data.mfa_challenge,
			recovery,
		);
		expect(ok.ok()).toBeTruthy();
		const left = await a.ctx.get(`${API_URL}/users/me/mfa`);
		expect((await left.json()).data.recovery_codes_left).toBe(7);

		const b = await rawLogin(playwright, username);
		const reuse = await verifyMfa(
			b.ctx,
			(await b.response.json()).data.mfa_challenge,
			recovery,
		);
		expect(reuse.status()).toBe(401);
		await Promise.all([setup.ctx, a.ctx, b.ctx].map((c) => c.dispose()));
	});

	test("five wrong codes lock the second factor, and an admin reset clears it", async ({
		request,
		playwright,
	}) => {
		const { username, id } = await newUser(request, playwright, "LOCK");
		const setup = await rawLogin(playwright, username);
		const mfa = await enrollMfa(setup.ctx);
		await setup.ctx.dispose();

		const { ctx, response } = await rawLogin(playwright, username);
		const challenge = (await response.json()).data.mfa_challenge as string;
		for (let i = 0; i < 4; i++) {
			const r = await verifyMfa(ctx, challenge, "000000");
			expect(r.status()).toBe(401);
		}
		const tripped = await verifyMfa(ctx, challenge, "000000");
		expect(tripped.status()).toBe(429);
		expect(Number(tripped.headers()["retry-after"])).toBeGreaterThan(0);

		// Even the right code is refused during the lockout.
		const refused = await verifyMfa(
			ctx,
			challenge,
			await freshCode(mfa.secret),
		);
		expect(refused.status()).toBe(429);

		// An admin clears the enrollment; sign-in is password-only again.
		await authRequest(request);
		const reset = await request.delete(`${API_URL}/admin/users/${id}/mfa`);
		expect(reset.status()).toBe(204);
		const plain = await rawLogin(playwright, username);
		expect(plain.response.ok()).toBeTruthy();
		expect((await plain.response.json()).data.mfa_required).toBeUndefined();
		await plain.ctx.dispose();
		await ctx.dispose();
	});
});

test.describe("Managing your own two-step verification", () => {
	test("disabling and regenerating recovery codes need a current code", async ({
		request,
		playwright,
	}) => {
		const { username } = await newUser(request, playwright, "MANAGE");
		const setup = await rawLogin(playwright, username);
		const mfa = await enrollMfa(setup.ctx);
		const ctx = setup.ctx;

		const noCode = await ctx.post(`${API_URL}/users/me/mfa/recovery-codes`, {
			data: { code: "000000" },
		});
		expect(noCode.status()).toBe(401);

		const regen = await ctx.post(`${API_URL}/users/me/mfa/recovery-codes`, {
			data: { code: await freshCode(mfa.secret) },
		});
		expect(regen.ok()).toBeTruthy();
		const fresh = (await regen.json()).data.recovery_codes as string[];
		expect(fresh).toHaveLength(8);
		expect(fresh.some((c) => mfa.recoveryCodes.includes(c))).toBe(false);

		// An old recovery code no longer disables anything.
		const stale = await ctx.post(`${API_URL}/users/me/mfa/disable`, {
			data: { code: mfa.recoveryCodes[0] },
		});
		expect(stale.status()).toBe(401);

		const off = await ctx.post(`${API_URL}/users/me/mfa/disable`, {
			data: { code: fresh[0] },
		});
		expect(off.status()).toBe(204);
		const status = await ctx.get(`${API_URL}/users/me/mfa`);
		expect((await status.json()).data.enabled).toBe(false);

		const plain = await rawLogin(playwright, username);
		expect((await plain.response.json()).data.mfa_required).toBeUndefined();
		await plain.ctx.dispose();
		await ctx.dispose();
	});
});

test.describe("IP allow-lists", () => {
	test("an account restricted to other addresses cannot sign in with its password", async ({
		request,
		playwright,
	}) => {
		const { username, id } = await newUser(request, playwright, "CIDR");
		await authRequest(request);
		const saved = await setUserCidrs(request, id, ["192.0.2.0/24"]);
		expect(saved.ok()).toBeTruthy();
		expect((await saved.json()).data.allowed_cidrs).toEqual(["192.0.2.0/24"]);

		const blocked = await rawLogin(playwright, username);
		expect(blocked.response.status()).toBe(403);
		expect(await errorCode(blocked.response)).toBe("AUTH_IP_NOT_ALLOWED");

		// A wrong password still reads as plain bad credentials: the allow-list
		// is only revealed to someone who knows the password.
		const wrongPw = await rawLogin(playwright, username, "Wrong-Password-1");
		expect(wrongPw.response.status()).toBe(401);
		expect(await errorCode(wrongPw.response)).toBe("AUTH_INVALID_CREDENTIALS");

		// Lists accept bare addresses and any range that covers the caller.
		await setUserCidrs(request, id, ["0.0.0.0/0", "::/0"]);
		const open = await rawLogin(playwright, username);
		expect(open.response.ok()).toBeTruthy();

		const bad = await setUserCidrs(request, id, ["not-an-ip"]);
		expect(bad.status()).toBe(400);
		expect(await errorCode(bad)).toBe("INVALID_CIDR");

		await Promise.all(
			[blocked.ctx, wrongPw.ctx, open.ctx].map((c) => c.dispose()),
		);
	});

	test("an admin cannot save a list that excludes their own address", async ({
		request,
	}) => {
		await authRequest(request);
		const me = await request.get(`${API_URL}/users/me`);
		const myId = (await me.json()).data.id as string;

		const own = await setUserCidrs(request, myId, ["192.0.2.0/24"]);
		expect(own.status()).toBe(400);
		expect(await errorCode(own)).toBe("SELF_LOCKOUT");

		const instance = await putPolicy(request, {
			...DEFAULT_POLICY,
			ip_allow_list: ["192.0.2.0/24"],
		});
		expect(instance.status()).toBe(400);
		expect(await errorCode(instance)).toBe("SELF_LOCKOUT");
	});
});

test.describe("Requiring two-step verification", () => {
	test("protected accounts must set it up before they can sign in", async ({
		request,
		playwright,
	}) => {
		const roleName = `${ROLE_PREFIX}${RUN_ID}`;
		await authRequest(request);
		await createGlobalRole(request, roleName, allowPolicy([]));
		const protectedUser = await newUser(request, playwright, "PROTECTED", [
			roleName,
		]);
		const bystander = await newUser(request, playwright, "BYSTANDER");

		const saved = await putPolicy(request, {
			privileged_roles: [roleName],
			require_mfa: true,
			ip_allow_list: [],
		});
		expect(saved.ok()).toBeTruthy();

		// Someone outside the protected roles is unaffected, and may not use the
		// password-only enrollment route.
		const free = await rawLogin(playwright, bystander.username);
		expect(free.response.ok()).toBeTruthy();
		const refused = await free.ctx.post(`${API_URL}/auth/mfa/setup`, {
			data: { username: bystander.username, password: RESTRICTED_PASSWORD },
		});
		expect(refused.status()).toBe(403);
		await free.ctx.dispose();

		const blocked = await rawLogin(playwright, protectedUser.username);
		expect(blocked.response.status()).toBe(403);
		expect(await errorCode(blocked.response)).toBe("AUTH_MFA_SETUP_REQUIRED");

		// Enroll with the password, as the sign-in page does.
		const anon = await playwright.request.newContext();
		const creds = {
			username: protectedUser.username,
			password: RESTRICTED_PASSWORD,
		};
		const begin = await anon.post(`${API_URL}/auth/mfa/setup`, { data: creds });
		expect(begin.ok()).toBeTruthy();
		const secret = (await begin.json()).data.secret as string;
		const wrong = await anon.post(`${API_URL}/auth/mfa/setup/confirm`, {
			data: { ...creds, code: "000000" },
		});
		expect(wrong.status()).toBe(401);
		const confirm = await anon.post(`${API_URL}/auth/mfa/setup/confirm`, {
			data: { ...creds, code: await freshCode(secret) },
		});
		expect(confirm.ok()).toBeTruthy();
		expect((await confirm.json()).data.recovery_codes).toHaveLength(8);

		// Now it signs in with a code, and setup can't be redone with a password.
		const ctx = await signInWithMfa(
			playwright,
			protectedUser.username,
			await freshCode(secret),
		);
		expect((await ctx.get(`${API_URL}/users/me`)).ok()).toBeTruthy();
		const redo = await anon.post(`${API_URL}/auth/mfa/setup`, { data: creds });
		expect(redo.status()).toBe(403);
		await anon.dispose();
		await ctx.dispose();
	});
});

test.describe("Who may change login security", () => {
	test("the policy and per-user controls are not available to ordinary admins", async ({
		request,
		playwright,
	}) => {
		const admin = await newUser(request, playwright, "ADMIN", ["ADMIN"]);
		const target = await newUser(request, playwright, "TARGET");
		const { ctx } = await rawLogin(playwright, admin.username);

		expect((await ctx.get(`${API_URL}/admin/auth-security`)).status()).toBe(
			403,
		);
		expect(
			(
				await ctx.put(`${API_URL}/admin/auth-security`, {
					data: DEFAULT_POLICY,
				})
			).status(),
		).toBe(403);
		expect(
			(
				await ctx.get(`${API_URL}/admin/users/${target.id}/allowed-cidrs`)
			).status(),
		).toBe(403);
		expect(
			(await ctx.delete(`${API_URL}/admin/users/${target.id}/mfa`)).status(),
		).toBe(403);
		await ctx.dispose();
	});
});

test.describe("Repeated wrong passwords", () => {
	test("the sign-in is locked after too many failures, even for unknown names", async ({
		request,
		playwright,
	}) => {
		const { username } = await newUser(request, playwright, "THROTTLE");
		const ctx = await playwright.request.newContext();
		let last = 0;
		for (let i = 0; i < 10; i++) {
			const r = await ctx.post(`${API_URL}/auth/login`, {
				data: { username, password: `Wrong-Password-${i}` },
			});
			last = r.status();
		}
		expect(last).toBe(429);
		const right = await ctx.post(`${API_URL}/auth/login`, {
			data: { username, password: RESTRICTED_PASSWORD },
		});
		expect(right.status()).toBe(429);

		// A name that doesn't exist locks the same way, so it proves nothing.
		const ghost = uniqueName("GHOST");
		let ghostLast = 0;
		for (let i = 0; i < 10; i++) {
			const r = await ctx.post(`${API_URL}/auth/login`, {
				data: { username: ghost, password: `Wrong-Password-${i}` },
			});
			ghostLast = r.status();
		}
		expect(ghostLast).toBe(429);
		await ctx.dispose();
	});
});

// ─── Browser flows ───────────────────────────────────────────────────────────

async function openSecurity(page: Page) {
	await page.goto(`${BASE_URL}/account/security`);
	await expect(page.getByRole("heading", { name: "Security" })).toBeVisible();
}

test.describe("In the browser", () => {
	test("a user turns two-step verification on, signs in with it, and turns it off", async ({
		page,
		request,
		playwright,
	}) => {
		const { username } = await newUser(request, playwright, "UI");
		await signIn(page, username, RESTRICTED_PASSWORD);
		await openSecurity(page);

		await page
			.getByRole("button", { name: "Set up two-step verification" })
			.click();
		const dialog = page.getByRole("dialog");
		const secret = (
			await dialog
				.getByLabel("Copy setup key")
				.locator("span")
				.first()
				.innerText()
		).trim();
		expect(secret).toMatch(/^[A-Z2-7]{16,}$/);

		// A wrong code is explained and keeps the dialog open.
		await dialog
			.getByLabel("Enter the 6-digit code from the app")
			.fill("000000");
		await dialog.getByRole("button", { name: "Verify and turn on" }).click();
		await expect(
			dialog.getByText("That code is invalid or was already used."),
		).toBeVisible();

		await dialog
			.getByLabel("Enter the 6-digit code from the app")
			.fill(await freshCode(secret));
		await dialog.getByRole("button", { name: "Verify and turn on" }).click();
		await expect(
			dialog.getByText(
				"These codes are shown only now. Store them somewhere safe before closing.",
			),
		).toBeVisible();
		const codeTexts = await dialog.locator("li, code").allInnerTexts();
		const recovery = codeTexts
			.map((t) => t.trim())
			.filter((t) => /^[a-z0-9]{5}-[a-z0-9]{5}$/.test(t));
		expect(recovery.length).toBe(8);
		await dialog.getByRole("button", { name: "I've saved them" }).click();
		await expect(page.getByText("Recovery codes remaining: 8")).toBeVisible();

		// Sign out, then back in: password, then the code page.
		await page.context().clearCookies();
		await page.goto(`${BASE_URL}/`);
		await page.getByRole("textbox", { name: "Username" }).fill(username);
		await page
			.getByRole("textbox", { name: "Password" })
			.fill(RESTRICTED_PASSWORD);
		await page.getByRole("button", { name: "Sign in" }).click();
		await expect(page).toHaveURL(/\/login\/mfa/);
		await expect(
			page.getByRole("heading", { name: "Enter your code" }),
		).toBeVisible();

		await page.getByLabel("Authentication code").fill("000000");
		await page.getByRole("button", { name: "Verify" }).click();
		await expect(
			page.getByText(
				"That code is invalid or was already used. Please try again.",
			),
		).toBeVisible();

		await page.getByLabel("Authentication code").fill(await freshCode(secret));
		await page.getByRole("button", { name: "Verify" }).click();
		await expect(
			page.getByRole("heading", { name: /Good (morning|afternoon|evening)/i }),
		).toBeVisible();

		// Turn it off again with a recovery code.
		await openSecurity(page);
		await page.getByRole("button", { name: "Turn off" }).first().click();
		const off = page.getByRole("dialog");
		await off
			.getByLabel("Authentication code or recovery code")
			.fill(recovery[0]);
		await off.getByRole("button", { name: "Turn off" }).click();
		await expect(
			page.getByRole("button", { name: "Set up two-step verification" }),
		).toBeVisible();
	});

	test("a recovery code signs in from the code page", async ({
		page,
		request,
		playwright,
	}) => {
		const { username } = await newUser(request, playwright, "UIREC");
		const setup = await rawLogin(playwright, username);
		const mfa = await enrollMfa(setup.ctx);
		await setup.ctx.dispose();

		await page.goto(`${BASE_URL}/`);
		await page.getByRole("textbox", { name: "Username" }).fill(username);
		await page
			.getByRole("textbox", { name: "Password" })
			.fill(RESTRICTED_PASSWORD);
		await page.getByRole("button", { name: "Sign in" }).click();
		await expect(page).toHaveURL(/\/login\/mfa/);
		await page.getByLabel("Authentication code").fill(mfa.recoveryCodes[0]);
		await page.getByRole("button", { name: "Verify" }).click();
		await expect(
			page.getByRole("heading", { name: /Good (morning|afternoon|evening)/i }),
		).toBeVisible();
	});

	test("a protected user is walked through setup on the sign-in page", async ({
		page,
		request,
		playwright,
	}) => {
		const roleName = `${ROLE_PREFIX}${RUN_ID}_UI`;
		await authRequest(request);
		await createGlobalRole(request, roleName, allowPolicy([]));
		const { username } = await newUser(request, playwright, "FORCED", [
			roleName,
		]);
		const saved = await putPolicy(request, {
			privileged_roles: [roleName],
			require_mfa: true,
			ip_allow_list: [],
		});
		expect(saved.ok()).toBeTruthy();

		await page.goto(`${BASE_URL}/`);
		await page.getByRole("textbox", { name: "Username" }).fill(username);
		await page
			.getByRole("textbox", { name: "Password" })
			.fill(RESTRICTED_PASSWORD);
		await page.getByRole("button", { name: "Sign in" }).click();

		await expect(
			page.getByRole("heading", { name: "Set up two-step verification" }),
		).toBeVisible();
		const secret = (
			await page.locator("p.select-all").first().innerText()
		).trim();
		expect(secret).toMatch(/^[A-Z2-7]{16,}$/);
		await page
			.getByLabel("Enter the 6-digit code from the app")
			.fill(await freshCode(secret));
		await page.getByRole("button", { name: "Verify and turn on" }).click();

		await expect(
			page.getByRole("heading", { name: "Save your recovery codes" }),
		).toBeVisible();
		await page.getByRole("button", { name: "Continue to sign in" }).click();

		await expect(page).toHaveURL(/\/login\/mfa/);
		await page.getByLabel("Authentication code").fill(await freshCode(secret));
		await page.getByRole("button", { name: "Verify" }).click();
		await expect(
			page.getByRole("heading", { name: /Good (morning|afternoon|evening)/i }),
		).toBeVisible();
	});

	test("an admin edits a user's allowed addresses and resets their two-step verification", async ({
		page,
		request,
		playwright,
	}) => {
		const target = await newUser(request, playwright, "ADMINUI");
		const setup = await rawLogin(playwright, target.username);
		await enrollMfa(setup.ctx);
		await setup.ctx.dispose();

		await signIn(page);
		await page.goto(`${BASE_URL}/admin/users`);
		const row = page.getByRole("row").filter({
			has: page.getByText(target.username, { exact: true }),
		});
		await row.getByRole("button", { name: "Security" }).click();
		const dialog = page.getByRole("dialog");
		await expect(dialog).toContainText("Sign-in protections for");

		await dialog.getByLabel("Allowed IP addresses").fill("not-an-ip");
		await dialog.getByRole("button", { name: "Save" }).click();
		await expect(
			dialog.getByText(
				"One of the entries is not a valid IP address or CIDR range.",
			),
		).toBeVisible();

		await dialog.getByLabel("Allowed IP addresses").fill("192.0.2.0/24");
		await dialog.getByRole("button", { name: "Save" }).click();
		await expect(dialog).toBeHidden();
		await authRequest(request);
		const list = await request.get(
			`${API_URL}/admin/users/${target.id}/allowed-cidrs`,
		);
		expect((await list.json()).data.allowed_cidrs).toEqual(["192.0.2.0/24"]);

		await row.getByRole("button", { name: "Security" }).click();
		await dialog
			.getByRole("button", { name: "Reset two-step verification" })
			.click();
		await dialog.getByRole("button", { name: "Yes, reset" }).click();
		await expect(
			dialog.getByText(
				"Two-step verification was reset. They can set it up again at their next sign-in.",
			),
		).toBeVisible();
		const status = await rawLogin(playwright, target.username);
		// The address list now blocks them, but the second factor is gone.
		expect(await errorCode(status.response)).toBe("AUTH_IP_NOT_ALLOWED");
		await status.ctx.dispose();
	});

	test("the login security settings explain a list that would lock the admin out", async ({
		page,
	}) => {
		await signIn(page);
		await page.goto(`${BASE_URL}/admin/settings`);
		await page.getByRole("link", { name: "Login security" }).click();
		await expect(
			page.getByRole("heading", { name: "Require two-step verification" }),
		).toBeVisible();
		await page.getByLabel("Allowed IP addresses").fill("192.0.2.0/24");
		await expect(
			page.getByText("Make sure your own address is included"),
		).toBeVisible();
		await page.getByRole("button", { name: "Save" }).click();
		await expect(
			page.getByText("Your own IP address must be on the list"),
		).toBeVisible();
	});
});
