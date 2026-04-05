import { queryOptions } from "@tanstack/react-query";

import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

// ── Shapes ────────────────────────────────────────────────────────────────────

export interface Project {
	id: string;
	name: string;
	description: string;
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

export interface ProjectMember {
	id: string;
	project_id: string;
	user_id: string;
	project_role_id: string;
	username: string;
	full_name: string;
	role_name: string;
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

export async function getProject(projectId: string): Promise<Project> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<Project>>(
		`/projects/${projectId}`,
	);
	return data.data;
}

export async function createProject(payload: {
	name: string;
	description?: string;
}): Promise<Project> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<Project>>(
		"/projects",
		payload,
	);
	return data.data;
}

export async function updateProject(
	projectId: string,
	payload: { name?: string; description?: string },
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
	payload: { user_id: string; project_role_id: string },
): Promise<ProjectMember> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<ProjectMember>
	>(`/projects/${projectId}/members`, payload);
	return data.data;
}

export async function updateProjectMemberRole(
	projectId: string,
	userId: string,
	payload: { project_role_id: string },
): Promise<ProjectMember> {
	const { data } = await apiClient.instance.patch<
		SuccessEnvelope<ProjectMember>
	>(`/projects/${projectId}/members/${userId}`, payload);
	return data.data;
}

export async function removeProjectMember(
	projectId: string,
	userId: string,
): Promise<void> {
	await apiClient.instance.delete(`/projects/${projectId}/members/${userId}`);
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

// ── Query Options ─────────────────────────────────────────────────────────────

export const projectsQueryOptions = (page = 1, pageSize = 50) =>
	queryOptions({
		queryKey: ["projects", { page, pageSize }],
		queryFn: () => listProjects(page, pageSize),
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
