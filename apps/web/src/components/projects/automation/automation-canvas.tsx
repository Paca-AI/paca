import {
	Background,
	type Connection,
	Controls,
	type Edge,
	Handle,
	type Node,
	type NodeProps,
	Position,
	ReactFlow,
	ReactFlowProvider,
	useNodesState,
	useUpdateNodeInternals,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { AlertCircle, GitBranch, Play, Trash2, X, Zap } from "lucide-react";
import {
	type CSSProperties,
	useCallback,
	useEffect,
	useMemo,
	useRef,
	useState,
} from "react";
import { useTranslation } from "react-i18next";
import type {
	AutomationEdge,
	AutomationNode,
	ConditionConfig,
	JevChoiceConfig,
	JevScoreConfig,
} from "@/lib/automation-api";
import {
	CONDITION_NODE_TYPE,
	ELSE_HANDLE,
	JEV_CHOICE_NODE_TYPE,
	JEV_NOUL_NODE_TYPE,
	JEV_SCORE_NODE_TYPE,
	PLUGIN_CONDITION_TRUE_HANDLE,
} from "@/lib/automation-api";
import { cn } from "@/lib/utils";

interface BaseNodeData extends Record<string, unknown> {
	node: AutomationNode;
	label: string;
	description?: string;
	canEdit: boolean;
	selected: boolean;
	onSelect: () => void;
	onDelete: () => void;
}

const KIND_STYLES: Record<
	string,
	{ border: string; bg: string; text: string; icon: typeof Zap }
> = {
	trigger: {
		border: "border-blue-500/50",
		bg: "bg-blue-500/10",
		text: "text-blue-600 dark:text-blue-400",
		icon: Zap,
	},
	condition: {
		border: "border-violet-500/50",
		bg: "bg-violet-500/10",
		text: "text-violet-600 dark:text-violet-400",
		icon: GitBranch,
	},
	action: {
		border: "border-amber-500/50",
		bg: "bg-amber-500/10",
		text: "text-amber-600 dark:text-amber-400",
		icon: Play,
	},
};

function NodeShell({ data }: NodeProps<Node<BaseNodeData>>) {
	const { node, label, description, canEdit, selected, onSelect, onDelete } =
		data;
	const style = KIND_STYLES[node.kind];
	const Icon = style.icon;

	return (
		// biome-ignore lint/a11y/noStaticElementInteractions: click opens the node's config panel; delete stays a separate keyboard-reachable button
		// biome-ignore lint/a11y/useKeyWithClickEvents: click-to-select card, consistent with the canvas's node-selection model
		<div
			onClick={onSelect}
			className={cn(
				"group relative w-64 rounded-xl border-2 bg-card shadow-sm hover:shadow-md cursor-pointer transition-shadow",
				style.border,
				selected && "shadow-md ring-2 ring-offset-2 ring-offset-background",
				selected && style.border.replace("border-", "ring-"),
			)}
		>
			{node.kind !== "trigger" && (
				<Handle
					type="target"
					position={Position.Left}
					id="default"
					className="size-2.5! bg-muted-foreground/50! border-2! border-background!"
				/>
			)}
			<div className="px-3 py-2.5">
				<div className="flex items-center gap-2">
					<div
						className={cn(
							"flex size-6 shrink-0 items-center justify-center rounded-md",
							style.bg,
						)}
					>
						<Icon className={cn("size-3.5", style.text)} />
					</div>
					<div className="min-w-0 flex-1">
						<div
							className={cn("text-xs font-medium truncate", style.text)}
							title={label}
						>
							{label}
						</div>
						{description && (
							<div
								className="space-y-0.5 text-sm font-semibold text-foreground"
								title={description}
							>
								{description.split("\n").map((line, i) => (
									<div
										// biome-ignore lint/suspicious/noArrayIndexKey: lines are a stable ordered render of a description string, no natural key
										key={i}
										className="truncate"
									>
										{line}
									</div>
								))}
							</div>
						)}
					</div>
				</div>
			</div>
			{canEdit && (
				<button
					type="button"
					onClick={(e) => {
						e.stopPropagation();
						onDelete();
					}}
					className="nodrag absolute -top-2 -right-2 opacity-0 group-hover:opacity-100 flex size-5 shrink-0 items-center justify-center rounded-full border border-border/40 bg-card text-muted-foreground/60 shadow-sm hover:text-destructive-foreground hover:bg-destructive hover:border-destructive transition-all"
				>
					<Trash2 className="size-3" />
				</button>
			)}
			{node.kind === "condition" ? (
				<ConditionBranchRows node={node} />
			) : (
				<Handle
					type="source"
					position={Position.Right}
					id="default"
					className="size-2.5! bg-muted-foreground/50! border-2! border-background!"
				/>
			)}
		</div>
	);
}

// branchesForNode computes the set of {handle, label} rows a condition node
// needs, one per outgoing edge it can drive (ELSE_HANDLE is always added
// separately below, not returned here):
//   - the built-in N-branch switch (CONDITION_NODE_TYPE): one per declared
//     ConditionConfig.Branches entry.
//   - jev_choice: one per criteria key — the key itself is the handle (see
//     JevChoiceConfig's doc comment), the description is the label.
//   - jev_score: one per criteria level, by index — "0", "1", ... — since
//     that's what the worker's matchedHandleForAnswer emits.
//   - everything else (jev_noul, and any plugin-contributed condition, e.g.
//     time_logging's total_minutes_exceeds) is a boolean gate: the worker
//     only ever follows PLUGIN_CONDITION_TRUE_HANDLE on a match, so render
//     that one synthetic branch — without it, only the ELSE_HANDLE row below
//     would exist and the "matched" path could never be wired up.
// Returns undefined `label` for the synthetic single-branch fallback case —
// ConditionBranchRows fills it in, since it needs a translated string and
// this function deliberately doesn't take a `t` (avoiding the strict,
// generated TFunction type just to pass it one level down for two static
// keys).
function branchesForNode(
	node: AutomationNode,
): { handle: string; label?: string }[] {
	switch (node.type) {
		case CONDITION_NODE_TYPE:
			return (node.config as unknown as ConditionConfig)?.branches ?? [];
		case JEV_CHOICE_NODE_TYPE:
			return Object.entries(
				(node.config as unknown as JevChoiceConfig)?.criteria ?? {},
			).map(([handle, label]) => ({ handle, label: label || handle }));
		case JEV_SCORE_NODE_TYPE:
			return ((node.config as unknown as JevScoreConfig)?.criteria ?? []).map(
				(label, i) => ({ handle: String(i), label }),
			);
		default:
			return [{ handle: PLUGIN_CONDITION_TRUE_HANDLE }];
	}
}

function ConditionBranchRows({ node }: { node: AutomationNode }) {
	const { t } = useTranslation("projects");
	const branches = branchesForNode(node).map((b) =>
		b.label !== undefined
			? b
			: {
					...b,
					label:
						node.type === JEV_NOUL_NODE_TYPE
							? t("automation.nodeConfig.description.jevNoulTrue")
							: t("automation.nodeConfig.description.pluginConditionMatched"),
				},
	);
	const updateNodeInternals = useUpdateNodeInternals();
	// React Flow caches each node's handle positions on mount and only
	// re-measures when told to — a Condition node's branch handles are added
	// dynamically (starting empty, growing as branches are configured), so
	// without this, new connections can only ever land on whichever handles
	// existed at mount (the "else" handle), never on later-added branches.
	const handleIds = branches.map((b) => b.handle).join(",");
	// biome-ignore lint/correctness/useExhaustiveDependencies: handleIds isn't read in the body — it's a change signal so this re-runs whenever the branch set changes, not just on node.id
	useEffect(() => {
		// updateNodeInternals() already rAF-defers its own measurement, but an
		// extra frame of margin here is cheap insurance against the new
		// branch <Handle> elements not being laid out yet when it queries them.
		const raf = requestAnimationFrame(() => updateNodeInternals(node.id));
		return () => cancelAnimationFrame(raf);
	}, [node.id, handleIds, updateNodeInternals]);

	return (
		<div className="border-t border-border/40">
			{branches.map((b, i) => (
				<div
					key={b.handle}
					className="relative flex items-center gap-2 border-b border-border/20 px-3 py-1.5"
				>
					<span className="min-w-0 flex-1 truncate text-[11px] text-muted-foreground">
						{b.label ||
							t("automation.nodeConfig.description.branchFallback", {
								n: i + 1,
							})}
					</span>
					<Handle
						type="source"
						position={Position.Right}
						id={b.handle}
						className="size-2.5! bg-violet-500/70! border-2! border-background!"
					/>
				</div>
			))}
			<div className="relative flex items-center gap-2 px-3 py-1.5">
				<span className="min-w-0 flex-1 truncate text-[11px] text-muted-foreground italic">
					{t("automation.nodeConfig.description.elseShort")}
				</span>
				<Handle
					type="source"
					position={Position.Right}
					id={ELSE_HANDLE}
					className="size-2.5! bg-muted-foreground/50! border-2! border-background!"
				/>
			</div>
		</div>
	);
}

const nodeTypes = { automationNode: NodeShell };

interface AutomationCanvasProps {
	nodes: AutomationNode[];
	edges: AutomationEdge[];
	nodeLabel: (node: AutomationNode) => string;
	nodeDescription?: (node: AutomationNode) => string | undefined;
	canEdit: boolean;
	selectedNodeId: string | null;
	onSelectNode: (nodeId: string) => void;
	onConnect: (
		sourceNodeId: string,
		sourceHandle: string | null,
		targetNodeId: string,
	) => void;
	onMoveNode: (nodeId: string, posX: number, posY: number) => void;
	onDeleteNode: (nodeId: string) => void;
	onDeleteEdge: (edgeId: string) => void;
	errorMessage?: string | null;
	onDismissError?: () => void;
}

export function AutomationCanvas({
	nodes,
	edges,
	nodeLabel,
	nodeDescription,
	canEdit,
	selectedNodeId,
	onSelectNode,
	onConnect,
	onMoveNode,
	onDeleteNode,
	onDeleteEdge,
	errorMessage,
	onDismissError,
}: AutomationCanvasProps) {
	const { t } = useTranslation("projects");
	const [selectedEdgeId, setSelectedEdgeId] = useState<string | null>(null);

	const flowNodes = useMemo<Node<BaseNodeData>[]>(
		() =>
			nodes.map((n) => ({
				id: n.id,
				type: "automationNode",
				position: { x: n.pos_x, y: n.pos_y },
				draggable: canEdit,
				connectable: canEdit,
				data: {
					node: n,
					label: nodeLabel(n),
					description: nodeDescription?.(n),
					canEdit,
					selected: n.id === selectedNodeId,
					onSelect: () => onSelectNode(n.id),
					onDelete: () => onDeleteNode(n.id),
				},
			})),
		[
			nodes,
			nodeLabel,
			nodeDescription,
			canEdit,
			selectedNodeId,
			onSelectNode,
			onDeleteNode,
		],
	);

	const [rfNodes, setRfNodes, onNodesChange] =
		useNodesState<Node<BaseNodeData>>(flowNodes);
	const isDraggingRef = useRef(false);
	const pendingPositionsRef = useRef(
		new Map<string, { x: number; y: number }>(),
	);

	useEffect(() => {
		if (isDraggingRef.current) return;
		const pending = pendingPositionsRef.current;
		if (pending.size === 0) {
			setRfNodes(flowNodes);
			return;
		}
		setRfNodes(
			flowNodes.map((n) => {
				const p = pending.get(n.id);
				if (!p) return n;
				if (n.position.x === p.x && n.position.y === p.y) {
					pending.delete(n.id);
					return n;
				}
				return { ...n, position: p };
			}),
		);
	}, [flowNodes, setRfNodes]);

	const flowEdges = useMemo<Edge[]>(
		() =>
			edges.map((e) => ({
				id: e.id,
				source: e.source_node_id,
				sourceHandle: e.source_handle ?? "default",
				target: e.target_node_id,
				targetHandle: "default",
				animated: true,
				selected: e.id === selectedEdgeId,
				style:
					e.id === selectedEdgeId
						? { stroke: "var(--color-primary)", strokeWidth: 2.5 }
						: undefined,
			})),
		[edges, selectedEdgeId],
	);

	const handleConnect = useCallback(
		(connection: Connection) => {
			if (!canEdit || !connection.source || !connection.target) return;
			const handle =
				connection.sourceHandle && connection.sourceHandle !== "default"
					? connection.sourceHandle
					: null;
			onConnect(connection.source, handle, connection.target);
		},
		[canEdit, onConnect],
	);

	return (
		<div className="relative flex-1 min-h-0">
			{errorMessage && (
				<div className="absolute top-3 left-1/2 -translate-x-1/2 z-10 flex items-center gap-2 rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive shadow-lg max-w-md">
					<AlertCircle className="size-3.5 shrink-0" />
					<span className="flex-1">{errorMessage}</span>
					{onDismissError && (
						<button type="button" onClick={onDismissError} className="shrink-0">
							<X className="size-3.5" />
						</button>
					)}
				</div>
			)}
			{selectedEdgeId && canEdit && (
				<div className="absolute top-3 right-3 z-10">
					<button
						type="button"
						onClick={() => {
							onDeleteEdge(selectedEdgeId);
							setSelectedEdgeId(null);
						}}
						className="flex items-center gap-1.5 rounded-lg border border-destructive/30 bg-card px-3 py-1.5 text-xs font-medium text-destructive shadow-lg hover:bg-destructive hover:text-destructive-foreground transition-colors"
					>
						<Trash2 className="size-3.5" />
						{t("automation.canvas.deleteEdge")}
					</button>
				</div>
			)}
			<ReactFlowProvider>
				<ReactFlow
					nodes={rfNodes}
					edges={flowEdges}
					nodeTypes={nodeTypes}
					onNodesChange={onNodesChange}
					onNodeDragStart={() => {
						isDraggingRef.current = true;
					}}
					onNodeDragStop={(_, node) => {
						isDraggingRef.current = false;
						pendingPositionsRef.current.set(node.id, node.position);
						onMoveNode(node.id, node.position.x, node.position.y);
					}}
					onConnect={handleConnect}
					onEdgeClick={(_, edge) => setSelectedEdgeId(edge.id)}
					onPaneClick={() => setSelectedEdgeId(null)}
					nodesDraggable={canEdit}
					nodesConnectable={canEdit}
					elementsSelectable
					fitView
					proOptions={{ hideAttribution: true }}
					className={cn(!canEdit && "opacity-90")}
				>
					<Background gap={20} />
					<Controls
						showInteractive={false}
						style={
							{
								"--xy-controls-button-background-color": "var(--sidebar)",
								"--xy-controls-button-background-color-hover":
									"var(--sidebar-accent)",
								"--xy-controls-button-color": "var(--sidebar-foreground)",
								"--xy-controls-button-color-hover":
									"var(--sidebar-accent-foreground)",
								"--xy-controls-button-border-color": "var(--sidebar-border)",
							} as CSSProperties
						}
					/>
				</ReactFlow>
			</ReactFlowProvider>
		</div>
	);
}
