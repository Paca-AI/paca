import { queryOptions } from "@tanstack/react-query";

import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";
import type { Policy, PolicyIssue } from "./policy";

/** A role as the roles API returns it. */
export interface Role {
	id: string;
	name: string;
	description: string;
	policy: Policy;
	/** The owning project, or null for a platform role. */
	project_id: string | null;
	is_system: boolean;
	is_default: boolean;
	/** How many attachments reference the role (those inside the project in a
	 *  project listing). */
	attachment_count: number;
	created_at: string;
	updated_at: string;
}

export interface RoleSummary {
	id: string;
	name: string;
}

export interface RoleInput {
	name: string;
	description: string;
	policy: Policy;
}

export interface ValidateResult {
	valid: boolean;
	issues: PolicyIssue[];
}

export interface SimulateInput {
	policy: Policy;
	principal?: { type: "user" | "agent"; id: string };
	action: string;
	resource: string;
	attributes?: Record<string, string | boolean | string[]>;
}

export interface SimulateResult {
	allowed: boolean;
	matched: { role_id: string; sid: string; effect: string; index: number }[];
}

export interface AttributeDef {
	key: string;
	resource_kind: string;
	type: string;
	multi_valued: boolean;
	label_key: string;
}

/** Roles live under /admin/roles (platform) or /projects/{id}/roles (project). */
function base(projectId?: string): string {
	return projectId ? `/projects/${projectId}/roles` : "/admin/roles";
}

/** Role-less helpers (validate, simulate, actions, attribute schema) have a
 *  workspace-wide route and a per-project one for callers who only hold
 *  roles:read inside a project. */
function helperBase(projectId?: string): string {
	return projectId ? `/projects/${projectId}/roles` : "/roles";
}

export async function listRoles(projectId?: string): Promise<Role[]> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<Role[]>>(
		base(projectId),
	);
	return data.data;
}

export async function getRole(
	roleId: string,
	projectId?: string,
): Promise<Role> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<Role>>(
		`${base(projectId)}/${roleId}`,
	);
	return data.data;
}

export async function createRole(
	input: RoleInput,
	projectId?: string,
): Promise<Role> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<Role>>(
		base(projectId),
		input,
	);
	return data.data;
}

export async function updateRole(
	roleId: string,
	input: RoleInput,
	projectId?: string,
): Promise<Role> {
	const { data } = await apiClient.instance.put<SuccessEnvelope<Role>>(
		`${base(projectId)}/${roleId}`,
		input,
	);
	return data.data;
}

export async function deleteRole(
	roleId: string,
	projectId?: string,
): Promise<void> {
	await apiClient.instance.delete(`${base(projectId)}/${roleId}`);
}

/** Makes a platform role the one new users and global agents start with. */
export async function setDefaultRole(roleId: string): Promise<Role> {
	const { data } = await apiClient.instance.put<SuccessEnvelope<Role>>(
		`/admin/roles/${roleId}/default`,
	);
	return data.data;
}

export async function validatePolicy(
	policy: Policy,
	projectId?: string,
): Promise<ValidateResult> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<ValidateResult>
	>(`${helperBase(projectId)}/validate`, { policy });
	return data.data;
}

export async function simulatePolicy(
	input: SimulateInput,
	projectId?: string,
): Promise<SimulateResult> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<SimulateResult>
	>(`${helperBase(projectId)}/simulate`, input);
	return data.data;
}

/** Every action the server knows (built-in and plugin-declared), sorted. */
export async function getKnownActions(projectId?: string): Promise<string[]> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<string[]>>(
		`${helperBase(projectId)}/actions`,
	);
	return data.data;
}

export async function getAttributeSchema(
	projectId?: string,
): Promise<AttributeDef[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<AttributeDef[]>
	>(`${helperBase(projectId)}/attribute-schema`);
	return data.data;
}

// --- Assignment -------------------------------------------------------------

/** Replaces the set of platform roles a user holds. */
export async function replaceUserRoles(
	userId: string,
	roleIds: string[],
): Promise<Role[]> {
	const { data } = await apiClient.instance.put<SuccessEnvelope<Role[]>>(
		`/admin/users/${userId}/roles`,
		{ role_ids: roleIds },
	);
	return data.data;
}

/** Replaces the set of platform roles a global agent holds. */
export async function replaceAgentRoles(
	agentId: string,
	roleIds: string[],
): Promise<Role[]> {
	const { data } = await apiClient.instance.put<SuccessEnvelope<Role[]>>(
		`/admin/agents/${agentId}/roles`,
		{ role_ids: roleIds },
	);
	return data.data;
}

/** Replaces the set of roles a project member holds inside the project. */
export async function replaceMemberRoles(
	projectId: string,
	memberId: string,
	roleIds: string[],
): Promise<Role[]> {
	const { data } = await apiClient.instance.put<SuccessEnvelope<Role[]>>(
		`/projects/${projectId}/members/${memberId}/roles`,
		{ role_ids: roleIds },
	);
	return data.data;
}

// --- Query options ----------------------------------------------------------

export const platformRolesQueryOptions = queryOptions({
	queryKey: ["admin", "roles"],
	queryFn: () => listRoles(),
});

export const projectRolesQueryOptions = (projectId: string) =>
	queryOptions({
		queryKey: ["projects", projectId, "roles"],
		queryFn: () => listRoles(projectId),
	});

/** Every action the server knows; the JSON editor autocompletes from it. */
export const knownActionsQueryOptions = (projectId?: string) =>
	queryOptions({
		queryKey: ["roles", "actions", projectId ?? null],
		queryFn: () => getKnownActions(projectId),
		staleTime: 5 * 60 * 1000,
		retry: false,
	});

/** The condition attributes the server knows; drives "Limit to…" and the
 *  JSON editor's key suggestions. */
export const attributeSchemaQueryOptions = (projectId?: string) =>
	queryOptions({
		queryKey: ["roles", "attribute-schema", projectId ?? null],
		queryFn: () => getAttributeSchema(projectId),
		staleTime: 5 * 60 * 1000,
		retry: false,
	});
