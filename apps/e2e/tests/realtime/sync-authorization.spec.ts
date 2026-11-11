// The sync feed follows IAM exactly as the REST routes and the legacy events
// do: what an account may not read arrives as a bare `{seq, skip}` (so its
// sequence stays gapless), a change of roles takes effect on the open socket,
// and a replay is judged by what the account may read now.

import {
	type APIRequestContext,
	expect,
	type PlaywrightWorkerArgs,
	test,
} from "@playwright/test";
import {
	API_URL,
	authRequest,
	createProject,
	replaceMemberRoles,
} from "../helpers/e2e-api";
import {
	allow,
	createDoc,
	createMember,
	createSprint,
	createTask,
	deny,
	type Member,
	policyOf,
	projectRoleFrom,
} from "../helpers/iam";
import { connectAs, type Probe, sleep } from "../helpers/realtime";
import {
	allChanges,
	chainProblems,
	changes,
	syncProbe,
	useSyncScaffold,
	waitForChange,
	waitForCursor,
	waitForType,
} from "../helpers/sync";

const { name } = useSyncScaffold("E2E_RTSYNCAZ_");
const res = (projectId: string) => `project/${projectId}`;

async function memberWith(
	request: APIRequestContext,
	playwright: PlaywrightWorkerArgs["playwright"],
	projectId: string,
	label: string,
	...statements: ReturnType<typeof allow>[]
): Promise<Member & { roleId: string }> {
	const roleId = await projectRoleFrom(
		request,
		projectId,
		name(`role_${label}`),
		...statements,
	);
	const member = await createMember(
		request,
		playwright,
		projectId,
		name(label),
		[roleId],
	);
	return { ...member, roleId };
}

const readAll = (projectId: string, ...domains: string[]) =>
	allow(
		["projects:read", ...domains.map((d) => `${d}:read`)],
		[`${res(projectId)}/*`, res(projectId)],
	);

async function setPolicy(
	request: APIRequestContext,
	projectId: string,
	roleId: string,
	label: string,
	...statements: ReturnType<typeof allow>[]
) {
	const response = await request.put(
		`${API_URL}/projects/${projectId}/roles/${roleId}`,
		{
			data: {
				name: name(`role_${label}`),
				description: "",
				policy: policyOf(...statements),
			},
		},
	);
	expect(response.ok(), await response.text()).toBeTruthy();
}

/** A member's own socket, joined with sync. */
async function memberProbe(
	playwright: PlaywrightWorkerArgs["playwright"],
	username: string,
	projectId: string,
	cursor: number | null = null,
	epoch: number | null = null,
) {
	const { probe } = await connectAs(playwright, username);
	const ack = await probe.syncJoin(projectId, cursor, epoch);
	return { probe, ack };
}

/** What the socket was told about `seq`: the change, or a skip. */
const at = (probe: Probe, seq: number) =>
	allChanges(probe).find((c) => c.seq === seq);

test.describe("what a member's socket receives", () => {
	test("only the domains the role reads: the rest arrive as skips", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("domains"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			readAll(projectId, "docs"),
		);
		const { probe, ack } = await memberProbe(
			playwright,
			bob.username,
			projectId,
		);
		expect(ack.namespaces).toEqual(expect.arrayContaining(["docs"]));
		expect(ack.namespaces).not.toContain("tasks");

		const taskId = await createTask(request, projectId, name("t"));
		const docId = await createDoc(request, projectId, name("d"));
		await waitForType(probe, "doc.created", { entityId: docId });

		const all = allChanges(probe);
		expect(all.some((c) => c.type === "task.created")).toBe(false);
		const skipped = all.filter((c) => c.skip);
		expect(skipped.length).toBeGreaterThan(0);
		expect(JSON.stringify(all)).not.toContain(taskId);
		expect(chainProblems(probe, ack.head)).toEqual([]);
	});

	test("a Deny on one task, one doc, one sprint and one view hides exactly those", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("deny"));
		const api = changes(request, projectId);
		const secretTask = await createTask(request, projectId, name("st"));
		const openTask = await createTask(request, projectId, name("ot"));
		const secretDoc = await createDoc(request, projectId, name("sd"));
		const openDoc = await createDoc(request, projectId, name("od"));
		const secretSprint = await createSprint(request, projectId, name("ss"));
		const openSprint = await createSprint(request, projectId, name("os"));
		const secretView = (await api.createView(name("sv"))).id;
		const openView = (await api.createView(name("ov"))).id;

		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			readAll(projectId, "tasks", "docs", "sprints", "views"),
			deny(["tasks:read"], [`${res(projectId)}/task/${secretTask}`]),
			deny(["docs:read"], [`${res(projectId)}/doc/${secretDoc}`]),
			deny(["sprints:read"], [`${res(projectId)}/sprint/${secretSprint}`]),
			deny(["views:read"], [`${res(projectId)}/view/${secretView}`]),
		);
		const { probe, ack } = await memberProbe(
			playwright,
			bob.username,
			projectId,
		);
		const admin = await syncProbe(request, projectId);

		await api.patchTask(secretTask, { title: name("st2") });
		await api.patchTask(openTask, { title: name("ot2") });
		await api.patchDoc(secretDoc, { title: name("sd2") });
		await api.patchDoc(openDoc, { title: name("od2") });
		await api.patchSprint(secretSprint, { name: name("ss2") });
		await api.patchSprint(openSprint, { name: name("os2") });
		await api.patchView(secretView, { name: name("sv2") });
		await api.patchView(openView, { name: name("ov2") });

		// Everything the admin sees, bob has been told about: as a change or a skip.
		await waitForCursor(admin.probe, admin.probe.batches.at(-1)?.to ?? 0);
		await expect
			.poll(
				() =>
					probe.syncedChanges().filter((c) => c.type?.endsWith(".updated"))
						.length,
				{ timeout: 15_000 },
			)
			.toBe(4);
		const got = probe
			.syncedChanges()
			.filter((c) => c.type?.endsWith(".updated"));
		const json = JSON.stringify(got);
		for (const secret of [secretTask, secretDoc, secretSprint, secretView]) {
			expect(json).not.toContain(secret);
		}
		for (const open of [openTask, openDoc, openSprint, openView]) {
			expect(json).toContain(open);
		}
		expect(chainProblems(probe, ack.head)).toEqual([]);
	});

	test("a role limited to one task still gets the tasks room, and only that task", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("onetask"));
		const mine = await createTask(request, projectId, name("mine"));
		const other = await createTask(request, projectId, name("other"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			allow(["projects:read"], [res(projectId)]),
			allow(["tasks:read"], [`${res(projectId)}/task/${mine}`]),
		);
		const { probe, ack } = await memberProbe(
			playwright,
			bob.username,
			projectId,
		);
		expect(ack.namespaces).toContain("tasks");

		const api = changes(request, projectId);
		await api.patchTask(other, { title: name("other2") });
		await api.patchTask(mine, { title: name("mine2") });
		await waitForType(probe, "task.updated", { entityId: mine });
		expect(JSON.stringify(allChanges(probe))).not.toContain(other);
	});

	test("a member who may only open the project gets project.updated and nothing else", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("open"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			allow(["projects:read"], [res(projectId)]),
		);
		const { probe, ack } = await memberProbe(
			playwright,
			bob.username,
			projectId,
		);
		expect(ack.namespaces).toEqual(["project"]);

		const api = changes(request, projectId);
		await createTask(request, projectId, name("t"));
		await createDoc(request, projectId, name("d"));
		await api.patchProject({ name: name("renamed") });
		const renamed = await waitForType(probe, "project.updated");
		expect(renamed.payload?.name).toBe(name("renamed"));
		expect(
			probe
				.syncedChanges()
				.filter((c) => c.type !== "project.updated")
				.map((c) => c.type),
		).toEqual([]);
	});

	test("the task schema needs tasks:read, members need project.members:read, agents need agents:read", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("schema"));
		const api = changes(request, projectId);
		const reader = await memberWith(
			request,
			playwright,
			projectId,
			"reader",
			readAll(projectId, "tasks", "project.members"),
		);
		const blind = await memberWith(
			request,
			playwright,
			projectId,
			"blind",
			readAll(projectId, "docs"),
		);
		const a = await memberProbe(playwright, reader.username, projectId);
		const b = await memberProbe(playwright, blind.username, projectId);

		await api.createStatus(name("st"));
		await api.createType(name("ty"));
		await createMember(request, playwright, projectId, name("newcomer"), [
			reader.roleId,
		]);
		await waitForType(a.probe, "project.task_status.created");
		await waitForType(a.probe, "project.task_type.created");
		await waitForType(a.probe, "member.added");
		await sleep(400);
		expect(b.probe.syncedChanges().filter((c) => c.type !== undefined)).toEqual(
			[],
		);
		expect(chainProblems(b.probe, b.ack.head)).toEqual([]);
	});

	test("two members with different rights see the same sequence, one with changes and one with skips", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("same"));
		const api = changes(request, projectId);
		const tasks = await memberWith(
			request,
			playwright,
			projectId,
			"tasks",
			readAll(projectId, "tasks"),
		);
		const docs = await memberWith(
			request,
			playwright,
			projectId,
			"docs",
			readAll(projectId, "docs"),
		);
		const a = await memberProbe(playwright, tasks.username, projectId);
		const b = await memberProbe(playwright, docs.username, projectId);

		const taskId = await createTask(request, projectId, name("t"));
		const docId = await createDoc(request, projectId, name("d"));
		await api.patchTask(taskId, { title: name("t2") });
		await waitForType(a.probe, "task.updated", { entityId: taskId });
		await waitForType(b.probe, "doc.created", { entityId: docId });
		const last = Math.max(
			a.probe.batches.at(-1)?.to ?? 0,
			b.probe.batches.at(-1)?.to ?? 0,
		);
		await waitForCursor(a.probe, last);
		await waitForCursor(b.probe, last);

		const seqs = (p: Probe) => allChanges(p).map((c) => c.seq);
		expect(seqs(a.probe)).toEqual(seqs(b.probe));
		// Where one has a change, the other has a skip, and the reverse.
		for (const seq of seqs(a.probe)) {
			const x = at(a.probe, seq);
			const y = at(b.probe, seq);
			expect(Boolean(x?.skip) || Boolean(y?.skip)).toBe(true);
		}
	});
});

test.describe("a socket that is not allowed in", () => {
	test("a user who is not a member syncs nothing and is told no rooms", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("outsider"));
		const otherProject = await createProject(request, name("other"));
		const outsider = await memberWith(
			request,
			playwright,
			otherProject,
			"out",
			readAll(otherProject, "tasks"),
		);
		const { probe, ack } = await memberProbe(
			playwright,
			outsider.username,
			projectId,
		);
		expect(ack).toMatchObject({ ok: true, namespaces: [], head: 0 });

		await createTask(request, projectId, name("t"));
		await sleep(1_200);
		expect(probe.batches).toEqual([]);
		// And the project they do belong to still works for them.
		const ownAck = await probe.syncJoin(otherProject);
		expect(ownAck.namespaces).toContain("tasks");
	});

	test("a project id that does not exist, and one that is not a uuid, give no rooms and no changes", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("bogus"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			readAll(projectId, "tasks"),
		);
		const { probe } = await connectAs(playwright, bob.username);
		for (const id of ["00000000-0000-0000-0000-000000000000", "not-a-uuid"]) {
			const ack = await probe.syncJoin(id).catch(() => null);
			// Refused, or an ack with no rooms; never rooms.
			if (ack?.ok) expect(ack.namespaces).toEqual([]);
		}
		expect(probe.batches).toEqual([]);
	});
});

test.describe("when roles change under an open socket", () => {
	test("revoking a domain turns its later changes into skips, and restoring brings them back", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("revoke"));
		const api = changes(request, projectId);
		const taskId = await createTask(request, projectId, name("t"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			readAll(projectId, "tasks", "docs"),
		);
		const { probe, ack } = await memberProbe(
			playwright,
			bob.username,
			projectId,
		);

		await api.patchTask(taskId, { title: name("t1") });
		await waitForType(probe, "task.updated", { entityId: taskId });

		await setPolicy(
			request,
			projectId,
			bob.roleId,
			"bob",
			readAll(projectId, "docs"),
		);
		await expect
			.poll(() => probe.lastChange(0)?.namespaces ?? [], { timeout: 10_000 })
			.not.toContain("tasks");
		const mark = Date.now();
		await api.patchTask(taskId, { title: name("t2") });
		const docId = await createDoc(request, projectId, name("d"));
		await waitForType(probe, "doc.created", { entityId: docId, since: mark });
		expect(
			probe.syncedChanges(mark).some((c) => c.type === "task.updated"),
		).toBe(false);

		await setPolicy(
			request,
			projectId,
			bob.roleId,
			"bob",
			readAll(projectId, "tasks", "docs"),
		);
		await expect
			.poll(() => probe.lastChange(mark)?.namespaces ?? [], { timeout: 10_000 })
			.toContain("tasks");
		const again = Date.now();
		await api.patchTask(taskId, { title: name("t3") });
		const c = await waitForType(probe, "task.updated", {
			entityId: taskId,
			since: again,
		});
		expect(c.entity?.title).toBe(name("t3"));
		expect(chainProblems(probe, ack.head)).toEqual([]);
	});

	test("adding a Deny takes effect on the next change, removing it on the next after that", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("denyflip"));
		const api = changes(request, projectId);
		const taskId = await createTask(request, projectId, name("t"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			readAll(projectId, "tasks"),
		);
		const { probe } = await memberProbe(playwright, bob.username, projectId);

		await api.patchTask(taskId, { title: name("a") });
		await waitForType(probe, "task.updated", { entityId: taskId });

		await setPolicy(
			request,
			projectId,
			bob.roleId,
			"bob",
			readAll(projectId, "tasks"),
			deny(["tasks:read"], [`${res(projectId)}/task/${taskId}`]),
		);
		await sleep(600);
		let mark = Date.now();
		await api.patchTask(taskId, { title: name("b") });
		const sentinel = await createTask(request, projectId, name("sentinel"));
		await waitForType(probe, "task.created", {
			entityId: sentinel,
			since: mark,
		});
		expect(
			probe.syncedChanges(mark).some((c) => c.entity?.title === name("b")),
		).toBe(false);

		await setPolicy(
			request,
			projectId,
			bob.roleId,
			"bob",
			readAll(projectId, "tasks"),
		);
		await sleep(600);
		mark = Date.now();
		await api.patchTask(taskId, { title: name("c") });
		await waitForChange(probe, (c) => c.entity?.title === name("c"), mark);
	});

	test("a role attached to the member later adds its rooms to the same socket", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("attach"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			readAll(projectId, "tasks"),
		);
		const docsRole = await projectRoleFrom(
			request,
			projectId,
			name("role_docs"),
			readAll(projectId, "docs"),
		);
		const { probe } = await memberProbe(playwright, bob.username, projectId);
		await createDoc(request, projectId, name("before"));
		await sleep(700);
		expect(probe.syncedChanges().some((c) => c.type === "doc.created")).toBe(
			false,
		);

		await replaceMemberRoles(request, projectId, bob.memberId, [
			bob.roleId,
			docsRole,
		]);
		await expect
			.poll(() => probe.lastChange(0)?.namespaces ?? [], { timeout: 10_000 })
			.toContain("docs");
		const mark = Date.now();
		const docId = await createDoc(request, projectId, name("after"));
		await waitForType(probe, "doc.created", { entityId: docId, since: mark });
	});

	test("removing the member empties their rooms and silences the feed", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("removed"));
		const api = changes(request, projectId);
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			readAll(projectId, "tasks"),
		);
		const { probe } = await memberProbe(playwright, bob.username, projectId);
		const taskId = await createTask(request, projectId, name("t"));
		await waitForType(probe, "task.created", { entityId: taskId });

		const removed = await request.delete(
			`${API_URL}/projects/${projectId}/members/${bob.memberId}`,
		);
		expect(removed.ok(), await removed.text()).toBeTruthy();
		await expect
			.poll(() => probe.lastChange(0)?.namespaces, { timeout: 10_000 })
			.toEqual([]);

		const mark = Date.now();
		await api.patchTask(taskId, { title: name("after") });
		await sleep(900);
		expect(
			probe.syncedChanges(mark).some((c) => c.type === "task.updated"),
		).toBe(false);
	});

	test("deleting the project tells its members' sockets to refresh", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("gone"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			readAll(projectId, "tasks"),
		);
		const { probe } = await memberProbe(playwright, bob.username, projectId);
		const mark = Date.now();
		const response = await request.delete(`${API_URL}/projects/${projectId}`);
		expect(response.ok(), await response.text()).toBeTruthy();
		// The project is soft-deleted and members keep their grants, so the rooms
		// stay; what matters is that the change is announced on the sync socket too.
		await expect
			.poll(() => probe.lastChange(mark)?.projectId, { timeout: 15_000 })
			.toBe(projectId);
		expect(probe.connected).toBe(true);
	});
});

test.describe("a replay is judged by what the account may read now", () => {
	test("a cursor from before a Deny replays the denied change as a skip", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("replaydeny"));
		const api = changes(request, projectId);
		const taskId = await createTask(request, projectId, name("t"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			readAll(projectId, "tasks"),
		);
		const first = await memberProbe(playwright, bob.username, projectId);
		const cursor = first.ack.head;
		first.probe.close();

		await api.patchTask(taskId, { title: name("secretly") });
		await setPolicy(
			request,
			projectId,
			bob.roleId,
			"bob",
			readAll(projectId, "tasks"),
			deny(["tasks:read"], [`${res(projectId)}/task/${taskId}`]),
		);
		await sleep(800);

		const second = await memberProbe(
			playwright,
			bob.username,
			projectId,
			cursor,
			first.ack.epoch,
		);
		expect(second.ack.mode).toBe("replay");
		expect(JSON.stringify(allChanges(second.probe))).not.toContain(
			name("secretly"),
		);
		expect(allChanges(second.probe).some((c) => c.skip)).toBe(true);
		expect(chainProblems(second.probe, cursor)).toEqual([]);
	});

	test("a replay never carries what the account lost the right to read", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("replaylost"));
		const api = changes(request, projectId);
		const taskId = await createTask(request, projectId, name("t"));
		const docId = await createDoc(request, projectId, name("d"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			readAll(projectId, "tasks", "docs"),
		);
		const first = await memberProbe(playwright, bob.username, projectId);
		first.probe.close();
		await api.patchTask(taskId, { title: name("t2") });
		await api.patchDoc(docId, { title: name("d2") });

		await setPolicy(
			request,
			projectId,
			bob.roleId,
			"bob",
			readAll(projectId, "docs"),
		);
		await sleep(800);
		const second = await memberProbe(
			playwright,
			bob.username,
			projectId,
			first.ack.head,
			first.ack.epoch,
		);
		expect(second.ack.mode).toBe("replay");
		const json = JSON.stringify(allChanges(second.probe));
		expect(json).toContain(name("d2"));
		expect(json).not.toContain(name("t2"));
	});

	test("history before the account became a member is replayed only as far as it may read it", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("history"));
		const admin = await syncProbe(request, projectId);
		const startHead = admin.ack.head;
		const taskId = await createTask(request, projectId, name("old"));
		const docId = await createDoc(request, projectId, name("olddoc"));
		await waitForType(admin.probe, "doc.created", { entityId: docId });

		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			readAll(projectId, "docs"),
		);
		const { probe, ack } = await memberProbe(
			playwright,
			bob.username,
			projectId,
			startHead,
			admin.ack.epoch,
		);
		expect(ack.mode).toBe("replay");
		const json = JSON.stringify(allChanges(probe));
		expect(json).toContain(name("olddoc"));
		expect(json).not.toContain(taskId);
	});
});

test.describe("sockets of different accounts", () => {
	test("an account's join does not change what another account's socket receives", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("accounts"));
		const a = await memberWith(
			request,
			playwright,
			projectId,
			"a",
			readAll(projectId, "tasks"),
		);
		const b = await memberWith(
			request,
			playwright,
			projectId,
			"b",
			readAll(projectId, "docs"),
		);
		const pa = await memberProbe(playwright, a.username, projectId);
		const pb = await memberProbe(playwright, b.username, projectId);
		const taskId = await createTask(request, projectId, name("t"));
		await waitForType(pa.probe, "task.created", { entityId: taskId });
		await sleep(500);
		expect(JSON.stringify(allChanges(pb.probe))).not.toContain(taskId);
	});
});
