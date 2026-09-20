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
 *   E2E_WORKERS  — parallel workers               (default: 3 locally, 2 on CI)
 */
export default defineConfig({
	testDir: "./tests",

	/*
	 * Spec files are spread across `workers` parallel workers; the tests inside
	 * one file always run in order on a single worker. Specs are written to be
	 * independent of each other so any file can run on any worker:
	 *   - every spec creates and cleans up its own data under a prefix that no
	 *     other spec shares (E2E_<AREA>_...), and signs in with its own session;
	 *   - instance-wide state (branding, global agents, user totals) is either
	 *     stubbed with page.route or asserted without assuming nobody else is
	 *     changing it.
	 * Browser projects must NOT run at the same time, because the same spec in
	 * two browsers shares its prefix - `bun run test` runs them one after another.
	 * Override the worker count with E2E_WORKERS.
	 */
	fullyParallel: false,
	forbidOnly: !!process.env.CI,
	/* 1 retry locally absorbs minor race-conditions; 2 on CI for reliability. */
	retries: process.env.CI ? 2 : 1,
	workers: Number(process.env.E2E_WORKERS) || (process.env.CI ? 2 : 3),

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
