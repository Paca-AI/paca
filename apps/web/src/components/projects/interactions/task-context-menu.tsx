import { Check, Copy, Layers, Link2, Trash2, User } from "lucide-react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { getTaskTypeIconComponent } from "@/components/projects/task-types/task-type-icons";
import {
	ContextMenu,
	ContextMenuContent,
	ContextMenuItem,
	ContextMenuSeparator,
	ContextMenuShortcut,
	ContextMenuSub,
	ContextMenuSubContent,
	ContextMenuSubTrigger,
	ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { KbdChord } from "@/components/ui/kbd";
import type { Task } from "@/lib/interaction-api";
import type { ProjectMember, TaskStatus, TaskType } from "@/lib/project-api";
import { isMacPlatform, SHORTCUT_DISPLAY } from "@/lib/shortcuts/keymap";

import {
	getPriority,
	IMPORTANCE_BUCKET_VALUES,
	PRIORITY_LEVELS,
} from "./priority";
import type {
	ColumnGroupDef,
	EpicsPagination,
	TaskFieldUpdate,
} from "./view-utils";

const shortcutChord = (id: string) =>
	SHORTCUT_DISPLAY.find((e) => e.id === id)?.chords[0];

function Hint({ actionId }: { actionId: string }) {
	const chord = shortcutChord(actionId);
	if (!chord) return null;
	return (
		<ContextMenuShortcut>
			<KbdChord chord={chord} className="gap-0" />
		</ContextMenuShortcut>
	);
}

interface TaskContextMenuProps {
	task: Task;
	statuses: TaskStatus[];
	taskTypes: TaskType[];
	members: ProjectMember[];
	epics: Task[];
	/** Load-more state for the epic submenu — omitted when the caller
	 * doesn't paginate epics (e.g. fewer than one page's worth). */
	epicsPagination?: EpicsPagination;
	canEdit: boolean;
	onOpen: () => void;
	onUpdate?: (taskId: string, update: TaskFieldUpdate) => void;
	onDelete?: (taskId: string) => void;
	/** All columns of the active view — omitted for views with no column
	 * concept (e.g. Roadmap), which hides the "Move to" submenu. */
	columnDefs?: ColumnGroupDef[];
	onMoveToColumnDef?: (task: Task, colDef: ColumnGroupDef) => void;
	taskIdPrefix?: string;
	projectId: string;
	children: ReactNode;
}

/** Right-click menu for a task card/row — mirrors the Mod+key task
 * shortcuts, showing each item's key as a hint (see keymap.ts). */
export function TaskContextMenu({
	task,
	statuses,
	taskTypes,
	members,
	epics,
	epicsPagination,
	canEdit,
	onOpen,
	onUpdate,
	onDelete,
	columnDefs,
	onMoveToColumnDef,
	taskIdPrefix = "",
	projectId,
	children,
}: TaskContextMenuProps) {
	const { t } = useTranslation("projects");
	const isMac = isMacPlatform();
	const taskLabel = taskIdPrefix
		? `${taskIdPrefix}-${task.task_number}`
		: `#${task.task_number}`;

	const copyId = () => {
		navigator.clipboard?.writeText(taskLabel)?.catch(() => {});
	};
	const copyLink = () => {
		const url = `${window.location.origin}/projects/${projectId}/tasks/${task.id}`;
		navigator.clipboard?.writeText(url)?.catch(() => {});
	};

	return (
		<ContextMenu>
			<ContextMenuTrigger>{children}</ContextMenuTrigger>
			<ContextMenuContent className="w-56">
				<ContextMenuItem onClick={onOpen}>
					{t("board.taskContextMenu.open")}
					<Hint actionId="task.open" />
				</ContextMenuItem>

				{canEdit &&
					columnDefs &&
					columnDefs.length > 0 &&
					onMoveToColumnDef && (
						<ContextMenuSub>
							<ContextMenuSubTrigger>
								{t("board.taskContextMenu.moveTo")}
							</ContextMenuSubTrigger>
							<ContextMenuSubContent className="w-48">
								{columnDefs.map((colDef) => (
									<ContextMenuItem
										key={colDef.key}
										onClick={() => onMoveToColumnDef(task, colDef)}
									>
										{colDef.color && (
											<span
												className="size-2 rounded-full shrink-0"
												style={{ background: colDef.color }}
											/>
										)}
										<span className="flex-1 truncate">{colDef.label}</span>
									</ContextMenuItem>
								))}
							</ContextMenuSubContent>
						</ContextMenuSub>
					)}

				{canEdit && statuses.length > 0 && (
					<ContextMenuSub>
						<ContextMenuSubTrigger>
							{t("board.taskContextMenu.status")}
						</ContextMenuSubTrigger>
						<ContextMenuSubContent className="w-44">
							{statuses.map((s) => (
								<ContextMenuItem
									key={s.id}
									onClick={() => onUpdate?.(task.id, { status_id: s.id })}
								>
									<span
										className="size-2 rounded-full shrink-0"
										style={{ background: s.color ?? undefined }}
									/>
									<span className="flex-1 truncate">{s.name}</span>
									{s.id === task.status_id && (
										<Check className="size-3.5 text-primary shrink-0" />
									)}
								</ContextMenuItem>
							))}
						</ContextMenuSubContent>
					</ContextMenuSub>
				)}

				{canEdit && members.length > 0 && (
					<ContextMenuSub>
						<ContextMenuSubTrigger>
							{t("board.taskContextMenu.assignee")}
						</ContextMenuSubTrigger>
						<ContextMenuSubContent className="w-48">
							<ContextMenuItem
								onClick={() => onUpdate?.(task.id, { assignee_ids: [] })}
							>
								<User className="size-3.5 opacity-60" />
								<span className="flex-1 truncate">
									{t("board.taskContextMenu.unassigned")}
								</span>
								{(task.assignee_ids ?? []).length === 0 && (
									<Check className="size-3.5 text-primary shrink-0" />
								)}
							</ContextMenuItem>
							{members.map((m) => {
								const isSelected = task.assignee_ids?.includes(m.id) ?? false;
								return (
									<ContextMenuItem
										key={m.id}
										onClick={() => {
											const current = task.assignee_ids ?? [];
											onUpdate?.(task.id, {
												assignee_ids: isSelected
													? current.filter((id) => id !== m.id)
													: [...current, m.id],
											});
										}}
									>
										<span className="flex-1 truncate">
											{m.full_name || m.username}
										</span>
										{isSelected && (
											<Check className="size-3.5 text-primary shrink-0" />
										)}
									</ContextMenuItem>
								);
							})}
						</ContextMenuSubContent>
					</ContextMenuSub>
				)}

				{canEdit && taskTypes.length > 0 && (
					<ContextMenuSub>
						<ContextMenuSubTrigger>
							{t("board.taskContextMenu.type")}
						</ContextMenuSubTrigger>
						<ContextMenuSubContent className="w-44">
							{taskTypes.map((tt) => {
								const Icon = getTaskTypeIconComponent(tt.icon);
								return (
									<ContextMenuItem
										key={tt.id}
										onClick={() => onUpdate?.(task.id, { task_type_id: tt.id })}
									>
										{Icon && (
											<Icon
												className="size-3.5 shrink-0"
												style={tt.color ? { color: tt.color } : undefined}
											/>
										)}
										<span className="flex-1 truncate">{tt.name}</span>
										{tt.id === task.task_type_id && (
											<Check className="size-3.5 text-primary shrink-0" />
										)}
									</ContextMenuItem>
								);
							})}
						</ContextMenuSubContent>
					</ContextMenuSub>
				)}

				{canEdit && (
					<ContextMenuSub>
						<ContextMenuSubTrigger>
							{t("board.taskContextMenu.priority")}
						</ContextMenuSubTrigger>
						<ContextMenuSubContent className="w-40">
							{PRIORITY_LEVELS.map((level) => (
								<ContextMenuItem
									key={level.value}
									onClick={() =>
										onUpdate?.(task.id, {
											importance: IMPORTANCE_BUCKET_VALUES[level.value] ?? 0,
										})
									}
								>
									<span
										className="size-2 rounded-full shrink-0"
										style={{ background: level.color }}
									/>
									<span
										className="flex-1 truncate"
										style={{ color: level.color }}
									>
										{t(level.labelKey)}
									</span>
									{getPriority(task.importance).labelKey === level.labelKey &&
										task.importance > 0 === level.value > 0 && (
											<Check className="size-3.5 text-primary shrink-0" />
										)}
								</ContextMenuItem>
							))}
						</ContextMenuSubContent>
					</ContextMenuSub>
				)}

				{canEdit && epics.length > 0 && (
					<ContextMenuSub>
						<ContextMenuSubTrigger>
							{t("board.taskContextMenu.epic")}
							<Hint actionId="task.epic" />
						</ContextMenuSubTrigger>
						<ContextMenuSubContent className="w-56">
							<ContextMenuItem
								onClick={() => onUpdate?.(task.id, { parent_task_id: null })}
							>
								<span className="flex-1 truncate">
									{t("board.taskContextMenu.noEpic")}
								</span>
								{!task.parent_task_id && (
									<Check className="size-3.5 text-primary shrink-0" />
								)}
							</ContextMenuItem>
							{epics.map((e) => (
								<ContextMenuItem
									key={e.id}
									onClick={() => onUpdate?.(task.id, { parent_task_id: e.id })}
								>
									<Layers className="size-3.5 shrink-0 text-violet-500 opacity-70" />
									<span className="flex-1 truncate">{e.title}</span>
									{e.id === task.parent_task_id && (
										<Check className="size-3.5 text-primary shrink-0" />
									)}
								</ContextMenuItem>
							))}
						{epicsPagination?.hasMore && (
							<ContextMenuItem
								closeOnClick={false}
								onClick={() => epicsPagination.onLoadMore()}
								disabled={epicsPagination.isLoadingMore}
							>
								<span className="flex-1 truncate text-muted-foreground">
									{epicsPagination.isLoadingMore
										? t("board.taskContextMenu.loadingEpics")
										: t("board.taskContextMenu.loadMoreEpics")}
								</span>
							</ContextMenuItem>
						)}
					</ContextMenuSubContent>
				</ContextMenuSub>
			)}

				<ContextMenuSeparator />

				<ContextMenuItem onClick={copyId}>
					<Copy className="size-3.5" />
					{t("board.taskContextMenu.copyId", { id: taskLabel })}
				</ContextMenuItem>
				<ContextMenuItem onClick={copyLink}>
					<Link2 className="size-3.5" />
					{t("board.taskContextMenu.copyLink")}
				</ContextMenuItem>

				{canEdit && onDelete && (
					<>
						<ContextMenuSeparator />
						<ContextMenuItem
							variant="destructive"
							onClick={() => onDelete(task.id)}
						>
							<Trash2 className="size-3.5" />
							{t("board.taskContextMenu.delete")}
							<ContextMenuShortcut>
								<KbdChord
									chord={{ mod: true, key: "⌫" }}
									className={isMac ? "gap-0" : "gap-1"}
								/>
							</ContextMenuShortcut>
						</ContextMenuItem>
					</>
				)}
			</ContextMenuContent>
		</ContextMenu>
	);
}
