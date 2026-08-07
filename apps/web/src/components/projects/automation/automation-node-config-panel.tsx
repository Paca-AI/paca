import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import {
	ChevronDown,
	Copy,
	Loader2,
	Plus,
	Search,
	Trash2,
	X,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
	getImportanceBucket,
	IMPORTANCE_BUCKET_VALUES,
	PRIORITY_LEVELS,
} from "@/components/projects/interactions/priority";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { useTaskPickerSearch } from "@/hooks/use-task-picker-search";
import {
	ACTION_TYPES,
	type ActionConfig,
	type AutomationNode,
	CONDITION_NODE_TYPE,
	type ConditionBranch,
	type ConditionConfig,
	type ConditionField,
	type ConditionLeaf,
	type ConditionOperator,
	generateWebhookToken,
	MULTI_VALUED_TARGET_KINDS,
	type PluginNodeConfigSchema,
	type PluginNodeConfigSchemaProperty,
	type TaskFieldUpdate,
	type TaskTarget,
	type TaskTargetKind,
	TRIGGER_TYPES,
	type TriggerConfig,
} from "@/lib/automation-api";
import {
	listAllTasks,
	sprintsQueryOptions,
	type Task,
	taskQueryOptions,
	tasksPickerInfiniteQueryOptions,
} from "@/lib/interaction-api";
import type {
	CustomFieldDefinition,
	ProjectMember,
	TaskStatus,
	TaskType,
} from "@/lib/project-api";
import { createLoadMoreScrollHandler } from "@/lib/scroll-pagination";

interface AutomationNodeConfigPanelProps {
	node: AutomationNode;
	projectId: string;
	automationId: string;
	statuses: TaskStatus[];
	members: ProjectMember[];
	customFields: CustomFieldDefinition[];
	taskTypes: TaskType[];
	canEdit: boolean;
	onSave: (config: Record<string, unknown>) => void;
	onClose: () => void;
	onRemove: () => void;
	saving?: boolean;
	/** Human-readable label for a plugin-contributed node type, from the
	 * plugin's own manifest — used instead of an i18n lookup when the
	 * node's type isn't one of the built-ins. */
	pluginLabel?: string;
	/** JSON Schema for a plugin-contributed node type's config, from the
	 * plugin's own manifest (plugin.json's
	 * automation.{triggers,conditions,actions}[].configSchema). When
	 * present with properties, drives SchemaConfigForm's UI-field
	 * rendering instead of GenericConfigForm's raw JSON textarea. */
	pluginConfigSchema?: PluginNodeConfigSchema;
}

// The task list API caps page_size at 200, so a full project task picker has
// to page through — otherwise tasks past the first 200 are invisible to it.
const MAX_TASK_PAGES = 25;

async function fetchAllProjectTasks(projectId: string): Promise<Task[]> {
	const all: Task[] = [];
	let cursor: string | undefined;
	for (let page = 0; page < MAX_TASK_PAGES; page++) {
		const result = await listAllTasks(projectId, { pageSize: 200, cursor });
		all.push(...result.items);
		const next = result.next_cursor;
		if (!next) break;
		cursor = next;
	}
	return all;
}

function taskLabel(task: Task): string {
	return `#${task.task_number} ${task.title}`;
}

// <input type="date"> exchanges bare "YYYY-MM-DD" values, but every date
// leaving this form (start_date/due_date on a condition leaf or an
// update_task field) is stored as RFC 3339 — matching how dates travel
// everywhere else in this API (see compareTimePtr's doc comment in
// condition.go) and the same convention task-detail/property-field/
// helpers.ts's toISODate uses for the task detail page's own date editor.
function toDateInputValue(iso?: string): string {
	return iso ? iso.slice(0, 10) : "";
}
function fromDateInputValue(value: string): string {
	return value ? `${value}T00:00:00Z` : "";
}

// Field choices for a condition leaf's Select — matches the domain's Field
// enum in condition.go. Plugin-contributed conditions are their own
// standalone condition node type (see AutomationNodeConfigPanel's dispatch
// on node.type), not a leaf field choice here.
// Ordered to match how these fields read on the task itself — title first
// (what a task is), then status/type/assignee (classification & ownership),
// importance/story_points (planning), tags, reporter, sprint/epic
// (organization), the two dates, and custom_field last since it's the
// generic catch-all that needs a second field_key picker.
const CONDITION_FIELDS: ConditionField[] = [
	"title",
	"status_id",
	"task_type_id",
	"assignee_ids",
	"importance",
	"story_points",
	"tags",
	"reporter_id",
	"sprint_id",
	"parent_task_id",
	"start_date",
	"due_date",
	"custom_field",
	"sprint_name",
	"sprint_status",
	"sprint_goal",
	"sprint_start_date",
	"sprint_end_date",
];

// SPRINT_CONDITION_FIELDS evaluate against the walk's current sprint (set
// directly by a sprint_* trigger, or resolved via a task's own sprint_id)
// rather than the task — mirrors IsSprintField in domain/automation/condition.go.
const SPRINT_CONDITION_FIELDS: ConditionField[] = [
	"sprint_name",
	"sprint_status",
	"sprint_goal",
	"sprint_start_date",
	"sprint_end_date",
];

// Mirrors validOperatorsByField in domain/automation/condition.go exactly —
// keep the two in sync.
const OPERATORS_BY_FIELD: Record<ConditionField, ConditionOperator[]> = {
	status_id: ["equals", "not_equals", "is_empty", "is_not_empty"],
	task_type_id: ["equals", "not_equals", "is_empty", "is_not_empty"],
	importance: ["equals", "not_equals", "greater_than", "less_than"],
	assignee_ids: ["contains", "not_equals", "is_empty", "is_not_empty"],
	tags: ["contains", "not_equals", "is_empty", "is_not_empty"],
	custom_field: [
		"equals",
		"not_equals",
		"is_empty",
		"is_not_empty",
		"greater_than",
		"less_than",
	],
	title: ["equals", "not_equals", "contains", "is_empty", "is_not_empty"],
	story_points: [
		"equals",
		"not_equals",
		"greater_than",
		"less_than",
		"is_empty",
		"is_not_empty",
	],
	sprint_id: ["equals", "not_equals", "is_empty", "is_not_empty"],
	parent_task_id: ["equals", "not_equals", "is_empty", "is_not_empty"],
	reporter_id: ["equals", "not_equals", "is_empty", "is_not_empty"],
	start_date: [
		"equals",
		"not_equals",
		"greater_than",
		"less_than",
		"is_empty",
		"is_not_empty",
	],
	due_date: [
		"equals",
		"not_equals",
		"greater_than",
		"less_than",
		"is_empty",
		"is_not_empty",
	],
	sprint_name: ["equals", "not_equals", "contains", "is_empty", "is_not_empty"],
	sprint_status: ["equals", "not_equals"],
	sprint_goal: ["equals", "not_equals", "contains", "is_empty", "is_not_empty"],
	sprint_start_date: [
		"equals",
		"not_equals",
		"greater_than",
		"less_than",
		"is_empty",
		"is_not_empty",
	],
	sprint_end_date: [
		"equals",
		"not_equals",
		"greater_than",
		"less_than",
		"is_empty",
		"is_not_empty",
	],
};

// UpdatableField enumerates every TaskFieldUpdate key with an editor in
// this form — the same set the "add field" picker offers. Excludes
// description (rich text, no dedicated editor here). Ordered to match
// CONDITION_FIELDS above for a consistent field-picking experience across
// conditions and actions.
type UpdatableField = Exclude<keyof TaskFieldUpdate, "description">;
const FIELD_UPDATE_ORDER: UpdatableField[] = [
	"title",
	"status_id",
	"task_type_id",
	"assignee_ids",
	"importance",
	"story_points",
	"tags",
	"reporter_id",
	"sprint_id",
	"parent_task_id",
	"start_date",
	"due_date",
	"custom_fields",
];

export function AutomationNodeConfigPanel({
	node,
	projectId,
	automationId,
	statuses,
	members,
	customFields,
	taskTypes,
	canEdit,
	onSave,
	onClose,
	onRemove,
	saving,
	pluginLabel,
	pluginConfigSchema,
}: AutomationNodeConfigPanelProps) {
	const { t } = useTranslation("projects");
	const [removeOpen, setRemoveOpen] = useState(false);

	const isBuiltinTrigger = (TRIGGER_TYPES as readonly string[]).includes(
		node.type,
	);
	const isBuiltinAction = (ACTION_TYPES as readonly string[]).includes(
		node.type,
	);
	const isBuiltinCondition = node.type === CONDITION_NODE_TYPE;

	if (node.kind === "trigger") {
		return (
			<Panel
				title={
					isBuiltinTrigger
						? t(
								`automation.triggerTypes.${node.type as (typeof TRIGGER_TYPES)[number]}`,
							)
						: (pluginLabel ?? node.type)
				}
				kind="trigger"
				canEdit={canEdit}
				onClose={onClose}
				onRemoveClick={() => setRemoveOpen(true)}
			>
				{isBuiltinTrigger ? (
					<TriggerConfigForm
						type={node.type}
						config={node.config as TriggerConfig}
						projectId={projectId}
						automationId={automationId}
						nodeId={node.id}
						statuses={statuses}
						canEdit={canEdit}
						onSave={onSave}
						saving={saving}
					/>
				) : pluginConfigSchema?.properties &&
					Object.keys(pluginConfigSchema.properties).length > 0 ? (
					<SchemaConfigForm
						schema={pluginConfigSchema}
						config={node.config}
						projectId={projectId}
						members={members}
						nodeKind="trigger"
						canEdit={canEdit}
						onSave={onSave}
						saving={saving}
					/>
				) : (
					<PluginTriggerNoConfigNotice />
				)}
				<RemoveDialog
					open={removeOpen}
					onOpenChange={setRemoveOpen}
					onConfirm={onRemove}
				/>
			</Panel>
		);
	}

	if (node.kind === "condition") {
		return (
			<Panel
				title={
					isBuiltinCondition
						? t("automation.nodeKind.condition")
						: (pluginLabel ?? node.type)
				}
				kind="condition"
				canEdit={canEdit}
				onClose={onClose}
				onRemoveClick={() => setRemoveOpen(true)}
			>
				{isBuiltinCondition ? (
					<ConditionConfigForm
						config={node.config as unknown as ConditionConfig}
						projectId={projectId}
						statuses={statuses}
						members={members}
						customFields={customFields}
						taskTypes={taskTypes}
						canEdit={canEdit}
						onSave={onSave}
						saving={saving}
					/>
				) : pluginConfigSchema?.properties &&
					Object.keys(pluginConfigSchema.properties).length > 0 ? (
					<SchemaConfigForm
						schema={pluginConfigSchema}
						config={node.config}
						projectId={projectId}
						members={members}
						nodeKind="condition"
						canEdit={canEdit}
						onSave={onSave}
						saving={saving}
					/>
				) : (
					<GenericConfigForm
						config={node.config}
						canEdit={canEdit}
						onSave={onSave}
						saving={saving}
					/>
				)}
				<RemoveDialog
					open={removeOpen}
					onOpenChange={setRemoveOpen}
					onConfirm={onRemove}
				/>
			</Panel>
		);
	}

	return (
		<Panel
			title={
				isBuiltinAction
					? t(
							`automation.actionTypes.${node.type as (typeof ACTION_TYPES)[number]}`,
						)
					: (pluginLabel ?? node.type)
			}
			kind="action"
			canEdit={canEdit}
			onClose={onClose}
			onRemoveClick={() => setRemoveOpen(true)}
		>
			{isBuiltinAction ? (
				<ActionConfigForm
					type={node.type}
					config={node.config as ActionConfig}
					projectId={projectId}
					statuses={statuses}
					members={members}
					customFields={customFields}
					taskTypes={taskTypes}
					canEdit={canEdit}
					onSave={onSave}
					saving={saving}
				/>
			) : pluginConfigSchema?.properties &&
				Object.keys(pluginConfigSchema.properties).length > 0 ? (
				<SchemaConfigForm
					schema={pluginConfigSchema}
					config={node.config}
					projectId={projectId}
					members={members}
					nodeKind="action"
					canEdit={canEdit}
					onSave={onSave}
					saving={saving}
				/>
			) : (
				<GenericConfigForm
					config={node.config}
					canEdit={canEdit}
					onSave={onSave}
					saving={saving}
				/>
			)}
			<RemoveDialog
				open={removeOpen}
				onOpenChange={setRemoveOpen}
				onConfirm={onRemove}
			/>
		</Panel>
	);
}

// SchemaConfigForm renders real UI fields (text/number inputs, dropdowns,
// textareas, date pickers, member pickers) for a plugin-contributed node's
// config, driven by the JSON Schema the plugin declares at plugin.json's
// automation.{triggers,conditions,actions}[].configSchema. This is the
// primary editor for any plugin node that declares a configSchema with
// properties — GenericConfigForm's raw JSON textarea remains only as a
// fallback for plugin nodes that declare no schema at all (or one with no
// properties), so an older/minimal plugin never loses the ability to be
// configured.
//
// Condition and action nodes also get the same "applies to" target picker
// the built-in condition leaf/action forms have (TaskTargetSelector, plus
// MatchModeSelector for a condition whose target can resolve to more than
// one task) — stored as config.target/config.match_mode alongside the
// plugin's own declared fields, the same JSON shape ConditionLeaf/
// ActionConfig already use, so the engine picks it up with no
// plugin-specific handling required. Triggers don't get one: a
// plugin-contributed trigger fires from whatever task its own eventTopic
// payload names, not something a user can retarget from the canvas.
function SchemaConfigForm({
	schema,
	config,
	projectId,
	members,
	nodeKind,
	canEdit,
	onSave,
	saving,
}: {
	schema: PluginNodeConfigSchema;
	config: Record<string, unknown>;
	projectId: string;
	members: ProjectMember[];
	nodeKind: "trigger" | "condition" | "action";
	canEdit: boolean;
	onSave: (config: Record<string, unknown>) => void;
	saving?: boolean;
}) {
	const { t } = useTranslation("projects");
	const properties = schema.properties ?? {};
	const required = useMemo(() => new Set(schema.required ?? []), [schema]);
	const fieldNames = Object.keys(properties);
	const showTarget = nodeKind !== "trigger";

	const [values, setValues] = useState<Record<string, string | boolean>>(() => {
		const initial: Record<string, string | boolean> = {};
		for (const name of fieldNames) {
			const prop = properties[name];
			const raw = config[name];
			if (prop.type === "boolean") {
				initial[name] =
					typeof raw === "boolean" ? raw : Boolean(prop.default ?? false);
			} else {
				initial[name] =
					raw != null
						? String(raw)
						: prop.default != null
							? String(prop.default)
							: "";
			}
		}
		return initial;
	});
	const [target, setTarget] = useState<TaskTarget | undefined>(
		config.target as TaskTarget | undefined,
	);
	const [matchMode, setMatchMode] = useState<"any" | "all" | undefined>(
		config.match_mode as "any" | "all" | undefined,
	);

	function setField(name: string, value: string | boolean) {
		setValues((prev) => ({ ...prev, [name]: value }));
	}

	const missingRequired = fieldNames.some((name) => {
		if (!required.has(name)) return false;
		const prop = properties[name];
		if (prop.type === "boolean") return false;
		return !String(values[name] ?? "").trim();
	});

	function buildConfig(): Record<string, unknown> {
		const out: Record<string, unknown> = {};
		for (const name of fieldNames) {
			const prop = properties[name];
			const raw = values[name];
			if (prop.type === "boolean") {
				out[name] = Boolean(raw);
				continue;
			}
			const strVal = String(raw ?? "").trim();
			if (!strVal) {
				if (required.has(name)) out[name] = strVal;
				continue;
			}
			if (prop.type === "integer" || prop.type === "number") {
				const num = Number(strVal);
				out[name] = Number.isFinite(num) ? num : strVal;
			} else {
				out[name] = strVal;
			}
		}
		if (showTarget) {
			out.target = target;
			if (nodeKind === "condition") out.match_mode = matchMode;
		}
		return out;
	}

	if (fieldNames.length === 0 && !showTarget) {
		return (
			<GenericConfigForm
				config={config}
				canEdit={canEdit}
				onSave={onSave}
				saving={saving}
			/>
		);
	}

	return (
		<div className="space-y-3">
			<SchemaFields
				properties={properties}
				required={required}
				values={values}
				setField={setField}
				members={members}
				canEdit={canEdit}
			/>
			{showTarget && (
				<div className="space-y-1">
					<Label className="text-[10px] text-muted-foreground">
						{t("automation.nodeConfig.target.label")}
					</Label>
					<TaskTargetSelector
						projectId={projectId}
						target={target}
						onChange={setTarget}
						canEdit={canEdit}
					/>
					{nodeKind === "condition" &&
						target &&
						MULTI_VALUED_TARGET_KINDS.includes(target.kind) && (
							<MatchModeSelector
								value={matchMode}
								onChange={setMatchMode}
								canEdit={canEdit}
							/>
						)}
				</div>
			)}
			{canEdit && (
				<SaveButton
					saving={saving}
					disabled={missingRequired}
					onClick={() => onSave(buildConfig())}
				/>
			)}
		</div>
	);
}

// SchemaFields renders one input per JSON-Schema property — shared by
// SchemaConfigForm and GenericConfigForm's schema-aware sibling for any
// plugin-contributed node (trigger, condition, or action), so both stay in
// sync with configSchema's supported shapes (boolean/enum/textarea/date/
// member/number/text) automatically.
function SchemaFields({
	properties,
	required,
	values,
	setField,
	members,
	canEdit,
}: {
	properties: Record<string, PluginNodeConfigSchemaProperty>;
	required: Set<string>;
	values: Record<string, string | boolean>;
	setField: (name: string, value: string | boolean) => void;
	members: ProjectMember[];
	canEdit: boolean;
}) {
	const fieldNames = Object.keys(properties);
	return (
		<>
			{fieldNames.map((name) => {
				const prop = properties[name];
				const label = prop.title ?? name;
				const isRequired = required.has(name);
				const value = values[name];

				if (prop.type === "boolean") {
					return (
						<div key={name} className="flex items-center justify-between gap-2">
							<Label>{label}</Label>
							<Switch
								checked={Boolean(value)}
								onCheckedChange={(checked) => setField(name, checked)}
								disabled={!canEdit}
							/>
						</div>
					);
				}

				if (prop.format === "member") {
					const selected = members.find((m) => m.id === value);
					return (
						<div key={name} className="space-y-1.5">
							<Label>
								{label}
								{isRequired && <span className="text-destructive"> *</span>}
							</Label>
							<Select
								value={String(value ?? "")}
								onValueChange={(v) => v && setField(name, v)}
								disabled={!canEdit}
							>
								<SelectTrigger className="w-full">
									<SelectValue>
										{selected?.full_name || selected?.username}
									</SelectValue>
								</SelectTrigger>
								<SelectContent>
									{members.map((m) => (
										<SelectItem key={m.id} value={m.id}>
											{m.full_name || m.username}
										</SelectItem>
									))}
								</SelectContent>
							</Select>
						</div>
					);
				}

				if (prop.enum && prop.enum.length > 0) {
					return (
						<div key={name} className="space-y-1.5">
							<Label>
								{label}
								{isRequired && <span className="text-destructive"> *</span>}
							</Label>
							<Select
								value={String(value ?? "")}
								onValueChange={(v) => v && setField(name, v)}
								disabled={!canEdit}
							>
								<SelectTrigger className="w-full">
									<SelectValue>{String(value ?? "")}</SelectValue>
								</SelectTrigger>
								<SelectContent>
									{prop.enum.map((option: string) => (
										<SelectItem key={option} value={option}>
											{option}
										</SelectItem>
									))}
								</SelectContent>
							</Select>
						</div>
					);
				}

				if (prop.format === "textarea") {
					return (
						<div key={name} className="space-y-1.5">
							<Label>
								{label}
								{isRequired && <span className="text-destructive"> *</span>}
							</Label>
							<Textarea
								value={String(value ?? "")}
								onChange={(e) => setField(name, e.target.value)}
								rows={4}
								disabled={!canEdit}
							/>
						</div>
					);
				}

				const inputType =
					prop.format === "date"
						? "date"
						: prop.type === "integer" || prop.type === "number"
							? "number"
							: "text";

				return (
					<div key={name} className="space-y-1.5">
						<Label>
							{label}
							{isRequired && <span className="text-destructive"> *</span>}
						</Label>
						<Input
							type={inputType}
							value={String(value ?? "")}
							onChange={(e) => setField(name, e.target.value)}
							min={prop.minimum}
							disabled={!canEdit}
						/>
					</div>
				);
			})}
		</>
	);
}

// PluginTriggerNoConfigNotice replaces GenericConfigForm's raw-JSON editor
// for a plugin-contributed trigger that declares no configSchema (or one
// with no properties). Unlike a condition or action, a trigger's config is
// never read by the automation engine at all — it matches purely on the
// manifest's declared eventTopic (see AutomationNodeManifest.EventTopic and
// validateTriggerConfig's plugin-trigger branch in the core's
// domain/plugin/entity.go and service/automation/automation_service.go). A
// plugin wanting per-instance filtering contributes a Condition node
// instead, placed right after the trigger in the graph — so offering a JSON
// editor here would just invite the user to configure something that can
// never take effect. Mirrors the "no config fields" message
// TriggerConfigForm shows for the built-in task_created/assignee_changed/
// priority_changed triggers.
function PluginTriggerNoConfigNotice() {
	const { t } = useTranslation("projects");
	return (
		<p className="text-xs text-muted-foreground">
			{t("automation.nodeConfig.trigger.noConfigNeeded")}
		</p>
	);
}

// GenericConfigForm is the fallback config editor for a plugin-contributed
// condition or action node type that declares no configSchema (or one with
// no properties): a raw JSON editor. Any plugin node with a real
// configSchema renders through SchemaConfigForm above instead — this
// remains reachable only so an older or minimal plugin never loses the
// ability to be configured at all. Not used for triggers — see
// PluginTriggerNoConfigNotice above.
function GenericConfigForm({
	config,
	canEdit,
	onSave,
	saving,
}: {
	config: Record<string, unknown>;
	canEdit: boolean;
	onSave: (config: Record<string, unknown>) => void;
	saving?: boolean;
}) {
	const { t } = useTranslation("projects");
	const [text, setText] = useState(() => JSON.stringify(config, null, 2));
	const [parseError, setParseError] = useState<string | null>(null);

	return (
		<div className="space-y-2">
			<Textarea
				value={text}
				onChange={(e) => setText(e.target.value)}
				rows={10}
				className="font-mono text-xs"
				disabled={!canEdit}
			/>
			{parseError && <p className="text-xs text-destructive">{parseError}</p>}
			{canEdit && (
				<SaveButton
					saving={saving}
					onClick={() => {
						try {
							const parsed = JSON.parse(text);
							setParseError(null);
							onSave(parsed);
						} catch {
							setParseError("Invalid JSON");
						}
					}}
				/>
			)}
			<p className="text-xs text-muted-foreground">
				{t("automation.nodeConfig.genericFormHint")}
			</p>
		</div>
	);
}

const NODE_KIND_LABEL_KEY = {
	trigger: "automation.nodeKind.trigger",
	condition: "automation.nodeKind.condition",
	action: "automation.nodeKind.action",
} as const;

function Panel({
	title,
	kind,
	canEdit,
	onClose,
	onRemoveClick,
	children,
}: {
	title: string;
	kind: keyof typeof NODE_KIND_LABEL_KEY;
	canEdit: boolean;
	onClose: () => void;
	onRemoveClick: () => void;
	children: React.ReactNode;
}) {
	const { t } = useTranslation("projects");
	return (
		<div className="flex h-full w-80 shrink-0 flex-col border-l border-border/60 bg-card">
			<div className="flex items-center justify-between border-b border-border/60 px-4 py-3">
				<div>
					<div className="text-[10px] font-mono uppercase tracking-wide text-muted-foreground">
						{t(NODE_KIND_LABEL_KEY[kind])}
					</div>
					<div className="text-sm font-semibold">{title}</div>
				</div>
				<button
					type="button"
					onClick={onClose}
					className="text-muted-foreground hover:text-foreground"
				>
					<X className="size-4" />
				</button>
			</div>
			<div className="flex-1 overflow-y-auto px-4 py-4 space-y-4">
				{children}
			</div>
			{canEdit && (
				<div className="border-t border-border/60 px-4 py-3">
					<Button
						variant="outline"
						size="sm"
						className="w-full gap-1.5 text-destructive hover:text-destructive"
						onClick={onRemoveClick}
					>
						<Trash2 className="size-3.5" />
						{t("automation.nodeConfig.remove")}
					</Button>
				</div>
			)}
		</div>
	);
}

function RemoveDialog({
	open,
	onOpenChange,
	onConfirm,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	onConfirm: () => void;
}) {
	const { t } = useTranslation("projects");
	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent>
				<DialogHeader>
					<DialogTitle>
						{t("automation.nodeConfig.removeConfirmTitle")}
					</DialogTitle>
				</DialogHeader>
				<p className="text-sm text-muted-foreground">
					{t("automation.nodeConfig.removeConfirmBody")}
				</p>
				<DialogFooter>
					<Button variant="outline" onClick={() => onOpenChange(false)}>
						{t("automation.nodeConfig.cancel")}
					</Button>
					<Button
						variant="destructive"
						onClick={() => {
							onConfirm();
							onOpenChange(false);
						}}
					>
						{t("automation.nodeConfig.removeConfirmAction")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

// ── Trigger config form ──────────────────────────────────────────────────────

function TriggerConfigForm({
	type,
	config,
	projectId,
	automationId,
	nodeId,
	statuses,
	canEdit,
	onSave,
	saving,
}: {
	type: string;
	config: TriggerConfig;
	projectId: string;
	automationId: string;
	nodeId: string;
	statuses: TaskStatus[];
	canEdit: boolean;
	onSave: (config: Record<string, unknown>) => void;
	saving?: boolean;
}) {
	const { t } = useTranslation("projects");
	const [statusId, setStatusId] = useState(config.status_id ?? "");
	const [tag, setTag] = useState(config.tag ?? "");
	const [offset, setOffset] = useState(
		config.due_date_offset_minutes?.toString() ?? "0",
	);
	const [watchedTaskIds, setWatchedTaskIds] = useState<string[]>(
		config.watched_task_ids ?? [],
	);
	const [targetTaskId, setTargetTaskId] = useState(config.target_task_id ?? "");
	const [cronExpression, setCronExpression] = useState(
		config.cron_expression ?? "",
	);
	// Only predecessor_done still needs the full task list — for its watched-
	// tasks multi-select. TargetTaskPicker (shared by predecessor_done, cron,
	// and api_trigger) fetches its own paginated/searched list instead.
	const { data: tasks = [] } = useQuery({
		queryKey: ["projects", projectId, "tasks", "all-for-automation-picker"],
		queryFn: () => fetchAllProjectTasks(projectId),
		enabled: type === "predecessor_done",
	});

	if (type === "status_changed") {
		return (
			<div className="space-y-3">
				<div className="space-y-1.5">
					<Label>{t("automation.nodeConfig.trigger.statusLabel")}</Label>
					<Select
						value={statusId || "__any__"}
						onValueChange={(v) => setStatusId(!v || v === "__any__" ? "" : v)}
						disabled={!canEdit}
					>
						<SelectTrigger className="w-full">
							<SelectValue>
								{statusId
									? statuses.find((s) => s.id === statusId)?.name
									: t("automation.nodeConfig.trigger.anyStatus")}
							</SelectValue>
						</SelectTrigger>
						<SelectContent>
							<SelectItem value="__any__">
								{t("automation.nodeConfig.trigger.anyStatus")}
							</SelectItem>
							{statuses.map((s) => (
								<SelectItem key={s.id} value={s.id}>
									{s.name}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				</div>
				{canEdit && (
					<SaveButton
						saving={saving}
						onClick={() => onSave({ status_id: statusId || undefined })}
					/>
				)}
			</div>
		);
	}

	if (type === "tag_added") {
		return (
			<div className="space-y-3">
				<div className="space-y-1.5">
					<Label>{t("automation.nodeConfig.trigger.tagLabel")}</Label>
					<Input
						value={tag}
						onChange={(e) => setTag(e.target.value)}
						placeholder={t("automation.nodeConfig.trigger.anyTag")}
						disabled={!canEdit}
					/>
				</div>
				{canEdit && (
					<SaveButton
						saving={saving}
						onClick={() => onSave({ tag: tag || undefined })}
					/>
				)}
			</div>
		);
	}

	if (type === "due_date_reached") {
		return (
			<div className="space-y-3">
				<div className="space-y-1.5">
					<Label>{t("automation.nodeConfig.trigger.dueDateOffsetLabel")}</Label>
					<Input
						type="number"
						value={offset}
						onChange={(e) => setOffset(e.target.value)}
						disabled={!canEdit}
					/>
					<p className="text-xs text-muted-foreground">
						{t("automation.nodeConfig.trigger.dueDateOffsetHint")}
					</p>
				</div>
				{canEdit && (
					<SaveButton
						saving={saving}
						onClick={() =>
							onSave({ due_date_offset_minutes: Number(offset) || 0 })
						}
					/>
				)}
			</div>
		);
	}

	if (type === "predecessor_done") {
		const watchedTasks = watchedTaskIds
			.map((id) => tasks.find((t) => t.id === id))
			.filter((t): t is Task => t != null);
		const availableToWatch = tasks.filter(
			(t) => !watchedTaskIds.includes(t.id),
		);
		return (
			<div className="space-y-3">
				<div className="space-y-1.5">
					<Label>{t("automation.nodeConfig.trigger.targetTaskLabel")}</Label>
					<TargetTaskPicker
						projectId={projectId}
						value={targetTaskId}
						onChange={setTargetTaskId}
						canEdit={canEdit}
					/>
					<p className="text-xs text-muted-foreground">
						{t("automation.nodeConfig.trigger.targetTaskHint")}
					</p>
				</div>
				<div className="space-y-1.5">
					<Label>{t("automation.nodeConfig.trigger.watchedTasksLabel")}</Label>
					{canEdit && (
						<Select
							value=""
							onValueChange={(v) => {
								if (v) setWatchedTaskIds((prev) => [...prev, v]);
							}}
						>
							<SelectTrigger className="w-full">
								<SelectValue
									placeholder={t(
										"automation.nodeConfig.trigger.addWatchedTaskPlaceholder",
									)}
								/>
							</SelectTrigger>
							<SelectContent>
								{availableToWatch.map((t) => (
									<SelectItem key={t.id} value={t.id}>
										{taskLabel(t)}
									</SelectItem>
								))}
							</SelectContent>
						</Select>
					)}
					{watchedTasks.length > 0 && (
						<div className="flex flex-wrap gap-1.5 pt-1">
							{watchedTasks.map((t) => (
								<Badge key={t.id} variant="secondary" className="gap-1">
									{taskLabel(t)}
									{canEdit && (
										<button
											type="button"
											onClick={() =>
												setWatchedTaskIds((prev) =>
													prev.filter((id) => id !== t.id),
												)
											}
										>
											<X className="size-3" />
										</button>
									)}
								</Badge>
							))}
						</div>
					)}
					<p className="text-xs text-muted-foreground">
						{t("automation.nodeConfig.trigger.watchedTasksHint")}
					</p>
				</div>
				{canEdit && (
					<SaveButton
						saving={saving}
						onClick={() =>
							onSave({
								target_task_id: targetTaskId.trim() || undefined,
								watched_task_ids: watchedTaskIds,
							})
						}
					/>
				)}
			</div>
		);
	}

	if (type === "cron") {
		return (
			<div className="space-y-3">
				<div className="space-y-1.5">
					<Label>
						{t("automation.nodeConfig.trigger.cronExpressionLabel")}
					</Label>
					<Input
						value={cronExpression}
						onChange={(e) => setCronExpression(e.target.value)}
						placeholder={t(
							"automation.nodeConfig.trigger.cronExpressionPlaceholder",
						)}
						disabled={!canEdit}
					/>
					<p className="text-xs text-muted-foreground">
						{t("automation.nodeConfig.trigger.cronExpressionHint")}
					</p>
				</div>
				<div className="space-y-1.5">
					<Label>{t("automation.nodeConfig.trigger.targetTaskLabel")}</Label>
					<TargetTaskPicker
						projectId={projectId}
						value={targetTaskId}
						onChange={setTargetTaskId}
						canEdit={canEdit}
					/>
					<p className="text-xs text-muted-foreground">
						{t("automation.nodeConfig.trigger.cronTargetTaskHint")}
					</p>
				</div>
				{canEdit && (
					<SaveButton
						saving={saving}
						disabled={!cronExpression.trim()}
						onClick={() =>
							onSave({
								cron_expression: cronExpression.trim(),
								target_task_id: targetTaskId.trim() || undefined,
							})
						}
					/>
				)}
			</div>
		);
	}

	if (type === "api_trigger") {
		return (
			<div className="space-y-3">
				<div className="space-y-1.5">
					<Label>{t("automation.nodeConfig.trigger.targetTaskLabel")}</Label>
					<TargetTaskPicker
						projectId={projectId}
						value={targetTaskId}
						onChange={setTargetTaskId}
						canEdit={canEdit}
					/>
					<p className="text-xs text-muted-foreground">
						{t("automation.nodeConfig.trigger.apiTriggerTargetTaskHint")}
					</p>
				</div>
				{canEdit && (
					<SaveButton
						saving={saving}
						onClick={() =>
							onSave({ target_task_id: targetTaskId.trim() || undefined })
						}
					/>
				)}
				<WebhookTokenSection
					projectId={projectId}
					automationId={automationId}
					nodeId={nodeId}
					canEdit={canEdit}
				/>
			</div>
		);
	}

	// task_created, assignee_changed, priority_changed, and the four
	// sprint_* triggers — no config fields. (The backend supports narrowing
	// a sprint_* trigger to one sprint via TriggerConfig.SprintID, but
	// there's no picker for it here yet — every sprint_* trigger fires
	// project-wide for now.)
	return (
		<p className="text-xs text-muted-foreground">
			{t("automation.nodeConfig.trigger.noConfigNeeded")}
		</p>
	);
}

// TargetTaskPicker is the target-task picker shared by predecessor_done,
// cron, and api_trigger — all three fire on a basis unrelated to any
// specific task event, so all three need this same fixed-task picker.
// Mirrors the epic picker's dropdown (task-detail/properties-panel.tsx):
// a debounced server-side search plus an infinite-scroll paginated list,
// rather than loading every project task up front — a project can have far
// more tasks than fit comfortably in one query.
function TargetTaskPicker({
	projectId,
	value,
	onChange,
	canEdit,
}: {
	projectId: string;
	value: string;
	onChange: (value: string) => void;
	canEdit: boolean;
}) {
	const { t } = useTranslation("projects");
	const [open, setOpen] = useState(false);

	const {
		data: pages,
		fetchNextPage,
		hasNextPage,
		isFetchingNextPage,
	} = useInfiniteQuery({
		...tasksPickerInfiniteQueryOptions(projectId),
		enabled: open && !!projectId,
	});
	const tasks = useMemo(
		() => pages?.pages.flatMap((page) => page.items) ?? [],
		[pages],
	);
	const pagination = {
		hasMore: !!hasNextPage,
		isLoadingMore: isFetchingNextPage,
		onLoadMore: () => void fetchNextPage(),
	};

	const {
		search,
		setSearch,
		isSearching,
		results: searchResults,
		isLoading: searchLoading,
		pagination: searchPagination,
	} = useTaskPickerSearch(projectId, open);

	// The selected task may not be in the loaded page/search results yet —
	// fetch it directly so the trigger always shows the right label.
	const { data: selectedTask } = useQuery({
		...taskQueryOptions(projectId, value),
		enabled: !!projectId && !!value,
	});

	const displayedTasks = isSearching ? searchResults : tasks;
	const activePagination = isSearching ? searchPagination : pagination;

	return (
		<DropdownMenu open={open} onOpenChange={setOpen}>
			<DropdownMenuTrigger
				disabled={!canEdit}
				className="flex h-8 w-full items-center justify-between gap-1.5 rounded-lg border border-input bg-transparent px-2.5 py-2 text-sm outline-none disabled:cursor-not-allowed disabled:opacity-50 dark:bg-input/30"
			>
				<span className="truncate text-left">
					{selectedTask ? (
						taskLabel(selectedTask)
					) : (
						<span className="text-muted-foreground">
							{t("automation.nodeConfig.trigger.selectTaskPlaceholder")}
						</span>
					)}
				</span>
				<ChevronDown className="size-4 shrink-0 text-muted-foreground" />
			</DropdownMenuTrigger>
			<DropdownMenuContent align="start" className="w-72">
				{value && (
					<>
						<DropdownMenuItem
							className="text-destructive focus:text-destructive"
							onClick={() => onChange("")}
						>
							<X className="size-3.5 mr-2 shrink-0" />
							{t("automation.nodeConfig.trigger.clearTargetTask")}
						</DropdownMenuItem>
						<DropdownMenuSeparator />
					</>
				)}
				<div className="px-1 pb-1">
					<div className="relative">
						<Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground/50" />
						<input
							type="text"
							value={search}
							onChange={(e) => setSearch(e.target.value)}
							onKeyDown={(e) => {
								// Let Escape still close the menu; swallow everything
								// else so typing doesn't trigger the menu's own arrow-key
								// navigation / type-ahead item selection.
								if (e.key !== "Escape") e.stopPropagation();
							}}
							placeholder={t(
								"automation.nodeConfig.trigger.searchTaskPlaceholder",
							)}
							className="w-full rounded-lg border border-border/30 bg-muted/25 py-1.5 pr-2 pl-8 text-sm placeholder:text-muted-foreground/50 transition-all duration-150 focus:border-primary/40 focus:outline-none focus:ring-2 focus:ring-primary/20"
						/>
					</div>
				</div>
				<div
					className="max-h-56 overflow-y-auto"
					onScroll={createLoadMoreScrollHandler(activePagination)}
				>
					{isSearching && searchLoading ? (
						<div className="flex items-center justify-center py-4">
							<Loader2 className="size-4 animate-spin text-muted-foreground/50" />
						</div>
					) : (
						<>
							{displayedTasks.map((task) => (
								<DropdownMenuItem
									key={task.id}
									onClick={() => onChange(task.id)}
								>
									<span className="truncate">{taskLabel(task)}</span>
								</DropdownMenuItem>
							))}
							{displayedTasks.length === 0 && (
								<p className="px-2 py-4 text-center text-xs text-muted-foreground/50">
									{isSearching
										? t("automation.nodeConfig.trigger.noTasksFound")
										: t("automation.nodeConfig.trigger.noTasksYet")}
								</p>
							)}
						</>
					)}
					{activePagination.isLoadingMore && (
						<div className="flex items-center justify-center gap-1.5 py-2 text-xs text-muted-foreground/50">
							<Loader2 className="size-3 animate-spin" />
							{t("automation.nodeConfig.trigger.loadingMore")}
						</div>
					)}
				</div>
			</DropdownMenuContent>
		</DropdownMenu>
	);
}

// Every kind a condition leaf or action can retarget onto, relative to the
// walk's own bound task — "self" (the default, matching every automation's
// behavior before this existed) plus the four link-relation directions
// (mirroring the task-detail "Linked Tasks" section's vocabulary exactly),
// parent, children, and an explicit other task.
const TASK_TARGET_KINDS: TaskTargetKind[] = [
	"self",
	"parent",
	"children",
	"blocks",
	"is_blocked_by",
	"relates_to",
	"duplicates",
	"is_duplicated_by",
	"other",
];

// TaskTargetSelector is the shared "applies to" picker for both a
// condition leaf and an action — every action except call_api, and every
// condition leaf, can retarget itself onto a task other than the walk's
// own bound task via this same control.
function TaskTargetSelector({
	projectId,
	target,
	onChange,
	canEdit,
}: {
	projectId: string;
	target: TaskTarget | undefined;
	onChange: (target: TaskTarget | undefined) => void;
	canEdit: boolean;
}) {
	const { t } = useTranslation("projects");
	const kind = target?.kind ?? "self";
	return (
		<div className="space-y-1.5">
			<Select
				value={kind}
				onValueChange={(v) => {
					if (!v) return;
					const k = v as TaskTargetKind;
					onChange(k === "self" ? undefined : { kind: k });
				}}
				disabled={!canEdit}
			>
				<SelectTrigger className="h-7 w-full text-xs">
					<SelectValue>
						{t(`automation.nodeConfig.target.kinds.${kind}`)}
					</SelectValue>
				</SelectTrigger>
				<SelectContent>
					{TASK_TARGET_KINDS.map((k) => (
						<SelectItem key={k} value={k}>
							{t(`automation.nodeConfig.target.kinds.${k}`)}
						</SelectItem>
					))}
				</SelectContent>
			</Select>
			{kind === "other" && (
				<TargetTaskPicker
					projectId={projectId}
					value={target?.other_task_id ?? ""}
					onChange={(taskId) =>
						onChange({ kind: "other", other_task_id: taskId || undefined })
					}
					canEdit={canEdit}
				/>
			)}
		</div>
	);
}

// MatchModeSelector picks how a retargeted condition leaf's resolved tasks
// (when the target can resolve to more than one) combine into a single
// true/false. Only rendered when the leaf's target is actually multi-valued.
function MatchModeSelector({
	value,
	onChange,
	canEdit,
}: {
	value: "any" | "all" | undefined;
	onChange: (mode: "any" | "all") => void;
	canEdit: boolean;
}) {
	const { t } = useTranslation("projects");
	const mode = value ?? "any";
	return (
		<Select
			value={mode}
			onValueChange={(v) => v && onChange(v as "any" | "all")}
			disabled={!canEdit}
		>
			<SelectTrigger className="h-7 w-full text-xs">
				<SelectValue>
					{t(`automation.nodeConfig.target.matchMode.${mode}`)}
				</SelectValue>
			</SelectTrigger>
			<SelectContent>
				<SelectItem value="any">
					{t("automation.nodeConfig.target.matchMode.any")}
				</SelectItem>
				<SelectItem value="all">
					{t("automation.nodeConfig.target.matchMode.all")}
				</SelectItem>
			</SelectContent>
		</Select>
	);
}

// WebhookTokenSection generates/regenerates an api_trigger node's webhook
// secret. The raw token is shown exactly once (right after generation) and
// never fetched back afterward — mirrors the "show once" pattern used for
// user API keys (apps/web/src/routes/_authenticated/profile/api-keys.tsx).
function WebhookTokenSection({
	projectId,
	automationId,
	nodeId,
	canEdit,
}: {
	projectId: string;
	automationId: string;
	nodeId: string;
	canEdit: boolean;
}) {
	const { t } = useTranslation("projects");
	const [revealedToken, setRevealedToken] = useState<string | null>(null);
	const [copied, setCopied] = useState<"token" | "url" | null>(null);
	const [generating, setGenerating] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const webhookUrl = `${window.location.origin}/api/v1/webhooks/automations/${nodeId}`;

	async function handleGenerate() {
		setGenerating(true);
		setError(null);
		try {
			const result = await generateWebhookToken(
				projectId,
				automationId,
				nodeId,
			);
			setRevealedToken(result.token);
			setCopied(null);
		} catch {
			setError(t("automation.builder.genericError"));
		} finally {
			setGenerating(false);
		}
	}

	async function handleCopy(value: string, which: "token" | "url") {
		try {
			await navigator.clipboard.writeText(value);
			setCopied(which);
		} catch {
			// Clipboard access can fail (permissions, insecure context); the
			// user can still select-and-copy the visible text manually.
		}
	}

	return (
		<div className="space-y-2 border-t border-border/60 pt-3">
			<div className="space-y-1.5">
				<Label>{t("automation.nodeConfig.trigger.webhookUrlLabel")}</Label>
				<div className="flex items-center gap-2">
					<code className="flex-1 truncate rounded-md bg-muted px-2 py-1.5 text-xs">
						{webhookUrl}
					</code>
					<Button
						variant="outline"
						size="icon"
						className="size-7 shrink-0"
						onClick={() => handleCopy(webhookUrl, "url")}
					>
						<Copy className="size-3.5" />
					</Button>
				</div>
				{copied === "url" && (
					<p className="text-xs text-green-600">
						{t("automation.nodeConfig.trigger.copied")}
					</p>
				)}
			</div>

			<div className="space-y-1.5">
				<Label>{t("automation.nodeConfig.trigger.webhookTokenLabel")}</Label>
				{revealedToken ? (
					<>
						<div className="flex items-center gap-2">
							<code className="flex-1 truncate rounded-md bg-muted px-2 py-1.5 text-xs">
								{revealedToken}
							</code>
							<Button
								variant="outline"
								size="icon"
								className="size-7 shrink-0"
								onClick={() => handleCopy(revealedToken, "token")}
							>
								<Copy className="size-3.5" />
							</Button>
						</div>
						{copied === "token" && (
							<p className="text-xs text-green-600">
								{t("automation.nodeConfig.trigger.copied")}
							</p>
						)}
						<p className="text-xs text-destructive">
							{t("automation.nodeConfig.trigger.tokenShownOnceWarning")}
						</p>
					</>
				) : (
					<p className="text-xs text-muted-foreground">
						{t("automation.nodeConfig.trigger.noTokenYet")}
					</p>
				)}
			</div>
			{error && <p className="text-xs text-destructive">{error}</p>}
			{canEdit && (
				<Button
					type="button"
					variant="outline"
					size="sm"
					disabled={generating}
					onClick={handleGenerate}
				>
					{revealedToken
						? t("automation.nodeConfig.trigger.regenerateTokenButton")
						: t("automation.nodeConfig.trigger.generateTokenButton")}
				</Button>
			)}
		</div>
	);
}

// ── Condition config form ────────────────────────────────────────────────────
// Each branch is edited as a single leaf comparison (the common case); the
// backend/domain model supports full nested AND/OR/NOT trees for
// API/MCP-authored automations, but the canvas form intentionally keeps the
// authoring surface simple.

function ConditionConfigForm({
	config,
	projectId,
	statuses,
	members,
	customFields,
	taskTypes,
	canEdit,
	onSave,
	saving,
}: {
	config: ConditionConfig;
	projectId: string;
	statuses: TaskStatus[];
	members: ProjectMember[];
	customFields: CustomFieldDefinition[];
	taskTypes: TaskType[];
	canEdit: boolean;
	onSave: (config: Record<string, unknown>) => void;
	saving?: boolean;
}) {
	const { t } = useTranslation("projects");
	const [branches, setBranches] = useState<ConditionBranch[]>(
		config.branches ?? [],
	);
	const { data: sprints } = useQuery({
		...sprintsQueryOptions(projectId),
		enabled: !!projectId,
	});

	useEffect(() => {
		setBranches(config.branches ?? []);
	}, [config.branches]);

	function updateBranch(index: number, patch: Partial<ConditionBranch>) {
		setBranches((prev) =>
			prev.map((b, i) => (i === index ? { ...b, ...patch } : b)),
		);
	}

	function updateLeaf(index: number, patch: Partial<ConditionLeaf>) {
		setBranches((prev) =>
			prev.map((b, i) => {
				if (i !== index) return b;
				const current: ConditionLeaf = b.tree ?? {
					field: "status_id",
					operator: "equals",
				};
				return { ...b, tree: { ...current, ...patch } };
			}),
		);
	}

	function addBranch() {
		const handle = `branch_${branches.length + 1}_${Date.now().toString(36)}`;
		setBranches((prev) => [
			...prev,
			{
				handle,
				label: "",
				tree: { field: "status_id", operator: "equals" },
			},
		]);
	}

	function removeBranch(index: number) {
		setBranches((prev) => prev.filter((_, i) => i !== index));
	}

	return (
		<div className="space-y-4">
			<div className="text-xs font-semibold text-muted-foreground">
				{t("automation.nodeConfig.condition.branchesTitle")}
			</div>
			{branches.map((branch, i) => {
				const leaf = branch.tree;
				const field = leaf?.field ?? "status_id";
				const operators = OPERATORS_BY_FIELD[field] ?? [];
				const selectedCustomField = customFields.find(
					(cf) => cf.field_key === leaf?.field_key,
				);
				return (
					<div
						// biome-ignore lint/suspicious/noArrayIndexKey: branch handles aren't stable input keys until saved
						key={i}
						className="space-y-2 rounded-lg border border-border/60 p-2.5"
					>
						<div className="flex items-center gap-2">
							<Input
								value={branch.label ?? ""}
								onChange={(e) => updateBranch(i, { label: e.target.value })}
								placeholder={t("automation.nodeConfig.condition.branchLabel")}
								disabled={!canEdit}
								className="h-7 text-xs"
							/>
							{canEdit && (
								<button
									type="button"
									onClick={() => removeBranch(i)}
									className="shrink-0 text-muted-foreground hover:text-destructive"
								>
									<Trash2 className="size-3.5" />
								</button>
							)}
						</div>
						<div className="grid grid-cols-2 gap-1.5">
							<Select
								value={field}
								onValueChange={(v) => {
									if (!v) return;
									const f = v as ConditionField;
									updateLeaf(i, {
										field: f,
										operator: (OPERATORS_BY_FIELD[f] ?? [])[0],
									});
								}}
								disabled={!canEdit}
							>
								<SelectTrigger className="h-7 text-xs">
									<SelectValue>
										{t(`automation.nodeConfig.condition.fields.${field}`)}
									</SelectValue>
								</SelectTrigger>
								<SelectContent>
									{CONDITION_FIELDS.map((f) => (
										<SelectItem key={f} value={f}>
											{t(`automation.nodeConfig.condition.fields.${f}`)}
										</SelectItem>
									))}
								</SelectContent>
							</Select>
							<Select
								value={leaf?.operator ?? operators[0]}
								onValueChange={(v) => {
									if (v) updateLeaf(i, { operator: v as ConditionOperator });
								}}
								disabled={!canEdit}
							>
								<SelectTrigger className="h-7 text-xs">
									<SelectValue />
								</SelectTrigger>
								<SelectContent>
									{operators.map((op) => (
										<SelectItem key={op} value={op}>
											{op}
										</SelectItem>
									))}
								</SelectContent>
							</Select>
						</div>
						{field === "custom_field" && (
							<Select
								value={leaf?.field_key ?? ""}
								onValueChange={(v) => updateLeaf(i, { field_key: v ?? "" })}
								disabled={!canEdit}
							>
								<SelectTrigger className="h-7 text-xs">
									<SelectValue
										placeholder={t(
											"automation.nodeConfig.action.fieldKeyLabel",
										)}
									>
										{selectedCustomField?.display_name}
									</SelectValue>
								</SelectTrigger>
								<SelectContent>
									{customFields.map((cf) => (
										<SelectItem key={cf.id} value={cf.field_key}>
											{cf.display_name}
										</SelectItem>
									))}
								</SelectContent>
							</Select>
						)}
						{leaf?.operator !== "is_empty" &&
							leaf?.operator !== "is_not_empty" &&
							(field === "status_id" ? (
								<Select
									value={(leaf?.value as string) ?? ""}
									onValueChange={(v) => updateLeaf(i, { value: v ?? "" })}
									disabled={!canEdit}
								>
									<SelectTrigger className="h-7 w-full text-xs">
										<SelectValue
											placeholder={t("automation.nodeConfig.condition.value")}
										>
											{statuses.find((s) => s.id === leaf?.value)?.name}
										</SelectValue>
									</SelectTrigger>
									<SelectContent>
										{statuses.map((s) => (
											<SelectItem key={s.id} value={s.id}>
												{s.name}
											</SelectItem>
										))}
									</SelectContent>
								</Select>
							) : field === "task_type_id" ? (
								<Select
									value={(leaf?.value as string) ?? ""}
									onValueChange={(v) => updateLeaf(i, { value: v ?? "" })}
									disabled={!canEdit}
								>
									<SelectTrigger className="h-7 w-full text-xs">
										<SelectValue
											placeholder={t("automation.nodeConfig.condition.value")}
										>
											{taskTypes.find((tt) => tt.id === leaf?.value)?.name}
										</SelectValue>
									</SelectTrigger>
									<SelectContent>
										{taskTypes.map((tt) => (
											<SelectItem key={tt.id} value={tt.id}>
												{tt.name}
											</SelectItem>
										))}
									</SelectContent>
								</Select>
							) : field === "assignee_ids" || field === "reporter_id" ? (
								<Select
									value={(leaf?.value as string) ?? ""}
									onValueChange={(v) => updateLeaf(i, { value: v ?? "" })}
									disabled={!canEdit}
								>
									<SelectTrigger className="h-7 w-full text-xs">
										<SelectValue
											placeholder={t("automation.nodeConfig.condition.value")}
										>
											{(() => {
												const assignee = members.find(
													(m) => m.id === leaf?.value,
												);
												return assignee?.full_name || assignee?.username;
											})()}
										</SelectValue>
									</SelectTrigger>
									<SelectContent>
										{members.map((m) => (
											<SelectItem key={m.id} value={m.id}>
												{m.full_name || m.username}
											</SelectItem>
										))}
									</SelectContent>
								</Select>
							) : field === "sprint_id" ? (
								<Select
									value={(leaf?.value as string) ?? ""}
									onValueChange={(v) => updateLeaf(i, { value: v ?? "" })}
									disabled={!canEdit}
								>
									<SelectTrigger className="h-7 w-full text-xs">
										<SelectValue
											placeholder={t("automation.nodeConfig.condition.value")}
										>
											{(sprints ?? []).find((s) => s.id === leaf?.value)?.name}
										</SelectValue>
									</SelectTrigger>
									<SelectContent>
										{(sprints ?? []).map((s) => (
											<SelectItem key={s.id} value={s.id}>
												{s.name}
											</SelectItem>
										))}
									</SelectContent>
								</Select>
							) : field === "parent_task_id" ? (
								<TargetTaskPicker
									projectId={projectId}
									value={(leaf?.value as string) ?? ""}
									onChange={(taskId) => updateLeaf(i, { value: taskId })}
									canEdit={canEdit}
								/>
							) : field === "start_date" ||
								field === "due_date" ||
								field === "sprint_start_date" ||
								field === "sprint_end_date" ? (
								<Input
									type="date"
									value={toDateInputValue(leaf?.value as string)}
									onChange={(e) =>
										updateLeaf(i, { value: fromDateInputValue(e.target.value) })
									}
									disabled={!canEdit}
									className="h-7 text-xs"
								/>
							) : field === "sprint_status" ? (
								<Select
									value={(leaf?.value as string) ?? ""}
									onValueChange={(v) => updateLeaf(i, { value: v ?? "" })}
									disabled={!canEdit}
								>
									<SelectTrigger className="h-7 w-full text-xs">
										<SelectValue
											placeholder={t("automation.nodeConfig.condition.value")}
										/>
									</SelectTrigger>
									<SelectContent>
										<SelectItem value="planned">
											{t("automation.nodeConfig.action.sprintStatusPlanned")}
										</SelectItem>
										<SelectItem value="active">
											{t("automation.nodeConfig.action.sprintStatusActive")}
										</SelectItem>
										<SelectItem value="completed">
											{t("automation.nodeConfig.action.sprintStatusCompleted")}
										</SelectItem>
									</SelectContent>
								</Select>
							) : field === "story_points" ? (
								<Input
									type="number"
									value={(leaf?.value as string) ?? ""}
									onChange={(e) => updateLeaf(i, { value: e.target.value })}
									placeholder={t("automation.nodeConfig.condition.value")}
									disabled={!canEdit}
									className="h-7 text-xs"
								/>
							) : field === "custom_field" &&
								selectedCustomField &&
								selectedCustomField.options.length > 0 ? (
								<Select
									value={(leaf?.value as string) ?? ""}
									onValueChange={(v) => updateLeaf(i, { value: v ?? "" })}
									disabled={!canEdit}
								>
									<SelectTrigger className="h-7 w-full text-xs">
										<SelectValue
											placeholder={t("automation.nodeConfig.condition.value")}
										>
											{leaf?.value as string}
										</SelectValue>
									</SelectTrigger>
									<SelectContent>
										{selectedCustomField.options.map((opt) => (
											<SelectItem key={opt} value={opt}>
												{opt}
											</SelectItem>
										))}
									</SelectContent>
								</Select>
							) : (
								<Input
									value={(leaf?.value as string) ?? ""}
									onChange={(e) => updateLeaf(i, { value: e.target.value })}
									placeholder={t("automation.nodeConfig.condition.value")}
									disabled={!canEdit}
									className="h-7 text-xs"
								/>
							))}
						{/* A sprint-scoped field (see SPRINT_CONDITION_FIELDS) always
						 * evaluates against the walk's own sprint — Target has no
						 * effect on it server-side, so the picker is hidden rather
						 * than shown non-functional. */}
						{!SPRINT_CONDITION_FIELDS.includes(field) && (
							<div className="space-y-1">
								<Label className="text-[10px] text-muted-foreground">
									{t("automation.nodeConfig.target.label")}
								</Label>
								<TaskTargetSelector
									projectId={projectId}
									target={leaf?.target}
									onChange={(target) => updateLeaf(i, { target })}
									canEdit={canEdit}
								/>
								{leaf?.target &&
									MULTI_VALUED_TARGET_KINDS.includes(leaf.target.kind) && (
										<MatchModeSelector
											value={leaf?.match_mode}
											onChange={(mode) => updateLeaf(i, { match_mode: mode })}
											canEdit={canEdit}
										/>
									)}
							</div>
						)}
					</div>
				);
			})}
			<div className="rounded-lg border border-dashed border-border/60 p-2.5 text-xs text-muted-foreground">
				{t("automation.nodeConfig.condition.elseLabel")}
			</div>
			{canEdit && (
				<>
					<Button
						variant="outline"
						size="sm"
						className="w-full gap-1.5"
						onClick={addBranch}
					>
						<Plus className="size-3.5" />
						{t("automation.nodeConfig.condition.addBranch")}
					</Button>
					<SaveButton saving={saving} onClick={() => onSave({ branches })} />
				</>
			)}
		</div>
	);
}

// ── Action config form ───────────────────────────────────────────────────────

function ActionConfigForm({
	type,
	config,
	projectId,
	statuses,
	members,
	customFields,
	taskTypes,
	canEdit,
	onSave,
	saving,
}: {
	type: string;
	config: ActionConfig;
	projectId: string;
	statuses: TaskStatus[];
	members: ProjectMember[];
	customFields: CustomFieldDefinition[];
	taskTypes: TaskType[];
	canEdit: boolean;
	onSave: (config: Record<string, unknown>) => void;
	saving?: boolean;
}) {
	const { t } = useTranslation("projects");
	const [target, setTarget] = useState<TaskTarget | undefined>(config.target);
	const [message, setMessage] = useState(config.message ?? "");
	const [method, setMethod] = useState(config.method ?? "GET");
	const [url, setUrl] = useState(config.url ?? "");
	const [headerRows, setHeaderRows] = useState<
		{ key: string; value: string }[]
	>(
		config.headers
			? Object.entries(config.headers).map(([key, value]) => ({
					key,
					value,
				}))
			: [],
	);
	const [body, setBody] = useState(config.body ?? "");
	const [waitMinutes, setWaitMinutes] = useState(
		config.wait_minutes?.toString() ?? "",
	);
	const [sprintName, setSprintName] = useState(
		config.sprint_update?.name ?? "",
	);
	const [sprintStartDate, setSprintStartDate] = useState(
		config.sprint_update?.start_date ?? "",
	);
	const [sprintEndDate, setSprintEndDate] = useState(
		config.sprint_update?.end_date ?? "",
	);
	const [sprintGoal, setSprintGoal] = useState(
		config.sprint_update?.goal ?? "",
	);
	const [sprintStatus, setSprintStatus] = useState(
		config.sprint_update?.status ?? "",
	);
	const [moveToSprintId, setMoveToSprintId] = useState(
		config.move_to_sprint_id ?? "",
	);

	// update_task's fields, each independently enabled — an unset field is
	// left untouched on the task; only enabled fields are sent.
	const initialFields = config.update ?? {};
	const [memberId, setMemberId] = useState(config.member_id ?? "");
	const [enabled, setEnabled] = useState<Set<keyof TaskFieldUpdate>>(
		() => new Set(Object.keys(initialFields) as (keyof TaskFieldUpdate)[]),
	);
	const [fields, setFields] = useState<TaskFieldUpdate>(initialFields);
	const { data: sprints } = useQuery({
		...sprintsQueryOptions(projectId),
		enabled: !!projectId,
	});

	function toggleField(key: keyof TaskFieldUpdate, on: boolean) {
		setEnabled((prev) => {
			const next = new Set(prev);
			if (on) next.add(key);
			else next.delete(key);
			return next;
		});
	}

	function setFieldValue<K extends keyof TaskFieldUpdate>(
		key: K,
		value: TaskFieldUpdate[K],
	) {
		setFields((prev) => ({ ...prev, [key]: value }));
	}

	if (type === "trigger_ai_agent") {
		const assignableMembers = members.filter((m) => m.member_type === "agent");
		return (
			<div className="space-y-3">
				<div className="space-y-1.5">
					<Label>{t("automation.nodeConfig.action.agentLabel")}</Label>
					<Select
						value={memberId}
						onValueChange={(v) => setMemberId(v ?? "")}
						disabled={!canEdit}
					>
						<SelectTrigger className="w-full">
							<SelectValue>
								{(() => {
									const selected = assignableMembers.find(
										(m) => m.id === memberId,
									);
									return selected?.full_name || selected?.username;
								})()}
							</SelectValue>
						</SelectTrigger>
						<SelectContent>
							{assignableMembers.map((m) => (
								<SelectItem key={m.id} value={m.id}>
									{m.full_name || m.username}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				</div>
				<div className="space-y-1.5">
					<Label>{t("automation.nodeConfig.action.messageLabel")}</Label>
					<Textarea
						value={message}
						onChange={(e) => setMessage(e.target.value)}
						placeholder={t("automation.nodeConfig.action.messagePlaceholder")}
						rows={4}
						disabled={!canEdit}
					/>
					<p className="text-xs text-muted-foreground">
						{t("automation.nodeConfig.action.variablesHint")}
					</p>
				</div>
				<div className="space-y-1">
					<Label className="text-[10px] text-muted-foreground">
						{t("automation.nodeConfig.target.label")}
					</Label>
					<TaskTargetSelector
						projectId={projectId}
						target={target}
						onChange={setTarget}
						canEdit={canEdit}
					/>
				</div>
				{canEdit && (
					<SaveButton
						saving={saving}
						disabled={!memberId}
						onClick={() => onSave({ member_id: memberId, message, target })}
					/>
				)}
			</div>
		);
	}

	if (type === "wait") {
		const minutes = Math.floor(Number(waitMinutes));
		return (
			<div className="space-y-3">
				<div className="space-y-1.5">
					<Label>{t("automation.nodeConfig.action.waitMinutesLabel")}</Label>
					<Input
						type="number"
						min={1}
						step={1}
						value={waitMinutes}
						onChange={(e) => setWaitMinutes(e.target.value)}
						disabled={!canEdit}
					/>
					<p className="text-xs text-muted-foreground">
						{t("automation.nodeConfig.action.waitMinutesHint")}
					</p>
				</div>
				{canEdit && (
					<SaveButton
						saving={saving}
						disabled={!(minutes > 0)}
						onClick={() => onSave({ wait_minutes: minutes })}
					/>
				)}
			</div>
		);
	}

	if (type === "update_sprint") {
		const hasAnyField =
			sprintName.trim() !== "" ||
			sprintStartDate !== "" ||
			sprintEndDate !== "" ||
			sprintGoal.trim() !== "" ||
			sprintStatus !== "";
		return (
			<div className="space-y-3">
				<p className="text-xs text-muted-foreground">
					{t("automation.nodeConfig.action.updateSprintHint")}{" "}
					{t("automation.nodeConfig.action.variablesHint")}
				</p>
				<div className="space-y-1.5">
					<Label>{t("automation.nodeConfig.action.sprintNameLabel")}</Label>
					<Input
						value={sprintName}
						onChange={(e) => setSprintName(e.target.value)}
						disabled={!canEdit}
					/>
				</div>
				<div className="space-y-1.5">
					<Label>
						{t("automation.nodeConfig.action.sprintStartDateLabel")}
					</Label>
					<Input
						type="date"
						value={toDateInputValue(sprintStartDate)}
						onChange={(e) =>
							setSprintStartDate(fromDateInputValue(e.target.value))
						}
						disabled={!canEdit}
					/>
				</div>
				<div className="space-y-1.5">
					<Label>{t("automation.nodeConfig.action.sprintEndDateLabel")}</Label>
					<Input
						type="date"
						value={toDateInputValue(sprintEndDate)}
						onChange={(e) =>
							setSprintEndDate(fromDateInputValue(e.target.value))
						}
						disabled={!canEdit}
					/>
				</div>
				<div className="space-y-1.5">
					<Label>{t("automation.nodeConfig.action.sprintGoalLabel")}</Label>
					<Textarea
						value={sprintGoal}
						onChange={(e) => setSprintGoal(e.target.value)}
						rows={3}
						disabled={!canEdit}
					/>
				</div>
				<div className="space-y-1.5">
					<Label>{t("automation.nodeConfig.action.sprintStatusLabel")}</Label>
					<Select
						value={sprintStatus || "__unchanged__"}
						onValueChange={(v) =>
							setSprintStatus(
								!v || v === "__unchanged__"
									? ""
									: (v as "planned" | "active" | "completed"),
							)
						}
						disabled={!canEdit}
					>
						<SelectTrigger className="w-full">
							<SelectValue>
								{
									{
										"": t("automation.nodeConfig.action.sprintStatusUnchanged"),
										planned: t(
											"automation.nodeConfig.action.sprintStatusPlanned",
										),
										active: t(
											"automation.nodeConfig.action.sprintStatusActive",
										),
										completed: t(
											"automation.nodeConfig.action.sprintStatusCompleted",
										),
									}[sprintStatus]
								}
							</SelectValue>
						</SelectTrigger>
						<SelectContent>
							<SelectItem value="__unchanged__">
								{t("automation.nodeConfig.action.sprintStatusUnchanged")}
							</SelectItem>
							<SelectItem value="planned">
								{t("automation.nodeConfig.action.sprintStatusPlanned")}
							</SelectItem>
							<SelectItem value="active">
								{t("automation.nodeConfig.action.sprintStatusActive")}
							</SelectItem>
							<SelectItem value="completed">
								{t("automation.nodeConfig.action.sprintStatusCompleted")}
							</SelectItem>
						</SelectContent>
					</Select>
				</div>
				{canEdit && (
					<SaveButton
						saving={saving}
						disabled={!hasAnyField}
						onClick={() =>
							onSave({
								sprint_update: {
									name: sprintName.trim() || undefined,
									start_date: sprintStartDate || undefined,
									end_date: sprintEndDate || undefined,
									goal: sprintGoal.trim() || undefined,
									status: sprintStatus || undefined,
								},
							})
						}
					/>
				)}
			</div>
		);
	}

	if (type === "complete_sprint") {
		return (
			<div className="space-y-3">
				<p className="text-xs text-muted-foreground">
					{t("automation.nodeConfig.action.completeSprintHint")}
				</p>
				<div className="space-y-1.5">
					<Label>{t("automation.nodeConfig.action.moveToSprintLabel")}</Label>
					<Select
						value={moveToSprintId || "__backlog__"}
						onValueChange={(v) =>
							setMoveToSprintId(!v || v === "__backlog__" ? "" : v)
						}
						disabled={!canEdit}
					>
						<SelectTrigger className="w-full">
							<SelectValue>
								{moveToSprintId
									? (sprints ?? []).find((s) => s.id === moveToSprintId)?.name
									: t("automation.nodeConfig.action.moveToSprintBacklog")}
							</SelectValue>
						</SelectTrigger>
						<SelectContent>
							<SelectItem value="__backlog__">
								{t("automation.nodeConfig.action.moveToSprintBacklog")}
							</SelectItem>
							{(sprints ?? []).map((s) => (
								<SelectItem key={s.id} value={s.id}>
									{s.name}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
					<p className="text-xs text-muted-foreground">
						{t("automation.nodeConfig.action.moveToSprintHint")}
					</p>
				</div>
				{canEdit && (
					<SaveButton
						saving={saving}
						onClick={() =>
							onSave({ move_to_sprint_id: moveToSprintId || undefined })
						}
					/>
				)}
			</div>
		);
	}

	if (type === "call_api") {
		const headersObject = () => {
			const obj: Record<string, string> = {};
			for (const row of headerRows) {
				if (row.key.trim()) obj[row.key.trim()] = row.value;
			}
			return obj;
		};
		return (
			<div className="space-y-3">
				<div className="grid grid-cols-[6rem_1fr] gap-1.5">
					<div className="space-y-1.5">
						<Label>{t("automation.nodeConfig.action.methodLabel")}</Label>
						<Select
							value={method}
							onValueChange={(v) => v && setMethod(v)}
							disabled={!canEdit}
						>
							<SelectTrigger className="w-full">
								<SelectValue>{method}</SelectValue>
							</SelectTrigger>
							<SelectContent>
								{["GET", "POST", "PUT", "PATCH", "DELETE"].map((m) => (
									<SelectItem key={m} value={m}>
										{m}
									</SelectItem>
								))}
							</SelectContent>
						</Select>
					</div>
					<div className="space-y-1.5">
						<Label>{t("automation.nodeConfig.action.urlLabel")}</Label>
						<Input
							value={url}
							onChange={(e) => setUrl(e.target.value)}
							placeholder={t("automation.nodeConfig.action.urlPlaceholder")}
							disabled={!canEdit}
						/>
					</div>
				</div>
				<p className="text-xs text-muted-foreground">
					{t("automation.nodeConfig.action.variablesHint")}
				</p>
				<div className="space-y-1.5">
					<Label>{t("automation.nodeConfig.action.headersLabel")}</Label>
					{headerRows.map((row, i) => (
						<div
							// biome-ignore lint/suspicious/noArrayIndexKey: header rows aren't stable input keys until saved
							key={i}
							className="flex items-center gap-1.5"
						>
							<Input
								value={row.key}
								onChange={(e) =>
									setHeaderRows((prev) =>
										prev.map((r, idx) =>
											idx === i ? { ...r, key: e.target.value } : r,
										),
									)
								}
								placeholder={t(
									"automation.nodeConfig.action.headerKeyPlaceholder",
								)}
								disabled={!canEdit}
								className="h-7 text-xs"
							/>
							<Input
								value={row.value}
								onChange={(e) =>
									setHeaderRows((prev) =>
										prev.map((r, idx) =>
											idx === i ? { ...r, value: e.target.value } : r,
										),
									)
								}
								placeholder={t(
									"automation.nodeConfig.action.headerValuePlaceholder",
								)}
								disabled={!canEdit}
								className="h-7 text-xs"
							/>
							{canEdit && (
								<button
									type="button"
									onClick={() =>
										setHeaderRows((prev) => prev.filter((_, idx) => idx !== i))
									}
									className="shrink-0 text-muted-foreground hover:text-destructive"
								>
									<Trash2 className="size-3.5" />
								</button>
							)}
						</div>
					))}
					{canEdit && (
						<Button
							variant="outline"
							size="sm"
							className="w-full gap-1.5"
							onClick={() =>
								setHeaderRows((prev) => [...prev, { key: "", value: "" }])
							}
						>
							<Plus className="size-3.5" />
							{t("automation.nodeConfig.action.addHeader")}
						</Button>
					)}
				</div>
				<div className="space-y-1.5">
					<Label>{t("automation.nodeConfig.action.bodyLabel")}</Label>
					<Textarea
						value={body}
						onChange={(e) => setBody(e.target.value)}
						rows={4}
						className="font-mono text-xs"
						disabled={!canEdit}
					/>
				</div>
				{canEdit && (
					<SaveButton
						saving={saving}
						disabled={!method || !url.trim()}
						onClick={() =>
							onSave({
								method,
								url: url.trim(),
								headers: headersObject(),
								body,
							})
						}
					/>
				)}
			</div>
		);
	}

	// update_task: dynamic field-update rows. Add a field via the picker,
	// which pairs it with a type-appropriate value editor below; remove it
	// via the trash icon. Fields never added here are left untouched on
	// the task (see TaskFieldUpdate — unset ≠ cleared).
	const selectedCustomFieldKeys = Object.keys(fields.custom_fields ?? {});
	const addableFields = FIELD_UPDATE_ORDER.filter((k) => !enabled.has(k));

	function fieldLabel(key: UpdatableField): string {
		if (key === "custom_fields") {
			return t("automation.nodeConfig.action.customFieldsLabel");
		}
		return t(`automation.nodeConfig.condition.fields.${key}`);
	}

	function renderFieldEditor(key: UpdatableField) {
		switch (key) {
			case "title":
				return (
					<Input
						value={fields.title ?? ""}
						onChange={(e) => setFieldValue("title", e.target.value)}
						disabled={!canEdit}
						className="h-7 text-xs"
					/>
				);
			case "status_id":
				return (
					<Select
						value={fields.status_id ?? ""}
						onValueChange={(v) => setFieldValue("status_id", v ?? undefined)}
						disabled={!canEdit}
					>
						<SelectTrigger className="h-7 w-full text-xs">
							<SelectValue>
								{statuses.find((s) => s.id === fields.status_id)?.name}
							</SelectValue>
						</SelectTrigger>
						<SelectContent>
							{statuses.map((s) => (
								<SelectItem key={s.id} value={s.id}>
									{s.name}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				);
			case "task_type_id":
				return (
					<Select
						value={fields.task_type_id ?? ""}
						onValueChange={(v) => setFieldValue("task_type_id", v ?? undefined)}
						disabled={!canEdit}
					>
						<SelectTrigger className="h-7 w-full text-xs">
							<SelectValue>
								{taskTypes.find((tt) => tt.id === fields.task_type_id)?.name}
							</SelectValue>
						</SelectTrigger>
						<SelectContent>
							{taskTypes.map((tt) => (
								<SelectItem key={tt.id} value={tt.id}>
									{tt.name}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				);
			case "assignee_ids":
				return (
					<Select
						value={fields.assignee_ids?.[0] ?? ""}
						onValueChange={(v) =>
							setFieldValue("assignee_ids", v ? [v] : undefined)
						}
						disabled={!canEdit}
					>
						<SelectTrigger className="h-7 w-full text-xs">
							<SelectValue>
								{(() => {
									const m = members.find(
										(mem) => mem.id === fields.assignee_ids?.[0],
									);
									return m?.full_name || m?.username;
								})()}
							</SelectValue>
						</SelectTrigger>
						<SelectContent>
							{members.map((m) => (
								<SelectItem key={m.id} value={m.id}>
									{m.full_name || m.username}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				);
			case "importance":
				return (
					<Select
						value={getImportanceBucket(fields.importance ?? 0).toString()}
						onValueChange={(v) =>
							v &&
							setFieldValue(
								"importance",
								IMPORTANCE_BUCKET_VALUES[Number(v)] ?? 0,
							)
						}
						disabled={!canEdit}
					>
						<SelectTrigger className="h-7 w-full text-xs">
							<SelectValue>
								{(() => {
									const level = PRIORITY_LEVELS.find(
										(l) =>
											l.value.toString() ===
											getImportanceBucket(fields.importance ?? 0).toString(),
									);
									return level ? t(level.labelKey) : undefined;
								})()}
							</SelectValue>
						</SelectTrigger>
						<SelectContent>
							{PRIORITY_LEVELS.map((level) => (
								<SelectItem key={level.value} value={level.value.toString()}>
									{t(level.labelKey)}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				);
			case "story_points":
				return (
					<Input
						type="number"
						value={fields.story_points ?? ""}
						onChange={(e) =>
							setFieldValue(
								"story_points",
								e.target.value ? Number(e.target.value) : undefined,
							)
						}
						disabled={!canEdit}
						className="h-7 text-xs"
					/>
				);
			case "tags":
				return (
					<Input
						value={(fields.tags ?? []).join(", ")}
						onChange={(e) =>
							setFieldValue(
								"tags",
								e.target.value
									.split(",")
									.map((s) => s.trim())
									.filter(Boolean),
							)
						}
						placeholder={t("automation.nodeConfig.action.tagLabel")}
						disabled={!canEdit}
						className="h-7 text-xs"
					/>
				);
			case "reporter_id":
				return (
					<Select
						value={fields.reporter_id ?? ""}
						onValueChange={(v) => setFieldValue("reporter_id", v ?? undefined)}
						disabled={!canEdit}
					>
						<SelectTrigger className="h-7 w-full text-xs">
							<SelectValue>
								{(() => {
									const m = members.find(
										(mem) => mem.id === fields.reporter_id,
									);
									return m?.full_name || m?.username;
								})()}
							</SelectValue>
						</SelectTrigger>
						<SelectContent>
							{members.map((m) => (
								<SelectItem key={m.id} value={m.id}>
									{m.full_name || m.username}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				);
			case "sprint_id":
				return (
					<Select
						value={fields.sprint_id ?? ""}
						onValueChange={(v) => setFieldValue("sprint_id", v ?? undefined)}
						disabled={!canEdit}
					>
						<SelectTrigger className="h-7 w-full text-xs">
							<SelectValue>
								{(sprints ?? []).find((s) => s.id === fields.sprint_id)?.name}
							</SelectValue>
						</SelectTrigger>
						<SelectContent>
							{(sprints ?? []).map((s) => (
								<SelectItem key={s.id} value={s.id}>
									{s.name}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				);
			case "parent_task_id":
				return (
					<TargetTaskPicker
						projectId={projectId}
						value={fields.parent_task_id ?? ""}
						onChange={(taskId) => setFieldValue("parent_task_id", taskId)}
						canEdit={canEdit}
					/>
				);
			case "start_date":
				return (
					<Input
						type="date"
						value={toDateInputValue(fields.start_date)}
						onChange={(e) =>
							setFieldValue("start_date", fromDateInputValue(e.target.value))
						}
						disabled={!canEdit}
						className="h-7 text-xs"
					/>
				);
			case "due_date":
				return (
					<Input
						type="date"
						value={toDateInputValue(fields.due_date)}
						onChange={(e) =>
							setFieldValue("due_date", fromDateInputValue(e.target.value))
						}
						disabled={!canEdit}
						className="h-7 text-xs"
					/>
				);
			case "custom_fields":
				return (
					<div className="space-y-1.5">
						<Select
							value=""
							onValueChange={(v) => {
								if (!v) return;
								setFieldValue("custom_fields", {
									...fields.custom_fields,
									[v]: "",
								});
							}}
							disabled={!canEdit}
						>
							<SelectTrigger className="h-7 w-full text-xs">
								<SelectValue
									placeholder={t("automation.nodeConfig.action.addCustomField")}
								/>
							</SelectTrigger>
							<SelectContent>
								{customFields
									.filter(
										(cf) => !selectedCustomFieldKeys.includes(cf.field_key),
									)
									.map((cf) => (
										<SelectItem key={cf.id} value={cf.field_key}>
											{cf.display_name}
										</SelectItem>
									))}
							</SelectContent>
						</Select>
						{selectedCustomFieldKeys.map((key) => {
							const def = customFields.find((cf) => cf.field_key === key);
							return (
								<div key={key} className="flex items-center gap-1.5">
									<span className="w-1/3 shrink-0 truncate text-[10px] text-muted-foreground">
										{def?.display_name ?? key}
									</span>
									<Input
										value={String(fields.custom_fields?.[key] ?? "")}
										onChange={(e) =>
											setFieldValue("custom_fields", {
												...fields.custom_fields,
												[key]: e.target.value,
											})
										}
										disabled={!canEdit}
										className="h-7 text-xs"
									/>
									{canEdit && (
										<button
											type="button"
											onClick={() => {
												const next = { ...fields.custom_fields };
												delete next[key];
												setFieldValue("custom_fields", next);
											}}
											className="shrink-0 text-muted-foreground hover:text-destructive"
										>
											<Trash2 className="size-3.5" />
										</button>
									)}
								</div>
							);
						})}
					</div>
				);
			default:
				return null;
		}
	}

	return (
		<div className="space-y-3">
			<p className="text-xs text-muted-foreground">
				{t("automation.nodeConfig.action.updateTaskHint")}{" "}
				{t("automation.nodeConfig.action.variablesHint")}
			</p>
			{canEdit && addableFields.length > 0 && (
				<Select
					value=""
					onValueChange={(v) => {
						if (!v) return;
						toggleField(v as UpdatableField, true);
					}}
				>
					<SelectTrigger className="h-7 w-full text-xs">
						<SelectValue
							placeholder={t("automation.nodeConfig.action.addFieldToUpdate")}
						/>
					</SelectTrigger>
					<SelectContent>
						{addableFields.map((key) => (
							<SelectItem key={key} value={key}>
								{fieldLabel(key)}
							</SelectItem>
						))}
					</SelectContent>
				</Select>
			)}
			{FIELD_UPDATE_ORDER.filter((key) => enabled.has(key)).map((key) => (
				<DynamicFieldRow
					key={key}
					label={fieldLabel(key)}
					onRemove={() => toggleField(key, false)}
					canEdit={canEdit}
				>
					{renderFieldEditor(key)}
				</DynamicFieldRow>
			))}
			<div className="space-y-1">
				<Label className="text-[10px] text-muted-foreground">
					{t("automation.nodeConfig.target.label")}
				</Label>
				<TaskTargetSelector
					projectId={projectId}
					target={target}
					onChange={setTarget}
					canEdit={canEdit}
				/>
			</div>
			{canEdit && (
				<SaveButton
					saving={saving}
					disabled={enabled.size === 0}
					onClick={() => {
						const savedFields: TaskFieldUpdate = {};
						for (const key of enabled) {
							// biome-ignore lint/suspicious/noExplicitAny: TaskFieldUpdate values are heterogeneous per key
							(savedFields as any)[key] = fields[key];
						}
						onSave({ update: savedFields, target });
					}}
				/>
			)}
		</div>
	);
}

// DynamicFieldRow pairs a field's label + value editor with a remove
// button — matches TaskFieldUpdate's "unset = untouched" semantics: a
// removed field is never sent, not sent-as-empty. Styled as a small card
// (mirrors the condition-branch cards above) so a multi-field update
// reads as a stack of distinct edits rather than a flat form.
function DynamicFieldRow({
	label,
	onRemove,
	canEdit,
	children,
}: {
	label: string;
	onRemove: () => void;
	canEdit: boolean;
	children: React.ReactNode;
}) {
	return (
		<div className="group space-y-1.5 rounded-lg border border-border/60 bg-muted/20 p-2.5 transition-colors hover:border-border">
			<div className="flex items-center justify-between gap-2">
				<Label className="text-xs font-medium">{label}</Label>
				{canEdit && (
					<button
						type="button"
						onClick={onRemove}
						className="shrink-0 rounded-md p-1 text-muted-foreground opacity-60 transition-opacity hover:bg-destructive/10 hover:text-destructive hover:opacity-100 group-hover:opacity-100"
					>
						<Trash2 className="size-3.5" />
					</button>
				)}
			</div>
			{children}
		</div>
	);
}

function SaveButton({
	onClick,
	saving,
	disabled,
}: {
	onClick: () => void;
	saving?: boolean;
	disabled?: boolean;
}) {
	const { t } = useTranslation("projects");
	return (
		<Button
			size="sm"
			className="w-full"
			disabled={saving || disabled}
			onClick={onClick}
		>
			{t("automation.nodeConfig.save")}
		</Button>
	);
}
