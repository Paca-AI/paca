import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { FieldRow, FieldValue } from "../primitives";
import { CheckboxEditor } from "./checkbox-editor";
import { CustomFieldEditor } from "./custom-field-editor";
import { DateRangeEditor, SingleDateEditor } from "./date-editor";
import { MultiUserEditor } from "./multi-user-editor";
import { NumberEditor } from "./number-editor";
import { SelectEditor } from "./select-editor";
import { TagsEditor } from "./tags-editor";
import { TextEditor } from "./text-editor";
import type { PropertyFieldProps } from "./types";
import { UserEditor } from "./user-editor";

export type {
	PropertyFieldMode,
	PropertyFieldProps,
	SelectOption,
	UserOption,
} from "./types";

export function PropertyField({
	label,
	mode,
	canEdit = true,
	hidden,
	align = "start",
	value,
	options = [],
	onChange,
	dateValue,
	onDateChange,
	startDate,
	dueDate,
	onDateRangeChange,
	numberValue = 0,
	onNumberChange,
	textValue,
	onTextChange,
	checked = false,
	onCheckedChange,
	userValue,
	users = [],
	onUserChange,
	showUnassigned = true,
	userValues,
	onUsersChange,
	tags = [],
	onTagsChange,
	displayValue,
	linkValue,
	onLinkClick,
	linkIcon,
	customType,
	customRawValue,
	onCustomChange,
	customOptions = [],
	customRows,
}: PropertyFieldProps) {
	const { t } = useTranslation("projects");
	if (hidden) return null;

	function renderContent(): ReactNode {
		switch (mode) {
			case "select":
				if (!canEdit) {
					const selected = options.find(
						(o) => o.value === (Array.isArray(value) ? value[0] : value),
					);
					if (!selected) return <FieldValue empty />;
					return (
						<button
							type="button"
							className="inline-flex items-center gap-2 rounded-full border border-border/30 bg-muted/30 px-3 py-1 text-sm font-semibold text-muted-foreground"
						>
							{selected.colorDot && (
								<span
									className="size-1.75 rounded-full shrink-0"
									style={{ background: selected.colorDot }}
								/>
							)}
							{selected.icon}
							{selected.label}
						</button>
					);
				}
				return (
					<SelectEditor
						value={value ?? null}
						options={options}
						onChange={(v) => onChange?.(v)}
						align={align}
					/>
				);

			case "date":
				if (!canEdit) {
					return (
						<span className="inline-flex items-center gap-1.5 rounded-lg border border-border/25 bg-muted/25 px-2.5 py-1.5 text-xs text-muted-foreground font-medium">
							{dateValue ?? t("taskDetail.common.empty")}
						</span>
					);
				}
				return <SingleDateEditor value={dateValue} onChange={onDateChange} />;

			case "date-range":
				return (
					<DateRangeEditor
						startDate={startDate}
						dueDate={dueDate}
						canEdit={canEdit}
						onChange={onDateRangeChange}
					/>
				);

			case "number":
				if (!canEdit) {
					return (
						<span className="text-sm tabular-nums font-medium text-foreground">
							{numberValue}
						</span>
					);
				}
				return (
					<NumberEditor value={numberValue ?? 0} onChange={onNumberChange} />
				);

			case "text":
				return (
					<TextEditor
						value={textValue ?? null}
						canEdit={canEdit}
						onChange={onTextChange}
					/>
				);

			case "checkbox":
				return (
					<CheckboxEditor
						checked={checked}
						canEdit={canEdit}
						onChange={onCheckedChange}
					/>
				);

			case "user":
				return (
					<UserEditor
						userValue={userValue}
						users={users}
						canEdit={canEdit}
						showUnassigned={showUnassigned}
						onChange={onUserChange}
					/>
				);

			case "multi-user":
				return (
					<MultiUserEditor
						userValues={userValues}
						users={users}
						canEdit={canEdit}
						onChange={onUsersChange}
					/>
				);

			case "tags":
				return (
					<TagsEditor tags={tags} canEdit={canEdit} onChange={onTagsChange} />
				);

			case "readonly":
				return <>{displayValue ?? <FieldValue empty />}</>;

			case "link":
				return (
					<button
						type="button"
						onClick={onLinkClick}
						className="flex items-center gap-1.5 text-sm text-primary/80 hover:text-primary font-medium hover:underline underline-offset-2 transition-colors duration-150"
					>
						{linkIcon}
						<span className="truncate">{linkValue}</span>
					</button>
				);

			case "custom":
				if (!customType) return <FieldValue empty />;
				return (
					<CustomFieldEditor
						customType={customType}
						rawValue={customRawValue}
						canEdit={canEdit}
						options={customOptions}
						rows={customRows}
						onChange={onCustomChange}
					/>
				);
		}
	}

	return <FieldRow label={label}>{renderContent()}</FieldRow>;
}
