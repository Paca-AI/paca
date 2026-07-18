import { create } from "zustand";

/**
 * Actions the currently-mounted interaction page (backlog/sprint/timeline)
 * exposes to the global shortcut provider. `InteractionLayout` registers on
 * mount and clears on unmount — only one interaction page is ever mounted at
 * a time, so this mirrors `hovered-task-store.ts`'s "latest registration
 * wins" shape.
 */
export interface PageShortcutActions {
	prevView: () => void;
	nextView: () => void;
	focusSearch: () => void;
	toggleViewSettings: () => void;
	focusCreateTask: () => void;
}

interface PageShortcutState {
	active: PageShortcutActions | null;
	setActive: (actions: PageShortcutActions) => void;
	clearActive: () => void;
}

export const usePageShortcutStore = create<PageShortcutState>((set) => ({
	active: null,
	setActive: (actions) => set({ active: actions }),
	clearActive: () => set({ active: null }),
}));
