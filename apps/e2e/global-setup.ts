import { chromium } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";

const AUTH_FILE = path.join(__dirname, "playwright/.auth/user.json");

/**
 * Global setup — runs once before the entire test suite.
 *
 * Logs in with the configured credentials, persists the browser auth state
 * to `playwright/.auth/user.json`, and closes the browser.  Session tests
 * that need a pre-authenticated context load this file via `storageState`.
 *
 * Required env vars (see `.env.example`):
 *   E2E_BASE_URL  — defaults to http://localhost
 *   E2E_USERNAME  — defaults to admin
 *   E2E_PASSWORD  — defaults to e2e-admin-password
 */
export default async function globalSetup() {
	const baseURL = process.env.E2E_BASE_URL ?? "http://localhost";
	const username = process.env.E2E_USERNAME ?? "admin";
	const password = process.env.E2E_PASSWORD ?? "e2e-admin-password";

	// Ensure the target directory exists before Playwright tries to write the file.
	fs.mkdirSync(path.dirname(AUTH_FILE), { recursive: true });

	const browser = await chromium.launch();
	const context = await browser.newContext();
	const page = await context.newPage();

	await page.goto(`${baseURL}/`);
	await page.getByRole("textbox", { name: "Username" }).fill(username);
	await page.getByRole("textbox", { name: "Password" }).fill(password);
	await page.getByRole("button", { name: "Sign in" }).click();
	await page.waitForURL(/\/dashboard/);

	await context.storageState({ path: AUTH_FILE });
	await browser.close();
}
