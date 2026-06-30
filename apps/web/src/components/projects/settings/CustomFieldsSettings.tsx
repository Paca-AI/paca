import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Edit2, Loader2, Plus, Trash2, X } from "lucide-react";
import { useEffect, useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
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
] as const;
type UIFieldType = (typeof UI_FIELD_TYPES)[number];

const UI_TO_API_FIELD_TYPE: Record<UIFieldType, FieldType> = {
	Text: "text",
	Number: "number",
	Date: "date",
	Checkbox: "boolean",
	Select: "select",
};

function toUIFieldType(apiType: string): string {
	const map: Record<string, string> = {
		text: "Text",
		number: "Number",
		date: "Date",
		boolean: "Checkbox",
		select: "Select",
		multi_select: "Multi-Select",
		url: "URL",
	};
	return map[apiType] ?? apiType;
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
	const { t } = useTranslation();
	const queryClient = useQueryClient();
	const [displayName, setDisplayName] = useState("");
	const [fieldKey, setFieldKey] = useState("");
	const [keyManuallyEdited, setKeyManuallyEdited] = useState(false);
	const [fieldType, setFieldType] = useState<UIFieldType>("Text");
	const [required, setRequired] = useState(false);
	const [options, setOptions] = useState<string[]>([]);
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
				options: fieldType === "Select" ? options : [],
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
				setError(t("project.settings.customFields.create.errKeyTaken"));
				return;
			}
			if (code === ApiErrorCode.CustomFieldKeyInvalid) {
				setError(t("project.settings.customFields.create.errKeyInvalid"));
				return;
			}
			if (code === ApiErrorCode.CustomFieldNameInvalid) {
				setError(t("project.settings.customFields.create.errNameInvalid"));
				return;
			}
			setError(t("project.settings.customFields.create.errCreateFailed"));
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
						{t("project.settings.customFields.create.title")}
					</DialogTitle>
					<DialogDescription>
						{t("project.settings.customFields.create.desc")}
					</DialogDescription>
				</DialogHeader>

				<div className="space-y-4">
					{/* Display name */}
					<div className="space-y-1.5">
						<Label htmlFor="cf-display-name">
							{t("project.settings.customFields.create.displayName")}{" "}
							<span className="text-destructive">*</span>
						</Label>
						<Input
							id="cf-display-name"
							value={displayName}
							onChange={(e) => handleDisplayName(e.target.value)}
							placeholder={t(
								"project.settings.customFields.create.displayNamePlaceholder",
							)}
							autoFocus
						/>
					</div>

					{/* Field key */}
					<div className="space-y-1.5">
						<Label htmlFor="cf-field-key">
							{t("project.settings.customFields.create.fieldKey")}
						</Label>
						<Input
							id="cf-field-key"
							value={fieldKey}
							onChange={(e) => {
								setKeyManuallyEdited(true);
								setFieldKey(slugify(e.target.value));
							}}
							placeholder={t(
								"project.settings.customFields.create.fieldKeyPlaceholder",
							)}
							className="font-mono text-sm"
						/>
						<p className="text-xs text-muted-foreground/60">
							{t("project.settings.customFields.create.fieldKeyHint")}
						</p>
					</div>

					{/* Field type */}
					<div className="space-y-1.5">
						<Label>{t("project.settings.customFields.create.fieldType")}</Label>
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
									{t(`project.settings.customFields.fieldTypes.${ft}`)}
								</button>
							))}
						</div>
					</div>

					{/* Options editor — only for Select type */}
					{fieldType === "Select" && (
						<div className="space-y-1.5">
							<Label>{t("project.settings.customFields.create.options")}</Label>
							<div className="space-y-1">
								{options.map((opt, i) => (
									<div
										key={opt + i.toString()}
										className="flex items-center gap-2"
									>
										<Input
											value={opt}
											onChange={(e) => {
												const updated = [...options];
												updated[i] = e.target.value;
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
												setOptions([...options, newOption.trim()]);
												setNewOption("");
											}
										}}
										placeholder={t(
											"project.settings.customFields.create.addOptionPlaceholder",
										)}
										className="text-xs h-8"
									/>
									<button
										type="button"
										disabled={!newOption.trim()}
										onClick={() => {
											setOptions([...options, newOption.trim()]);
											setNewOption("");
										}}
										className="flex items-center gap-1 rounded-md bg-muted px-2.5 text-xs font-medium text-muted-foreground hover:bg-muted/80 disabled:opacity-40 transition-colors"
									>
										<Plus className="size-3" />
										{t("project.settings.customFields.create.add")}
									</button>
								</div>
							</div>
						</div>
					)}

					{/* Required toggle */}
					<div className="flex items-center justify-between rounded-xl border border-border/50 bg-muted/20 px-4 py-3">
						<div>
							<p className="text-sm font-medium">
								{t("project.settings.customFields.create.required")}
							</p>
							<p className="text-xs text-muted-foreground/70">
								{t("project.settings.customFields.create.requiredHint")}
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
						{t("project.settings.customFields.cancel")}
					</DialogClose>
					<Button
						size="sm"
						disabled={!displayName.trim() || mutation.isPending}
						onClick={() => mutation.mutate()}
					>
						{mutation.isPending ? (
							<Loader2 className="size-3.5 animate-spin" />
						) : null}
						{t("project.settings.customFields.create.createField")}
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
	const { t } = useTranslation();
	const queryClient = useQueryClient();
	const [displayName, setDisplayName] = useState(field?.display_name ?? "");
	const [options, setOptions] = useState<string[]>(field?.options ?? []);
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

	const uiFieldType = field ? toUIFieldType(field.field_type) : "";

	const mutation = useMutation({
		mutationFn: () => {
			if (!field)
				return Promise.resolve(field as unknown as CustomFieldDefinition);
			return updateCustomFieldDefinition(projectId, field.id, {
				display_name: displayName.trim(),
				options: field.field_type === "select" ? options : undefined,
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
				setError(t("project.settings.customFields.edit.errNameInvalid"));
				return;
			}
			setError(t("project.settings.customFields.edit.errSaveFailed"));
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
						{t("project.settings.customFields.edit.title")}
					</DialogTitle>
					<DialogDescription>
						{t("project.settings.customFields.edit.desc")}
					</DialogDescription>
				</DialogHeader>

				<div className="space-y-4">
					<div className="space-y-1.5">
						<Label htmlFor="cf-edit-name">
							{t("project.settings.customFields.edit.displayName")}
						</Label>
						<Input
							id="cf-edit-name"
							value={displayName}
							onChange={(e) => setDisplayName(e.target.value)}
							autoFocus
						/>
					</div>

					<div className="space-y-1.5">
						<Label>{t("project.settings.customFields.edit.fieldKey")}</Label>
						<Input
							value={field.field_key}
							disabled
							className="font-mono text-sm opacity-60"
						/>
						<p className="text-xs text-muted-foreground/60">
							{t("project.settings.customFields.edit.fieldKeyHint")}
						</p>
					</div>

					<div className="space-y-1.5">
						<Label>{t("project.settings.customFields.edit.fieldType")}</Label>
						<div className="flex flex-wrap gap-1.5">
							<button
								type="button"
								disabled
								className="rounded-lg border border-primary bg-primary/10 px-3 py-1.5 text-xs font-medium text-primary opacity-60 cursor-not-allowed"
							>
								{t(`project.settings.customFields.fieldTypes.${uiFieldType}`, {
									defaultValue: uiFieldType,
								})}
							</button>
						</div>
						<p className="text-xs text-muted-foreground/60">
							{t("project.settings.customFields.edit.fieldTypeHint")}
						</p>
					</div>

					{field.field_type === "select" && (
						<div className="space-y-1.5">
							<Label>{t("project.settings.customFields.create.options")}</Label>
							<div className="space-y-1">
								{options.map((opt, i) => (
									<div
										key={opt + i.toString()}
										className="flex items-center gap-2"
									>
										<Input
											value={opt}
											onChange={(e) => {
												const updated = [...options];
												updated[i] = e.target.value;
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
												setOptions([...options, newOption.trim()]);
												setNewOption("");
											}
										}}
										placeholder={t(
											"project.settings.customFields.create.addOptionPlaceholder",
										)}
										className="text-xs h-8"
									/>
									<button
										type="button"
										disabled={!newOption.trim()}
										onClick={() => {
											setOptions([...options, newOption.trim()]);
											setNewOption("");
										}}
										className="flex items-center gap-1 rounded-md bg-muted px-2.5 text-xs font-medium text-muted-foreground hover:bg-muted/80 disabled:opacity-40"
									>
										<Plus className="size-3" />
										{t("project.settings.customFields.create.add")}
									</button>
								</div>
							</div>
						</div>
					)}

					{/* Required toggle */}
					<div className="flex items-center justify-between rounded-xl border border-border/50 bg-muted/20 px-4 py-3">
						<div>
							<p className="text-sm font-medium">
								{t("project.settings.customFields.create.required")}
							</p>
							<p className="text-xs text-muted-foreground/70">
								{t("project.settings.customFields.create.requiredHint")}
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
						{t("project.settings.customFields.cancel")}
					</DialogClose>
					<Button
						size="sm"
						disabled={!displayName.trim() || mutation.isPending}
						onClick={() => mutation.mutate()}
					>
						{mutation.isPending ? (
							<Loader2 className="size-3.5 animate-spin" />
						) : null}
						{t("project.settings.customFields.saveChanges")}
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
	const { t } = useTranslation();
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
			setError(t("project.settings.customFields.delete.errDeleteFailed"));
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
						{t("project.settings.customFields.delete.title")}
					</DialogTitle>
					<DialogDescription>
						<Trans
							i18nKey="project.settings.customFields.delete.confirm"
							values={{ name: field.display_name }}
							components={{
								name: <span className="font-semibold text-foreground" />,
							}}
						/>
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
						{t("project.settings.customFields.cancel")}
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
						{t("project.settings.customFields.deleteField")}
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
	const { t } = useTranslation();
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
						{t("project.settings.customFields.title")}
					</h3>
					<p className="text-xs text-muted-foreground mt-0.5 max-w-xs">
						{t("project.settings.customFields.subtitle")}
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
						{t("project.settings.customFields.newField")}
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
							{t("project.settings.customFields.emptyTitle")}
						</p>
						<p className="mt-1 text-xs text-muted-foreground max-w-xs mx-auto">
							{t("project.settings.customFields.emptyDesc")}
						</p>
					</div>
					{canWrite && (
						<Button
							size="sm"
							variant="outline"
							onClick={() => setCreateOpen(true)}
						>
							<Plus className="size-4 mr-1" />
							{t("project.settings.customFields.createFirst")}
						</Button>
					)}
				</div>
			) : (
				<div className="mt-4 overflow-x-auto rounded-xl border">
					<Table>
						<TableHeader>
							<TableRow className="bg-muted/40 hover:bg-muted/40">
								<TableHead className="px-5 text-xs font-semibold uppercase tracking-wide">
									{t("project.settings.customFields.colDisplayName")}
								</TableHead>
								<TableHead className="px-5 text-xs font-semibold uppercase tracking-wide">
									{t("project.settings.customFields.colFieldKey")}
								</TableHead>
								<TableHead className="px-5 text-xs font-semibold uppercase tracking-wide">
									{t("project.settings.customFields.colType")}
								</TableHead>
								<TableHead className="px-5 text-xs font-semibold uppercase tracking-wide">
									{t("project.settings.customFields.colRequired")}
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
											{t(
												`project.settings.customFields.fieldTypes.${toUIFieldType(field.field_type)}`,
												{ defaultValue: toUIFieldType(field.field_type) },
											)}
										</span>
									</TableCell>
									<TableCell className="px-5">
										{field.is_required ? (
											<span className="inline-flex items-center gap-1 text-emerald-600 text-xs font-medium">
												<Check className="size-3" />
												{t("project.settings.customFields.yes")}
											</span>
										) : (
											<span className="text-xs text-muted-foreground/50">
												{t("project.settings.customFields.no")}
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
													title={t("project.settings.customFields.editField")}
													aria-label={t(
														"project.settings.customFields.editField",
													)}
												>
													<Edit2 className="size-3.5" />
												</Button>
												<Button
													variant="ghost"
													size="icon-sm"
													className="text-destructive hover:text-destructive hover:bg-destructive/10"
													onClick={() => setDeleteField(field)}
													title={t("project.settings.customFields.deleteField")}
													aria-label={t(
														"project.settings.customFields.deleteField",
													)}
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
