import { ListChecks, Loader2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { Task } from "@/lib/interaction-api";
import type { ProjectMember, TaskStatus, TaskType } from "@/lib/project-api";
import { AddTaskRow } from "../add-task-row";
import { SubtaskRow } from "./subtask-row";

interface SubtasksSectionProps {
	projectId?: string;
	parentTaskId: string;
	subtasks: Task[];
	statuses: TaskStatus[];
	taskTypes?: TaskType[];
	members?: ProjectMember[];
	canEdit?: boolean;
	task: Task;
	taskIdPrefix?: string;
	/** Available types for the type picker when creating subtasks */
	normalTaskTypes?: TaskType[];
	/** True while more subtasks exist beyond what's currently loaded. */
	hasMore?: boolean;
	isLoadingMore?: boolean;
	onLoadMore?: () => void;
	onSubtaskUpdate?: (
		subtaskId: string,
		payload: Partial<{
			status_id: string | null;
			task_type_id: string | null;
			assignee_ids: string[];
			importance: number;
		}>,
	) => void;
	onSubtaskCreate?: (payload: {
		title: string;
		status_id?: string | null;
		task_type_id?: string | null;
	}) => void;
	onSubtaskClick?: (task: Task) => void;
}

export function SubtasksSection({
	subtasks,
	statuses,
	taskTypes = [],
	members = [],
	canEdit = true,
	taskIdPrefix = "",
	normalTaskTypes = [],
	hasMore = false,
	isLoadingMore = false,
	onLoadMore,
	onSubtaskUpdate,
	onSubtaskCreate,
	onSubtaskClick,
}: SubtasksSectionProps) {
	const { t } = useTranslation("projects");
	return (
		<div className="space-y-3">
			<h3 className="text-xs font-semibold uppercase tracking-[0.08em] text-muted-foreground/70 flex items-center gap-2">
				<span>{t("taskDetail.subtasks.title")}</span>
				<div className="flex-1 h-px bg-linear-to-r from-border/40 to-transparent" />
			</h3>

			{(subtasks.length > 0 || canEdit) && (
				<div className="rounded-xl border border-border/25 bg-card/50 divide-y divide-border/15 overflow-hidden">
					{subtasks.map((sub) => (
						<SubtaskRow
							key={sub.id}
							task={sub}
							taskIdPrefix={taskIdPrefix}
							statuses={statuses}
							taskTypes={taskTypes}
							members={members}
							showTypeField
							canEdit={canEdit}
							onUpdate={onSubtaskUpdate}
							onClick={onSubtaskClick ? () => onSubtaskClick(sub) : undefined}
						/>
					))}
					{hasMore && (
						<button
							type="button"
							onClick={onLoadMore}
							disabled={isLoadingMore}
							className="flex w-full items-center justify-center gap-1.5 py-2.5 text-xs font-medium text-muted-foreground/60 hover:text-primary hover:bg-primary/5 transition-all duration-150 disabled:opacity-50"
						>
							{isLoadingMore ? (
								<>
									<Loader2 className="size-3 animate-spin" />
									{t("taskDetail.subtasks.loadingMore")}
								</>
							) : (
								t("taskDetail.subtasks.loadMore")
							)}
						</button>
					)}
					{canEdit && (
						<AddTaskRow
							variant="list"
							taskTypes={normalTaskTypes}
							label={t("taskDetail.subtasks.addButton")}
							placeholder={t("taskDetail.subtasks.titlePlaceholder")}
							onAdd={(title, taskTypeId) =>
								onSubtaskCreate?.({ title, task_type_id: taskTypeId })
							}
						/>
					)}
				</div>
			)}

			{!canEdit && subtasks.length === 0 && (
				<div className="flex items-center gap-3 px-1 py-3 text-muted-foreground/45">
					<ListChecks className="size-4 opacity-70" />
					<p className="text-sm italic">{t("taskDetail.subtasks.empty")}</p>
				</div>
			)}
		</div>
	);
}
