import { Check } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import type { Task } from "@/lib/interaction-api";
import type { ProjectMember, TaskStatus, TaskType } from "@/lib/project-api";
import { cn } from "@/lib/utils";
import { AssigneePicker } from "../assignee-picker";
import {
	getPriority,
	IMPORTANCE_BUCKET_VALUES,
	PRIORITY_LEVELS,
} from "../priority";
import { TaskTypeSelector } from "../task-type-selector";

type SubtaskUpdatePayload = Partial<{
	status_id: string | null;
	task_type_id: string | null;
	assignee_ids: string[];
	importance: number;
}>;

interface SubtaskRowProps {
	task: Task;
	taskIdPrefix?: string;
	statuses: TaskStatus[];
	taskTypes?: TaskType[];
	members?: ProjectMember[];
	/** Show the Type field (used for epic's task list) */
	showTypeField?: boolean;
	canEdit?: boolean;
	onUpdate?: (taskId: string, payload: SubtaskUpdatePayload) => void;
	onClick?: () => void;
}

export function SubtaskRow({
	task,
	taskIdPrefix = "",
	statuses,
	taskTypes = [],
	members = [],
	showTypeField = false,
	canEdit = true,
	onUpdate,
	onClick,
}: SubtaskRowProps) {
	const { t } = useTranslation("projects");
	const status = statuses.find((s) => s.id === task.status_id);
	const priority = getPriority(task.importance ?? 0);
	const canEditField = !!(canEdit && onUpdate);

	const displayId = taskIdPrefix
		? `${taskIdPrefix}-${task.task_number}`
		: task.task_number > 0
			? `#${task.task_number}`
			: "";

	return (
		// biome-ignore lint/a11y/useKeyWithClickEvents: list row with click handler
		// biome-ignore lint/a11y/noStaticElementInteractions: list row
		<div
			onClick={onClick}
			className={cn(
				"group flex items-center gap-2.5 px-3.5 py-2 transition-colors duration-150 border-b border-border/15 last:border-0",
				onClick ? "cursor-pointer hover:bg-muted/30" : "hover:bg-muted/20",
			)}
		>
			{/* Task ID */}
			<span className="w-16 shrink-0 font-[JetBrains_Mono,monospace] text-xs font-semibold text-muted-foreground/50 tracking-wide truncate">
				{displayId}
			</span>

			{/* Title */}
			<span className="flex-1 text-sm font-medium text-foreground truncate min-w-0">
				{task.title}
			</span>

			{/* Type — only in epic's task list */}
			{showTypeField && (
				// biome-ignore lint/a11y/noStaticElementInteractions: cell container stops propagation; inner controls are the interactive elements
				<div
					className="shrink-0"
					onClick={(e) => e.stopPropagation()}
					onKeyDown={(e) => e.stopPropagation()}
				>
					<TaskTypeSelector
						taskTypes={taskTypes}
						value={task.task_type_id}
						canEdit={canEditField && taskTypes.length > 0}
						onChange={(id) => onUpdate?.(task.id, { task_type_id: id })}
						align="end"
					/>
				</div>
			)}

			{/* Assignee */}
			{/* biome-ignore lint/a11y/noStaticElementInteractions: cell container stops propagation; inner controls are the interactive elements */}
			<div
				className="shrink-0 flex items-center justify-center"
				onClick={(e) => e.stopPropagation()}
				onKeyDown={(e) => e.stopPropagation()}
			>
				<AssigneePicker
					members={members}
					assigneeIds={task.assignee_ids ?? []}
					canEdit={canEditField}
					align="end"
					onChange={(patch) => onUpdate?.(task.id, patch)}
				/>
			</div>

			{/* Priority */}
			{/* biome-ignore lint/a11y/noStaticElementInteractions: cell container stops propagation; inner controls are the interactive elements */}
			<div
				className="shrink-0 w-16 flex items-center"
				onClick={(e) => e.stopPropagation()}
				onKeyDown={(e) => e.stopPropagation()}
			>
				{canEditField ? (
					<DropdownMenu>
						<DropdownMenuTrigger className="flex items-center gap-1 hover:opacity-80 transition-opacity cursor-pointer">
							{(task.importance ?? 0) > 0 ? (
								<>
									<span
										className="size-1.5 rounded-full shrink-0"
										style={{ background: priority.color }}
									/>
									<span
										className="text-xs font-medium truncate"
										style={{ color: priority.color }}
									>
										{t(priority.labelKey)}
									</span>
								</>
							) : (
								<span className="text-xs text-muted-foreground/40">—</span>
							)}
						</DropdownMenuTrigger>
						<DropdownMenuContent align="end">
							{PRIORITY_LEVELS.map((p) => (
								<DropdownMenuItem
									key={p.value}
									onClick={() =>
										onUpdate?.(task.id, {
											importance: IMPORTANCE_BUCKET_VALUES[p.value] ?? 0,
										})
									}
								>
									<span
										className="size-2 rounded-full shrink-0 mr-2"
										style={{ background: p.color }}
									/>
									<span style={{ color: p.color }}>{t(p.labelKey)}</span>
									{priority.labelKey === p.labelKey &&
										(task.importance ?? 0) > 0 && (
											<Check className="size-3.5 text-primary ml-auto" />
										)}
								</DropdownMenuItem>
							))}
						</DropdownMenuContent>
					</DropdownMenu>
				) : (task.importance ?? 0) > 0 ? (
					<>
						<span
							className="size-1.5 rounded-full shrink-0 mr-1"
							style={{ background: priority.color }}
						/>
						<span
							className="text-xs font-medium truncate"
							style={{ color: priority.color }}
						>
							{t(priority.labelKey)}
						</span>
					</>
				) : (
					<span className="text-xs text-muted-foreground/40">—</span>
				)}
			</div>

			{/* Status */}
			{/* biome-ignore lint/a11y/noStaticElementInteractions: cell container stops propagation; inner controls are the interactive elements */}
			<div
				className="shrink-0 w-24 flex items-center"
				onClick={(e) => e.stopPropagation()}
				onKeyDown={(e) => e.stopPropagation()}
			>
				{canEditField && statuses.length > 0 ? (
					<DropdownMenu>
						<DropdownMenuTrigger className="inline-flex items-center gap-1.5 rounded-full border border-border/40 bg-muted/40 px-2 py-0.5 text-xs font-semibold text-muted-foreground tracking-wide hover:opacity-80 transition-opacity cursor-pointer truncate max-w-full">
							{status ? (
								<>
									<span
										className="size-1.5 rounded-full shrink-0"
										style={{
											background: status.color ?? "var(--muted-foreground)",
										}}
									/>
									<span className="truncate">{status.name}</span>
								</>
							) : (
								<span className="text-muted-foreground/40">—</span>
							)}
						</DropdownMenuTrigger>
						<DropdownMenuContent align="end">
							{statuses.map((s) => (
								<DropdownMenuItem
									key={s.id}
									onClick={() => onUpdate?.(task.id, { status_id: s.id })}
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
				) : status ? (
					<span className="inline-flex items-center gap-1.5 rounded-full border border-border/40 bg-muted/40 px-2 py-0.5 text-xs font-semibold text-muted-foreground tracking-wide truncate max-w-full">
						<span
							className="size-1.5 rounded-full shrink-0"
							style={{ background: status.color ?? "var(--muted-foreground)" }}
						/>
						<span className="truncate">{status.name}</span>
					</span>
				) : (
					<span className="text-xs text-muted-foreground/40">—</span>
				)}
			</div>
		</div>
	);
}
