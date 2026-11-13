import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
	createCustomFieldDefinition,
	customFieldsQueryOptions,
} from "@/lib/project-api";
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
		<Dialog
			open={open}
			onOpenChange={(next) => {
				if (!next) reset();
				onOpenChange(next);
			}}
		>
			{/* z-60: above the task-detail overlay (z-50), below the floating chat. */}
			<DialogContent className="z-60 sm:max-w-sm" overlayClassName="z-60">
				<DialogHeader>
					<DialogTitle className="font-[Syne] text-base font-bold tracking-tight">
						{t("taskDetail.addFieldDialog.title")}
					</DialogTitle>
				</DialogHeader>

				<div className="space-y-5">
					{/* Display name */}
					<div className="space-y-2">
						<Label
							htmlFor="add-field-display-name"
							className="text-sm font-semibold uppercase tracking-wide text-foreground/80"
						>
							{t("taskDetail.addFieldDialog.displayNameLabel")}{" "}
							<span className="text-destructive/70">*</span>
						</Label>
						<Input
							id="add-field-display-name"
							value={displayName}
							onChange={(e) => {
								setDisplayName(e.target.value);
								if (!keyManual) setFieldKey(slugify(e.target.value));
							}}
							placeholder={t(
								"taskDetail.addFieldDialog.displayNamePlaceholder",
							)}
						/>
					</div>

					{/* Field key */}
					<div className="space-y-2">
						<Label
							htmlFor="add-field-key"
							className="text-sm font-semibold uppercase tracking-wide text-foreground/80"
						>
							{t("taskDetail.addFieldDialog.fieldKeyLabel")}
						</Label>
						<Input
							id="add-field-key"
							value={fieldKey}
							onChange={(e) => {
								setKeyManual(true);
								setFieldKey(slugify(e.target.value));
							}}
							placeholder={t("taskDetail.addFieldDialog.fieldKeyPlaceholder")}
							className="font-mono"
						/>
					</div>

					{/* Field type */}
					<div className="space-y-2.5">
						<p className="text-sm font-semibold uppercase tracking-wide text-foreground/80">
							{t("taskDetail.addFieldDialog.fieldTypeLabel")}
						</p>
						<ToggleGroup
							aria-label={t("taskDetail.addFieldDialog.fieldTypeLabel")}
							variant="outline"
							size="sm"
							spacing={1}
							className="flex-wrap"
							value={[fieldType]}
							onValueChange={(next) => {
								// Re-pressing the active type would empty the group; keep one.
								const value = next[0];
								if (value) setFieldType(value as UiFieldType);
							}}
						>
							{FIELD_TYPES.map((ft) => (
								<ToggleGroupItem key={ft} value={ft} className="px-3 text-xs">
									{t(FIELD_TYPE_LABEL_KEYS[ft])}
								</ToggleGroupItem>
							))}
						</ToggleGroup>
					</div>

					{/* Required toggle */}
					<div className="flex items-center justify-between rounded-xl border border-border/20 bg-muted/15 px-4 py-3">
						<Label htmlFor="add-field-required" className="font-medium">
							{t("taskDetail.addFieldDialog.requiredLabel")}
						</Label>
						<Switch
							id="add-field-required"
							checked={required}
							onCheckedChange={setRequired}
						/>
					</div>
				</div>

				<DialogFooter>
					<Button
						type="button"
						variant="outline"
						onClick={() => {
							reset();
							onOpenChange(false);
						}}
					>
						{t("taskDetail.addFieldDialog.cancel")}
					</Button>
					<Button
						type="button"
						disabled={!displayName.trim() || submitting}
						onClick={handleCreate}
					>
						{submitting
							? t("taskDetail.addFieldDialog.creating")
							: t("taskDetail.addFieldDialog.createField")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
