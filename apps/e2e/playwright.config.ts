import { defineConfig, devices } from "@playwright/test";
import path from "node:path";

/**
 * Path to the saved browser auth state produced by `global-setup.ts`.
 * Session tests load this file via `test.use({ storageState: AUTH_FILE })`.
 */
export const AUTH_FILE = path.join(__dirname, "playwright/.auth/user.json");

/**
 * See https://playwright.dev/docs/test-configuration.
 *
 * Environment variables (see .env.example):
 *   E2E_BASE_URL — base URL of the running app  (default: http://localhost)
 *   E2E_USERNAME — test user username            (default: admin)
 *   E2E_PASSWORD — test user password            (default: e2e-admin-password)
 */
export default defineConfig({
	testDir: "./tests",

	fullyParallel: true,
	forbidOnly: !!process.env.CI,
	retries: process.env.CI ? 2 : 0,
	/* Limit parallelism to reduce resource contention; keep 1 on CI, 1 locally for better test stability. */
	workers: 1,

	reporter: [["html"], ["list"]],

	use: {
		baseURL: process.env.E2E_BASE_URL ?? "http://localhost",
		trace: "on-first-retry",
		screenshot: "only-on-failure",
		actionTimeout: 15_000,
	},

	/* Logs in once and persists the auth state for session tests. */
	globalSetup: "./global-setup.ts",

	projects: [
		/* Desktop browsers */
		{
			name: "chromium",
			use: { ...devices["Desktop Chrome"] },
		},
		{
			name: "firefox",
			use: { ...devices["Desktop Firefox"] },
		},
		{
			name: "webkit",
			use: { ...devices["Desktop Safari"] },
		},

		/* Mobile browsers */
		{
			name: "mobile-chrome",
			use: { ...devices["Pixel 5"] },
		},
		{
			name: "mobile-safari",
			use: { ...devices["iPhone 12"] },
		},
	],
});
