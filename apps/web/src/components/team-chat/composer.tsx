import { SendHorizontal } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { CommentEditorHandle } from "@/components/shared/comment-blocknote";
import { MessageEditor } from "@/components/shared/message-editor";
import { Button } from "@/components/ui/button";
import { useCommandRunner, useSlashCommands } from "@/hooks/use-slash-commands";
import type { ChatBlock } from "@/lib/chat-api";
import { isEmptyMessage, prepareForSend } from "@/lib/chat-blocks";
import { blocksToText, MAX_MESSAGE_LENGTH } from "@/lib/chat-content";
import { chatErrorKey } from "@/lib/chat-errors";
import type { SlashCommand } from "@/lib/slash-commands";
import { findCommand } from "@/lib/slash-commands";

export type { SlashCommand } from "@/lib/slash-commands";

const draftKey = (id: string) => `paca:chat-draft:${id}`;

/** The saved draft: blocks, or the plain text an older version kept. */
function readDraft(id: string): ChatBlock[] {
	try {
		const raw = localStorage.getItem(draftKey(id));
		if (!raw) return [];
		try {
			const parsed: unknown = JSON.parse(raw);
			if (Array.isArray(parsed)) return parsed as ChatBlock[];
		} catch {
			/* plain text */
		}
		return [
			{
				type: "paragraph",
				content: [{ type: "text", text: raw, styles: {} }],
			},
		];
	} catch {
		return [];
	}
}

function writeDraft(id: string, blocks: ChatBlock[]): void {
	try {
		if (isEmptyMessage(blocks)) localStorage.removeItem(draftKey(id));
		else localStorage.setItem(draftKey(id), JSON.stringify(blocks));
	} catch {
		/* a draft is a convenience */
	}
}

/**
 * The message box, a BlockNote editor (see CommentEditor). Enter sends,
 * Shift+Enter adds a line; the draft is kept per `draftId` (a channel or a
 * thread) so switching away loses nothing. While a send is in flight the text
 * stays, and is cleared only when it succeeded.
 *
 * "@" suggests people, "#" the project's tasks and documents, and "/" the
 * `commands` it is given (a command runs instead of sending its text).
 */
export function Composer({
	draftId,
	placeholder,
	disabledReason,
	onSend,
	autoFocus,
	mentionProjectId,
	onTyping,
	extraCommands,
}: {
	/** Reports whether the box has text, for the "is typing" indicator. */
	onTyping?: (typing: boolean) => void;
	/** Offers "@" and "#" suggestions from this project when set. */
	mentionProjectId?: string;
	/** Commands only chat has (`/new`), added to the shared task and document
	 *  commands every rich editor offers. */
	extraCommands?: SlashCommand[];
	draftId: string;
	placeholder: string;
	/** Why posting is not possible; the box is replaced by this text. */
	disabledReason?: string;
	onSend: (content: ChatBlock[]) => Promise<void>;
	autoFocus?: boolean;
}) {
	const { t } = useTranslation("chat");
	const editor = useRef<CommentEditorHandle>(null);
	const [initial] = useState(() => readDraft(draftId));
	const [empty, setEmpty] = useState(() => isEmptyMessage(initial));
	const [tooLong, setTooLong] = useState(false);
	const [sending, setSending] = useState(false);
	const [sendError, setSendError] = useState<string | null>(null);
	const commands = useSlashCommands({
		projectId: mentionProjectId,
		post: onSend,
		extra: extraCommands,
	});
	const describeError = useCallback(
		(err: unknown) => t(`errors.${chatErrorKey(err)}`),
		[t],
	);
	const runner = useCommandRunner(commands, describeError);
	const error = sendError ?? runner.error;

	const onTypingRef = useRef(onTyping);
	onTypingRef.current = onTyping;
	// A different channel or thread is a different draft (the editor is keyed).
	// biome-ignore lint/correctness/useExhaustiveDependencies: reset on draft change only
	useEffect(() => {
		setSendError(null);
		runner.clearError();
	}, [draftId]);

	// The latest props, for the editor's key handler which outlives a render.
	const live = useRef({ commands, onSend, draftId, sending });
	live.current = { commands, onSend, draftId, sending };

	const changed = (blocks: ChatBlock[]) => {
		const nowEmpty = isEmptyMessage(blocks);
		setEmpty(nowEmpty);
		setTooLong(blocksToText(blocks).length > MAX_MESSAGE_LENGTH);
		writeDraft(live.current.draftId, blocks);
		onTypingRef.current?.(!nowEmpty);
	};

	const clear = () => {
		editor.current?.clear();
		writeDraft(live.current.draftId, []);
	};

	const runCommand = async (c: SlashCommand, args: string) => {
		setSending(true);
		try {
			if (await runner.run(c, args)) clear();
		} finally {
			setSending(false);
			editor.current?.focus();
		}
	};

	const submit = async () => {
		const { onSend: send, sending: busy } = live.current;
		const blocks = (editor.current?.getBlocks() ?? []) as ChatBlock[];
		if (busy || isEmptyMessage(blocks)) return;
		if (blocksToText(blocks).length > MAX_MESSAGE_LENGTH) return;
		const hit = findCommand(live.current.commands, blocks);
		if (hit) {
			await runCommand(hit.command, hit.args);
			return;
		}
		setSending(true);
		setSendError(null);
		try {
			await send(prepareForSend(blocks));
			clear();
		} catch (err) {
			setSendError(describeError(err));
		} finally {
			setSending(false);
			editor.current?.focus();
		}
	};

	if (disabledReason) {
		return (
			<p className="border-t px-4 py-3 text-center text-sm text-muted-foreground">
				{disabledReason}
			</p>
		);
	}

	const hasCommands = !!commands && commands.length > 0;
	return (
		<div className="relative border-t p-3">
			<div className="flex items-end gap-2">
				<MessageEditor
					key={draftId}
					ref={editor}
					placeholder={placeholder}
					projectId={mentionProjectId}
					commands={commands}
					submitKey="enter"
					initialBlocks={initial}
					autoFocus={autoFocus}
					onChange={(blocks) => changed(blocks as ChatBlock[])}
					onSubmit={() => void submit()}
					onCommand={(c) => void runCommand(c, "")}
				/>
				<Button
					type="button"
					size="icon"
					aria-label={t("composer.send")}
					disabled={empty || tooLong || sending}
					onClick={() => void submit()}
				>
					<SendHorizontal />
				</Button>
			</div>
			<p
				className={
					tooLong || error
						? "mt-1 text-xs text-destructive"
						: "mt-1 hidden text-xs text-muted-foreground md:block"
				}
				role={tooLong || error ? "alert" : undefined}
			>
				{tooLong
					? t("composer.tooLong")
					: (error ??
						(hasCommands ? t("composer.hintCommands") : t("composer.hint")))}
			</p>
		</div>
	);
}
