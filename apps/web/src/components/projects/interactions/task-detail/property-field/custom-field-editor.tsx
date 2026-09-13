import { CalendarDays } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { CustomFieldOption } from "@/lib/project-api";
import { FieldValue } from "../primitives";
import { CheckboxEditor } from "./checkbox-editor";
import { SingleDateEditor } from "./date-editor";
import { displayDate } from "./helpers";
import { MultiSelectEditor } from "./multi-select-editor";
import { NumberEditor } from "./number-editor";
import { SelectEditor } from "./select-editor";
import { TextEditor } from "./text-editor";
import type { SelectOption } from "./types";
import { UrlEditor } from "./url-editor";

export function CustomFieldEditor({
	customType,
	rawValue,
	canEdit,
	options = [],
	onChange,
}: {
	customType:
		| "Text"
		| "Number"
		| "Date"
		| "Checkbox"
		| "Select"
		| "MultiSelect"
		| "Url";
	rawValue: unknown;
	canEdit: boolean;
	options?: CustomFieldOption[];
	onChange?: (value: unknown) => void;
}) {
	const { t } = useTranslation("projects");
	switch (customType) {
		case "Text":
			return (
				<TextEditor
					value={rawValue != null ? String(rawValue) : null}
					canEdit={canEdit}
					onChange={(v) => onChange?.(v)}
				/>
			);
		case "Number": {
			const num =
				typeof rawValue === "number" ? rawValue : Number(rawValue) || 0;
			if (!canEdit) {
				return (
					<span className="text-sm tabular-nums font-medium text-foreground">
						{num}
					</span>
				);
			}
			return <NumberEditor value={num} onChange={(v) => onChange?.(v)} />;
		}
		case "Date":
			if (!canEdit) {
				return (
					<span className="inline-flex items-center gap-1.5 rounded-lg border border-border/25 bg-muted/25 px-2.5 py-1.5 text-xs text-muted-foreground font-medium">
						<CalendarDays className="size-3 shrink-0 opacity-70" />
						{displayDate(rawValue as string | null) ??
							t("taskDetail.common.empty")}
					</span>
				);
			}
			return (
				<SingleDateEditor
					value={rawValue as string | null}
					onChange={(v) => onChange?.(v)}
				/>
			);
		case "Checkbox":
			return (
				<CheckboxEditor
					checked={Boolean(rawValue)}
					canEdit={canEdit}
					onChange={(v) => onChange?.(v)}
				/>
			);
		case "Select": {
			const selectOptions: SelectOption[] = options.map((o) => ({
				value: o.value,
				label: o.value,
				colorDot: o.color ?? undefined,
			}));
			const currentVal = rawValue != null ? String(rawValue) : null;
			if (!canEdit) {
				return <FieldValue empty={!currentVal}>{currentVal}</FieldValue>;
			}
			return (
				<SelectEditor
					value={currentVal}
					options={selectOptions}
					onChange={(v) => onChange?.(v)}
				/>
			);
		}
		case "MultiSelect": {
			const selectOptions: SelectOption[] = options.map((o) => ({
				value: o.value,
				label: o.value,
				colorDot: o.color ?? undefined,
			}));
			const currentVal = Array.isArray(rawValue)
				? rawValue.filter((v): v is string => typeof v === "string")
				: typeof rawValue === "string" && rawValue
					? [rawValue]
					: [];
			return (
				<MultiSelectEditor
					value={currentVal}
					options={selectOptions}
					canEdit={canEdit}
					onChange={(v) => onChange?.(v)}
				/>
			);
		}
		case "Url":
			return (
				<UrlEditor
					value={rawValue != null ? String(rawValue) : null}
					canEdit={canEdit}
					onChange={(v) => onChange?.(v)}
				/>
			);
	}
}
