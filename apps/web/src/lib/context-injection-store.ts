import { create } from "zustand";
import type { ContextItem, ContextItemType } from "@/lib/context-items";

/**
 * Items staged for the *next* outgoing chat message — populated by the "+"
 * search popover / quick-add affordance in
 * components/assistant-ui/context-injection.tsx, read and cleared by
 * whichever composer call site actually sends the message (ai-chat-float.tsx,
 * ai-chat-float-global.tsx, new-conversation-thread.tsx, conversation-view.tsx).
 *
 * A single global (non-scoped) store is safe here: exactly one `Composer`
 * (components/assistant-ui/thread.tsx) is ever mounted anywhere in the app at
 * a time — the project-scoped and global floating chats hide themselves on
 * the routes where the Conversations page's own composer mounts instead (see
 * routes/_authenticated/projects/$projectId.tsx's onConversationsPage check
 * and routes/_authenticated.tsx's CONVERSATIONS_ROUTE_RE check).
 */
interface ContextInjectionState {
	items: ContextItem[];
	add: (item: ContextItem) => void;
	remove: (type: ContextItemType, id: string) => void;
	clear: () => void;
}

export const useContextInjectionStore = create<ContextInjectionState>(
	(set, get) => ({
		items: [],
		add: (item) => {
			if (get().items.some((i) => i.type === item.type && i.id === item.id)) {
				return;
			}
			set((s) => ({ items: [...s.items, item] }));
		},
		remove: (type, id) =>
			set((s) => ({
				items: s.items.filter((i) => !(i.type === type && i.id === id)),
			})),
		clear: () => set({ items: [] }),
	}),
);
