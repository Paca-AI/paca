import { createContext, useContext } from "react";

export type ChatContextValue = {
	/** The agent is mid-turn. */
	isRunning: boolean;
	/** Id of the newest message, for "only the latest question is answerable". */
	lastMessageId: string | undefined;
	/** Sends `text` as the user's next message. */
	send: (text: string) => Promise<void> | void;
};

const ChatContext = createContext<ChatContextValue | null>(null);

export const ChatProvider = ChatContext.Provider;

export function useChat(): ChatContextValue {
	const value = useContext(ChatContext);
	if (!value) throw new Error("useChat must be used inside <Thread>");
	return value;
}
