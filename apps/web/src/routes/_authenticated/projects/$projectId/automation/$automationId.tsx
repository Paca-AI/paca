import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import {
	Archive as ArchiveIcon,
	ArrowLeft,
	History,
	Loader2,
	Pause,
	Pencil,
	Play,
	Save,
	X,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { AutomationCanvas } from "@/components/projects/automation/automation-canvas";
import { AutomationNodeConfigPanel } from "@/components/projects/automation/automation-node-config-panel";
import { AutomationNodePalette } from "@/components/projects/automation/automation-node-palette";
import { AutomationRunHistoryPanel } from "@/components/projects/automation/automation-run-history-panel";
import { getPriority } from "@/components/projects/interactions/priority";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import {
	ACTION_TYPES,
	type ActionConfig,
	type ActionType,
	type AutomationEdge,
	type AutomationNode,
	activateAutomation,
	addAutomationEdge,
	addAutomationNode,
	archiveAutomation,
	automationQueryOptions,
	automationRunsQueryOptions,
	type ConditionConfig,
	type PluginNodeConfigSchema,
	pluginNodeTypesQueryOptions,
	removeAutomationEdge,
	removeAutomationNode,
	revertAutomationToDraft,
	TRIGGER_TYPES,
	type TriggerConfig,
	type TriggerType,
	updateAutomation,
	updateAutomationNode,
} from "@/lib/automation-api";
import { sprintsQueryOptions } from "@/lib/interaction-api";
import {
	customFieldsQueryOptions,
	projectMembersQueryOptions,
	taskStatusesQueryOptions,
	taskTypesQueryOptions,
} from "@/lib/project-api";
import { cn } from "@/lib/utils";

function extractErrorMessage(err: unknown, fallback: string): string {
	const e = err as { response?: { data?: { error?: string } } };
	return e?.response?.data?.error || fallback;
}

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/automation/$automationId",
)({
	loader: async ({
		context: { queryClient },
		params: { projectId, automationId },
	}) => {
		await Promise.all([
			queryClient.ensureQueryData(
				automationQueryOptions(projectId, automationId),
			),
			queryClient.ensureQueryData(taskStatusesQueryOptions(projectId)),
			queryClient.ensureQueryData(projectMembersQueryOptions(projectId)),
			queryClient.ensureQueryData(customFieldsQueryOptions(projectId)),
			queryClient.ensureQueryData(taskTypesQueryOptions(projectId)),
		]);
	},
	component: AutomationBuilderPage,
});

function AutomationBuilderPage() {
	const { t } = useTranslation("projects");
	const { projectId, automationId } = Route.useParams();
	const qc = useQueryClient();
	const { hasProjectPermission } = useProjectPermissions(projectId);
	const canManage = hasProjectPermission("workflows.write");

	const { data: graph } = useQuery(
		automationQueryOptions(projectId, automationId),
	);
	const { data: statuses = [] } = useQuery(taskStatusesQueryOptions(projectId));
	const { data: members = [] } = useQuery(
		projectMembersQueryOptions(projectId),
	);
	const { data: customFields = [] } = useQuery(
		customFieldsQueryOptions(projectId),
	);
	const { data: taskTypes = [] } = useQuery(taskTypesQueryOptions(projectId));
	const { data: sprints = [] } = useQuery(sprintsQueryOptions(projectId));
	const { data: runs = [] } = useQuery(
		automationRunsQueryOptions(projectId, automationId),
	);
	const { data: pluginTypes } = useQuery(
		pluginNodeTypesQueryOptions(projectId),
	);

	const [tab, setTab] = useState<"graph" | "runs">("graph");
	const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
	const [errorMessage, setErrorMessage] = useState<string | null>(null);
	const [renaming, setRenaming] = useState(false);
	const [nameDraft, setNameDraft] = useState("");

	const graphKey = automationQueryOptions(projectId, automationId).queryKey;
	const invalidate = () => qc.invalidateQueries({ queryKey: graphKey });

	const isDraft = graph?.automation.status === "draft";
	const isArchived = graph?.automation.status === "archived";
	const canEditGraph = canManage && !isArchived;

	function reportError(err: unknown) {
		setErrorMessage(
			extractErrorMessage(err, t("automation.builder.genericError")),
		);
	}

	function labelForType(
		kind: "trigger" | "condition" | "action",
		type: string,
	): string {
		if (kind === "condition" && type === "condition") {
			return t("automation.nodeKind.condition");
		}
		if (
			kind === "trigger" &&
			(TRIGGER_TYPES as readonly string[]).includes(type)
		) {
			return t(`automation.triggerTypes.${type as TriggerType}`);
		}
		if (
			kind === "action" &&
			(ACTION_TYPES as readonly string[]).includes(type)
		) {
			return t(`automation.actionTypes.${type as ActionType}`);
		}
		const pluginList =
			kind === "trigger"
				? pluginTypes?.triggers
				: kind === "condition"
					? pluginTypes?.conditions
					: pluginTypes?.actions;
		return pluginList?.find((p) => p.type === type)?.label ?? type;
	}

	function nodeLabel(nodeId: string): string {
		const node = graph?.nodes.find((n) => n.id === nodeId);
		if (!node) return nodeId;
		return labelForType(node.kind, node.type);
	}

	// Looks up the plugin-declared configSchema for a plugin-contributed
	// node type — undefined for built-in node types (which use their own
	// dedicated config forms, not the plugin schema path) or plugin types
	// that declare no configSchema.
	function configSchemaForType(
		kind: "trigger" | "condition" | "action",
		type: string,
	): PluginNodeConfigSchema | undefined {
		const pluginList =
			kind === "trigger"
				? pluginTypes?.triggers
				: kind === "condition"
					? pluginTypes?.conditions
					: pluginTypes?.actions;
		return pluginList?.find((p) => p.type === type)?.configSchema;
	}

	// Mirrors the backend's NodeRequiresTask/validateTaskReachability
	// (services/api/internal/domain/automation/entity.go,
	// internal/service/automation/automation_service.go) — checked
	// client-side too so a bad drag never round-trips to the server just to
	// be rejected. A trigger with no target_task_id (predecessor_done, cron,
	// api_trigger) has no task for the whole run, so every condition and
	// every action except call_api and trigger_ai_agent (which fires a
	// standalone message at the agent instead of assigning a task when
	// there's none) is off-limits downstream from it.
	function nodeRequiresTask(node: AutomationNode): boolean {
		if (node.kind === "condition") return true;
		if (node.kind === "action") {
			return node.type !== "call_api" && node.type !== "trigger_ai_agent";
		}
		return false;
	}

	function isTaskLessTrigger(node: AutomationNode): boolean {
		if (node.kind !== "trigger") return false;
		if (!["predecessor_done", "cron", "api_trigger"].includes(node.type)) {
			return false;
		}
		return !(node.config as TriggerConfig).target_task_id;
	}

	function wouldViolateTaskReachability(
		nodes: AutomationNode[],
		edges: AutomationEdge[],
		candidateSource: string,
		candidateTarget: string,
	): boolean {
		const nodesById = new Map(nodes.map((n) => [n.id, n]));
		const outgoing = new Map<string, string[]>();
		for (const e of edges) {
			outgoing.set(e.source_node_id, [
				...(outgoing.get(e.source_node_id) ?? []),
				e.target_node_id,
			]);
		}
		outgoing.set(candidateSource, [
			...(outgoing.get(candidateSource) ?? []),
			candidateTarget,
		]);

		for (const trigger of nodes) {
			if (!isTaskLessTrigger(trigger)) continue;
			const visited = new Set([trigger.id]);
			const queue = [...(outgoing.get(trigger.id) ?? [])];
			for (let id = queue.shift(); id !== undefined; id = queue.shift()) {
				if (visited.has(id)) continue;
				visited.add(id);
				const node = nodesById.get(id);
				if (!node) continue;
				if (nodeRequiresTask(node)) return true;
				queue.push(...(outgoing.get(id) ?? []));
			}
		}
		return false;
	}

	function describeNode(node: AutomationNode): string | undefined {
		if (node.kind === "trigger") {
			const cfg = node.config as TriggerConfig;
			switch (node.type) {
				case "status_changed":
					return cfg.status_id
						? statuses.find((s) => s.id === cfg.status_id)?.name
						: t("automation.nodeConfig.trigger.anyStatus");
				case "tag_added":
					return cfg.tag
						? `#${cfg.tag}`
						: t("automation.nodeConfig.trigger.anyTag");
				case "due_date_reached": {
					const minutes = cfg.due_date_offset_minutes ?? 0;
					if (minutes === 0) {
						return t("automation.nodeConfig.description.dueDateAt");
					}
					return minutes < 0
						? t("automation.nodeConfig.description.dueDateBefore", {
								minutes: Math.abs(minutes),
							})
						: t("automation.nodeConfig.description.dueDateAfter", {
								minutes,
							});
				}
				case "predecessor_done":
					return t("automation.dependencyMap.watchedTasks", {
						count: cfg.watched_task_ids?.length ?? 0,
					});
				case "cron":
					return cfg.cron_expression
						? t("automation.nodeConfig.description.cronSchedule", {
								expression: cfg.cron_expression,
							})
						: t("automation.nodeConfig.description.notConfigured");
				case "api_trigger":
					return t("automation.nodeConfig.description.webhookReady");
				default:
					return undefined;
			}
		}
		if (node.kind === "condition" && node.type === "condition") {
			const cfg = node.config as unknown as ConditionConfig;
			return t("automation.nodeConfig.description.branches", {
				count: cfg.branches?.length ?? 0,
			});
		}
		if (node.kind === "action") {
			const cfg = node.config as ActionConfig;
			switch (node.type) {
				case "trigger_ai_agent": {
					if (!cfg.member_id) {
						return t("automation.nodeConfig.description.unassigned");
					}
					const member = members.find((m) => m.id === cfg.member_id);
					return member?.full_name || member?.username;
				}
				case "update_task": {
					const fields = cfg.update ?? {};
					const parts: string[] = [];
					const FIELD_LABEL_KEYS = {
						title: "automation.nodeConfig.condition.fields.title",
						status_id: "automation.nodeConfig.condition.fields.status_id",
						task_type_id: "automation.nodeConfig.condition.fields.task_type_id",
						assignee_ids: "automation.nodeConfig.condition.fields.assignee_ids",
						importance: "automation.nodeConfig.condition.fields.importance",
						story_points: "automation.nodeConfig.condition.fields.story_points",
						tags: "automation.nodeConfig.condition.fields.tags",
						reporter_id: "automation.nodeConfig.condition.fields.reporter_id",
						sprint_id: "automation.nodeConfig.condition.fields.sprint_id",
						start_date: "automation.nodeConfig.condition.fields.start_date",
						due_date: "automation.nodeConfig.condition.fields.due_date",
					} as const;
					const pushField = (
						fieldKey: keyof typeof FIELD_LABEL_KEYS,
						value: string | undefined,
					) => {
						if (!value) return;
						parts.push(
							t("automation.nodeConfig.description.fieldValue", {
								field: t(FIELD_LABEL_KEYS[fieldKey]),
								value,
							}),
						);
					};
					pushField("title", fields.title);
					pushField(
						"status_id",
						statuses.find((s) => s.id === fields.status_id)?.name,
					);
					pushField(
						"task_type_id",
						taskTypes.find((tt) => tt.id === fields.task_type_id)?.name,
					);
					if (fields.assignee_ids?.length) {
						const member = members.find(
							(m) => m.id === fields.assignee_ids?.[0],
						);
						pushField("assignee_ids", member?.full_name || member?.username);
					}
					if (fields.importance !== undefined) {
						pushField("importance", t(getPriority(fields.importance).labelKey));
					}
					if (fields.story_points !== undefined) {
						pushField("story_points", String(fields.story_points));
					}
					if (fields.tags?.length) {
						pushField("tags", fields.tags.map((tag) => `#${tag}`).join(" "));
					}
					pushField(
						"reporter_id",
						members.find((m) => m.id === fields.reporter_id)?.full_name ||
							members.find((m) => m.id === fields.reporter_id)?.username,
					);
					pushField(
						"sprint_id",
						sprints.find((s) => s.id === fields.sprint_id)?.name,
					);
					pushField("start_date", fields.start_date?.slice(0, 10));
					pushField("due_date", fields.due_date?.slice(0, 10));
					return parts.length > 0
						? parts.join("\n")
						: t("automation.nodeConfig.description.notConfigured");
				}
				case "call_api": {
					if (!cfg.method || !cfg.url) {
						return t("automation.nodeConfig.description.notConfigured");
					}
					let host = cfg.url;
					try {
						host = new URL(cfg.url).host;
					} catch {
						// Leave the raw (possibly still-being-typed) URL as-is.
					}
					return t("automation.nodeConfig.description.callApiSummary", {
						method: cfg.method.toUpperCase(),
						host,
					});
				}
				default:
					return undefined;
			}
		}
		return undefined;
	}

	const randomPos = () => ({
		x: 80 + Math.random() * 300,
		y: 80 + Math.random() * 200,
	});

	const addNodeMutation = useMutation({
		mutationFn: (input: {
			kind: "trigger" | "condition" | "action";
			type: string;
		}) => {
			const pos = randomPos();
			return addAutomationNode(projectId, automationId, {
				kind: input.kind,
				type: input.type,
				config: {},
				pos_x: pos.x,
				pos_y: pos.y,
			});
		},
		onSuccess: (created) => {
			invalidate();
			setSelectedNodeId(created.id);
		},
		onError: reportError,
	});

	const updateNodeMutation = useMutation({
		mutationFn: ({
			nodeId,
			config,
		}: {
			nodeId: string;
			config?: Record<string, unknown>;
			posX?: number;
			posY?: number;
		}) =>
			updateAutomationNode(projectId, automationId, nodeId, {
				config,
			}),
		onSuccess: invalidate,
		onError: reportError,
	});

	const moveNodeMutation = useMutation({
		mutationFn: ({
			nodeId,
			posX,
			posY,
		}: {
			nodeId: string;
			posX: number;
			posY: number;
		}) =>
			updateAutomationNode(projectId, automationId, nodeId, {
				pos_x: posX,
				pos_y: posY,
			}),
		onSuccess: invalidate,
	});

	const removeNodeMutation = useMutation({
		mutationFn: (nodeId: string) =>
			removeAutomationNode(projectId, automationId, nodeId),
		onSuccess: () => {
			setSelectedNodeId(null);
			invalidate();
		},
		onError: reportError,
	});

	const addEdgeMutation = useMutation({
		mutationFn: ({
			source,
			sourceHandle,
			target,
		}: {
			source: string;
			sourceHandle: string | null;
			target: string;
		}) =>
			addAutomationEdge(projectId, automationId, {
				source_node_id: source,
				source_handle: sourceHandle,
				target_node_id: target,
			}),
		onSuccess: invalidate,
		onError: reportError,
	});

	const removeEdgeMutation = useMutation({
		mutationFn: (edgeId: string) =>
			removeAutomationEdge(projectId, automationId, edgeId),
		onSuccess: invalidate,
		onError: reportError,
	});

	const renameMutation = useMutation({
		mutationFn: () =>
			updateAutomation(projectId, automationId, { name: nameDraft }),
		onSuccess: () => {
			setRenaming(false);
			invalidate();
		},
		onError: reportError,
	});

	const activateMutation = useMutation({
		mutationFn: () => activateAutomation(projectId, automationId),
		onSuccess: invalidate,
		onError: reportError,
	});
	const archiveMutation = useMutation({
		mutationFn: () => archiveAutomation(projectId, automationId),
		onSuccess: invalidate,
		onError: reportError,
	});
	const revertMutation = useMutation({
		mutationFn: () => revertAutomationToDraft(projectId, automationId),
		onSuccess: invalidate,
		onError: reportError,
	});

	if (!graph) {
		return (
			<div className="flex items-center justify-center flex-1">
				<Loader2 className="size-5 animate-spin text-muted-foreground" />
			</div>
		);
	}

	const selectedNode = graph.nodes.find((n) => n.id === selectedNodeId) ?? null;

	return (
		<div className="flex flex-col min-h-0 flex-1">
			{/* Toolbar */}
			<div className="flex items-center justify-between gap-3 border-b border-border/50 px-4 py-3 shrink-0">
				<div className="flex items-center gap-3 min-w-0">
					<Link
						to="/projects/$projectId/automation"
						params={{ projectId }}
						className={buttonVariants({ variant: "ghost", size: "icon" })}
					>
						<ArrowLeft className="size-4" />
					</Link>
					{renaming ? (
						<div className="flex items-center gap-1.5">
							<Input
								autoFocus
								value={nameDraft}
								onChange={(e) => setNameDraft(e.target.value)}
								className="h-8 w-56"
							/>
							<Button
								size="icon"
								variant="ghost"
								className="size-8"
								onClick={() => renameMutation.mutate()}
							>
								<Save className="size-3.5" />
							</Button>
							<Button
								size="icon"
								variant="ghost"
								className="size-8"
								onClick={() => setRenaming(false)}
							>
								<X className="size-3.5" />
							</Button>
						</div>
					) : (
						<div className="flex items-center gap-2 min-w-0">
							<h1 className="font-semibold text-sm truncate">
								{graph.automation.name}
							</h1>
							{canManage && !isArchived && (
								<button
									type="button"
									onClick={() => {
										setNameDraft(graph.automation.name);
										setRenaming(true);
									}}
									className="text-muted-foreground/50 hover:text-foreground transition-colors"
								>
									<Pencil className="size-3" />
								</button>
							)}
						</div>
					)}
					<Badge
						variant={
							graph.automation.status === "active"
								? "default"
								: graph.automation.status === "draft"
									? "secondary"
									: "outline"
						}
					>
						{t(`automation.status.${graph.automation.status}`)}
					</Badge>
					<div className="flex items-center gap-1 ml-2">
						<button
							type="button"
							onClick={() => setTab("graph")}
							className={cn(
								"rounded-md px-2.5 py-1 text-xs font-medium transition-colors",
								tab === "graph"
									? "bg-muted text-foreground"
									: "text-muted-foreground hover:text-foreground",
							)}
						>
							{t("automation.builder.tabGraph")}
						</button>
						<button
							type="button"
							onClick={() => setTab("runs")}
							className={cn(
								"flex items-center gap-1 rounded-md px-2.5 py-1 text-xs font-medium transition-colors",
								tab === "runs"
									? "bg-muted text-foreground"
									: "text-muted-foreground hover:text-foreground",
							)}
						>
							<History className="size-3" />
							{t("automation.builder.tabRuns")}
						</button>
					</div>
				</div>

				<div className="flex items-center gap-2 shrink-0">
					{tab === "graph" && (
						<AutomationNodePalette
							projectId={projectId}
							canEdit={canEditGraph}
							onAddTrigger={(triggerType) =>
								addNodeMutation.mutate({ kind: "trigger", type: triggerType })
							}
							onAddCondition={(conditionType) =>
								addNodeMutation.mutate({
									kind: "condition",
									type: conditionType,
								})
							}
							onAddAction={(actionType) =>
								addNodeMutation.mutate({ kind: "action", type: actionType })
							}
						/>
					)}
					{canManage && isDraft && (
						<Button
							size="sm"
							disabled={activateMutation.isPending}
							onClick={() => activateMutation.mutate()}
						>
							{activateMutation.isPending ? (
								<Loader2 className="size-3.5 mr-1.5 animate-spin" />
							) : (
								<Play className="size-3.5 mr-1.5" />
							)}
							{t("automation.builder.activate")}
						</Button>
					)}
					{canManage && graph.automation.status === "active" && (
						<Button
							size="sm"
							variant="outline"
							disabled={archiveMutation.isPending}
							onClick={() => archiveMutation.mutate()}
						>
							<ArchiveIcon className="size-3.5 mr-1.5" />
							{t("automation.builder.archive")}
						</Button>
					)}
					{canManage && graph.automation.status === "active" && (
						<Button
							size="sm"
							variant="outline"
							disabled={revertMutation.isPending}
							onClick={() => revertMutation.mutate()}
						>
							<Pause className="size-3.5 mr-1.5" />
							{t("automation.builder.deactivate")}
						</Button>
					)}
				</div>
			</div>

			{isArchived && (
				<div className="px-4 py-2 bg-muted/30 border-b border-border/30 text-xs text-muted-foreground shrink-0">
					{t("automation.builder.readOnlyNoticeArchived")}
				</div>
			)}

			{tab === "graph" ? (
				<div className="flex flex-1 min-h-0 relative">
					<AutomationCanvas
						nodes={graph.nodes}
						edges={graph.edges}
						nodeLabel={(n) => labelForType(n.kind, n.type)}
						nodeDescription={describeNode}
						canEdit={canEditGraph}
						selectedNodeId={selectedNodeId}
						onSelectNode={setSelectedNodeId}
						onConnect={(source, sourceHandle, target) => {
							if (
								wouldViolateTaskReachability(
									graph.nodes,
									graph.edges,
									source,
									target,
								)
							) {
								setErrorMessage(
									t("automation.builder.taskLessTriggerConnectionError"),
								);
								return;
							}
							addEdgeMutation.mutate({ source, sourceHandle, target });
						}}
						onMoveNode={(nodeId, posX, posY) =>
							moveNodeMutation.mutate({ nodeId, posX, posY })
						}
						onDeleteNode={(nodeId) => removeNodeMutation.mutate(nodeId)}
						onDeleteEdge={(edgeId) => removeEdgeMutation.mutate(edgeId)}
						errorMessage={errorMessage}
						onDismissError={() => setErrorMessage(null)}
					/>
					{selectedNode && (
						<AutomationNodeConfigPanel
							key={selectedNode.id}
							node={selectedNode}
							projectId={projectId}
							automationId={automationId}
							statuses={statuses}
							members={members}
							customFields={customFields}
							taskTypes={taskTypes}
							canEdit={canEditGraph}
							saving={updateNodeMutation.isPending}
							pluginLabel={labelForType(selectedNode.kind, selectedNode.type)}
							pluginConfigSchema={configSchemaForType(
								selectedNode.kind,
								selectedNode.type,
							)}
							onSave={(config) =>
								updateNodeMutation.mutate({ nodeId: selectedNode.id, config })
							}
							onClose={() => setSelectedNodeId(null)}
							onRemove={() => removeNodeMutation.mutate(selectedNode.id)}
						/>
					)}
				</div>
			) : (
				<AutomationRunHistoryPanel
					projectId={projectId}
					automationId={automationId}
					runs={runs}
					nodeLabel={nodeLabel}
				/>
			)}
		</div>
	);
}
