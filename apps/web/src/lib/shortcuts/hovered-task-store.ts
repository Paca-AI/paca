import { create } from "zustand";

/**
 * Action callbacks the currently-hovered task card or row exposes to the
 * global shortcut provider. `TaskCard`/`TaskRow` register a task's actions
 * on mouseenter and clear them on mouseleave. Only one task can be "active"
 * at a time, so the store just holds the latest registration; no id
 * bookkeeping needed since callers always clear their own entry.
 */
export interface TaskShortcutActions {
	taskId: string;
	canEdit: boolean;
	open: () => void;
	openEpic: () => void;
	/** Absent when the view has no column concept (e.g. Roadmap). */
	moveLeft?: () => void;
	moveRight?: () => void;
	delete: () => void;
}

interface HoveredTaskState {
	active: TaskShortcutActions | null;
	setActive: (actions: TaskShortcutActions) => void;
	clearActive: (taskId: string) => void;
}

export const useHoveredTaskStore = create<HoveredTaskState>((set, get) => ({
	active: null,
	setActive: (actions) => set({ active: actions }),
	clearActive: (taskId) => {
		if (get().active?.taskId === taskId) set({ active: null });
	},
}));
