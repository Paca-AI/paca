import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import {
	ChevronDown,
	Copy,
	Loader2,
	Plus,
	Search,
	Trash2,
	X,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
	getImportanceBucket,
	IMPORTANCE_BUCKET_VALUES,
	PRIORITY_LEVELS,
} from "@/components/projects/interactions/priority";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { useTaskPickerSearch } from "@/hooks/use-task-picker-search";
import {
	ACTION_TYPES,
	type ActionConfig,
	type AutomationNode,
	CONDITION_NODE_TYPE,
	type ConditionBranch,
	type ConditionConfig,
	type ConditionField,
	type ConditionOperator,
	generateWebhookToken,
	MULTI_VALUED_TARGET_KINDS,
	type TaskTarget,
	type TaskTargetKind,
	TRIGGER_TYPES,
	type TriggerConfig,
} from "@/lib/automation-api";
import {
	listAllTasks,
	type Task,
	taskQueryOptions,
	tasksPickerInfiniteQueryOptions,
} from "@/lib/interaction-api";
import type {
	CustomFieldDefinition,
	ProjectMember,
	TaskStatus,
	TaskType,
} from "@/lib/project-api";
import { createLoadMoreScrollHandler } from "@/lib/scroll-pagination";

interface AutomationNodeConfigPanelProps {
	node: AutomationNode;
	projectId: string;
	automationId: string;
	statuses: TaskStatus[];
	members: ProjectMember[];
	customFields: CustomFieldDefinition[];
	taskTypes: TaskType[];
	canEdit: boolean;
	onSave: (config: Record<string, unknown>) => void;
	onClose: () => void;
	onRemove: () => void;
	saving?: boolean;
	/** Human-readable label for a plugin-contributed node type, from the
	 * plugin's own manifest — used instead of an i18n lookup when the
	 * node's type isn't one of the built-ins. */
	pluginLabel?: string;
}

// The task list API caps page_size at 200, so a full project task picker has
// to page through — otherwise tasks past the first 200 are invisible to it.
const MAX_TASK_PAGES = 25;

async function fetchAllProjectTasks(projectId: string): Promise<Task[]> {
	const all: Task[] = [];
	let cursor: string | undefined;
	for (let page = 0; page < MAX_TASK_PAGES; page++) {
		const result = await listAllTasks(projectId, { pageSize: 200, cursor });
		all.push(...result.items);
		const next = result.next_cursor;
		if (!next) break;
		cursor = next;
	}
	return all;
}

function taskLabel(task: Task): string {
	return `#${task.task_number} ${task.title}`;
}

const CONDITION_FIELDS: ConditionField[] = [
	"status_id",
	"task_type_id",
	"importance",
	"assignee_ids",
	"tags",
	"custom_field",
];

const OPERATORS_BY_FIELD: Record<ConditionField, ConditionOperator[]> = {
	status_id: ["equals", "not_equals"],
	task_type_id: ["equals", "not_equals"],
	importance: ["equals", "not_equals", "greater_than", "less_than"],
	assignee_ids: ["contains", "not_equals", "is_empty", "is_not_empty"],
	tags: ["contains", "not_equals", "is_empty", "is_not_empty"],
	custom_field: ["equals", "not_equals", "is_empty", "is_not_empty"],
};

export function AutomationNodeConfigPanel({
	node,
	projectId,
	automationId,
	statuses,
	members,
	customFields,
	taskTypes,
	canEdit,
	onSave,
	onClose,
	onRemove,
	saving,
	pluginLabel,
}: AutomationNodeConfigPanelProps) {
	const { t } = useTranslation("projects");
	const [removeOpen, setRemoveOpen] = useState(false);

	const isBuiltinTrigger = (TRIGGER_TYPES as readonly string[]).includes(
		node.type,
	);
	const isBuiltinAction = (ACTION_TYPES as readonly string[]).includes(
		node.type,
	);
	const isBuiltinCondition = node.type === CONDITION_NODE_TYPE;

	if (node.kind === "trigger") {
		return (
			<Panel
				title={
					isBuiltinTrigger
						? t(
								`automation.triggerTypes.${node.type as (typeof TRIGGER_TYPES)[number]}`,
							)
						: (pluginLabel ?? node.type)
				}
				kind="trigger"
				canEdit={canEdit}
				onClose={onClose}
				onRemoveClick={() => setRemoveOpen(true)}
			>
				{isBuiltinTrigger ? (
					<TriggerConfigForm
						type={node.type}
						config={node.config as TriggerConfig}
						projectId={projectId}
						automationId={automationId}
						nodeId={node.id}
						statuses={statuses}
						canEdit={canEdit}
						onSave={onSave}
						saving={saving}
					/>
				) : (
					<GenericConfigForm
						config={node.config}
						canEdit={canEdit}
						onSave={onSave}
						saving={saving}
					/>
				)}
				<RemoveDialog
					open={removeOpen}
					onOpenChange={setRemoveOpen}
					onConfirm={onRemove}
				/>
			</Panel>
		);
	}

	if (node.kind === "condition") {
		return (
			<Panel
				title={
					isBuiltinCondition
						? t("automation.nodeKind.condition")
						: (pluginLabel ?? node.type)
				}
				kind="condition"
				canEdit={canEdit}
				onClose={onClose}
				onRemoveClick={() => setRemoveOpen(true)}
			>
				{isBuiltinCondition ? (
					<ConditionConfigForm
						config={node.config as unknown as ConditionConfig}
						projectId={projectId}
						statuses={statuses}
						members={members}
						customFields={customFields}
						taskTypes={taskTypes}
						canEdit={canEdit}
						onSave={onSave}
						saving={saving}
					/>
				) : (
					<GenericConfigForm
						config={node.config}
						canEdit={canEdit}
						onSave={onSave}
						saving={saving}
					/>
				)}
				<RemoveDialog
					open={removeOpen}
					onOpenChange={setRemoveOpen}
					onConfirm={onRemove}
				/>
			</Panel>
		);
	}

	return (
		<Panel
			title={
				isBuiltinAction
					? t(
							`automation.actionTypes.${node.type as (typeof ACTION_TYPES)[number]}`,
						)
					: (pluginLabel ?? node.type)
			}
			kind="action"
			canEdit={canEdit}
			onClose={onClose}
			onRemoveClick={() => setRemoveOpen(true)}
		>
			{isBuiltinAction ? (
				<ActionConfigForm
					type={node.type}
					config={node.config as ActionConfig}
					projectId={projectId}
					statuses={statuses}
					members={members}
					customFields={customFields}
					canEdit={canEdit}
					onSave={onSave}
					saving={saving}
				/>
			) : (
				<GenericConfigForm
					config={node.config}
					canEdit={canEdit}
					onSave={onSave}
					saving={saving}
				/>
			)}
			<RemoveDialog
				open={removeOpen}
				onOpenChange={setRemoveOpen}
				onConfirm={onRemove}
			/>
		</Panel>
	);
}

// GenericConfigForm is the fallback config editor for any plugin-contributed
// node type: a raw JSON editor. Plugins that want a real UI (a live picker,
// not a bare textarea) can register a component at the
// "automation.node.config" frontend extension point once one exists — see
// the design note in the plugin automation bridge; until then, this is the
// only surface available for editing a plugin node's config.
function GenericConfigForm({
	config,
	canEdit,
	onSave,
	saving,
}: {
	config: Record<string, unknown>;
	canEdit: boolean;
	onSave: (config: Record<string, unknown>) => void;
	saving?: boolean;
}) {
	const { t } = useTranslation("projects");
	const [text, setText] = useState(() => JSON.stringify(config, null, 2));
	const [parseError, setParseError] = useState<string | null>(null);

	return (
		<div className="space-y-2">
			<Textarea
				value={text}
				onChange={(e) => setText(e.target.value)}
				rows={10}
				className="font-mono text-xs"
				disabled={!canEdit}
			/>
			{parseError && <p className="text-xs text-destructive">{parseError}</p>}
			{canEdit && (
				<SaveButton
					saving={saving}
					onClick={() => {
						try {
							const parsed = JSON.parse(text);
							setParseError(null);
							onSave(parsed);
						} catch {
							setParseError("Invalid JSON");
						}
					}}
				/>
			)}
			<p className="text-xs text-muted-foreground">
				{t("automation.nodeConfig.genericFormHint")}
			</p>
		</div>
	);
}

const NODE_KIND_LABEL_KEY = {
	trigger: "automation.nodeKind.trigger",
	condition: "automation.nodeKind.condition",
	action: "automation.nodeKind.action",
} as const;

function Panel({
	title,
	kind,
	canEdit,
	onClose,
	onRemoveClick,
	children,
}: {
	title: string;
	kind: keyof typeof NODE_KIND_LABEL_KEY;
	canEdit: boolean;
	onClose: () => void;
	onRemoveClick: () => void;
	children: React.ReactNode;
}) {
	const { t } = useTranslation("projects");
	return (
		<div className="flex h-full w-80 shrink-0 flex-col border-l border-border/60 bg-card">
			<div className="flex items-center justify-between border-b border-border/60 px-4 py-3">
				<div>
					<div className="text-[10px] font-mono uppercase tracking-wide text-muted-foreground">
						{t(NODE_KIND_LABEL_KEY[kind])}
					</div>
					<div className="text-sm font-semibold">{title}</div>
				</div>
				<button
					type="button"
					onClick={onClose}
					className="text-muted-foreground hover:text-foreground"
				>
					<X className="size-4" />
				</button>
			</div>
			<div className="flex-1 overflow-y-auto px-4 py-4 space-y-4">
				{children}
			</div>
			{canEdit && (
				<div className="border-t border-border/60 px-4 py-3">
					<Button
						variant="outline"
						size="sm"
						className="w-full gap-1.5 text-destructive hover:text-destructive"
						onClick={onRemoveClick}
					>
						<Trash2 className="size-3.5" />
						{t("automation.nodeConfig.remove")}
					</Button>
				</div>
			)}
		</div>
	);
}

function RemoveDialog({
	open,
	onOpenChange,
	onConfirm,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	onConfirm: () => void;
}) {
	const { t } = useTranslation("projects");
	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent>
				<DialogHeader>
					<DialogTitle>
						{t("automation.nodeConfig.removeConfirmTitle")}
					</DialogTitle>
				</DialogHeader>
				<p className="text-sm text-muted-foreground">
					{t("automation.nodeConfig.removeConfirmBody")}
				</p>
				<DialogFooter>
					<Button variant="outline" onClick={() => onOpenChange(false)}>
						{t("automation.nodeConfig.cancel")}
					</Button>
					<Button
						variant="destructive"
						onClick={() => {
							onConfirm();
							onOpenChange(false);
						}}
					>
						{t("automation.nodeConfig.removeConfirmAction")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

// ── Trigger config form ──────────────────────────────────────────────────────

function TriggerConfigForm({
	type,
	config,
	projectId,
	automationId,
	nodeId,
	statuses,
	canEdit,
	onSave,
	saving,
}: {
	type: string;
	config: TriggerConfig;
	projectId: string;
	automationId: string;
	nodeId: string;
	statuses: TaskStatus[];
	canEdit: boolean;
	onSave: (config: Record<string, unknown>) => void;
	saving?: boolean;
}) {
	const { t } = useTranslation("projects");
	const [statusId, setStatusId] = useState(config.status_id ?? "");
	const [tag, setTag] = useState(config.tag ?? "");
	const [offset, setOffset] = useState(
		config.due_date_offset_minutes?.toString() ?? "0",
	);
	const [watchedTaskIds, setWatchedTaskIds] = useState<string[]>(
		config.watched_task_ids ?? [],
	);
	const [targetTaskId, setTargetTaskId] = useState(config.target_task_id ?? "");
	const [cronExpression, setCronExpression] = useState(
		config.cron_expression ?? "",
	);
	// Only predecessor_done still needs the full task list — for its watched-
	// tasks multi-select. TargetTaskPicker (shared by predecessor_done, cron,
	// and api_trigger) fetches its own paginated/searched list instead.
	const { data: tasks = [] } = useQuery({
		queryKey: ["projects", projectId, "tasks", "all-for-automation-picker"],
		queryFn: () => fetchAllProjectTasks(projectId),
		enabled: type === "predecessor_done",
	});

	if (type === "status_changed") {
		return (
			<div className="space-y-3">
				<div className="space-y-1.5">
					<Label>{t("automation.nodeConfig.trigger.statusLabel")}</Label>
					<Select
						value={statusId || "__any__"}
						onValueChange={(v) => setStatusId(!v || v === "__any__" ? "" : v)}
						disabled={!canEdit}
					>
						<SelectTrigger className="w-full">
							<SelectValue>
								{statusId
									? statuses.find((s) => s.id === statusId)?.name
									: t("automation.nodeConfig.trigger.anyStatus")}
							</SelectValue>
						</SelectTrigger>
						<SelectContent>
							<SelectItem value="__any__">
								{t("automation.nodeConfig.trigger.anyStatus")}
							</SelectItem>
							{statuses.map((s) => (
								<SelectItem key={s.id} value={s.id}>
									{s.name}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				</div>
				{canEdit && (
					<SaveButton
						saving={saving}
						onClick={() => onSave({ status_id: statusId || undefined })}
					/>
				)}
			</div>
		);
	}

	if (type === "tag_added") {
		return (
			<div className="space-y-3">
				<div className="space-y-1.5">
					<Label>{t("automation.nodeConfig.trigger.tagLabel")}</Label>
					<Input
						value={tag}
						onChange={(e) => setTag(e.target.value)}
						placeholder={t("automation.nodeConfig.trigger.anyTag")}
						disabled={!canEdit}
					/>
				</div>
				{canEdit && (
					<SaveButton
						saving={saving}
						onClick={() => onSave({ tag: tag || undefined })}
					/>
				)}
			</div>
		);
	}

	if (type === "due_date_reached") {
		return (
			<div className="space-y-3">
				<div className="space-y-1.5">
					<Label>{t("automation.nodeConfig.trigger.dueDateOffsetLabel")}</Label>
					<Input
						type="number"
						value={offset}
						onChange={(e) => setOffset(e.target.value)}
						disabled={!canEdit}
					/>
					<p className="text-xs text-muted-foreground">
						{t("automation.nodeConfig.trigger.dueDateOffsetHint")}
					</p>
				</div>
				{canEdit && (
					<SaveButton
						saving={saving}
						onClick={() =>
							onSave({ due_date_offset_minutes: Number(offset) || 0 })
						}
					/>
				)}
			</div>
		);
	}

	if (type === "predecessor_done") {
		const watchedTasks = watchedTaskIds
			.map((id) => tasks.find((t) => t.id === id))
			.filter((t): t is Task => t != null);
		const availableToWatch = tasks.filter(
			(t) => !watchedTaskIds.includes(t.id),
		);
		return (
			<div className="space-y-3">
				<div className="space-y-1.5">
					<Label>{t("automation.nodeConfig.trigger.targetTaskLabel")}</Label>
					<TargetTaskPicker
						projectId={projectId}
						value={targetTaskId}
						onChange={setTargetTaskId}
						canEdit={canEdit}
					/>
					<p className="text-xs text-muted-foreground">
						{t("automation.nodeConfig.trigger.targetTaskHint")}
					</p>
				</div>
				<div className="space-y-1.5">
					<Label>{t("automation.nodeConfig.trigger.watchedTasksLabel")}</Label>
					{canEdit && (
						<Select
							value=""
							onValueChange={(v) => {
								if (v) setWatchedTaskIds((prev) => [...prev, v]);
							}}
						>
							<SelectTrigger className="w-full">
								<SelectValue
									placeholder={t(
										"automation.nodeConfig.trigger.addWatchedTaskPlaceholder",
									)}
								/>
							</SelectTrigger>
							<SelectContent>
								{availableToWatch.map((t) => (
									<SelectItem key={t.id} value={t.id}>
										{taskLabel(t)}
									</SelectItem>
								))}
							</SelectContent>
						</Select>
					)}
					{watchedTasks.length > 0 && (
						<div className="flex flex-wrap gap-1.5 pt-1">
							{watchedTasks.map((t) => (
								<Badge key={t.id} variant="secondary" className="gap-1">
									{taskLabel(t)}
									{canEdit && (
										<button
											type="button"
											onClick={() =>
												setWatchedTaskIds((prev) =>
													prev.filter((id) => id !== t.id),
												)
											}
										>
											<X className="size-3" />
										</button>
									)}
								</Badge>
							))}
						</div>
					)}
					<p className="text-xs text-muted-foreground">
						{t("automation.nodeConfig.trigger.watchedTasksHint")}
					</p>
				</div>
				{canEdit && (
					<SaveButton
						saving={saving}
						onClick={() =>
							onSave({
								target_task_id: targetTaskId.trim() || undefined,
								watched_task_ids: watchedTaskIds,
							})
						}
					/>
				)}
			</div>
		);
	}

	if (type === "cron") {
		return (
			<div className="space-y-3">
				<div className="space-y-1.5">
					<Label>
						{t("automation.nodeConfig.trigger.cronExpressionLabel")}
					</Label>
					<Input
						value={cronExpression}
						onChange={(e) => setCronExpression(e.target.value)}
						placeholder={t(
							"automation.nodeConfig.trigger.cronExpressionPlaceholder",
						)}
						disabled={!canEdit}
					/>
					<p className="text-xs text-muted-foreground">
						{t("automation.nodeConfig.trigger.cronExpressionHint")}
					</p>
				</div>
				<div className="space-y-1.5">
					<Label>{t("automation.nodeConfig.trigger.targetTaskLabel")}</Label>
					<TargetTaskPicker
						projectId={projectId}
						value={targetTaskId}
						onChange={setTargetTaskId}
						canEdit={canEdit}
					/>
					<p className="text-xs text-muted-foreground">
						{t("automation.nodeConfig.trigger.cronTargetTaskHint")}
					</p>
				</div>
				{canEdit && (
					<SaveButton
						saving={saving}
						disabled={!cronExpression.trim()}
						onClick={() =>
							onSave({
								cron_expression: cronExpression.trim(),
								target_task_id: targetTaskId.trim() || undefined,
							})
						}
					/>
				)}
			</div>
		);
	}

	if (type === "api_trigger") {
		return (
			<div className="space-y-3">
				<div className="space-y-1.5">
					<Label>{t("automation.nodeConfig.trigger.targetTaskLabel")}</Label>
					<TargetTaskPicker
						projectId={projectId}
						value={targetTaskId}
						onChange={setTargetTaskId}
						canEdit={canEdit}
					/>
					<p className="text-xs text-muted-foreground">
						{t("automation.nodeConfig.trigger.apiTriggerTargetTaskHint")}
					</p>
				</div>
				{canEdit && (
					<SaveButton
						saving={saving}
						onClick={() =>
							onSave({ target_task_id: targetTaskId.trim() || undefined })
						}
					/>
				)}
				<WebhookTokenSection
					projectId={projectId}
					automationId={automationId}
					nodeId={nodeId}
					canEdit={canEdit}
				/>
			</div>
		);
	}

	// task_created, assignee_changed, priority_changed — no config fields.
	return (
		<p className="text-xs text-muted-foreground">
			{t("automation.nodeConfig.trigger.anyStatus")}
		</p>
	);
}

// TargetTaskPicker is the target-task picker shared by predecessor_done,
// cron, and api_trigger — all three fire on a basis unrelated to any
// specific task event, so all three need this same fixed-task picker.
// Mirrors the epic picker's dropdown (task-detail/properties-panel.tsx):
// a debounced server-side search plus an infinite-scroll paginated list,
// rather than loading every project task up front — a project can have far
// more tasks than fit comfortably in one query.
function TargetTaskPicker({
	projectId,
	value,
	onChange,
	canEdit,
}: {
	projectId: string;
	value: string;
	onChange: (value: string) => void;
	canEdit: boolean;
}) {
	const { t } = useTranslation("projects");
	const [open, setOpen] = useState(false);

	const {
		data: pages,
		fetchNextPage,
		hasNextPage,
		isFetchingNextPage,
	} = useInfiniteQuery({
		...tasksPickerInfiniteQueryOptions(projectId),
		enabled: open && !!projectId,
	});
	const tasks = useMemo(
		() => pages?.pages.flatMap((page) => page.items) ?? [],
		[pages],
	);
	const pagination = {
		hasMore: !!hasNextPage,
		isLoadingMore: isFetchingNextPage,
		onLoadMore: () => void fetchNextPage(),
	};

	const {
		search,
		setSearch,
		isSearching,
		results: searchResults,
		isLoading: searchLoading,
		pagination: searchPagination,
	} = useTaskPickerSearch(projectId, open);

	// The selected task may not be in the loaded page/search results yet —
	// fetch it directly so the trigger always shows the right label.
	const { data: selectedTask } = useQuery({
		...taskQueryOptions(projectId, value),
		enabled: !!projectId && !!value,
	});

	const displayedTasks = isSearching ? searchResults : tasks;
	const activePagination = isSearching ? searchPagination : pagination;

	return (
		<DropdownMenu open={open} onOpenChange={setOpen}>
			<DropdownMenuTrigger
				disabled={!canEdit}
				className="flex h-8 w-full items-center justify-between gap-1.5 rounded-lg border border-input bg-transparent px-2.5 py-2 text-sm outline-none disabled:cursor-not-allowed disabled:opacity-50 dark:bg-input/30"
			>
				<span className="truncate text-left">
					{selectedTask ? (
						taskLabel(selectedTask)
					) : (
						<span className="text-muted-foreground">
							{t("automation.nodeConfig.trigger.selectTaskPlaceholder")}
						</span>
					)}
				</span>
				<ChevronDown className="size-4 shrink-0 text-muted-foreground" />
			</DropdownMenuTrigger>
			<DropdownMenuContent align="start" className="w-72">
				{value && (
					<>
						<DropdownMenuItem
							className="text-destructive focus:text-destructive"
							onClick={() => onChange("")}
						>
							<X className="size-3.5 mr-2 shrink-0" />
							{t("automation.nodeConfig.trigger.clearTargetTask")}
						</DropdownMenuItem>
						<DropdownMenuSeparator />
					</>
				)}
				<div className="px-1 pb-1">
					<div className="relative">
						<Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground/50" />
						<input
							type="text"
							value={search}
							onChange={(e) => setSearch(e.target.value)}
							onKeyDown={(e) => {
								// Let Escape still close the menu; swallow everything
								// else so typing doesn't trigger the menu's own arrow-key
								// navigation / type-ahead item selection.
								if (e.key !== "Escape") e.stopPropagation();
							}}
							placeholder={t(
								"automation.nodeConfig.trigger.searchTaskPlaceholder",
							)}
							className="w-full rounded-lg border border-border/30 bg-muted/25 py-1.5 pr-2 pl-8 text-sm placeholder:text-muted-foreground/50 transition-all duration-150 focus:border-primary/40 focus:outline-none focus:ring-2 focus:ring-primary/20"
						/>
					</div>
				</div>
				<div
					className="max-h-56 overflow-y-auto"
					onScroll={createLoadMoreScrollHandler(activePagination)}
				>
					{isSearching && searchLoading ? (
						<div className="flex items-center justify-center py-4">
							<Loader2 className="size-4 animate-spin text-muted-foreground/50" />
						</div>
					) : (
						<>
							{displayedTasks.map((task) => (
								<DropdownMenuItem
									key={task.id}
									onClick={() => onChange(task.id)}
								>
									<span className="truncate">{taskLabel(task)}</span>
								</DropdownMenuItem>
							))}
							{displayedTasks.length === 0 && (
								<p className="px-2 py-4 text-center text-xs text-muted-foreground/50">
									{isSearching
										? t("automation.nodeConfig.trigger.noTasksFound")
										: t("automation.nodeConfig.trigger.noTasksYet")}
								</p>
							)}
						</>
					)}
					{activePagination.isLoadingMore && (
						<div className="flex items-center justify-center gap-1.5 py-2 text-xs text-muted-foreground/50">
							<Loader2 className="size-3 animate-spin" />
							{t("automation.nodeConfig.trigger.loadingMore")}
						</div>
					)}
				</div>
			</DropdownMenuContent>
		</DropdownMenu>
	);
}

// Every kind a condition leaf or action can retarget onto, relative to the
// walk's own bound task — "self" (the default, matching every automation's
// behavior before this existed) plus the four link-relation directions
// (mirroring the task-detail "Linked Tasks" section's vocabulary exactly),
// parent, children, and an explicit other task.
const TASK_TARGET_KINDS: TaskTargetKind[] = [
	"self",
	"parent",
	"children",
	"blocks",
	"is_blocked_by",
	"relates_to",
	"duplicates",
	"is_duplicated_by",
	"other",
];

// TaskTargetSelector is the shared "applies to" picker for both a
// condition leaf and an action — every action except call_api, and every
// condition leaf, can retarget itself onto a task other than the walk's
// own bound task via this same control.
function TaskTargetSelector({
	projectId,
	target,
	onChange,
	canEdit,
}: {
	projectId: string;
	target: TaskTarget | undefined;
	onChange: (target: TaskTarget | undefined) => void;
	canEdit: boolean;
}) {
	const { t } = useTranslation("projects");
	const kind = target?.kind ?? "self";
	return (
		<div className="space-y-1.5">
			<Select
				value={kind}
				onValueChange={(v) => {
					if (!v) return;
					const k = v as TaskTargetKind;
					onChange(k === "self" ? undefined : { kind: k });
				}}
				disabled={!canEdit}
			>
				<SelectTrigger className="h-7 w-full text-xs">
					<SelectValue>
						{t(`automation.nodeConfig.target.kinds.${kind}`)}
					</SelectValue>
				</SelectTrigger>
				<SelectContent>
					{TASK_TARGET_KINDS.map((k) => (
						<SelectItem key={k} value={k}>
							{t(`automation.nodeConfig.target.kinds.${k}`)}
						</SelectItem>
					))}
				</SelectContent>
			</Select>
			{kind === "other" && (
				<TargetTaskPicker
					projectId={projectId}
					value={target?.other_task_id ?? ""}
					onChange={(taskId) =>
						onChange({ kind: "other", other_task_id: taskId || undefined })
					}
					canEdit={canEdit}
				/>
			)}
		</div>
	);
}

// MatchModeSelector picks how a retargeted condition leaf's resolved tasks
// (when the target can resolve to more than one) combine into a single
// true/false. Only rendered when the leaf's target is actually multi-valued.
function MatchModeSelector({
	value,
	onChange,
	canEdit,
}: {
	value: "any" | "all" | undefined;
	onChange: (mode: "any" | "all") => void;
	canEdit: boolean;
}) {
	const { t } = useTranslation("projects");
	const mode = value ?? "any";
	return (
		<Select
			value={mode}
			onValueChange={(v) => v && onChange(v as "any" | "all")}
			disabled={!canEdit}
		>
			<SelectTrigger className="h-7 w-full text-xs">
				<SelectValue>
					{t(`automation.nodeConfig.target.matchMode.${mode}`)}
				</SelectValue>
			</SelectTrigger>
			<SelectContent>
				<SelectItem value="any">
					{t("automation.nodeConfig.target.matchMode.any")}
				</SelectItem>
				<SelectItem value="all">
					{t("automation.nodeConfig.target.matchMode.all")}
				</SelectItem>
			</SelectContent>
		</Select>
	);
}

// WebhookTokenSection generates/regenerates an api_trigger node's webhook
// secret. The raw token is shown exactly once (right after generation) and
// never fetched back afterward — mirrors the "show once" pattern used for
// user API keys (apps/web/src/routes/_authenticated/profile/api-keys.tsx).
function WebhookTokenSection({
	projectId,
	automationId,
	nodeId,
	canEdit,
}: {
	projectId: string;
	automationId: string;
	nodeId: string;
	canEdit: boolean;
}) {
	const { t } = useTranslation("projects");
	const [revealedToken, setRevealedToken] = useState<string | null>(null);
	const [copied, setCopied] = useState<"token" | "url" | null>(null);
	const [generating, setGenerating] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const webhookUrl = `${window.location.origin}/api/v1/webhooks/automations/${nodeId}`;

	async function handleGenerate() {
		setGenerating(true);
		setError(null);
		try {
			const result = await generateWebhookToken(
				projectId,
				automationId,
				nodeId,
			);
			setRevealedToken(result.token);
			setCopied(null);
		} catch {
			setError(t("automation.builder.genericError"));
		} finally {
			setGenerating(false);
		}
	}

	async function handleCopy(value: string, which: "token" | "url") {
		try {
			await navigator.clipboard.writeText(value);
			setCopied(which);
		} catch {
			// Clipboard access can fail (permissions, insecure context); the
			// user can still select-and-copy the visible text manually.
		}
	}

	return (
		<div className="space-y-2 border-t border-border/60 pt-3">
			<div className="space-y-1.5">
				<Label>{t("automation.nodeConfig.trigger.webhookUrlLabel")}</Label>
				<div className="flex items-center gap-2">
					<code className="flex-1 truncate rounded-md bg-muted px-2 py-1.5 text-xs">
						{webhookUrl}
					</code>
					<Button
						variant="outline"
						size="icon"
						className="size-7 shrink-0"
						onClick={() => handleCopy(webhookUrl, "url")}
					>
						<Copy className="size-3.5" />
					</Button>
				</div>
				{copied === "url" && (
					<p className="text-xs text-green-600">
						{t("automation.nodeConfig.trigger.copied")}
					</p>
				)}
			</div>

			<div className="space-y-1.5">
				<Label>{t("automation.nodeConfig.trigger.webhookTokenLabel")}</Label>
				{revealedToken ? (
					<>
						<div className="flex items-center gap-2">
							<code className="flex-1 truncate rounded-md bg-muted px-2 py-1.5 text-xs">
								{revealedToken}
							</code>
							<Button
								variant="outline"
								size="icon"
								className="size-7 shrink-0"
								onClick={() => handleCopy(revealedToken, "token")}
							>
								<Copy className="size-3.5" />
							</Button>
						</div>
						{copied === "token" && (
							<p className="text-xs text-green-600">
								{t("automation.nodeConfig.trigger.copied")}
							</p>
						)}
						<p className="text-xs text-destructive">
							{t("automation.nodeConfig.trigger.tokenShownOnceWarning")}
						</p>
					</>
				) : (
					<p className="text-xs text-muted-foreground">
						{t("automation.nodeConfig.trigger.noTokenYet")}
					</p>
				)}
			</div>
			{error && <p className="text-xs text-destructive">{error}</p>}
			{canEdit && (
				<Button
					type="button"
					variant="outline"
					size="sm"
					disabled={generating}
					onClick={handleGenerate}
				>
					{revealedToken
						? t("automation.nodeConfig.trigger.regenerateTokenButton")
						: t("automation.nodeConfig.trigger.generateTokenButton")}
				</Button>
			)}
		</div>
	);
}

// ── Condition config form ────────────────────────────────────────────────────
// Each branch is edited as a single leaf comparison (the common case); the
// backend/domain model supports full nested AND/OR/NOT trees for
// API/MCP-authored automations, but the canvas form intentionally keeps the
// authoring surface simple.

function ConditionConfigForm({
	config,
	projectId,
	statuses,
	members,
	customFields,
	taskTypes,
	canEdit,
	onSave,
	saving,
}: {
	config: ConditionConfig;
	projectId: string;
	statuses: TaskStatus[];
	members: ProjectMember[];
	customFields: CustomFieldDefinition[];
	taskTypes: TaskType[];
	canEdit: boolean;
	onSave: (config: Record<string, unknown>) => void;
	saving?: boolean;
}) {
	const { t } = useTranslation("projects");
	const [branches, setBranches] = useState<ConditionBranch[]>(
		config.branches ?? [],
	);

	useEffect(() => {
		setBranches(config.branches ?? []);
	}, [config.branches]);

	function updateBranch(index: number, patch: Partial<ConditionBranch>) {
		setBranches((prev) =>
			prev.map((b, i) => (i === index ? { ...b, ...patch } : b)),
		);
	}

	function updateLeaf(
		index: number,
		patch: Partial<{
			field: ConditionField;
			field_key: string;
			operator: ConditionOperator;
			value: unknown;
			target: TaskTarget | undefined;
			match_mode: "any" | "all";
		}>,
	) {
		setBranches((prev) =>
			prev.map((b, i) => {
				if (i !== index) return b;
				const current = b.tree ?? {
					field: "status_id" as ConditionField,
					operator: "equals" as ConditionOperator,
				};
				return { ...b, tree: { ...current, ...patch } };
			}),
		);
	}

	function addBranch() {
		const handle = `branch_${branches.length + 1}_${Date.now().toString(36)}`;
		setBranches((prev) => [
			...prev,
			{
				handle,
				label: "",
				tree: { field: "status_id", operator: "equals" },
			},
		]);
	}

	function removeBranch(index: number) {
		setBranches((prev) => prev.filter((_, i) => i !== index));
	}

	return (
		<div className="space-y-4">
			<div className="text-xs font-semibold text-muted-foreground">
				{t("automation.nodeConfig.condition.branchesTitle")}
			</div>
			{branches.map((branch, i) => {
				const leaf = branch.tree;
				const field = leaf?.field ?? "status_id";
				const operators = OPERATORS_BY_FIELD[field] ?? [];
				const selectedCustomField = customFields.find(
					(cf) => cf.field_key === leaf?.field_key,
				);
				return (
					<div
						// biome-ignore lint/suspicious/noArrayIndexKey: branch handles aren't stable input keys until saved
						key={i}
						className="space-y-2 rounded-lg border border-border/60 p-2.5"
					>
						<div className="flex items-center gap-2">
							<Input
								value={branch.label ?? ""}
								onChange={(e) => updateBranch(i, { label: e.target.value })}
								placeholder={t("automation.nodeConfig.condition.branchLabel")}
								disabled={!canEdit}
								className="h-7 text-xs"
							/>
							{canEdit && (
								<button
									type="button"
									onClick={() => removeBranch(i)}
									className="shrink-0 text-muted-foreground hover:text-destructive"
								>
									<Trash2 className="size-3.5" />
								</button>
							)}
						</div>
						<div className="grid grid-cols-2 gap-1.5">
							<Select
								value={field}
								onValueChange={(v) => {
									if (!v) return;
									const f = v as ConditionField;
									updateLeaf(i, {
										field: f,
										operator: (OPERATORS_BY_FIELD[f] ?? [])[0],
									});
								}}
								disabled={!canEdit}
							>
								<SelectTrigger className="h-7 text-xs">
									<SelectValue>
										{t(`automation.nodeConfig.condition.fields.${field}`)}
									</SelectValue>
								</SelectTrigger>
								<SelectContent>
									{CONDITION_FIELDS.map((f) => (
										<SelectItem key={f} value={f}>
											{t(`automation.nodeConfig.condition.fields.${f}`)}
										</SelectItem>
									))}
								</SelectContent>
							</Select>
							<Select
								value={leaf?.operator ?? operators[0]}
								onValueChange={(v) => {
									if (v) updateLeaf(i, { operator: v as ConditionOperator });
								}}
								disabled={!canEdit}
							>
								<SelectTrigger className="h-7 text-xs">
									<SelectValue />
								</SelectTrigger>
								<SelectContent>
									{operators.map((op) => (
										<SelectItem key={op} value={op}>
											{op}
										</SelectItem>
									))}
								</SelectContent>
							</Select>
						</div>
						{field === "custom_field" && (
							<Select
								value={leaf?.field_key ?? ""}
								onValueChange={(v) => updateLeaf(i, { field_key: v ?? "" })}
								disabled={!canEdit}
							>
								<SelectTrigger className="h-7 text-xs">
									<SelectValue
										placeholder={t(
											"automation.nodeConfig.action.fieldKeyLabel",
										)}
									>
										{selectedCustomField?.display_name}
									</SelectValue>
								</SelectTrigger>
								<SelectContent>
									{customFields.map((cf) => (
										<SelectItem key={cf.id} value={cf.field_key}>
											{cf.display_name}
										</SelectItem>
									))}
								</SelectContent>
							</Select>
						)}
						{leaf?.operator !== "is_empty" &&
							leaf?.operator !== "is_not_empty" &&
							(field === "status_id" ? (
								<Select
									value={(leaf?.value as string) ?? ""}
									onValueChange={(v) => updateLeaf(i, { value: v ?? "" })}
									disabled={!canEdit}
								>
									<SelectTrigger className="h-7 w-full text-xs">
										<SelectValue
											placeholder={t("automation.nodeConfig.condition.value")}
										>
											{statuses.find((s) => s.id === leaf?.value)?.name}
										</SelectValue>
									</SelectTrigger>
									<SelectContent>
										{statuses.map((s) => (
											<SelectItem key={s.id} value={s.id}>
												{s.name}
											</SelectItem>
										))}
									</SelectContent>
								</Select>
							) : field === "task_type_id" ? (
								<Select
									value={(leaf?.value as string) ?? ""}
									onValueChange={(v) => updateLeaf(i, { value: v ?? "" })}
									disabled={!canEdit}
								>
									<SelectTrigger className="h-7 w-full text-xs">
										<SelectValue
											placeholder={t("automation.nodeConfig.condition.value")}
										>
											{taskTypes.find((tt) => tt.id === leaf?.value)?.name}
										</SelectValue>
									</SelectTrigger>
									<SelectContent>
										{taskTypes.map((tt) => (
											<SelectItem key={tt.id} value={tt.id}>
												{tt.name}
											</SelectItem>
										))}
									</SelectContent>
								</Select>
							) : field === "assignee_ids" ? (
								<Select
									value={(leaf?.value as string) ?? ""}
									onValueChange={(v) => updateLeaf(i, { value: v ?? "" })}
									disabled={!canEdit}
								>
									<SelectTrigger className="h-7 w-full text-xs">
										<SelectValue
											placeholder={t("automation.nodeConfig.condition.value")}
										>
											{(() => {
												const assignee = members.find(
													(m) => m.id === leaf?.value,
												);
												return assignee?.full_name || assignee?.username;
											})()}
										</SelectValue>
									</SelectTrigger>
									<SelectContent>
										{members.map((m) => (
											<SelectItem key={m.id} value={m.id}>
												{m.full_name || m.username}
											</SelectItem>
										))}
									</SelectContent>
								</Select>
							) : field === "custom_field" &&
								selectedCustomField &&
								selectedCustomField.options.length > 0 ? (
								<Select
									value={(leaf?.value as string) ?? ""}
									onValueChange={(v) => updateLeaf(i, { value: v ?? "" })}
									disabled={!canEdit}
								>
									<SelectTrigger className="h-7 w-full text-xs">
										<SelectValue
											placeholder={t("automation.nodeConfig.condition.value")}
										>
											{leaf?.value as string}
										</SelectValue>
									</SelectTrigger>
									<SelectContent>
										{selectedCustomField.options.map((opt) => (
											<SelectItem key={opt} value={opt}>
												{opt}
											</SelectItem>
										))}
									</SelectContent>
								</Select>
							) : (
								<Input
									value={(leaf?.value as string) ?? ""}
									onChange={(e) => updateLeaf(i, { value: e.target.value })}
									placeholder={t("automation.nodeConfig.condition.value")}
									disabled={!canEdit}
									className="h-7 text-xs"
								/>
							))}
						<div className="space-y-1">
							<Label className="text-[10px] text-muted-foreground">
								{t("automation.nodeConfig.target.label")}
							</Label>
							<TaskTargetSelector
								projectId={projectId}
								target={leaf?.target}
								onChange={(target) => updateLeaf(i, { target })}
								canEdit={canEdit}
							/>
							{leaf?.target &&
								MULTI_VALUED_TARGET_KINDS.includes(leaf.target.kind) && (
									<MatchModeSelector
										value={leaf?.match_mode}
										onChange={(mode) => updateLeaf(i, { match_mode: mode })}
										canEdit={canEdit}
									/>
								)}
						</div>
					</div>
				);
			})}
			<div className="rounded-lg border border-dashed border-border/60 p-2.5 text-xs text-muted-foreground">
				{t("automation.nodeConfig.condition.elseLabel")}
			</div>
			{canEdit && (
				<>
					<Button
						variant="outline"
						size="sm"
						className="w-full gap-1.5"
						onClick={addBranch}
					>
						<Plus className="size-3.5" />
						{t("automation.nodeConfig.condition.addBranch")}
					</Button>
					<SaveButton saving={saving} onClick={() => onSave({ branches })} />
				</>
			)}
		</div>
	);
}

// ── Action config form ───────────────────────────────────────────────────────

function ActionConfigForm({
	type,
	config,
	projectId,
	statuses,
	members,
	customFields,
	canEdit,
	onSave,
	saving,
}: {
	type: string;
	config: ActionConfig;
	projectId: string;
	statuses: TaskStatus[];
	members: ProjectMember[];
	customFields: CustomFieldDefinition[];
	canEdit: boolean;
	onSave: (config: Record<string, unknown>) => void;
	saving?: boolean;
}) {
	const { t } = useTranslation("projects");
	const [target, setTarget] = useState<TaskTarget | undefined>(config.target);
	const [memberId, setMemberId] = useState(config.member_id ?? "");
	const [statusId, setStatusId] = useState(config.status_id ?? "");
	const [priorityBucket, setPriorityBucket] = useState(
		getImportanceBucket(config.importance ?? 0).toString(),
	);
	const [tag, setTag] = useState(config.tag ?? "");
	const [fieldKey, setFieldKey] = useState(config.field_key ?? "");
	const [value, setValue] = useState((config.value as string) ?? "");
	const [message, setMessage] = useState(config.message ?? "");
	const [method, setMethod] = useState(config.method ?? "GET");
	const [url, setUrl] = useState(config.url ?? "");
	const [headerRows, setHeaderRows] = useState<
		{ key: string; value: string }[]
	>(
		config.headers
			? Object.entries(config.headers).map(([key, value]) => ({
					key,
					value,
				}))
			: [],
	);
	const [body, setBody] = useState(config.body ?? "");

	if (type === "assign" || type === "trigger_ai_agent") {
		const isAiAgentAction = type === "trigger_ai_agent";
		const assignableMembers = isAiAgentAction
			? members.filter((m) => m.member_type === "agent")
			: members;
		return (
			<div className="space-y-3">
				<div className="space-y-1.5">
					<Label>
						{t(
							isAiAgentAction
								? "automation.nodeConfig.action.agentLabel"
								: "automation.nodeConfig.action.memberLabel",
						)}
					</Label>
					<Select
						value={memberId}
						onValueChange={(v) => setMemberId(v ?? "")}
						disabled={!canEdit}
					>
						<SelectTrigger className="w-full">
							<SelectValue>
								{(() => {
									const selected = assignableMembers.find(
										(m) => m.id === memberId,
									);
									return selected?.full_name || selected?.username;
								})()}
							</SelectValue>
						</SelectTrigger>
						<SelectContent>
							{assignableMembers.map((m) => (
								<SelectItem key={m.id} value={m.id}>
									{m.full_name || m.username}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				</div>
				{type === "trigger_ai_agent" && (
					<div className="space-y-1.5">
						<Label>{t("automation.nodeConfig.action.messageLabel")}</Label>
						<Textarea
							value={message}
							onChange={(e) => setMessage(e.target.value)}
							placeholder={t("automation.nodeConfig.action.messagePlaceholder")}
							rows={4}
							disabled={!canEdit}
						/>
					</div>
				)}
				<div className="space-y-1">
					<Label className="text-[10px] text-muted-foreground">
						{t("automation.nodeConfig.target.label")}
					</Label>
					<TaskTargetSelector
						projectId={projectId}
						target={target}
						onChange={setTarget}
						canEdit={canEdit}
					/>
				</div>
				{canEdit && (
					<SaveButton
						saving={saving}
						disabled={!memberId}
						onClick={() =>
							onSave(
								type === "trigger_ai_agent"
									? { member_id: memberId, message, target }
									: { member_id: memberId, target },
							)
						}
					/>
				)}
			</div>
		);
	}

	if (type === "set_status") {
		return (
			<div className="space-y-3">
				<div className="space-y-1.5">
					<Label>{t("automation.nodeConfig.action.statusLabel")}</Label>
					<Select
						value={statusId}
						onValueChange={(v) => setStatusId(v ?? "")}
						disabled={!canEdit}
					>
						<SelectTrigger className="w-full">
							<SelectValue>
								{statuses.find((s) => s.id === statusId)?.name}
							</SelectValue>
						</SelectTrigger>
						<SelectContent>
							{statuses.map((s) => (
								<SelectItem key={s.id} value={s.id}>
									{s.name}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				</div>
				<div className="space-y-1">
					<Label className="text-[10px] text-muted-foreground">
						{t("automation.nodeConfig.target.label")}
					</Label>
					<TaskTargetSelector
						projectId={projectId}
						target={target}
						onChange={setTarget}
						canEdit={canEdit}
					/>
				</div>
				{canEdit && (
					<SaveButton
						saving={saving}
						disabled={!statusId}
						onClick={() => onSave({ status_id: statusId, target })}
					/>
				)}
			</div>
		);
	}

	if (type === "set_priority") {
		return (
			<div className="space-y-3">
				<div className="space-y-1.5">
					<Label>{t("automation.nodeConfig.action.importanceLabel")}</Label>
					<Select
						value={priorityBucket}
						onValueChange={(v) => v && setPriorityBucket(v)}
						disabled={!canEdit}
					>
						<SelectTrigger className="w-full">
							<SelectValue>
								{(() => {
									const level = PRIORITY_LEVELS.find(
										(l) => l.value.toString() === priorityBucket,
									);
									return level ? t(level.labelKey) : undefined;
								})()}
							</SelectValue>
						</SelectTrigger>
						<SelectContent>
							{PRIORITY_LEVELS.map((level) => (
								<SelectItem key={level.value} value={level.value.toString()}>
									{t(level.labelKey)}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				</div>
				<div className="space-y-1">
					<Label className="text-[10px] text-muted-foreground">
						{t("automation.nodeConfig.target.label")}
					</Label>
					<TaskTargetSelector
						projectId={projectId}
						target={target}
						onChange={setTarget}
						canEdit={canEdit}
					/>
				</div>
				{canEdit && (
					<SaveButton
						saving={saving}
						onClick={() =>
							onSave({
								importance:
									IMPORTANCE_BUCKET_VALUES[Number(priorityBucket)] ?? 0,
								target,
							})
						}
					/>
				)}
			</div>
		);
	}

	if (type === "add_tag") {
		return (
			<div className="space-y-3">
				<div className="space-y-1.5">
					<Label>{t("automation.nodeConfig.action.tagLabel")}</Label>
					<Input
						value={tag}
						onChange={(e) => setTag(e.target.value)}
						disabled={!canEdit}
					/>
				</div>
				<div className="space-y-1">
					<Label className="text-[10px] text-muted-foreground">
						{t("automation.nodeConfig.target.label")}
					</Label>
					<TaskTargetSelector
						projectId={projectId}
						target={target}
						onChange={setTarget}
						canEdit={canEdit}
					/>
				</div>
				{canEdit && (
					<SaveButton
						saving={saving}
						disabled={!tag.trim()}
						onClick={() => onSave({ tag: tag.trim(), target })}
					/>
				)}
			</div>
		);
	}

	if (type === "call_api") {
		const headersObject = () => {
			const obj: Record<string, string> = {};
			for (const row of headerRows) {
				if (row.key.trim()) obj[row.key.trim()] = row.value;
			}
			return obj;
		};
		return (
			<div className="space-y-3">
				<div className="grid grid-cols-[6rem_1fr] gap-1.5">
					<div className="space-y-1.5">
						<Label>{t("automation.nodeConfig.action.methodLabel")}</Label>
						<Select
							value={method}
							onValueChange={(v) => v && setMethod(v)}
							disabled={!canEdit}
						>
							<SelectTrigger className="w-full">
								<SelectValue>{method}</SelectValue>
							</SelectTrigger>
							<SelectContent>
								{["GET", "POST", "PUT", "PATCH", "DELETE"].map((m) => (
									<SelectItem key={m} value={m}>
										{m}
									</SelectItem>
								))}
							</SelectContent>
						</Select>
					</div>
					<div className="space-y-1.5">
						<Label>{t("automation.nodeConfig.action.urlLabel")}</Label>
						<Input
							value={url}
							onChange={(e) => setUrl(e.target.value)}
							placeholder={t("automation.nodeConfig.action.urlPlaceholder")}
							disabled={!canEdit}
						/>
					</div>
				</div>
				<div className="space-y-1.5">
					<Label>{t("automation.nodeConfig.action.headersLabel")}</Label>
					{headerRows.map((row, i) => (
						<div
							// biome-ignore lint/suspicious/noArrayIndexKey: header rows aren't stable input keys until saved
							key={i}
							className="flex items-center gap-1.5"
						>
							<Input
								value={row.key}
								onChange={(e) =>
									setHeaderRows((prev) =>
										prev.map((r, idx) =>
											idx === i ? { ...r, key: e.target.value } : r,
										),
									)
								}
								placeholder={t(
									"automation.nodeConfig.action.headerKeyPlaceholder",
								)}
								disabled={!canEdit}
								className="h-7 text-xs"
							/>
							<Input
								value={row.value}
								onChange={(e) =>
									setHeaderRows((prev) =>
										prev.map((r, idx) =>
											idx === i ? { ...r, value: e.target.value } : r,
										),
									)
								}
								placeholder={t(
									"automation.nodeConfig.action.headerValuePlaceholder",
								)}
								disabled={!canEdit}
								className="h-7 text-xs"
							/>
							{canEdit && (
								<button
									type="button"
									onClick={() =>
										setHeaderRows((prev) => prev.filter((_, idx) => idx !== i))
									}
									className="shrink-0 text-muted-foreground hover:text-destructive"
								>
									<Trash2 className="size-3.5" />
								</button>
							)}
						</div>
					))}
					{canEdit && (
						<Button
							variant="outline"
							size="sm"
							className="w-full gap-1.5"
							onClick={() =>
								setHeaderRows((prev) => [...prev, { key: "", value: "" }])
							}
						>
							<Plus className="size-3.5" />
							{t("automation.nodeConfig.action.addHeader")}
						</Button>
					)}
				</div>
				<div className="space-y-1.5">
					<Label>{t("automation.nodeConfig.action.bodyLabel")}</Label>
					<Textarea
						value={body}
						onChange={(e) => setBody(e.target.value)}
						rows={4}
						className="font-mono text-xs"
						disabled={!canEdit}
					/>
				</div>
				{canEdit && (
					<SaveButton
						saving={saving}
						disabled={!method || !url.trim()}
						onClick={() =>
							onSave({
								method,
								url: url.trim(),
								headers: headersObject(),
								body,
							})
						}
					/>
				)}
			</div>
		);
	}

	// set_custom_field
	const selectedField = customFields.find((cf) => cf.field_key === fieldKey);
	return (
		<div className="space-y-3">
			<div className="space-y-1.5">
				<Label>{t("automation.nodeConfig.action.fieldKeyLabel")}</Label>
				<Select
					value={fieldKey}
					onValueChange={(v) => setFieldKey(v ?? "")}
					disabled={!canEdit}
				>
					<SelectTrigger className="w-full">
						<SelectValue>{selectedField?.display_name}</SelectValue>
					</SelectTrigger>
					<SelectContent>
						{customFields.map((cf) => (
							<SelectItem key={cf.id} value={cf.field_key}>
								{cf.display_name}
							</SelectItem>
						))}
					</SelectContent>
				</Select>
			</div>
			<div className="space-y-1.5">
				<Label>{t("automation.nodeConfig.action.valueLabel")}</Label>
				{selectedField && selectedField.options.length > 0 ? (
					<Select
						value={value}
						onValueChange={(v) => setValue(v ?? "")}
						disabled={!canEdit}
					>
						<SelectTrigger className="w-full">
							<SelectValue>{value}</SelectValue>
						</SelectTrigger>
						<SelectContent>
							{selectedField.options.map((opt) => (
								<SelectItem key={opt} value={opt}>
									{opt}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				) : (
					<Input
						value={value}
						onChange={(e) => setValue(e.target.value)}
						disabled={!canEdit}
					/>
				)}
			</div>
			<div className="space-y-1">
				<Label className="text-[10px] text-muted-foreground">
					{t("automation.nodeConfig.target.label")}
				</Label>
				<TaskTargetSelector
					projectId={projectId}
					target={target}
					onChange={setTarget}
					canEdit={canEdit}
				/>
			</div>
			{canEdit && (
				<SaveButton
					saving={saving}
					disabled={!fieldKey}
					onClick={() => onSave({ field_key: fieldKey, value, target })}
				/>
			)}
		</div>
	);
}

function SaveButton({
	onClick,
	saving,
	disabled,
}: {
	onClick: () => void;
	saving?: boolean;
	disabled?: boolean;
}) {
	const { t } = useTranslation("projects");
	return (
		<Button
			size="sm"
			className="w-full"
			disabled={saving || disabled}
			onClick={onClick}
		>
			{t("automation.nodeConfig.save")}
		</Button>
	);
}
