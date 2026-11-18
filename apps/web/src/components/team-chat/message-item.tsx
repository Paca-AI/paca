import { format, isToday } from "date-fns";
import {
	Bookmark,
	MessageSquareReply,
	Pencil,
	SmilePlus,
	Trash2,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ConversationMessage } from "@/components/conversation/conversation-message";
import { ReactionBar } from "@/components/conversation/reaction-bar";
import { RunLink } from "@/components/conversation/run-card";
import { EntityAvatar } from "@/components/shared/entity-avatar";
import { Button } from "@/components/ui/button";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import { Textarea } from "@/components/ui/textarea";
import type { ChatBlock, ChatMessage, ChatPeer } from "@/lib/chat-api";
import {
	blocksToText,
	mentionsOf,
	sameText,
	textToBlocks,
} from "@/lib/chat-content";
import { MessageContent } from "./message-content";
import { PeerAvatar } from "./peer-avatar";

export const QUICK_REACTIONS = ["👍", "❤️", "😄", "🎉", "👀", "🙏"] as const;

export function formatMessageTime(iso: string): string {
	const d = new Date(iso);
	return isToday(d) ? format(d, "HH:mm") : format(d, "d MMM HH:mm");
}

export interface MessageActions {
	react: (m: ChatMessage, emoji: string, on: boolean) => void;
	edit: (m: ChatMessage, content: ChatBlock[]) => Promise<void>;
	remove: (m: ChatMessage) => void;
	openThread?: (m: ChatMessage) => void;
	/** Saves or unsaves a message for the caller. */
	save?: (m: ChatMessage, saved: boolean) => void;
}

/**
 * One message. `compact` omits the author line (a follow-up from the same
 * person); `canReact`, `canModerate` and `canReply` come from the viewer's
 * permissions.
 */
export function MessageItem({
	message,
	currentUserId,
	compact,
	canReact,
	canModerate,
	canReply,
	actions,
	peer,
}: {
	message: ChatMessage;
	currentUserId: string | undefined;
	compact?: boolean;
	canReact: boolean;
	canModerate: boolean;
	canReply: boolean;
	actions: MessageActions;
	/** The other party of a direct message, to show an agent or service account as such. */
	peer?: ChatPeer | null;
}) {
	const { t } = useTranslation("chat");
	const [editing, setEditing] = useState(false);
	const [draft, setDraft] = useState("");
	const [saving, setSaving] = useState(false);
	const [pickerOpen, setPickerOpen] = useState(false);
	const mine = message.author_id === currentUserId;
	const deleted = message.deleted_at !== null;
	const fromPeer =
		!!peer && peer.kind !== "user" && message.author_id === peer.account_id;

	const startEdit = () => {
		setDraft(blocksToText(message.content));
		setEditing(true);
	};
	const save = async () => {
		// The mentions already in the message stay linked if their text is kept.
		const next = textToBlocks(draft, mentionsOf(message.content));
		if (next.length === 0) return;
		if (sameText(next, message.content)) {
			setEditing(false);
			return;
		}
		setSaving(true);
		try {
			await actions.edit(message, next);
			setEditing(false);
		} finally {
			setSaving(false);
		}
	};

	const toolbar =
		!deleted && !editing ? (
			<>
				{canReact ? (
					<Popover open={pickerOpen} onOpenChange={setPickerOpen}>
						<PopoverTrigger
							render={
								<Button
									size="icon-sm"
									variant="ghost"
									aria-label={t("message.react")}
								/>
							}
						>
							<SmilePlus />
						</PopoverTrigger>
						<PopoverContent className="flex w-auto gap-1 p-1">
							{QUICK_REACTIONS.map((e) => (
								<button
									key={e}
									type="button"
									className="rounded p-1 text-lg hover:bg-muted"
									onClick={() => {
										actions.react(
											message,
											e,
											!message.reactions.some((r) => r.emoji === e && r.mine),
										);
										setPickerOpen(false);
									}}
								>
									{e}
								</button>
							))}
						</PopoverContent>
					</Popover>
				) : null}
				{canReply && actions.openThread ? (
					<Button
						size="icon-sm"
						variant="ghost"
						aria-label={t("message.reply")}
						onClick={() => actions.openThread?.(message)}
					>
						<MessageSquareReply />
					</Button>
				) : null}
				{actions.save ? (
					<Button
						size="icon-sm"
						variant="ghost"
						aria-label={
							message.saved ? t("message.unbookmark") : t("message.bookmark")
						}
						aria-pressed={message.saved}
						onClick={() => actions.save?.(message, !message.saved)}
					>
						<Bookmark className={message.saved ? "fill-current" : undefined} />
					</Button>
				) : null}
				{mine && canReply ? (
					<Button
						size="icon-sm"
						variant="ghost"
						aria-label={t("message.edit")}
						onClick={startEdit}
					>
						<Pencil />
					</Button>
				) : null}
				{mine || canModerate ? (
					<Button
						size="icon-sm"
						variant="ghost"
						aria-label={
							mine ? t("message.delete") : t("message.moderatorDelete")
						}
						onClick={() => actions.remove(message)}
					>
						<Trash2 />
					</Button>
				) : null}
			</>
		) : null;

	const footer = !deleted ? (
		<>
			<ReactionBar
				reactions={message.reactions}
				disabled={!canReact}
				onToggle={(emoji, on) => actions.react(message, emoji, on)}
			/>
			{message.reply_count > 0 && actions.openThread ? (
				<button
					type="button"
					className="mt-1 block text-xs font-medium text-primary hover:underline"
					onClick={() => actions.openThread?.(message)}
				>
					{t("message.viewThread", { count: message.reply_count })}
				</button>
			) : null}
			{message.run_id ? (
				<RunLink runId={message.run_id}>{t("message.viewRun")}</RunLink>
			) : null}
		</>
	) : null;

	return (
		<ConversationMessage
			data-message-id={message.id}
			data-seq={message.seq}
			avatar={
				fromPeer && peer ? (
					<PeerAvatar name={peer.name} kind={peer.kind} size="default" />
				) : (
					<EntityAvatar name={message.author_name} size="default" />
				)
			}
			badge={
				fromPeer && peer ? (
					<span className="rounded bg-muted px-1 text-[10px] text-muted-foreground">
						{peer.kind === "agent"
							? t("dm.badge.agent")
							: t("dm.badge.service_account")}
					</span>
				) : null
			}
			name={message.author_name}
			time={
				<time dateTime={message.created_at}>
					{formatMessageTime(message.created_at)}
				</time>
			}
			compact={compact}
			toolbar={toolbar}
			footer={footer}
		>
			{deleted ? (
				<p className="text-sm italic text-muted-foreground">
					{t("message.deleted")}
				</p>
			) : editing ? (
				<div className="space-y-2">
					<Textarea
						value={draft}
						onChange={(e) => setDraft(e.target.value)}
						onKeyDown={(e) => {
							if (e.key === "Escape") setEditing(false);
							if (
								e.key === "Enter" &&
								!e.shiftKey &&
								!e.nativeEvent.isComposing
							) {
								e.preventDefault();
								void save();
							}
						}}
						autoFocus
						aria-label={t("message.edit")}
					/>
					<div className="flex gap-2">
						<Button size="sm" disabled={saving} onClick={() => void save()}>
							{t("message.save")}
						</Button>
						<Button size="sm" variant="ghost" onClick={() => setEditing(false)}>
							{t("message.cancel")}
						</Button>
					</div>
				</div>
			) : (
				<>
					<MessageContent
						blocks={message.content}
						currentUserId={currentUserId}
					/>
					{message.edited_at ? (
						<span className="text-xs text-muted-foreground">
							{t("message.edited")}
						</span>
					) : null}
				</>
			)}
		</ConversationMessage>
	);
}
