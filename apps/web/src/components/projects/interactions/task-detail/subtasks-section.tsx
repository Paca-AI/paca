import { ListChecks } from "lucide-react";
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
	onSubtaskUpdate?: (
		subtaskId: string,
		payload: Partial<{
			status_id: string | null;
			task_type_id: string | null;
			assignee_id: string | null;
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
	onSubtaskUpdate,
	onSubtaskCreate,
	onSubtaskClick,
}: SubtasksSectionProps) {
	const { t } = useTranslation();
	return (
		<div className="space-y-3">
			<h3 className="text-xs font-semibold uppercase tracking-[0.08em] text-muted-foreground/70 flex items-center gap-2">
				<span>{t("project.interactions.taskDetail.subtasks")}</span>
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
					{canEdit && (
						<AddTaskRow
							variant="list"
							taskTypes={normalTaskTypes}
							label={t("project.interactions.taskDetail.addSubtask")}
							placeholder={t(
								"project.interactions.taskDetail.subtaskTitlePlaceholder",
							)}
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
					<p className="text-sm italic">
						{t("project.interactions.taskDetail.noSubtasks")}
					</p>
				</div>
			)}
		</div>
	);
}
