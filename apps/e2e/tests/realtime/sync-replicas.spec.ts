// Collaborative editing across two realtime replicas. The e2e stack runs one
// realtime container; this spec starts a second one on the same network and
// Valkey, so two members of the same room can sit on different replicas.
// Needs `docker` and the stack of deploy/docker-compose.e2e.yml.

import { execFileSync } from "node:child_process";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { expect, test } from "@playwright/test";
import { io, type Socket } from "socket.io-client";
import * as Y from "../../../web/node_modules/yjs";
import {
	authRequest,
	BASE_URL,
	createProject,
	createUserWithProjectPermissions,
} from "../helpers/e2e-api";
import { createTask } from "../helpers/iam";
import { loginContext } from "../helpers/profile-users";
import { cookieHeaderOf, SOCKET_PATH, sleep } from "../helpers/realtime";
import { useSyncScaffold } from "../helpers/sync";

const { name } = useSyncScaffold("E2E_RTREPL_");

const REPLICA_B = "paca-e2e-realtime-b";
const B_PORT = 3102;
const EDITOR = {
	"docs:read": true,
	"docs:write": true,
	"tasks:read": true,
	"tasks:write": true,
	"views:read": true,
} as const;

const docker = (args: string[]) =>
	execFileSync("docker", args, { encoding: "utf8", timeout: 60_000 }).trim();

function startReplicaB(): void {
	try {
		docker(["rm", "-f", REPLICA_B]);
	} catch {
		// not running
	}
	// Same environment as the stack's replica, so the same Valkey and API.
	const env = docker([
		"inspect",
		"paca-e2e-realtime-1",
		"--format",
		"{{range .Config.Env}}{{println .}}{{end}}",
	])
		.split("\n")
		.filter((l) =>
			/^(PORT|NODE_ENV|API_URL|REDIS_URL|CORS_ORIGINS|LOG_LEVEL)=/.test(l),
		)
		.concat("REPLICA_ID=replica-b");
	const dir = mkdtempSync(path.join(tmpdir(), "paca-replica-"));
	const file = path.join(dir, "env");
	writeFileSync(file, `${env.join("\n")}\n`);
	docker([
		"run",
		"-d",
		"--name",
		REPLICA_B,
		"--network",
		"paca-e2e_default",
		"-p",
		`${B_PORT}:3001`,
		"--env-file",
		file,
		"paca-realtime:e2e",
	]);
}

async function waitUp(): Promise<void> {
	for (let i = 0; i < 40; i++) {
		try {
			const r = await fetch(
				`http://localhost:${B_PORT}/socket.io/?EIO=4&transport=polling`,
			);
			if (r.ok) return;
		} catch {
			// not yet
		}
		await sleep(500);
	}
	throw new Error("second replica did not come up");
}

interface Member {
	socket: Socket;
	doc: Y.Doc;
	room: { projectId: string; kind: "task"; id: string };
	join(): Promise<{ ok: boolean; seed?: boolean; reason?: string }>;
	type(text: string): void;
	text(): string;
}

async function member(
	playwright: Parameters<typeof loginContext>[0],
	username: string,
	onReplicaB: boolean,
	room: Member["room"],
): Promise<Member> {
	const api = await loginContext(playwright, username);
	const cookie = await cookieHeaderOf(api);
	const socket = onReplicaB
		? io(`http://localhost:${B_PORT}`, {
				path: "/socket.io",
				transports: ["websocket"],
				extraHeaders: { Cookie: cookie },
				reconnection: false,
				forceNew: true,
			})
		: io(BASE_URL, {
				path: SOCKET_PATH,
				transports: ["websocket"],
				extraHeaders: { Cookie: cookie },
				reconnection: false,
				forceNew: true,
			});
	await new Promise<void>((resolve, reject) => {
		socket.once("connect", () => resolve());
		socket.once("connect_error", (e: Error) =>
			reject(
				new Error(
					`${onReplicaB ? "B" : "A"}: ${e.message} ${JSON.stringify((e as { description?: unknown }).description ?? "")}`,
				),
			),
		);
	});
	const doc = new Y.Doc();
	const text = doc.getText("t");
	socket.on("collab:update", (m: { update: Uint8Array }) => {
		Y.applyUpdate(doc, new Uint8Array(m.update), "remote");
	});
	doc.on("update", (u: Uint8Array, origin: unknown) => {
		if (origin === "remote") return;
		socket.emit("collab:update", { room: room, update: u });
	});
	return {
		socket,
		doc,
		room,
		join: () =>
			new Promise((resolve) => {
				socket.timeout(10_000).emit(
					"collab:join",
					{ ...room, stateVector: Y.encodeStateVector(doc) },
					(
						err: Error | null,
						res: {
							ok: boolean;
							seed?: boolean;
							reason?: string;
							update?: Uint8Array;
						},
					) => {
						if (err) return resolve({ ok: false, reason: "timeout" });
						if (res.ok && res.update) {
							Y.applyUpdate(doc, new Uint8Array(res.update), "remote");
						}
						resolve(res);
					},
				);
			}),
		type: (s: string) => text.insert(text.length, s),
		text: () => text.toString(),
	};
}

async function until(fn: () => boolean, ms = 8_000) {
	const end = Date.now() + ms;
	while (Date.now() < end) {
		if (fn()) return;
		await sleep(100);
	}
}

test.describe("collaborative editing across replicas", () => {
	test.describe.configure({ mode: "serial" });
	test.beforeAll(async () => {
		startReplicaB();
		await waitUp();
		// let both replicas register their heartbeats
		await sleep(1_500);
	});
	test.afterAll(() => {
		try {
			docker(["rm", "-f", REPLICA_B]);
		} catch {
			// already gone
		}
	});

	async function arrange(
		request: Parameters<typeof authRequest>[0],
		playwright: Parameters<typeof loginContext>[0],
	) {
		await authRequest(request);
		const projectId = await createProject(request, name("p"));
		for (const who of ["ann", "bob"]) {
			await createUserWithProjectPermissions(request, playwright, {
				projectId,
				username: name(who),
				roleName: name(`role_${who}`),
				permissions: EDITOR,
			});
		}
		const id = await createTask(request, projectId, name("t"));
		return {
			room: { projectId, kind: "task" as const, id },
			ann: name("ann"),
			bob: name("bob"),
		};
	}

	test("members on different replicas see each other's edits, and a late joiner gets the state", async ({
		request,
		playwright,
	}) => {
		const { room, ann, bob } = await arrange(request, playwright);
		const a = await member(playwright, ann, false, room);
		const b = await member(playwright, bob, true, room);

		expect((await a.join()).ok).toBe(true);
		expect((await b.join()).ok).toBe(true);

		a.type("from-a ");
		await until(() => b.text().includes("from-a"));
		expect(b.text()).toContain("from-a");

		b.type("from-b");
		await until(() => a.text().includes("from-b"));
		expect(a.text()).toContain("from-b");
		expect(a.text()).toBe(b.text());

		// A third member joining on replica B starts from what the room holds.
		const late = await member(playwright, bob, true, room);
		expect((await late.join()).ok).toBe(true);
		expect(late.text()).toBe(a.text());

		for (const m of [a, b, late]) m.socket.close();
	});

	test("the room survives its owner replica going away", async ({
		request,
		playwright,
	}) => {
		const { room, ann, bob } = await arrange(request, playwright);
		const a = await member(playwright, ann, false, room);
		const b = await member(playwright, bob, true, room);
		expect((await a.join()).ok).toBe(true);
		expect((await b.join()).ok).toBe(true);
		a.type("before ");
		await until(() => b.text().includes("before"));

		// Take replica B away abruptly; its member is gone with it.
		docker(["kill", REPLICA_B]);
		b.socket.close();

		// A member rejoining on replica A is served, whichever replica owned the
		// room, and can edit with another member who joins after it.
		const a2 = await member(playwright, bob, false, room);
		let joined = { ok: false } as { ok: boolean };
		for (let i = 0; i < 20 && !joined.ok; i++) {
			joined = await a2.join();
			if (!joined.ok) await sleep(1_000);
		}
		expect(joined.ok).toBe(true);

		a2.type("after");
		await until(() => a.text().includes("after"));
		expect(a.text()).toContain("after");
		expect(a2.text()).toContain("after");

		for (const m of [a, a2]) m.socket.close();
	});
});
