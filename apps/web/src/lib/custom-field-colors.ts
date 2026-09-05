import type { CSSProperties } from "react";
import type { CustomFieldOption } from "@/lib/project-api";

// Looks up the color chosen for one select/multi_select option value, if any.
export function getCustomFieldOptionColor(
	options: CustomFieldOption[] | undefined,
	value: string,
): string | undefined {
	return options?.find((o) => o.value === value)?.color ?? undefined;
}

// Matches exactly the "#rrggbb" shape the color picker (presets + native
// <input type="color">) always produces. Nothing enforces this format
// server-side, so a value written some other way (a bare color name, 3-digit
// hex, garbage from a direct API call or MCP tool call) could otherwise reach
// here — appending an alpha suffix to a non-6-digit-hex string produces
// invalid CSS, silently dropping the background tint. Reject anything else
// up front so callers cleanly fall back to their default styling instead.
const HEX_COLOR_RE = /^#[0-9a-fA-F]{6}$/;

// Badge style for a colored custom field option chip: a tinted background
// derived from the option's color plus solid-color text. Returns undefined
// when no color was chosen (or it isn't a valid "#rrggbb" hex string), so
// callers fall back to their default (bg-primary/10 text-primary/80)
// className instead.
export function customFieldBadgeStyle(
	color: string | undefined,
): CSSProperties | undefined {
	if (!color || !HEX_COLOR_RE.test(color)) return undefined;
	return { backgroundColor: `${color}1a`, color };
}
