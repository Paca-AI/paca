import { Puzzle } from "lucide-react";
import { type ComponentType, lazy, Suspense } from "react";

const resolvedIconCache = new Map<
	string,
	ComponentType<{ className?: string }>
>();

/**
 * Resolves a lucide-react icon by its PascalCase export name (as referenced
 * in a plugin manifest's `navItems[].icon`, e.g. "Clock", "BarChart3").
 * Falls back to a generic puzzle-piece icon for unknown/omitted names, and
 * transiently while the real icon is still loading.
 *
 * lucide-react's `icons` export — every icon in the library, since a plugin
 * can name any of them — is loaded lazily on first use instead of imported
 * statically. app-sidebar.tsx (the only caller) renders unconditionally on
 * every authenticated page, so a static import here put the whole icon set
 * on that page's critical path even for sessions with zero installed
 * plugins declaring a custom nav icon.
 */
export function resolvePluginIcon(
	name?: string,
): ComponentType<{ className?: string }> {
	if (!name) return Puzzle;

	const cached = resolvedIconCache.get(name);
	if (cached) return cached;

	const LazyIcon = lazy(async () => {
		const { icons } = await import("lucide-react");
		const Icon = (
			icons as Record<string, ComponentType<{ className?: string }> | undefined>
		)[name];
		return { default: Icon ?? Puzzle };
	});

	function Resolved({ className }: { className?: string }) {
		return (
			<Suspense fallback={<Puzzle className={className} />}>
				<LazyIcon className={className} />
			</Suspense>
		);
	}
	Resolved.displayName = `PluginIcon(${name})`;

	resolvedIconCache.set(name, Resolved);
	return Resolved;
}
