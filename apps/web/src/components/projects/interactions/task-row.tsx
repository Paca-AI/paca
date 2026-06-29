import { Check, GripVertical, Layers, Link, User } from "lucide-react";

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
import type { Task } from "@/lib/interaction-api";
import type {
	CustomFieldDefinition,
	ProjectMember,
	TaskStatus,
	TaskType,
} from "@/lib/project-api";
import { cn } from "@/lib/utils";

import {
	getPriority,
	IMPORTANCE_BUCKET_VALUES,
	PRIORITY_LEVELS,
} from "./priority";
import { TaskTypeSelector } from "./task-type-selector";
import { DEFAULT_VISIBLE_FIELDS, type TaskFieldUpdate } from "./view-utils";

// ── Column config ──────────────────────────────────────────────────────────────

interface ColConfig {
	className: string;
	headerLabel: string;
	responsive?: boolean; // hide on xs screens
}

export function getRowColConfig(
	fieldKey: string,
	customFields: CustomFieldDefinition[],
): ColConfig {
	switch (fieldKey) {
		case "type":
			return { className: "w-28 shrink-0", headerLabel: "Type" };
		case "importance":
			return {
				className: "w-20 shrink-0",
				headerLabel: "Importance",
				responsive: true,
			};
		case "story_points":
			return {
				className: "w-14 shrink-0",
				headerLabel: "SP",
				responsive: true,
			};
		case "status":
			return {
				className: "w-24 shrink-0",
				headerLabel: "Status",
				responsive: true,
			};
		case "assignee":
			return { className: "w-20 shrink-0", headerLabel: "Assignee" };
		case "reporter":
			return {
				className: "w-20 shrink-0",
				headerLabel: "Reporter",
				responsive: true,
			};
		case "start_date":
			return {
				className: "w-24 shrink-0",
				headerLabel: "Start Date",
				responsive: true,
			};
		case "due_date":
			return {
				className: "w-24 shrink-0",
				headerLabel: "Due Date",
				responsive: true,
			};
		case "epic":
			return {
				className: "w-32 shrink-0",
				headerLabel: "Epic",
				responsive: true,
			};
		case "created":
			return {
				className: "w-24 shrink-0",
				headerLabel: "Created",
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

function formatDate(iso: string): string {
	const d = new Date(iso);
	return d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

// ── Props ──────────────────────────────────────────────────────────────────────

interface TaskRowProps {
	task: Task;
	taskIdPrefix?: string;
	statuses: TaskStatus[];
	taskTypes: TaskType[];
	members?: ProjectMember[];
	epics?: Task[];
	customFields?: CustomFieldDefinition[];
	visibleFields?: string[];
	onClick?: () => void;
	showDragHandle?: boolean;
	isDragging?: boolean;
	canEdit?: boolean;
	onUpdateTaskField?: (taskId: string, update: TaskFieldUpdate) => void;
	onDoubleClick?: () => void;
}

export function TaskRow({
	task,
	taskIdPrefix = "",
	statuses,
	taskTypes,
	members = [],
	epics = [],
	customFields = [],
	visibleFields = DEFAULT_VISIBLE_FIELDS,
	onClick,
	showDragHandle,
	isDragging,
	canEdit,
	onUpdateTaskField,
		onDoubleClick,
}: TaskRowProps) {
	const status = statuses.find((s) => s.id === task.status_id);

	/** Renders a single cell value for the given field key. */
	const renderCell = (fieldKey: string) => {
		const col = getRowColConfig(fieldKey, customFields);
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
												{p.label}
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
										<span style={{ color: p.color }}>{p.label}</span>
										{getPriority(task.importance).label === p.label &&
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
										{p.label}
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
							<span className="inline-flex items-center gap-1.5 rounded-full border border-border/40 bg-muted/40 px-2.5 py-0.5 text-xs font-semibold text-muted-foreground tracking-wide">
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
								{status.name}
							</span>
						) : (
							<span className="text-xs text-muted-foreground/50">—</span>
						)}
					</div>
				);

			case "assignee": {
				const assignee = task.assignee_id
					? members.find((m) => m.id === task.assignee_id)
					: undefined;
				return canEditField && members.length > 0 ? (
					// biome-ignore lint/a11y/noStaticElementInteractions: cell container stops propagation; inner controls are the interactive elements
					<div
						key="assignee"
						className={cn(col.className, "flex items-center justify-center")}
						onClick={(e) => e.stopPropagation()}
						onKeyDown={(e) => e.stopPropagation()}
					>
						<Popover>
							<PopoverTrigger
								type="button"
								className="flex size-6 items-center justify-center rounded-full transition-all duration-150 hover:ring-2 hover:ring-primary/30"
							>
								<div
									className={cn(
										"flex size-6 items-center justify-center rounded-full text-xs font-bold ring-1",
										assignee
											? "bg-linear-to-br from-primary/20 to-primary/10 text-primary ring-primary/20"
											: "bg-linear-to-br from-muted/80 to-muted/40 text-muted-foreground ring-border/25",
									)}
								>
									{assignee ? (
										(assignee.full_name || assignee.username)
											.slice(0, 1)
											.toUpperCase()
									) : (
										<User className="size-3" />
									)}
								</div>
							</PopoverTrigger>
							<PopoverContent
								className="w-48 p-1 rounded-xl border border-border/40 shadow-lg"
								align="start"
							>
								<button
									type="button"
									className="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-sm text-muted-foreground hover:bg-muted/60 transition-colors duration-100"
									onClick={() =>
										onUpdateTaskField(task.id, { assignee_id: null })
									}
								>
									<User className="size-3.5 opacity-60" />
									<span className="flex-1 text-left">Unassigned</span>
									{!assignee && <Check className="size-3.5 text-primary" />}
								</button>
								{members.map((m) => (
									<button
										key={m.id}
										type="button"
										className="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-sm hover:bg-muted/60 transition-colors duration-100"
										onClick={() =>
											onUpdateTaskField(task.id, { assignee_id: m.id })
										}
									>
										<div className="flex size-5 items-center justify-center rounded-full bg-linear-to-br from-primary/20 to-primary/10 text-primary text-xs font-bold">
											{(m.full_name || m.username).slice(0, 1).toUpperCase()}
										</div>
										<span className="flex-1 text-left truncate">
											{m.full_name || m.username}
										</span>
										{m.id === task.assignee_id && (
											<Check className="size-3.5 text-primary" />
										)}
									</button>
								))}
							</PopoverContent>
						</Popover>
					</div>
				) : (
					<div
						key="assignee"
						className={cn(col.className, "flex items-center justify-center")}
					>
						<div
							className={cn(
								"flex size-6 items-center justify-center rounded-full text-xs font-bold ring-1",
								assignee
									? "bg-linear-to-br from-primary/20 to-primary/10 text-primary ring-primary/20"
									: "bg-linear-to-br from-muted/80 to-muted/40 text-muted-foreground ring-border/25",
							)}
						>
							{assignee ? (
								(assignee.full_name || assignee.username)
									.slice(0, 1)
									.toUpperCase()
							) : (
								<User className="size-3" />
							)}
						</div>
					</div>
				);
			}

			case "reporter": {
				const reporter = task.reporter_id
					? members.find((m) => m.id === task.reporter_id)
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
						<div className="flex size-6 items-center justify-center rounded-full bg-linear-to-br from-muted/80 to-muted/40 text-muted-foreground text-xs font-bold ring-1 ring-border/25">
							{reporter ? (
								(reporter.full_name || reporter.username)
									.slice(0, 1)
									.toUpperCase()
							) : (
								<User className="size-3" />
							)}
						</div>
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
							{task.start_date ? formatDate(task.start_date) : "—"}
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
							{task.due_date ? formatDate(task.due_date) : "—"}
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
							{formatDate(task.created_at)}
						</span>
					</div>
				);

			case "epic": {
				const epic = task.parent_task_id
					? epics.find((e) => e.id === task.parent_task_id)
					: undefined;
				return canEditField ? (
					// biome-ignore lint/a11y/noStaticElementInteractions: cell container stops propagation; inner controls are the interactive elements
					<div
						key="epic"
						className={cn(col.className, responsiveClass, "items-center")}
						onClick={(e) => e.stopPropagation()}
						onKeyDown={(e) => e.stopPropagation()}
					>
						<Popover>
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
								className="w-56 p-1 rounded-xl border border-border/40 shadow-lg"
								align="start"
							>
								<button
									type="button"
									className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-sm text-muted-foreground hover:bg-muted/60 transition-colors duration-100"
									onClick={() =>
										onUpdateTaskField(task.id, { parent_task_id: null })
									}
								>
									<span className="flex-1 text-left">No Epic</span>
									{!task.parent_task_id && (
										<Check className="size-3.5 text-primary" />
									)}
								</button>
								{epics.map((e) => (
									<button
										key={e.id}
										type="button"
										className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-sm hover:bg-muted/60 transition-colors duration-100"
										onClick={() =>
											onUpdateTaskField(task.id, { parent_task_id: e.id })
										}
									>
										<Layers className="size-3.5 shrink-0 text-violet-500 opacity-70" />
										<span className="flex-1 text-left truncate">{e.title}</span>
										{e.id === task.parent_task_id && (
											<Check className="size-3.5 text-primary" />
										)}
									</button>
								))}
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
									{formatDate(String(val))}
								</span>
							);
						case "select":
							return (
								<span className="inline-flex items-center rounded-full bg-primary/10 px-2 py-0.5 text-xs font-medium text-primary/80 truncate max-w-full">
									{String(val)}
								</span>
							);
						case "multi_select": {
							const arr = Array.isArray(val)
								? (val as string[])
								: [String(val)];
							return (
								<span className="inline-flex gap-1 flex-wrap">
									{arr.map((v) => (
										<span
											key={v}
											className="inline-flex items-center rounded-full bg-primary/10 px-1.5 py-0.5 text-xs font-medium text-primary/80"
										>
											{v}
										</span>
									))}
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
			onDoubleClick={onDoubleClick}
			className={cn(
				"group flex items-center gap-3 px-4 py-2.5 cursor-pointer",
				"hover:bg-muted/30 transition-colors duration-150 border-b border-border/20 last:border-0",
				isDragging && "opacity-40 bg-muted/20",
			)}
		>
			{showDragHandle && (
				<GripVertical className="size-3.5 shrink-0 -ml-1.5 text-muted-foreground/30 group-hover:text-muted-foreground/70 cursor-grab opacity-0 group-hover:opacity-100 transition-opacity duration-200" />
			)}

			{/* Task ID — separate fixed-width column left of title */}
			<span className="w-20 shrink-0 font-[JetBrains_Mono,monospace] text-xs font-semibold text-muted-foreground/55 tracking-wide">
				{taskIdPrefix
					? `${taskIdPrefix}-${task.task_number}`
					: task.task_number > 0
						? `#${task.task_number}`
						: ""}
			</span>

			{/* Title */}
			<span className="flex-1 text-sm font-medium text-foreground truncate">
				{task.title}
			</span>

			{/* Dynamic field columns */}
			{visibleFields.map(renderCell)}
		</div>
	);
}
