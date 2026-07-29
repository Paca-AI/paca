import { queryOptions } from "@tanstack/react-query";
import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

// ── API shapes ────────────────────────────────────────────────────────────────

export type AutomationStatus = "draft" | "active" | "archived";
export type NodeKind = "trigger" | "condition" | "action";

export const TRIGGER_TYPES = [
	"status_changed",
	"task_created",
	"assignee_changed",
	"priority_changed",
	"tag_added",
	"due_date_reached",
	"predecessor_done",
	"cron",
	"api_trigger",
] as const;
export type TriggerType = (typeof TRIGGER_TYPES)[number];

export const ACTION_TYPES = [
	"assign",
	"set_status",
	"set_priority",
	"add_tag",
	"set_custom_field",
	"trigger_ai_agent",
	"call_api",
] as const;
export type ActionType = (typeof ACTION_TYPES)[number];

export const CONDITION_NODE_TYPE = "condition";

export interface Automation {
	id: string;
	project_id: string;
	name: string;
	description: string;
	status: AutomationStatus;
	created_by: string | null;
	created_at: string;
	updated_at: string;
}

export interface AutomationNode {
	id: string;
	automation_id: string;
	kind: NodeKind;
	type: string;
	config: Record<string, unknown>;
	pos_x: number;
	pos_y: number;
}

export interface AutomationEdge {
	id: string;
	automation_id: string;
	source_node_id: string;
	source_handle: string | null;
	target_node_id: string;
}

export interface AutomationGraph {
	automation: Automation;
	nodes: AutomationNode[];
	edges: AutomationEdge[];
}

export type RunStatus = "running" | "completed" | "failed";
export type RunStepStatus = "completed" | "failed" | "skipped";

export interface AutomationRun {
	id: string;
	automation_id: string;
	trigger_node_id: string;
	/** Null when the trigger that started this run had no target task
	 * configured (only possible when the whole walk stayed within call_api
	 * actions). */
	task_id: string | null;
	status: RunStatus;
	started_at: string;
	finished_at: string | null;
}

export interface AutomationRunStep {
	id: string;
	run_id: string;
	node_id: string;
	status: RunStepStatus;
	input_snapshot: Record<string, unknown> | null;
	output_snapshot: Record<string, unknown> | null;
	error?: string;
	executed_at: string;
}

export interface DependencyMapEntry {
	automation_id: string;
	automation_name: string;
	node_id: string;
	target_task_id: string;
	watched_task_ids: string[];
}

// A plugin-contributed node type — Type is reverse-DNS namespaced (e.g.
// "com.acme.github.pr_merged") and doubles as its own registry key.
export interface PluginNodeType {
	type: string;
	label: string;
	configSchema?: Record<string, unknown>;
	eventTopic?: string;
}

export interface PluginNodeTypes {
	triggers: PluginNodeType[];
	conditions: PluginNodeType[];
	actions: PluginNodeType[];
}

// ── Trigger/condition/action config shapes ───────────────────────────────────

export interface TriggerConfig {
	status_id?: string;
	tag?: string;
	due_date_offset_minutes?: number;
	watched_task_ids?: string[];
	/** predecessor_done: the task the graph runs against once every watched
	 * task is done — a distinct task from its predecessors, re-evaluated
	 * using its own current state.
	 *
	 * Also used by cron and api_trigger: both fire on a basis unrelated to
	 * any specific task event, so — like predecessor_done — they need an
	 * explicit fixed task for the graph to run against. */
	target_task_id?: string;
	/** cron: a standard 5-field cron expression (minute hour day-of-month
	 * month day-of-week), evaluated in UTC. */
	cron_expression?: string;
}

export type ConditionOperator =
	| "equals"
	| "not_equals"
	| "contains"
	| "greater_than"
	| "less_than"
	| "is_empty"
	| "is_not_empty";
export type ConditionField =
	| "status_id"
	| "task_type_id"
	| "importance"
	| "assignee_ids"
	| "tags"
	| "custom_field";

/** Which task a condition leaf or action operates on, relative to the
 * walk's own bound task — "self" (or omitted) is the walk's own task, the
 * default and every automation's behavior before this existed. The rest
 * resolve relative to that task: its linked tasks by relation type (the
 * same five directions the task-detail "Linked Tasks" section uses), its
 * parent, its children, or an explicit other task chosen by ID. */
export type TaskTargetKind =
	| "self"
	| "parent"
	| "children"
	| "blocks"
	| "is_blocked_by"
	| "relates_to"
	| "duplicates"
	| "is_duplicated_by"
	| "other";

/** Target kinds that can resolve to more than one task — a task can have
 * several children, or block/relate to/duplicate several other tasks. */
export const MULTI_VALUED_TARGET_KINDS: readonly TaskTargetKind[] = [
	"children",
	"blocks",
	"is_blocked_by",
	"relates_to",
	"duplicates",
	"is_duplicated_by",
];

export interface TaskTarget {
	kind: TaskTargetKind;
	/** other: the explicit task to operate on, unrelated to the walk's own
	 * task. Unused for every other kind. */
	other_task_id?: string;
}

export interface ConditionLeaf {
	field: ConditionField;
	field_key?: string;
	operator: ConditionOperator;
	value?: unknown;
	/** Retargets this leaf onto a task other than the walk's own bound task
	 * (omitted/self = evaluated against the walk's task directly). */
	target?: TaskTarget;
	/** How a MultiValued target's resolved tasks combine into one
	 * true/false: "all" requires every resolved task to satisfy the leaf;
	 * anything else (including omitted and "any") requires only one.
	 * Ignored unless target resolves to more than one task. */
	match_mode?: "any" | "all";
}

export const ELSE_HANDLE = "else";

export interface ConditionBranch {
	handle: string;
	label?: string;
	tree: ConditionLeaf;
}

export interface ConditionConfig {
	branches: ConditionBranch[];
}

export interface ActionConfig {
	member_id?: string;
	status_id?: string;
	importance?: number;
	tag?: string;
	field_key?: string;
	value?: unknown;
	message?: string;
	/** call_api: the outbound request to make when the walk reaches this node. */
	method?: string;
	url?: string;
	headers?: Record<string, string>;
	body?: string;
	/** Retargets this action onto a task other than the walk's own bound
	 * task (omitted/self = today's behavior). Not supported by call_api,
	 * which doesn't operate on a task at all. When target resolves to more
	 * than one task, the action fans out — applied once per resolved task. */
	target?: TaskTarget;
}

// ── API calls ─────────────────────────────────────────────────────────────────

export async function listAutomations(
	projectId: string,
	status?: AutomationStatus,
): Promise<Automation[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ items: Automation[] }>
	>(`/projects/${projectId}/automations`, { params: status ? { status } : {} });
	return data.data.items;
}

export async function createAutomation(
	projectId: string,
	payload: { name: string; description?: string },
): Promise<Automation> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<Automation>>(
		`/projects/${projectId}/automations`,
		payload,
	);
	return data.data;
}

export async function getAutomation(
	projectId: string,
	automationId: string,
): Promise<AutomationGraph> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<AutomationGraph>
	>(`/projects/${projectId}/automations/${automationId}`);
	return data.data;
}

export async function updateAutomation(
	projectId: string,
	automationId: string,
	payload: { name?: string; description?: string },
): Promise<Automation> {
	const { data } = await apiClient.instance.patch<SuccessEnvelope<Automation>>(
		`/projects/${projectId}/automations/${automationId}`,
		payload,
	);
	return data.data;
}

export async function deleteAutomation(
	projectId: string,
	automationId: string,
): Promise<void> {
	await apiClient.instance.delete(
		`/projects/${projectId}/automations/${automationId}`,
	);
}

export async function activateAutomation(
	projectId: string,
	automationId: string,
): Promise<Automation> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<Automation>>(
		`/projects/${projectId}/automations/${automationId}/activate`,
	);
	return data.data;
}

export async function archiveAutomation(
	projectId: string,
	automationId: string,
): Promise<Automation> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<Automation>>(
		`/projects/${projectId}/automations/${automationId}/archive`,
	);
	return data.data;
}

export async function revertAutomationToDraft(
	projectId: string,
	automationId: string,
): Promise<Automation> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<Automation>>(
		`/projects/${projectId}/automations/${automationId}/revert-to-draft`,
	);
	return data.data;
}

export async function addAutomationNode(
	projectId: string,
	automationId: string,
	payload: {
		kind: NodeKind;
		type: string;
		config: Record<string, unknown>;
		pos_x: number;
		pos_y: number;
	},
): Promise<AutomationNode> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<AutomationNode>
	>(`/projects/${projectId}/automations/${automationId}/nodes`, payload);
	return data.data;
}

export async function updateAutomationNode(
	projectId: string,
	automationId: string,
	nodeId: string,
	payload: {
		config?: Record<string, unknown>;
		pos_x?: number;
		pos_y?: number;
	},
): Promise<AutomationNode> {
	const { data } = await apiClient.instance.patch<
		SuccessEnvelope<AutomationNode>
	>(
		`/projects/${projectId}/automations/${automationId}/nodes/${nodeId}`,
		payload,
	);
	return data.data;
}

export async function removeAutomationNode(
	projectId: string,
	automationId: string,
	nodeId: string,
): Promise<void> {
	await apiClient.instance.delete(
		`/projects/${projectId}/automations/${automationId}/nodes/${nodeId}`,
	);
}

export interface WebhookTokenResponse {
	id: string;
	token_prefix: string;
	created_at: string;
	last_used_at: string | null;
}

/** Token is present only on this response — shown once, never retrievable
 * again afterward. */
export interface CreateWebhookTokenResponse extends WebhookTokenResponse {
	token: string;
}

export async function generateWebhookToken(
	projectId: string,
	automationId: string,
	nodeId: string,
): Promise<CreateWebhookTokenResponse> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<CreateWebhookTokenResponse>
	>(
		`/projects/${projectId}/automations/${automationId}/nodes/${nodeId}/webhook-token`,
	);
	return data.data;
}

export async function addAutomationEdge(
	projectId: string,
	automationId: string,
	payload: {
		source_node_id: string;
		source_handle?: string | null;
		target_node_id: string;
	},
): Promise<AutomationEdge> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<AutomationEdge>
	>(`/projects/${projectId}/automations/${automationId}/edges`, payload);
	return data.data;
}

export async function removeAutomationEdge(
	projectId: string,
	automationId: string,
	edgeId: string,
): Promise<void> {
	await apiClient.instance.delete(
		`/projects/${projectId}/automations/${automationId}/edges/${edgeId}`,
	);
}

export async function listAutomationRuns(
	projectId: string,
	automationId: string,
	limit = 50,
): Promise<AutomationRun[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ items: AutomationRun[] }>
	>(`/projects/${projectId}/automations/${automationId}/runs`, {
		params: { limit },
	});
	return data.data.items;
}

export async function listAutomationRunSteps(
	projectId: string,
	automationId: string,
	runId: string,
): Promise<AutomationRunStep[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ items: AutomationRunStep[] }>
	>(`/projects/${projectId}/automations/${automationId}/runs/${runId}/steps`);
	return data.data.items;
}

export async function getAutomationDependencyMap(
	projectId: string,
): Promise<DependencyMapEntry[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ items: DependencyMapEntry[] }>
	>(`/projects/${projectId}/automation-dependency-map`);
	return data.data.items;
}

export async function getPluginNodeTypes(
	projectId: string,
): Promise<PluginNodeTypes> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<PluginNodeTypes>
	>(`/projects/${projectId}/automation-plugin-node-types`);
	return data.data;
}

// ── Query Options ─────────────────────────────────────────────────────────────

export const automationsQueryOptions = (
	projectId: string,
	status?: AutomationStatus,
) =>
	queryOptions({
		queryKey: ["projects", projectId, "automations", { status }],
		queryFn: () => listAutomations(projectId, status),
	});

export const automationQueryOptions = (
	projectId: string,
	automationId: string,
) =>
	queryOptions({
		queryKey: ["projects", projectId, "automations", automationId],
		queryFn: () => getAutomation(projectId, automationId),
	});

export const automationRunsQueryOptions = (
	projectId: string,
	automationId: string,
) =>
	queryOptions({
		queryKey: ["projects", projectId, "automations", automationId, "runs"],
		queryFn: () => listAutomationRuns(projectId, automationId),
	});

export const automationRunStepsQueryOptions = (
	projectId: string,
	automationId: string,
	runId: string,
) =>
	queryOptions({
		queryKey: [
			"projects",
			projectId,
			"automations",
			automationId,
			"runs",
			runId,
			"steps",
		],
		queryFn: () => listAutomationRunSteps(projectId, automationId, runId),
	});

export const automationDependencyMapQueryOptions = (projectId: string) =>
	queryOptions({
		queryKey: ["projects", projectId, "automation-dependency-map"],
		queryFn: () => getAutomationDependencyMap(projectId),
	});

export const pluginNodeTypesQueryOptions = (projectId: string) =>
	queryOptions({
		queryKey: ["projects", projectId, "automation-plugin-node-types"],
		queryFn: () => getPluginNodeTypes(projectId),
	});
