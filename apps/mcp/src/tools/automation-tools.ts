import type { Tool } from "@modelcontextprotocol/sdk/types.js";
import { z } from "zod";
import type { PacaAPIAutomationClient } from "../api/index.js";
import type { Automation, AutomationGraph } from "../types/index.js";
import { formatToolError } from "../utils/index.js";

const PROJECT_ID_DESC =
	"The technical UUID of the project (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_projects to get the project ID. Do NOT use the project name.";

const AUTOMATION_ID_DESC =
	"The technical UUID of the automation. Use get_automation (without automationId) to list automations and get their IDs.";

const STATUS_ID_DESC =
	"The technical UUID of a task status in this project. Use list_task_statuses to get status IDs.";

const MEMBER_ID_DESC =
	"The technical UUID of the project member (human or agent) to assign. Use list_project_members to get member IDs.";

const AutomationStatusEnum = z.enum(["draft", "active", "archived"]);
const NodeKindEnum = z.enum(["trigger", "condition", "action"]);

const TRIGGER_TYPES = [
	"status_changed",
	"task_created",
	"assignee_changed",
	"priority_changed",
	"tag_added",
	"due_date_reached",
	"predecessor_done",
	"cron",
	"api_trigger",
];
const ACTION_TYPES = [
	"assign",
	"set_status",
	"set_priority",
	"add_tag",
	"set_custom_field",
	"trigger_ai_agent",
	"call_api",
];

// .strict() on every item schema below is deliberate: agents occasionally
// invent a plausible-but-wrong field name. Without .strict(), Zod silently
// drops the unrecognized key; with .strict(), the caller also gets an
// explicit "Unrecognized key(s)" issue naming the mistake.

const NodeSetInputSchema = z
	.object({
		id: z
			.string()
			.optional()
			.describe(
				"Omit to CREATE a new node (kind/type are then required). Pass an existing node's id to UPDATE its config/position instead (kind/type are ignored for an update — a node's kind/type can't change after creation).",
			),
		ref: z
			.string()
			.optional()
			.describe(
				"Only meaningful when creating a new node (id omitted): a label YOU choose for this node, so edges.add in this SAME call can reference it by ref before it has a real id yet.",
			),
		kind: NodeKindEnum.optional(),
		type: z
			.string()
			.optional()
			.describe(
				`Built-in trigger types: ${TRIGGER_TYPES.join(", ")}. Built-in action types: ${ACTION_TYPES.join(", ")}. A Condition node's type is always "condition". A plugin may also contribute its own reverse-DNS-namespaced type (e.g. "com.acme.github.pr_status") — check the project's installed plugins if unsure what's available.`,
			),
		config: z
			.record(z.string(), z.unknown())
			.optional()
			.describe(
				`Type-specific config. Trigger: {status_id?} for status_changed (omit = any status; ${STATUS_ID_DESC}), {tag?} for tag_added (omit = any tag), {due_date_offset_minutes?} for due_date_reached, {watched_task_ids, target_task_id?} for predecessor_done (fires once every watched task is done; watched_task_ids is required; target_task_id is the DIFFERENT task the graph then runs against, re-evaluated using its own current state — optional, see the note below), {cron_expression, target_task_id?} for cron (cron_expression is a standard 5-field cron expression evaluated in UTC, e.g. "*/15 * * * *" — required; target_task_id is the fixed task the graph runs against on every fire — optional, see the note below), {target_task_id?} for api_trigger (fires when an external caller POSTs to this node's webhook URL with a valid secret token; target_task_id is the fixed task the graph runs against on every call — optional, see the note below; the webhook secret itself can only be generated from the web UI, not via this tool). Note on target_task_id: leave it unset if the trigger should only reach call_api and/or trigger_ai_agent actions downstream — those are the only two action types that don't need a task (call_api operates on static config; trigger_ai_agent fires a standalone message at the agent instead of assigning a task when there's none), so a task-less trigger may only connect to them (edge/node-update calls that would connect it to a condition or any other action are rejected). Action: {member_id} for assign/trigger_ai_agent (${MEMBER_ID_DESC} trigger_ai_agent also takes {message} — a free-text instruction for the agent; message is required when the upstream trigger has no target_task_id, since there's no task for the agent to fall back to), {status_id} for set_status, {importance} for set_priority, {tag} for add_tag, {field_key, value} for set_custom_field, {method, url, headers?, body?} for call_api (method is one of GET/POST/PUT/PATCH/DELETE; url must be http(s); headers is a string→string map; body is sent verbatim — both method and url are required). Every action except call_api also accepts {target?} (see the target note below) to retarget itself onto a task other than the one the graph is walking — e.g. set_status with target {kind: "children"} sets every child task's status instead of the walked task's own. Condition: {branches: [{handle, label?, tree}]} where tree is a single leaf comparison {field, operator, value?, target?, match_mode?}; fields are status_id/task_type_id/importance/assignee_ids/tags/custom_field (custom_field also needs field_key), operators are equals/not_equals/contains/greater_than/less_than/is_empty/is_not_empty; target retargets the leaf the same way an action's does (see below); match_mode ("any", the default, or "all") only matters when target resolves to more than one task — "any" passes if at least one resolved task satisfies the leaf, "all" requires every one to. Target shape (shared by every action's and condition leaf's {target}): {kind, other_task_id?} — kind is one of "self" (default; the task the graph is walking, i.e. omit target entirely for today's behavior), "parent", "children", "blocks", "is_blocked_by", "relates_to", "duplicates", "is_duplicated_by" (the same five relation directions as the task's Linked Tasks section, minus self), or "other" (an explicit, otherwise-unrelated task — requires other_task_id). "children" and the four link kinds can resolve to more than one task (e.g. several children, or several blocking tasks); a retargeted action then fans out, applying once per resolved task. A trigger with no target_task_id has no task to retarget FROM, so its downstream nodes may only use target "self"/omitted with call_api or trigger_ai_agent — anything else needs a task to resolve relative to.`,
			),
		posX: z.number().optional(),
		posY: z.number().optional(),
	})
	.strict();

const EdgeAddInputSchema = z
	.object({
		source: z
			.string()
			.describe(
				"Either an existing node's id, or a ref you assigned to a node being created in the SAME call's nodes.set list.",
			),
		sourceHandle: z
			.string()
			.optional()
			.describe(
				'Required when source is a Condition node: must match one of that node\'s declared branch handles, or the literal "else" for the fallback path. Must be omitted when source is a Trigger or Action node (they have exactly one outgoing path).',
			),
		target: z
			.string()
			.describe(
				"Either an existing node's id, or a ref you assigned to a node being created in the SAME call's nodes.set list. Cannot be a Trigger node (nothing can point into a trigger).",
			),
	})
	.strict();

const GetAutomationSchema = z.object({
	projectId: z.string(),
	automationId: z.string().optional(),
	status: AutomationStatusEnum.optional(),
});

const CreateAutomationSchema = z.object({
	projectId: z.string(),
	name: z.string().trim().min(1),
	description: z.string().optional(),
	nodes: z.array(NodeSetInputSchema).optional(),
	edges: z.array(EdgeAddInputSchema).optional(),
	activate: z.boolean().optional(),
});

const UpdateAutomationSchema = z.object({
	projectId: z.string(),
	automationId: z.string(),
	name: z.string().trim().min(1).optional(),
	description: z.string().optional(),
	status: AutomationStatusEnum.optional(),
	nodes: z
		.object({
			set: z.array(NodeSetInputSchema).optional(),
			remove: z.array(z.string()).optional(),
		})
		.optional(),
	edges: z
		.object({
			add: z.array(EdgeAddInputSchema).optional(),
			remove: z.array(z.string()).optional(),
		})
		.optional(),
});

const DeleteAutomationSchema = z.object({
	projectId: z.string(),
	automationId: z.string(),
});

/**
 * Returns all automation MCP tools.
 */
export function getAutomationTools(): Tool[] {
	return [
		{
			name: "get_automation",
			description:
				"Get information about automations in a project. Pass automationId to fetch one automation's full graph: every node (Trigger/Condition/Action, with its type and config) and every edge (which node feeds which, and — for a Condition source — which branch). Omit automationId to list all automations in the project instead (optionally filtered by status) — e.g. to find an automation's ID before fetching its graph.\n\n" +
				"An automation is a graph of three node kinds:\n" +
				"- Trigger: starts a run when a matching event occurs (a task's status/assignee/priority changes, a task is created, a tag is added, a due date is reached, or all of a set of watched tasks reach a 'done' status). A graph can have more than one trigger — any of them firing starts the same downstream chain.\n" +
				"- Condition: branches. Evaluates each declared branch's leaf-tree in order against the task and follows the first one that matches, or the 'else' edge if none do.\n" +
				"- Action: mutates the task (assign, set status, set priority, add a tag, set a custom field, or hand off to an AI agent with a custom instruction), then continues along its own edge.\n\n" +
				"Every automation whose trigger matches an event runs independently — there's no cross-automation priority; if two automations could both act on the same event, put that decision inside one automation's Condition branches instead of splitting it across two automations.\n\n" +
				"Call this before editing an automation you didn't just create, so you know its current graph and node IDs — update_automation addresses existing nodes/edges by the IDs this tool returns.",
			inputSchema: {
				type: "object",
				properties: {
					projectId: { type: "string", description: PROJECT_ID_DESC },
					automationId: {
						type: "string",
						description: `Optional. ${AUTOMATION_ID_DESC} Omit to list all automations in the project instead of fetching one's graph.`,
					},
					status: {
						type: "string",
						enum: ["draft", "active", "archived"],
						description:
							"Optional filter by lifecycle status. Only used when automationId is omitted.",
					},
				},
				required: ["projectId"],
			},
		},
		{
			name: "create_automation",
			description:
				"Create a new automation, optionally building out its whole graph in one call: nodes (triggers/conditions/actions) and edges between them. Starts in 'draft' state — the engine ignores it until activated (pass activate: true once the graph is complete, or activate later via update_automation). Activating requires at least one Trigger node and at least one Action node, and the graph must remain acyclic.\n\n" +
				"Give each new node a `ref` (a label you choose) so edges in this SAME call can wire them together before they have real IDs — e.g. nodes: [{ref:'t1', kind:'trigger', type:'status_changed', config:{status_id:'...'}}, {ref:'a1', kind:'action', type:'assign', config:{member_id:'...'}}], edges: [{source:'t1', target:'a1'}]. See get_automation's tool description for the node-kind vocabulary and the config shape per type.\n\n" +
				"Each entry in nodes/edges is applied independently — one bad entry (e.g. an edge that would create a cycle) doesn't block the others; check the response for any 'failed' items. activate is only attempted if every requested item above succeeded.",
			inputSchema: {
				type: "object",
				properties: {
					projectId: { type: "string", description: PROJECT_ID_DESC },
					name: { type: "string", description: "Automation name." },
					description: {
						type: "string",
						description: "Optional description.",
					},
					nodes: {
						type: "array",
						description:
							"Nodes to create. Each requires kind and type; config/posX/posY are optional. Assign a ref to reference this node from edges in the same call.",
					},
					edges: {
						type: "array",
						description:
							"Links between the nodes above (by ref) — see EdgeAddInputSchema fields.",
					},
					activate: {
						type: "boolean",
						description:
							"If true, activate the automation immediately after building the graph above — only attempted if every item succeeded.",
					},
				},
				required: ["projectId", "name"],
			},
		},
		{
			name: "update_automation",
			description:
				"Update an automation: rename/describe it, change its lifecycle status, and/or edit its graph (nodes, edges) — all in one call. Use get_automation first to see the automation's current graph and node IDs.\n\n" +
				"Graph edits work whether the automation is 'draft' or 'active'. Only 'archived' locks the graph (delete or build a new one instead). Do not call this tool once per node; pass every node you're touching as one nodes.set array in a single call.\n\n" +
				"Pass status when you want a different lifecycle state: 'draft' to pause the engine while keeping the automation editable; 'active' to activate a currently-draft automation (requires at least one Trigger node and at least one Action node, and an acyclic graph); 'archived' to archive a currently-active one (archived automations can never be reverted — delete or build a new one instead).\n\n" +
				"nodes.set: pass id to update an existing node's config/position; omit id (and supply kind+type) to create a new node, optionally with a ref other edges.add entries in this same call can reference. nodes.remove deletes nodes by id (and their edges). edges.add creates new edges (source/target may be an id or a ref from nodes.set in this call); edges.remove deletes edges by id. Every item in every list is attempted independently — one item failing doesn't block its siblings.",
			inputSchema: {
				type: "object",
				properties: {
					projectId: { type: "string", description: PROJECT_ID_DESC },
					automationId: { type: "string", description: AUTOMATION_ID_DESC },
					name: { type: "string", description: "New name." },
					description: { type: "string", description: "New description." },
					status: {
						type: "string",
						enum: ["draft", "active", "archived"],
						description:
							"New lifecycle status. See the tool description above for preconditions.",
					},
					nodes: {
						type: "object",
						description: "Add/update or remove nodes.",
						properties: {
							set: { type: "array", description: "Nodes to create or update." },
							remove: {
								type: "array",
								description: "Node IDs to remove.",
								items: { type: "string" },
							},
						},
					},
					edges: {
						type: "object",
						description: "Add or remove edges.",
						properties: {
							add: { type: "array", description: "Edges to create." },
							remove: {
								type: "array",
								description: "Edge IDs to remove.",
								items: { type: "string" },
							},
						},
					},
				},
				required: ["projectId", "automationId"],
			},
		},
		{
			name: "delete_automation",
			description: "Permanently delete an automation.",
			inputSchema: {
				type: "object",
				properties: {
					projectId: { type: "string", description: PROJECT_ID_DESC },
					automationId: { type: "string", description: AUTOMATION_ID_DESC },
				},
				required: ["projectId", "automationId"],
			},
		},
	];
}

function formatAutomation(automation: Automation): string {
	return (
		`- **${automation.name}** (status: ${automation.status})\n` +
		`  ID: ${automation.id}\n` +
		`  Description: ${automation.description || "(none)"}`
	);
}

function formatAutomationGraph(graph: AutomationGraph): string {
	const a = graph.automation;
	const nodes = graph.nodes || [];
	const edges = graph.edges || [];
	const lines = [
		`Automation '${a.name}' (status: ${a.status}, id: ${a.id})`,
		`${nodes.length} node(s), ${edges.length} edge(s):`,
		"",
	];
	for (const n of nodes) {
		lines.push(
			`- node ${n.id}: kind=${n.kind} type=${n.type} config=${JSON.stringify(n.config)} position=(${n.pos_x}, ${n.pos_y})`,
		);
	}
	for (const e of edges) {
		lines.push(
			`- edge ${e.id}: ${e.source_node_id}${e.source_handle ? ` [${e.source_handle}]` : ""} -> ${e.target_node_id}`,
		);
	}
	return lines.join("\n");
}

/** Resolves a node-set item's ref/id pairing after nodes.set has been applied. */
interface RefResolution {
	refToId: Map<string, string>;
}

async function applyNodeSet(
	client: PacaAPIAutomationClient,
	projectId: string,
	automationId: string,
	items: z.infer<typeof NodeSetInputSchema>[] | undefined,
): Promise<{ refToId: Map<string, string>; failures: string[] }> {
	const refToId = new Map<string, string>();
	const failures: string[] = [];
	for (const item of items ?? []) {
		try {
			if (item.id) {
				await client.updateAutomationNode(projectId, automationId, item.id, {
					config: item.config,
					pos_x: item.posX,
					pos_y: item.posY,
				});
				if (item.ref) refToId.set(item.ref, item.id);
			} else {
				if (!item.kind || !item.type) {
					failures.push(
						`node ${item.ref ?? "(no ref)"}: kind and type are required when creating a new node`,
					);
					continue;
				}
				const created = await client.addAutomationNode(
					projectId,
					automationId,
					{
						kind: item.kind,
						type: item.type,
						config: item.config ?? {},
						pos_x: item.posX ?? 0,
						pos_y: item.posY ?? 0,
					},
				);
				if (item.ref) refToId.set(item.ref, created.id);
			}
		} catch (err) {
			failures.push(
				`node ${item.ref ?? item.id ?? "(unknown)"}: ${err instanceof Error ? err.message : String(err)}`,
			);
		}
	}
	return { refToId, failures };
}

async function applyNodeRemovals(
	client: PacaAPIAutomationClient,
	projectId: string,
	automationId: string,
	ids: string[] | undefined,
): Promise<string[]> {
	const failures: string[] = [];
	for (const id of ids ?? []) {
		try {
			await client.removeAutomationNode(projectId, automationId, id);
		} catch (err) {
			failures.push(
				`node ${id}: ${err instanceof Error ? err.message : String(err)}`,
			);
		}
	}
	return failures;
}

async function applyEdgeAdds(
	client: PacaAPIAutomationClient,
	projectId: string,
	automationId: string,
	items: z.infer<typeof EdgeAddInputSchema>[] | undefined,
	resolution: RefResolution,
): Promise<string[]> {
	const failures: string[] = [];
	for (const item of items ?? []) {
		const sourceId = resolution.refToId.get(item.source) ?? item.source;
		const targetId = resolution.refToId.get(item.target) ?? item.target;
		try {
			await client.addAutomationEdge(projectId, automationId, {
				source_node_id: sourceId,
				source_handle: item.sourceHandle,
				target_node_id: targetId,
			});
		} catch (err) {
			failures.push(
				`edge ${item.source}->${item.target}: ${err instanceof Error ? err.message : String(err)}`,
			);
		}
	}
	return failures;
}

async function applyEdgeRemovals(
	client: PacaAPIAutomationClient,
	projectId: string,
	automationId: string,
	ids: string[] | undefined,
): Promise<string[]> {
	const failures: string[] = [];
	for (const id of ids ?? []) {
		try {
			await client.removeAutomationEdge(projectId, automationId, id);
		} catch (err) {
			failures.push(
				`edge ${id}: ${err instanceof Error ? err.message : String(err)}`,
			);
		}
	}
	return failures;
}

/**
 * Handles automation MCP tool calls.
 */
export async function handleAutomationTool(
	name: string,
	args: unknown,
	client: PacaAPIAutomationClient,
): Promise<{
	content: Array<{ type: "text"; text: string }>;
	isError?: boolean;
}> {
	try {
		switch (name) {
			case "get_automation": {
				const input = GetAutomationSchema.parse(args);
				if (input.automationId) {
					const graph = await client.getAutomation(
						input.projectId,
						input.automationId,
					);
					return {
						content: [{ type: "text", text: formatAutomationGraph(graph) }],
					};
				}
				const automations = await client.listAutomations(
					input.projectId,
					input.status,
				);
				if (automations.length === 0) {
					return {
						content: [
							{ type: "text", text: "No automations found in this project." },
						],
					};
				}
				return {
					content: [
						{
							type: "text",
							text: automations.map(formatAutomation).join("\n\n"),
						},
					],
				};
			}

			case "create_automation": {
				const input = CreateAutomationSchema.parse(args);
				const automation = await client.createAutomation(input.projectId, {
					name: input.name,
					description: input.description,
				});

				const { refToId, failures: nodeFailures } = await applyNodeSet(
					client,
					input.projectId,
					automation.id,
					input.nodes,
				);
				const edgeFailures = await applyEdgeAdds(
					client,
					input.projectId,
					automation.id,
					input.edges,
					{ refToId },
				);

				const allFailures = [...nodeFailures, ...edgeFailures];
				let activated = false;
				if (input.activate && allFailures.length === 0) {
					await client.activateAutomation(input.projectId, automation.id);
					activated = true;
				}

				const graph = await client.getAutomation(
					input.projectId,
					automation.id,
				);
				const lines = [formatAutomationGraph(graph)];
				if (activated) lines.push("\nActivated.");
				if (allFailures.length > 0) {
					lines.push(
						`\nSome items failed:\n${allFailures.map((f) => `- ${f}`).join("\n")}`,
					);
				}
				return { content: [{ type: "text", text: lines.join("\n") }] };
			}

			case "update_automation": {
				const input = UpdateAutomationSchema.parse(args);

				if (
					input.name !== undefined ||
					input.description !== undefined ||
					(input.status !== undefined && input.status === "draft")
				) {
					await client.updateAutomation(input.projectId, input.automationId, {
						name: input.name,
						description: input.description,
					});
				}
				if (input.status === "draft") {
					await client.revertAutomationToDraft(
						input.projectId,
						input.automationId,
					);
				}

				const { refToId, failures: nodeSetFailures } = await applyNodeSet(
					client,
					input.projectId,
					input.automationId,
					input.nodes?.set,
				);
				const nodeRemoveFailures = await applyNodeRemovals(
					client,
					input.projectId,
					input.automationId,
					input.nodes?.remove,
				);
				const edgeAddFailures = await applyEdgeAdds(
					client,
					input.projectId,
					input.automationId,
					input.edges?.add,
					{ refToId },
				);
				const edgeRemoveFailures = await applyEdgeRemovals(
					client,
					input.projectId,
					input.automationId,
					input.edges?.remove,
				);

				if (input.status === "active") {
					await client.activateAutomation(input.projectId, input.automationId);
				} else if (input.status === "archived") {
					await client.archiveAutomation(input.projectId, input.automationId);
				}

				const allFailures = [
					...nodeSetFailures,
					...nodeRemoveFailures,
					...edgeAddFailures,
					...edgeRemoveFailures,
				];

				const graph = await client.getAutomation(
					input.projectId,
					input.automationId,
				);
				const lines = [formatAutomationGraph(graph)];
				if (allFailures.length > 0) {
					lines.push(
						`\nSome items failed:\n${allFailures.map((f) => `- ${f}`).join("\n")}`,
					);
				}
				return { content: [{ type: "text", text: lines.join("\n") }] };
			}

			case "delete_automation": {
				const input = DeleteAutomationSchema.parse(args);
				await client.deleteAutomation(input.projectId, input.automationId);
				return {
					content: [
						{ type: "text", text: `Automation ${input.automationId} deleted.` },
					],
				};
			}

			default:
				return {
					content: [{ type: "text", text: `Unknown automation tool: ${name}` }],
					isError: true,
				};
		}
	} catch (err) {
		return {
			content: [{ type: "text", text: formatToolError(err) }],
			isError: true,
		};
	}
}
