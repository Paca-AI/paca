import { create } from "zustand";
import type { ContextItem, ContextItemType } from "@/lib/context-items";

/**
 * The Task/Doc/Automation currently on screen (a task detail modal/page, a
 * doc editor page, or an automation builder page) — registered by that
 * page/modal on mount/data-load and cleared on unmount, mirroring
 * hovered-task-store.ts's "latest registration wins" pattern exactly: only
 * one such page can be showing at a time, so the store just holds the latest
 * registration, with `clearActive` guarded so a slow-unmounting page can't
 * clobber a newly-focused one's registration.
 *
 * Powers the chat composer's one-click "quick add" affordance (see
 * components/assistant-ui/context-injection.tsx) — read `active` there and
 * call the context-injection-store's `add` directly, no search needed.
 */
interface CurrentContextState {
	active: ContextItem | null;
	setActive: (item: ContextItem) => void;
	clearActive: (type: ContextItemType, id: string) => void;
}

export const useCurrentContextStore = create<CurrentContextState>(
	(set, get) => ({
		active: null,
		setActive: (item) => set({ active: item }),
		clearActive: (type, id) => {
			const active = get().active;
			if (active?.type === type && active.id === id) set({ active: null });
		},
	}),
);
