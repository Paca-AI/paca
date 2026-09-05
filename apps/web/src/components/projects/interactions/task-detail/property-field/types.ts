import type { ReactNode } from "react";

export type PropertyFieldMode =
	| "select"
	| "multi-select"
	| "date"
	| "date-range"
	| "number"
	| "text"
	| "checkbox"
	| "user"
	| "multi-user"
	| "tags"
	| "readonly"
	| "link"
	| "custom";

export interface SelectOption {
	value: string;
	label: string;
	icon?: ReactNode;
	colorDot?: string;
	/** Small muted hint shown after the label in the dropdown list only (not
	 * in the compact trigger pill once a value is selected). */
	hint?: string;
}

export interface UserOption {
	value: string;
	label: string;
	initials: string;
	avatarUrl?: string | null;
}

export interface PropertyFieldProps {
	label: string;
	mode: PropertyFieldMode;
	canEdit?: boolean;

	value?: string | string[] | null;
	options?: SelectOption[];
	onChange?: (value: string | string[] | null) => void;

	dateValue?: string | null;
	onDateChange?: (value: string | null) => void;

	startDate?: string | null;
	dueDate?: string | null;
	onStartDateChange?: (value: string | null) => void;
	onDueDateChange?: (value: string | null) => void;

	numberValue?: number | null;
	onNumberChange?: (value: number) => void;

	textValue?: string | null;
	onTextChange?: (value: string) => void;

	checked?: boolean;
	onCheckedChange?: (value: boolean) => void;

	userValue?: UserOption | null;
	users?: UserOption[];
	onUserChange?: (value: string | null) => void;
	showUnassigned?: boolean;

	userValues?: UserOption[];
	onUsersChange?: (values: string[]) => void;

	tags?: string[];
	onTagsChange?: (tags: string[]) => void;

	displayValue?: ReactNode;
	linkValue?: string;
	onLinkClick?: () => void;

	customType?:
		| "Text"
		| "Number"
		| "Date"
		| "Checkbox"
		| "Select"
		| "MultiSelect"
		| "Url";
	customRawValue?: unknown;
	onCustomChange?: (value: unknown) => void;
	customOptions?: string[];

	hidden?: boolean;
	linkIcon?: ReactNode;
	align?: "start" | "end" | "center";
}
