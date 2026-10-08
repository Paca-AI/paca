// spec: features/admin/sso.feature
// seed: tests/seed.spec.ts

import {
	type APIRequestContext,
	type Browser,
	expect,
	type Page,
	test,
} from "@playwright/test";
import {
	API_URL,
	authRequest,
	BASE_URL,
	cleanupGlobalRolesByPrefix,
	cleanupUsersByPrefix,
	createUserWithGlobalPermissions,
	ensureLoginForm,
	newRunId,
	RESTRICTED_PASSWORD,
	signIn,
} from "../helpers/e2e-api";

const PREFIX = "E2E_SSO_";
const RUN_ID = newRunId();
const run = RUN_ID.toLowerCase();
const SETTINGS_URL = `${BASE_URL}/admin/settings`;

// Accounts the provider creates are named after preferred_username, which
// the API lowercases.
const SSO_USER_PREFIX = PREFIX.toLowerCase();
const SLUG_PREFIX = "e2e-sso-";
const SLUG = `${SLUG_PREFIX}${run}`;
const PROVIDER_NAME = `E2E SSO ${RUN_ID}`;

// The API reaches the mock provider (deploy/docker-compose.e2e.yml,
// service mock-oidc) by its service name; the browser reaches it on the
// published port. mock-oauth2-server serves an independent issuer under any
// path, so each run gets its own.
const MOCK_OIDC_INTERNAL = "http://mock-oidc:9090";
const MOCK_OIDC_PUBLIC =
	process.env.E2E_MOCK_OIDC_URL ?? "http://localhost:9090";
const ISSUER_URL = `${MOCK_OIDC_INTERNAL}/${SLUG}`;

interface SsoProvider {
	id: string;
	slug: string;
	display_name: string;
	issuer_url: string;
	client_id: string;
	scopes: string[];
	enabled: boolean;
	auto_provision: boolean;
	link_by_email: boolean;
	allowed_domains: string[];
}

async function listProviders(
	request: APIRequestContext,
): Promise<SsoProvider[]> {
	await authRequest(request);
	const response = await request.get(`${API_URL}/admin/sso/providers`);
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data ?? [];
}

async function cleanupProviders(request: APIRequestContext): Promise<void> {
	const providers = await listProviders(request);
	await Promise.all(
		providers
			.filter((p) => p.slug.startsWith(SLUG_PREFIX))
			.map((p) => request.delete(`${API_URL}/admin/sso/providers/${p.id}`)),
	);
}

async function updateProvider(
	request: APIRequestContext,
	changes: Partial<SsoProvider>,
): Promise<void> {
	const provider = (await listProviders(request)).find((p) => p.slug === SLUG);
	expect(provider, `provider ${SLUG} should exist`).toBeTruthy();
	const response = await request.put(
		`${API_URL}/admin/sso/providers/${provider?.id}`,
		// A null client_secret keeps the stored one.
		{ data: { ...provider, ...changes, client_secret: null } },
	);
	expect(response.ok()).toBeTruthy();
}

/**
 * The browser can't resolve "mock-oidc", and Playwright can't intercept a
 * request the browser reaches by following a redirect. So the Paca sign-in
 * redirect itself is rewritten to point at the provider's published port.
 * The provider then redirects straight back to Paca, and the API redeems the
 * code at mock-oidc:9090 — mock-oauth2-server takes the ID token's issuer
 * from that request's Host, so it still matches the configured issuer.
 */
async function routeMockProvider(page: Page): Promise<void> {
	await page.route("**/api/v1/auth/sso/*/login*", async (route) => {
		const response = await route.fetch({ maxRedirects: 0 });
		const location = (response.headers().location ?? "").replace(
			MOCK_OIDC_INTERNAL,
			MOCK_OIDC_PUBLIC,
		);
		// WebKit refuses to fulfill a navigation with a redirect status, so
		// answer 200 with a meta refresh to the rewritten location instead.
		// The original headers include Set-Cookie: sso_state, which binds
		// the sign-in to this browser.
		const headers = response.headers();
		delete headers.location;
		delete headers["content-length"];
		headers["content-type"] = "text/html; charset=utf-8";
		const url = location.replaceAll("&", "&amp;").replaceAll('"', "&quot;");
		await route.fulfill({
			status: 200,
			headers,
			body: `<!doctype html><meta http-equiv="refresh" content="0;url=${url}">`,
		});
	});
}

/** A fresh, signed-out browser page that can talk to the mock provider. */
async function signedOutPage(browser: Browser): Promise<Page> {
	const context = await browser.newContext();
	const page = await context.newPage();
	await routeMockProvider(page);
	return page;
}

/**
 * Continues with the run's provider from the sign-in page and signs in at
 * the mock provider as `subject` with `claims`.
 */
async function continueWithSso(
	page: Page,
	subject: string,
	claims: Record<string, unknown>,
): Promise<void> {
	await page.goto(`${BASE_URL}/`);
	await ensureLoginForm(page);
	await page
		.getByRole("link", { name: `Continue with ${PROVIDER_NAME}` })
		.click();

	// mock-oauth2-server's interactive login form.
	const subjectField = page.locator('input[name="username"]');
	await expect(subjectField).toBeVisible();
	await subjectField.fill(subject);
	await page.locator('textarea[name="claims"]').fill(JSON.stringify(claims));
	await page.getByRole("button", { name: "Sign-in" }).click();
}

async function expectHome(page: Page): Promise<void> {
	await expect(
		page.getByRole("heading", { name: /Good (morning|afternoon|evening)/i }),
	).toBeVisible();
}

async function currentUser(
	page: Page,
): Promise<{ id: string; username: string; email?: string }> {
	// page.request shares the browser context's cookies, i.e. its session.
	const response = await page.request.get(`${API_URL}/users/me`);
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data;
}

test.describe("Single sign-on (SSO)", () => {
	test.describe.configure({ mode: "serial" });

	test.beforeAll(async ({ request }) => {
		await cleanupProviders(request);
	});

	test.afterAll(async ({ request }) => {
		await cleanupProviders(request);
		await cleanupUsersByPrefix(request, SSO_USER_PREFIX);
		await cleanupUsersByPrefix(request, PREFIX);
		await cleanupGlobalRolesByPrefix(request, PREFIX);
	});

	test.describe("Access is gated by the settings.sso.write permission", () => {
		async function signInWithPermissions(
			page: Page,
			request: APIRequestContext,
			playwright: Parameters<typeof createUserWithGlobalPermissions>[1],
			label: string,
			permissions: Record<string, boolean>,
		): Promise<void> {
			const username = `${PREFIX}${label}_${RUN_ID}`;
			await authRequest(request);
			await createUserWithGlobalPermissions(request, playwright, {
				username,
				roleName: `${PREFIX}ROLE_${label}_${RUN_ID}`,
				// projects.read lets the account reach the home page.
				permissions: { "projects:read": true, ...permissions },
			});
			await signIn(page, username, RESTRICTED_PASSWORD);
			await page.goto(SETTINGS_URL);
		}

		test("A user with only settings.write does not see the SSO section", async ({
			page,
			request,
			playwright,
		}) => {
			await signInWithPermissions(page, request, playwright, "BRAND", {
				"settings:write": true,
			});
			await expect(
				page.getByRole("heading", { name: "Logo & Favicon" }),
			).toBeVisible();
			await expect(
				page.getByRole("heading", { name: "Single sign-on (SSO)" }),
			).toHaveCount(0);
		});

		test("A user with only settings.sso.write sees the SSO section but not branding", async ({
			page,
			request,
			playwright,
		}) => {
			await signInWithPermissions(page, request, playwright, "SSOADMIN", {
				"settings.sso:write": true,
			});
			await expect(
				page.getByRole("heading", { name: "Single sign-on (SSO)" }),
			).toBeVisible();
			await expect(
				page.getByRole("heading", { name: "Logo & Favicon" }),
			).toHaveCount(0);
		});
	});

	test.describe("An administrator manages providers, and users sign in with them", () => {
		const subject = `${run}-jane`;
		const username = `${SSO_USER_PREFIX}${run}_jane`;
		const email = `${username}@corp.example`;
		let janeId = "";

		test("An administrator adds a provider", async ({ page }) => {
			await signIn(page);
			await page.goto(SETTINGS_URL);
			await page.getByRole("button", { name: "Add provider" }).click();

			const dialog = page.getByRole("dialog", { name: "Add SSO provider" });
			await dialog.getByLabel("Display name").fill(PROVIDER_NAME);
			await dialog.getByLabel("Slug").fill(SLUG);
			await expect(dialog.getByLabel("Callback URL")).toHaveValue(
				`${BASE_URL}/api/v1/auth/sso/${SLUG}/callback`,
			);
			await dialog.getByLabel("Issuer URL").fill(ISSUER_URL);
			await dialog.getByLabel("Client ID").fill("paca-e2e");
			await dialog.getByLabel("Client secret").fill("e2e-client-secret");
			await dialog.getByRole("button", { name: "Save" }).click();
			await expect(dialog).toBeHidden();

			const row = page.getByRole("listitem").filter({ hasText: PROVIDER_NAME });
			await expect(row).toBeVisible();
			await expect(row.getByText("Enabled", { exact: true })).toBeVisible();
		});

		test("The sign-in page offers enabled providers", async ({ browser }) => {
			const page = await signedOutPage(browser);
			await page.goto(`${BASE_URL}/`);
			await ensureLoginForm(page);
			await expect(
				page.getByRole("link", { name: `Continue with ${PROVIDER_NAME}` }),
			).toBeVisible();
			await page.context().close();
		});

		test("A first-time user gets a new account", async ({ browser }) => {
			const page = await signedOutPage(browser);
			await continueWithSso(page, subject, {
				email,
				email_verified: true,
				name: "Jane Sso",
				preferred_username: username,
			});
			await expectHome(page);

			const me = await currentUser(page);
			expect(me.username).toBe(username);
			expect(me.email).toBe(email);
			janeId = me.id;
			await page.context().close();
		});

		test("A returning user signs back in to the same account", async ({
			browser,
		}) => {
			const page = await signedOutPage(browser);
			await continueWithSso(page, subject, {
				email,
				email_verified: true,
				preferred_username: username,
			});
			await expectHome(page);

			expect((await currentUser(page)).id).toBe(janeId);
			await page.context().close();
		});

		test("An email outside the allowed domains is refused", async ({
			browser,
			request,
		}) => {
			await updateProvider(request, { allowed_domains: ["corp.example"] });

			const page = await signedOutPage(browser);
			await continueWithSso(page, `${run}-mallory`, {
				email: `${SSO_USER_PREFIX}${run}_mallory@elsewhere.example`,
				email_verified: true,
			});
			await expect(page).toHaveURL(/[?&]sso_error=email_not_allowed/);
			await expect(page.getByRole("alert")).toContainText(
				"Your email address isn't allowed to sign in here.",
			);
			await page.context().close();
		});

		test("A deleted provider disappears from the sign-in page", async ({
			page,
			browser,
		}) => {
			await signIn(page);
			await page.goto(SETTINGS_URL);
			await page
				.getByRole("button", { name: `Delete ${PROVIDER_NAME}` })
				.click();
			const dialog = page.getByRole("dialog", {
				name: "Delete SSO provider",
			});
			await dialog.getByRole("button", { name: "Delete" }).click();
			await expect(dialog).toBeHidden();
			await expect(page.getByText(PROVIDER_NAME)).toHaveCount(0);

			const visitor = await signedOutPage(browser);
			await visitor.goto(`${BASE_URL}/`);
			await ensureLoginForm(visitor);
			await expect(
				visitor.getByRole("link", { name: `Continue with ${PROVIDER_NAME}` }),
			).toHaveCount(0);
			await visitor.context().close();
		});
	});
});
