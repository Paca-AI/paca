import { useQuery } from "@tanstack/react-query";
import { useNavigate, useParams } from "@tanstack/react-router";
import { type ReactNode, useCallback, useEffect, useMemo, useRef } from "react";

import { ShortcutHelpDialog } from "@/components/shortcuts/shortcut-help-dialog";
import { usePermissions } from "@/hooks/use-permissions";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import { listSprints } from "@/lib/interaction-api";
import { useGlobalSearchStore } from "./global-search-store";
import { useShortcutHelpStore } from "./help-dialog-store";
import { useHoveredTaskStore } from "./hovered-task-store";
import { matchShortcut, type ShortcutActionId } from "./keymap";
import { usePageShortcutStore } from "./page-shortcut-store";

const TYPING_TAGS = new Set(["INPUT", "TEXTAREA", "SELECT"]);

function isTypingTarget(target: EventTarget | null): boolean {
	if (!(target instanceof HTMLElement)) return false;
	if (TYPING_TAGS.has(target.tagName)) return true;
	if (target.isContentEditable) return true;
	return !!target.closest('[contenteditable="true"]');
}

/**
 * Mounts the single global keydown listener for the whole shortcut system.
 * Navigation actions are resolved here since they need router state,
 * permissions, and the sprint list; page- and task-scope actions are looked
 * up live from their stores (see page-shortcut-store.ts / hovered-task-store.ts)
 * so this component never needs to know what page or task is on screen.
 */
export function ShortcutProvider({ children }: { children: ReactNode }) {
	const navigate = useNavigate();
	const { projectId } = useParams({ strict: false });
	const { hasPermission } = usePermissions();
	const { hasProjectPermission } = useProjectPermissions(projectId ?? "");

	const canViewSprints =
		!!projectId &&
		(hasPermission("sprints:read") || hasProjectPermission("sprints:read"));

	// Only the first open sprint is needed (the "go to sprint" shortcut).
	const { data: activeSprints } = useQuery({
		queryKey: ["projects", projectId, "sprints", "first-active"],
		queryFn: () =>
			listSprints(projectId ?? "", { status: ["active"], pageSize: 1 }),
		enabled: !!projectId && canViewSprints,
		staleTime: 30_000,
	});

	const openSprints = useMemo(
		() => activeSprints?.items ?? [],
		[activeSprints],
	);

	const helpOpen = useShortcutHelpStore((s) => s.open);
	const setHelpOpen = useShortcutHelpStore((s) => s.setOpen);
	const toggleHelp = useShortcutHelpStore((s) => s.toggle);

	const goto = useCallback(
		(path: string) => navigate({ to: path as "/" }),
		[navigate],
	);

	// Mirrors the permission gating in app-sidebar.tsx so a shortcut never
	// navigates somewhere the sidebar wouldn't show.
	const runGotoAction = useCallback(
		(action: ShortcutActionId) => {
			if (action === "goto.home") {
				goto("/home");
				return;
			}
			if (!projectId) return;
			const base = `/projects/${projectId}`;
			switch (action) {
				case "goto.timeline":
					if (canViewSprints) goto(`${base}/interactions/timeline`);
					return;
				case "goto.backlog":
					if (canViewSprints) goto(`${base}/interactions/backlog`);
					return;
				case "goto.activeSprint":
					if (canViewSprints && openSprints[0]) {
						goto(`${base}/interactions/sprints/${openSprints[0].id}`);
					}
					return;
				case "goto.agents":
					goto(`${base}/agents`);
					return;
				case "goto.conversations":
					goto(`${base}/conversations`);
					return;
				case "goto.automation":
					goto(`${base}/automation`);
					return;
				case "goto.team":
					goto(`${base}/team`);
					return;
				case "goto.docs":
					goto(`${base}/docs`);
					return;
				case "goto.settings":
					goto(`${base}/settings`);
					return;
				default:
					return;
			}
		},
		[goto, projectId, canViewSprints, openSprints],
	);

	// Read the latest handler via a ref so the keydown listener effect below
	// only ever needs to attach once, instead of re-binding on every render.
	const runGotoActionRef = useRef(runGotoAction);
	runGotoActionRef.current = runGotoAction;

	useEffect(() => {
		const handleKeyDown = (e: KeyboardEvent) => {
			if (e.isComposing || e.key === "Process") return;

			const action = matchShortcut(e);
			if (!action) return;
			// Search works from anywhere, a text field included: Mod+K has no
			// editing meaning here, and the browser would otherwise take it.
			if (action === "general.search") {
				e.preventDefault();
				useGlobalSearchStore.getState().toggle();
				return;
			}
			if (isTypingTarget(e.target)) return;

			if (action === "general.help") {
				e.preventDefault();
				toggleHelp();
				return;
			}
			if (action.startsWith("goto.")) {
				e.preventDefault();
				runGotoActionRef.current(action);
				return;
			}
			if (action.startsWith("page.")) {
				// Only claim the key when an interaction page actually registered a
				// handler — otherwise leave the browser default (e.g. native
				// find-in-page on Mod+F) alone on pages with no page-scope actions.
				const page = usePageShortcutStore.getState().active;
				if (!page) return;
				e.preventDefault();
				switch (action) {
					case "page.prevView":
						page.prevView();
						return;
					case "page.nextView":
						page.nextView();
						return;
					case "page.search":
						page.focusSearch();
						return;
					case "page.viewSettings":
						page.toggleViewSettings();
						return;
					case "page.createTask":
						page.focusCreateTask();
						return;
					default:
						return;
				}
			}
			if (action.startsWith("task.")) {
				// Only claim the key while a task is actually hovered — otherwise
				// leave the browser default (e.g. native Mod+O) alone everywhere else.
				const task = useHoveredTaskStore.getState().active;
				if (!task) return;
				e.preventDefault();
				switch (action) {
					case "task.open":
						task.open();
						return;
					case "task.epic":
						if (task.canEdit) task.openEpic();
						return;
					case "task.moveLeft":
						if (task.canEdit) task.moveLeft?.();
						return;
					case "task.moveRight":
						if (task.canEdit) task.moveRight?.();
						return;
					case "task.delete":
						if (task.canEdit) task.delete();
						return;
					default:
						return;
				}
			}
		};

		window.addEventListener("keydown", handleKeyDown);
		return () => window.removeEventListener("keydown", handleKeyDown);
	}, [toggleHelp]);

	return (
		<>
			{children}
			<ShortcutHelpDialog open={helpOpen} onOpenChange={setHelpOpen} />
		</>
	);
}
