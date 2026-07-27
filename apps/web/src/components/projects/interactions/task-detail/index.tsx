import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { AlertCircle } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
	createTask,
	epicTasksInfiniteQueryOptions,
	sprintsQueryOptions,
	subtasksQueryOptions,
	taskQueryOptions,
	updateTask,
} from "@/lib/interaction-api";
import { ExtensionPoint } from "@/lib/plugins/extension-point";
import {
	customFieldsQueryOptions,
	findEpicType,
	getNormalTaskTypes,
	isEpicType,
	projectQueryOptions,
} from "@/lib/project-api";
import { cleanBlocks, cn } from "@/lib/utils";
import { getTaskTypeIconComponent } from "../../task-types/task-type-icons";
import { getPriority } from "../priority";
import type { EpicsPagination } from "../view-utils";
import { TaskActivityPane as ActivityPane } from "./activity-pane";
import { AttachmentsSection } from "./attachments-section";
import { DescriptionSection } from "./description-section";
import { mapApiFieldToUi } from "./helpers";
import { PropertiesPanel } from "./properties-panel";
import { SubtasksSection } from "./subtasks-section";
import { TaskHeader } from "./task-header";
import { TaskLinksSection } from "./task-links-section";
import type { TaskDetailModalProps } from "./types";
import { WorkflowsSection } from "./workflows-section";

// Re-exports for consumers
export type {
	ActivityEntry,
	Attachment,
	CustomFieldDef,
	TaskDetailModalProps,
} from "./types";

const TITLE_CLASSES =
	"font-[Syne] text-xl lg:text-3xl font-bold leading-snug text-foreground tracking-tight w-full";

export function TaskDetailModal({
	task: taskProp,
	open,
	onOpenChange,
	statuses,
	taskTypes,
	members = [],
	projectName,
	interactionName,
	projectId,
	taskIdPrefix: taskIdPrefixProp = "",
	mode = "modal",
	canEdit = true,
}: TaskDetailModalProps) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const navigate = useNavigate();

	// Fetch project to get task ID prefix
	const { data: project } = useQuery({
		...projectQueryOptions(projectId ?? ""),
		enabled: !!projectId && (open || mode === "page"),
	});
	const taskIdPrefix = project?.task_id_prefix || taskIdPrefixProp;

	// Fetch fresh task data whenever the modal is open and we have a projectId
	const { data: freshTask } = useQuery({
		...taskQueryOptions(projectId ?? "", taskProp?.id ?? ""),
		enabled: !!projectId && !!taskProp?.id && (open || mode === "page"),
	});

	// Use fresh task if available, fall back to prop
	const task = freshTask ?? taskProp;

	// Fetch subtasks
	const { data: subtasks = [] } = useQuery({
		...subtasksQueryOptions(projectId ?? "", task?.id ?? ""),
		enabled: !!projectId && !!task?.id && (open || mode === "page"),
	});

	// Fetch sprints for sprint name display + assignment
	const { data: sprints = [] } = useQuery({
		...sprintsQueryOptions(projectId ?? ""),
		enabled: !!projectId && (open || mode === "page"),
	});

	// Fetch custom field definitions from API
	const { data: apiCustomFields = [] } = useQuery({
		...customFieldsQueryOptions(projectId ?? ""),
		enabled: !!projectId && (open || mode === "page"),
	});
	const customFieldDefs = useMemo(
		() => apiCustomFields.map(mapApiFieldToUi),
		[apiCustomFields],
	);

	const status = statuses.find((s) => s.id === task?.status_id);
	const taskType = taskTypes.find((t) => t.id === task?.task_type_id);
	const priority = getPriority(task?.importance ?? 0);
	const assignees = members.filter((m) => task?.assignee_ids?.includes(m.id));
	const reporter = members.find((m) => m.id === task?.reporter_id);

	// ── Task role detection ────────────────────────────────────────────────────
	const isEpic = isEpicType(taskType);
	const taskRole = isEpic ? "epic" : "normal";

	const epicType = findEpicType(taskTypes);
	const normalTaskTypes = getNormalTaskTypes(taskTypes);

	// For normal tasks: fetch epics (paginated 20-per-page) to populate the
	// Epic picker, with a "Load more" affordance for projects with >20 epics.
	const {
		data: epicPages,
		fetchNextPage: fetchNextEpicsPage,
		hasNextPage: hasMoreEpics,
		isFetchingNextPage: isFetchingMoreEpics,
	} = useInfiniteQuery({
		...epicTasksInfiniteQueryOptions(projectId ?? "", epicType?.id ?? ""),
		enabled:
			!!projectId &&
			!!epicType?.id &&
			taskRole === "normal" &&
			(open || mode === "page"),
	});
	const epicTasks = useMemo(
		() => epicPages?.pages.flatMap((page) => page.items) ?? [],
		[epicPages],
	);
	const epicsPagination: EpicsPagination = useMemo(
		() => ({
			hasMore: !!hasMoreEpics,
			isLoadingMore: isFetchingMoreEpics,
			onLoadMore: () => void fetchNextEpicsPage(),
		}),
		[hasMoreEpics, isFetchingMoreEpics, fetchNextEpicsPage],
	);

	// Fetch parent task to display its title/icon (for non-epic parents)
	const { data: parentTask } = useQuery({
		...taskQueryOptions(projectId ?? "", task?.parent_task_id ?? ""),
		enabled: !!projectId && !!task?.parent_task_id && (open || mode === "page"),
	});

	// ── Title inline edit ─────────────────────────────────────────────────────
	const [editingTitle, setEditingTitle] = useState(false);
	const [titleDraft, setTitleDraft] = useState("");
	const titleInputRef = useRef<HTMLTextAreaElement>(null);

	// ── Navigation ────────────────────────────────────────────────────────────
	function navigateToTask(taskId: string) {
		if (!projectId) return;
		navigate({
			to: "/projects/$projectId/tasks/$taskId",
			params: { projectId, taskId },
		});
	}

	function navigateToWorkflow(workflowId: string) {
		if (!projectId) return;
		navigate({
			to: "/projects/$projectId/automation/$workflowId",
			params: { projectId, workflowId },
		});
	}

	// ── Update mutation ────────────────────────────────────────────────────────
	const updateMutation = useMutation({
		mutationFn: (payload: Parameters<typeof updateTask>[2]) => {
			if (!projectId || !task) throw new Error("missing context");

			// Filter out fields that haven't actually changed
			const filtered: typeof payload = {};
			for (const key of Object.keys(payload) as Array<keyof typeof payload>) {
				const newValue = payload[key];
				const oldValue = task[key as keyof typeof task];

				if (key === "tags") {
					const a = newValue as string[] | undefined;
					const b = oldValue as string[] | undefined;
					const aStr = a ? [...a].sort().join(",") : "";
					const bStr = b ? [...b].sort().join(",") : "";
					if (aStr !== bStr) {
						filtered.tags = a;
					}
				} else if (key === "assignee_ids") {
					const a = newValue as string[] | undefined;
					const b = oldValue as string[] | undefined;
					const aStr = a ? [...a].sort().join(",") : "";
					const bStr = b ? [...b].sort().join(",") : "";
					if (aStr !== bStr) {
						filtered.assignee_ids = a;
					}
				} else if (key === "custom_fields") {
					const a = newValue as Record<string, unknown> | undefined;
					const b = oldValue as Record<string, unknown> | undefined;
					if (JSON.stringify(a) !== JSON.stringify(b)) {
						filtered.custom_fields = a;
					}
				} else if (key === "description") {
					const normalizedNewValue = newValue ?? null;
					const aClean = JSON.stringify(
						cleanBlocks(normalizedNewValue as unknown[] | null),
					);
					const bClean = JSON.stringify(
						cleanBlocks(oldValue as unknown[] | null),
					);
					if (
						aClean !== bClean &&
						(newValue === null || Array.isArray(newValue))
					) {
						filtered.description = newValue;
					}
				} else {
					if (newValue !== oldValue) {
						(filtered as Record<string, unknown>)[key] = newValue;
					}
				}
			}

			if (Object.keys(filtered).length === 0) {
				return Promise.resolve(task);
			}

			return updateTask(projectId, task.id, filtered);
		},
		onSuccess: (updated) => {
			if (!projectId) return;
			qc.setQueryData(
				taskQueryOptions(projectId, updated.id).queryKey,
				updated,
			);
			qc.invalidateQueries({
				queryKey: ["projects", projectId],
				predicate: (q) => {
					const key = q.queryKey as string[];
					return key.includes("tasks") || key.includes("backlog-tasks");
				},
			});
		},
	});

	const handleUpdate = canEdit ? updateMutation.mutate : undefined;

	// Close on Escape (modal mode only)
	useEffect(() => {
		if (!open || mode === "page") return;
		const handler = (e: KeyboardEvent) => {
			if (e.key === "Escape") onOpenChange(false);
		};
		document.addEventListener("keydown", handler);
		return () => document.removeEventListener("keydown", handler);
	}, [open, mode, onOpenChange]);

	if (mode === "modal" && !open) return null;

	// Resolve task type icon component
	const TypeIcon = taskType ? getTaskTypeIconComponent(taskType.icon) : null;

	// ── Content ────────────────────────────────────────────────────────────────
	const content = task ? (
		<div className="flex h-full flex-col">
			<TaskHeader
				task={task}
				mode={mode}
				projectName={projectName}
				interactionName={interactionName}
				projectId={projectId}
				taskIdPrefix={taskIdPrefix}
				canDelete={canEdit}
				onClose={() => onOpenChange(false)}
				onDeleted={() => {
					if (mode === "page" && projectId) {
						navigate({
							to: "/projects/$projectId",
							params: { projectId },
						});
					} else {
						onOpenChange(false);
					}
				}}
			/>

			{/* ── Body: stacks on mobile, side-by-side on lg+ ── */}
			<div className="flex flex-col lg:flex-row flex-1 min-w-0 min-h-0 overflow-y-auto lg:overflow-hidden">
				{/* Main content area: no own scroll on mobile (body scrolls), scrollable on lg+ */}
				<div className="lg:flex-1 lg:overflow-y-auto [scrollbar-gutter:stable]">
					<div className="px-4 lg:px-8 py-5 lg:py-7 space-y-6 lg:space-y-8 max-w-3xl mx-auto">
						{/* Type badge + Status chip + Title */}
						<div className="space-y-4">
							<div className="flex items-center gap-2.5 flex-wrap">
								{taskType && (
									<span
										className="inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-bold leading-tight tracking-wide border"
										style={{
											borderColor: taskType.color
												? `${taskType.color}44`
												: "var(--border)",
											backgroundColor: taskType.color
												? `${taskType.color}15`
												: "var(--muted)",
											color: taskType.color ?? "inherit",
										}}
									>
										{TypeIcon && <TypeIcon className="size-3.5 opacity-70" />}
										{taskType.name}
									</span>
								)}
								{status && (
									<span className="inline-flex items-center gap-2 rounded-full border border-border/40 bg-muted/40 px-3 py-1 text-xs font-semibold text-muted-foreground tracking-wide">
										<span
											className="size-1.75 rounded-full shrink-0 ring-2 ring-offset-1 ring-offset-background"
											style={{
												background: status.color ?? "var(--muted-foreground)",
												boxShadow: `0 0 6px ${status.color ?? "var(--muted-foreground)"}40`,
											}}
										/>
										{status.name}
									</span>
								)}
							</div>

							{editingTitle ? (
								<textarea
									ref={titleInputRef}
									value={titleDraft}
									onChange={(e) => setTitleDraft(e.target.value)}
									onBlur={() => {
										setEditingTitle(false);
										const trimmed = titleDraft.trim();
										if (trimmed && trimmed !== task.title)
											handleUpdate?.({ title: trimmed });
									}}
									onKeyDown={(e) => {
										if (e.key === "Enter" && !e.shiftKey) {
											e.preventDefault();
											e.currentTarget.blur();
										}
										if (e.key === "Escape") {
											setEditingTitle(false);
											setTitleDraft(task.title);
										}
									}}
									rows={1}
									className={cn(
										TITLE_CLASSES,
										"resize-none bg-transparent outline-none py-0",
									)}
									data-testid="task-title-input"
								/>
							) : (
								// biome-ignore lint/a11y/useKeyWithClickEvents: inline title click-to-edit
								<h1
									className={cn(
										TITLE_CLASSES,
										canEdit &&
											"cursor-text hover:bg-muted/15 rounded-md px-2 -ml-2 py-1 transition-all duration-150",
									)}
									data-testid="task-title"
									onClick={() => {
										if (!canEdit) return;
										setTitleDraft(task.title);
										setEditingTitle(true);
										setTimeout(() => titleInputRef.current?.focus(), 0);
									}}
								>
									{task.title}
								</h1>
							)}
						</div>

						{/* Properties */}
						<div>
							<h3 className="text-xs font-semibold uppercase tracking-[0.08em] text-muted-foreground/70 mb-3 flex items-center gap-2">
								<span>{t("taskDetail.properties.title")}</span>
								<div className="flex-1 h-px bg-linear-to-r from-border/40 to-transparent" />
							</h3>
							<PropertiesPanel
								task={task}
								status={status}
								taskType={taskType}
								priority={priority}
								assignees={assignees}
								reporter={reporter}
								statuses={statuses}
								taskTypes={taskTypes}
								members={members}
								sprints={sprints}
								projectId={projectId}
								initialCustomFields={customFieldDefs}
								canEdit={canEdit}
								taskRole={taskRole}
								epicTasks={epicTasks}
								epicsPagination={epicsPagination}
								parentTask={parentTask}
								onUpdate={handleUpdate}
								onNavigateToTask={navigateToTask}
							/>
						</div>

						{/* Description */}
						<DescriptionSection
							description={task.description}
							canEdit={canEdit}
							projectId={projectId}
							taskId={task.id}
							onUpdate={handleUpdate}
						/>
						{/* Child tasks section */}
						<SubtasksSection
							projectId={projectId}
							parentTaskId={task.id}
							subtasks={subtasks}
							statuses={statuses}
							taskTypes={taskTypes}
							members={members}
							canEdit={canEdit}
							task={task}
							taskIdPrefix={taskIdPrefix}
							normalTaskTypes={normalTaskTypes}
							onSubtaskClick={(sub) => navigateToTask(sub.id)}
							onSubtaskUpdate={(subtaskId, payload) => {
								if (!projectId) return;
								updateTask(projectId, subtaskId, payload).then(() => {
									qc.invalidateQueries({
										queryKey: subtasksQueryOptions(projectId, task.id).queryKey,
									});
								});
							}}
							onSubtaskCreate={(payload) => {
								if (!projectId) return;
								const todoStatus =
									statuses.find((s) => s.category === "todo") ?? statuses[0];
								createTask(projectId, {
									...payload,
									status_id: todoStatus?.id ?? payload.status_id ?? null,
									parent_task_id: task.id,
								}).then(() => {
									qc.invalidateQueries({
										queryKey: subtasksQueryOptions(projectId, task.id).queryKey,
									});
								});
							}}
						/>

						{/* Linked tasks */}
						{projectId && (
							<TaskLinksSection
								projectId={projectId}
								taskId={task.id}
								taskIdPrefix={taskIdPrefix}
								canEdit={canEdit}
								onNavigateToTask={navigateToTask}
							/>
						)}

						{/* Workflows this task belongs to */}
						{projectId && (
							<WorkflowsSection
								projectId={projectId}
								taskId={task.id}
								onNavigateToWorkflow={navigateToWorkflow}
							/>
						)}

						{/* Plugin extension points */}
						{projectId && (
							<ExtensionPoint
								point="task.detail.section"
								componentProps={{ projectId, taskId: task.id, canEdit }}
							/>
						)}

						{/* Attachments */}
						<AttachmentsSection
							projectId={projectId ?? ""}
							taskId={task.id}
							canEdit={canEdit}
						/>

						{/* Bottom breathing room */}
						<div className="h-8" />
					</div>
				</div>

				{/* ── Right: Activity pane ── */}
				<ActivityPane
					projectId={projectId ?? ""}
					taskId={task?.id ?? ""}
					canEdit={canEdit}
				/>
			</div>
		</div>
	) : (
		<div className="flex h-full items-center justify-center">
			<div className="flex flex-col items-center gap-4 text-muted-foreground/70">
				<div className="flex size-14 items-center justify-center rounded-xl bg-muted/50">
					<AlertCircle className="size-7 text-muted-foreground/60" />
				</div>
				<div className="text-center">
					<p className="text-lg font-semibold text-foreground/80">
						{t("taskDetail.notFound.title")}
					</p>
					<p className="text-base mt-1.5 text-muted-foreground/70">
						{t("taskDetail.notFound.description")}
					</p>
				</div>
			</div>
		</div>
	);

	if (mode === "page") {
		return (
			<div className="flex h-full flex-col overflow-hidden bg-background">
				{content}
			</div>
		);
	}

	return (
		<>
			{/* Backdrop */}
			<div
				className={cn(
					"fixed inset-0 z-50 bg-black/30 backdrop-blur-[3px] transition-opacity duration-200",
					open ? "opacity-100" : "opacity-0 pointer-events-none",
				)}
				onClick={() => onOpenChange(false)}
				aria-hidden="true"
			/>

			{/* Modal panel */}
			<div
				role="dialog"
				aria-modal="true"
				aria-label={task?.title ?? t("taskDetail.notFound.ariaLabel")}
				className={cn(
					"fixed z-50",
					"inset-0 lg:left-1/2 lg:top-1/2 lg:-translate-x-1/2 lg:-translate-y-1/2",
					"flex flex-col overflow-hidden",
					"lg:h-[90vh] lg:w-[92vw] lg:max-w-6xl",
					"lg:rounded-xl lg:border lg:border-border/50 bg-background",
					"lg:shadow-[0_25px_60px_-12px_rgba(0,0,0,0.25),0_0_0_1px_rgba(255,255,255,0.05)_inset]",
					"transition-all duration-200 origin-center",
					open
						? "opacity-100 scale-100"
						: "opacity-0 scale-[0.97] pointer-events-none",
				)}
			>
				{content}
			</div>
		</>
	);
}
