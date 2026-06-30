import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ExternalLink, Link2, Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
	createTaskLink,
	type DisplayLinkType,
	deleteTaskLink,
	type TaskLink,
	taskLinksQueryOptions,
} from "@/lib/interaction-api";
import {
	AddTaskLinkModal,
	type AddTaskLinkPayload,
} from "./add-task-link-modal";

interface TaskLinksSectionProps {
	projectId: string;
	taskId: string;
	taskIdPrefix?: string;
	canEdit?: boolean;
	onNavigateToTask?: (taskId: string) => void;
}

// Groups links by their display type label
type GroupedLinks = Record<string, TaskLink[]>;

const DISPLAY_ORDER: DisplayLinkType[] = [
	"blocks",
	"is_blocked_by",
	"relates_to",
	"duplicates",
	"is_duplicated_by",
];

// Maps the API display link type to its translation key suffix
const LINK_TYPE_KEYS: Record<DisplayLinkType, string> = {
	blocks: "blocks",
	is_blocked_by: "isBlockedBy",
	relates_to: "relatesTo",
	duplicates: "duplicates",
	is_duplicated_by: "isDuplicatedBy",
};

function groupLinks(links: TaskLink[]): GroupedLinks {
	const groups: GroupedLinks = {};
	for (const link of links) {
		const key = link.display_link_type;
		if (!groups[key]) groups[key] = [];
		groups[key].push(link);
	}
	return groups;
}

export function TaskLinksSection({
	projectId,
	taskId,
	taskIdPrefix = "",
	canEdit = true,
	onNavigateToTask,
}: TaskLinksSectionProps) {
	const { t } = useTranslation();
	const qc = useQueryClient();
	const [modalOpen, setModalOpen] = useState(false);

	const { data: links = [] } = useQuery(
		taskLinksQueryOptions(projectId, taskId),
	);

	const createMutation = useMutation({
		mutationFn: ({
			sourceTaskId,
			targetTaskId,
			linkType,
		}: AddTaskLinkPayload) =>
			createTaskLink(projectId, sourceTaskId, {
				target_task_id: targetTaskId,
				link_type: linkType,
			}),
		onSuccess: () => {
			qc.invalidateQueries({
				queryKey: taskLinksQueryOptions(projectId, taskId).queryKey,
			});
		},
	});

	const deleteMutation = useMutation({
		mutationFn: (linkId: string) => deleteTaskLink(projectId, taskId, linkId),
		onSuccess: () => {
			qc.invalidateQueries({
				queryKey: taskLinksQueryOptions(projectId, taskId).queryKey,
			});
		},
	});

	function handleAdd(payload: AddTaskLinkPayload) {
		setModalOpen(false);
		createMutation.mutate(payload);
	}

	const grouped = groupLinks(links);
	const orderedKeys = DISPLAY_ORDER.filter((k) => grouped[k]?.length > 0);

	return (
		<div className="space-y-3">
			<div className="flex items-center justify-between">
				<h3 className="text-xs font-semibold uppercase tracking-[0.08em] text-muted-foreground/70 flex items-center gap-2">
					<span>{t("project.interactions.taskDetail.links.title")}</span>
					<div className="flex-1 h-px bg-linear-to-r from-border/40 to-transparent" />
				</h3>
				{canEdit && (
					<button
						type="button"
						onClick={() => setModalOpen(true)}
						className="flex items-center gap-1.5 rounded-lg bg-primary/8 text-primary/80 hover:bg-primary/15 hover:text-primary px-2.5 py-1.5 text-xs font-semibold transition-all duration-150"
					>
						<Plus className="size-3" />
						{t("project.interactions.taskDetail.links.addLink")}
					</button>
				)}
			</div>

			{createMutation.error && (
				<p className="text-sm text-destructive">
					{createMutation.error.message}
				</p>
			)}

			{orderedKeys.length > 0 && (
				<div className="space-y-3">
					{orderedKeys.map((displayType) => (
						<div key={displayType}>
							<p className="text-xs font-semibold uppercase tracking-widest text-muted-foreground/50 mb-1.5 px-0.5">
								{t(
									`project.interactions.taskDetail.links.types.${LINK_TYPE_KEYS[displayType as DisplayLinkType]}`,
								)}
							</p>
							<div className="rounded-xl border border-border/25 bg-card/50 divide-y divide-border/15 overflow-hidden">
								{grouped[displayType].map((link) => {
									const linked = link.linked_task;
									const prefix = taskIdPrefix
										? `${taskIdPrefix}-${linked.task_number}`
										: `#${linked.task_number}`;
									return (
										<div
											key={link.id}
											className="flex items-center gap-3 px-4 py-2.5 group"
										>
											<Link2 className="size-3 text-muted-foreground/40 shrink-0" />
											<span className="shrink-0 text-xs font-mono text-muted-foreground/50">
												{prefix}
											</span>
											{/* biome-ignore lint/a11y/useSemanticElements: span for styling and truncation */}
											<span
												role="button"
												tabIndex={onNavigateToTask ? 0 : -1}
												className={`flex-1 text-sm text-foreground truncate ${
													onNavigateToTask
														? "cursor-pointer hover:text-primary transition-colors duration-100"
														: ""
												}`}
												onClick={() => onNavigateToTask?.(linked.id)}
												onKeyDown={(e) => {
													if (e.key === "Enter" || e.key === " ") {
														e.preventDefault();
														onNavigateToTask?.(linked.id);
													}
												}}
											>
												{linked.title}
											</span>
											<div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity duration-150">
												{onNavigateToTask && (
													<button
														type="button"
														onClick={() => onNavigateToTask(linked.id)}
														className="size-6 rounded flex items-center justify-center text-muted-foreground/50 hover:text-foreground hover:bg-muted/30 transition-all duration-100"
														title={t(
												"project.interactions.taskDetail.links.openTask",
											)}
													>
														<ExternalLink className="size-3" />
													</button>
												)}
												{canEdit && (
													<button
														type="button"
														onClick={() => deleteMutation.mutate(link.id)}
														className="size-6 rounded flex items-center justify-center text-muted-foreground/50 hover:text-destructive hover:bg-destructive/10 transition-all duration-100"
														title={t(
												"project.interactions.taskDetail.links.removeLink",
											)}
													>
														<Trash2 className="size-3" />
													</button>
												)}
											</div>
										</div>
									);
								})}
							</div>
						</div>
					))}
				</div>
			)}

			{orderedKeys.length === 0 && (
				<div className="flex items-center gap-3 px-1 py-3 text-muted-foreground/45">
					<Link2 className="size-4 opacity-70" />
					<p className="text-sm italic">
						{t("project.interactions.taskDetail.links.empty")}
					</p>
				</div>
			)}

			<AddTaskLinkModal
				open={modalOpen}
				onClose={() => setModalOpen(false)}
				onAdd={handleAdd}
				projectId={projectId}
				currentTaskId={taskId}
				taskIdPrefix={taskIdPrefix}
			/>
		</div>
	);
}
