import { useQuery } from "@tanstack/react-query";
import {
	Check,
	ChevronDown,
	ChevronRight,
	GripVertical,
	Settings,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { getTaskTypeIconComponent } from "@/components/projects/task-types/task-type-icons";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import {
	type FilterConfig,
	type FilterEntry,
	type InteractionView,
	sprintsQueryOptions,
	type ViewConfig,
	type ViewFilters,
} from "@/lib/interaction-api";
import {
	type CustomFieldDefinition,
	customFieldsQueryOptions,
	projectMembersQueryOptions,
	STATUS_CATEGORIES,
	type StatusCategory,
	type TaskStatus,
	type TaskType,
	taskStatusesQueryOptions,
	taskTypesQueryOptions,
} from "@/lib/project-api";
import { cn } from "@/lib/utils";

import {
	buildAllFieldOptions,
	buildColumnByOptions,
	buildFieldSumOptions,
	buildSortByOptions,
	buildSwimlaneOptions,
	DEFAULT_VISIBLE_FIELDS,
	getDefaultInitialPageSize,
	getDefaultPageSize,
	PAGE_SIZE_OPTIONS,
} from "./view-utils";

// ── Category visual config ────────────────────────────────────────────────────

const CATEGORY_COLORS: Record<StatusCategory, string> = {
	backlog: "bg-muted-foreground/40",
	refinement: "bg-violet-400/70",
	ready: "bg-sky-400/70",
	todo: "bg-amber-400/70",
	inprogress: "bg-primary/70",
	done: "bg-emerald-500/70",
};

// ── Primitives ────────────────────────────────────────────────────────────────

function PanelSectionHeader({
	children,
	action,
}: {
	children: React.ReactNode;
	action?: React.ReactNode;
}) {
	return (
		<div className="flex items-center justify-between px-3 pt-3.5 pb-1.5">
			<span className="text-xs font-semibold uppercase tracking-widest text-muted-foreground/50">
				{children}
			</span>
			{action}
		</div>
	);
}

function SettingRow({
	label,
	children,
}: {
	label: string;
	children: React.ReactNode;
}) {
	return (
		<div className="flex items-center gap-3 px-3 py-1.5">
			<span className="w-19 shrink-0 text-xs font-medium text-muted-foreground/80">
				{label}
			</span>
			{children}
		</div>
	);
}

function DynamicSelect({
	value,
	options,
	onChange,
}: {
	value: string | undefined;
	options: { key: string; label: string }[];
	onChange: (v: string | undefined) => void;
}) {
	const selected = options.find((o) => o.key === value);
	return (
		<Popover>
			<PopoverTrigger
				type="button"
				className="flex flex-1 items-center justify-between gap-1.5 cursor-pointer rounded-md border border-border/30 bg-background px-2 py-1 text-xs font-medium outline-none transition-all hover:border-border/50 focus:border-primary/50 focus:ring-1 focus:ring-primary/20 min-w-0"
			>
				<span className="truncate">{selected?.label ?? "—"}</span>
				<ChevronDown className="size-3 shrink-0 text-muted-foreground/50" />
			</PopoverTrigger>
			<PopoverContent
				className="w-44 p-1 rounded-lg border border-border/40 shadow-lg"
				align="start"
			>
				{options.map((o) => (
					<button
						key={o.key}
						type="button"
						className="flex w-full items-center gap-2 rounded-md px-2.5 py-1.5 text-xs hover:bg-muted/60 transition-colors duration-100"
						onClick={() => onChange(o.key)}
					>
						<span className="flex-1 text-left truncate">{o.label}</span>
						{o.key === value && (
							<Check className="size-3.5 text-primary shrink-0" />
						)}
					</button>
				))}
			</PopoverContent>
		</Popover>
	);
}

// ── Filter pill ───────────────────────────────────────────────────────────────

function FilterPill({
	selected,
	onClick,
	children,
}: {
	selected: boolean;
	onClick: () => void;
	children: React.ReactNode;
}) {
	return (
		<button
			type="button"
			onClick={onClick}
			className={cn(
				"inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-xs font-medium transition-all duration-100",
				selected
					? "border-primary/30 bg-primary/8 text-primary"
					: "border-border/30 bg-transparent text-muted-foreground hover:border-border/60 hover:text-foreground",
			)}
		>
			{selected && <Check className="size-2.5 shrink-0" />}
			{children}
		</button>
	);
}

// ── Checklist row ─────────────────────────────────────────────────────────────

function CheckRow({
	id,
	label,
	checked,
	onChange,
	indeterminate,
	bold,
	dot,
	icon,
}: {
	id: string;
	label: string;
	checked: boolean;
	onChange: () => void;
	indeterminate?: boolean;
	bold?: boolean;
	dot?: React.ReactNode;
	icon?: React.ReactNode;
}) {
	const ref = useRef<HTMLInputElement>(null);
	useEffect(() => {
		if (ref.current) ref.current.indeterminate = !!indeterminate;
	}, [indeterminate]);

	return (
		<label
			htmlFor={`check-${id}`}
			className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-xs transition-colors hover:bg-muted/40"
		>
			<input
				ref={ref}
				id={`check-${id}`}
				type="checkbox"
				checked={checked}
				onChange={onChange}
				className="size-3.5 cursor-pointer rounded accent-primary shrink-0"
			/>
			{dot}
			{icon}
			<span className={cn("truncate", bold && "font-semibold")}>{label}</span>
		</label>
	);
}

// ── Field picker ──────────────────────────────────────────────────────────────

interface FieldPickerProps {
	visibleFields: string[];
	customFields: CustomFieldDefinition[];
	onChange: (fields: string[]) => void;
}

function FieldPicker({
	visibleFields,
	customFields,
	onChange,
}: FieldPickerProps) {
	const { t } = useTranslation();
	const allFields = buildAllFieldOptions(customFields, t);
	const dragRef = useRef<string | null>(null);

	const toggle = (key: string) => {
		if (visibleFields.includes(key)) {
			onChange(visibleFields.filter((f) => f !== key));
		} else {
			onChange([...visibleFields, key]);
		}
	};

	const handleDragStart = (key: string) => {
		dragRef.current = key;
	};

	const handleDrop = (targetKey: string) => {
		const src = dragRef.current;
		if (!src || src === targetKey) return;
		const next = [...visibleFields];
		const si = next.indexOf(src);
		const ti = next.indexOf(targetKey);
		if (si !== -1 && ti !== -1) {
			next.splice(si, 1);
			next.splice(ti, 0, src);
			onChange(next);
		}
		dragRef.current = null;
	};

	const enabled = visibleFields
		.map((k) => allFields.find((f) => f.key === k))
		.filter((f): f is { key: string; label: string } => Boolean(f));
	const disabled = allFields.filter((f) => !visibleFields.includes(f.key));

	return (
		<div className="flex flex-col gap-0.5 py-1 max-h-60 overflow-y-auto">
			{enabled.map((f) => (
				// biome-ignore lint/a11y/noStaticElementInteractions: drag-to-reorder row
				<div
					key={f.key}
					draggable
					onDragStart={() => handleDragStart(f.key)}
					onDragOver={(e) => e.preventDefault()}
					onDrop={() => handleDrop(f.key)}
					className="flex cursor-grab items-center gap-2 rounded-lg px-2 py-1.5 hover:bg-muted/40 active:cursor-grabbing"
				>
					<GripVertical className="size-3 shrink-0 text-muted-foreground/40" />
					<input
						type="checkbox"
						id={`field-${f.key}`}
						checked
						onChange={() => toggle(f.key)}
						className="size-3.5 cursor-pointer rounded accent-primary"
					/>
					<label
						htmlFor={`field-${f.key}`}
						className="flex-1 cursor-pointer truncate text-xs font-medium"
					>
						{f.label}
					</label>
				</div>
			))}
			{disabled.length > 0 && (
				<div className="mx-2 my-1 border-t border-border/20" />
			)}
			{disabled.map((f) => (
				<div
					key={f.key}
					className="flex items-center gap-2 rounded-lg px-2 py-1.5 hover:bg-muted/40"
				>
					<div className="size-3 shrink-0" />
					<input
						type="checkbox"
						id={`field-${f.key}`}
						checked={false}
						onChange={() => toggle(f.key)}
						className="size-3.5 cursor-pointer rounded accent-primary"
					/>
					<label
						htmlFor={`field-${f.key}`}
						className="flex-1 cursor-pointer truncate text-xs font-medium text-muted-foreground/60"
					>
						{f.label}
					</label>
				</div>
			))}
		</div>
	);
}

// ── Sprint filter ─────────────────────────────────────────────────────────────

function SprintFilterSection({
	sprints,
	selectedIds,
	onChange,
}: {
	sprints: { id: string; name: string }[];
	selectedIds: string[];
	onChange: (ids: string[]) => void;
}) {
	const { t } = useTranslation();
	const isAll = selectedIds.length === 0;

	if (sprints.length === 0) {
		return (
			<p className="px-3 pb-2 text-xs text-muted-foreground/50">
				{t("project.interactions.viewSettings.sprintFilter.empty")}
			</p>
		);
	}

	const toggle = (id: string) => {
		if (selectedIds.includes(id)) {
			onChange(selectedIds.filter((x) => x !== id));
		} else {
			onChange([...selectedIds, id]);
		}
	};

	return (
		<div className="px-3 pb-3 space-y-1.5">
			<div className="flex flex-wrap gap-1.5">
				<FilterPill selected={isAll} onClick={() => onChange([])}>
					{t("project.interactions.viewSettings.sprintFilter.all")}
				</FilterPill>
				{sprints.map((sprint) => {
					const selected = selectedIds.includes(sprint.id);
					return (
						<FilterPill
							key={sprint.id}
							selected={selected}
							onClick={() => toggle(sprint.id)}
						>
							{sprint.name}
						</FilterPill>
					);
				})}
			</div>
		</div>
	);
}

// ── Status filter ─────────────────────────────────────────────────────────────

function StatusFilterSection({
	statuses,
	selectedIds,
	onChange,
}: {
	statuses: TaskStatus[];
	selectedIds: string[];
	onChange: (ids: string[]) => void;
}) {
	const { t } = useTranslation();
	const allIds = statuses.map((s) => s.id);
	const isAll = selectedIds.length === 0;

	const toggle = (id: string) => {
		if (isAll) {
			onChange(allIds.filter((x) => x !== id));
		} else if (selectedIds.includes(id)) {
			const next = selectedIds.filter((x) => x !== id);
			onChange(next.length === allIds.length ? [] : next);
		} else {
			const next = [...selectedIds, id];
			onChange(next.length === allIds.length ? [] : next);
		}
	};

	const toggleGroup = (groupIds: string[]) => {
		const effectiveIds = isAll ? allIds : selectedIds;
		const allInGroup = groupIds.every((id) => effectiveIds.includes(id));
		if (allInGroup) {
			const next = effectiveIds.filter((id) => !groupIds.includes(id));
			onChange(next.length === allIds.length ? [] : next);
		} else {
			const next = [...effectiveIds];
			for (const id of groupIds) {
				if (!next.includes(id)) next.push(id);
			}
			onChange(next.length === allIds.length ? [] : next);
		}
	};

	if (statuses.length === 0) {
		return (
			<p className="px-3 pb-2 text-xs text-muted-foreground/50">
				{t("project.interactions.viewSettings.statusFilter.empty")}
			</p>
		);
	}

	return (
		<div className="px-1 pb-3 space-y-0.5">
			<CheckRow
				id="status-all"
				label={t("project.interactions.viewSettings.statusFilter.all")}
				checked={isAll}
				bold
				onChange={() => onChange([])}
			/>
			{STATUS_CATEGORIES.map((cat) => {
				const groupStatuses = statuses.filter((s) => s.category === cat);
				if (groupStatuses.length === 0) return null;
				const groupIds = groupStatuses.map((s) => s.id);
				const allChecked =
					isAll || groupIds.every((id) => selectedIds.includes(id));
				return (
					<div key={cat}>
						<div className="flex items-center gap-1.5 px-2 pt-2 pb-0.5">
							<div
								className={cn(
									"size-1.5 rounded-full shrink-0",
									CATEGORY_COLORS[cat],
								)}
							/>
							<span className="text-xs font-semibold uppercase tracking-[0.08em] text-muted-foreground/50">
								{t(`project.statusCategories.${cat}`)}
							</span>
							<button
								type="button"
								onClick={() => toggleGroup(groupIds)}
								className="ml-auto text-xs text-muted-foreground/50 hover:text-primary transition-colors font-medium"
							>
								{allChecked
									? t("project.interactions.viewSettings.statusFilter.clear")
									: t("project.interactions.viewSettings.statusFilter.selectAll")}
							</button>
						</div>
						{groupStatuses.map((status) => (
							<CheckRow
								key={status.id}
								id={`status-${status.id}`}
								label={status.name}
								checked={isAll || selectedIds.includes(status.id)}
								onChange={() => toggle(status.id)}
								dot={
									<span
										className="size-2 shrink-0 rounded-full"
										style={{
											backgroundColor:
												status.color ?? "oklch(var(--muted-foreground))",
										}}
									/>
								}
							/>
						))}
					</div>
				);
			})}
		</div>
	);
}

// ── Assignee filter ───────────────────────────────────────────────────────────

export const UNASSIGNED_FILTER_ID = "__unassigned";

function AssigneeFilterSection({
	members,
	selectedIds,
	onChange,
}: {
	members: { id: string; full_name: string; username: string }[];
	selectedIds: string[];
	onChange: (ids: string[]) => void;
}) {
	const { t } = useTranslation();
	const allFilterIds = [...members.map((m) => m.id), UNASSIGNED_FILTER_ID];
	const isAll = selectedIds.length === 0;

	const toggle = (id: string) => {
		if (isAll) {
			onChange([id]);
		} else if (selectedIds.includes(id)) {
			const next = selectedIds.filter((x) => x !== id);
			onChange(next.length === allFilterIds.length ? [] : next);
		} else {
			const next = [...selectedIds, id];
			onChange(next.length === allFilterIds.length ? [] : next);
		}
	};

	return (
		<div className="px-1 pb-3 space-y-0.5 max-h-44 overflow-y-auto">
			<CheckRow
				id="assignee-all"
				label={t("project.interactions.viewSettings.assigneeFilter.all")}
				checked={isAll}
				bold
				onChange={() => onChange([])}
			/>
			{members.length === 0 ? (
				<p className="px-2 py-1 text-xs text-muted-foreground/50">
					{t("project.interactions.viewSettings.assigneeFilter.empty")}
				</p>
			) : (
				members.map((m) => {
					const display = m.full_name || m.username;
					return (
						<CheckRow
							key={m.id}
							id={`assignee-${m.id}`}
							label={display}
							checked={isAll || selectedIds.includes(m.id)}
							onChange={() => toggle(m.id)}
							icon={
								<div className="flex size-5 shrink-0 items-center justify-center rounded-full bg-linear-to-br from-primary/20 to-primary/10 text-primary text-xs font-bold ring-1 ring-primary/20">
									{display.slice(0, 1).toUpperCase()}
								</div>
							}
						/>
					);
				})
			)}
			<CheckRow
				id="assignee-unassigned"
				label={t("project.interactions.viewSettings.assigneeFilter.unassigned")}
				checked={isAll || selectedIds.includes(UNASSIGNED_FILTER_ID)}
				onChange={() => toggle(UNASSIGNED_FILTER_ID)}
				icon={
					<div className="flex size-5 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground text-xs ring-1 ring-border">
						–
					</div>
				}
			/>
		</div>
	);
}

// ── FilterConfig helpers ──────────────────────────────────────────────────────

function filterConfigToIds(config: FilterConfig | undefined): string[] {
	if (!config || config.all) return [];
	return Object.entries(config.items ?? {})
		.filter(
			([, v]) =>
				v === true || (typeof v === "object" && (v as FilterConfig).all),
		)
		.map(([k]) => k);
}

function idsToFilterConfig(ids: string[]): FilterConfig | undefined {
	if (ids.length === 0) return undefined;
	const items: Record<string, FilterEntry> = {};
	for (const id of ids) items[id] = true;
	return { all: false, items };
}

function taskTypeConfigToUI(config: FilterConfig | undefined): {
	allNormal: boolean;
	selectedIds: string[];
} {
	if (!config) return { allNormal: false, selectedIds: [] };
	const normalEntry = config.items?.normal;
	const allNormal =
		normalEntry === true ||
		(typeof normalEntry === "object" &&
			(normalEntry as FilterConfig).all === true);
	const selectedIds = Object.entries(config.items ?? {})
		.filter(
			([k, v]) =>
				k !== "normal" &&
				(v === true || (typeof v === "object" && (v as FilterConfig).all)),
		)
		.map(([k]) => k);
	return { allNormal, selectedIds };
}

function uiToTaskTypeConfig(
	allNormal: boolean,
	selectedIds: string[],
): FilterConfig | undefined {
	if (!allNormal && selectedIds.length === 0) return undefined;
	const items: Record<string, FilterEntry> = {};
	if (allNormal) items.normal = { all: true };
	for (const id of selectedIds) items[id] = true;
	return { all: false, items };
}

// ── Task type filter ──────────────────────────────────────────────────────────

function TaskTypeFilterSection({
	taskTypes,
	selectedIds,
	allNormal,
	onChange,
	onAllNormalChange,
}: {
	taskTypes: TaskType[];
	selectedIds: string[];
	allNormal: boolean;
	onChange: (ids: string[]) => void;
	onAllNormalChange: (allNormal: boolean) => void;
}) {
	const { t } = useTranslation();
	const normalTypes = taskTypes.filter((tt) => !tt.is_system);
	const systemTypes = taskTypes.filter((tt) => tt.is_system);

	const toggle = (id: string) => {
		if (selectedIds.includes(id)) {
			onChange(selectedIds.filter((x) => x !== id));
		} else {
			onChange([...selectedIds, id]);
		}
	};

	const renderTypeIcon = (type: TaskType) => {
		const IconComp = type.icon ? getTaskTypeIconComponent(type.icon) : null;
		if (IconComp) {
			return (
				<IconComp
					className="size-3.5 shrink-0"
					style={{ color: type.color ?? "#6366f1" }}
				/>
			);
		}
		return (
			<span
				className="size-2 shrink-0 rounded-full"
				style={{ backgroundColor: type.color ?? "#6366f1" }}
			/>
		);
	};

	return (
		<div className="px-1 pb-3 space-y-0.5">
			{/* Normal types group */}
			{normalTypes.length > 0 && (
				<>
					<div className="flex items-center gap-1.5 px-2 pt-2 pb-0.5">
						<span className="text-xs font-semibold uppercase tracking-[0.08em] text-muted-foreground/50">
							{t("project.interactions.viewSettings.taskTypeFilter.normalTypes")}
						</span>
					</div>
					{/* "All normal types" toggle */}
					<CheckRow
						id="type-all-normal"
						label={t(
							"project.interactions.viewSettings.taskTypeFilter.allNormal",
						)}
						checked={allNormal}
						bold
						onChange={() => onAllNormalChange(!allNormal)}
					/>
					{/* Individual normal types — shown always; disabled/checked when allNormal */}
					{normalTypes.map((type) => {
						const inMode = allNormal;
						return (
							<div
								key={type.id}
								className={cn("transition-opacity", inMode && "opacity-40")}
							>
								<CheckRow
									id={`type-${type.id}`}
									label={type.name}
									checked={inMode || selectedIds.includes(type.id)}
									onChange={() => {
										if (inMode) return; // noop when mode handles it
										toggle(type.id);
									}}
									icon={renderTypeIcon(type)}
								/>
							</div>
						);
					})}
				</>
			)}

			{/* System types group */}
			{systemTypes.length > 0 && (
				<>
					<div className="flex items-center gap-1.5 px-2 pt-2 pb-0.5">
						<span className="text-xs font-semibold uppercase tracking-[0.08em] text-muted-foreground/50">
							{t("project.interactions.viewSettings.taskTypeFilter.systemTypes")}
						</span>
					</div>
					{systemTypes.map((type) => (
						<CheckRow
							key={type.id}
							id={`type-${type.id}`}
							label={type.name}
							checked={selectedIds.includes(type.id)}
							onChange={() => toggle(type.id)}
							icon={renderTypeIcon(type)}
						/>
					))}
				</>
			)}

			{taskTypes.length === 0 && (
				<p className="px-2 py-1 text-xs text-muted-foreground/50">
					{t("project.interactions.viewSettings.taskTypeFilter.empty")}
				</p>
			)}
		</div>
	);
}

// ── Collapsible filter group ──────────────────────────────────────────────────

function CollapsibleFilter({
	label,
	badge,
	defaultOpen = false,
	children,
}: {
	label: string;
	badge?: number;
	defaultOpen?: boolean;
	children: React.ReactNode;
}) {
	const [open, setOpen] = useState(defaultOpen);
	return (
		<div className="border-b border-border/20 last:border-0">
			<button
				type="button"
				onClick={() => setOpen((v) => !v)}
				className="flex w-full items-center gap-2 px-3 py-2 text-left hover:bg-muted/30 transition-colors"
			>
				{open ? (
					<ChevronDown className="size-3 shrink-0 text-muted-foreground/50" />
				) : (
					<ChevronRight className="size-3 shrink-0 text-muted-foreground/50" />
				)}
				<span className="flex-1 text-xs font-medium text-foreground/80">
					{label}
				</span>
				{badge != null && badge > 0 && (
					<span className="inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-primary/15 px-1 text-xs font-semibold text-primary">
						{badge}
					</span>
				)}
			</button>
			{open && children}
		</div>
	);
}

// ── Main component ────────────────────────────────────────────────────────────

interface ViewSettingsPanelProps {
	projectId: string;
	view: InteractionView | null;
	open: boolean;
	onOpenChange: (open: boolean) => void;
	onSave: (viewId: string, config: ViewConfig) => Promise<unknown>;
	onPreview: (config: ViewConfig) => void;
	isPending?: boolean;
}

export function ViewSettingsPanel({
	projectId,
	view,
	open,
	onOpenChange,
	onSave,
	onPreview,
	isPending,
}: ViewSettingsPanelProps) {
	const { t } = useTranslation();
	const { data: customFields = [] } = useQuery(
		customFieldsQueryOptions(projectId),
	);
	const { data: statuses = [] } = useQuery(taskStatusesQueryOptions(projectId));
	const { data: taskTypes = [] } = useQuery(taskTypesQueryOptions(projectId));
	const { data: members = [] } = useQuery(
		projectMembersQueryOptions(projectId),
	);
	const { data: sprints = [] } = useQuery(sprintsQueryOptions(projectId));

	const [draft, setDraft] = useState<ViewConfig>(() => view?.config ?? {});
	const [fieldsOpen, setFieldsOpen] = useState(false);

	// biome-ignore lint/correctness/useExhaustiveDependencies: intentionally keyed on view?.id
	useEffect(() => {
		if (open) setDraft(view?.config ?? {});
	}, [open, view?.id]);

	// biome-ignore lint/correctness/useExhaustiveDependencies: onPreview is stable
	useEffect(() => {
		if (open) onPreview(draft);
	}, [draft, open]);

	const handleOpenChange = (newOpen: boolean) => {
		if (!newOpen && view) {
			onPreview(view.config ?? {});
			setFieldsOpen(false);
		}
		onOpenChange(newOpen);
	};

	const update = (patch: Partial<ViewConfig>) => {
		setDraft((prev) => {
			const next = { ...prev, ...patch };
			for (const key of Object.keys(patch) as (keyof ViewConfig)[]) {
				if (patch[key] === undefined) delete next[key];
			}
			return next;
		});
	};

	const updateFilters = (patch: Partial<ViewFilters>) => {
		const current = draft.filters ?? {};
		const next: ViewFilters = { ...current, ...patch };
		if (!next.sprints) delete next.sprints;
		if (!next.statuses) delete next.statuses;
		if (!next.assignees) delete next.assignees;
		if (!next.task_types) delete next.task_types;
		update({ filters: Object.keys(next).length > 0 ? next : {} });
	};

	const handleSave = async () => {
		if (!view) return;
		await onSave(view.id, draft);
		setFieldsOpen(false);
		onOpenChange(false);
	};

	const handleReset = () => {
		const saved = view?.config ?? {};
		setDraft(saved);
		onPreview(saved);
		setFieldsOpen(false);
	};

	const visibleFields: string[] =
		draft.fields && draft.fields.length > 0
			? draft.fields
			: DEFAULT_VISIBLE_FIELDS;

	const allFieldOpts = buildAllFieldOptions(customFields, t);
	const fieldsLabel = [
		t("project.interactions.viewSettings.fieldNames.title"),
		...visibleFields.map(
			(k) => allFieldOpts.find((f) => f.key === k)?.label ?? k,
		),
	].join(", ");

	const columnByOpts = buildColumnByOptions(customFields, t);
	const sortByOpts = buildSortByOptions(customFields, t);
	const swimlaneOpts = buildSwimlaneOptions(customFields, t);
	const fieldSumOpts = buildFieldSumOptions(customFields, t);

	const sortByValue = draft.sort_by ?? "manual";
	// Mirrors the runtime defaults in interaction-layout.tsx (see
	// PAGE_SIZE_DEFAULTS in view-utils.ts).
	const defaultInitialPageSize = getDefaultInitialPageSize(view?.layout);
	const defaultPageSize = getDefaultPageSize(view?.layout);
	const filterSprintIds = filterConfigToIds(draft.filters?.sprints);
	const filterStatusIds = filterConfigToIds(draft.filters?.statuses);
	const filterAssigneeIds = filterConfigToIds(draft.filters?.assignees);
	const { allNormal: filterTaskTypeAllNormal, selectedIds: filterTaskTypeIds } =
		taskTypeConfigToUI(draft.filters?.task_types);

	const filterBadge =
		filterSprintIds.length +
		filterStatusIds.length +
		filterAssigneeIds.length +
		filterTaskTypeIds.length +
		(filterTaskTypeAllNormal ? 1 : 0);
	const hasSavedFilters = filterBadge > 0;

	return (
		<Popover open={open} onOpenChange={handleOpenChange}>
			<PopoverTrigger
				render={
					<button
						type="button"
						aria-label={t("project.interactions.viewSettings.ariaLabel")}
						className={cn(
							"relative flex size-7 items-center justify-center rounded-md transition-all duration-150",
							open
								? "bg-primary/8 text-primary/80"
								: "text-muted-foreground/60 hover:text-foreground hover:bg-muted/60",
						)}
					/>
				}
			>
				<Settings className="size-3.5" />
			</PopoverTrigger>
			<PopoverContent
				side="bottom"
				align="end"
				className="w-80 p-0 rounded-xl border border-border/40 shadow-xl overflow-hidden"
				sideOffset={6}
			>
				{/* ── Header ───────────────────────────────────────────────── */}
				<div className="flex items-center justify-between px-3 py-2.5 border-b border-border/30 bg-muted/20">
					{fieldsOpen ? (
						<>
							<p className="text-xs font-semibold text-muted-foreground/80">
								{t("project.interactions.viewSettings.chooseFields")}
							</p>
							<button
								type="button"
								onClick={() => setFieldsOpen(false)}
								className="text-xs font-medium text-primary/80 hover:text-primary transition-colors"
							>
								{t("project.interactions.viewSettings.back")}
							</button>
						</>
					) : (
						<>
							<p className="text-xs font-semibold uppercase tracking-[0.08em] text-muted-foreground/70">
								{t("project.interactions.viewSettings.title")}
							</p>
							{hasSavedFilters && (
								<button
									type="button"
									onClick={() => update({ filters: {} })}
									className="text-xs font-medium text-muted-foreground/60 hover:text-foreground transition-colors"
								>
									{t("project.interactions.viewSettings.clearFilters")}
								</button>
							)}
						</>
					)}
				</div>

				{/* ── Field picker sub-page ─────────────────────────────────── */}
				{fieldsOpen ? (
					<div className="px-1">
						<FieldPicker
							visibleFields={visibleFields}
							customFields={customFields}
							onChange={(fields) => update({ fields })}
						/>
					</div>
				) : (
					<div className="overflow-y-auto max-h-[70vh]">
						{/* ── Display section ─────────────────────────────────── */}
						<PanelSectionHeader>
							{t("project.interactions.viewSettings.display")}
						</PanelSectionHeader>
						<div className="pb-2 space-y-0.5">
							<SettingRow
								label={t("project.interactions.viewSettings.rows.fields")}
							>
								<button
									type="button"
									onClick={() => setFieldsOpen(true)}
									className="flex-1 truncate text-left text-xs font-medium text-foreground hover:text-primary transition-colors duration-150"
								>
									{fieldsLabel}
								</button>
							</SettingRow>

							<SettingRow
								label={t("project.interactions.viewSettings.rows.columnBy")}
							>
								<DynamicSelect
									value={draft.column_by ?? "status"}
									options={columnByOpts}
									onChange={(v) => update({ column_by: v })}
								/>
							</SettingRow>

							<SettingRow
								label={t("project.interactions.viewSettings.rows.swimlanes")}
							>
								<DynamicSelect
									value={draft.swimlanes ?? "none"}
									options={swimlaneOpts}
									onChange={(v) => update({ swimlanes: v })}
								/>
							</SettingRow>

							<SettingRow
								label={t("project.interactions.viewSettings.rows.sortBy")}
							>
								<DynamicSelect
									value={sortByValue}
									options={sortByOpts}
									onChange={(v) => update({ sort_by: v })}
								/>
							</SettingRow>

							<SettingRow
								label={t("project.interactions.viewSettings.rows.fieldSum")}
							>
								<DynamicSelect
									value={draft.field_sum ?? "count"}
									options={fieldSumOpts}
									onChange={(v) => update({ field_sum: v })}
								/>
							</SettingRow>

							<SettingRow
								label={t("project.interactions.viewSettings.rows.initialSize")}
							>
								<DynamicSelect
									value={String(
										draft.initial_page_size ?? defaultInitialPageSize,
									)}
									options={PAGE_SIZE_OPTIONS}
									onChange={(v) =>
										update({ initial_page_size: v ? Number(v) : undefined })
									}
								/>
							</SettingRow>

							<SettingRow
								label={t("project.interactions.viewSettings.rows.perPage")}
							>
								<DynamicSelect
									value={String(draft.page_size ?? defaultPageSize)}
									options={PAGE_SIZE_OPTIONS}
									onChange={(v) =>
										update({ page_size: v ? Number(v) : undefined })
									}
								/>
							</SettingRow>
						</div>

						{/* ── Filters section ──────────────────────────────────── */}
						<div className="border-t border-border/20">
							<PanelSectionHeader>
								{t("project.interactions.viewSettings.filters")}
							</PanelSectionHeader>

							<div className="pb-1">
								<CollapsibleFilter
									label={t(
										"project.interactions.viewSettings.filterGroups.sprints",
									)}
									badge={filterSprintIds.length}
									defaultOpen={filterSprintIds.length > 0}
								>
									<SprintFilterSection
										sprints={sprints}
										selectedIds={filterSprintIds}
										onChange={(ids) =>
											updateFilters({ sprints: idsToFilterConfig(ids) })
										}
									/>
								</CollapsibleFilter>

								<CollapsibleFilter
									label={t(
										"project.interactions.viewSettings.filterGroups.statuses",
									)}
									badge={filterStatusIds.length}
									defaultOpen={filterStatusIds.length > 0}
								>
									<StatusFilterSection
										statuses={statuses}
										selectedIds={filterStatusIds}
										onChange={(ids) =>
											updateFilters({ statuses: idsToFilterConfig(ids) })
										}
									/>
								</CollapsibleFilter>

								<CollapsibleFilter
									label={t(
										"project.interactions.viewSettings.filterGroups.assignees",
									)}
									badge={filterAssigneeIds.length}
									defaultOpen={filterAssigneeIds.length > 0}
								>
									<AssigneeFilterSection
										members={members}
										selectedIds={filterAssigneeIds}
										onChange={(ids) =>
											updateFilters({ assignees: idsToFilterConfig(ids) })
										}
									/>
								</CollapsibleFilter>

								<CollapsibleFilter
									label={t(
										"project.interactions.viewSettings.filterGroups.taskTypes",
									)}
									badge={
										filterTaskTypeIds.length + (filterTaskTypeAllNormal ? 1 : 0)
									}
									defaultOpen={
										filterTaskTypeIds.length > 0 || filterTaskTypeAllNormal
									}
								>
									<TaskTypeFilterSection
										taskTypes={taskTypes}
										selectedIds={filterTaskTypeIds}
										allNormal={filterTaskTypeAllNormal}
										onChange={(ids) =>
											updateFilters({
												task_types: uiToTaskTypeConfig(
													filterTaskTypeAllNormal,
													ids,
												),
											})
										}
										onAllNormalChange={(next) =>
											updateFilters({
												task_types: uiToTaskTypeConfig(
													next,
													next
														? filterTaskTypeIds.filter(
																(id) =>
																	taskTypes.find((tt) => tt.id === id)
																		?.is_system,
															)
														: filterTaskTypeIds,
												),
											})
										}
									/>
								</CollapsibleFilter>
							</div>
						</div>
					</div>
				)}

				{/* ── Footer ───────────────────────────────────────────────── */}
				<div className="flex items-center justify-end gap-2 px-3 py-2.5 border-t border-border/30 bg-muted/10">
					<button
						type="button"
						onClick={handleReset}
						className="rounded-lg bg-muted/50 px-2.5 py-1.5 text-xs font-semibold text-muted-foreground/80 transition-all duration-150 hover:bg-muted hover:text-foreground"
					>
						{t("project.interactions.viewSettings.reset")}
					</button>
					<button
						type="button"
						onClick={handleSave}
						disabled={isPending}
						className="rounded-lg bg-primary px-3 py-1.5 text-xs font-semibold text-primary-foreground shadow-sm transition-all duration-150 hover:bg-primary/90 disabled:opacity-40"
					>
						{isPending
							? t("project.interactions.viewSettings.saving")
							: t("project.interactions.viewSettings.save")}
					</button>
				</div>
			</PopoverContent>
		</Popover>
	);
}
