// spec: features/admin/changelog.feature
// seed: tests/seed.spec.ts

import { expect, type Page, test } from "@playwright/test";
import { BASE_URL, signIn } from "../helpers/e2e-api";

const CHANGELOG_URL = `${BASE_URL}/admin/changelog`;

// There is no lever to seed GitHub release data, so every test stubs the
// backend's GET /api/v1/releases response. The route must be registered
// before the page navigates.
const RELEASES_ROUTE = "**/api/v1/releases";

interface StubRelease {
	tag: string;
	name: string;
	url: string;
	publishedAt: string;
	body: string;
	isCurrent: boolean;
}

const RELEASE_CURRENT: StubRelease = {
	tag: "v1.2.0",
	name: "Paca 1.2.0",
	url: "https://github.com/paca-ai/paca/releases/tag/v1.2.0",
	// Noon UTC keeps the rendered calendar day stable across time zones.
	publishedAt: "2026-03-04T12:00:00Z",
	body: [
		"## Features",
		"- Added **dark mode**",
		"- Fixed `login` bug",
		"",
		"See [docs](https://example.com/docs) for details.",
	].join("\n"),
	isCurrent: true,
};

const RELEASE_PREVIOUS: StubRelease = {
	tag: "v1.1.0",
	name: "Paca 1.1.0",
	url: "https://github.com/paca-ai/paca/releases/tag/v1.1.0",
	publishedAt: "2026-02-01T12:00:00Z",
	body: "Maintenance release.",
	isCurrent: false,
};

async function stubReleases(page: Page, releases: StubRelease[]) {
	await page.route(RELEASES_ROUTE, (route) =>
		route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify({
				success: true,
				data: {
					current: releases.find((r) => r.isCurrent)?.tag ?? "",
					repo: "paca-ai/paca",
					releases,
				},
			}),
		}),
	);
}

async function openChangelog(page: Page) {
	await signIn(page);
	await page.goto(CHANGELOG_URL);
	await expect(page.getByRole("heading", { name: "What's New" })).toBeVisible();
}

const releaseCard = (page: Page, name: string) =>
	page
		.getByRole("article")
		.filter({ has: page.getByRole("heading", { name, exact: true }) });

// ===========================================================================
// Rule: Release list
// ===========================================================================

test.describe("Changelog release list", () => {
	test("The changelog page shows its title and description", async ({
		page,
	}) => {
		await stubReleases(page, [RELEASE_CURRENT, RELEASE_PREVIOUS]);
		await openChangelog(page);

		await expect(
			page.getByText(
				"Follow the improvements and highlights from the latest Paca releases.",
			),
		).toBeVisible();
	});

	test("Releases are listed in the order returned by the API", async ({
		page,
	}) => {
		await stubReleases(page, [RELEASE_CURRENT, RELEASE_PREVIOUS]);
		await openChangelog(page);

		await expect(page.getByRole("heading", { level: 2 })).toHaveText([
			"Paca 1.2.0",
			"Paca 1.1.0",
		]);
	});

	test("Only the running release carries the Current version badge", async ({
		page,
	}) => {
		await stubReleases(page, [RELEASE_CURRENT, RELEASE_PREVIOUS]);
		await openChangelog(page);

		await expect(
			releaseCard(page, "Paca 1.2.0").getByText("Current version"),
		).toBeVisible();
		await expect(
			releaseCard(page, "Paca 1.1.0").getByText("Current version"),
		).toHaveCount(0);
		await expect(page.getByText("Current version")).toHaveCount(1);
	});

	test("A release card shows its publish date and a link to the release", async ({
		page,
	}) => {
		await stubReleases(page, [RELEASE_CURRENT, RELEASE_PREVIOUS]);
		await openChangelog(page);

		const card = releaseCard(page, "Paca 1.2.0");
		await expect(card.getByText("Mar 4, 2026")).toBeVisible();
		await expect(card.getByRole("link", { name: "v1.2.0" })).toHaveAttribute(
			"href",
			RELEASE_CURRENT.url,
		);
	});

	test("Release notes render headings, bullet lists, bold, inline code and links", async ({
		page,
	}) => {
		await stubReleases(page, [RELEASE_CURRENT, RELEASE_PREVIOUS]);
		await openChangelog(page);

		const card = releaseCard(page, "Paca 1.2.0");
		await expect(
			card.getByRole("heading", { level: 3, name: "Features" }),
		).toBeVisible();
		await expect(card.getByRole("listitem")).toHaveText([
			"Added dark mode",
			"Fixed login bug",
		]);
		await expect(card.locator("strong")).toHaveText("dark mode");
		await expect(card.locator("code")).toHaveText("login");
		await expect(card.getByRole("link", { name: "docs" })).toHaveAttribute(
			"href",
			"https://example.com/docs",
		);
	});

	test("Links with an unsafe protocol are rendered as plain text", async ({
		page,
	}) => {
		await stubReleases(page, [
			{
				...RELEASE_CURRENT,
				body: "Try [click me](javascript:alert(1)) now",
			},
		]);
		await openChangelog(page);

		const card = releaseCard(page, "Paca 1.2.0");
		await expect(card.getByText("click me", { exact: false })).toBeVisible();
		await expect(page.getByRole("link", { name: "click me" })).toHaveCount(0);
	});

	test("A release without notes shows a placeholder", async ({ page }) => {
		await stubReleases(page, [
			{
				tag: "v1.0.0",
				name: "Paca 1.0.0",
				url: "https://github.com/paca-ai/paca/releases/tag/v1.0.0",
				publishedAt: "2026-01-01T12:00:00Z",
				body: "",
				isCurrent: false,
			},
		]);
		await openChangelog(page);

		await expect(
			releaseCard(page, "Paca 1.0.0").getByText("—", { exact: true }),
		).toBeVisible();
	});
});

// ===========================================================================
// Rule: Empty and error states
// ===========================================================================

test.describe("Changelog empty and error states", () => {
	test("No published releases shows an empty state", async ({ page }) => {
		await stubReleases(page, []);
		await openChangelog(page);

		await expect(
			page.getByText("No release notes available yet."),
		).toBeVisible();
		await expect(page.getByRole("article")).toHaveCount(0);
	});

	test("A failing releases request shows an error state", async ({ page }) => {
		await page.route(RELEASES_ROUTE, (route) =>
			route.fulfill({
				status: 502,
				contentType: "application/json",
				body: JSON.stringify({
					success: false,
					error: { code: "BAD_GATEWAY", message: "upstream unavailable" },
				}),
			}),
		);
		await openChangelog(page);

		await expect(
			page.getByText(
				"Couldn't load the changelog right now. Please try again later.",
			),
		).toBeVisible();
		await expect(page.getByRole("article")).toHaveCount(0);
	});
});
