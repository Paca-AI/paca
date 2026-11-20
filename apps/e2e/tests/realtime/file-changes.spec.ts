// spec: features/realtime/file-changes.feature
// seed: tests/seed.spec.ts
//
// Another user's change to the file manager shows up without a reload. The
// browser is signed in as a member; the admin changes files over the API. The
// probe specs check the wire: ids only, and delivery follows files:read (on the
// project for a project's files, platform-level for the general scope).

import {
	type APIRequestContext,
	expect,
	type Page,
	test,
} from "@playwright/test";
import {
	API_URL,
	authRequest,
	BASE_URL,
	cleanupGlobalRolesByPrefix,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	createProject,
	createUserWithGlobalPermissions,
	createUserWithProjectPermissions,
	newRunId,
	RESTRICTED_PASSWORD,
	signIn,
} from "../helpers/e2e-api";
import { closeAll, connectAs, QUIET_MS, sleep } from "../helpers/realtime";

const PREFIX = "E2E_RTFILES_";
const RUN_ID = newRunId();
const name = (label: string) => `${PREFIX}${label}_${RUN_ID}`;

async function cleanup(request: APIRequestContext) {
	await cleanupProjectsByPrefix(request, PREFIX);
	await cleanupUsersByPrefix(request, PREFIX);
	await cleanupGlobalRolesByPrefix(request, PREFIX);
}

test.beforeEach(async ({ request, context }) => {
	await authRequest(request);
	await cleanup(request);
	await context.clearCookies();
});

test.afterEach(async ({ request }) => {
	await closeAll();
	await authRequest(request);
	await cleanup(request);
});

// ─── Arrangement ─────────────────────────────────────────────────────────────

type Scope = { projectId?: string };

const filesApi = ({ projectId }: Scope) =>
	projectId ? `${API_URL}/projects/${projectId}/files` : `${API_URL}/files`;

async function createFolder(
	request: APIRequestContext,
	scope: Scope,
	folderName: string,
	parentId: string | null = null,
): Promise<string> {
	const res = await request.post(`${filesApi(scope)}/folders`, {
		data: { parent_id: parentId, name: folderName },
	});
	expect(res.ok(), await res.text()).toBeTruthy();
	return (await res.json()).data.id as string;
}

async function patchNode(
	request: APIRequestContext,
	scope: Scope,
	id: string,
	data: Record<string, unknown>,
) {
	const res = await request.patch(`${filesApi(scope)}/nodes/${id}`, { data });
	expect(res.ok(), await res.text()).toBeTruthy();
}

async function trash(request: APIRequestContext, scope: Scope, id: string) {
	const res = await request.delete(`${filesApi(scope)}/nodes/${id}`);
	expect(res.ok(), await res.text()).toBeTruthy();
}

async function restore(request: APIRequestContext, scope: Scope, id: string) {
	const res = await request.post(`${filesApi(scope)}/nodes/${id}/restore`);
	expect(res.ok(), await res.text()).toBeTruthy();
}

/** A project and a member who can read its files (and the platform's). */
async function arrange(
	request: APIRequestContext,
	playwright: Parameters<typeof createUserWithProjectPermissions>[1],
) {
	await authRequest(request);
	const projectId = await createProject(request, name("p"));
	const username = name("bob");
	await createUserWithProjectPermissions(request, playwright, {
		projectId,
		username,
		roleName: name("role"),
		permissions: { "files:read": true },
	});
	return { projectId, username };
}

/** A row or card of the file list, found by its visible name. */
const item = (page: Page, itemName: string) =>
	page.locator("[data-file-id]").filter({ hasText: itemName }).first();

async function openProjectFiles(
	page: Page,
	projectId: string,
	folder?: string,
) {
	await page.goto(
		`${BASE_URL}/projects/${projectId}/files${folder ? `?folder=${folder}` : ""}`,
	);
	await expect(item(page, "Task attachments")).toBeVisible({
		timeout: 15_000,
	});
	await settleSocket(page);
}

/**
 * Gives the page's socket time to connect and join its rooms: an event sent
 * before the join is not replayed, so a change made too early is never seen.
 */
async function settleSocket(page: Page) {
	await page.waitForLoadState("networkidle");
	await sleep(1_500);
}

// ─── A project's file manager ────────────────────────────────────────────────

test.describe("A project's file manager follows other members", () => {
	test("a folder created by someone else appears without a reload", async ({
		page,
		request,
		playwright,
	}) => {
		const { projectId, username } = await arrange(request, playwright);
		await signIn(page, username, RESTRICTED_PASSWORD);
		await openProjectFiles(page, projectId);

		const folder = name("created");
		await createFolder(request, { projectId }, folder);
		await expect(item(page, folder)).toBeVisible({ timeout: 10_000 });
	});

	test("a renamed folder updates in place", async ({
		page,
		request,
		playwright,
	}) => {
		const { projectId, username } = await arrange(request, playwright);
		const before = name("before");
		const id = await createFolder(request, { projectId }, before);
		await signIn(page, username, RESTRICTED_PASSWORD);
		await openProjectFiles(page, projectId);
		await expect(item(page, before)).toBeVisible();

		const after = name("after");
		await patchNode(request, { projectId }, id, { name: after });
		await expect(item(page, after)).toBeVisible({ timeout: 10_000 });
		await expect(item(page, before)).toHaveCount(0);
	});

	test("a trashed folder disappears and a restored one comes back", async ({
		page,
		request,
		playwright,
	}) => {
		const { projectId, username } = await arrange(request, playwright);
		const folder = name("gone");
		const id = await createFolder(request, { projectId }, folder);
		await signIn(page, username, RESTRICTED_PASSWORD);
		await openProjectFiles(page, projectId);
		await expect(item(page, folder)).toBeVisible();

		await trash(request, { projectId }, id);
		await expect(item(page, folder)).toHaveCount(0, { timeout: 10_000 });

		await restore(request, { projectId }, id);
		await expect(item(page, folder)).toBeVisible({ timeout: 10_000 });
	});

	test("a folder moved into another folder leaves the listing it was in", async ({
		page,
		request,
		playwright,
	}) => {
		const { projectId, username } = await arrange(request, playwright);
		const target = name("target");
		const moving = name("moving");
		const targetId = await createFolder(request, { projectId }, target);
		const movingId = await createFolder(request, { projectId }, moving);
		await signIn(page, username, RESTRICTED_PASSWORD);
		await openProjectFiles(page, projectId);
		await expect(item(page, moving)).toBeVisible();

		await patchNode(request, { projectId }, movingId, {
			move: true,
			parent_id: targetId,
		});
		await expect(item(page, moving)).toHaveCount(0, { timeout: 10_000 });
		await expect(item(page, target)).toBeVisible();
	});
});

// ─── The general scope ───────────────────────────────────────────────────────

test.describe("The general scope follows other people's changes", () => {
	test("a folder created in the general scope appears without a reload", async ({
		page,
		request,
		playwright,
	}) => {
		await authRequest(request);
		const username = name("gen");
		await createUserWithGlobalPermissions(request, playwright, {
			username,
			roleName: name("grole"),
			permissions: { "files:read": true },
		});
		await signIn(page, username, RESTRICTED_PASSWORD);
		await page.goto(`${BASE_URL}/files?scope=general`);
		// The sidebar (with its Trash link) is a closed Sheet on phones, so wait on the page itself.
		await expect(page.getByRole("main")).toBeVisible({ timeout: 15_000 });
		await settleSocket(page);

		const folder = name("general");
		await createFolder(request, {}, folder);
		await expect(item(page, folder)).toBeVisible({ timeout: 10_000 });
	});
});

// ─── Who receives file.changed ───────────────────────────────────────────────

test.describe("Who receives file.changed", () => {
	test("a member with files:read receives the event, without any file name", async ({
		request,
		playwright,
	}) => {
		const { projectId, username } = await arrange(request, playwright);
		const { probe, namespaces } = await connectAs(
			playwright,
			username,
			projectId,
		);
		expect(namespaces).toContain("files");

		const folder = name("secret-name");
		const id = await createFolder(request, { projectId }, folder);
		const event = await probe.waitForEvent(
			(e) => e.type === "file.changed" && e.payload.node_id === id,
		);
		expect(event.payload).toMatchObject({
			change: "created",
			scope: "project",
			project_id: projectId,
			kind: "folder",
			root: true,
		});
		expect(JSON.stringify(event.payload)).not.toContain(folder);
	});

	test("a member without files:read receives nothing", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("p"));
		const username = name("noread");
		await createUserWithProjectPermissions(request, playwright, {
			projectId,
			username,
			roleName: name("role"),
			permissions: { "tasks:read": true },
		});
		const { probe, namespaces } = await connectAs(
			playwright,
			username,
			projectId,
		);
		expect(namespaces).not.toContain("files");

		await createFolder(request, { projectId }, name("hidden"));
		await sleep(QUIET_MS);
		expect(probe.eventsOfType("file.changed")).toHaveLength(0);
	});

	test("an account with platform files:read receives a general-scope event", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const username = name("gread");
		await createUserWithGlobalPermissions(request, playwright, {
			username,
			roleName: name("grole"),
			permissions: { "files:read": true },
		});
		const { probe } = await connectAs(playwright, username);

		const id = await createFolder(request, {}, name("general"));
		const event = await probe.waitForEvent(
			(e) => e.type === "file.changed" && e.payload.node_id === id,
		);
		expect(event.payload).toMatchObject({
			scope: "general",
			change: "created",
		});
	});

	test("an account without platform files:read receives no general-scope event", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const username = name("gnone");
		await createUserWithGlobalPermissions(request, playwright, {
			username,
			roleName: name("grole"),
			permissions: { "tasks:read": true },
		});
		const { probe } = await connectAs(playwright, username);

		await createFolder(request, {}, name("general"));
		await sleep(QUIET_MS);
		expect(probe.eventsOfType("file.changed")).toHaveLength(0);
	});
});
