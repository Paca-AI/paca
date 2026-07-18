import { create } from "zustand";

/** Lets any component (user menu, `?`) open the shortcuts help dialog that
 * `ShortcutProvider` renders once at the app root. */
export const useShortcutHelpStore = create<{
	open: boolean;
	setOpen: (open: boolean) => void;
	toggle: () => void;
}>((set) => ({
	open: false,
	setOpen: (open) => set({ open }),
	toggle: () => set((s) => ({ open: !s.open })),
}));
