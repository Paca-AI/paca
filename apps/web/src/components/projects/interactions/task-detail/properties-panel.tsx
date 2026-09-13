import {
	ArrowRight,
	BookOpen,
	Check,
	ExternalLink,
	KanbanSquare,
	Layers,
	Loader2,
	Plus,
	Search,
	X,
} from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import type { Sprint, Task } from "@/lib/interaction-api";
import {
	findEpicType,
	type ProjectMember,
	type TaskStatus,
	type TaskType,
} from "@/lib/project-api";
import { resolveMemberAvatarUrl } from "@/lib/provider-logos";
import { getTaskTypeIconComponent } from "../../task-types/task-type-icons";
import type { PriorityMeta } from "../priority";
import {
	getImportanceBucket,
	getPriority,
	IMPORTANCE_BUCKET_VALUES,
	PRIORITY_LEVELS,
} from "../priority";
import { TaskTypeSelector } from "../task-type-selector";
import { useEpicSearch } from "../use-epic-search";
import { createEpicScrollHandler, type EpicsPagination } from "../view-utils";
import { AddFieldDialog } from "./add-field-dialog";
import { FieldRow } from "./primitives";
import type { SelectOption, UserOption } from "./property-field";
import { PropertyField } from "./property-field";
import { NumberEditor } from "./property-field/number-editor";
import { StoryPointsEditor } from "./property-field/story-points-editor";
import type { CustomFieldDef } from "./types";

type UpdatePayload = Partial<{
	status_id: string | null;
	task_type_id: string | null;
	assignee_ids: string[];
	reporter_id: string | null;
	importance: number;
	story_points: number | null;
	start_date: string | null;
	due_date: string | null;
	tags: string[];
	sprint_id: string | null;
	parent_task_id: string | null;
	custom_fields: Record<string, unknown>;
}>;

interface PropertiesPanelProps {
	task: Task;
	status: TaskStatus | undefined;
	taskType: TaskType | undefined;
	priority: PriorityMeta;
	assignees: ProjectMember[];
	reporter: ProjectMember | undefined;
	statuses?: TaskStatus[];
	taskTypes?: TaskType[];
	members?: ProjectMember[];
	sprints?: Sprint[];
	projectId?: string;
	initialCustomFields?: CustomFieldDef[];
	canEdit?: boolean;
	/** Role of the current task: "epic" or "normal" */
	taskRole?: "epic" | "normal";
	/** All epic tasks in the project, for the epic picker on normal tasks */
	epicTasks?: Task[];
	/** Load-more state for the epic picker's paginated (20-per-page) query */
	epicsPagination?: EpicsPagination;
	/** The resolved parent task object (when parent_task_id is set) */
	parentTask?: Task;
	onUpdate?: (payload: UpdatePayload) => void;
	/** Navigate to a task's detail page */
	onNavigateToTask?: (taskId: string) => void;
}

function toUserOption(m: ProjectMember): UserOption {
	return {
		value: m.id,
		label: m.full_name || m.username,
		initials: (m.full_name || m.username).slice(0, 1).toUpperCase(),
		avatarUrl: resolveMemberAvatarUrl(m),
	};
}

export function PropertiesPanel({
	task,
	status,
	taskType,
	assignees,
	reporter,
	statuses = [],
	taskTypes = [],
	members = [],
	sprints = [],
	projectId,
	initialCustomFields = [],
	canEdit = true,
	taskRole = "normal",
	epicTasks = [],
	epicsPagination,
	parentTask,
	onUpdate,
	onNavigateToTask,
}: PropertiesPanelProps) {
	const { t } = useTranslation("projects");
	const [localCustomFields, setLocalCustomFields] =
		useState<CustomFieldDef[]>(initialCustomFields);
	const [addFieldOpen, setAddFieldOpen] = useState(false);
	const [epicMenuOpen, setEpicMenuOpen] = useState(false);
	const epicTypeId = findEpicType(taskTypes)?.id;
	const {
		search: epicSearch,
		setSearch: setEpicSearch,
		isSearching: isEpicSearching,
		results: epicSearchResults,
		isLoading: epicSearchLoading,
		pagination: epicSearchPagination,
	} = useEpicSearch(task.project_id, epicTypeId, epicMenuOpen);

	useEffect(() => {
		setLocalCustomFields(initialCustomFields);
	}, [initialCustomFields]);

	const statusOptions: SelectOption[] = statuses.map((s) => ({
		value: s.id,
		label: s.name,
		colorDot: s.color ?? undefined,
	}));

	const sprintOptions: SelectOption[] = [
		{
			value: "__backlog__",
			label: t("taskDetail.properties.productBacklog"),
			icon: <BookOpen className="size-3 shrink-0 opacity-60" />,
		},
		...sprints.map((s) => ({
			value: s.id,
			label: s.name,
			icon: (
				<KanbanSquare className="size-3 shrink-0 text-muted-foreground/70" />
			),
			hint:
				s.status === "planned"
					? t("taskDetail.properties.sprintNotStarted")
					: undefined,
		})),
	];

	const memberUserOptions: UserOption[] = members.map(toUserOption);
	const assigneeUserOptions: UserOption[] = assignees.map(toUserOption);
	const reporterUserOption = reporter ? toUserOption(reporter) : null;

	return (
		<>
			<div className="divide-y divide-border/20 rounded-xl border border-border/30 bg-card/50 px-4 py-0.5">
				<PropertyField
					label={t("taskDetail.properties.status")}
					mode="select"
					value={status?.id}
					options={statusOptions}
					onChange={(v) =>
						onUpdate?.({ status_id: typeof v === "string" ? v : null })
					}
					canEdit={canEdit && statuses.length > 0}
				/>

				<PropertyField
					label={t("taskDetail.properties.dates")}
					mode="date-range"
					startDate={task.start_date}
					dueDate={task.due_date}
					onStartDateChange={(v) => onUpdate?.({ start_date: v })}
					onDueDateChange={(v) => onUpdate?.({ due_date: v })}
					canEdit={canEdit}
				/>

				{(taskType || (canEdit && taskTypes.length > 0)) && (
					<FieldRow label={t("taskDetail.properties.type")}>
						<TaskTypeSelector
							taskTypes={taskTypes}
							value={taskType?.id}
							canEdit={canEdit && taskTypes.length > 0}
							onChange={(id) => onUpdate?.({ task_type_id: id })}
						/>
					</FieldRow>
				)}

				<PropertyField
					label={t("taskDetail.properties.assignees")}
					mode="multi-user"
					userValues={assigneeUserOptions}
					users={memberUserOptions}
					onUsersChange={(v) => onUpdate?.({ assignee_ids: v })}
					canEdit={canEdit && members.length > 0}
				/>

				<FieldRow label={t("taskDetail.properties.importance")}>
					{canEdit ? (
						<div className="flex items-center gap-2 flex-wrap">
							<DropdownMenu>
								<DropdownMenuTrigger className="inline-flex items-center gap-1.5 rounded-full border border-border/30 bg-muted/30 px-3 py-1 text-sm font-semibold text-muted-foreground hover:bg-muted/50 hover:border-border/50 transition-all duration-150">
									{(() => {
										const bucket = getImportanceBucket(task.importance ?? 0);
										const level = PRIORITY_LEVELS.find(
											(l) => l.value === bucket,
										);
										return level ? (
											<>
												<span
													className="size-1.5 rounded-full shrink-0"
													style={{ background: level.color }}
												/>
												{t(level.labelKey)}
											</>
										) : (
											t("taskDetail.properties.none")
										);
									})()}
								</DropdownMenuTrigger>
								<DropdownMenuContent align="start">
									{PRIORITY_LEVELS.map((level) => {
										const currentBucket = getImportanceBucket(
											task.importance ?? 0,
										);
										return (
											<DropdownMenuItem
												key={level.value}
												onClick={() =>
													onUpdate?.({
														importance:
															IMPORTANCE_BUCKET_VALUES[level.value] ?? 0,
													})
												}
											>
												<span
													className="size-2 rounded-full shrink-0 mr-2"
													style={{ background: level.color }}
												/>
												<span style={{ color: level.color }}>
													{t(level.labelKey)}
												</span>
												{currentBucket === level.value && (
													<Check className="size-3.5 text-primary ml-auto" />
												)}
											</DropdownMenuItem>
										);
									})}
								</DropdownMenuContent>
							</DropdownMenu>
							<NumberEditor
								key={task.importance ?? 0}
								value={task.importance ?? 0}
								onChange={(v) => onUpdate?.({ importance: v })}
							/>
						</div>
					) : (
						(() => {
							const p = getPriority(task.importance ?? 0);
							return (
								<div className="flex items-center gap-2 text-sm font-medium">
									<span
										className="size-2 rounded-full shrink-0"
										style={{ background: p.color }}
									/>
									<span style={{ color: p.color }}>{t(p.labelKey)}</span>
									{(task.importance ?? 0) > 0 && (
										<span className="text-muted-foreground tabular-nums">
											({task.importance})
										</span>
									)}
								</div>
							);
						})()
					)}
				</FieldRow>

				<FieldRow label={t("taskDetail.properties.storyPoints")}>
					{canEdit ? (
						<StoryPointsEditor
							value={task.story_points ?? null}
							onChange={(v) => onUpdate?.({ story_points: v })}
						/>
					) : (
						<span className="text-sm font-medium tabular-nums">
							{task.story_points != null
								? task.story_points
								: t("taskDetail.common.dash")}
						</span>
					)}
				</FieldRow>

				<PropertyField
					label={t("taskDetail.properties.tags")}
					mode="tags"
					tags={task.tags ?? []}
					onTagsChange={(tags) => onUpdate?.({ tags })}
					canEdit={canEdit}
				/>

				<PropertyField
					label={t("taskDetail.properties.reporter")}
					mode="user"
					userValue={reporterUserOption}
					users={[]}
					canEdit={false}
					hidden={!reporter}
				/>

				<PropertyField
					label={t("taskDetail.properties.sprint")}
					mode="select"
					value={task.sprint_id ?? "__backlog__"}
					options={sprintOptions}
					onChange={(v) =>
						onUpdate?.({
							sprint_id:
								v === "__backlog__" ? null : typeof v === "string" ? v : null,
						})
					}
					canEdit={canEdit && sprints.length > 0}
					hidden={!task.sprint_id && !(canEdit && sprints.length > 0)}
				/>

				{/* Epic field – normal tasks only */}
				{taskRole === "normal" &&
					(epicTasks.length > 0 || task.parent_task_id) &&
					(() => {
						// epicTasks is paginated 20-per-page, so a task's own epic may
						// not be loaded into it yet — fall back to parentTask (fetched
						// directly for this task regardless of pagination) when it's
						// actually Epic-typed, so the field doesn't render as unset.
						const epic = task.parent_task_id
							? (epicTasks.find((e) => e.id === task.parent_task_id) ??
								(parentTask?.id === task.parent_task_id &&
								parentTask.task_type_id === epicTypeId
									? parentTask
									: undefined))
							: undefined;
						const otherEpics = epicTasks.filter(
							(e) => e.id !== task.parent_task_id,
						);
						const displayedEpics = isEpicSearching
							? epicSearchResults.filter((e) => e.id !== task.parent_task_id)
							: otherEpics;
						const activeEpicsPagination = isEpicSearching
							? epicSearchPagination
							: epicsPagination;
						const hasActions =
							(epic && onNavigateToTask) || (!!task.parent_task_id && canEdit);
						return (
							<FieldRow label={t("taskDetail.properties.epic")}>
								<DropdownMenu
									open={epicMenuOpen}
									onOpenChange={setEpicMenuOpen}
								>
									<DropdownMenuTrigger className="inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-sm font-medium hover:bg-muted/50 transition-colors duration-150 cursor-pointer -ml-2 max-w-52 truncate">
										{epic ? (
											<>
												<Layers className="size-3.5 shrink-0 text-violet-500/80" />
												<span className="truncate text-foreground/80">
													{epic.title}
												</span>
											</>
										) : (
											<span className="text-muted-foreground/50 italic">
												{t("taskDetail.properties.none")}
											</span>
										)}
									</DropdownMenuTrigger>
									<DropdownMenuContent align="start" className="w-64">
										{epic && onNavigateToTask && (
											<DropdownMenuItem
												onClick={() => onNavigateToTask(epic.id)}
											>
												<ExternalLink className="size-3.5 mr-2 shrink-0" />
												{t("taskDetail.properties.viewEpic")}
											</DropdownMenuItem>
										)}
										{task.parent_task_id && canEdit && (
											<DropdownMenuItem
												className="text-destructive focus:text-destructive"
												onClick={() => onUpdate?.({ parent_task_id: null })}
											>
												<X className="size-3.5 mr-2 shrink-0" />
												{t("taskDetail.properties.removeEpic")}
											</DropdownMenuItem>
										)}
										{hasActions && <DropdownMenuSeparator />}
										<div className="px-1 pb-1">
											<div className="relative">
												<Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground/50" />
												<input
													type="text"
													value={epicSearch}
													onChange={(e) => setEpicSearch(e.target.value)}
													onKeyDown={(e) => {
														// Let Escape still close the menu; swallow everything
														// else so typing doesn't trigger the menu's own arrow-key
														// navigation / type-ahead item selection.
														if (e.key !== "Escape") e.stopPropagation();
													}}
													placeholder={t("epicPicker.searchPlaceholder")}
													className="w-full rounded-lg border border-border/30 bg-muted/25 py-1.5 pr-2 pl-8 text-sm placeholder:text-muted-foreground/50 transition-all duration-150 focus:border-primary/40 focus:outline-none focus:ring-2 focus:ring-primary/20"
												/>
											</div>
										</div>
										<div
											className="max-h-56 overflow-y-auto"
											onScroll={createEpicScrollHandler(activeEpicsPagination)}
										>
											{isEpicSearching && epicSearchLoading ? (
												<div className="flex items-center justify-center py-4">
													<Loader2 className="size-4 animate-spin text-muted-foreground/50" />
												</div>
											) : (
												<>
													{displayedEpics.map((e) => (
														<DropdownMenuItem
															key={e.id}
															onClick={() =>
																onUpdate?.({ parent_task_id: e.id })
															}
														>
															<Layers className="size-3.5 mr-2 shrink-0 text-violet-500/80" />
															<span className="truncate">{e.title}</span>
														</DropdownMenuItem>
													))}
													{displayedEpics.length === 0 && (
														<p className="px-2 py-4 text-center text-xs text-muted-foreground/50">
															{isEpicSearching
																? t("epicPicker.noEpicsFound")
																: t("epicPicker.noEpicsYet")}
														</p>
													)}
												</>
											)}
											{activeEpicsPagination?.isLoadingMore && (
												<div className="flex items-center justify-center gap-1.5 py-2 text-xs text-muted-foreground/50">
													<Loader2 className="size-3 animate-spin" />
													{t("epicPicker.loadingMore")}
												</div>
											)}
										</div>
									</DropdownMenuContent>
								</DropdownMenu>
							</FieldRow>
						);
					})()}
				{/* Parent field – shown when the parent task is not an epic (story/task/bug nesting).
				 * Checked against parentTask's own type rather than epicTasks membership —
				 * epicTasks is paginated, so an actual epic parent can be legitimately
				 * absent from it without meaning "this parent isn't an epic". */}
				{taskRole === "normal" &&
					task.parent_task_id &&
					parentTask &&
					parentTask.task_type_id !== epicTypeId && (
						<FieldRow label={t("taskDetail.properties.parent")}>
							{(() => {
								const parentType = taskTypes.find(
									(tt) => tt.id === parentTask.task_type_id,
								);
								const ParentIcon = parentType
									? getTaskTypeIconComponent(parentType.icon)
									: null;
								return (
									<DropdownMenu>
										<DropdownMenuTrigger className="inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-sm font-medium hover:bg-muted/50 transition-colors duration-150 cursor-pointer -ml-2 max-w-52 truncate">
											{ParentIcon ? (
												<ParentIcon className="size-3.5 shrink-0 text-muted-foreground/80" />
											) : (
												<ArrowRight className="size-3.5 shrink-0 opacity-60" />
											)}
											<span className="truncate text-foreground/80">
												{parentTask.title}
											</span>
										</DropdownMenuTrigger>
										<DropdownMenuContent align="start" className="w-56">
											{onNavigateToTask && (
												<DropdownMenuItem
													onClick={() => onNavigateToTask(parentTask.id)}
												>
													<ExternalLink className="size-3.5 mr-2 shrink-0" />
													{t("taskDetail.properties.viewParent")}
												</DropdownMenuItem>
											)}
											{canEdit && (
												<DropdownMenuItem
													className="text-destructive focus:text-destructive"
													onClick={() => onUpdate?.({ parent_task_id: null })}
												>
													<X className="size-3.5 mr-2 shrink-0" />
													{t("taskDetail.properties.removeParent")}
												</DropdownMenuItem>
											)}
										</DropdownMenuContent>
									</DropdownMenu>
								);
							})()}
						</FieldRow>
					)}

				{localCustomFields.map((cf) => (
					<PropertyField
						key={cf.id}
						label={cf.display_name}
						mode="custom"
						customType={cf.field_type}
						customRawValue={task.custom_fields?.[cf.field_key]}
						onCustomChange={(v) => {
							onUpdate?.({
								custom_fields: {
									...task.custom_fields,
									[cf.field_key]: v,
								},
							});
						}}
						customOptions={cf.options}
						canEdit={canEdit}
					/>
				))}
			</div>

			{canEdit && (
				<button
					type="button"
					onClick={() => setAddFieldOpen(true)}
					className="mt-3 flex items-center gap-2 text-sm text-muted-foreground/60 hover:text-muted-foreground transition-colors duration-150 font-medium"
				>
					<Plus className="size-3.5" />
					{t("taskDetail.properties.addFields")}
				</button>
			)}

			<AddFieldDialog
				open={addFieldOpen}
				onOpenChange={setAddFieldOpen}
				projectId={projectId}
				onAdd={(field) => setLocalCustomFields((prev) => [...prev, field])}
			/>
		</>
	);
}
