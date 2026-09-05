import type { CSSProperties } from "react";
import type { CustomFieldOption } from "@/lib/project-api";

// Looks up the color chosen for one select/multi_select option value, if any.
export function getCustomFieldOptionColor(
	options: CustomFieldOption[] | undefined,
	value: string,
): string | undefined {
	return options?.find((o) => o.value === value)?.color ?? undefined;
}

// Badge style for a colored custom field option chip: a tinted background
// derived from the option's color plus solid-color text. Returns undefined
// when no color was chosen, so callers fall back to their default
// (bg-primary/10 text-primary/80) className instead.
export function customFieldBadgeStyle(
	color: string | undefined,
): CSSProperties | undefined {
	if (!color) return undefined;
	return { backgroundColor: `${color}1a`, color };
}
