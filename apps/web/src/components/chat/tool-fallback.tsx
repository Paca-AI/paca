"use client";

import { CheckIcon, LoaderIcon, XCircleIcon } from "lucide-react";
import { memo, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useChat } from "@/components/chat/chat-context";
import type { ChatToolCallPart, ToolCallStatus } from "@/components/chat/types";
import { AskQuestionCard } from "@/components/shared/ask-question-card";
import {
	DiffCollapsedRow,
	DiffLineRow,
} from "@/components/shared/content-diff";
import {
	TraceCollapsible,
	TraceCollapsibleContent,
	TraceCollapsibleTrigger,
	TraceDescription,
	TraceIcon,
	TraceLine,
	TraceTitle,
} from "@/components/ui/trace";
import {
	isAskQuestionTool,
	parseAskQuestionFromCall,
} from "@/lib/ask-question";
import { collapseUnchangedContext, computeLineDiff } from "@/lib/diff-utils";
import { cn } from "@/lib/utils";
import { getToolCommand } from "./tool-args";

// A tool call's detail panel must stay expanded for the rest of the run once
// a user opens it, but the component's local `open` state does not survive
// every remount as new events stream in. Persisting by the stable
// toolCallId outside component state means a remount picks the same
// open/closed value back up instead of snapping shut. Module-scoped (not
// per-conversation) since it's keyed by an already-unique id.
const openToolCallIds = new Set<string>();

const formatToolDuration = (ms: number) => {
	if (ms < 1000) return "<1s";
	const seconds = ms / 1000;
	if (seconds < 10) return `${(Math.floor(seconds * 10) / 10).toFixed(1)}s`;
	if (seconds < 60) return `${Math.floor(seconds)}s`;
	return `${Math.floor(seconds / 60)}m ${Math.floor(seconds % 60)}s`;
};

/** Live elapsed time while a call runs; undefined once it is not running. */
function useRunningElapsed(running: boolean): number | undefined {
	const [start] = useState(() => Date.now());
	const [now, setNow] = useState(() => Date.now());
	useEffect(() => {
		if (!running) return;
		const id = setInterval(() => setNow(Date.now()), 500);
		return () => clearInterval(id);
	}, [running]);
	return running ? now - start : undefined;
}

function ToolFallbackArgs({
	argsText,
	className,
	...props
}: React.ComponentProps<"div"> & {
	argsText?: string;
}) {
	if (!argsText) return null;
	// A shell command reads best verbatim (real newlines, no JSON escaping).
	const command = getToolCommand(argsText);

	return (
		<div
			data-slot="tool-fallback-args"
			className={cn("aui-tool-fallback-args", className)}
			{...props}
		>
			<pre className="aui-tool-fallback-args-value bg-muted/50 text-foreground/90 rounded-md p-2.5 text-xs whitespace-pre-wrap">
				{command ? `$ ${command}` : argsText}
			</pre>
		</div>
	);
}

interface ToolFallbackDiffBlock {
	path?: string;
	oldText: string | null;
	newText: string;
}

// computeLineDiff is O(oldLines * newLines) time and space (a full DP
// matrix) — fine for the small BlockNote documents content-diff.tsx was
// originally written for, but file_editor/ACP diffs can carry a full
// multi-thousand-line file as old/new text. Skip the computation above this
// threshold rather than risk freezing the tab or spiking memory on a large
// file edit; capping the inputs also bounds the number of rendered rows,
// since diff output length can't exceed oldLines.length + newLines.length.
const MAX_DIFF_LINES_PER_SIDE = 2000;

// `artifact` is a UI-only side channel on a tool-call part (not part of the
// model-visible args/result) — untyped, so the shape produced by conversation-to-thread-messages.ts for ACP
// edit/write tool calls has to be verified at runtime rather than assumed.
function getToolDiffs(artifact: unknown): ToolFallbackDiffBlock[] | undefined {
	if (typeof artifact !== "object" || artifact === null) return undefined;
	const diffs = (artifact as { diffs?: unknown }).diffs;
	if (!Array.isArray(diffs)) return undefined;
	const result: ToolFallbackDiffBlock[] = [];
	for (const d of diffs) {
		if (typeof d !== "object" || d === null) continue;
		const { path, oldText, newText } = d as Record<string, unknown>;
		if (typeof newText !== "string") continue;
		result.push({
			path: typeof path === "string" ? path : undefined,
			oldText: typeof oldText === "string" ? oldText : null,
			newText,
		});
	}
	return result.length > 0 ? result : undefined;
}

function ToolFallbackDiffBlockView({ diff }: { diff: ToolFallbackDiffBlock }) {
	const { t } = useTranslation("shared");
	const oldLines = useMemo(
		() => (diff.oldText ?? "").split("\n"),
		[diff.oldText],
	);
	const newLines = useMemo(() => diff.newText.split("\n"), [diff.newText]);
	const isTooLarge =
		oldLines.length > MAX_DIFF_LINES_PER_SIDE ||
		newLines.length > MAX_DIFF_LINES_PER_SIDE;
	const lines = useMemo(
		() => (isTooLarge ? [] : computeLineDiff(oldLines, newLines)),
		[isTooLarge, oldLines, newLines],
	);
	const hasChanges =
		!isTooLarge && lines.some((line) => line.type !== "unchanged");
	// Collapse long unchanged runs down to a few lines of context around each
	// change, the way `git diff` hunks do — a single-line edit to a large
	// file should not render every unchanged line around it.
	const rows = useMemo(() => collapseUnchangedContext(lines), [lines]);

	return (
		<div className="aui-tool-fallback-diff-block flex flex-col gap-1">
			{diff.path && (
				<p className="aui-tool-fallback-diff-path text-muted-foreground/70 font-mono text-[11px]">
					{diff.path}
				</p>
			)}
			{isTooLarge ? (
				<p className="text-muted-foreground/40 text-xs italic">
					{t("contentDiff.diffTooLarge", {
						lines: Math.max(oldLines.length, newLines.length),
					})}
				</p>
			) : hasChanges ? (
				<div className="rounded-md border border-border/30 overflow-hidden bg-muted/5">
					{rows.map((row, idx) =>
						row.type === "collapsed" ? (
							// biome-ignore lint/suspicious/noArrayIndexKey: stable diff output
							<DiffCollapsedRow key={idx} count={row.count} />
						) : (
							// biome-ignore lint/suspicious/noArrayIndexKey: stable diff output
							<DiffLineRow key={idx} line={row} />
						),
					)}
				</div>
			) : (
				<p className="text-muted-foreground/40 text-xs italic">
					{t("contentDiff.noDifferences")}
				</p>
			)}
		</div>
	);
}

function ToolFallbackDiffs({
	diffs,
	className,
	...props
}: React.ComponentProps<"div"> & {
	diffs: ToolFallbackDiffBlock[];
}) {
	if (diffs.length === 0) return null;

	return (
		<div
			data-slot="tool-fallback-diffs"
			className={cn("aui-tool-fallback-diffs flex flex-col gap-3", className)}
			{...props}
		>
			{diffs.map((diff, idx) => (
				// biome-ignore lint/suspicious/noArrayIndexKey: diff order is stable per render; path alone isn't guaranteed unique/present
				<ToolFallbackDiffBlockView key={idx} diff={diff} />
			))}
		</div>
	);
}

function ToolFallbackResult({
	result,
	isError,
	className,
	...props
}: React.ComponentProps<"div"> & {
	result?: unknown;
	isError?: boolean;
}) {
	const { t } = useTranslation("projects");
	if (result === undefined) return null;

	return (
		<div
			data-slot="tool-fallback-result"
			className={cn("aui-tool-fallback-result", className)}
			{...props}
		>
			<p
				className={cn(
					"aui-tool-fallback-result-header text-xs font-medium",
					isError ? "text-destructive" : "text-muted-foreground",
				)}
			>
				{t(isError ? "agents.thread.errorLabel" : "agents.thread.resultLabel")}
			</p>
			<pre
				className={cn(
					"aui-tool-fallback-result-content mt-1 rounded-md p-2.5 text-xs whitespace-pre-wrap",
					isError
						? "bg-destructive/10 text-destructive border-destructive/30 border"
						: "bg-muted/50 text-foreground/90",
				)}
			>
				{typeof result === "string" ? result : JSON.stringify(result, null, 2)}
			</pre>
		</div>
	);
}

// Chat-side rendering of the `ask_user_question` MCP tool: the question
// arrives as the tool call's input, and picking an answer sends it as the
// user's next message in the thread.
function ToolFallbackAskQuestion({
	toolCallId,
	argsText,
	result,
	isLastMessage,
}: {
	toolCallId: string;
	argsText?: string;
	result?: unknown;
	isLastMessage: boolean;
}) {
	const { isRunning, send } = useChat();
	const ask = parseAskQuestionFromCall(argsText, result);
	if (!ask) return null;

	return (
		<AskQuestionCard
			key={toolCallId}
			ask={ask}
			// Only the latest question can be answered: an older one has
			// already been followed by more conversation, and while the agent
			// is still mid-turn a reply would just be queued behind it.
			disabled={!isLastMessage || isRunning}
			onAnswer={(answer) => send(answer)}
		/>
	);
}

type ToolFallbackProps = {
	part: ChatToolCallPart;
	status: ToolCallStatus;
	/** Whether the message this call belongs to is the newest one. */
	isLastMessage: boolean;
};

const ToolFallbackImpl = ({
	part,
	status,
	isLastMessage,
}: ToolFallbackProps) => {
	const { t } = useTranslation("projects");
	const { toolCallId, toolName, argsText, result, isError, artifact } = part;
	const isRunning = status.type === "running";
	const isCancelled =
		status.type === "incomplete" && status.reason === "cancelled";
	const diffs = getToolDiffs(artifact);
	const isAskQuestion =
		isAskQuestionTool(toolName) &&
		parseAskQuestionFromCall(argsText, result) !== null;
	const elapsedMs = useRunningElapsed(isRunning);

	const [open, setOpenState] = useState(() => openToolCallIds.has(toolCallId));
	const setOpen = (next: boolean) => {
		setOpenState(next);
		if (next) openToolCallIds.add(toolCallId);
		else openToolCallIds.delete(toolCallId);
	};

	if (isAskQuestion) {
		return (
			<div data-slot="tool-fallback-ask" className="w-full min-w-0 py-1.5">
				<ToolFallbackAskQuestion
					toolCallId={toolCallId}
					argsText={argsText}
					result={result}
					isLastMessage={isLastMessage}
				/>
			</div>
		);
	}

	const lineStatus = isError ? "error" : isRunning ? "running" : undefined;
	const Icon = isError ? XCircleIcon : isRunning ? LoaderIcon : CheckIcon;
	const labelPrefix = t(
		isCancelled ? "agents.thread.cancelledTool" : "agents.thread.usedTool",
	);

	return (
		<TraceCollapsible open={open} onOpenChange={setOpen}>
			<TraceCollapsibleTrigger>
				<TraceLine status={lineStatus}>
					<TraceIcon>
						<Icon className={cn(isRunning && "animate-spin")} />
					</TraceIcon>
					<TraceTitle className={cn(isCancelled && "line-through")}>
						{labelPrefix}: <b>{toolName}</b>
					</TraceTitle>
					{elapsedMs !== undefined && (
						<TraceDescription>{formatToolDuration(elapsedMs)}</TraceDescription>
					)}
				</TraceLine>
			</TraceCollapsibleTrigger>
			<TraceCollapsibleContent className="flex flex-col gap-2 text-sm">
				{diffs ? (
					<ToolFallbackDiffs
						diffs={diffs}
						className={cn(isCancelled && "opacity-60")}
					/>
				) : (
					<ToolFallbackArgs
						argsText={argsText}
						className={cn(isCancelled && "opacity-60")}
					/>
				)}
				{!isCancelled && (
					<ToolFallbackResult result={result} isError={isError} />
				)}
			</TraceCollapsibleContent>
		</TraceCollapsible>
	);
};

export const ToolFallback = memo(ToolFallbackImpl);
export { ToolFallbackArgs, ToolFallbackDiffs, ToolFallbackResult };
