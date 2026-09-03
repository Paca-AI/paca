"use client";

import {
	ActionBarMorePrimitive,
	ActionBarPrimitive,
	type AssistantState,
	AuiIf,
	BranchPickerPrimitive,
	ComposerPrimitive,
	ErrorPrimitive,
	groupPartByType,
	MessagePrimitive,
	SuggestionPrimitive,
	ThreadPrimitive,
	type ToolCallMessagePartComponent,
	useAuiState,
} from "@assistant-ui/react";
import {
	ArrowDownIcon,
	ArrowUpIcon,
	CheckIcon,
	ChevronLeftIcon,
	ChevronRightIcon,
	CopyIcon,
	DownloadIcon,
	MicIcon,
	MoreHorizontalIcon,
	PencilIcon,
	RefreshCwIcon,
	SquareIcon,
} from "lucide-react";
import {
	type ComponentType,
	createContext,
	type FC,
	type PropsWithChildren,
	type ReactNode,
	useContext,
	useMemo,
} from "react";
import { useTranslation } from "react-i18next";
import { UserMessageAttachments } from "@/components/assistant-ui/attachment";
import {
	ContextInjectionRow,
	ContextItemReadOnlyRow,
} from "@/components/assistant-ui/context-injection";
import { ThreadFollowupSuggestions } from "@/components/assistant-ui/follow-up-suggestions";
import { MarkdownText } from "@/components/assistant-ui/markdown-text";
import {
	Reasoning,
	ReasoningContent,
	ReasoningRoot,
	ReasoningText,
	ReasoningTrigger,
} from "@/components/assistant-ui/reasoning";
import { ToolFallback } from "@/components/assistant-ui/tool-fallback";
import {
	ToolGroupContent,
	ToolGroupRoot,
	ToolGroupTrigger,
} from "@/components/assistant-ui/tool-group";
import { TooltipIconButton } from "@/components/assistant-ui/tooltip-icon-button";
import { Button } from "@/components/ui/button";
import { getAnnotation } from "@/lib/annotation-api";
import { matchAnnotationLinkOnly } from "@/lib/annotation-link";
import { useContextInjectionStore } from "@/lib/context-injection-store";
import { parseContextItems } from "@/lib/context-items";
import { excerptOf } from "@/lib/mention-api";
import { cn } from "@/lib/utils";

export type ThreadGroupPart = MessagePrimitive.GroupedParts.GroupPart;

/**
 * Optional component overrides for the thread. `AssistantMessage` and
 * `Welcome` replace whole sections; the remaining slots override how the
 * assistant message renders tool calls and part groups. Tool UIs registered
 * by name (toolkit `render`, `useAssistantDataUI`) take precedence over
 * `ToolFallback`.
 */
export type ThreadComponents = {
	AssistantMessage?: ComponentType | undefined;
	Welcome?: ComponentType | undefined;
	/** Rendered at the start of the composer's action row (left of the mic/send
	 * controls) — e.g. an agent picker shown inline while starting a new chat. */
	ComposerStart?: ComponentType | undefined;
	ToolFallback?: ToolCallMessagePartComponent | undefined;
	ToolGroup?:
		| ComponentType<PropsWithChildren<{ group: ThreadGroupPart }>>
		| undefined;
	ReasoningGroup?:
		| ComponentType<PropsWithChildren<{ group: ThreadGroupPart }>>
		| undefined;
};

export type ThreadProps = {
	components?: ThreadComponents | undefined;
	/** Rendered inside the viewport, above the messages. */
	viewportHeader?: ReactNode | undefined;
	/** Rendered inside the viewport, below the messages. */
	viewportOverlay?: ReactNode | undefined;
	/**
	 * "bottom" gives classic chat behaviour: opens on the newest message and
	 * stays pinned there while the reader is at the bottom. "top" anchors each
	 * new user message at the top of the viewport and disables auto-scroll.
	 */
	turnAnchor?: "top" | "bottom" | undefined;
	/**
	 * Whether a starting run scrolls to the newest message. Queues a scroll the
	 * viewport re-applies on every content resize until it is cleared, which
	 * overrides a caller that is positioning the scroll itself.
	 */
	scrollToBottomOnRunStart?: boolean | undefined;
	/**
	 * Whether the sandbox/session backing this thread is confirmed ready to
	 * run a turn (a fresh sandbox finished starting, an ACP local-bridge
	 * conversation — which has no sandbox to start — or a resumed session).
	 * Governs the empty-message indicator's wording: "setting up your
	 * environment" beforehand, "thinking" after. Callers derive this from
	 * `hasEnvironmentReadyEvent`/`isACP`; defaults to `false` (the more
	 * conservative "still setting up" reading) so a caller that hasn't
	 * wired this up yet doesn't claim readiness it can't back up.
	 */
	environmentReady?: boolean | undefined;
};

const EMPTY_COMPONENTS: ThreadComponents = {};

const ThreadComponentsContext =
	createContext<ThreadComponents>(EMPTY_COMPONENTS);
const ThreadStatusContext = createContext<{ environmentReady: boolean }>({
	environmentReady: false,
});

// Startup exposes a loading placeholder thread; treat it as a new chat so
// the composer mounts centered. Loads after startup keep the docked layout.
const isNewChatView = (s: AssistantState) =>
	s.thread.messages.length === 0 &&
	(!s.thread.isLoading || s.threads.isLoading);

export const Thread: FC<ThreadProps> = ({
	components = EMPTY_COMPONENTS,
	viewportHeader,
	viewportOverlay,
	turnAnchor = "top",
	scrollToBottomOnRunStart = true,
	environmentReady = false,
}) => {
	const isEmpty = useAuiState(isNewChatView);

	return (
		<ThreadComponentsContext.Provider value={components}>
			<ThreadStatusContext.Provider value={{ environmentReady }}>
				<ThreadRoot
					isEmpty={isEmpty}
					viewportHeader={viewportHeader}
					viewportOverlay={viewportOverlay}
					turnAnchor={turnAnchor}
					scrollToBottomOnRunStart={scrollToBottomOnRunStart}
				/>
			</ThreadStatusContext.Provider>
		</ThreadComponentsContext.Provider>
	);
};

const ThreadRoot: FC<{
	isEmpty: boolean;
	viewportHeader?: ReactNode | undefined;
	viewportOverlay?: ReactNode | undefined;
	turnAnchor: "top" | "bottom";
	scrollToBottomOnRunStart: boolean;
}> = ({
	isEmpty,
	viewportHeader,
	viewportOverlay,
	turnAnchor,
	scrollToBottomOnRunStart,
}) => {
	const { Welcome = ThreadWelcome } = useContext(ThreadComponentsContext);

	return (
		<ThreadPrimitive.Root
			className="aui-root aui-thread-root bg-background @container flex h-full flex-col text-sm"
			style={{
				["--thread-max-width" as string]: "44rem",
				["--composer-bg" as string]:
					"color-mix(in oklab, var(--color-muted) 30%, var(--color-background))",
				["--composer-radius" as string]: "var(--radius-xl)",
				["--composer-padding" as string]: "8px",
			}}
		>
			<ThreadPrimitive.Viewport
				turnAnchor={turnAnchor}
				scrollToBottomOnRunStart={scrollToBottomOnRunStart}
				data-slot="aui_thread-viewport"
				className="relative flex flex-1 flex-col overflow-x-auto overflow-y-scroll scroll-smooth [scrollbar-gutter:stable]"
			>
				<div
					className={cn(
						"mx-auto flex w-full max-w-(--thread-max-width) flex-1 flex-col pl-4 pr-1 pt-4",
						isEmpty && "justify-center",
					)}
				>
					<AuiIf condition={isNewChatView}>
						<Welcome />
					</AuiIf>

					{viewportHeader}

					<div
						data-slot="aui_message-group"
						className="mb-14 flex flex-col gap-y-6 empty:hidden"
					>
						<ThreadPrimitive.Messages>
							{() => <ThreadMessage />}
						</ThreadPrimitive.Messages>
					</div>

					{viewportOverlay}

					<ThreadPrimitive.ViewportFooter
						className={cn(
							"aui-thread-viewport-footer bg-background flex flex-col overflow-visible pb-4",
							!isEmpty &&
								"sticky bottom-0 mt-auto rounded-t-(--composer-radius)",
						)}
					>
						<ThreadScrollToBottom />
						<ThreadFollowupSuggestions />
						<AuiIf condition={(s) => !s.thread.isDisabled}>
							<Composer />
						</AuiIf>
						<AuiIf condition={(s) => isNewChatView(s) && s.composer.isEmpty}>
							<ThreadSuggestions />
						</AuiIf>
					</ThreadPrimitive.ViewportFooter>
				</div>
			</ThreadPrimitive.Viewport>
		</ThreadPrimitive.Root>
	);
};

const ThreadMessage: FC = () => {
	const { AssistantMessage: AssistantMessageComponent = AssistantMessage } =
		useContext(ThreadComponentsContext);
	const role = useAuiState((s) => s.message.role);
	const isEditing = useAuiState((s) => s.message.composer.isEditing);

	if (isEditing) return <EditComposer />;
	if (role === "user") return <UserMessage />;
	return <AssistantMessageComponent />;
};

const ThreadScrollToBottom: FC = () => {
	const { t } = useTranslation("projects");
	return (
		<ThreadPrimitive.ScrollToBottom
			render={
				<TooltipIconButton
					tooltip={t("agents.thread.scrollToBottom")}
					variant="outline"
					className="aui-thread-scroll-to-bottom dark:border-border dark:bg-background dark:hover:bg-accent absolute -top-12 z-10 self-center rounded-full p-4 disabled:invisible"
				/>
			}
		>
			<ArrowDownIcon />
		</ThreadPrimitive.ScrollToBottom>
	);
};

const ThreadWelcome: FC = () => {
	const { t } = useTranslation("projects");
	return (
		<div className="aui-thread-welcome-root mb-6 flex flex-col items-center px-4 text-center">
			<h1 className="aui-thread-welcome-message-inner fade-in slide-in-from-bottom-1 animate-in fill-mode-both text-2xl font-semibold duration-200">
				{t("agents.thread.welcomeHeading")}
			</h1>
		</div>
	);
};

const ThreadSuggestions: FC = () => {
	return (
		<div className="aui-thread-welcome-suggestions flex w-full flex-wrap items-center justify-center gap-2 px-4">
			<ThreadPrimitive.Suggestions>
				{() => <ThreadSuggestionItem />}
			</ThreadPrimitive.Suggestions>
		</div>
	);
};

const ThreadSuggestionItem: FC = () => {
	return (
		<div className="aui-thread-welcome-suggestion-display fade-in slide-in-from-bottom-2 animate-in fill-mode-both duration-200">
			<SuggestionPrimitive.Trigger
				send
				render={
					<Button
						variant="ghost"
						className="aui-thread-welcome-suggestion text-foreground hover:bg-muted border-border/60 h-auto gap-1.5 rounded-full border px-3.5 py-1.5 text-sm font-normal whitespace-nowrap transition-colors"
					/>
				}
			>
				<SuggestionPrimitive.Title className="aui-thread-welcome-suggestion-text-1" />
				<SuggestionPrimitive.Description className="aui-thread-welcome-suggestion-text-2 empty:hidden" />
			</SuggestionPrimitive.Trigger>
		</div>
	);
};

/** Recognizes a comment-detail-page link (copied via the extension's pin
 * popover or the comment detail page's own Copy button — see
 * lib/annotation-link.ts) pasted into the message box and attaches it as
 * context instead of letting the raw URL be typed in. A plain `onPaste` prop
 * works here with no wrapping: ComposerPrimitive.Input composes a
 * caller-supplied onPaste with its own default paste handling via
 * @radix-ui/primitive's composeEventHandlers, running the caller's handler
 * first and only falling through to the default behavior (e.g. file-paste
 * attachments) if the event is still not defaultPrevented — so
 * preventDefault() here doesn't affect any other paste behavior.
 *
 * Uses the projectId parsed out of the pasted link itself, not the
 * currently open project — a pasted link may point to a different project,
 * and the global chat surface has no project route param at all. If the
 * background fetch fails (annotation deleted, network error), the paste is
 * simply a no-op rather than falling back to inserting the raw text — this
 * primitive's controlled value isn't something a plain event handler can
 * splice text into after the fact.
 *
 * Uses matchAnnotationLinkOnly, not matchAnnotationLink's plain substring
 * search: preventDefault() below discards the entire pasted text in favor
 * of attaching context instead, so this must only fire when the paste is
 * nothing but the link — otherwise a paste that also carries other text
 * around the link (e.g. "see this: <link> — thanks") would silently drop
 * that surrounding text instead of inserting it. */
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

const Composer: FC = () => {
	const { t } = useTranslation("projects");
	return (
		<ComposerPrimitive.Root className="aui-composer-root relative flex w-full flex-col">
			{/* No AttachmentDropzone/ComposerAttachments here — onNew only
			 * accepts a single text part (see conversation-view.tsx /
			 * ai-chat-float.tsx), so accepting dropped files here would be a
			 * dead end: the composer would show them as attached, then send
			 * would reject the message. */}
			<div
				data-slot="aui_composer-shell"
				className="border-border/60 focus-within:border-border dark:border-muted-foreground/15 dark:focus-within:border-muted-foreground/30 flex w-full flex-col gap-2 rounded-(--composer-radius) border bg-(--composer-bg) p-(--composer-padding) shadow-[0_4px_16px_-8px_rgba(0,0,0,0.08),0_1px_2px_rgba(0,0,0,0.04)] transition-[border-color,box-shadow] focus-within:shadow-[0_6px_24px_-8px_rgba(0,0,0,0.12),0_1px_2px_rgba(0,0,0,0.05)] dark:shadow-none"
			>
				<ContextInjectionRow />
				<ComposerPrimitive.Input
					placeholder={t("agents.thread.composerPlaceholder")}
					className="aui-composer-input caret-primary placeholder:text-muted-foreground/80 max-h-32 min-h-10 w-full resize-none bg-transparent px-2.5 py-1 text-sm outline-none"
					rows={1}
					autoFocus
					enterKeyHint="send"
					aria-label={t("agents.thread.messageInputAriaLabel")}
					onPaste={handleComposerPaste}
				/>
				<ComposerAction />
			</div>
		</ComposerPrimitive.Root>
	);
};

const ComposerAction: FC = () => {
	const { t } = useTranslation("projects");
	const { ComposerStart } = useContext(ThreadComponentsContext);
	return (
		<div className="aui-composer-action-wrapper relative flex items-center justify-between gap-2">
			{ComposerStart ? <ComposerStart /> : <div />}
			<div className="flex items-center gap-1.5">
				<AuiIf condition={(s) => s.thread.capabilities.dictation}>
					<AuiIf condition={(s) => s.composer.dictation == null}>
						<ComposerPrimitive.Dictate
							render={
								<TooltipIconButton
									tooltip={t("agents.thread.voiceInput")}
									side="bottom"
									type="button"
									variant="ghost"
									size="icon"
									className="aui-composer-dictate size-7 rounded-full"
									aria-label={t("agents.thread.startVoiceInputAriaLabel")}
								/>
							}
						>
							<MicIcon className="aui-composer-dictate-icon size-4" />
						</ComposerPrimitive.Dictate>
					</AuiIf>
					<AuiIf condition={(s) => s.composer.dictation != null}>
						<ComposerPrimitive.StopDictation
							render={
								<TooltipIconButton
									tooltip={t("agents.thread.stopDictation")}
									side="bottom"
									type="button"
									variant="ghost"
									size="icon"
									className="aui-composer-stop-dictation text-destructive size-7 rounded-full"
									aria-label={t("agents.thread.stopVoiceInputAriaLabel")}
								/>
							}
						>
							<SquareIcon className="aui-composer-stop-dictation-icon size-3.5 animate-pulse fill-current" />
						</ComposerPrimitive.StopDictation>
					</AuiIf>
				</AuiIf>
				<AuiIf condition={(s) => !s.thread.isRunning}>
					<ComposerPrimitive.Send
						render={
							<TooltipIconButton
								tooltip={t("agents.thread.sendMessage")}
								side="bottom"
								type="button"
								variant="default"
								size="icon"
								className="aui-composer-send size-7 rounded-full"
								aria-label={t("agents.thread.sendMessage")}
							/>
						}
					>
						<ArrowUpIcon className="aui-composer-send-icon size-4.5" />
					</ComposerPrimitive.Send>
				</AuiIf>
				<AuiIf condition={(s) => s.thread.isRunning}>
					<ComposerPrimitive.Cancel
						render={
							<Button
								type="button"
								variant="default"
								size="icon"
								className="aui-composer-cancel size-7 rounded-full"
								aria-label={t("agents.thread.stopGeneratingAriaLabel")}
							/>
						}
					>
						<SquareIcon className="aui-composer-cancel-icon size-3.5 fill-current" />
					</ComposerPrimitive.Cancel>
				</AuiIf>
			</div>
		</div>
	);
};

const MessageError: FC = () => {
	return (
		<MessagePrimitive.Error>
			<ErrorPrimitive.Root className="aui-message-error-root border-destructive bg-destructive/10 text-destructive dark:bg-destructive/5 mt-2 rounded-md border p-3 text-sm dark:text-red-200">
				<ErrorPrimitive.Message className="aui-message-error-message line-clamp-2" />
			</ErrorPrimitive.Root>
		</MessagePrimitive.Error>
	);
};

const AssistantMessage: FC = () => {
	const { t } = useTranslation("projects");
	const {
		ToolFallback: ToolFallbackComponent = ToolFallback,
		ToolGroup,
		ReasoningGroup,
	} = useContext(ThreadComponentsContext);
	// A message with zero parts and no content yet is the optimistic
	// placeholder assistant-ui's external-store runtime synthesizes while
	// `isRunning` is true but nothing assistant-authored has arrived (queued
	// conversation, or a sandbox still spinning up before its first ACP
	// event) — see hasUpcomingMessage in
	// @assistant-ui/core's external-store-thread-runtime-core. Distinguished
	// from a real in-progress message that merely ended on a tool call (also
	// "no-text" mode, but has parts already), which should keep showing the
	// plain working dot rather than claim the environment is still starting.
	const hasNoParts = useAuiState((s) => s.message.parts.length === 0);
	const { environmentReady } = useContext(ThreadStatusContext);

	// reserves space for action bar and compensates with `-mb` for consistent msg spacing
	// keeps hovered action bar from shifting layout (autohide doesn't support absolute positioning well)
	// for pt-[n] use -mb-[n + 6] & min-h-[n + 6] to preserve compensation
	const ACTION_BAR_PT = "pt-1.5";
	const ACTION_BAR_HEIGHT = `-mb-7.5 min-h-7.5 ${ACTION_BAR_PT}`;

	return (
		<MessagePrimitive.Root
			data-slot="aui_assistant-message-root"
			data-role="assistant"
			className="fade-in slide-in-from-bottom-1 animate-in relative duration-150"
		>
			<div
				data-slot="aui_assistant-message-content"
				// [contain-intrinsic-size:auto_24px] fixes issue #4104, don't change without checking for regressions
				className="text-foreground px-2 leading-relaxed wrap-break-word [contain-intrinsic-size:auto_24px] [content-visibility:auto]"
			>
				<MessagePrimitive.GroupedParts
					groupBy={groupPartByType({
						reasoning: ["group-chainOfThought", "group-reasoning"],
						"tool-call": ["group-chainOfThought", "group-tool"],
						"standalone-tool-call": [],
					})}
				>
					{({ part, children }) => {
						switch (part.type) {
							case "group-chainOfThought":
								return <div data-slot="aui_chain-of-thought">{children}</div>;
							case "group-tool":
								if (ToolGroup) {
									return <ToolGroup group={part}>{children}</ToolGroup>;
								}
								return (
									<ToolGroupRoot variant="ghost">
										<ToolGroupTrigger
											count={part.indices.length}
											active={part.status.type === "running"}
										/>
										<ToolGroupContent>{children}</ToolGroupContent>
									</ToolGroupRoot>
								);
							case "group-reasoning": {
								if (ReasoningGroup) {
									return (
										<ReasoningGroup group={part}>{children}</ReasoningGroup>
									);
								}
								const running = part.status.type === "running";
								return (
									<ReasoningRoot streaming={running}>
										<ReasoningTrigger active={running} />
										<ReasoningContent aria-busy={running}>
											<ReasoningText>{children}</ReasoningText>
										</ReasoningContent>
									</ReasoningRoot>
								);
							}
							case "text":
								return <MarkdownText />;
							case "reasoning":
								return <Reasoning {...part} />;
							case "tool-call":
								return part.toolUI ?? <ToolFallbackComponent {...part} />;
							case "data":
								return part.dataRendererUI;
							case "indicator":
								return hasNoParts ? (
									<span
										data-slot="aui_assistant-message-indicator"
										role="status"
										className="animate-pulse text-muted-foreground"
									>
										{environmentReady
											? t("agents.thread.thinking")
											: t("agents.thread.environmentInitializing")}
									</span>
								) : (
									<span
										data-slot="aui_assistant-message-indicator"
										role="status"
										className="animate-pulse font-sans"
										aria-label={t("agents.thread.assistantWorkingAriaLabel")}
									>
										{"●"}
									</span>
								);
							default:
								return null;
						}
					}}
				</MessagePrimitive.GroupedParts>
				<MessageError />
			</div>

			<div
				data-slot="aui_assistant-message-footer"
				className={cn("ms-2 flex items-center", ACTION_BAR_HEIGHT)}
			>
				<BranchPicker />
				<AssistantActionBar />
			</div>
		</MessagePrimitive.Root>
	);
};

const AssistantActionBar: FC = () => {
	const { t } = useTranslation("projects");
	return (
		<ActionBarPrimitive.Root
			hideWhenRunning
			autohide="not-last"
			className="aui-assistant-action-bar-root text-muted-foreground animate-in fade-in col-start-3 row-start-2 -ms-1 flex gap-1 duration-200"
		>
			<ActionBarPrimitive.Copy
				render={<TooltipIconButton tooltip={t("agents.thread.copy")} />}
			>
				<AuiIf condition={(s) => s.message.isCopied}>
					<CheckIcon className="animate-in zoom-in-50 fade-in duration-200 ease-out" />
				</AuiIf>
				<AuiIf condition={(s) => !s.message.isCopied}>
					<CopyIcon className="animate-in zoom-in-75 fade-in duration-150" />
				</AuiIf>
			</ActionBarPrimitive.Copy>
			<ActionBarPrimitive.Reload
				render={<TooltipIconButton tooltip={t("agents.thread.refresh")} />}
			>
				<RefreshCwIcon />
			</ActionBarPrimitive.Reload>
			<ActionBarMorePrimitive.Root>
				<ActionBarMorePrimitive.Trigger
					render={
						<TooltipIconButton
							tooltip={t("agents.thread.more")}
							className="data-[state=open]:bg-accent"
						/>
					}
				>
					<MoreHorizontalIcon />
				</ActionBarMorePrimitive.Trigger>
				<ActionBarMorePrimitive.Content
					side="bottom"
					align="start"
					sideOffset={6}
					className="aui-action-bar-more-content bg-popover/95 text-popover-foreground data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95 data-[state=open]:animate-in data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95 data-[state=closed]:animate-out data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2 z-80 min-w-32 overflow-hidden rounded-xl border p-1.5 shadow-lg backdrop-blur-sm"
				>
					<ActionBarPrimitive.ExportMarkdown
						render={
							<ActionBarMorePrimitive.Item className="aui-action-bar-more-item hover:bg-accent hover:text-accent-foreground focus:bg-accent focus:text-accent-foreground flex cursor-pointer items-center gap-2 rounded-lg px-2.5 py-1.5 text-sm outline-none select-none" />
						}
					>
						<DownloadIcon className="size-4" />
						{t("agents.thread.exportAsMarkdown")}
					</ActionBarPrimitive.ExportMarkdown>
				</ActionBarMorePrimitive.Content>
			</ActionBarMorePrimitive.Root>
		</ActionBarPrimitive.Root>
	);
};

const UserMessage: FC = () => {
	// ThreadMessageLike.metadata.custom is a real, otherwise-unused
	// assistant-ui escape hatch for arbitrary app data — set by
	// conversation-to-thread-messages.ts's user_message branch when a
	// historical message carried attached context.
	//
	// useAuiState compares its selector's return value with Object.is —
	// parseContextItems builds a brand-new array on every call, so calling
	// it *inside* the selector returned a fresh reference on every store
	// snapshot check, which useSyncExternalStore always read as "changed,"
	// forcing a re-render, which called the selector again, forever
	// ("Maximum update depth exceeded"). Selecting the raw, referentially
	// stable field and doing the array-producing work in a separate
	// useMemo (keyed on that same stable reference) breaks the loop.
	const rawContextItems = useAuiState(
		(s) => s.message.metadata.custom.contextItems,
	);
	const contextItems = useMemo(
		() => parseContextItems(rawContextItems),
		[rawContextItems],
	);
	return (
		<MessagePrimitive.Root
			data-slot="aui_user-message-root"
			className="fade-in slide-in-from-bottom-1 animate-in grid auto-rows-auto grid-cols-[minmax(72px,1fr)_auto] content-start gap-y-2 px-2 duration-150 [contain-intrinsic-size:auto_60px] [content-visibility:auto] [&:where(>*)]:col-start-2"
			data-role="user"
		>
			<UserMessageAttachments />
			<ContextItemReadOnlyRow items={contextItems} />

			<div className="aui-user-message-content-wrapper relative col-start-2 min-w-0">
				<div className="aui-user-message-content peer bg-muted text-foreground rounded-xl px-4 py-2 wrap-break-word empty:hidden">
					<MessagePrimitive.Parts />
				</div>
				<div className="aui-user-action-bar-wrapper absolute inset-s-0 top-1/2 -translate-x-full -translate-y-1/2 pe-2 peer-empty:hidden rtl:translate-x-full">
					<UserActionBar />
				</div>
			</div>

			<BranchPicker
				data-slot="aui_user-branch-picker"
				className="col-span-full col-start-1 row-start-3 -me-1 justify-end"
			/>
		</MessagePrimitive.Root>
	);
};

const UserActionBar: FC = () => {
	const { t } = useTranslation("projects");
	return (
		<ActionBarPrimitive.Root
			hideWhenRunning
			autohide="not-last"
			className="aui-user-action-bar-root flex flex-col items-end"
		>
			{/* assistant-ui's edit button doesn't check the runtime's `edit`
			 * capability on its own — it renders whenever a message isn't
			 * already being edited. Our runtimes never wire up `onEdit`
			 * (see conversation-view.tsx / ai-chat-float.tsx), so without this
			 * gate the pencil shows up on every message and throws
			 * "Runtime does not support editing messages" when clicked. */}
			<AuiIf condition={(s) => s.thread.capabilities.edit}>
				<ActionBarPrimitive.Edit
					render={
						<TooltipIconButton
							tooltip={t("agents.thread.edit")}
							className="aui-user-action-edit"
						/>
					}
				>
					<PencilIcon />
				</ActionBarPrimitive.Edit>
			</AuiIf>
		</ActionBarPrimitive.Root>
	);
};

const EditComposer: FC = () => {
	const { t } = useTranslation("projects");
	return (
		<MessagePrimitive.Root
			data-slot="aui_edit-composer-wrapper"
			className="flex flex-col px-2"
		>
			<ComposerPrimitive.Root className="aui-edit-composer-root border-border/60 dark:border-muted-foreground/15 ms-auto flex w-full max-w-[85%] flex-col rounded-(--composer-radius) border bg-(--composer-bg) shadow-[0_4px_16px_-8px_rgba(0,0,0,0.08),0_1px_2px_rgba(0,0,0,0.04)] dark:shadow-none">
				<ComposerPrimitive.Input
					className="aui-edit-composer-input text-foreground min-h-14 w-full resize-none bg-transparent px-4 pt-3 pb-1 text-sm outline-none"
					autoFocus
				/>
				<div className="aui-edit-composer-footer mx-2.5 mb-2.5 flex items-center gap-1.5 self-end">
					<ComposerPrimitive.Cancel
						render={
							<Button
								variant="ghost"
								size="sm"
								className="h-8 rounded-full px-3.5"
							/>
						}
					>
						{t("agents.thread.cancel")}
					</ComposerPrimitive.Cancel>
					<ComposerPrimitive.Send
						render={<Button size="sm" className="h-8 rounded-full px-3.5" />}
					>
						{t("agents.thread.update")}
					</ComposerPrimitive.Send>
				</div>
			</ComposerPrimitive.Root>
		</MessagePrimitive.Root>
	);
};

const BranchPicker: FC<BranchPickerPrimitive.Root.Props> = ({
	className,
	...rest
}) => {
	const { t } = useTranslation("projects");
	return (
		// `hideWhenSingleBranch` only hides per-message when a message
		// happens to have one branch; it doesn't know the runtime can't
		// switch branches at all. Our runtimes never wire up `setMessages`
		// (see conversation-view.tsx / ai-chat-float.tsx), so gate on the
		// capability too — otherwise Previous/Next render and throw "Runtime
		// does not support switching branches" when clicked.
		<AuiIf condition={(s) => s.thread.capabilities.switchToBranch}>
			<BranchPickerPrimitive.Root
				hideWhenSingleBranch
				className={cn(
					"aui-branch-picker-root text-muted-foreground -ms-2 me-2 inline-flex items-center text-xs",
					className,
				)}
				{...rest}
			>
				<BranchPickerPrimitive.Previous
					render={<TooltipIconButton tooltip={t("agents.thread.previous")} />}
				>
					<ChevronLeftIcon />
				</BranchPickerPrimitive.Previous>
				<span className="aui-branch-picker-state font-medium">
					<BranchPickerPrimitive.Number /> / <BranchPickerPrimitive.Count />
				</span>
				<BranchPickerPrimitive.Next
					render={<TooltipIconButton tooltip={t("agents.thread.next")} />}
				>
					<ChevronRightIcon />
				</BranchPickerPrimitive.Next>
			</BranchPickerPrimitive.Root>
		</AuiIf>
	);
};
