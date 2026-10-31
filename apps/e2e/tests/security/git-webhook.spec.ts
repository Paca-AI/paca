// spec: features/security/git-webhook.feature
// seed: tests/seed.spec.ts

import { expect, test } from "@playwright/test";
import { API_URL, authRequest } from "../helpers/e2e-api";
import { anonymous } from "../helpers/git";
import { errorCode } from "../helpers/iam";

const hook = (key: string) => `${API_URL}/git/webhooks/${key}`;

test.describe("Git webhook intake", () => {
	test("An unknown endpoint key is a 404 GIT_NOT_FOUND whatever the payload", async ({
		playwright,
	}) => {
		await anonymous(playwright, async (api) => {
			const deliveries: Array<{
				data: unknown;
				headers?: Record<string, string>;
			}> = [
				{ data: { zen: "Keep it logically awesome." } },
				{ data: "" },
				{ data: "not json at all", headers: { "content-type": "text/plain" } },
				{
					data: { action: "opened" },
					headers: { "x-github-event": "pull_request" },
				},
				{
					data: { action: "opened" },
					headers: {
						"x-github-event": "pull_request",
						"x-hub-signature-256": `sha256=${"0".repeat(64)}`,
						"x-github-delivery": "00000000-0000-4000-8000-000000000001",
					},
				},
			];
			for (const [i, delivery] of deliveries.entries()) {
				const response = await api.post(hook("e2e-no-such-endpoint"), delivery);
				expect(response.status(), `delivery ${i}`).toBe(404);
				expect(await errorCode(response), `delivery ${i}`).toBe(
					"GIT_NOT_FOUND",
				);
			}
		});
	});

	for (const method of ["GET", "PUT", "DELETE", "PATCH"] as const) {
		test(`${method} is not accepted`, async ({ playwright }) => {
			await anonymous(playwright, async (api) => {
				const response = await api.fetch(hook("e2e-no-such-endpoint"), {
					method,
				});
				expect(response.status()).toBe(405);
			});
		});
	}

	test("A key with the same shape as a real one is still unknown", async ({
		playwright,
	}) => {
		await anonymous(playwright, async (api) => {
			const key = "a".repeat(48);
			expect((await api.post(hook(key), { data: {} })).status()).toBe(404);
		});
	});

	test("Odd keys do not crash the intake", async ({ playwright }) => {
		await anonymous(playwright, async (api) => {
			const keys = [
				"a".repeat(1500),
				"%20%20",
				"..%2F..%2Fadmin",
				"a%00b",
				"' OR '1'='1",
				"%E2%98%83",
			];
			for (const key of keys) {
				const response = await api.post(hook(key), { data: {} });
				expect(response.status(), key).toBeGreaterThanOrEqual(400);
				expect(response.status(), key).toBeLessThan(500);
			}
		});
	});

	test("The webhook needs no session but the other Git routes do", async ({
		playwright,
	}) => {
		await anonymous(playwright, async (api) => {
			expect((await api.post(hook("e2e-none"), { data: {} })).status()).toBe(
				404,
			);
			expect((await api.get(`${API_URL}/git/identities`)).status()).toBe(401);
			expect((await api.get(`${API_URL}/git/providers`)).status()).toBe(401);
		});
	});

	test("A browser cannot call a provider's internal operations", async ({
		request,
	}) => {
		await authRequest(request);
		for (const op of ["ping", "mint_credential", "verify_and_parse_webhook"]) {
			const response = await request.post(
				`${API_URL}/plugins/com.paca.github/_git/${op}`,
				{ data: {} },
			);
			expect(response.status(), op).not.toBe(200);
			expect(response.status(), op).toBeLessThan(500);
		}
	});
});
