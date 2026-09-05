import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { TFunction } from "i18next";
import { Check, Edit2, Loader2, Plus, Trash2, X } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { ColorSwatchPicker } from "@/components/ui/color-swatch-picker";
import {
	Dialog,
	DialogClose,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import {
	type CustomFieldDefinition,
	type CustomFieldOption,
	createCustomFieldDefinition,
	customFieldsQueryOptions,
	deleteCustomFieldDefinition,
	type FieldType,
	updateCustomFieldDefinition,
} from "@/lib/project-api";
import { cn } from "@/lib/utils";

// ── Field type utilities ──────────────────────────────────────────────────────

const UI_FIELD_TYPES = [
	"Text",
	"Number",
	"Date",
	"Checkbox",
	"Select",
	"MultiSelect",
	"Url",
] as const;
type UIFieldType = (typeof UI_FIELD_TYPES)[number];

const UI_TO_API_FIELD_TYPE: Record<UIFieldType, FieldType> = {
	Text: "text",
	Number: "number",
	Date: "date",
	Checkbox: "boolean",
	Select: "select",
	MultiSelect: "multi_select",
	Url: "url",
};

const UI_FIELD_TYPE_LABEL_KEYS = {
	Text: "settings.customFields.fieldTypes.text",
	Number: "settings.customFields.fieldTypes.number",
	Date: "settings.customFields.fieldTypes.date",
	Checkbox: "settings.customFields.fieldTypes.checkbox",
	Select: "settings.customFields.fieldTypes.select",
	MultiSelect: "settings.customFields.fieldTypes.multiSelect",
	Url: "settings.customFields.fieldTypes.url",
} as const satisfies Record<UIFieldType, string>;

const API_TO_UI_FIELD_TYPE_LABEL_KEY = {
	text: "settings.customFields.fieldTypes.text",
	number: "settings.customFields.fieldTypes.number",
	date: "settings.customFields.fieldTypes.date",
	boolean: "settings.customFields.fieldTypes.checkbox",
	select: "settings.customFields.fieldTypes.select",
	multi_select: "settings.customFields.fieldTypes.multiSelect",
	url: "settings.customFields.fieldTypes.url",
} as const satisfies Record<string, string>;

function toUIFieldType(apiType: string, t: TFunction<"projects">): string {
	const key =
		API_TO_UI_FIELD_TYPE_LABEL_KEY[
			apiType as keyof typeof API_TO_UI_FIELD_TYPE_LABEL_KEY
		];
	return key ? t(key) : apiType;
}

function slugify(s: string): string {
	return s
		.toLowerCase()
		.replace(/\s+/g, "_")
		.replace(/[^a-z0-9_]/g, "")
		.replace(/_+/g, "_")
		.slice(0, 64);
}

// ── Create dialog ─────────────────────────────────────────────────────────────

function CreateCustomFieldDialog({
	projectId,
	open,
	onOpenChange,
}: {
	projectId: string;
	open: boolean;
	onOpenChange: (v: boolean) => void;
}) {
	const { t } = useTranslation("projects");
	const queryClient = useQueryClient();
	const [displayName, setDisplayName] = useState("");
	const [fieldKey, setFieldKey] = useState("");
	const [keyManuallyEdited, setKeyManuallyEdited] = useState(false);
	const [fieldType, setFieldType] = useState<UIFieldType>("Text");
	const [required, setRequired] = useState(false);
	const [options, setOptions] = useState<CustomFieldOption[]>([]);
	const [newOption, setNewOption] = useState("");
	const [error, setError] = useState<string | null>(null);

	const reset = () => {
		setDisplayName("");
		setFieldKey("");
		setKeyManuallyEdited(false);
		setFieldType("Text");
		setRequired(false);
		setOptions([]);
		setNewOption("");
		setError(null);
	};

	const handleDisplayName = (v: string) => {
		setDisplayName(v);
		if (!keyManuallyEdited) setFieldKey(slugify(v));
	};

	const mutation = useMutation({
		mutationFn: () =>
			createCustomFieldDefinition(projectId, {
				display_name: displayName.trim(),
				field_key: fieldKey || slugify(displayName),
				field_type: UI_TO_API_FIELD_TYPE[fieldType],
				options:
					fieldType === "Select" || fieldType === "MultiSelect" ? options : [],
				is_required: required,
			}),
		onSuccess: () => {
			void queryClient.invalidateQueries({
				queryKey: customFieldsQueryOptions(projectId).queryKey,
			});
			reset();
			onOpenChange(false);
		},
		onError: (err: unknown) => {
			const code = getApiErrorCode(err);
			if (code === ApiErrorCode.CustomFieldKeyTaken) {
				setError(t("settings.customFields.createDialog.errors.keyTaken"));
				return;
			}
			if (code === ApiErrorCode.CustomFieldKeyInvalid) {
				setError(t("settings.customFields.createDialog.errors.keyInvalid"));
				return;
			}
			if (code === ApiErrorCode.CustomFieldNameInvalid) {
				setError(t("settings.customFields.createDialog.errors.nameInvalid"));
				return;
			}
			setError(t("settings.customFields.createDialog.errors.createFailed"));
		},
	});

	return (
		<Dialog
			open={open}
			onOpenChange={(o) => {
				if (!o) reset();
				onOpenChange(o);
			}}
		>
			<DialogContent className="sm:max-w-md">
				<DialogHeader>
					<DialogTitle>
						{t("settings.customFields.createDialog.title")}
					</DialogTitle>
					<DialogDescription>
						{t("settings.customFields.createDialog.description")}
					</DialogDescription>
				</DialogHeader>

				<div className="space-y-4">
					{/* Display name */}
					<div className="space-y-1.5">
						<Label htmlFor="cf-display-name">
							{t("settings.customFields.displayNameLabel")}{" "}
							<span className="text-destructive">*</span>
						</Label>
						<Input
							id="cf-display-name"
							value={displayName}
							onChange={(e) => handleDisplayName(e.target.value)}
							placeholder={t("settings.customFields.displayNamePlaceholder")}
							autoFocus
						/>
					</div>

					{/* Field key */}
					<div className="space-y-1.5">
						<Label htmlFor="cf-field-key">
							{t("settings.customFields.fieldKeyLabel")}
						</Label>
						<Input
							id="cf-field-key"
							value={fieldKey}
							onChange={(e) => {
								setKeyManuallyEdited(true);
								setFieldKey(slugify(e.target.value));
							}}
							placeholder="release_tag"
							className="font-mono text-sm"
						/>
						<p className="text-xs text-muted-foreground/60">
							{t("settings.customFields.fieldKeyHint")}
						</p>
					</div>

					{/* Field type */}
					<div className="space-y-1.5">
						<Label>{t("settings.customFields.fieldTypeLabel")}</Label>
						<div className="flex flex-wrap gap-1.5">
							{UI_FIELD_TYPES.map((ft) => (
								<button
									key={ft}
									type="button"
									onClick={() => setFieldType(ft)}
									className={cn(
										"rounded-lg border px-3 py-1.5 text-xs font-medium transition-colors",
										fieldType === ft
											? "border-primary bg-primary/10 text-primary"
											: "border-border/60 text-muted-foreground hover:border-border hover:bg-muted/50",
									)}
								>
									{t(UI_FIELD_TYPE_LABEL_KEYS[ft])}
								</button>
							))}
						</div>
					</div>

					{/* Options editor — only for Select / MultiSelect types */}
					{(fieldType === "Select" || fieldType === "MultiSelect") && (
						<div className="space-y-1.5">
							<Label>{t("settings.customFields.optionsLabel")}</Label>
							<div className="space-y-1">
								{options.map((opt, i) => (
									<div
										key={opt.value + i.toString()}
										className="flex items-center gap-2"
									>
										<ColorSwatchPicker
											value={opt.color}
											onChange={(color) => {
												const updated = [...options];
												updated[i] = { ...updated[i], color };
												setOptions(updated);
											}}
											triggerLabel={t("settings.customFields.optionColorLabel")}
											customColorTitle={t(
												"taskStatuses.formDialog.customColorTitle",
											)}
											clearLabel={t("settings.customFields.noColor")}
										/>
										<Input
											value={opt.value}
											onChange={(e) => {
												const updated = [...options];
												updated[i] = { ...updated[i], value: e.target.value };
												setOptions(updated);
											}}
											className="text-xs h-8"
										/>
										<button
											type="button"
											onClick={() =>
												setOptions(options.filter((_, j) => j !== i))
											}
											className="shrink-0 text-muted-foreground hover:text-destructive transition-colors"
										>
											<X className="size-3.5" />
										</button>
									</div>
								))}
								<div className="flex gap-2">
									<Input
										value={newOption}
										onChange={(e) => setNewOption(e.target.value)}
										onKeyDown={(e) => {
											if (e.key === "Enter" && newOption.trim()) {
												setOptions([...options, { value: newOption.trim() }]);
												setNewOption("");
											}
										}}
										placeholder={t(
											"settings.customFields.addOptionPlaceholder",
										)}
										className="text-xs h-8"
									/>
									<button
										type="button"
										disabled={!newOption.trim()}
										onClick={() => {
											setOptions([...options, { value: newOption.trim() }]);
											setNewOption("");
										}}
										className="flex items-center gap-1 rounded-md bg-muted px-2.5 text-xs font-medium text-muted-foreground hover:bg-muted/80 disabled:opacity-40 transition-colors"
									>
										<Plus className="size-3" />
										{t("settings.customFields.addOption")}
									</button>
								</div>
							</div>
						</div>
					)}

					{/* Required toggle */}
					<div className="flex items-center justify-between rounded-xl border border-border/50 bg-muted/20 px-4 py-3">
						<div>
							<p className="text-sm font-medium">
								{t("settings.customFields.requiredLabel")}
							</p>
							<p className="text-xs text-muted-foreground/70">
								{t("settings.customFields.requiredHint")}
							</p>
						</div>
						<button
							type="button"
							role="switch"
							aria-checked={required}
							onClick={() => setRequired(!required)}
							className={cn(
								"relative inline-flex h-5 w-9 items-center rounded-full border-2 transition-colors",
								required
									? "border-primary bg-primary"
									: "border-border bg-muted",
							)}
						>
							<span
								className={cn(
									"inline-block size-3.5 rounded-full bg-white shadow transition-transform",
									required ? "translate-x-4" : "translate-x-0.5",
								)}
							/>
						</button>
					</div>

					{error ? (
						<p className="text-xs text-destructive bg-destructive/10 rounded-lg px-3 py-2">
							{error}
						</p>
					) : null}
				</div>

				<DialogFooter>
					<DialogClose
						render={
							<Button
								variant="outline"
								size="sm"
								disabled={mutation.isPending}
							/>
						}
					>
						{t("settings.customFields.cancel")}
					</DialogClose>
					<Button
						size="sm"
						disabled={!displayName.trim() || mutation.isPending}
						onClick={() => mutation.mutate()}
					>
						{mutation.isPending ? (
							<Loader2 className="size-3.5 animate-spin" />
						) : null}
						{t("settings.customFields.createDialog.createField")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

// ── Edit dialog ───────────────────────────────────────────────────────────────

function EditCustomFieldDialog({
	projectId,
	field,
	open,
	onOpenChange,
}: {
	projectId: string;
	field: CustomFieldDefinition | null;
	open: boolean;
	onOpenChange: (v: boolean) => void;
}) {
	const { t } = useTranslation("projects");
	const queryClient = useQueryClient();
	const [displayName, setDisplayName] = useState(field?.display_name ?? "");
	const [options, setOptions] = useState<CustomFieldOption[]>(
		field?.options ?? [],
	);
	const [required, setRequired] = useState(field?.is_required ?? false);
	const [newOption, setNewOption] = useState("");
	const [error, setError] = useState<string | null>(null);

	useEffect(() => {
		setDisplayName(field?.display_name ?? "");
		setOptions(field?.options ?? []);
		setRequired(field?.is_required ?? false);
		setError(null);
		setNewOption("");
	}, [field]);

	const uiFieldType = field ? toUIFieldType(field.field_type, t) : "";

	const mutation = useMutation({
		mutationFn: () => {
			if (!field)
				return Promise.resolve(field as unknown as CustomFieldDefinition);
			return updateCustomFieldDefinition(projectId, field.id, {
				display_name: displayName.trim(),
				options:
					field.field_type === "select" || field.field_type === "multi_select"
						? options
						: undefined,
				is_required: required,
			});
		},
		onSuccess: () => {
			void queryClient.invalidateQueries({
				queryKey: customFieldsQueryOptions(projectId).queryKey,
			});
			onOpenChange(false);
		},
		onError: (err: unknown) => {
			const code = getApiErrorCode(err);
			if (code === ApiErrorCode.CustomFieldNameInvalid) {
				setError(t("settings.customFields.editDialog.errors.nameInvalid"));
				return;
			}
			setError(t("settings.customFields.editDialog.errors.saveFailed"));
		},
	});

	if (!field) return null;

	return (
		<Dialog
			open={open}
			onOpenChange={(o) => {
				if (!o) setError(null);
				onOpenChange(o);
			}}
		>
			<DialogContent className="sm:max-w-md">
				<DialogHeader>
					<DialogTitle>
						{t("settings.customFields.editDialog.title")}
					</DialogTitle>
					<DialogDescription>
						{t("settings.customFields.editDialog.description")}
					</DialogDescription>
				</DialogHeader>

				<div className="space-y-4">
					<div className="space-y-1.5">
						<Label htmlFor="cf-edit-name">
							{t("settings.customFields.displayNameLabel")}
						</Label>
						<Input
							id="cf-edit-name"
							value={displayName}
							onChange={(e) => setDisplayName(e.target.value)}
							autoFocus
						/>
					</div>

					<div className="space-y-1.5">
						<Label>{t("settings.customFields.fieldKeyLabel")}</Label>
						<Input
							value={field.field_key}
							disabled
							className="font-mono text-sm opacity-60"
						/>
						<p className="text-xs text-muted-foreground/60">
							{t("settings.customFields.fieldKeyImmutableHint")}
						</p>
					</div>

					<div className="space-y-1.5">
						<Label>{t("settings.customFields.fieldTypeLabel")}</Label>
						<div className="flex flex-wrap gap-1.5">
							<button
								type="button"
								disabled
								className="rounded-lg border border-primary bg-primary/10 px-3 py-1.5 text-xs font-medium text-primary opacity-60 cursor-not-allowed"
							>
								{uiFieldType}
							</button>
						</div>
						<p className="text-xs text-muted-foreground/60">
							{t("settings.customFields.fieldTypeImmutableHint")}
						</p>
					</div>

					{(field.field_type === "select" ||
						field.field_type === "multi_select") && (
						<div className="space-y-1.5">
							<Label>{t("settings.customFields.optionsLabel")}</Label>
							<div className="space-y-1">
								{options.map((opt, i) => (
									<div
										key={opt.value + i.toString()}
										className="flex items-center gap-2"
									>
										<ColorSwatchPicker
											value={opt.color}
											onChange={(color) => {
												const updated = [...options];
												updated[i] = { ...updated[i], color };
												setOptions(updated);
											}}
											triggerLabel={t("settings.customFields.optionColorLabel")}
											customColorTitle={t(
												"taskStatuses.formDialog.customColorTitle",
											)}
											clearLabel={t("settings.customFields.noColor")}
										/>
										<Input
											value={opt.value}
											onChange={(e) => {
												const updated = [...options];
												updated[i] = { ...updated[i], value: e.target.value };
												setOptions(updated);
											}}
											className="text-xs h-8"
										/>
										<button
											type="button"
											onClick={() =>
												setOptions(options.filter((_, j) => j !== i))
											}
											className="shrink-0 text-muted-foreground hover:text-destructive"
										>
											<X className="size-3.5" />
										</button>
									</div>
								))}
								<div className="flex gap-2">
									<Input
										value={newOption}
										onChange={(e) => setNewOption(e.target.value)}
										onKeyDown={(e) => {
											if (e.key === "Enter" && newOption.trim()) {
												setOptions([...options, { value: newOption.trim() }]);
												setNewOption("");
											}
										}}
										placeholder={t(
											"settings.customFields.addOptionPlaceholder",
										)}
										className="text-xs h-8"
									/>
									<button
										type="button"
										disabled={!newOption.trim()}
										onClick={() => {
											setOptions([...options, { value: newOption.trim() }]);
											setNewOption("");
										}}
										className="flex items-center gap-1 rounded-md bg-muted px-2.5 text-xs font-medium text-muted-foreground hover:bg-muted/80 disabled:opacity-40"
									>
										<Plus className="size-3" />
										{t("settings.customFields.addOption")}
									</button>
								</div>
							</div>
						</div>
					)}

					{/* Required toggle */}
					<div className="flex items-center justify-between rounded-xl border border-border/50 bg-muted/20 px-4 py-3">
						<div>
							<p className="text-sm font-medium">
								{t("settings.customFields.requiredLabel")}
							</p>
							<p className="text-xs text-muted-foreground/70">
								{t("settings.customFields.requiredHint")}
							</p>
						</div>
						<button
							type="button"
							role="switch"
							aria-checked={required}
							onClick={() => setRequired(!required)}
							className={cn(
								"relative inline-flex h-5 w-9 items-center rounded-full border-2 transition-colors",
								required
									? "border-primary bg-primary"
									: "border-border bg-muted",
							)}
						>
							<span
								className={cn(
									"inline-block size-3.5 rounded-full bg-white shadow transition-transform",
									required ? "translate-x-4" : "translate-x-0.5",
								)}
							/>
						</button>
					</div>

					{error ? (
						<p className="text-xs text-destructive bg-destructive/10 rounded-lg px-3 py-2">
							{error}
						</p>
					) : null}
				</div>

				<DialogFooter>
					<DialogClose
						render={
							<Button
								variant="outline"
								size="sm"
								disabled={mutation.isPending}
							/>
						}
					>
						{t("settings.customFields.cancel")}
					</DialogClose>
					<Button
						size="sm"
						disabled={!displayName.trim() || mutation.isPending}
						onClick={() => mutation.mutate()}
					>
						{mutation.isPending ? (
							<Loader2 className="size-3.5 animate-spin" />
						) : null}
						{t("settings.customFields.saveChanges")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

// ── Delete dialog ─────────────────────────────────────────────────────────────

function DeleteCustomFieldDialog({
	projectId,
	field,
	open,
	onOpenChange,
}: {
	projectId: string;
	field: CustomFieldDefinition | null;
	open: boolean;
	onOpenChange: (v: boolean) => void;
}) {
	const { t } = useTranslation("projects");
	const queryClient = useQueryClient();
	const [error, setError] = useState<string | null>(null);

	const mutation = useMutation({
		mutationFn: () => {
			if (!field) return Promise.resolve();
			return deleteCustomFieldDefinition(projectId, field.id);
		},
		onSuccess: () => {
			void queryClient.invalidateQueries({
				queryKey: customFieldsQueryOptions(projectId).queryKey,
			});
			onOpenChange(false);
		},
		onError: (err: unknown) => {
			const code = getApiErrorCode(err);
			if (code === ApiErrorCode.CustomFieldNotFound) {
				void queryClient.invalidateQueries({
					queryKey: customFieldsQueryOptions(projectId).queryKey,
				});
				onOpenChange(false);
				return;
			}
			setError(t("settings.customFields.deleteDialog.deleteFailed"));
		},
	});

	if (!field) return null;

	return (
		<Dialog
			open={open}
			onOpenChange={(o) => {
				if (!o) setError(null);
				onOpenChange(o);
			}}
		>
			<DialogContent className="sm:max-w-sm">
				<DialogHeader>
					<div className="flex size-10 items-center justify-center rounded-full bg-destructive/10 mb-2">
						<Trash2 className="size-5 text-destructive" />
					</div>
					<DialogTitle>
						{t("settings.customFields.deleteDialog.title")}
					</DialogTitle>
					<DialogDescription>
						{t("settings.customFields.deleteDialog.confirmTextPrefix")}{" "}
						<span className="font-semibold text-foreground">
							&ldquo;{field.display_name}&rdquo;
						</span>
						{t("settings.customFields.deleteDialog.confirmTextSuffix")}
					</DialogDescription>
				</DialogHeader>

				{error ? (
					<p className="text-xs text-destructive bg-destructive/10 rounded-lg px-3 py-2">
						{error}
					</p>
				) : null}

				<DialogFooter>
					<DialogClose
						render={
							<Button
								variant="outline"
								size="sm"
								disabled={mutation.isPending}
							/>
						}
					>
						{t("settings.customFields.cancel")}
					</DialogClose>
					<Button
						variant="destructive"
						size="sm"
						disabled={mutation.isPending}
						onClick={() => mutation.mutate()}
					>
						{mutation.isPending ? (
							<Loader2 className="size-3.5 animate-spin" />
						) : null}
						{t("settings.customFields.deleteDialog.deleteField")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

// ── Main section component ────────────────────────────────────────────────────

export function CustomFieldsSettings({
	projectId,
	canWrite,
}: {
	projectId: string;
	canWrite: boolean;
}) {
	const { t } = useTranslation("projects");
	const { data: fields = [], isLoading } = useQuery(
		customFieldsQueryOptions(projectId),
	);
	const [createOpen, setCreateOpen] = useState(false);
	const [editField, setEditField] = useState<CustomFieldDefinition | null>(
		null,
	);
	const [deleteField, setDeleteField] = useState<CustomFieldDefinition | null>(
		null,
	);

	return (
		<div className="rounded-xl border border-border/60 bg-card p-6">
			<div className="flex items-start justify-between mb-1">
				<div>
					<h3 className="font-[Syne] text-base font-semibold">
						{t("settings.customFields.title")}
					</h3>
					<p className="text-xs text-muted-foreground mt-0.5 max-w-xs">
						{t("settings.customFields.description")}
					</p>
				</div>
				{canWrite && (
					<Button
						size="sm"
						variant="outline"
						className="gap-1.5 border-border/60 shrink-0"
						onClick={() => setCreateOpen(true)}
					>
						<Plus className="size-3.5" />
						{t("settings.customFields.newField")}
					</Button>
				)}
			</div>

			{isLoading ? (
				<div className="rounded-xl border overflow-hidden mt-4">
					{["cf1", "cf2", "cf3"].map((k) => (
						<div
							key={k}
							className="flex items-center gap-4 border-b px-5 py-4 last:border-0"
						>
							<Skeleton className="h-4 w-36" />
							<Skeleton className="h-4 w-24 font-mono" />
							<Skeleton className="h-5 w-16 rounded-md" />
							<Skeleton className="h-4 w-8 ml-auto" />
						</div>
					))}
				</div>
			) : fields.length === 0 ? (
				<div className="mt-4 flex flex-col items-center gap-4 rounded-xl border border-dashed border-border/60 bg-muted/10 py-14 text-center">
					<div className="flex size-11 items-center justify-center rounded-xl bg-muted">
						<Plus className="size-5 text-muted-foreground/60" />
					</div>
					<div>
						<p className="text-sm font-medium">
							{t("settings.customFields.empty.title")}
						</p>
						<p className="mt-1 text-xs text-muted-foreground max-w-xs mx-auto">
							{t("settings.customFields.empty.description")}
						</p>
					</div>
					{canWrite && (
						<Button
							size="sm"
							variant="outline"
							onClick={() => setCreateOpen(true)}
						>
							<Plus className="size-4 mr-1" />
							{t("settings.customFields.empty.createFirstField")}
						</Button>
					)}
				</div>
			) : (
				<div className="mt-4 overflow-x-auto rounded-xl border">
					<Table>
						<TableHeader>
							<TableRow className="bg-muted/40 hover:bg-muted/40">
								<TableHead className="px-5 text-xs font-semibold uppercase tracking-wide">
									{t("settings.customFields.table.displayName")}
								</TableHead>
								<TableHead className="px-5 text-xs font-semibold uppercase tracking-wide">
									{t("settings.customFields.table.fieldKey")}
								</TableHead>
								<TableHead className="px-5 text-xs font-semibold uppercase tracking-wide">
									{t("settings.customFields.table.type")}
								</TableHead>
								<TableHead className="px-5 text-xs font-semibold uppercase tracking-wide">
									{t("settings.customFields.table.required")}
								</TableHead>
								{canWrite && <TableHead className="w-20 px-5" />}
							</TableRow>
						</TableHeader>
						<TableBody>
							{fields.map((field) => (
								<TableRow key={field.id} className="group">
									<TableCell className="px-5 font-medium">
										{field.display_name}
									</TableCell>
									<TableCell className="px-5 font-mono text-xs text-muted-foreground">
										{field.field_key}
									</TableCell>
									<TableCell className="px-5">
										<span className="inline-flex items-center rounded-md border border-border/40 bg-muted/40 px-2 py-0.5 text-xs font-semibold text-muted-foreground">
											{toUIFieldType(field.field_type, t)}
										</span>
									</TableCell>
									<TableCell className="px-5">
										{field.is_required ? (
											<span className="inline-flex items-center gap-1 text-emerald-600 text-xs font-medium">
												<Check className="size-3" />
												{t("settings.customFields.yes")}
											</span>
										) : (
											<span className="text-xs text-muted-foreground/50">
												{t("settings.customFields.no")}
											</span>
										)}
									</TableCell>
									{canWrite && (
										<TableCell className="px-5">
											<div className="flex items-center justify-end gap-0.5 opacity-100 transition-opacity sm:opacity-0 sm:group-hover:opacity-100">
												<Button
													variant="ghost"
													size="icon-sm"
													onClick={() => setEditField(field)}
													title={t("settings.customFields.editField")}
													aria-label={t("settings.customFields.editField")}
												>
													<Edit2 className="size-3.5" />
												</Button>
												<Button
													variant="ghost"
													size="icon-sm"
													className="text-destructive hover:text-destructive hover:bg-destructive/10"
													onClick={() => setDeleteField(field)}
													title={t("settings.customFields.deleteField")}
													aria-label={t("settings.customFields.deleteField")}
												>
													<Trash2 className="size-3.5" />
												</Button>
											</div>
										</TableCell>
									)}
								</TableRow>
							))}
						</TableBody>
					</Table>
				</div>
			)}

			<CreateCustomFieldDialog
				projectId={projectId}
				open={createOpen}
				onOpenChange={setCreateOpen}
			/>

			<EditCustomFieldDialog
				projectId={projectId}
				field={editField}
				open={!!editField}
				onOpenChange={(v) => {
					if (!v) setEditField(null);
				}}
			/>

			<DeleteCustomFieldDialog
				projectId={projectId}
				field={deleteField}
				open={!!deleteField}
				onOpenChange={(v) => {
					if (!v) setDeleteField(null);
				}}
			/>
		</div>
	);
}
