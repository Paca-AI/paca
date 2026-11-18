import { Fragment } from "react";
import { useTranslation } from "react-i18next";
import {
	DayDivider,
	NewMessagesDivider,
} from "@/components/conversation/dividers";
import { ConversationScroller } from "@/components/conversation/scroller";
import {
	isFollowUp as isFollowUpEntry,
	layout,
} from "@/components/conversation/timeline";
import type { ChatMessage, ChatPeer } from "@/lib/chat-api";
import { type MessageActions, MessageItem } from "./message-item";

const entry = (m: ChatMessage) => ({
	id: m.id,
	authorId: m.author_id,
	at: m.created_at,
	deleted: m.deleted_at !== null,
	seq: m.seq,
});

/** A follow-up from the same person within a few minutes drops its author line. */
export function isFollowUp(
	prev: ChatMessage | undefined,
	m: ChatMessage,
): boolean {
	return isFollowUpEntry(prev ? entry(prev) : undefined, entry(m));
}

/**
 * The messages of a channel, oldest first, in the shared conversation scroller,
 * with a divider at each new day and where the reader's unread messages begin
 * (`readSeq` is how far they had read when they arrived).
 */
export function MessageList({
	items,
	hasOlder,
	loadingOlder,
	onLoadOlder,
	currentUserId,
	canReact,
	canModerate,
	canReply,
	actions,
	emptyLabel,
	readSeq,
	footer,
	peer,
}: {
	items: ChatMessage[];
	hasOlder: boolean;
	loadingOlder: boolean;
	onLoadOlder: () => void;
	currentUserId: string | undefined;
	canReact: boolean;
	canModerate: boolean;
	canReply: boolean;
	actions: MessageActions;
	emptyLabel?: string;
	readSeq?: number;
	footer?: React.ReactNode;
	peer?: ChatPeer | null;
}) {
	const { t } = useTranslation("chat");
	// The reader's own messages are never "new" to them.
	const slots = layout(
		items.map((m) => ({
			...entry(m),
			seq: m.author_id === currentUserId ? undefined : m.seq,
		})),
		readSeq,
	);
	return (
		<ConversationScroller
			ids={items.map((m) => m.id)}
			hasOlder={hasOlder}
			loadingOlder={loadingOlder}
			onLoadOlder={onLoadOlder}
			loadOlderLabel={t("list.loadOlder")}
			newLabel={t("list.newMessages")}
			emptyLabel={emptyLabel ?? t("list.empty")}
			footer={footer}
		>
			{items.map((m, i) => {
				const slot = slots[i];
				return (
					<Fragment key={m.id}>
						{slot?.dayStart ? (
							<DayDivider
								iso={m.created_at}
								today={t("list.today")}
								yesterday={t("list.yesterday")}
							/>
						) : null}
						{slot?.newStart ? (
							<NewMessagesDivider label={t("list.newDivider")} />
						) : null}
						<MessageItem
							message={m}
							currentUserId={currentUserId}
							compact={slot?.compact}
							canReact={canReact}
							canModerate={canModerate}
							canReply={canReply}
							actions={actions}
							peer={peer}
						/>
					</Fragment>
				);
			})}
		</ConversationScroller>
	);
}
