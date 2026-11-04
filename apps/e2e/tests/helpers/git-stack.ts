import { execFileSync } from "node:child_process";
import path from "node:path";

/**
 * The e2e stack has no Git provider plugin, so no webhook can be delivered.
 * These helpers reach into the stack's own containers instead, to put the
 * state a webhook would have left (a linked pull request) and the event core
 * would have queued for the automation engine, exactly where the engine reads
 * them. They need `docker` and the stack of deploy/docker-compose.e2e.yml.
 */
const COMPOSE_FILE =
	process.env.E2E_COMPOSE_FILE ??
	path.resolve(__dirname, "../../../../deploy/docker-compose.e2e.yml");

/** The stream core appends Git automation events to, and plugins' own events. */
const TRIGGER_STREAM = "paca.plugin_trigger_events";

function compose(args: string[]): string {
	return execFileSync("docker", ["compose", "-f", COMPOSE_FILE, ...args], {
		encoding: "utf8",
		timeout: 30_000,
		stdio: ["ignore", "pipe", "pipe"],
	}).trim();
}

/** True when the stack's database and queue answer; specs skip otherwise. */
export function stackReachable(): boolean {
	try {
		compose(["exec", "-T", "valkey", "valkey-cli", "ping"]);
		compose(["exec", "-T", "postgres", "pg_isready", "-U", "paca"]);
		return true;
	} catch {
		return false;
	}
}

function sql(statement: string): string {
	return compose([
		"exec",
		"-T",
		"postgres",
		"psql",
		"-U",
		"paca",
		"-d",
		"paca",
		"-v",
		"ON_ERROR_STOP=1",
		"-tA",
		"-c",
		statement,
	]);
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const literal = (value: string) => `'${value.replaceAll("'", "''")}'`;

function uuid(value: string): string {
	if (!UUID.test(value)) throw new Error(`not a uuid: ${value}`);
	return literal(value);
}

export type ChangeRequestState = "open" | "closed" | "merged";

export interface SeededChangeRequest {
	repositoryId: string;
	changeRequestId: string;
	number: string;
}

/**
 * Links a pull request to a task the way an ingested webhook does: a cached
 * repository, the cached request and the task link. Project deletion removes
 * all of it (the rows cascade).
 */
export function seedChangeRequest(args: {
	projectId: string;
	taskId: string;
	number: string;
	state: ChangeRequestState;
	draft?: boolean;
	baseRef?: string;
}): SeededChangeRequest {
	const repositoryId = crypto.randomUUID();
	const changeRequestId = crypto.randomUUID();
	sql(`
		INSERT INTO git_repositories (id, project_id, provider_id, host, full_path)
		VALUES (${uuid(repositoryId)}, ${uuid(args.projectId)}, 'com.example.git', 'git.example.test', ${literal(`acme/web-${repositoryId.slice(0, 8)}`)});
		INSERT INTO git_change_requests (id, repository_id, number, title, state, draft, base_ref, head_ref)
		VALUES (${uuid(changeRequestId)}, ${uuid(repositoryId)}, ${literal(args.number)}, 'E2E pull request', ${literal(args.state)}, ${args.draft ? "TRUE" : "FALSE"}, ${literal(args.baseRef ?? "main")}, 'feat/e2e');
		INSERT INTO git_task_links (task_id, change_request_id)
		VALUES (${uuid(args.taskId)}, ${uuid(changeRequestId)});
	`);
	return { repositoryId, changeRequestId, number: args.number };
}

/** What a webhook leaves behind when the request later changes state. */
export function setChangeRequestState(
	changeRequestId: string,
	state: ChangeRequestState,
): void {
	sql(
		`UPDATE git_change_requests SET state = ${literal(state)}, updated_at = NOW() WHERE id = ${uuid(changeRequestId)}`,
	);
}

/**
 * Queues a Git automation event the way core does after a webhook: on the
 * trigger stream, with the project (and task, when the event has one).
 */
export function emitGitEvent(
	topic: string,
	payload: { project_id: string; task_id?: string } & Record<string, unknown>,
): void {
	compose([
		"exec",
		"-T",
		"valkey",
		"valkey-cli",
		"XADD",
		TRIGGER_STREAM,
		"*",
		"type",
		topic,
		"payload",
		JSON.stringify(payload),
	]);
}
