import { create } from "zustand";

/** Lets the sidebar button and the Mod+K shortcut open the one global search
 *  dialog, `GlobalSearchDialog`. */
export const useGlobalSearchStore = create<{
	open: boolean;
	setOpen: (open: boolean) => void;
	toggle: () => void;
}>((set) => ({
	open: false,
	setOpen: (open) => set({ open }),
	toggle: () => set((s) => ({ open: !s.open })),
}));
