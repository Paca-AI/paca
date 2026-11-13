"use client";

import {
	ArrowUpIcon,
	CheckIcon,
	CopyIcon,
	DownloadIcon,
	MoreHorizontalIcon,
	SquareIcon,
} from "lucide-react";
import {
	type ComponentType,
	type FC,
	type KeyboardEvent,
	type ReactNode,
	useCallback,
	useEffect,
	useMemo,
	useRef,
	useState,
} from "react";
import { useTranslation } from "react-i18next";
import { ChatProvider } from "@/components/chat/chat-context";
import {
	ContextInjectionRow,
	ContextItemReadOnlyRow,
} from "@/components/chat/context-injection";
import { MarkdownText } from "@/components/chat/markdown-text";
import { ReasoningGroup } from "@/components/chat/reasoning";
import { ToolFallback } from "@/components/chat/tool-fallback";
import { ToolGroup } from "@/components/chat/tool-group";
import { TooltipIconButton } from "@/components/chat/tooltip-icon-button";
import {
	type ChatMessage,
	type ChatPart,
	type ChatToolCallPart,
	toolCallStatus,
} from "@/components/chat/types";
import { Bubble, BubbleContent } from "@/components/ui/bubble";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
	InputGroup,
	InputGroupAddon,
	InputGroupTextarea,
} from "@/components/ui/input-group";
import { Message, MessageContent } from "@/components/ui/message";
import {
	MessageScroller,
	MessageScrollerButton,
	MessageScrollerContent,
	MessageScrollerItem,
	MessageScrollerProvider,
	MessageScrollerViewport,
} from "@/components/ui/message-scroller";
import { Spinner } from "@/components/ui/spinner";
import { getAnnotation } from "@/lib/annotation-api";
import { matchAnnotationLinkOnly } from "@/lib/annotation-link";
import {
	isAskQuestionTool,
	parseAskQuestionFromCall,
} from "@/lib/ask-question";
import { copyToClipboard } from "@/lib/clipboard";
import {
	composerDraftKey,
	loadComposerDraft,
	saveComposerDraft,
} from "@/lib/composer-draft";
import { useContextInjectionStore } from "@/lib/context-injection-store";
import { parseContextItems } from "@/lib/context-items";
import { excerptOf } from "@/lib/mention-api";
import { cn } from "@/lib/utils";

/** Optional slot overrides for the thread. */
export type ThreadComponents = {
	Welcome?: ComponentType | undefined;
	/** Rendered at the start of the composer's action row (left of the send
	 * control) — e.g. an agent picker shown inline while starting a new chat. */
	ComposerStart?: ComponentType | undefined;
};

export type ThreadProps = {
	messages: readonly ChatMessage[];
	/** The agent is mid-turn: shows the working indicator and a stop button. */
	isRunning: boolean;
	/** No composer at all (the viewer cannot reply). */
	disabled?: boolean | undefined;
	/** The composer is shown but sending is blocked. */
	sendDisabled?: boolean | undefined;
	/** Sends the typed text as the user's next message. */
	onSend: (text: string) => Promise<void> | void;
	onCancel?: (() => Promise<void> | void) | undefined;
	components?: ThreadComponents | undefined;
	/** Rendered inside the viewport, above the messages. */
	viewportHeader?: ReactNode | undefined;
	/** Rendered inside the viewport, below the messages. */
	viewportOverlay?: ReactNode | undefined;
	/**
	 * "bottom" gives classic chat behaviour: opens on the newest message and
	 * stays pinned there while the reader is at the bottom. "top" anchors each
	 * new user message at the top of the viewport.
	 */
	turnAnchor?: "top" | "bottom" | undefined;
	/**
	 * Whether the sandbox/session backing this thread is confirmed ready to
	 * run a turn. Governs the empty-message indicator's wording: "setting up
	 * your environment" beforehand, "thinking" after. Defaults to `false`
	 * (the more conservative reading).
	 */
	environmentReady?: boolean | undefined;
};

export const Thread: FC<ThreadProps> = ({
	messages,
	isRunning,
	disabled = false,
	sendDisabled = false,
	onSend,
	onCancel,
	components,
	viewportHeader,
	viewportOverlay,
	turnAnchor = "top",
	environmentReady = false,
}) => {
	const { Welcome = ThreadWelcome, ComposerStart } = components ?? {};
	const isEmpty = messages.length === 0;

	// A run that has started but produced nothing assistant-authored yet
	// (queued conversation, sandbox still spinning up) still needs a message to
	// hang the "working" indicator on.
	const needsPlaceholder =
		isRunning && messages[messages.length - 1]?.role !== "assistant";

	const lastMessageId = messages[messages.length - 1]?.id;
	const chat = useMemo(
		() => ({ isRunning, lastMessageId, send: onSend }),
		[isRunning, lastMessageId, onSend],
	);

	return (
		<ChatProvider value={chat}>
			<MessageScrollerProvider
				defaultScrollPosition={turnAnchor === "top" ? "last-anchor" : "end"}
				// "bottom" is classic chat behaviour: stay pinned to the tail while
				// the reader is there. Without autoScroll the scroller never follows
				// content that arrives after a one-shot scrollToEnd (a sent message
				// and its reply land later), leaving the viewport behind.
				autoScroll={turnAnchor === "bottom"}
			>
				<div
					className="bg-background @container flex h-full flex-col text-sm"
					style={{ ["--thread-max-width" as string]: "44rem" }}
				>
					<MessageScroller className="flex-1">
						<MessageScrollerViewport preserveScrollOnPrepend>
							<MessageScrollerContent
								className={cn(
									"mx-auto w-full max-w-(--thread-max-width) px-4",
									isEmpty ? "justify-center" : "pt-4",
								)}
							>
								{isEmpty && <Welcome />}
								{viewportHeader}
								{messages.map((message, index) => (
									<MessageScrollerItem
										key={message.id}
										messageId={message.id}
										scrollAnchor={message.role === "user"}
									>
										{message.role === "user" ? (
											<UserMessage message={message} />
										) : (
											<AssistantMessage
												message={message}
												isLast={index === messages.length - 1}
												environmentReady={environmentReady}
											/>
										)}
									</MessageScrollerItem>
								))}
								{needsPlaceholder && (
									<MessageScrollerItem messageId="__pending__">
										<AssistantMessage
											message={PENDING_MESSAGE}
											isLast
											environmentReady={environmentReady}
										/>
									</MessageScrollerItem>
								)}
								{viewportOverlay}
							</MessageScrollerContent>
						</MessageScrollerViewport>
						<ThreadScrollToBottom />
					</MessageScroller>

					{!disabled && (
						<div
							className={cn(
								"mx-auto w-full max-w-(--thread-max-width) px-4 pt-2 pb-4",
							)}
						>
							<Composer
								isRunning={isRunning}
								sendDisabled={sendDisabled}
								onSend={onSend}
								onCancel={onCancel}
								ComposerStart={ComposerStart}
							/>
						</div>
					)}
				</div>
			</MessageScrollerProvider>
		</ChatProvider>
	);
};

const PENDING_MESSAGE: ChatMessage = {
	id: "__pending__",
	role: "assistant",
	status: "running",
	content: [],
};

const ThreadScrollToBottom: FC = () => {
	const { t } = useTranslation("projects");
	return (
		<MessageScrollerButton
			direction="end"
			size="sm"
			className="rounded-full text-xs shadow-md"
		>
			<span>{t("agents.thread.scrollToBottom")}</span>
		</MessageScrollerButton>
	);
};

const ThreadWelcome: FC = () => {
	const { t } = useTranslation("projects");
	return (
		<div className="mb-6 flex flex-col items-center px-4 text-center">
			<h1 className="fade-in slide-in-from-bottom-1 animate-in fill-mode-both text-2xl font-semibold duration-200">
				{t("agents.thread.welcomeHeading")}
			</h1>
		</div>
	);
};

// ── Composer ────────────────────────────────────────────────────────────────

/** Recognizes a comment-detail-page link (copied via the extension's pin
 * popover or the comment detail page's own Copy button — see
 * lib/annotation-link.ts) pasted into the message box and attaches it as
 * context instead of letting the raw URL be typed in.
 *
 * Uses the projectId parsed out of the pasted link itself, not the
 * currently open project — a pasted link may point to a different project,
 * and the global chat surface has no project route param at all. If the
 * background fetch fails (annotation deleted, network error), the paste is
 * simply a no-op rather than falling back to inserting the raw text.
 *
 * Uses matchAnnotationLinkOnly, not a substring search: preventDefault()
 * discards the entire pasted text in favor of attaching context, so this
 * must only fire when the paste is nothing but the link — otherwise text
 * around the link would be silently dropped. */
function handleComposerPaste(
	event: React.ClipboardEvent<HTMLTextAreaElement>,
): void {
	const text = event.clipboardData.getData("text/plain");
	const match = matchAnnotationLinkOnly(text);
	if (!match) return;
	event.preventDefault();
	getAnnotation(
		match.projectId,
		match.environmentId,
		match.portForwardId,
		match.annotationId,
	)
		.then((annotation) => {
			useContextInjectionStore.getState().add({
				type: "annotation",
				id: annotation.id,
				title: excerptOf(annotation.body),
				projectId: match.projectId,
			});
		})
		.catch(() => {
			// Nothing to recover here -- see doc comment above.
		});
}

/**
 * Keeps whatever is typed in the composer until it is sent, so a reload the
 * app cannot avoid — an expired session bouncing through the login/SSO flow
 * and back to the same URL — restores the message instead of throwing away
 * work that was never submitted (see lib/composer-draft.ts for the storage).
 * Attached context chips are not restored: that store is memory-only.
 */
function useComposerDraft(text: string, setText: (text: string) => void) {
	// Fixed at mount: a composer that outlives an SPA navigation (the project
	// float, a reused conversation view) keeps writing to the page it was
	// mounted on, rather than copying its live text onto every page it passes.
	const key = useRef(composerDraftKey(window.location.pathname)).current;
	// Restoring must happen before the first persist, or an empty first render
	// deletes the very draft it is meant to bring back.
	const restored = useRef(false);

	useEffect(() => {
		if (!restored.current) {
			restored.current = true;
			const saved = loadComposerDraft(key);
			if (saved && !text) {
				setText(saved);
				return;
			}
		}
		saveComposerDraft(key, text);
	}, [key, text, setText]);
}

const Composer: FC<{
	isRunning: boolean;
	sendDisabled: boolean;
	onSend: (text: string) => Promise<void> | void;
	onCancel: (() => Promise<void> | void) | undefined;
	ComposerStart: ComponentType | undefined;
}> = ({ isRunning, sendDisabled, onSend, onCancel, ComposerStart }) => {
	const { t } = useTranslation("projects");
	const [text, setText] = useState("");
	useComposerDraft(text, setText);

	const canSend = text.trim().length > 0 && !sendDisabled;

	const submit = useCallback(() => {
		const value = text.trim();
		if (!value || sendDisabled) return;
		setText("");
		// A rejected send is the caller's to report (each surface renders its own
		// error box); swallowing here keeps it from becoming an unhandled
		// rejection.
		void Promise.resolve(onSend(value)).catch(() => {});
	}, [text, sendDisabled, onSend]);

	const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
		// Never send mid-IME-composition (Enter confirms the candidate).
		if (e.nativeEvent.isComposing || e.key !== "Enter" || e.shiftKey) return;
		e.preventDefault();
		if (!isRunning) submit();
	};

	return (
		<div className="flex w-full flex-col gap-2">
			<ContextInjectionRow />
			<InputGroup className="rounded-xl bg-muted/30 shadow-sm dark:bg-muted/30">
				<InputGroupTextarea
					value={text}
					onChange={(e) => setText(e.target.value)}
					onKeyDown={onKeyDown}
					onPaste={handleComposerPaste}
					placeholder={t("agents.thread.composerPlaceholder")}
					aria-label={t("agents.thread.messageInputAriaLabel")}
					className="max-h-32 min-h-10 px-3 py-2.5 text-sm"
					rows={1}
					autoFocus
					enterKeyHint="send"
				/>
				<InputGroupAddon align="block-end" className="justify-between">
					{ComposerStart ? <ComposerStart /> : <div />}
					{isRunning ? (
						<Button
							type="button"
							size="icon-sm"
							className="rounded-full"
							aria-label={t("agents.thread.stopGeneratingAriaLabel")}
							onClick={() => void onCancel?.()}
							disabled={!onCancel}
						>
							<SquareIcon className="fill-current" />
						</Button>
					) : (
						<TooltipIconButton
							tooltip={t("agents.thread.sendMessage")}
							side="bottom"
							type="button"
							size="icon-sm"
							className="rounded-full"
							aria-label={t("agents.thread.sendMessage")}
							onClick={submit}
							disabled={!canSend}
						>
							<ArrowUpIcon />
						</TooltipIconButton>
					)}
				</InputGroupAddon>
			</InputGroup>
		</div>
	);
};

// ── Messages ────────────────────────────────────────────────────────────────

const UserMessage: FC<{ message: ChatMessage }> = ({ message }) => {
	const contextItems = useMemo(
		() => parseContextItems(message.metadata?.custom?.contextItems),
		[message.metadata],
	);
	const text = message.content
		.filter((p): p is Extract<ChatPart, { type: "text" }> => p.type === "text")
		.map((p) => p.text)
		.join("\n");

	return (
		<Message
			align="end"
			data-role="user"
			className="fade-in slide-in-from-bottom-1 animate-in duration-150"
		>
			<MessageContent>
				<ContextItemReadOnlyRow items={contextItems} />
				{text && (
					<Bubble align="end" variant="muted">
						<BubbleContent className="whitespace-pre-wrap">
							{text}
						</BubbleContent>
					</Bubble>
				)}
			</MessageContent>
		</Message>
	);
};

type PartBlock =
	| { kind: "text"; text: string; key: string }
	| { kind: "reasoning"; texts: string[]; key: string; streaming: boolean }
	| {
			kind: "tools";
			parts: ChatToolCallPart[];
			key: string;
			running: boolean;
	  }
	| { kind: "ask"; part: ChatToolCallPart; key: string };

/** An ask_user_question call is a prompt for the user, not background work: it
 * stays outside the collapsible tool group so its answer buttons are visible
 * without expanding anything. */
function isAskPart(part: ChatToolCallPart): boolean {
	return (
		isAskQuestionTool(part.toolName) &&
		parseAskQuestionFromCall(part.argsText, part.result) !== null
	);
}

/** Folds consecutive reasoning parts and consecutive tool calls into groups. */
function groupParts(
	parts: readonly ChatPart[],
	messageRunning: boolean,
): PartBlock[] {
	const blocks: PartBlock[] = [];
	parts.forEach((part, i) => {
		const isTail = i === parts.length - 1;
		const last = blocks[blocks.length - 1];
		if (part.type === "text") {
			blocks.push({ kind: "text", text: part.text, key: `t${i}` });
		} else if (part.type === "reasoning") {
			if (last?.kind === "reasoning") {
				last.texts.push(part.text);
				last.streaming = messageRunning && isTail;
			} else {
				blocks.push({
					kind: "reasoning",
					texts: [part.text],
					key: `r${i}`,
					streaming: messageRunning && isTail,
				});
			}
		} else if (isAskPart(part)) {
			blocks.push({ kind: "ask", part, key: part.toolCallId });
		} else if (last?.kind === "tools") {
			last.parts.push(part);
			last.running ||= toolCallStatus(part, messageRunning).type === "running";
		} else {
			blocks.push({
				kind: "tools",
				parts: [part],
				key: `g${part.toolCallId}`,
				running: toolCallStatus(part, messageRunning).type === "running",
			});
		}
	});
	return blocks;
}

function messageText(message: ChatMessage): string {
	return message.content
		.filter((p): p is Extract<ChatPart, { type: "text" }> => p.type === "text")
		.map((p) => p.text)
		.join("\n\n");
}

const AssistantMessage: FC<{
	message: ChatMessage;
	isLast: boolean;
	environmentReady: boolean;
}> = ({ message, isLast, environmentReady }) => {
	const { t } = useTranslation("projects");
	const running = message.status === "running";
	const blocks = useMemo(
		() => groupParts(message.content, running),
		[message.content, running],
	);
	const hasNoParts = message.content.length === 0;
	const text = messageText(message);

	return (
		<Message
			data-role="assistant"
			className="fade-in slide-in-from-bottom-1 animate-in duration-150"
		>
			<MessageContent className="px-2 leading-relaxed">
				{blocks.map((block) => {
					switch (block.kind) {
						case "text":
							return <MarkdownText key={block.key} text={block.text} />;
						case "reasoning":
							return (
								<ReasoningGroup
									key={block.key}
									texts={block.texts}
									streaming={block.streaming}
								/>
							);
						case "ask":
							return (
								<ToolFallback
									key={block.key}
									part={block.part}
									status={toolCallStatus(block.part, running)}
									isLastMessage={isLast}
								/>
							);
						case "tools":
							return (
								<ToolGroup
									key={block.key}
									count={block.parts.length}
									active={block.running}
								>
									{block.parts.map((part) => (
										<ToolFallback
											key={part.toolCallId}
											part={part}
											status={toolCallStatus(part, running)}
											isLastMessage={isLast}
										/>
									))}
								</ToolGroup>
							);
						default:
							return null;
					}
				})}
				{running &&
					(hasNoParts ? (
						<span
							role="status"
							className="inline-flex animate-pulse items-center gap-2 text-muted-foreground"
						>
							<Spinner aria-hidden="true" className="size-3.5" />
							{environmentReady
								? t("agents.thread.thinking")
								: t("agents.thread.environmentInitializing")}
						</span>
					) : (
						<span
							role="status"
							className="animate-pulse font-sans"
							aria-label={t("agents.thread.assistantWorkingAriaLabel")}
						>
							{"●"}
						</span>
					))}
				{!running && text && <AssistantActions text={text} isLast={isLast} />}
			</MessageContent>
		</Message>
	);
};

const AssistantActions: FC<{ text: string; isLast: boolean }> = ({
	text,
	isLast,
}) => {
	const { t } = useTranslation("projects");
	const [copied, setCopied] = useState(false);

	// The clipboard API is undefined on non-secure (plain http) origins;
	// copyToClipboard falls back to execCommand.
	const handleCopy = () => {
		void copyToClipboard(text).then((ok) => {
			if (!ok) return;
			setCopied(true);
			setTimeout(() => setCopied(false), 3000);
		});
	};

	const handleExport = () => {
		const url = URL.createObjectURL(
			new Blob([text], { type: "text/markdown;charset=utf-8" }),
		);
		const a = document.createElement("a");
		a.href = url;
		a.download = "message.md";
		a.click();
		URL.revokeObjectURL(url);
	};

	return (
		<div
			className={cn(
				"-ms-1 flex items-center gap-1 text-muted-foreground transition-opacity",
				// Older messages reveal their actions on hover/focus only.
				!isLast &&
					"opacity-0 focus-within:opacity-100 group-hover/message:opacity-100",
			)}
		>
			<TooltipIconButton tooltip={t("agents.thread.copy")} onClick={handleCopy}>
				{copied ? (
					<CheckIcon className="animate-in zoom-in-50 fade-in duration-200 ease-out" />
				) : (
					<CopyIcon />
				)}
			</TooltipIconButton>
			<DropdownMenu>
				<DropdownMenuTrigger
					render={
						<TooltipIconButton
							tooltip={t("agents.thread.more")}
							className="data-[popup-open]:bg-accent"
						/>
					}
				>
					<MoreHorizontalIcon />
				</DropdownMenuTrigger>
				<DropdownMenuContent side="bottom" align="start" sideOffset={6}>
					<DropdownMenuItem onClick={handleExport}>
						<DownloadIcon />
						{t("agents.thread.exportAsMarkdown")}
					</DropdownMenuItem>
				</DropdownMenuContent>
			</DropdownMenu>
		</div>
	);
};
