export interface PacaConfig {
	apiKey: string;
	baseURL: string;
	/**
	 * Base URL used to resolve plugin MCP entry URLs (e.g. relative paths like
	 * `/plugins-mcp/<id>/mcp.js`).  Defaults to `baseURL` when not set.
	 *
	 * In Docker deployments the MCP bundles are served by the gateway (Caddy),
	 * not by the API service, so this should be set to the gateway's internal
	 * URL (e.g. `http://gateway`).
	 */
	gatewayURL?: string;
	/** Agent UUID forwarded as X-Agent-ID on every API request. */
	agentId?: string;
	/**
	 * Project UUID — required when agentId is provided AND the agent is
	 * project-scoped ("single-project agent mode": every tool call is
	 * pinned to this one project, see server.ts). Left unset for a
	 * global-scope agent (agentId set, no fixed project) or a personal API
	 * key — either runs "unpinned": each tool call may target any project
	 * the caller has permission in, passed explicitly per call.
	 */
	projectId?: string;
}

export interface PermissionMap {
	global: Record<string, boolean>;
	projects: Record<string, Record<string, boolean>>;
}

export interface SuccessEnvelope<T> {
	success: true;
	data: T;
	request_id?: string;
}

export interface ApiErrorEnvelope {
	success: false;
	error_code: string;
	error: string;
	request_id?: string;
}

export type ApiEnvelope<T> = SuccessEnvelope<T> | ApiErrorEnvelope;

// ==================== Project ====================

export interface Project {
	id: string;
	name: string;
	description: string;
	task_id_prefix: string;
	settings: Record<string, unknown>;
	created_by?: string;
	created_at: string;
}

export interface ProjectListResult {
	items: Project[];
	total: number;
	page: number;
	page_size: number;
}

export interface CreateProjectInput {
	name: string;
	description?: string;
	task_id_prefix?: string;
}

export interface UpdateProjectInput {
	name?: string;
	description?: string;
	task_id_prefix?: string;
}

// ==================== Task ====================

export interface Task {
	id: string;
	project_id: string;
	title: string;
	task_number: number;
	task_type_id?: string | null;
	status_id?: string | null;
	sprint_id?: string | null;
	parent_task_id?: string | null;
	description?: unknown[] | null;
	importance: number;
	story_points?: number | null;
	assignee_ids?: string[];
	reporter_id?: string | null;
	custom_fields: Record<string, unknown>;
	start_date?: string | null;
	due_date?: string | null;
	tags?: string[];
	view_position?: number | null;
	view_group_key?: string | null;
	created_at: string;
	updated_at: string;
}

export interface TaskListResult {
	items: Task[];
	total: number;
	page: number;
	page_size: number;
}

export interface CreateTaskInput {
	project_id: string;
	title: string;
	description?: string;
	status_id?: string | null;
	task_type_id?: string | null;
	sprint_id?: string | null;
	assignee_id?: string | null;
	parent_task_id?: string | null;
	importance?: number;
	story_points?: number | null;
	tags?: string[];
	start_date?: string | null;
	due_date?: string | null;
}

export interface UpdateTaskInput {
	title?: string;
	description?: string;
	status_id?: string | null;
	task_type_id?: string | null;
	sprint_id?: string | null;
	assignee_id?: string | null;
	reporter_id?: string | null;
	parent_task_id?: string | null;
	importance?: number;
	story_points?: number | null;
	tags?: string[];
	start_date?: string | null;
	due_date?: string | null;
	custom_fields?: Record<string, unknown>;
}

// ==================== Sprint ====================

export type SprintStatus = "planned" | "active" | "completed";

export interface Sprint {
	id: string;
	project_id: string;
	name: string;
	start_date?: string | null;
	end_date?: string | null;
	goal?: string | null;
	status: SprintStatus;
	created_at: string;
	updated_at: string;
}

export interface SprintListResult {
	items: Sprint[];
}

export interface CreateSprintInput {
	project_id: string;
	name: string;
	start_date?: string | null;
	end_date?: string | null;
	goal?: string | null;
	status?: SprintStatus;
}

export interface UpdateSprintInput {
	name?: string;
	start_date?: string | null;
	end_date?: string | null;
	goal?: string | null;
	status?: SprintStatus;
}

export interface CompleteSprintInput {
	move_to_sprint_id?: string | null;
}

// ==================== Document ====================

export interface Document {
	id: string;
	project_id?: string | null;
	folder_id?: string | null;
	title: string;
	content: unknown[] | null;
	position: number;
	created_by?: string | null;
	updated_by?: string | null;
	created_at: string;
	updated_at: string;
}

export interface DocumentListResult {
	items: Document[];
}

export interface CreateDocumentInput {
	project_id: string;
	title: string;
	folder_id?: string | null;
	content?: string;
	position?: number;
}

export interface UpdateDocumentInput {
	title?: string;
	content?: string;
	folder_id?: string | null;
	position?: number;
}

// ==================== Document Folders ====================

export interface DocumentFolder {
	id: string;
	project_id?: string | null;
	parent_id?: string | null;
	name: string;
	position: number;
	created_by?: string | null;
	created_at: string;
	updated_at: string;
}

export interface DocumentFolderListResult {
	items: DocumentFolder[];
}

export interface CreateFolderInput {
	name: string;
	parent_id?: string;
	position?: number;
}

export interface UpdateFolderInput {
	name?: string;
	parent_id?: string | null;
	position?: number;
}

// ==================== Document Snapshots ====================

export interface DocumentSnapshot {
	id: string;
	document_id?: string | null;
	title: string;
	content: unknown[] | null;
	snapshot_number: number;
	created_by?: string | null;
	created_by_name?: string;
	created_at: string;
}

export interface DocumentSnapshotListResult {
	items: DocumentSnapshot[];
}

// ==================== Document Activities ====================

export type DocActivityType =
	| "doc.created"
	| "doc.updated"
	| "doc.deleted"
	| "doc.moved"
	| "doc.folder.created"
	| "doc.folder.updated"
	| "doc.folder.deleted"
	| "comment";

export interface FieldChange {
	field: string;
	old: string;
	new: string;
}

export interface DocActivityContent {
	text?: unknown;
	changes?: FieldChange[] | null;
	[key: string]: unknown;
}

export interface DocumentActivity {
	id: string;
	document_id: string;
	actor_id: string | null;
	actor_name: string;
	actor_username: string;
	activity_type: DocActivityType;
	content: string | DocActivityContent | null;
	created_at: string;
	updated_at: string;
}

export interface DocumentActivityListResult {
	items: DocumentActivity[];
}

// ==================== Document Comments ====================
// The API's add/update comment endpoints both respond with the same
// DocActivityResponse DTO as list-activities (actor_id/actor_name/
// actor_username, not a separate user_id/user_name shape), so comment
// methods are typed as DocumentActivity rather than a distinct interface.

// ==================== Project Members ====================

export interface ProjectMember {
	id: string;
	project_id: string;
	user_id: string;
	project_role_id: string;
	username: string;
	full_name: string;
	role_name: string;
	joined_at?: string;
}

export interface AddMemberInput {
	user_id: string;
	project_role_id: string;
}

export interface UpdateMemberRoleInput {
	project_role_id: string;
}

// ==================== Project Roles ====================

export interface ProjectRole {
	id: string;
	project_id?: string;
	role_name: string;
	description?: string | null;
	permissions: Record<string, unknown>;
	is_system?: boolean;
	created_at: string;
	updated_at: string;
}

export interface CreateRoleInput {
	role_name: string;
	description?: string;
	permissions: Record<string, unknown>;
}

export interface UpdateRoleInput {
	role_name?: string;
	description?: string;
	permissions?: Record<string, unknown>;
}

// ==================== Task Types ====================

export interface TaskType {
	id: string;
	project_id: string;
	name: string;
	icon?: string | null;
	color?: string | null;
	description?: string | null;
	is_default?: boolean;
	is_system?: boolean;
	created_at: string;
	updated_at: string;
}

export interface CreateTaskTypeInput {
	name: string;
	icon?: string;
	color?: string;
	description?: string;
}

export interface UpdateTaskTypeInput {
	name?: string;
	icon?: string;
	color?: string;
	description?: string;
}

// ==================== Task Statuses ====================

export type StatusCategory =
	| "backlog"
	| "refinement"
	| "ready"
	| "todo"
	| "inprogress"
	| "done";

export interface TaskStatus {
	id: string;
	project_id: string;
	name: string;
	color?: string | null;
	position: number;
	category: StatusCategory;
	is_default?: boolean;
	created_at: string;
	updated_at: string;
}

export interface CreateTaskStatusInput {
	name: string;
	color?: string;
	category: StatusCategory;
	position: number;
}

export interface UpdateTaskStatusInput {
	name?: string;
	color?: string;
	category?: StatusCategory;
	position?: number;
}

// ==================== Views ====================

export type ViewType = "table" | "board" | "roadmap" | "plugin";
export type ViewLayout = "Board" | "Table" | "Roadmap";
export type ViewsContext = "sprint" | "backlog" | "timeline";

export type FilterEntry = boolean | FilterConfig;

export interface FilterConfig {
	all: boolean;
	items?: Record<string, FilterEntry>;
}

export interface CustomFieldFilterConfig {
	selector?: FilterConfig;
	min?: number;
	max?: number;
	after?: string;
	before?: string;
	contains?: string;
}

export interface ViewFilters {
	task_types?: FilterConfig;
	statuses?: FilterConfig;
	assignees?: FilterConfig;
	sprints?: FilterConfig;
	custom_fields?: Record<string, CustomFieldFilterConfig>;
}

export interface ViewConfig {
	fields?: string[];
	column_by?: string;
	swimlanes?: string;
	sort_by?: string;
	field_sum?: string;
	slice_by?: string;
	filters?: ViewFilters;
	/** Required when view_type is "plugin": the reverse-DNS plugin manifest ID that provides this view (e.g. "com.paca.dashboard"). On update_view, null explicitly clears an existing binding. */
	plugin_manifest_id?: string | null;
	/** Required when view_type is "plugin": the component name from that plugin's "view" extension point (e.g. "DashboardIntegrationView"). On update_view, null explicitly clears an existing binding. */
	plugin_component?: string | null;
}

export interface View {
	id: string;
	project_id: string;
	name: string;
	view_type: ViewType;
	layout?: ViewLayout;
	config?: ViewConfig;
	context?: string;
	sprint_id?: string | null;
	position: number;
	created_at: string;
	updated_at: string;
}

export interface CreateViewInput {
	name: string;
	view_type: ViewType;
	context?: string;
	sprint_id?: string | null;
	config?: ViewConfig;
}

export interface UpdateViewInput {
	name?: string;
	view_type?: ViewType;
	context?: string;
	sprint_id?: string | null;
	config?: ViewConfig;
	position?: number;
}

export interface TaskPosition {
	task_id: string;
	view_id: string;
	position: number;
	group_key?: string | null;
}

export interface ReorderViewsInput {
	view_ids: string[];
}

export interface BulkMoveTasksInput {
	task_id: string;
	target_view_id: string;
	target_status_id: string | null;
	target_position?: number;
}

export interface MoveTaskInput {
	target_view_id: string;
	target_status_id: string | null;
	target_position?: number;
}

// ==================== Custom Fields ====================

export type FieldType =
	| "text"
	| "number"
	| "date"
	| "select"
	| "multi_select"
	| "boolean"
	| "url";

export interface CustomFieldDefinition {
	id: string;
	project_id: string;
	field_key: string;
	display_name: string;
	field_type: FieldType;
	options: string[];
	is_required: boolean;
	created_at: string;
	updated_at: string;
}

export interface CreateCustomFieldInput {
	display_name: string;
	field_key: string;
	field_type: FieldType;
	options?: string[];
	is_required?: boolean;
}

export interface UpdateCustomFieldInput {
	display_name?: string;
	field_type?: FieldType;
	options?: string[];
	is_required?: boolean;
}

// ==================== Attachments ====================

export interface AttachmentFile {
	id: string;
	file_name: string;
	content_type: string;
	file_size: number;
	created_at: string;
}

export interface Attachment {
	id: string;
	task_id: string;
	file_id: string;
	created_by?: string | null;
	created_at: string;
	file: AttachmentFile;
}

export interface UploadSession {
	file_id: string;
	is_multipart: boolean;
	upload_url?: string;
	multipart?: {
		upload_id: string;
		parts: Array<{
			part_number: number;
			upload_url: string;
		}>;
	};
}

// ==================== Task Activities & Comments ====================

export interface TaskActivity {
	id: string;
	task_id: string;
	actor_id?: string | null;
	actor_name: string;
	actor_username: string;
	activity_type: string;
	content: Record<string, unknown>;
	created_at: string;
	updated_at: string;
}

export interface TaskActivityListResult {
	items: TaskActivity[];
}

export interface TaskComment {
	id: string;
	task_id: string;
	user_id: string;
	user_name: string;
	content: string;
	created_at: string;
	updated_at: string;
}

export interface CreateCommentInput {
	content: string;
}

export interface UpdateCommentInput {
	content: string;
}

// ==================== GitHub Integration ====================

export interface GitHubIntegration {
	id: string;
	project_id: string;
	created_at: string;
	updated_at: string;
}

export interface AccessibleRepo {
	full_name: string;
	owner: string;
	repo_name: string;
	default_branch: string;
	private: boolean;
	description: string;
}

export interface GitHubRepository {
	id: string;
	project_id: string;
	integration_id: string;
	owner: string;
	repo_name: string;
	full_name: string;
	default_branch: string;
	webhook_id: number;
	created_at: string;
	updated_at: string;
}

export interface PullRequest {
	id: string;
	project_id: string;
	repo_id: string;
	pr_number: number;
	github_pr_id: number;
	title: string;
	state: "open" | "closed" | "merged";
	html_url: string;
	head_branch: string;
	base_branch: string;
	author: string;
	merged_at: string | null;
	created_at: string;
	updated_at: string;
}

export interface TaskBranch {
	id: string;
	task_id: string;
	repo_id: string;
	branch_name: string;
	created_at: string;
}

export interface SetTokenInput {
	token: string;
}

export interface LinkRepositoryInput {
	owner: string;
	repo_name: string;
}

export interface LinkPRInput {
	repo_id: string;
	pr_number: number;
}

export interface CreateBranchInput {
	repo_id: string;
	branch_name: string;
	source_branch?: string;
}

export interface CreateBranchResult {
	branch_name: string;
}

// ==================== Task Links ====================

export type LinkType = "blocks" | "relates_to" | "duplicates";
export type DisplayLinkType =
	| "blocks"
	| "relates_to"
	| "duplicates"
	| "is_blocked_by"
	| "is_duplicated_by";

export interface LinkedTaskSummary {
	id: string;
	task_number: number;
	title: string;
	status_id?: string | null;
	task_type_id?: string | null;
}

export interface TaskLink {
	id: string;
	source_task_id: string;
	target_task_id: string;
	link_type: LinkType;
	display_link_type: DisplayLinkType;
	linked_task: LinkedTaskSummary;
	created_by?: string | null;
	created_at: string;
}

export interface CreateTaskLinkInput {
	target_task_id: string;
	link_type: LinkType;
}

// ==================== Automation ====================
// A project-scoped graph of Trigger/Condition/Action nodes connected by
// edges — replaces the old task-dependency workflow canvas and the flat
// trigger/condition/action rule list with one unified model.

export type AutomationStatus = "draft" | "active" | "archived";
export type AutomationNodeKind = "trigger" | "condition" | "action";

export interface Automation {
	id: string;
	project_id: string;
	name: string;
	description: string;
	status: AutomationStatus;
	created_by?: string | null;
	created_at: string;
	updated_at: string;
}

export interface AutomationNode {
	id: string;
	automation_id: string;
	kind: AutomationNodeKind;
	type: string;
	config: Record<string, unknown>;
	pos_x: number;
	pos_y: number;
}

export interface AutomationEdge {
	id: string;
	automation_id: string;
	source_node_id: string;
	source_handle?: string | null;
	target_node_id: string;
}

export interface AutomationGraph {
	automation: Automation;
	nodes: AutomationNode[];
	edges: AutomationEdge[];
}

export type AutomationRunStatus = "running" | "completed" | "failed";
export type AutomationRunStepStatus = "completed" | "failed" | "skipped";

export interface AutomationRun {
	id: string;
	automation_id: string;
	trigger_node_id: string;
	task_id: string;
	status: AutomationRunStatus;
	started_at: string;
	finished_at?: string | null;
}

export interface AutomationRunStep {
	id: string;
	run_id: string;
	node_id: string;
	status: AutomationRunStepStatus;
	input_snapshot?: Record<string, unknown> | null;
	output_snapshot?: Record<string, unknown> | null;
	error?: string;
	executed_at: string;
}

export interface CreateAutomationInput {
	name: string;
	description?: string;
}

export interface UpdateAutomationInput {
	name?: string;
	description?: string;
}

export interface AddAutomationNodeInput {
	kind: AutomationNodeKind;
	type: string;
	config?: Record<string, unknown>;
	pos_x?: number;
	pos_y?: number;
}

export interface UpdateAutomationNodeInput {
	config?: Record<string, unknown>;
	pos_x?: number;
	pos_y?: number;
}

export interface AddAutomationEdgeInput {
	source_node_id: string;
	source_handle?: string | null;
	target_node_id: string;
}

// ==================== API Response Helpers ====================

export interface APIResponse<T> {
	items?: T[];
	[key: string]: any;
}
