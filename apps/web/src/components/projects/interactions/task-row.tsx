import type { TFunction } from "i18next";
import {
	Check,
	ChevronRight,
	GripVertical,
	Layers,
	Link,
	ListChecks,
	Search,
	User,
} from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { EntityAvatar } from "@/components/shared/entity-avatar";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import { Spinner } from "@/components/ui/spinner";
import { useJevEnabled } from "@/hooks/use-jev-enabled";
import { useIsMobile } from "@/hooks/use-mobile";
import {
	customFieldBadgeStyle,
	getCustomFieldOptionColor,
} from "@/lib/custom-field-colors";
import { formatDate } from "@/lib/format-date";
import type { Task } from "@/lib/interaction-api";
import { useMemberDirectory } from "@/lib/member-directory";
import {
	type CustomFieldDefinition,
	findEpicType,
	type TaskStatus,
	type TaskType,
} from "@/lib/project-api";
import { memberAvatar } from "@/lib/provider-logos";
import { useHoveredTaskStore } from "@/lib/shortcuts/hovered-task-store";
import { cn } from "@/lib/utils";
import { AssigneePicker } from "./assignee-picker";
import {
	getPriority,
	IMPORTANCE_BUCKET_VALUES,
	PRIORITY_LEVELS,
} from "./priority";
import { TaskTypeSelector } from "./task-type-selector";
import { useEpicSearch } from "./use-epic-search";
import {
	createEpicScrollHandler,
	DEFAULT_VISIBLE_FIELDS,
	type EpicsPagination,
	isAutoAssignPending,
	type TaskFieldUpdate,
} from "./view-utils";

// ── Column config ──────────────────────────────────────────────────────────────

interface ColConfig {
	className: string;
	headerLabel: string;
	responsive?: boolean; // hide on xs screens
}

export function getRowColConfig(
	fieldKey: string,
	customFields: CustomFieldDefinition[],
	t: TFunction<"projects">,
): ColConfig {
	switch (fieldKey) {
		case "type":
			return {
				className: "w-28 shrink-0",
				headerLabel: t("board.taskRow.columnHeaders.type"),
			};
		case "importance":
			return {
				className: "w-20 shrink-0",
				headerLabel: t("board.taskRow.columnHeaders.importance"),
				responsive: true,
			};
		case "story_points":
			return {
				className: "w-14 shrink-0",
				headerLabel: t("board.taskRow.columnHeaders.storyPoints"),
				responsive: true,
			};
		case "status":
			return {
				className: "w-32 shrink-0",
				headerLabel: t("board.taskRow.columnHeaders.status"),
				responsive: true,
			};
		case "assignee":
			return {
				className: "w-20 shrink-0",
				headerLabel: t("board.taskRow.columnHeaders.assignee"),
			};
		case "reporter":
			return {
				className: "w-20 shrink-0",
				headerLabel: t("board.taskRow.columnHeaders.reporter"),
				responsive: true,
			};
		case "start_date":
			return {
				className: "w-24 shrink-0",
				headerLabel: t("board.taskRow.columnHeaders.startDate"),
				responsive: true,
			};
		case "due_date":
			return {
				className: "w-24 shrink-0",
				headerLabel: t("board.taskRow.columnHeaders.dueDate"),
				responsive: true,
			};
		case "epic":
			return {
				className: "w-32 shrink-0",
				headerLabel: t("board.taskRow.columnHeaders.epic"),
				responsive: true,
			};
		case "tags":
			return {
				className: "w-40 shrink-0",
				headerLabel: t("board.taskRow.columnHeaders.tags"),
				responsive: true,
			};
		case "created":
			return {
				className: "w-24 shrink-0",
				headerLabel: t("board.taskRow.columnHeaders.created"),
				responsive: true,
			};
		default: {
			const cf = customFields.find((f) => f.field_key === fieldKey);
			const label = cf?.display_name ?? fieldKey;
			const width =
				cf?.field_type === "boolean"
					? "w-12"
					: cf?.field_type === "number"
						? "w-16"
						: "w-24";
			return {
				className: `${width} shrink-0`,
				headerLabel: label,
				responsive: true,
			};
		}
	}
}

// ── Props ──────────────────────────────────────────────────────────────────────

interface TaskRowProps {
	task: Task;
	taskIdPrefix?: string;
	statuses: TaskStatus[];
	taskTypes: TaskType[];
	epics?: Task[];
	/** Load-more state for the epic picker Popover — omitted when the caller
	 * doesn't paginate epics (e.g. fewer than one page's worth). */
	epicsPagination?: EpicsPagination;
	customFields?: CustomFieldDefinition[];
	visibleFields?: string[];
	onClick?: () => void;
	showDragHandle?: boolean;
	isDragging?: boolean;
	canEdit?: boolean;
	onUpdateTaskField?: (taskId: string, update: TaskFieldUpdate) => void;
	/** Keyboard-shortcut hooks (Mod+Backspace / Mod+Left / Mod+Right) — fire
	 * only while this row is hovered. `onMoveLeft`/`onMoveRight` are omitted
	 * by the caller when there's no adjacent group. */
	onDelete?: (taskId: string) => void;
	onMoveLeft?: () => void;
	onMoveRight?: () => void;
	/** Nesting level of a subtask row under its parent (0 = top level). */
	depth?: number;
	/** Present when the view nests subtasks: reserves the chevron slot so ids
	 *  stay aligned, and drives the expand toggle + progress chip. */
	subtasks?: {
		expanded: boolean;
		onToggle: () => void;
	};
}

/** Indentation stops growing past this depth so deep chains stay readable. */
const MAX_INDENT_DEPTH = 3;
/** Child rows have no drag handle; this keeps their ids indented past the
 *  parent's id (handle 14px + gap 12px). */
const DRAG_HANDLE_WIDTH = 26;

export function TaskRow({
	task,
	taskIdPrefix = "",
	statuses,
	taskTypes,
	epics = [],
	epicsPagination,
	customFields = [],
	visibleFields = DEFAULT_VISIBLE_FIELDS,
	onClick,
	showDragHandle,
	isDragging,
	canEdit,
	onUpdateTaskField,
	onDelete,
	onMoveLeft,
	onMoveRight,
	depth = 0,
	subtasks,
}: TaskRowProps) {
	const { t } = useTranslation("projects");
	const isMobile = useIsMobile();
	// Touch screens have no drag handle and little width, so nest tighter.
	const indentStep = isMobile ? 12 : 20;
	const handleOffset = isMobile ? 0 : DRAG_HANDLE_WIDTH;
	const subtaskCount = task.subtask_count ?? 0;
	const subtaskDone = task.subtask_done_count ?? 0;
	const status = statuses.find((s) => s.id === task.status_id);
	const jevEnabled = useJevEnabled(task.project_id);
	const getMember = useMemberDirectory(task.project_id);
	const isAutoAssign = isAutoAssignPending(task);
	const [isHovered, setIsHovered] = useState(false);
	const [epicOpen, setEpicOpen] = useState(false);
	const epicTypeId = findEpicType(taskTypes)?.id;
	const {
		search: epicSearch,
		setSearch: setEpicSearch,
		isSearching: isEpicSearching,
		results: epicSearchResults,
		isLoading: epicSearchLoading,
		pagination: epicSearchPagination,
	} = useEpicSearch(task.project_id, epicTypeId, epicOpen);

	// Keyboard shortcuts act on whichever task is hovered — see
	// hovered-task-store.ts and lib/shortcuts/provider.tsx.
	useEffect(() => {
		if (!isHovered) return;
		useHoveredTaskStore.getState().setActive({
			taskId: task.id,
			canEdit: !!canEdit,
			open: () => onClick?.(),
			openEpic: () => setEpicOpen(true),
			moveLeft: onMoveLeft,
			moveRight: onMoveRight,
			delete: () => onDelete?.(task.id),
		});
		return () => useHoveredTaskStore.getState().clearActive(task.id);
	}, [isHovered, canEdit, task.id, onClick, onMoveLeft, onMoveRight, onDelete]);

	/** Renders a single cell value for the given field key. */
	const renderCell = (fieldKey: string) => {
		const col = getRowColConfig(fieldKey, customFields, t);
		const responsiveClass = col.responsive ? "hidden sm:flex" : "flex";
		const canEditField = !!(canEdit && onUpdateTaskField);

		switch (fieldKey) {
			case "type":
				return canEditField ? (
					// biome-ignore lint/a11y/noStaticElementInteractions: cell container stops propagation; inner controls are the interactive elements
					<div
						key="type"
						className={cn(col.className, responsiveClass, "items-center")}
						onClick={(e) => e.stopPropagation()}
						onKeyDown={(e) => e.stopPropagation()}
					>
						<TaskTypeSelector
							taskTypes={taskTypes}
							value={task.task_type_id}
							onChange={(id) =>
								onUpdateTaskField(task.id, { task_type_id: id })
							}
						/>
					</div>
				) : (
					<div
						key="type"
						className={cn(col.className, responsiveClass, "items-center")}
					>
						<TaskTypeSelector
							taskTypes={taskTypes}
							value={task.task_type_id}
							canEdit={false}
						/>
					</div>
				);

			case "importance":
				return canEditField ? (
					// biome-ignore lint/a11y/noStaticElementInteractions: cell container stops propagation; inner controls are the interactive elements
					<div
						key="importance"
						className={cn(col.className, responsiveClass, "items-center gap-1")}
						onClick={(e) => e.stopPropagation()}
						onKeyDown={(e) => e.stopPropagation()}
					>
						<DropdownMenu>
							<DropdownMenuTrigger className="flex items-center gap-1 hover:opacity-80 transition-opacity cursor-pointer">
								{(() => {
									const p = getPriority(task.importance);
									return task.importance > 0 ? (
										<>
											<span
												className="size-2 rounded-full shrink-0"
												style={{ background: p.color }}
											/>
											<span
												className="text-xs font-medium truncate"
												style={{ color: p.color }}
											>
												{t(p.labelKey)}
											</span>
										</>
									) : (
										<span className="text-xs text-muted-foreground/50">—</span>
									);
								})()}
							</DropdownMenuTrigger>
							<DropdownMenuContent align="start">
								{PRIORITY_LEVELS.map((p) => (
									<DropdownMenuItem
										key={p.value}
										onClick={() =>
											onUpdateTaskField(task.id, {
												importance: IMPORTANCE_BUCKET_VALUES[p.value] ?? 0,
											})
										}
									>
										<span
											className="size-2 rounded-full shrink-0 mr-2"
											style={{ background: p.color }}
										/>
										<span style={{ color: p.color }}>{t(p.labelKey)}</span>
										{getPriority(task.importance).labelKey === p.labelKey &&
											task.importance > 0 === p.value > 0 && (
												<Check className="size-3.5 text-primary ml-auto" />
											)}
									</DropdownMenuItem>
								))}
							</DropdownMenuContent>
						</DropdownMenu>
					</div>
				) : (
					<div
						key="importance"
						className={cn(col.className, responsiveClass, "items-center gap-1")}
					>
						{(() => {
							const p = getPriority(task.importance);
							return task.importance > 0 ? (
								<>
									<span
										className="size-2 rounded-full shrink-0"
										style={{ background: p.color }}
									/>
									<span
										className="text-xs font-medium truncate"
										style={{ color: p.color }}
									>
										{t(p.labelKey)}
									</span>
								</>
							) : (
								<span className="text-xs text-muted-foreground/50">—</span>
							);
						})()}
					</div>
				);

			case "status":
				return canEditField && statuses.length > 0 ? (
					// biome-ignore lint/a11y/noStaticElementInteractions: cell container stops propagation; inner controls are the interactive elements
					<div
						key="status"
						className={cn(col.className, responsiveClass, "items-center")}
						onClick={(e) => e.stopPropagation()}
						onKeyDown={(e) => e.stopPropagation()}
					>
						<DropdownMenu>
							<DropdownMenuTrigger className="inline-flex items-center gap-1.5 rounded-full border border-border/40 bg-muted/40 px-2.5 py-0.5 text-xs font-semibold text-muted-foreground tracking-wide hover:opacity-80 transition-opacity truncate max-w-full cursor-pointer">
								{status ? (
									<>
										<span
											className="size-1.5 rounded-full shrink-0"
											style={{
												background:
													status.color ?? "oklch(var(--muted-foreground))",
											}}
										/>
										{status.name}
									</>
								) : (
									<span className="text-xs text-muted-foreground/50">—</span>
								)}
							</DropdownMenuTrigger>
							<DropdownMenuContent align="start">
								{statuses.map((s) => (
									<DropdownMenuItem
										key={s.id}
										onClick={() =>
											onUpdateTaskField(task.id, { status_id: s.id })
										}
									>
										<span
											className="size-2 rounded-full shrink-0 mr-2"
											style={{ background: s.color ?? undefined }}
										/>
										{s.name}
										{s.id === task.status_id && (
											<Check className="size-3.5 text-primary ml-auto" />
										)}
									</DropdownMenuItem>
								))}
							</DropdownMenuContent>
						</DropdownMenu>
					</div>
				) : (
					<div
						key="status"
						className={cn(col.className, responsiveClass, "items-center")}
					>
						{status ? (
							<span className="inline-flex items-center gap-1.5 rounded-full border border-border/40 bg-muted/40 px-2.5 py-0.5 text-xs font-semibold text-muted-foreground tracking-wide whitespace-nowrap max-w-full">
								<span
									className="size-1.5 rounded-full shrink-0"
									style={{
										background:
											status.color ?? "oklch(var(--muted-foreground))",
										boxShadow: status.color
											? `0 0 4px ${status.color}40`
											: undefined,
									}}
								/>
								<span className="truncate">{status.name}</span>
							</span>
						) : (
							<span className="text-xs text-muted-foreground/50">—</span>
						)}
					</div>
				);

			case "assignee":
				return (
					// biome-ignore lint/a11y/noStaticElementInteractions: cell container stops propagation; inner controls are the interactive elements
					<div
						key="assignee"
						className={cn(col.className, "flex items-center justify-center")}
						onClick={(e) => e.stopPropagation()}
						onKeyDown={(e) => e.stopPropagation()}
					>
						<AssigneePicker
							projectId={task.project_id}
							assigneeIds={task.assignee_ids ?? []}
							isAutoAssign={isAutoAssign}
							autoEnabled={jevEnabled}
							canEdit={canEditField}
							size="md"
							onChange={(patch) => onUpdateTaskField?.(task.id, patch)}
						/>
					</div>
				);

			case "reporter": {
				const reporter = task.reporter_id
					? getMember(task.reporter_id)
					: undefined;
				return (
					<div
						key="reporter"
						className={cn(
							col.className,
							responsiveClass,
							"items-center justify-center",
						)}
					>
						<EntityAvatar
							{...(reporter ? memberAvatar(reporter) : { name: "" })}
							fallback={<User className="size-3" />}
							size="sm"
						/>
					</div>
				);
			}

			case "start_date":
				return (
					<div
						key="start_date"
						className={cn(col.className, responsiveClass, "items-center")}
					>
						<span className="text-xs text-muted-foreground/70 truncate">
							{task.start_date
								? formatDate(
										task.start_date,
										{ month: "short", day: "numeric" },
										{ dateOnly: true },
									)
								: "—"}
						</span>
					</div>
				);

			case "due_date":
				return (
					<div
						key="due_date"
						className={cn(col.className, responsiveClass, "items-center")}
					>
						<span className="text-xs text-muted-foreground/70 truncate">
							{task.due_date
								? formatDate(
										task.due_date,
										{ month: "short", day: "numeric" },
										{ dateOnly: true },
									)
								: "—"}
						</span>
					</div>
				);

			case "story_points":
				return (
					<div
						key="story_points"
						className={cn(
							col.className,
							responsiveClass,
							"items-center justify-center",
						)}
					>
						{task.story_points != null ? (
							<span className="text-xs font-medium tabular-nums">
								{task.story_points}
							</span>
						) : (
							<span className="text-xs text-muted-foreground/50">—</span>
						)}
					</div>
				);

			case "created":
				return (
					<div
						key="created"
						className={cn(col.className, responsiveClass, "items-center")}
					>
						<span className="text-xs text-muted-foreground/50 truncate">
							{formatDate(task.created_at, { month: "short", day: "numeric" })}
						</span>
					</div>
				);

			case "tags":
				return (
					<div
						key="tags"
						className={cn(col.className, responsiveClass, "items-center")}
					>
						{task.tags && task.tags.length > 0 ? (
							<span className="inline-flex gap-1 flex-wrap">
								{task.tags.map((tag) => (
									<span
										key={tag}
										className="inline-flex items-center rounded-full bg-primary/10 px-1.5 py-0.5 text-xs font-medium text-primary/80"
									>
										{tag}
									</span>
								))}
							</span>
						) : (
							<span className="text-xs text-muted-foreground/40">—</span>
						)}
					</div>
				);

			case "epic": {
				const epic = task.parent_task_id
					? epics.find((e) => e.id === task.parent_task_id)
					: undefined;
				const displayedEpics = isEpicSearching ? epicSearchResults : epics;
				const activeEpicsPagination = isEpicSearching
					? epicSearchPagination
					: epicsPagination;
				return canEditField ? (
					// biome-ignore lint/a11y/noStaticElementInteractions: cell container stops propagation; inner controls are the interactive elements
					<div
						key="epic"
						className={cn(col.className, responsiveClass, "items-center")}
						onClick={(e) => e.stopPropagation()}
						onKeyDown={(e) => e.stopPropagation()}
					>
						<Popover open={epicOpen} onOpenChange={setEpicOpen}>
							<PopoverTrigger
								type="button"
								className="inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-xs font-medium border border-violet-500/30 bg-violet-500/10 text-violet-600 dark:text-violet-400 hover:opacity-80 transition-opacity truncate max-w-full"
							>
								{epic ? (
									<>
										<Layers className="size-3 shrink-0 opacity-70" />
										<span className="truncate">{epic.title}</span>
									</>
								) : (
									<span className="text-muted-foreground/40">—</span>
								)}
							</PopoverTrigger>
							<PopoverContent
								className="w-64 p-1.5 rounded-xl border border-border/40 shadow-lg"
								align="start"
							>
								<div className="relative mb-1">
									<Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground/50" />
									<input
										type="text"
										value={epicSearch}
										onChange={(e) => setEpicSearch(e.target.value)}
										placeholder={t("epicPicker.searchPlaceholder")}
										className="w-full rounded-lg border border-border/30 bg-muted/25 py-1.5 pr-2 pl-8 text-sm placeholder:text-muted-foreground/50 transition-all duration-150 focus:border-primary/40 focus:outline-none focus:ring-2 focus:ring-primary/20"
									/>
								</div>
								<div
									className="max-h-64 overflow-y-auto"
									onScroll={createEpicScrollHandler(activeEpicsPagination)}
								>
									<button
										type="button"
										className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-sm text-muted-foreground hover:bg-muted/60 transition-colors duration-100"
										onClick={() =>
											onUpdateTaskField(task.id, { parent_task_id: null })
										}
									>
										<span className="flex-1 text-left">
											{t("board.taskRow.noEpic")}
										</span>
										{!task.parent_task_id && (
											<Check className="size-3.5 text-primary" />
										)}
									</button>
									{isEpicSearching && epicSearchLoading ? (
										<div className="flex items-center justify-center py-4">
											<Spinner
												aria-hidden="true"
												className="text-muted-foreground/50"
											/>
										</div>
									) : (
										<>
											{displayedEpics.map((e) => (
												<button
													key={e.id}
													type="button"
													className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-sm hover:bg-muted/60 transition-colors duration-100"
													onClick={() =>
														onUpdateTaskField(task.id, {
															parent_task_id: e.id,
														})
													}
												>
													<Layers className="size-3.5 shrink-0 text-violet-500 opacity-70" />
													<span className="flex-1 text-left truncate">
														{e.title}
													</span>
													{e.id === task.parent_task_id && (
														<Check className="size-3.5 text-primary" />
													)}
												</button>
											))}
											{displayedEpics.length === 0 && (
												<p className="px-3 py-4 text-center text-xs text-muted-foreground/50">
													{isEpicSearching
														? t("epicPicker.noEpicsFound")
														: t("epicPicker.noEpicsYet")}
												</p>
											)}
										</>
									)}
									{activeEpicsPagination?.isLoadingMore && (
										<div className="flex items-center justify-center gap-1.5 py-2 text-xs text-muted-foreground/50">
											<Spinner aria-hidden="true" className="size-3" />
											{t("epicPicker.loadingMore")}
										</div>
									)}
								</div>
							</PopoverContent>
						</Popover>
					</div>
				) : (
					<div
						key="epic"
						className={cn(col.className, responsiveClass, "items-center")}
					>
						{epic ? (
							<span className="inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-xs font-medium border border-violet-500/30 bg-violet-500/10 text-violet-600 dark:text-violet-400 truncate max-w-full">
								<Layers className="size-3 shrink-0 opacity-70" />
								<span className="truncate">{epic.title}</span>
							</span>
						) : (
							<span className="text-xs text-muted-foreground/40">—</span>
						)}
					</div>
				);
			}

			default: {
				// Custom field
				const cf = customFields.find((f) => f.field_key === fieldKey);
				if (!cf) return null;
				const val = task.custom_fields[cf.field_key];

				const renderValue = () => {
					if (val === null || val === undefined || val === "")
						return <span className="text-xs text-muted-foreground/40">—</span>;
					switch (cf.field_type) {
						case "boolean":
							return val ? (
								<Check className="size-3.5 text-primary" />
							) : (
								<span className="text-xs text-muted-foreground/40">—</span>
							);
						case "number":
							return (
								<span className="text-xs font-medium text-foreground/80">
									{String(val)}
								</span>
							);
						case "date":
							return (
								<span className="text-xs text-muted-foreground/70">
									{formatDate(
										String(val),
										{ month: "short", day: "numeric" },
										{ dateOnly: true },
									)}
								</span>
							);
						case "select": {
							const color = getCustomFieldOptionColor(cf.options, String(val));
							return (
								<span
									className={cn(
										"inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium truncate max-w-full",
										!color && "bg-primary/10 text-primary/80",
									)}
									style={customFieldBadgeStyle(color)}
								>
									{String(val)}
								</span>
							);
						}
						case "multi_select": {
							const arr = Array.isArray(val)
								? (val as string[])
								: [String(val)];
							return (
								<span className="inline-flex gap-1 flex-wrap">
									{arr.map((v) => {
										const color = getCustomFieldOptionColor(cf.options, v);
										return (
											<span
												key={v}
												className={cn(
													"inline-flex items-center rounded-full px-1.5 py-0.5 text-xs font-medium",
													!color && "bg-primary/10 text-primary/80",
												)}
												style={customFieldBadgeStyle(color)}
											>
												{v}
											</span>
										);
									})}
								</span>
							);
						}
						case "url":
							return <Link className="size-3.5 text-primary/60" />;
						default:
							return (
								<span className="text-xs text-foreground/70 truncate max-w-full">
									{String(val)}
								</span>
							);
					}
				};

				return (
					<div
						key={fieldKey}
						className={cn(col.className, responsiveClass, "items-center")}
					>
						{renderValue()}
					</div>
				);
			}
		}
	};

	return (
		// biome-ignore lint/a11y/noStaticElementInteractions: draggable list row with click; converting to button breaks drag-and-drop
		// biome-ignore lint/a11y/useKeyWithClickEvents: drag-and-drop row; keyboard nav handled by parent
		<div
			onClick={onClick}
			onMouseEnter={() => setIsHovered(true)}
			onMouseLeave={() => setIsHovered(false)}
			className={cn(
				"group relative flex items-center gap-2 px-4 py-2.5 cursor-pointer sm:gap-3",
				"hover:bg-muted/30 transition-colors duration-150 border-b border-border/20 last:border-0",
				isDragging && "opacity-40 bg-muted/20",
				depth > 0 && "bg-muted/10",
			)}
			style={
				depth > 0
					? {
							paddingLeft: `${16 + handleOffset + Math.min(depth, MAX_INDENT_DEPTH) * indentStep}px`,
						}
					: undefined
			}
		>
			{depth > 0 && (
				<span
					aria-hidden
					className="absolute inset-y-0 w-px bg-border/50"
					style={{
						left: `${16 + handleOffset + (Math.min(depth, MAX_INDENT_DEPTH) - 1) * indentStep + 8}px`,
					}}
				/>
			)}
			{subtasks &&
				(subtaskCount > 0 ? (
					<button
						type="button"
						aria-expanded={subtasks.expanded}
						aria-label={t(
							subtasks.expanded
								? "board.subtasks.collapse"
								: "board.subtasks.expand",
							{ n: subtaskCount },
						)}
						onClick={(e) => {
							e.stopPropagation();
							subtasks.onToggle();
						}}
						onKeyDown={(e) => e.stopPropagation()}
						className="-ml-1.5 -mr-1.5 flex size-6 shrink-0 items-center justify-center rounded-md text-muted-foreground/70 hover:bg-muted/60 hover:text-foreground focus-visible:outline-2 focus-visible:outline-primary"
					>
						<ChevronRight
							className={cn(
								"size-3.5 transition-transform duration-150 motion-reduce:transition-none",
								subtasks.expanded && "rotate-90",
							)}
						/>
					</button>
				) : (
					<span aria-hidden className="-ml-1.5 -mr-1.5 size-6 shrink-0" />
				))}
			{showDragHandle && (
				<GripVertical className="size-3.5 shrink-0 -ml-1.5 text-muted-foreground/30 group-hover:text-muted-foreground/70 cursor-grab opacity-0 group-hover:opacity-100 transition-opacity duration-200" />
			)}

			{/* Task ID — separate fixed-width column left of title */}
			<span className="w-14 shrink-0 font-[JetBrains_Mono,monospace] text-xs font-semibold text-muted-foreground/55 tracking-wide sm:w-20">
				{taskIdPrefix
					? `${taskIdPrefix}-${task.task_number}`
					: task.task_number > 0
						? `#${task.task_number}`
						: ""}
			</span>

			{/* Title */}
			<span className="flex min-w-0 flex-1 flex-wrap items-center gap-x-2 gap-y-1">
				<span className="min-w-12 truncate text-sm font-medium text-foreground">
					{task.title}
				</span>
				{subtasks && subtaskCount > 0 && (
					<span
						title={t("board.subtasks.progress", {
							done: subtaskDone,
							n: subtaskCount,
						})}
						className={cn(
							"inline-flex shrink-0 items-center gap-1 rounded-md bg-muted/50 px-1.5 py-0.5 text-[11px] font-semibold tabular-nums text-muted-foreground",
							subtaskDone === subtaskCount && "text-success",
						)}
					>
						<ListChecks className="hidden size-3 sm:block" aria-hidden />
						{subtaskDone}/{subtaskCount}
					</span>
				)}
			</span>

			{/* Dynamic field columns */}
			{visibleFields.map(renderCell)}
		</div>
	);
}
