import type { Tool } from "@modelcontextprotocol/sdk/types.js";
import * as z from "zod";
import type { PacaAPIClient } from "../api/index.js";
import { formatList, formatProject } from "../utils/index.js";

const GetProjectSchema = z.object({
	projectId: z.string(),
});

const CreateProjectSchema = z.object({
	name: z.string(),
	description: z.string().optional(),
});

const UpdateProjectSchema = z.object({
	projectId: z.string(),
	name: z.string().optional(),
	description: z.string().optional(),
});

const DeleteProjectSchema = z.object({
	projectId: z.string(),
});

/**
 * Returns all project-related MCP tools.
 * @returns Array of project tools
 */
export function getProjectTools(): Tool[] {
	return [
		{
			name: "list_projects",
			description: "List all projects accessible to the authenticated user",
			inputSchema: {
				type: "object",
				properties: {},
			},
		},
		{
			name: "get_project",
			description: "Get details of a specific project",
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
			name: "create_project",
			description: "Create a new project",
			inputSchema: {
				type: "object",
				properties: {
					name: {
						type: "string",
						description: "The name of the project",
					},
					description: {
						type: "string",
						description: "The description of the project",
					},
				},
				required: ["name"],
			},
		},
		{
			name: "update_project",
			description: "Update an existing project",
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
						description: "The new name of the project",
					},
					description: {
						type: "string",
						description: "The new description of the project",
					},
				},
				required: ["projectId"],
			},
		},
		{
			name: "delete_project",
			description: "Delete a project",
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
	];
}

/**
 * Handles project-related tool calls.
 * @param toolName - Name of the tool being called
 * @param args - Tool arguments
 * @param client - Paca API client instance
 * @returns Tool response
 */
export async function handleProjectTool(
	toolName: string,
	args: any,
	client: PacaAPIClient,
): Promise<any> {
	switch (toolName) {
		case "list_projects": {
			const projects = await client.listProjects();
			const formatted = formatList(projects, formatProject);
			return {
				content: [
					{
						type: "text",
						text: `Projects:\n\n${formatted}`,
					},
				],
			};
		}

		case "get_project": {
			const { projectId } = GetProjectSchema.parse(args);
			const project = await client.getProject(projectId);
			return {
				content: [
					{
						type: "text",
						text: formatProject(project),
					},
				],
			};
		}

		case "create_project": {
			const { name, description } = CreateProjectSchema.parse(args);
			const project = await client.createProject({ name, description });
			return {
				content: [
					{
						type: "text",
						text: `Project created successfully:\n\n${formatProject(project)}`,
					},
				],
			};
		}

		case "update_project": {
			const { projectId, name, description } = UpdateProjectSchema.parse(args);
			const project = await client.updateProject(projectId, {
				name,
				description,
			});
			return {
				content: [
					{
						type: "text",
						text: `Project updated successfully:\n\n${formatProject(project)}`,
					},
				],
			};
		}

		case "delete_project": {
			const { projectId } = DeleteProjectSchema.parse(args);
			await client.deleteProject(projectId);
			return {
				content: [
					{
						type: "text",
						text: `Project ${projectId} deleted successfully`,
					},
				],
			};
		}

		default:
			throw new Error(`Unknown project tool: ${toolName}`);
	}
}
