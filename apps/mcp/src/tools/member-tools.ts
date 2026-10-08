import type { Tool } from "@modelcontextprotocol/sdk/types.js";
import { z } from "zod";
import type { PacaAPIExtendedClient } from "../api/index.js";
import { formatList } from "../utils/index.js";

const ListProjectMembersSchema = z.object({
	projectId: z.string(),
});

const AddProjectMemberSchema = z.object({
	projectId: z.string(),
	userId: z.string(),
	roleIds: z.array(z.string()).min(1),
});

const GetMyProjectPermissionsSchema = z.object({
	projectId: z.string(),
});

const UpdateProjectMemberRoleSchema = z.object({
	projectId: z.string(),
	userId: z.string(),
	roleIds: z.array(z.string()).min(1),
});

const RemoveProjectMemberSchema = z.object({
	projectId: z.string(),
	userId: z.string(),
});

const ListProjectRolesSchema = z.object({
	projectId: z.string(),
});

const POLICY_VERSION = "2026-10-01";

const PolicySchema = z.object({
	version: z.string().optional(),
	statements: z.array(
		z.object({
			sid: z.string().optional(),
			effect: z.enum(["Allow", "Deny"]),
			actions: z.array(z.string()),
			resources: z.array(z.string()),
			conditions: z.record(z.record(z.unknown())).optional(),
		}),
	),
});

const CreateProjectRoleSchema = z.object({
	projectId: z.string(),
	name: z.string(),
	description: z.string().optional(),
	policy: PolicySchema,
});

const UpdateProjectRoleSchema = z.object({
	projectId: z.string(),
	roleId: z.string(),
	name: z.string().optional(),
	description: z.string().optional(),
	policy: PolicySchema.optional(),
});

const DeleteProjectRoleSchema = z.object({
	projectId: z.string(),
	roleId: z.string(),
});

/**
 * Returns all project member-related MCP tools.
 */
export function getProjectMemberTools(): Tool[] {
	return [
		{
			name: "list_project_members",
			description: "List all members of a project",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_projects to get the project ID. Do NOT use the project name.",
					},
				},
				required: ["projectId"],
			},
		},
		{
			name: "add_project_member",
			description: "Add a member to a project",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_projects to get the project ID. Do NOT use the project name.",
					},
					userId: {
						type: "string",
						description:
							"The technical UUID of the user to add (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_project_members to see existing member user IDs.",
					},
					roleIds: {
						type: "array",
						items: { type: "string" },
						description:
							"The technical UUIDs of the roles the member holds in the project. Use list_project_roles to get the role IDs.",
					},
				},
				required: ["projectId", "userId", "roleIds"],
			},
		},
		{
			name: "get_my_project_permissions",
			description:
				"Get the IAM actions (e.g. 'tasks:write') the current user may perform in a project",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_projects to get the project ID. Do NOT use the project name.",
					},
				},
				required: ["projectId"],
			},
		},
		{
			name: "update_project_member_role",
			description:
				"Replace the set of roles a project member holds in the project",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_projects to get the project ID. Do NOT use the project name.",
					},
					userId: {
						type: "string",
						description:
							"The technical UUID of the user (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_project_members to get user IDs.",
					},
					roleIds: {
						type: "array",
						items: { type: "string" },
						description:
							"The technical UUIDs of the roles the member should hold (replaces the current set). Use list_project_roles to get the role IDs.",
					},
				},
				required: ["projectId", "userId", "roleIds"],
			},
		},
		{
			name: "remove_project_member",
			description: "Remove a member from a project",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_projects to get the project ID. Do NOT use the project name.",
					},
					userId: {
						type: "string",
						description:
							"The technical UUID of the user to remove (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_project_members to get user IDs.",
					},
				},
				required: ["projectId", "userId"],
			},
		},
	];
}

/**
 * Returns all project role-related MCP tools.
 */
export function getProjectRoleTools(): Tool[] {
	return [
		{
			name: "list_project_roles",
			description: "List all roles in a project",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_projects to get the project ID. Do NOT use the project name.",
					},
				},
				required: ["projectId"],
			},
		},
		{
			name: "create_project_role",
			description: "Create a new project role",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_projects to get the project ID. Do NOT use the project name.",
					},
					name: {
						type: "string",
						description: "The name of the role",
					},
					description: {
						type: "string",
						description: "The description of the role",
					},
					policy: {
						type: "object",
						description:
							'IAM policy document: {"version":"2026-10-01","statements":[{"effect":"Allow"|"Deny","actions":["tasks:read"],"resources":["project/<projectId>/*"],"conditions":{...}}]}. Actions are \'domain:verb\' (wildcards \'*\' and \'domain:*\'); a Deny always wins. Use get_my_project_permissions to see action names.',
						properties: {
							version: { type: "string" },
							statements: {
								type: "array",
								items: {
									type: "object",
									properties: {
										sid: { type: "string" },
										effect: { type: "string", enum: ["Allow", "Deny"] },
										actions: { type: "array", items: { type: "string" } },
										resources: { type: "array", items: { type: "string" } },
										conditions: { type: "object" },
									},
									required: ["effect", "actions", "resources"],
								},
							},
						},
						required: ["statements"],
					},
				},
				required: ["projectId", "name", "policy"],
			},
		},
		{
			name: "update_project_role",
			description: "Update an existing project role",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_projects to get the project ID. Do NOT use the project name.",
					},
					roleId: {
						type: "string",
						description:
							"The technical UUID of the role (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_project_roles to get the role ID.",
					},
					name: {
						type: "string",
						description: "The new name of the role",
					},
					description: {
						type: "string",
						description: "The new description of the role",
					},
					policy: {
						type: "object",
						description:
							'IAM policy document: {"version":"2026-10-01","statements":[{"effect":"Allow"|"Deny","actions":["tasks:read"],"resources":["project/<projectId>/*"],"conditions":{...}}]}. Actions are \'domain:verb\' (wildcards \'*\' and \'domain:*\'); a Deny always wins. Use get_my_project_permissions to see action names.',
						properties: {
							version: { type: "string" },
							statements: {
								type: "array",
								items: {
									type: "object",
									properties: {
										sid: { type: "string" },
										effect: { type: "string", enum: ["Allow", "Deny"] },
										actions: { type: "array", items: { type: "string" } },
										resources: { type: "array", items: { type: "string" } },
										conditions: { type: "object" },
									},
									required: ["effect", "actions", "resources"],
								},
							},
						},
						required: ["statements"],
					},
				},
				required: ["projectId", "roleId"],
			},
		},
		{
			name: "delete_project_role",
			description: "Delete a project role",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_projects to get the project ID. Do NOT use the project name.",
					},
					roleId: {
						type: "string",
						description:
							"The technical UUID of the role (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_project_roles to get the role ID.",
					},
				},
				required: ["projectId", "roleId"],
			},
		},
	];
}

function formatProjectMember(member: any): string {
	// Member ID (member.id) is what task/automation assignment tools expect
	// (assigneeId, trigger_ai_agent's member_id, ...) — it is NOT the same
	// value as User ID, and only agent members even have a User ID. Every
	// assignment-taking tool's description points back here for "the member
	// ID", so this field must actually be present in the output.
	return `Member: ${member.username} (${member.full_name})
Member ID: ${member.id}
User ID: ${member.user_id}
Roles: ${
		(member.roles ?? []).map((r: any) => `${r.name} (${r.id})`).join(", ") ||
		"None"
	}
Joined: ${member.joined_at || "N/A"}`;
}

function formatProjectRole(role: any): string {
	return `Role: ${role.name}
ID: ${role.id}
Description: ${role.description || "None"}
System: ${role.is_system}
Policy: ${JSON.stringify(role.policy, null, 2)}
Created: ${role.created_at}`;
}

/**
 * Handles project member and role tool calls.
 */
export async function handleProjectMemberTool(
	toolName: string,
	args: any,
	client: PacaAPIExtendedClient,
): Promise<any> {
	switch (toolName) {
		case "list_project_members": {
			const { projectId } = ListProjectMembersSchema.parse(args);
			const members = await client.listProjectMembers(projectId);
			const formatted = formatList(members, formatProjectMember);
			return {
				content: [
					{
						type: "text",
						text: `Project Members:\n\n${formatted}`,
					},
				],
			};
		}

		case "add_project_member": {
			const { projectId, userId, roleIds } = AddProjectMemberSchema.parse(args);
			const member = await client.addProjectMember(projectId, {
				user_id: userId,
				role_ids: roleIds,
			});
			return {
				content: [
					{
						type: "text",
						text: `Member added successfully:\n\n${formatProjectMember(member)}`,
					},
				],
			};
		}

		case "get_my_project_permissions": {
			const { projectId } = GetMyProjectPermissionsSchema.parse(args);
			const actions = await client.getMyProjectPermissions(projectId);
			return {
				content: [
					{
						type: "text",
						text: `My Actions:\n\n${JSON.stringify(actions, null, 2)}`,
					},
				],
			};
		}

		case "update_project_member_role": {
			const { projectId, userId, roleIds } =
				UpdateProjectMemberRoleSchema.parse(args);
			const roles = await client.updateProjectMemberRole(projectId, userId, {
				role_ids: roleIds,
			});
			return {
				content: [
					{
						type: "text",
						text: `Member roles updated successfully:\n\n${formatList(roles, formatProjectRole)}`,
					},
				],
			};
		}

		case "remove_project_member": {
			const { projectId, userId } = RemoveProjectMemberSchema.parse(args);
			await client.removeProjectMember(projectId, userId);
			return {
				content: [
					{
						type: "text",
						text: `Member ${userId} removed successfully`,
					},
				],
			};
		}

		case "list_project_roles": {
			const { projectId } = ListProjectRolesSchema.parse(args);
			const roles = await client.listProjectRoles(projectId);
			const formatted = formatList(roles, formatProjectRole);
			return {
				content: [
					{
						type: "text",
						text: `Project Roles:\n\n${formatted}`,
					},
				],
			};
		}

		case "create_project_role": {
			const { projectId, name, description, policy } =
				CreateProjectRoleSchema.parse(args);
			const role = await client.createProjectRole(projectId, {
				name,
				description,
				policy: { version: POLICY_VERSION, ...policy },
			});
			return {
				content: [
					{
						type: "text",
						text: `Role created successfully:\n\n${formatProjectRole(role)}`,
					},
				],
			};
		}

		case "update_project_role": {
			const { projectId, roleId, name, description, policy } =
				UpdateProjectRoleSchema.parse(args);
			// The API replaces the whole role, so omitted fields keep their
			// current value.
			const current = await client.getProjectRole(projectId, roleId);
			const role = await client.updateProjectRole(projectId, roleId, {
				name: name ?? current.name,
				description: description ?? current.description ?? "",
				policy: policy
					? { version: POLICY_VERSION, ...policy }
					: current.policy,
			});
			return {
				content: [
					{
						type: "text",
						text: `Role updated successfully:\n\n${formatProjectRole(role)}`,
					},
				],
			};
		}

		case "delete_project_role": {
			const { projectId, roleId } = DeleteProjectRoleSchema.parse(args);
			await client.deleteProjectRole(projectId, roleId);
			return {
				content: [
					{
						type: "text",
						text: `Role ${roleId} deleted successfully`,
					},
				],
			};
		}

		default:
			throw new Error(`Unknown project member/role tool: ${toolName}`);
	}
}
