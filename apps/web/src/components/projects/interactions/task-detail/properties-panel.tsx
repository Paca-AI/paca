import {
	ArrowRight,
	BookOpen,
	Check,
	Clock,
	ExternalLink,
	KanbanSquare,
	Layers,
	Link2,
	Plus,
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
import type { ProjectMember, TaskStatus, TaskType } from "@/lib/project-api";
import { getTaskTypeIconComponent } from "../../task-types/task-type-icons";
import type { PriorityMeta } from "../priority";
import {
	getImportanceBucket,
	getPriority,
	IMPORTANCE_BUCKET_VALUES,
	PRIORITY_LEVELS,
} from "../priority";
import { TaskTypeSelector } from "../task-type-selector";
import { AddFieldDialog } from "./add-field-dialog";
import { FieldRow, FieldValue } from "./primitives";
import type { SelectOption, UserOption } from "./property-field";
import { PropertyField } from "./property-field";
import { NumberEditor } from "./property-field/number-editor";
import { StoryPointsEditor } from "./property-field/story-points-editor";
import type { CustomFieldDef } from "./types";

/** Maps a priority bucket index (0-4) to its translation key. */
const PRIORITY_LABEL_KEYS = [
	"none",
	"low",
	"medium",
	"high",
	"critical",
] as const;

type UpdatePayload = Partial<{
	status_id: string | null;
	task_type_id: string | null;
	assignee_id: string | null;
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
	assignee: ProjectMember | undefined;
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
	};
}

export function PropertiesPanel({
	task,
	status,
	taskType,
	assignee,
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
	parentTask,
	onUpdate,
	onNavigateToTask,
}: PropertiesPanelProps) {
	const { t } = useTranslation();
	const [localCustomFields, setLocalCustomFields] =
		useState<CustomFieldDef[]>(initialCustomFields);
	const [addFieldOpen, setAddFieldOpen] = useState(false);

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
			label: t("common.productBacklog"),
			icon: <BookOpen className="size-3 shrink-0 opacity-60" />,
		},
		...sprints.map((s) => ({
			value: s.id,
			label: s.name,
			icon: (
				<KanbanSquare className="size-3 shrink-0 text-muted-foreground/70" />
			),
		})),
	];

	const memberUserOptions: UserOption[] = members.map(toUserOption);
	const assigneeUserOption = assignee ? toUserOption(assignee) : null;
	const reporterUserOption = reporter ? toUserOption(reporter) : null;

	return (
		<>
			<div className="divide-y divide-border/20 rounded-xl border border-border/30 bg-card/50 px-4 py-0.5">
				<PropertyField
					label={t("project.interactions.taskDetail.fields.status")}
					mode="select"
					value={status?.id}
					options={statusOptions}
					onChange={(v) =>
						onUpdate?.({ status_id: typeof v === "string" ? v : null })
					}
					canEdit={canEdit && statuses.length > 0}
				/>

				<PropertyField
					label={t("project.interactions.taskDetail.fields.dates")}
					mode="date-range"
					startDate={task.start_date}
					dueDate={task.due_date}
					onStartDateChange={(v) => onUpdate?.({ start_date: v })}
					onDueDateChange={(v) => onUpdate?.({ due_date: v })}
					canEdit={canEdit}
				/>

				<FieldRow label={t("project.interactions.taskDetail.fields.trackTime")}>
					<button
						type="button"
						className="inline-flex items-center gap-1.5 text-sm text-muted-foreground/70 hover:text-foreground transition-colors duration-150 font-medium"
					>
						<Clock className="size-3.5 opacity-70" />
						{t("project.interactions.taskDetail.fields.addTime")}
					</button>
				</FieldRow>

				{(taskType || (canEdit && taskTypes.length > 0)) && (
					<FieldRow label={t("project.interactions.taskDetail.fields.type")}>
						<TaskTypeSelector
							taskTypes={taskTypes}
							value={taskType?.id}
							canEdit={canEdit && taskTypes.length > 0}
							onChange={(id) => onUpdate?.({ task_type_id: id })}
						/>
					</FieldRow>
				)}

				<FieldRow
					label={t("project.interactions.taskDetail.fields.relationships")}
				>
					<button
						type="button"
						className="inline-flex items-center gap-1.5 text-sm text-muted-foreground/70 hover:text-foreground transition-colors duration-150 font-medium"
					>
						<Link2 className="size-3.5 opacity-70" />
						<FieldValue empty />
					</button>
				</FieldRow>

				<PropertyField
					label={t("project.interactions.taskDetail.fields.assignees")}
					mode="user"
					userValue={assigneeUserOption}
					users={memberUserOptions}
					onUserChange={(v) => onUpdate?.({ assignee_id: v })}
					canEdit={canEdit && members.length > 0}
					showUnassigned
				/>

				<FieldRow label={t("project.interactions.taskDetail.fields.importance")}>
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
												{t(
													`project.interactions.taskDetail.priorities.${PRIORITY_LABEL_KEYS[level.value]}`,
												)}
											</>
										) : (
											t("project.interactions.taskDetail.priorities.none")
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
													{t(
														`project.interactions.taskDetail.priorities.${PRIORITY_LABEL_KEYS[level.value]}`,
													)}
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
							const bucket = getImportanceBucket(task.importance ?? 0);
							const p = getPriority(task.importance ?? 0);
							return (
								<div className="flex items-center gap-2 text-sm font-medium">
									<span
										className="size-2 rounded-full shrink-0"
										style={{ background: p.color }}
									/>
									<span style={{ color: p.color }}>
										{t(
											`project.interactions.taskDetail.priorities.${PRIORITY_LABEL_KEYS[bucket]}`,
										)}
									</span>
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

				<FieldRow
					label={t("project.interactions.taskDetail.fields.storyPoints")}
				>
					{canEdit ? (
						<StoryPointsEditor
							value={task.story_points ?? null}
							onChange={(v) => onUpdate?.({ story_points: v })}
						/>
					) : (
						<span className="text-sm font-medium tabular-nums">
							{task.story_points != null ? task.story_points : "—"}
						</span>
					)}
				</FieldRow>

				<PropertyField
					label={t("project.interactions.taskDetail.fields.tags")}
					mode="tags"
					tags={task.tags ?? []}
					onTagsChange={(t) => onUpdate?.({ tags: t })}
					canEdit={canEdit}
				/>

				<PropertyField
					label={t("project.interactions.taskDetail.fields.reporter")}
					mode="user"
					userValue={reporterUserOption}
					users={[]}
					canEdit={false}
					hidden={!reporter}
				/>

				<PropertyField
					label={t("project.interactions.taskDetail.fields.sprint")}
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
						const epic = task.parent_task_id
							? epicTasks.find((e) => e.id === task.parent_task_id)
							: undefined;
						const otherEpics = epicTasks.filter(
							(e) => e.id !== task.parent_task_id,
						);
						const hasActions =
							(epic && onNavigateToTask) || (!!task.parent_task_id && canEdit);
						return (
							<FieldRow label={t("project.interactions.taskDetail.fields.epic")}>
								<DropdownMenu>
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
												{t("project.interactions.taskDetail.none")}
											</span>
										)}
									</DropdownMenuTrigger>
									<DropdownMenuContent align="start" className="w-64">
										{epic && onNavigateToTask && (
											<DropdownMenuItem
												onClick={() => onNavigateToTask(epic.id)}
											>
												<ExternalLink className="size-3.5 mr-2 shrink-0" />
												{t("project.interactions.taskDetail.viewEpic")}
											</DropdownMenuItem>
										)}
										{task.parent_task_id && canEdit && (
											<DropdownMenuItem
												className="text-destructive focus:text-destructive"
												onClick={() => onUpdate?.({ parent_task_id: null })}
											>
												<X className="size-3.5 mr-2 shrink-0" />
												{t("project.interactions.taskDetail.removeEpic")}
											</DropdownMenuItem>
										)}
										{hasActions && otherEpics.length > 0 && (
											<DropdownMenuSeparator />
										)}
										{otherEpics.map((e) => (
											<DropdownMenuItem
												key={e.id}
												onClick={() => onUpdate?.({ parent_task_id: e.id })}
											>
												<Layers className="size-3.5 mr-2 shrink-0 text-violet-500/80" />
												<span className="truncate">{e.title}</span>
											</DropdownMenuItem>
										))}
									</DropdownMenuContent>
								</DropdownMenu>
							</FieldRow>
						);
					})()}
				{/* Parent field – shown when the parent task is not an epic (story/task/bug nesting) */}
				{taskRole === "normal" &&
					task.parent_task_id &&
					parentTask &&
					!epicTasks.find((e) => e.id === task.parent_task_id) && (
						<FieldRow label={t("project.interactions.taskDetail.fields.parent")}>
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
													{t("project.interactions.taskDetail.viewParent")}
												</DropdownMenuItem>
											)}
											{canEdit && (
												<DropdownMenuItem
													className="text-destructive focus:text-destructive"
													onClick={() => onUpdate?.({ parent_task_id: null })}
												>
													<X className="size-3.5 mr-2 shrink-0" />
													{t("project.interactions.taskDetail.removeParent")}
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
					{t("project.interactions.taskDetail.addFields")}
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
