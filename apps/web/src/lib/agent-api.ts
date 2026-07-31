import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";
import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

// ── Shapes ────────────────────────────────────────────────────────────────────

export interface AgentPreset {
	id: string;
	label: string;
	description: string;
	defaultLLMProvider: string;
	defaultLLMModel: string;
	defaultSystemPrompt: string;
}

export const AGENT_PRESETS: AgentPreset[] = [
	{
		id: "software-engineer",
		label: "Software Engineer",
		description:
			"An AI agent focused on implementing features and fixing bugs.",
		defaultLLMProvider: "anthropic",
		defaultLLMModel: "claude-sonnet-4-6",
		defaultSystemPrompt:
			"You are an expert software engineer. You implement features and fix bugs by writing clean, maintainable code and following best practices.",
	},
	{
		id: "code-reviewer",
		label: "Code Reviewer",
		description:
			"An AI agent that reviews code for quality, bugs, and best practices.",
		defaultLLMProvider: "anthropic",
		defaultLLMModel: "claude-sonnet-4-6",
		defaultSystemPrompt:
			"You are a meticulous code reviewer. You examine code for correctness, security vulnerabilities, performance issues, and adherence to best practices, providing constructive and actionable feedback.",
	},
	{
		id: "qa-engineer",
		label: "QA Engineer",
		description: "An AI agent specialized in writing and running tests.",
		defaultLLMProvider: "anthropic",
		defaultLLMModel: "claude-sonnet-4-6",
		defaultSystemPrompt:
			"You are a quality assurance engineer. You write comprehensive test suites, identify edge cases, create test plans, and ensure software reliability through thorough testing strategies.",
	},
	{
		id: "planner",
		label: "Planner",
		description:
			"An AI agent that breaks down goals into tasks and organises sprint work.",
		defaultLLMProvider: "anthropic",
		defaultLLMModel: "claude-sonnet-4-6",
		defaultSystemPrompt:
			"You are an expert project planner. You break down goals into well-defined tasks using `create_task`. For each task, set an appropriate task type (use `list_task_types` to see available types), a clear title, description, and acceptance criteria. Group related tasks under Epics or parent tasks where appropriate. Use `list_task_statuses` to understand the project's workflow.",
	},
	{
		id: "business-analyst",
		label: "Business Analyst",
		description:
			"An AI agent that writes requirements, user stories, and acceptance criteria.",
		defaultLLMProvider: "anthropic",
		defaultLLMModel: "claude-sonnet-4-6",
		defaultSystemPrompt:
			'You are an expert business analyst. You produce requirements by:\n- Writing detailed user stories (`create_task` with type Story) in the format "As a [persona], I want [goal] so that [benefit]".\n- Adding clear, testable acceptance criteria to each story.\n- Creating Epics (`create_task` with type Epic) to group related stories.\n- Documenting business rules, edge cases, and non-functional requirements as comments or task description updates.\n\nUse `list_task_types` and `list_tasks` to understand the project context and avoid duplicating requirements.',
	},
	{
		id: "custom",
		label: "Custom",
		description: "Start from scratch with your own configuration.",
		defaultLLMProvider: "",
		defaultLLMModel: "",
		defaultSystemPrompt: "",
	},
];
export interface AgentMCPServer {
	id: string;
	agent_id: string;
	server_name: string;
	transport: "stdio" | "sse" | "http";
	command?: string | null;
	args?: string[];
	url?: string | null;
	env?: Record<string, string>;
	is_enabled: boolean;
	created_at: string;
	updated_at: string;
}

export interface AgentSkill {
	id: string;
	agent_id: string;
	skill_name: string;
	skill_source: "inline" | "marketplace" | "github_url";
	skill_content?: string | null;
	source_url?: string | null;
	triggers: string[];
	is_enabled: boolean;
	created_at: string;
	updated_at: string;
}

export interface AgentEnvVar {
	id: string;
	agent_id: string;
	key: string;
	// Always "***" — the plaintext value is never returned by the API.
	value: string;
	created_at: string;
}

export type AgentType = "llm" | "acp";
export type ACPProvider = "claude-code" | "codex" | "gemini-cli" | "custom";

export interface Agent {
	id: string;
	project_id: string;
	name: string;
	handle: string;
	avatar_url?: string | null;
	agent_type: AgentType;
	llm_provider: string;
	llm_model: string;
	llm_base_url: string;
	acp_provider?: ACPProvider | null;
	acp_command?: string[];
	has_acp_bridge_token: boolean;
	system_prompt: string;
	git_committer_name: string;
	git_committer_email: string;
	member_id?: string | null;
	mcp_servers?: AgentMCPServer[];
	skills?: AgentSkill[];
	env_vars?: AgentEnvVar[];
	created_at: string;
	updated_at: string;
}

export type ConversationStatus =
	| "queued"
	| "running"
	| "paused"
	| "finished"
	| "failed"
	| "stopped";

export type ConversationTriggerType =
	| "task_assigned"
	| "comment_mention"
	| "chat_message"
	| "description_write"
	| "automation_message";

export interface AgentConversation {
	id: string;
	agent_id: string;
	project_id: string;
	trigger_type: ConversationTriggerType;
	task_id?: string | null;
	comment_id?: string | null;
	chat_session_id?: string | null;
	triggered_by_member_id?: string;
	status: ConversationStatus;
	iteration_count: number;
	error_message?: string | null;
	branch_name?: string | null;
	pr_url?: string | null;
	started_at?: string | null;
	finished_at?: string | null;
	created_at: string;
	updated_at: string;
}

export interface AgentConversationEvent {
	id: string;
	conversation_id: string;
	event_index: number;
	event_type: string;
	event_source: "agent" | "user" | "system";
	payload: Record<string, unknown>;
	created_at: string;
}

export interface AgentChatSession {
	id: string;
	agent_id: string;
	project_id: string;
	member_id: string;
	title?: string | null;
	last_message_at?: string | null;
	created_at: string;
	updated_at: string;
}

// ── Agents ────────────────────────────────────────────────────────────────────

export async function listAgents(projectId: string): Promise<Agent[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ items: Agent[] }>
	>(`/projects/${projectId}/agents`);
	return data.data.items;
}

export async function getAgent(
	projectId: string,
	agentId: string,
): Promise<Agent> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<Agent>>(
		`/projects/${projectId}/agents/${agentId}`,
	);
	return data.data;
}

export async function createAgent(
	projectId: string,
	payload: {
		name: string;
		handle: string;
		agent_type?: AgentType;
		llm_provider?: string;
		llm_model?: string;
		llm_api_key?: string;
		llm_base_url?: string;
		acp_provider?: ACPProvider;
		acp_command?: string[];
		system_prompt?: string;
		git_committer_name?: string;
		git_committer_email?: string;
		project_role_id: string;
	},
): Promise<Agent> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<Agent>>(
		`/projects/${projectId}/agents`,
		payload,
	);
	return data.data;
}

export async function updateAgent(
	projectId: string,
	agentId: string,
	payload: {
		name?: string;
		handle?: string;
		llm_provider?: string;
		llm_model?: string;
		llm_api_key?: string;
		llm_base_url?: string | null;
		acp_provider?: ACPProvider;
		acp_command?: string[];
		system_prompt?: string;
		git_committer_name?: string;
		git_committer_email?: string;
	},
): Promise<Agent> {
	const { data } = await apiClient.instance.patch<SuccessEnvelope<Agent>>(
		`/projects/${projectId}/agents/${agentId}`,
		payload,
	);
	return data.data;
}

// ── ACP Local Bridge ─────────────────────────────────────────────────────────

export interface AcpBridgeToken {
	token: string;
	run_command: string;
}

export async function generateAcpBridgeToken(
	projectId: string,
	agentId: string,
): Promise<AcpBridgeToken> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<AcpBridgeToken>
	>(`/projects/${projectId}/agents/${agentId}/acp-bridge-token`);
	return data.data;
}

export interface AcpBridgeStatus {
	connected: boolean;
}

export async function getAcpBridgeStatus(
	projectId: string,
	agentId: string,
): Promise<AcpBridgeStatus> {
	// Unlike most endpoints, this one proxies ai-agent's response verbatim
	// (no SuccessEnvelope wrapping) — same pass-through pattern as
	// listLLMModels below.
	const { data } = await apiClient.instance.get<AcpBridgeStatus>(
		`/projects/${projectId}/agents/${agentId}/acp-bridge-status`,
	);
	return data;
}

export async function writeTaskDescriptionWithAI(
	projectId: string,
	taskId: string,
	agentId: string,
): Promise<AgentConversation> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<{ conversation: AgentConversation }>
	>(`/projects/${projectId}/tasks/${taskId}/write-with-ai`, {
		agent_id: agentId,
	});
	return data.data.conversation;
}

export async function deleteAgent(
	projectId: string,
	agentId: string,
): Promise<void> {
	await apiClient.instance.delete(`/projects/${projectId}/agents/${agentId}`);
}

// ── MCP Servers ───────────────────────────────────────────────────────────────

export async function listMCPServers(
	projectId: string,
	agentId: string,
): Promise<AgentMCPServer[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ items: AgentMCPServer[] }>
	>(`/projects/${projectId}/agents/${agentId}/mcp-servers`);
	return data.data.items;
}

export async function addMCPServer(
	projectId: string,
	agentId: string,
	payload: {
		server_name: string;
		transport: "stdio" | "sse" | "http";
		command?: string | null;
		args?: string[];
		url?: string | null;
		env?: Record<string, string>;
	},
): Promise<AgentMCPServer> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<AgentMCPServer>
	>(`/projects/${projectId}/agents/${agentId}/mcp-servers`, payload);
	return data.data;
}

export async function updateMCPServer(
	projectId: string,
	agentId: string,
	serverId: string,
	payload: {
		is_enabled?: boolean;
		command?: string;
		args?: string[];
		url?: string | null;
	},
): Promise<AgentMCPServer> {
	const { data } = await apiClient.instance.patch<
		SuccessEnvelope<AgentMCPServer>
	>(
		`/projects/${projectId}/agents/${agentId}/mcp-servers/${serverId}`,
		payload,
	);
	return data.data;
}

export async function deleteMCPServer(
	projectId: string,
	agentId: string,
	serverId: string,
): Promise<void> {
	await apiClient.instance.delete(
		`/projects/${projectId}/agents/${agentId}/mcp-servers/${serverId}`,
	);
}

// ── Skills ────────────────────────────────────────────────────────────────────

export async function listSkills(
	projectId: string,
	agentId: string,
): Promise<AgentSkill[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ items: AgentSkill[] }>
	>(`/projects/${projectId}/agents/${agentId}/skills`);
	return data.data.items;
}

export async function addSkill(
	projectId: string,
	agentId: string,
	payload: {
		skill_name: string;
		skill_source: "inline" | "marketplace" | "github_url";
		skill_content?: string;
		source_url?: string | null;
		triggers?: string[];
	},
): Promise<AgentSkill> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<AgentSkill>>(
		`/projects/${projectId}/agents/${agentId}/skills`,
		payload,
	);
	return data.data;
}

export async function updateSkill(
	projectId: string,
	agentId: string,
	skillId: string,
	payload: {
		is_enabled?: boolean;
		triggers?: string[];
		skill_content?: string;
	},
): Promise<AgentSkill> {
	const { data } = await apiClient.instance.patch<SuccessEnvelope<AgentSkill>>(
		`/projects/${projectId}/agents/${agentId}/skills/${skillId}`,
		payload,
	);
	return data.data;
}

export async function deleteSkill(
	projectId: string,
	agentId: string,
	skillId: string,
): Promise<void> {
	await apiClient.instance.delete(
		`/projects/${projectId}/agents/${agentId}/skills/${skillId}`,
	);
}

// ── Environment Variables ────────────────────────────────────────────────────

export async function listEnvVars(
	projectId: string,
	agentId: string,
): Promise<AgentEnvVar[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ items: AgentEnvVar[] }>
	>(`/projects/${projectId}/agents/${agentId}/env-vars`);
	return data.data.items;
}

export async function addEnvVar(
	projectId: string,
	agentId: string,
	payload: { key: string; value: string },
): Promise<AgentEnvVar> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<AgentEnvVar>>(
		`/projects/${projectId}/agents/${agentId}/env-vars`,
		payload,
	);
	return data.data;
}

export async function updateEnvVar(
	projectId: string,
	agentId: string,
	envVarId: string,
	payload: { value: string },
): Promise<AgentEnvVar> {
	const { data } = await apiClient.instance.patch<SuccessEnvelope<AgentEnvVar>>(
		`/projects/${projectId}/agents/${agentId}/env-vars/${envVarId}`,
		payload,
	);
	return data.data;
}

export async function deleteEnvVar(
	projectId: string,
	agentId: string,
	envVarId: string,
): Promise<void> {
	await apiClient.instance.delete(
		`/projects/${projectId}/agents/${agentId}/env-vars/${envVarId}`,
	);
}

// ── Conversations ─────────────────────────────────────────────────────────────

export interface ConversationListResult {
	items: AgentConversation[];
	page_size: number;
	next_cursor: string | null;
}

export const CONVERSATIONS_PAGE_SIZE = 20;

export interface ListConversationsOptions {
	agentIds?: string[];
	statuses?: ConversationStatus[];
	triggerTypes?: ConversationTriggerType[];
	/**
	 * Inclusive/exclusive created_at range, as local calendar dates
	 * ("YYYY-MM-DD") in the browser's own timezone — e.g. from the date
	 * picker. Converted to precise UTC-instant boundaries for the request
	 * (see localDateStartISO/localDateExclusiveEndISO) so the filtered range
	 * matches the user's local day rather than a UTC calendar day.
	 */
	createdAfter?: string;
	createdBefore?: string;
	/** Free-text search matched server-side against conversation event content. */
	search?: string;
	cursor?: string;
	pageSize?: number;
}

/** Start of the local calendar day `dateStr` ("YYYY-MM-DD"), as a UTC instant. */
function localDateStartISO(dateStr: string): string {
	const [y, m, d] = dateStr.split("-").map(Number);
	return new Date(y, m - 1, d, 0, 0, 0, 0).toISOString();
}

/**
 * Exclusive end of the local calendar day `dateStr` — i.e. the start of the
 * next local day — as a UTC instant. Sending this (rather than the bare
 * date) means a range picked in the user's local timezone covers the same
 * wall-clock day server-side, instead of being reinterpreted as a UTC day.
 */
function localDateExclusiveEndISO(dateStr: string): string {
	const [y, m, d] = dateStr.split("-").map(Number);
	return new Date(y, m - 1, d + 1, 0, 0, 0, 0).toISOString();
}

function buildConversationQueryParams(opts: ListConversationsOptions = {}) {
	const params: Record<string, string | number> = {};
	params.page_size = opts.pageSize ?? CONVERSATIONS_PAGE_SIZE;
	if (opts.cursor) params.cursor = opts.cursor;
	if (opts.agentIds && opts.agentIds.length > 0)
		params.agent_id = opts.agentIds.join(",");
	if (opts.statuses && opts.statuses.length > 0)
		params.status = opts.statuses.join(",");
	if (opts.triggerTypes && opts.triggerTypes.length > 0)
		params.trigger_type = opts.triggerTypes.join(",");
	if (opts.createdAfter)
		params.created_after = localDateStartISO(opts.createdAfter);
	if (opts.createdBefore)
		params.created_before = localDateExclusiveEndISO(opts.createdBefore);
	if (opts.search?.trim()) params.search = opts.search.trim();
	return params;
}

export async function listConversations(
	projectId: string,
	options?: ListConversationsOptions,
): Promise<ConversationListResult> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<ConversationListResult>
	>(`/projects/${projectId}/conversations`, {
		params: buildConversationQueryParams(options),
	});
	return data.data;
}

export async function getConversation(
	projectId: string,
	conversationId: string,
): Promise<AgentConversation> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<AgentConversation>
	>(`/projects/${projectId}/conversations/${conversationId}`);
	return data.data;
}

export async function listConversationEvents(
	projectId: string,
	conversationId: string,
): Promise<AgentConversationEvent[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ items: AgentConversationEvent[] }>
	>(`/projects/${projectId}/conversations/${conversationId}/events`, {
		params: { limit: 200 },
	});
	return data.data.items;
}

export async function stopConversation(
	projectId: string,
	conversationId: string,
): Promise<AgentConversation> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<AgentConversation>
	>(`/projects/${projectId}/conversations/${conversationId}/stop`);
	return data.data;
}

// pauseConversation interrupts the in-flight turn only — the sandbox stays
// alive and the conversation goes back to "paused", ready for another reply.
// This is what the assistant-ui Cancel/Stop button calls; stopConversation
// above is the full, permanent teardown.
export async function pauseConversation(
	projectId: string,
	conversationId: string,
): Promise<{ message: string }> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<{ message: string }>
	>(`/projects/${projectId}/conversations/${conversationId}/pause`);
	return data.data;
}

// sendConversationMessage replies to a conversation directly by id, rather
// than through a chat session — used for ACP conversations of any trigger
// type (task_assigned, comment_mention, etc.), which stay resumable straight
// through a terminal status since the user's local bridge daemon keeps them
// alive regardless of why they were started. LLM conversations don't use
// this path today; that flow is still the chat-session-based sendChatMessage
// above.
export async function sendConversationMessage(
	projectId: string,
	conversationId: string,
	message: string,
): Promise<void> {
	await apiClient.instance.post(
		`/projects/${projectId}/conversations/${conversationId}/messages`,
		{ message },
	);
}

// heartbeatConversation refreshes a chat conversation's idle timer on the
// ai-agent service — pinged periodically while a conversation is loaded in a
// browser tab so its sandbox isn't reclaimed as long as the tab stays open.
export async function heartbeatConversation(
	projectId: string,
	conversationId: string,
): Promise<void> {
	await apiClient.instance.post(
		`/projects/${projectId}/conversations/${conversationId}/heartbeat`,
	);
}

export const CONVERSATION_HEARTBEAT_INTERVAL_MS = 30_000;

// ── Chat Sessions ─────────────────────────────────────────────────────────────

export async function listChatSessions(
	projectId: string,
	agentId: string,
): Promise<AgentChatSession[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ items: AgentChatSession[] }>
	>(`/projects/${projectId}/agents/${agentId}/chat-sessions`);
	return data.data.items;
}

export interface StartChatSessionResponse {
	session: AgentChatSession;
	conversation: AgentConversation;
}

export async function startChatSession(
	projectId: string,
	agentId: string,
	payload: { message: string; title?: string },
): Promise<StartChatSessionResponse> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<StartChatSessionResponse>
	>(`/projects/${projectId}/agents/${agentId}/chat-sessions`, payload);
	return data.data;
}

export async function sendChatMessage(
	projectId: string,
	agentId: string,
	sessionId: string,
	payload: { message: string },
): Promise<AgentConversation> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<{ conversation: AgentConversation }>
	>(
		`/projects/${projectId}/agents/${agentId}/chat-sessions/${sessionId}/messages`,
		payload,
	);
	return data.data.conversation;
}

// ── Query Options ─────────────────────────────────────────────────────────────

export const agentsQueryOptions = (projectId: string) =>
	queryOptions({
		queryKey: ["projects", projectId, "agents"],
		queryFn: () => listAgents(projectId),
	});

export const agentQueryOptions = (projectId: string, agentId: string) =>
	queryOptions({
		queryKey: ["projects", projectId, "agents", agentId],
		queryFn: () => getAgent(projectId, agentId),
	});

export const agentMCPServersQueryOptions = (
	projectId: string,
	agentId: string,
) =>
	queryOptions({
		queryKey: ["projects", projectId, "agents", agentId, "mcp-servers"],
		queryFn: () => listMCPServers(projectId, agentId),
	});

export const agentSkillsQueryOptions = (projectId: string, agentId: string) =>
	queryOptions({
		queryKey: ["projects", projectId, "agents", agentId, "skills"],
		queryFn: () => listSkills(projectId, agentId),
	});

export const agentEnvVarsQueryOptions = (projectId: string, agentId: string) =>
	queryOptions({
		queryKey: ["projects", projectId, "agents", agentId, "env-vars"],
		queryFn: () => listEnvVars(projectId, agentId),
	});

// Fetched once (no polling) — kept live afterward via useProjectRealtime's
// direct cache update on "agent.acp_bridge.status" events (published by
// services/ai-agent's acp_bridge.py on every bridge connect/disconnect).
export const acpBridgeStatusQueryOptions = (
	projectId: string,
	agentId: string,
	options?: { enabled?: boolean },
) =>
	queryOptions({
		queryKey: ["projects", projectId, "agents", agentId, "acp-bridge-status"],
		queryFn: () => getAcpBridgeStatus(projectId, agentId),
		enabled: options?.enabled ?? true,
	});

// No refetchInterval: kept live via useProjectRealtime's socket-driven
// invalidation of the ["projects", projectId, "conversations"] prefix on
// every "agent.*" event instead of polling. Cursor-paginated — each page
// carries the opaque cursor to resume after its last item, so fetchNextPage
// just forwards the backend's next_cursor until it comes back null.
export type ConversationFilters = Omit<
	ListConversationsOptions,
	"cursor" | "pageSize"
>;

export const conversationsQueryOptions = (
	projectId: string,
	filters: ConversationFilters = {},
) =>
	infiniteQueryOptions({
		queryKey: ["projects", projectId, "conversations", filters],
		queryFn: ({ pageParam }: { pageParam: string | undefined }) =>
			listConversations(projectId, {
				...filters,
				cursor: pageParam,
				pageSize: CONVERSATIONS_PAGE_SIZE,
			}),
		initialPageParam: undefined as string | undefined,
		getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
	});

export const conversationQueryOptions = (
	projectId: string,
	conversationId: string,
) =>
	queryOptions({
		queryKey: ["projects", projectId, "conversations", conversationId],
		queryFn: () => getConversation(projectId, conversationId),
	});

export const conversationEventsQueryOptions = (
	projectId: string,
	conversationId: string,
) =>
	queryOptions({
		queryKey: [
			"projects",
			projectId,
			"conversations",
			conversationId,
			"events",
		],
		queryFn: () => listConversationEvents(projectId, conversationId),
	});

export const chatSessionsQueryOptions = (projectId: string, agentId: string) =>
	queryOptions({
		queryKey: ["projects", projectId, "agents", agentId, "chat-sessions"],
		queryFn: () => listChatSessions(projectId, agentId),
	});

// ── LLM Models ────────────────────────────────────────────────────────────────

export interface LLMProviderInfo {
	models: string[];
	base_url: string | null;
}

export interface LLMModelsResponse {
	[provider: string]: LLMProviderInfo;
}

export async function listLLMModels(): Promise<LLMModelsResponse> {
	const { data } =
		await apiClient.instance.get<LLMModelsResponse>("/agents/llm-models");
	return data;
}

export const llmModelsQueryOptions = queryOptions({
	queryKey: ["agents", "llm-models"],
	queryFn: listLLMModels,
	staleTime: 10 * 60 * 1000, // 10 min — provider list rarely changes
});

// ── Helpers ───────────────────────────────────────────────────────────────────

export const CONVERSATION_STATUS_LABELS: Record<ConversationStatus, string> = {
	queued: "Queued",
	running: "Running",
	paused: "Waiting for reply",
	finished: "Finished",
	failed: "Failed",
	stopped: "Stopped",
};

export const CONVERSATION_STATUS_COLORS: Record<ConversationStatus, string> = {
	queued: "text-muted-foreground",
	running: "text-blue-500",
	paused: "text-amber-500",
	finished: "text-emerald-500",
	failed: "text-destructive",
	stopped: "text-muted-foreground",
};

export const CONVERSATION_TRIGGER_TYPE_LABELS: Record<
	ConversationTriggerType,
	string
> = {
	task_assigned: "Task Assigned",
	comment_mention: "Comment Mention",
	chat_message: "Chat Message",
	description_write: "Description Write",
	automation_message: "Automation",
};
