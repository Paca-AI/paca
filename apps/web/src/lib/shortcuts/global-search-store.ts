import { create } from "zustand";

/** Lets the header button and the Mod+P shortcut open the one global search
 *  dialog that `GlobalSearch` renders in the app header. */
export const useGlobalSearchStore = create<{
	open: boolean;
	setOpen: (open: boolean) => void;
	toggle: () => void;
}>((set) => ({
	open: false,
	setOpen: (open) => set({ open }),
	toggle: () => set((s) => ({ open: !s.open })),
}));
