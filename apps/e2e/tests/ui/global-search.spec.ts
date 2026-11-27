// spec: features/ui/global-search.feature
// seed: tests/seed.spec.ts
//
// GET /search is open to every signed-in user and decides what it returns in
// the handler: projects the caller may read, the tasks their roles let them
// read in each project, and the members of their teams (every user with
// users:write). These scenarios check each of those against real roles, then
// drive the dialog end to end.

import {
	type APIRequestContext,
	expect,
	type Playwright,
	test,
} from "@playwright/test";
import {
	API_URL,
	authRequest,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	newRunId,
	projectRoleIdByName,
	signIn,
} from "../helpers/e2e-api";
import {
	allow,
	asUser,
	createMember,
	createPerson,
	createSprint,
	createTask,
	dataOf,
	openProject,
	projectRoleFrom,
} from "../helpers/iam";

const PREFIX = "E2E_SEARCH_";
const RUN_ID = newRunId();
// Every name below contains TOKEN, so one query finds this run's data and
// nothing another spec created.
const TOKEN = `qx${RUN_ID.toLowerCase()}`;
const name = (label: string) => `${PREFIX}${label}_${TOKEN}`;
const KEY_PREFIX = `Q${RUN_ID}`;

interface SearchResults {
	projects: Array<{ id: string; name: string }>;
	tasks: Array<{ id: string; title: string; task_key: string }>;
	people: Array<{ id: string; name: string; project_id?: string }>;
}

async function search(
	api: APIRequestContext,
	q: string,
): Promise<SearchResults> {
	const response = await api.get(
		`${API_URL}/search?q=${encodeURIComponent(q)}&limit=20`,
	);
	expect(response.status(), await response.text()).toBe(200);
	return dataOf<SearchResults>(response);
}

const names = (items: Array<{ name?: string; title?: string }>) =>
	items.map((i) => i.name ?? i.title ?? "").sort();

async function cleanup(request: APIRequestContext) {
	await cleanupProjectsByPrefix(request, PREFIX);
	await cleanupUsersByPrefix(request, PREFIX);
}

async function createKeyedProject(
	request: APIRequestContext,
	projectName: string,
	taskIdPrefix?: string,
): Promise<string> {
	const response = await request.post(`${API_URL}/projects`, {
		data: {
			name: projectName,
			...(taskIdPrefix ? { task_id_prefix: taskIdPrefix } : {}),
		},
	});
	expect(response.ok(), await response.text()).toBeTruthy();
	return (await response.json()).data.id as string;
}

interface Fixture {
	projectA: string;
	projectB: string;
	sprint1: string;
	s1Task: string;
}

/** Project A (keyed) with an S1 task and a backlog task, project B with one task. */
async function seedProjects(request: APIRequestContext): Promise<Fixture> {
	const projectA = await createKeyedProject(request, name("A"), KEY_PREFIX);
	const projectB = await createKeyedProject(request, name("B"));
	const sprint1 = await createSprint(request, projectA, name("S1"));
	const s1Task = await createTask(request, projectA, name("TASK_S1"), sprint1);
	await createTask(request, projectA, name("TASK_BACKLOG"));
	await createTask(request, projectB, name("TASK_B"));
	return { projectA, projectB, sprint1, s1Task };
}

/** A teammate in project A and an outsider who is only in project B, both Viewers. */
async function seedPeople(
	request: APIRequestContext,
	playwright: Playwright,
	fx: Fixture,
) {
	const viewerIn = (projectId: string) =>
		projectRoleIdByName(request, projectId, "Viewer");
	await createMember(request, playwright, fx.projectA, name("TEAMMATE"), [
		await viewerIn(fx.projectA),
	]);
	await createMember(request, playwright, fx.projectB, name("OUTSIDER"), [
		await viewerIn(fx.projectB),
	]);
}

test.describe("Results follow the caller's access", () => {
	let fx: Fixture;

	test.beforeEach(async ({ request, playwright, context }) => {
		await authRequest(request);
		await cleanup(request);
		await context.clearCookies();
		fx = await seedProjects(request);
		await seedPeople(request, playwright, fx);
	});

	test.afterEach(async ({ request }) => {
		await authRequest(request);
		await cleanup(request);
	});

	test("A member limited to one sprint finds only what they may read", async ({
		request,
		playwright,
	}) => {
		const role = await projectRoleFrom(
			request,
			fx.projectA,
			name("S1READER"),
			openProject(fx.projectA, ["tasks:read", "project.members:read"]),
			allow(["tasks:read"], [`project/${fx.projectA}/task/*`], {
				In: { "task.sprint_id": [fx.sprint1] },
			}),
		);
		const member = await createMember(
			request,
			playwright,
			fx.projectA,
			name("SEARCHER"),
			[role],
		);

		await asUser(playwright, member.username, async (api) => {
			const got = await search(api, TOKEN);
			expect(names(got.projects)).toEqual([name("A")]);
			expect(names(got.tasks)).toEqual([name("TASK_S1")]);
			// The member is on team A themselves, with the teammate; the
			// outsider shares no team with them.
			expect(names(got.people)).toEqual(
				[name("SEARCHER"), name("TEAMMATE")].sort(),
			);
			for (const person of got.people) {
				expect(person.project_id).toBe(fx.projectA);
			}
		});
	});

	test("A member who may not read the member list finds no people", async ({
		request,
		playwright,
	}) => {
		const role = await projectRoleFrom(
			request,
			fx.projectA,
			name("TASKSONLY"),
			openProject(fx.projectA, ["tasks:read"]),
			allow(["tasks:read"], [`project/${fx.projectA}/task/*`]),
		);
		const member = await createMember(
			request,
			playwright,
			fx.projectA,
			name("NOPEOPLE"),
			[role],
		);

		await asUser(playwright, member.username, async (api) => {
			const got = await search(api, TOKEN);
			expect(names(got.tasks)).toEqual(
				[name("TASK_BACKLOG"), name("TASK_S1")].sort(),
			);
			expect(got.people).toEqual([]);
		});
	});

	test("A user with no projects finds nothing", async ({
		request,
		playwright,
	}) => {
		const loner = await createPerson(request, playwright, name("LONER"));
		await asUser(playwright, loner.username, async (api) => {
			const got = await search(api, TOKEN);
			expect(got).toMatchObject({ projects: [], tasks: [], people: [] });
		});
	});

	test("The admin finds every match", async ({ request }) => {
		const got = await search(request, TOKEN);
		expect(names(got.projects)).toEqual([name("A"), name("B")].sort());
		expect(names(got.tasks)).toEqual(
			[name("TASK_B"), name("TASK_BACKLOG"), name("TASK_S1")].sort(),
		);
		expect(names(got.people)).toEqual(
			[name("OUTSIDER"), name("TEAMMATE")].sort(),
		);
	});

	test("A task key finds that task first", async ({ request }) => {
		const all = await search(request, TOKEN);
		const s1 = all.tasks.find((t) => t.id === fx.s1Task);
		expect(s1?.task_key).toMatch(new RegExp(`^${KEY_PREFIX}-\\d+$`));

		const got = await search(request, (s1?.task_key ?? "").toLowerCase());
		expect(got.tasks[0]?.id).toBe(fx.s1Task);
	});
});

test.describe("The search dialog", () => {
	let fx: Fixture;

	test.beforeEach(async ({ request, context }) => {
		await authRequest(request);
		await cleanup(request);
		await context.clearCookies();
		fx = await seedProjects(request);
	});

	test.afterEach(async ({ request }) => {
		await authRequest(request);
		await cleanup(request);
	});

	test("The sidebar button opens the search", async ({ page, isMobile }) => {
		await signIn(page);
		// On mobile the sidebar is a sheet, not in the page until it is opened.
		if (isMobile) {
			await page.getByRole("button", { name: "Toggle Sidebar" }).click();
		}
		const sidebar = page.locator('[data-slot="sidebar"]');

		await sidebar.getByRole("button", { name: "Search", exact: true }).click();
		await expect(page.getByRole("combobox")).toBeVisible();
		if (isMobile) {
			// The sheet makes way for the dialog.
			await expect(sidebar).toHaveCount(0);
		}
	});

	test("Cmd/Ctrl+K opens the search and Enter opens the first task", async ({
		page,
	}) => {
		await signIn(page);

		await page.keyboard.press("ControlOrMeta+k");
		const input = page.getByRole("combobox");
		await expect(input).toBeVisible();
		await input.fill(name("TASK_S1"));

		const option = page.getByRole("option", {
			name: new RegExp(name("TASK_S1")),
		});
		await expect(option).toBeVisible();
		await expect(option).toContainText(new RegExp(`${KEY_PREFIX}-\\d+`));
		await expect(option).toHaveAttribute("aria-selected", "true");

		await page.keyboard.press("Enter");
		await expect(page).toHaveURL(
			new RegExp(`/projects/${fx.projectA}/tasks/${fx.s1Task}`),
		);
	});
});
