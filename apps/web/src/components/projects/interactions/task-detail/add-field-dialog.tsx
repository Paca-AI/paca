import { useQueryClient } from "@tanstack/react-query";
import { X } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Switch } from "@/components/ui/switch";
import {
	createCustomFieldDefinition,
	customFieldsQueryOptions,
} from "@/lib/project-api";
import { cn } from "@/lib/utils";
import { mapApiFieldToUi, mapUiFieldTypeToApi, slugify } from "./helpers";
import type { CustomFieldDef } from "./types";

type UiFieldType =
	| "Text"
	| "Number"
	| "Date"
	| "Checkbox"
	| "Select"
	| "MultiSelect"
	| "Url"
	| "LongText";
const FIELD_TYPES: UiFieldType[] = [
	"Text",
	"LongText",
	"Number",
	"Date",
	"Checkbox",
	"Select",
	"MultiSelect",
	"Url",
];

const FIELD_TYPE_LABEL_KEYS = {
	Text: "taskDetail.addFieldDialog.fieldTypes.text",
	Number: "taskDetail.addFieldDialog.fieldTypes.number",
	Date: "taskDetail.addFieldDialog.fieldTypes.date",
	Checkbox: "taskDetail.addFieldDialog.fieldTypes.checkbox",
	Select: "taskDetail.addFieldDialog.fieldTypes.select",
	MultiSelect: "taskDetail.addFieldDialog.fieldTypes.multiSelect",
	Url: "taskDetail.addFieldDialog.fieldTypes.url",
	LongText: "taskDetail.addFieldDialog.fieldTypes.longText",
} as const satisfies Record<UiFieldType, string>;

interface AddFieldDialogProps {
	open: boolean;
	onOpenChange: (v: boolean) => void;
	projectId?: string;
	onAdd: (field: CustomFieldDef) => void;
}

export function AddFieldDialog({
	open,
	onOpenChange,
	projectId,
	onAdd,
}: AddFieldDialogProps) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const [displayName, setDisplayName] = useState("");
	const [fieldKey, setFieldKey] = useState("");
	const [keyManual, setKeyManual] = useState(false);
	const [fieldType, setFieldType] = useState<UiFieldType>("Text");
	const [required, setRequired] = useState(false);
	const [submitting, setSubmitting] = useState(false);

	const reset = () => {
		setDisplayName("");
		setFieldKey("");
		setKeyManual(false);
		setFieldType("Text");
		setRequired(false);
		setSubmitting(false);
	};

	if (!open) return null;

	const handleCreate = async () => {
		if (!displayName.trim()) return;
		const key = fieldKey || slugify(displayName);
		const apiFieldType = mapUiFieldTypeToApi(fieldType);

		if (projectId) {
			setSubmitting(true);
			try {
				const created = await createCustomFieldDefinition(projectId, {
					display_name: displayName.trim(),
					field_key: key,
					field_type: apiFieldType,
					is_required: required,
					options:
						fieldType === "Select" || fieldType === "MultiSelect"
							? []
							: undefined,
				});
				const mapped = mapApiFieldToUi(created);
				onAdd(mapped);
				await qc.invalidateQueries({
					queryKey: customFieldsQueryOptions(projectId).queryKey,
				});
				reset();
				onOpenChange(false);
			} catch (err) {
				console.error("Failed to create custom field:", err);
			} finally {
				setSubmitting(false);
			}
		} else {
			onAdd({
				id: crypto.randomUUID(),
				display_name: displayName.trim(),
				field_key: key,
				field_type: fieldType,
				required,
				options: [],
			});
			reset();
			onOpenChange(false);
		}
	};

	return (
		// biome-ignore lint/a11y/noStaticElementInteractions: modal backdrop closes on click; keyboard handled by inner close button
		// biome-ignore lint/a11y/useKeyWithClickEvents: modal backdrop; Escape key handled by inner elements
		<div
			className="fixed inset-0 z-60 flex items-center justify-center"
			onClick={() => {
				reset();
				onOpenChange(false);
			}}
		>
			<div className="fixed inset-0 bg-black/25 backdrop-blur-[3px]" />
			{/* biome-ignore lint/a11y/noStaticElementInteractions: stopPropagation on modal content prevents backdrop close */}
			{/* biome-ignore lint/a11y/useKeyWithClickEvents: stopPropagation only; no action triggered */}
			<div
				className="relative z-10 w-full max-w-sm rounded-xl border border-border/40 bg-background p-6 shadow-[0_25px_60px_-12px_rgba(0,0,0,0.2),0_0_0_1px_rgba(255,255,255,0.04)_inset]"
				onClick={(e) => e.stopPropagation()}
			>
				{/* Header */}
				<div className="flex items-center justify-between mb-6">
					<h2 className="font-[Syne] text-base font-bold tracking-tight text-foreground">
						{t("taskDetail.addFieldDialog.title")}
					</h2>
					<button
						type="button"
						onClick={() => {
							reset();
							onOpenChange(false);
						}}
						className="size-7 flex items-center justify-center rounded-lg text-muted-foreground/60 hover:text-foreground hover:bg-muted/50 transition-all duration-150"
					>
						<X className="size-3.5" />
					</button>
				</div>

				<div className="space-y-5">
					{/* Display name */}
					<div className="space-y-2">
						<label
							htmlFor="add-field-display-name"
							className="text-sm font-semibold text-foreground/80 uppercase tracking-wide"
						>
							{t("taskDetail.addFieldDialog.displayNameLabel")}{" "}
							<span className="text-destructive/70">*</span>
						</label>
						<input
							id="add-field-display-name"
							value={displayName}
							onChange={(e) => {
								setDisplayName(e.target.value);
								if (!keyManual) setFieldKey(slugify(e.target.value));
							}}
							placeholder={t(
								"taskDetail.addFieldDialog.displayNamePlaceholder",
							)}
							className="w-full rounded-lg border border-border/30 bg-muted/15 px-3.5 py-2.5 text-sm outline-none focus:border-primary/40 focus:ring-2 focus:ring-primary/15 placeholder:text-muted-foreground/45 transition-all duration-150"
						/>
					</div>

					{/* Field key */}
					<div className="space-y-2">
						<label
							htmlFor="add-field-key"
							className="text-sm font-semibold text-foreground/80 uppercase tracking-wide"
						>
							{t("taskDetail.addFieldDialog.fieldKeyLabel")}
						</label>
						<input
							id="add-field-key"
							value={fieldKey}
							onChange={(e) => {
								setKeyManual(true);
								setFieldKey(slugify(e.target.value));
							}}
							placeholder={t("taskDetail.addFieldDialog.fieldKeyPlaceholder")}
							className="w-full rounded-lg border border-border/30 bg-muted/15 px-3.5 py-2.5 text-sm font-mono outline-none focus:border-primary/40 focus:ring-2 focus:ring-primary/15 placeholder:text-muted-foreground/45 transition-all duration-150"
						/>
					</div>

					{/* Field type */}
					<div className="space-y-2.5">
						<p className="text-sm font-semibold text-foreground/80 uppercase tracking-wide">
							{t("taskDetail.addFieldDialog.fieldTypeLabel")}
						</p>
						<div className="flex flex-wrap gap-1.5">
							{FIELD_TYPES.map((ft) => (
								<button
									key={ft}
									type="button"
									onClick={() => setFieldType(ft)}
									className={cn(
										"rounded-lg border px-3 py-1.5 text-xs font-semibold transition-all duration-150",
										fieldType === ft
											? "border-primary/40 bg-primary/10 text-primary shadow-sm shadow-primary/10"
											: "border-border/25 text-muted-foreground/70 hover:border-border/50 hover:bg-muted/30 hover:text-muted-foreground",
									)}
								>
									{t(FIELD_TYPE_LABEL_KEYS[ft])}
								</button>
							))}
						</div>
					</div>

					{/* Required toggle */}
					<div className="flex items-center justify-between rounded-xl border border-border/20 bg-muted/15 px-4 py-3">
						<span className="text-sm text-foreground/80 font-medium">
							{t("taskDetail.addFieldDialog.requiredLabel")}
						</span>
						<Switch checked={required} onCheckedChange={setRequired} />
					</div>
				</div>

				{/* Footer */}
				<div className="mt-6 flex justify-end gap-2">
					<button
						type="button"
						onClick={() => {
							reset();
							onOpenChange(false);
						}}
						className="rounded-lg border border-border/30 px-4 py-2 text-sm font-medium text-muted-foreground/80 hover:bg-muted/30 hover:text-foreground transition-all duration-150"
					>
						{t("taskDetail.addFieldDialog.cancel")}
					</button>
					<button
						type="button"
						disabled={!displayName.trim() || submitting}
						onClick={handleCreate}
						className="rounded-lg bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground disabled:opacity-40 hover:bg-primary/90 transition-all duration-150 shadow-sm disabled:shadow-none"
					>
						{submitting
							? t("taskDetail.addFieldDialog.creating")
							: t("taskDetail.addFieldDialog.createField")}
					</button>
				</div>
			</div>
		</div>
	);
}
