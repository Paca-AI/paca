import { useCallback, useState } from "react";
import {
	type ChatSidebarState,
	loadSidebarState,
	saveSidebarState,
} from "@/lib/chat-sidebar";

/** What the chat sidebar remembers between visits (which sections and
 * project groups are open or closed), kept in localStorage. */
export function useChatSidebarState() {
	const [state, setState] = useState<ChatSidebarState>(loadSidebarState);
	const update = useCallback(
		(change: (s: ChatSidebarState) => ChatSidebarState) =>
			setState((cur) => {
				const next = change(cur);
				saveSidebarState(next);
				return next;
			}),
		[],
	);
	return { state, update };
}
