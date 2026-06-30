import { useQueryClient } from "@tanstack/react-query";
import { X } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
	createCustomFieldDefinition,
	customFieldsQueryOptions,
} from "@/lib/project-api";
import { cn } from "@/lib/utils";
import { mapApiFieldToUi, mapUiFieldTypeToApi, slugify } from "./helpers";
import type { CustomFieldDef } from "./types";

type UiFieldType = "Text" | "Number" | "Date" | "Checkbox" | "Select";
const FIELD_TYPES: UiFieldType[] = [
	"Text",
	"Number",
	"Date",
	"Checkbox",
	"Select",
];

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
	const { t } = useTranslation();
	const qc = useQueryClient();
	const fieldTypeLabels: Record<UiFieldType, string> = {
		Text: t("project.interactions.taskDetail.addField.types.text"),
		Number: t("project.interactions.taskDetail.addField.types.number"),
		Date: t("project.interactions.taskDetail.addField.types.date"),
		Checkbox: t("project.interactions.taskDetail.addField.types.checkbox"),
		Select: t("project.interactions.taskDetail.addField.types.select"),
	};
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
					options: fieldType === "Select" ? [] : undefined,
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
						{t("project.interactions.taskDetail.addField.title")}
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
							{t("project.interactions.taskDetail.addField.displayName")}{" "}
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
								"project.interactions.taskDetail.addField.displayNamePlaceholder",
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
							{t("project.interactions.taskDetail.addField.fieldKey")}
						</label>
						<input
							id="add-field-key"
							value={fieldKey}
							onChange={(e) => {
								setKeyManual(true);
								setFieldKey(slugify(e.target.value));
							}}
							placeholder="release_tag"
							className="w-full rounded-lg border border-border/30 bg-muted/15 px-3.5 py-2.5 text-sm font-mono outline-none focus:border-primary/40 focus:ring-2 focus:ring-primary/15 placeholder:text-muted-foreground/45 transition-all duration-150"
						/>
					</div>

					{/* Field type */}
					<div className="space-y-2.5">
						<p className="text-sm font-semibold text-foreground/80 uppercase tracking-wide">
							{t("project.interactions.taskDetail.addField.fieldType")}
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
									{fieldTypeLabels[ft]}
								</button>
							))}
						</div>
					</div>

					{/* Required toggle */}
					<div className="flex items-center justify-between rounded-xl border border-border/20 bg-muted/15 px-4 py-3">
						<span className="text-sm text-foreground/80 font-medium">
							{t("project.interactions.taskDetail.addField.required")}
						</span>
						<button
							type="button"
							role="switch"
							aria-checked={required}
							onClick={() => setRequired(!required)}
							className={cn(
								"relative inline-flex h-5 w-9 items-center rounded-full transition-all duration-200",
								required
									? "bg-primary shadow-sm shadow-primary/20"
									: "bg-muted/60",
							)}
						>
							<span
								className={cn(
									"inline-block size-3.5 rounded-full bg-white shadow-sm transition-transform duration-200",
									required ? "translate-x-4.5" : "translate-x-0.75",
								)}
							/>
						</button>
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
						{t("project.interactions.taskDetail.cancel")}
					</button>
					<button
						type="button"
						disabled={!displayName.trim() || submitting}
						onClick={handleCreate}
						className="rounded-lg bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground disabled:opacity-40 hover:bg-primary/90 transition-all duration-150 shadow-sm disabled:shadow-none"
					>
						{submitting
							? t("project.interactions.taskDetail.addField.creating")
							: t("project.interactions.taskDetail.addField.create")}
					</button>
				</div>
			</div>
		</div>
	);
}
