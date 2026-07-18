import { useQuery } from "@tanstack/react-query";
import { useNavigate, useParams } from "@tanstack/react-router";
import { type ReactNode, useCallback, useEffect, useMemo, useRef } from "react";

import { ShortcutHelpDialog } from "@/components/shortcuts/shortcut-help-dialog";
import { usePermissions } from "@/hooks/use-permissions";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import { currentUserOptionalQueryOptions } from "@/lib/auth-api";
import { sprintsQueryOptions } from "@/lib/interaction-api";
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
	const { data: user } = useQuery(currentUserOptionalQueryOptions);
	const isAnonymous = !user;
	const { hasPermission } = usePermissions();
	const { hasProjectPermission } = useProjectPermissions(projectId ?? "");

	const canViewSprints =
		!!projectId &&
		(hasPermission("sprints.read") || hasProjectPermission("sprints.read"));

	const { data: sprints = [] } = useQuery({
		...sprintsQueryOptions(projectId ?? ""),
		enabled: !!projectId && (canViewSprints || isAnonymous),
	});

	const openSprints = useMemo(
		() =>
			sprints
				.filter((s) => s.status === "active")
				.sort((a, b) => a.name.localeCompare(b.name)),
		[sprints],
	);

	const helpOpen = useShortcutHelpStore((s) => s.open);
	const setHelpOpen = useShortcutHelpStore((s) => s.setOpen);
	const toggleHelp = useShortcutHelpStore((s) => s.toggle);

	const goto = useCallback(
		(path: string) => navigate({ to: path as "/" }),
		[navigate],
	);

	// Mirrors ANON_HIDDEN_SEGMENTS / canViewSprints gating in app-sidebar.tsx so
	// a shortcut never navigates somewhere the sidebar wouldn't show.
	const runGotoAction = useCallback(
		(action: ShortcutActionId) => {
			if (action === "goto.home") {
				goto("/home");
				return;
			}
			if (!projectId) return;
			const base = `/projects/${projectId}`;
			const sprintsAllowed = canViewSprints || isAnonymous;
			switch (action) {
				case "goto.timeline":
					if (sprintsAllowed) goto(`${base}/interactions/timeline`);
					return;
				case "goto.backlog":
					if (sprintsAllowed) goto(`${base}/interactions/backlog`);
					return;
				case "goto.activeSprint":
					if (sprintsAllowed && openSprints[0]) {
						goto(`${base}/interactions/sprints/${openSprints[0].id}`);
					}
					return;
				case "goto.agents":
					if (!isAnonymous) goto(`${base}/agents`);
					return;
				case "goto.conversations":
					if (!isAnonymous) goto(`${base}/conversations`);
					return;
				case "goto.automation":
					if (!isAnonymous) goto(`${base}/automation`);
					return;
				case "goto.team":
					if (!isAnonymous) goto(`${base}/team`);
					return;
				case "goto.docs":
					goto(`${base}/docs`);
					return;
				case "goto.settings":
					if (!isAnonymous) goto(`${base}/settings`);
					return;
				default:
					return;
			}
		},
		[goto, projectId, canViewSprints, isAnonymous, openSprints],
	);

	// Read the latest handler via a ref so the keydown listener effect below
	// only ever needs to attach once, instead of re-binding on every render.
	const runGotoActionRef = useRef(runGotoAction);
	runGotoActionRef.current = runGotoAction;

	useEffect(() => {
		const handleKeyDown = (e: KeyboardEvent) => {
			if (e.isComposing || e.key === "Process") return;
			if (isTypingTarget(e.target)) return;

			const action = matchShortcut(e);
			if (!action) return;

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
