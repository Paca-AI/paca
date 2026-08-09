import { useEffect } from "react";
import { useBranding } from "@/hooks/use-branding";
import { useThemeMode } from "@/hooks/use-theme-mode";

const FAVICON_LINK_ID = "app-favicon";
const DEFAULT_FAVICON_HREF = "/favicon.ico";
const DEFAULT_TITLE = "Paca";

// Exported for unit testing (see branding-effects.test.ts) — otherwise only
// used within this module.
export function hexToRgb(hex: string): [number, number, number] | null {
	const match = /^#([0-9a-f]{6})$/i.exec(hex);
	if (!match) return null;
	const int = parseInt(match[1], 16);
	return [(int >> 16) & 255, (int >> 8) & 255, int & 255];
}

export function rgbToHex([r, g, b]: [number, number, number]): string {
	const clamp = (n: number) => Math.max(0, Math.min(255, Math.round(n)));
	return `#${[r, g, b].map((c) => clamp(c).toString(16).padStart(2, "0")).join("")}`;
}

/** Picks the same near-black/white pair index.css already hardcodes for
 * --primary-foreground (#0a0a0a / #ffffff) via a standard perceived-
 * brightness threshold, so admin-set colors keep readable button/icon text
 * without the admin having to pick a foreground color themselves. */
export function foregroundFor(hex: string): string {
	const rgb = hexToRgb(hex);
	if (!rgb) return "#ffffff";
	const [r, g, b] = rgb;
	const brightness = (r * 299 + g * 587 + b * 114) / 1000;
	return brightness > 150 ? "#0a0a0a" : "#ffffff";
}

/** Darkens hex toward black by `amount` (0-1) — used for --lagoon-deep, a
 * hover-state shade one step darker than the base link color, the same
 * relationship index.css's own hardcoded --lagoon/--lagoon-deep pair has. */
export function darken(hex: string, amount: number): string {
	const rgb = hexToRgb(hex);
	if (!rgb) return hex;
	return rgbToHex(rgb.map((c) => c * (1 - amount)) as [number, number, number]);
}

// index.css ties every one of these to the exact same hex as --primary in
// both light and dark mode by default (the sidebar's active nav item, focus
// rings, the app-wide default link color, and a couple of decorative accents
// are all meant to read as one brand color, not independently-styled ones) —
// so an admin-set color needs to override the whole family together, or the
// app keeps showing the old hardcoded green everywhere these are used
// instead of --primary directly: the sidebar, every <a> link (index.css's
// `@layer base` sets `color: var(--lagoon)` globally), the login hero's
// feature icons, and the rich-text editor's dark-mode text-selection
// highlight.
const DIRECT_VARS = [
	"--primary",
	"--sidebar-primary",
	"--ring",
	"--sidebar-ring",
	"--lagoon",
	"--palm",
	"--bn-colors-selected-background",
];
const FOREGROUND_VARS = [
	"--primary-foreground",
	"--sidebar-primary-foreground",
	"--bn-colors-selected-text",
];
const DARKEN_AMOUNT = 0.25;

/**
 * No visual output — applies fetched instance branding (primary color CSS
 * variables, favicon) to the document. Mounted once in routes/__root.tsx so
 * it runs for both authenticated and public (login) pages alike.
 */
export function BrandingEffects() {
	const branding = useBranding();
	const { resolvedMode } = useThemeMode();

	const colorLight = branding?.primary_color_light;
	const colorDark = branding?.primary_color_dark;

	useEffect(() => {
		const root = document.documentElement;
		const color = resolvedMode === "dark" ? colorDark : colorLight;

		if (color) {
			const foreground = foregroundFor(color);
			for (const v of DIRECT_VARS) root.style.setProperty(v, color);
			for (const v of FOREGROUND_VARS) root.style.setProperty(v, foreground);
			root.style.setProperty("--lagoon-deep", darken(color, DARKEN_AMOUNT));
		} else {
			for (const v of [...DIRECT_VARS, ...FOREGROUND_VARS, "--lagoon-deep"]) {
				root.style.removeProperty(v);
			}
		}
	}, [colorLight, colorDark, resolvedMode]);

	const faviconUrl = branding?.favicon_thumb_url ?? branding?.favicon_url;

	useEffect(() => {
		const link = document.getElementById(
			FAVICON_LINK_ID,
		) as HTMLLinkElement | null;
		if (!link) return;

		if (faviconUrl) {
			link.href = faviconUrl;
			link.type = "image/png";
		} else {
			link.href = DEFAULT_FAVICON_HREF;
			link.type = "image/x-icon";
		}
	}, [faviconUrl]);

	const brandName = branding?.brand_name;

	useEffect(() => {
		document.title = brandName || DEFAULT_TITLE;
	}, [brandName]);

	return null;
}
