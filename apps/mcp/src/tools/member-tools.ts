import type { Tool } from "@modelcontextprotocol/sdk/types.js";
import * as z from "zod";
import type { PacaAPIExtendedClient } from "../api/index.js";
import { formatList } from "../utils/index.js";

const ListProjectMembersSchema = z.object({
	projectId: z.string(),
});

const AddProjectMemberSchema = z.object({
	projectId: z.string(),
	userId: z.string(),
	roleId: z.string(),
});

const GetMyProjectPermissionsSchema = z.object({
	projectId: z.string(),
});

const UpdateProjectMemberRoleSchema = z.object({
	projectId: z.string(),
	userId: z.string(),
	roleId: z.string(),
});

const RemoveProjectMemberSchema = z.object({
	projectId: z.string(),
	userId: z.string(),
});

const ListProjectRolesSchema = z.object({
	projectId: z.string(),
});

const CreateProjectRoleSchema = z.object({
	projectId: z.string(),
	name: z.string(),
	description: z.string().optional(),
	permissions: z.array(z.string()),
});

const UpdateProjectRoleSchema = z.object({
	projectId: z.string(),
	roleId: z.string(),
	name: z.string().optional(),
	description: z.string().optional(),
	permissions: z.array(z.string()).optional(),
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
					roleId: {
						type: "string",
						description:
							"The technical UUID of the role to assign (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_project_roles to get the role ID.",
					},
				},
				required: ["projectId", "userId", "roleId"],
			},
		},
		{
			name: "get_my_project_permissions",
			description: "Get the current user's permissions in a project",
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
			description: "Update a project member's role",
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
					roleId: {
						type: "string",
						description:
							"The technical UUID of the new role (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_project_roles to get the role ID.",
					},
				},
				required: ["projectId", "userId", "roleId"],
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
					permissions: {
						type: "array",
						items: { type: "string" },
						description: "Array of permission strings",
					},
				},
				required: ["projectId", "name", "permissions"],
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
					permissions: {
						type: "array",
						items: { type: "string" },
						description: "Array of permission strings",
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
Role: ${member.role_name}
Role ID: ${member.project_role_id}
Joined: ${member.joined_at || "N/A"}`;
}

function formatProjectRole(role: any): string {
	return `Role: ${role.role_name}
ID: ${role.id}
Description: ${role.description || "None"}
System: ${role.is_system}
Permissions: ${JSON.stringify(role.permissions, null, 2)}
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
			const { projectId, userId, roleId } = AddProjectMemberSchema.parse(args);
			const member = await client.addProjectMember(projectId, {
				user_id: userId,
				project_role_id: roleId,
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
			const permissions = await client.getMyProjectPermissions(projectId);
			return {
				content: [
					{
						type: "text",
						text: `My Permissions:\n\n${JSON.stringify(permissions, null, 2)}`,
					},
				],
			};
		}

		case "update_project_member_role": {
			const { projectId, userId, roleId } =
				UpdateProjectMemberRoleSchema.parse(args);
			const member = await client.updateProjectMemberRole(projectId, userId, {
				project_role_id: roleId,
			});
			return {
				content: [
					{
						type: "text",
						text: `Member role updated successfully:\n\n${formatProjectMember(member)}`,
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
			const { projectId, name, description, permissions } =
				CreateProjectRoleSchema.parse(args);
			const permissionsRecord: Record<string, unknown> = {};
			for (const perm of permissions) {
				permissionsRecord[perm] = true;
			}
			const role = await client.createProjectRole(projectId, {
				role_name: name,
				description,
				permissions: permissionsRecord,
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
			const { projectId, roleId, name, description, permissions } =
				UpdateProjectRoleSchema.parse(args);
			const permissionsRecord: Record<string, unknown> | undefined = permissions
				? Object.fromEntries(permissions.map((perm) => [perm, true]))
				: undefined;
			const role = await client.updateProjectRole(projectId, roleId, {
				role_name: name,
				description,
				permissions: permissionsRecord,
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
