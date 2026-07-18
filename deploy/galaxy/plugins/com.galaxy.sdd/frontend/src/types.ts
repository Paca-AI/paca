// API shapes returned by the SDD Coordination Server read API (central/index.js),
// reached through the same-origin /sdd-api proxy. Field names match the server's
// JSON exactly (these are ported from the standalone client's lib/types.ts).

export interface TeamOverview {
	machines_online: number;
	machines_total: number;
	active_devs: number;
	total_users: number;
	total_sessions: number;
	active_sessions: number;
	total_events: number;
	open_conflicts: number;
	pending_gates: number;
	tasksByStatus: Record<string, number>;
	recent: Array<{
		phase: string | null;
		level: number | null;
		tool_name: string | null;
		created_at: string;
		hostname: string | null;
		user_name: string | null;
	}>;
}

export interface TeamBar {
	label: string;
	n: number;
}

export interface TeamAnalytics {
	byUser: TeamBar[];
	byHost: TeamBar[];
	byRepo: TeamBar[];
	phaseDist: TeamBar[];
	levelDist: Array<{ level: number; n: number }>;
	daily: Array<{ day: string; n: number }>;
}

export interface FleetMachine {
	user_id: string;
	user_name: string | null;
	email: string | null;
	hostname: string | null;
	sessions: number;
	last_seen: string | null;
	current_phase: string | null;
	current_level: number | null;
}

export interface FleetResult {
	machines: FleetMachine[];
}

export interface TeamCoordination {
	conflicts: Array<{
		id: number;
		kind: string;
		conflict_key: string | null;
		detail: unknown;
		created_at: string;
	}>;
	byRepo: Array<{
		repo: string;
		devs: number;
		sessions: number;
		machines: number;
		phases: string[];
		dev_names: string[];
	}>;
}

// ── SDD phases / governance ──────────────────────────────────────────────────
export interface SddPhase {
	key: string;
	label: string;
	owner?: string | null;
	gate?: string | null;
}

export interface SddAgentCard {
	id: string;
	name: string;
	type: string;
	status: string;
	session_id: string;
	sdd_phase: string | null;
	sdd_level: number | null;
	spec_doc_id: string | null;
	spec_version: string | null;
	email?: string | null;
	user_name?: string | null;
}

export interface SddActivity {
	id: number;
	session_id: string;
	agent_id: string | null;
	hook_type: string | null;
	tool_name: string | null;
	phase: string | null;
	level: number | null;
	lifecycle: string | null;
	spec_doc_id: string | null;
	spec_version: string | null;
	shared_core_touch: number;
	file_path: string | null;
	summary: string | null;
	created_at: string;
	email?: string | null;
}

export interface SddOverview {
	phases: SddPhase[];
	phaseCounts: Record<string, number>;
	levelCounts: Record<string, number>;
	board: Record<string, SddAgentCard[]>;
	recent: SddActivity[];
	sharedCoreCount: number;
	unapprovedL3Count: number;
}

export interface SddSpecVersion {
	id: number;
	doc_id: string;
	version: string;
	title: string | null;
	source: string;
	implemented_ref: string | null;
	published_at: string;
}

export interface SddSpecVersionsResult {
	docs: Record<string, SddSpecVersion[]>;
	count: number;
}

export interface SddFlagsResult {
	sharedCore: SddActivity[];
	unapprovedL3: SddActivity[];
}

// ── Coordination tasks (read-only in the plugin) ─────────────────────────────
export interface Task {
	id: number;
	title: string;
	description: string | null;
	status: "todo" | "assigned" | "in_progress" | "review" | "done";
	assignee_user_id: string | null;
	assignee_hostname: string | null;
	assignee_name: string | null;
	assignee_email: string | null;
	creator_name: string | null;
	repo: string | null;
	spec_doc_id: string | null;
	priority: "low" | "normal" | "high";
	live_phase: string | null;
	live_level: number | null;
	live_at: string | null;
	created_at: string;
	updated_at: string;
}

export interface TasksResult {
	tasks: Task[];
	statuses: Task["status"][];
}

// ── Sessions + raw events (the two extra sub-tabs) ───────────────────────────
export interface SessionRow {
	id: string;
	user_id: string;
	hostname: string | null;
	repo: string | null;
	cwd: string | null;
	status: string;
	started_at?: string | null;
	updated_at: string;
	email: string | null;
	user_name: string | null;
}

export interface SessionsResult {
	sessions: SessionRow[];
	total: number;
}

export interface EventRow {
	id: number;
	session_id: string;
	user_id: string;
	event_type: string;
	tool_name: string | null;
	summary: string | null;
	created_at: string;
	email: string | null;
}

export interface EventsResult {
	events: EventRow[];
	total: number;
	limit: number;
	offset: number;
}
