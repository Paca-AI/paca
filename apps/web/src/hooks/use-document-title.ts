import { useQuery } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { useEffect } from "react";
import { DEFAULT_TITLE } from "@/components/app-shell/branding-effects";
import { useBranding } from "@/hooks/use-branding";
import { MAX_DISPLAYED_UNREAD_COUNT } from "@/lib/notification-api";
import { projectQueryOptions } from "@/lib/project-api";

// Exported for unit testing (see use-document-title.test.ts).
export function buildDocumentTitle(
	unreadCount: number,
	projectName: string | null | undefined,
	appTitle: string,
): string {
	const base = projectName ? `${projectName} · ${appTitle}` : appTitle;
	if (unreadCount <= 0) return base;
	const count =
		unreadCount > MAX_DISPLAYED_UNREAD_COUNT
			? `${MAX_DISPLAYED_UNREAD_COUNT}+`
			: unreadCount;
	return `(${count}) ${base}`;
}

/**
 * Keeps the browser tab title in sync with the unread notification count
 * and, when inside a project, that project's name — e.g. "(3) Acme · Paca".
 * Mounted from NotificationBell, which already tracks unreadCount — so this
 * only fires for authenticated users, same as the bell itself.
 *
 * `useParams({ strict: false })` mirrors AppSidebar's ProjectSwitcher and
 * ShortcutProvider (see components/app-shell/app-sidebar.tsx and
 * lib/shortcuts/provider.tsx): it reads projectId from whatever route is
 * currently active without this hook needing to be mounted inside the
 * project route tree itself. The project query reuses ProjectLayout's own
 * cache entry (see routes/_authenticated/projects/$projectId.tsx), so this
 * doesn't trigger an extra fetch.
 *
 * BrandingEffects (components/app-shell/branding-effects.tsx) is the one
 * other place that writes document.title, reacting only to brandName
 * changes. It's mounted as an earlier sibling of the routed page in
 * routes/__root.tsx, so its effect always commits before this one on the
 * same render — this hook's cleanup then restores the bare base title on
 * unmount (e.g. logout) so it doesn't linger with a stale project/count.
 */
export function useDocumentTitle(unreadCount: number): void {
	const branding = useBranding();
	const appTitle = branding?.brand_name || DEFAULT_TITLE;

	const { projectId } = useParams({ strict: false });
	const { data: project } = useQuery({
		...projectQueryOptions(projectId ?? ""),
		enabled: !!projectId,
	});
	const projectName = projectId ? project?.name : null;

	useEffect(() => {
		document.title = buildDocumentTitle(unreadCount, projectName, appTitle);
		return () => {
			document.title = appTitle;
		};
	}, [unreadCount, projectName, appTitle]);
}
