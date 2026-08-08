import { queryOptions } from "@tanstack/react-query";

import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

// ── Shapes ────────────────────────────────────────────────────────────────────

export interface Project {
	id: string;
	name: string;
	description: string;
	is_public: boolean;
	task_id_prefix: string;
	settings: Record<string, unknown>;
	avatar_url?: string | null;
	avatar_thumb_url?: string | null;
	created_by?: string;
	created_at: string;
}

/** Text-avatar fallback for a project — one letter per word, up to two
 * words (e.g. "Test Project" -> "TP"). Shared so every surface (sidebar,
 * project cards, settings) renders the same initials for the same name. */
export function getProjectInitials(name: string): string {
	return name
		.split(/\s+/)
		.filter(Boolean)
		.slice(0, 2)
		.map((w) => w[0].toUpperCase())
		.join("");
}

export interface ProjectListResult {
	items: Project[];
	total: number;
	page: number;
	page_size: number;
}

export interface WorkspaceStats {
	open_task_count: number;
	team_member_count: number;
	ai_agent_count: number;
}

export interface ProjectMember {
	id: string;
	project_id: string;
	user_id: string;
	project_role_id: string;
	username: string;
	full_name: string;
	role_name: string;
	member_type?: string; // "human" | "agent"
	agent_id?: string;
	agent_name?: string;
	agent_handle?: string;
	avatar_url?: string | null;
	avatar_thumb_url?: string | null;
	// Only meaningful when member_type is "agent" — used to pick a default
	// provider-logo avatar when this member has no avatar_url of its own.
	agent_type?: string; // "llm" | "acp"
	agent_llm_provider?: string;
	agent_acp_provider?: string | null;
}

export interface ProjectRole {
	id: string;
	project_id?: string;
	role_name: string;
	permissions: Record<string, unknown>;
	created_at: string;
	updated_at: string;
}

// ── Project CRUD ──────────────────────────────────────────────────────────────

export async function listProjects(
	page = 1,
	pageSize = 50,
): Promise<ProjectListResult> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<ProjectListResult>
	>("/projects", { params: { page, page_size: pageSize } });
	return data.data;
}

export async function getWorkspaceStats(): Promise<WorkspaceStats> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<WorkspaceStats>
	>("/projects/workspace-stats");
	return data.data;
}

export async function getProject(projectId: string): Promise<Project> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<Project>>(
		`/projects/${projectId}`,
	);
	return data.data;
}

export async function createProject(payload: {
	name: string;
	description?: string;
	task_id_prefix?: string;
	is_public?: boolean;
}): Promise<Project> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<Project>>(
		"/projects",
		payload,
	);
	return data.data;
}

export async function updateProject(
	projectId: string,
	payload: {
		name?: string;
		description?: string;
		task_id_prefix?: string;
		is_public?: boolean;
	},
): Promise<Project> {
	const { data } = await apiClient.instance.patch<SuccessEnvelope<Project>>(
		`/projects/${projectId}`,
		payload,
	);
	return data.data;
}

export async function deleteProject(projectId: string): Promise<void> {
	await apiClient.instance.delete(`/projects/${projectId}`);
}

// ── Members ───────────────────────────────────────────────────────────────────

export async function listProjectMembers(
	projectId: string,
): Promise<ProjectMember[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<ProjectMember[]>
	>(`/projects/${projectId}/members`);
	return data.data;
}

export async function addProjectMember(
	projectId: string,
	payload:
		| { user_id: string; project_role_id: string }
		| { agent_id: string; project_role_id: string },
): Promise<ProjectMember> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<ProjectMember>
	>(`/projects/${projectId}/members`, payload);
	return data.data;
}

export async function updateProjectMemberRole(
	projectId: string,
	memberId: string,
	payload: { project_role_id: string },
): Promise<ProjectMember> {
	const { data } = await apiClient.instance.patch<
		SuccessEnvelope<ProjectMember>
	>(`/projects/${projectId}/members/${memberId}`, payload);
	return data.data;
}

export async function removeProjectMember(
	projectId: string,
	memberId: string,
): Promise<void> {
	await apiClient.instance.delete(`/projects/${projectId}/members/${memberId}`);
}

export async function getMyProjectPermissions(
	projectId: string,
): Promise<Record<string, boolean>> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ permissions: Record<string, boolean> }>
	>(`/projects/${projectId}/members/me/permissions`);
	return data.data.permissions;
}

// ── Roles ─────────────────────────────────────────────────────────────────────

export async function listProjectRoles(
	projectId: string,
): Promise<ProjectRole[]> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<ProjectRole[]>>(
		`/projects/${projectId}/roles`,
	);
	return data.data;
}

export async function createProjectRole(
	projectId: string,
	payload: { role_name: string; permissions?: Record<string, unknown> },
): Promise<ProjectRole> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<ProjectRole>>(
		`/projects/${projectId}/roles`,
		payload,
	);
	return data.data;
}

export async function updateProjectRole(
	projectId: string,
	roleId: string,
	payload: { role_name: string; permissions?: Record<string, unknown> },
): Promise<ProjectRole> {
	const { data } = await apiClient.instance.patch<SuccessEnvelope<ProjectRole>>(
		`/projects/${projectId}/roles/${roleId}`,
		payload,
	);
	return data.data;
}

export async function deleteProjectRole(
	projectId: string,
	roleId: string,
): Promise<void> {
	await apiClient.instance.delete(`/projects/${projectId}/roles/${roleId}`);
}

// ── Task Types ────────────────────────────────────────────────────────────────

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

export async function listTaskTypes(projectId: string): Promise<TaskType[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ items: TaskType[] }>
	>(`/projects/${projectId}/task-types`);
	return data.data.items;
}

export async function createTaskType(
	projectId: string,
	payload: {
		name: string;
		icon?: string | null;
		color?: string | null;
		description?: string | null;
	},
): Promise<TaskType> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<TaskType>>(
		`/projects/${projectId}/task-types`,
		payload,
	);
	return data.data;
}

export async function updateTaskType(
	projectId: string,
	typeId: string,
	payload: {
		name?: string;
		icon?: string | null;
		color?: string | null;
		description?: string | null;
	},
): Promise<TaskType> {
	const { data } = await apiClient.instance.patch<SuccessEnvelope<TaskType>>(
		`/projects/${projectId}/task-types/${typeId}`,
		payload,
	);
	return data.data;
}

export async function deleteTaskType(
	projectId: string,
	typeId: string,
): Promise<void> {
	await apiClient.instance.delete(
		`/projects/${projectId}/task-types/${typeId}`,
	);
}

export async function setDefaultTaskType(
	projectId: string,
	typeId: string,
): Promise<TaskType> {
	const { data } = await apiClient.instance.put<SuccessEnvelope<TaskType>>(
		`/projects/${projectId}/task-types/${typeId}/set-default`,
	);
	return data.data;
}

// ── Task type role helpers ─────────────────────────────────────────────────────

/** Returns true if this task type is the system "Epic" type. */
export function isEpicType(t: TaskType | undefined | null): boolean {
	return !!t && !!t.is_system && t.name === "Epic";
}

/** Finds the Epic system type from a list of task types. */
export function findEpicType(types: TaskType[]): TaskType | undefined {
	return types.find(isEpicType);
}

/** Returns non-epic task types (Task, Bug, Story, etc). */
export function getNormalTaskTypes(types: TaskType[]): TaskType[] {
	return types.filter((t) => !isEpicType(t));
}

// ── Task Statuses ─────────────────────────────────────────────────────────────

export type StatusCategory =
	| "backlog"
	| "refinement"
	| "ready"
	| "todo"
	| "inprogress"
	| "done";

export const STATUS_CATEGORIES: StatusCategory[] = [
	"backlog",
	"refinement",
	"ready",
	"todo",
	"inprogress",
	"done",
];

export const STATUS_CATEGORY_LABELS: Record<StatusCategory, string> = {
	backlog: "Backlog",
	refinement: "Refinement",
	ready: "Ready",
	todo: "To Do",
	inprogress: "In Progress",
	done: "Done",
};

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

export async function listTaskStatuses(
	projectId: string,
): Promise<TaskStatus[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ items: TaskStatus[] }>
	>(`/projects/${projectId}/task-statuses`);
	return data.data.items;
}

export async function createTaskStatus(
	projectId: string,
	payload: {
		name: string;
		color?: string | null;
		position: number;
		category: StatusCategory;
	},
): Promise<TaskStatus> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<TaskStatus>>(
		`/projects/${projectId}/task-statuses`,
		payload,
	);
	return data.data;
}

export async function updateTaskStatus(
	projectId: string,
	statusId: string,
	payload: {
		name?: string;
		color?: string | null;
		position?: number;
		category?: StatusCategory;
	},
): Promise<TaskStatus> {
	const { data } = await apiClient.instance.patch<SuccessEnvelope<TaskStatus>>(
		`/projects/${projectId}/task-statuses/${statusId}`,
		payload,
	);
	return data.data;
}

export async function deleteTaskStatus(
	projectId: string,
	statusId: string,
): Promise<void> {
	await apiClient.instance.delete(
		`/projects/${projectId}/task-statuses/${statusId}`,
	);
}

export async function setDefaultTaskStatus(
	projectId: string,
	statusId: string,
): Promise<TaskStatus> {
	const { data } = await apiClient.instance.put<SuccessEnvelope<TaskStatus>>(
		`/projects/${projectId}/task-statuses/${statusId}/set-default`,
	);
	return data.data;
}

/** Persists a new display order for task statuses in a single atomic request. */
export async function reorderTaskStatuses(
	projectId: string,
	orderedStatusIds: string[],
): Promise<void> {
	await apiClient.instance.put(
		`/projects/${projectId}/task-statuses/positions`,
		{
			status_ids: orderedStatusIds,
		},
	);
}

// ── Custom Field Definitions ─────────────────────────────────────────────────

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

export async function listCustomFieldDefinitions(
	projectId: string,
): Promise<CustomFieldDefinition[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ items: CustomFieldDefinition[] }>
	>(`/projects/${projectId}/custom-fields`);
	return data.data.items;
}

export async function getCustomFieldDefinition(
	projectId: string,
	fieldId: string,
): Promise<CustomFieldDefinition> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<CustomFieldDefinition>
	>(`/projects/${projectId}/custom-fields/${fieldId}`);
	return data.data;
}

export async function createCustomFieldDefinition(
	projectId: string,
	payload: {
		display_name: string;
		field_key: string;
		field_type: FieldType;
		options?: string[];
		is_required?: boolean;
	},
): Promise<CustomFieldDefinition> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<CustomFieldDefinition>
	>(`/projects/${projectId}/custom-fields`, payload);
	return data.data;
}

export async function updateCustomFieldDefinition(
	projectId: string,
	fieldId: string,
	payload: {
		display_name?: string;
		options?: string[];
		is_required?: boolean;
	},
): Promise<CustomFieldDefinition> {
	const { data } = await apiClient.instance.patch<
		SuccessEnvelope<CustomFieldDefinition>
	>(`/projects/${projectId}/custom-fields/${fieldId}`, payload);
	return data.data;
}

export async function deleteCustomFieldDefinition(
	projectId: string,
	fieldId: string,
): Promise<void> {
	await apiClient.instance.delete(
		`/projects/${projectId}/custom-fields/${fieldId}`,
	);
}

// ── Query Options ─────────────────────────────────────────────────────────────

export const projectsQueryOptions = (page = 1, pageSize = 50) =>
	queryOptions({
		queryKey: ["projects", { page, pageSize }],
		queryFn: () => listProjects(page, pageSize),
	});

export const workspaceStatsQueryOptions = () =>
	queryOptions({
		queryKey: ["workspace-stats"],
		queryFn: () => getWorkspaceStats(),
		staleTime: 2 * 60 * 1000,
	});

export const projectQueryOptions = (projectId: string) =>
	queryOptions({
		queryKey: ["projects", projectId],
		queryFn: () => getProject(projectId),
		staleTime: 2 * 60 * 1000,
	});

export const projectMembersQueryOptions = (projectId: string) =>
	queryOptions({
		queryKey: ["projects", projectId, "members"],
		queryFn: () => listProjectMembers(projectId),
	});

export const myProjectPermissionsQueryOptions = (projectId: string) =>
	queryOptions({
		queryKey: ["projects", projectId, "members", "me", "permissions"],
		queryFn: () => getMyProjectPermissions(projectId),
		staleTime: 2 * 60 * 1000,
		retry: false,
	});

export const projectRolesQueryOptions = (projectId: string) =>
	queryOptions({
		queryKey: ["projects", projectId, "roles"],
		queryFn: () => listProjectRoles(projectId),
	});

export const taskTypesQueryOptions = (projectId: string) =>
	queryOptions({
		queryKey: ["projects", projectId, "task-types"],
		queryFn: () => listTaskTypes(projectId),
	});

export const taskStatusesQueryOptions = (projectId: string) =>
	queryOptions({
		queryKey: ["projects", projectId, "task-statuses"],
		queryFn: () => listTaskStatuses(projectId),
	});

export const customFieldsQueryOptions = (projectId: string) =>
	queryOptions({
		queryKey: ["projects", projectId, "custom-fields"],
		queryFn: () => listCustomFieldDefinitions(projectId),
	});
