"use client";

import {
	type ToolApprovalOption,
	type ToolCallMessagePart,
	type ToolCallMessagePartComponent,
	type ToolCallMessagePartProps,
	type ToolCallMessagePartStatus,
	useScrollLock,
	useToolCallElapsed,
} from "@assistant-ui/react";
import type { TFunction } from "i18next";
import {
	AlertCircleIcon,
	CheckIcon,
	ChevronDownIcon,
	LoaderIcon,
	XCircleIcon,
} from "lucide-react";
import { memo, useCallback, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { DiffLineRow } from "@/components/shared/content-diff";
import { Button } from "@/components/ui/button";
import {
	Collapsible,
	CollapsibleContent,
	CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { computeLineDiff } from "@/lib/diff-utils";
import { cn } from "@/lib/utils";

const ANIMATION_DURATION = 200;

const pressable = "active:scale-[0.98]";

export type ToolFallbackRootProps = Omit<
	React.ComponentProps<typeof Collapsible>,
	"open" | "onOpenChange"
> & {
	open?: boolean;
	onOpenChange?: (open: boolean) => void;
	defaultOpen?: boolean;
};

function ToolFallbackRoot({
	className,
	open: controlledOpen,
	onOpenChange: controlledOnOpenChange,
	defaultOpen = false,
	children,
	...props
}: ToolFallbackRootProps) {
	const collapsibleRef = useRef<HTMLDivElement>(null);
	const [uncontrolledOpen, setUncontrolledOpen] = useState(defaultOpen);
	const lockScroll = useScrollLock(collapsibleRef, ANIMATION_DURATION);

	const isControlled = controlledOpen !== undefined;
	const isOpen = isControlled ? controlledOpen : uncontrolledOpen;

	const handleOpenChange = useCallback(
		(open: boolean) => {
			lockScroll();
			if (!isControlled) {
				setUncontrolledOpen(open);
			}
			controlledOnOpenChange?.(open);
		},
		[lockScroll, isControlled, controlledOnOpenChange],
	);

	return (
		<Collapsible
			ref={collapsibleRef}
			data-slot="tool-fallback-root"
			open={isOpen}
			onOpenChange={handleOpenChange}
			className={cn(
				"aui-tool-fallback-root group/tool-fallback-root w-full",
				className,
			)}
			style={
				{
					"--animation-duration": `${ANIMATION_DURATION}ms`,
				} as React.CSSProperties
			}
			{...props}
		>
			{children}
		</Collapsible>
	);
}

type ToolStatus = ToolCallMessagePartStatus["type"];

const statusIconMap: Record<ToolStatus, React.ElementType> = {
	running: LoaderIcon,
	complete: CheckIcon,
	incomplete: XCircleIcon,
	"requires-action": AlertCircleIcon,
};

const formatToolDuration = (ms: number) => {
	if (ms < 1000) return "<1s";
	const seconds = ms / 1000;
	if (seconds < 10) return `${(Math.floor(seconds * 10) / 10).toFixed(1)}s`;
	if (seconds < 60) return `${Math.floor(seconds)}s`;
	return `${Math.floor(seconds / 60)}m ${Math.floor(seconds % 60)}s`;
};

function ToolFallbackDuration({
	className,
	...props
}: React.ComponentProps<"span">) {
	const elapsedMs = useToolCallElapsed();
	if (elapsedMs === undefined) return null;

	return (
		<span
			data-slot="tool-fallback-duration"
			className={cn(
				"aui-tool-fallback-duration text-muted-foreground text-xs tabular-nums",
				className,
			)}
			{...props}
		>
			{formatToolDuration(elapsedMs)}
		</span>
	);
}

function ToolFallbackTrigger({
	toolName,
	status,
	isError,
	className,
	...props
}: React.ComponentProps<typeof CollapsibleTrigger> & {
	toolName: string;
	status?: ToolCallMessagePartStatus;
	isError?: boolean;
}) {
	const { t } = useTranslation("projects");
	const statusType = status?.type ?? "complete";
	const isRunning = statusType === "running";
	const isCancelled =
		status?.type === "incomplete" && status.reason === "cancelled";
	// A tool call with a result is always reported "complete" by the runtime
	// regardless of whether that result was an error (see ToolCallMessagePart
	// .isError) — check it separately to still show a failure indicator.
	const isFailed = isError === true && statusType === "complete";

	const Icon = isFailed ? XCircleIcon : statusIconMap[statusType];
	const labelPrefix = t(
		isCancelled ? "agents.thread.cancelledTool" : "agents.thread.usedTool",
	);

	return (
		<CollapsibleTrigger
			data-slot="tool-fallback-trigger"
			className={cn(
				"aui-tool-fallback-trigger group/trigger text-muted-foreground hover:text-foreground flex w-fit origin-left items-center gap-2 py-1.5 text-sm transition-[color,scale] active:scale-[0.98]",
				className,
			)}
			{...props}
		>
			<Icon
				data-slot="tool-fallback-trigger-icon"
				className={cn(
					"aui-tool-fallback-trigger-icon size-4 shrink-0",
					isCancelled && "text-muted-foreground",
					isFailed && "text-destructive",
					isRunning && "animate-spin [animation-duration:0.6s]",
				)}
			/>
			<span
				data-slot="tool-fallback-trigger-label"
				className={cn(
					"aui-tool-fallback-trigger-label-wrapper relative inline-block text-start leading-none",
					isCancelled && "text-muted-foreground line-through",
				)}
			>
				<span>
					{labelPrefix}: <b>{toolName}</b>
				</span>
				{isRunning && (
					<span
						aria-hidden
						data-slot="tool-fallback-trigger-shimmer"
						className="aui-tool-fallback-trigger-shimmer shimmer pointer-events-none absolute inset-0 motion-reduce:animate-none"
					>
						{labelPrefix}: <b>{toolName}</b>
					</span>
				)}
			</span>
			<ToolFallbackDuration />
			<ChevronDownIcon
				data-slot="tool-fallback-trigger-chevron"
				className={cn(
					"aui-tool-fallback-trigger-chevron size-4 shrink-0",
					"transition-transform duration-(--animation-duration) ease-[cubic-bezier(0.32,0.72,0,1)] motion-reduce:transition-none",
					"group-data-[state=closed]/trigger:-rotate-90",
					"group-data-[state=open]/trigger:rotate-0",
				)}
			/>
		</CollapsibleTrigger>
	);
}

function ToolFallbackContent({
	className,
	children,
	...props
}: React.ComponentProps<typeof CollapsibleContent>) {
	return (
		<CollapsibleContent
			data-slot="tool-fallback-content"
			className={cn(
				"aui-tool-fallback-content relative overflow-hidden text-sm outline-none",
				"group/collapsible-content ease-[cubic-bezier(0.32,0.72,0,1)] motion-reduce:animate-none",
				"data-[state=closed]:animate-collapsible-up",
				"data-[state=open]:animate-collapsible-down",
				"data-[state=closed]:fill-mode-forwards",
				"data-[state=closed]:pointer-events-none",
				"data-[state=open]:duration-(--animation-duration)",
				"data-[state=closed]:duration-(--animation-duration)",
				className,
			)}
			{...props}
		>
			<div
				className={cn(
					"flex flex-col gap-2 ps-6 pt-1 pb-2 ease-[cubic-bezier(0.32,0.72,0,1)] motion-reduce:animate-none",
					"group-data-[state=open]/collapsible-content:animate-in group-data-[state=open]/collapsible-content:fade-in-0 group-data-[state=open]/collapsible-content:blur-in-[2px] group-data-[state=open]/collapsible-content:slide-in-from-top-1",
					"group-data-[state=closed]/collapsible-content:animate-out group-data-[state=closed]/collapsible-content:fade-out-0 group-data-[state=closed]/collapsible-content:blur-out-[2px] group-data-[state=closed]/collapsible-content:slide-out-to-top-1",
					"group-data-[state=closed]/collapsible-content:duration-(--animation-duration) group-data-[state=open]/collapsible-content:duration-(--animation-duration)",
				)}
			>
				{children}
			</div>
		</CollapsibleContent>
	);
}

function ToolFallbackArgs({
	argsText,
	className,
	...props
}: React.ComponentProps<"div"> & {
	argsText?: string;
}) {
	if (!argsText) return null;

	return (
		<div
			data-slot="tool-fallback-args"
			className={cn("aui-tool-fallback-args", className)}
			{...props}
		>
			<pre className="aui-tool-fallback-args-value bg-muted/50 text-foreground/90 rounded-md p-2.5 text-xs whitespace-pre-wrap">
				{argsText}
			</pre>
		</div>
	);
}

interface ToolFallbackDiffBlock {
	path?: string;
	oldText: string | null;
	newText: string;
}

// `artifact` is assistant-ui's generic "UI-only" side channel on a tool-call
// part (not part of the model-visible args/result) — untyped at the library
// level, so the shape produced by conversation-to-thread-messages.ts for ACP
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
	const lines = useMemo(
		() =>
			computeLineDiff(
				(diff.oldText ?? "").split("\n"),
				diff.newText.split("\n"),
			),
		[diff.oldText, diff.newText],
	);
	const hasChanges = lines.some((line) => line.type !== "unchanged");

	return (
		<div className="aui-tool-fallback-diff-block flex flex-col gap-1">
			{diff.path && (
				<p className="aui-tool-fallback-diff-path text-muted-foreground/70 font-mono text-[11px]">
					{diff.path}
				</p>
			)}
			{hasChanges ? (
				<div className="rounded-md border border-border/30 overflow-hidden bg-muted/5">
					{lines.map((line, idx) => (
						// biome-ignore lint/suspicious/noArrayIndexKey: stable diff output
						<DiffLineRow key={idx} line={line} />
					))}
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
				<ToolFallbackDiffBlockView key={diff.path ?? idx} diff={diff} />
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

function ToolFallbackError({
	status,
	className,
	...props
}: React.ComponentProps<"div"> & {
	status?: ToolCallMessagePartStatus;
}) {
	const { t } = useTranslation("projects");
	if (status?.type !== "incomplete") return null;

	const error = status.error;
	const errorText = error
		? typeof error === "string"
			? error
			: JSON.stringify(error)
		: null;

	if (!errorText) return null;

	const isCancelled = status.reason === "cancelled";
	const headerText = isCancelled
		? t("agents.thread.cancelledReasonLabel")
		: t("agents.thread.errorLabel");

	return (
		<div
			data-slot="tool-fallback-error"
			className={cn("aui-tool-fallback-error", className)}
			{...props}
		>
			<p className="aui-tool-fallback-error-header text-muted-foreground font-semibold">
				{headerText}
			</p>
			<p className="aui-tool-fallback-error-reason text-muted-foreground">
				{errorText}
			</p>
		</div>
	);
}

const KNOWN_APPROVAL_KINDS = new Set([
	"allow-once",
	"allow-always",
	"reject-once",
	"reject-always",
]);

const isAllowKind = (kind: string) =>
	kind === "allow-once" || kind === "allow-always";

function defaultApprovalOptionLabel(
	kind: string,
	t: TFunction<"projects">,
): string | undefined {
	switch (kind) {
		case "allow-once":
			return t("agents.thread.allow");
		case "allow-always":
			return t("agents.thread.alwaysAllow");
		case "reject-once":
			return t("agents.thread.deny");
		case "reject-always":
			return t("agents.thread.alwaysDeny");
		default:
			return undefined;
	}
}

const approvalOptionLabel = (
	option: ToolApprovalOption,
	t: TFunction<"projects">,
) => option.label ?? defaultApprovalOptionLabel(option.kind, t) ?? option.id;

function ToolFallbackApproval({
	className,
	addResult,
	resume,
	interrupt,
	approval,
	respondToApproval,
	...props
}: React.ComponentProps<"div"> &
	Partial<
		Pick<ToolCallMessagePartProps, "addResult" | "resume" | "respondToApproval">
	> & {
		interrupt?: ToolCallMessagePart["interrupt"];
		approval?: ToolCallMessagePart["approval"];
	}) {
	const { t } = useTranslation("projects");
	const [submitted, setSubmitted] = useState(false);
	const [confirmingId, setConfirmingId] = useState<string | null>(null);

	if (
		approval != null &&
		(approval.approved !== undefined || approval.resolution !== undefined)
	)
		return null;

	// Custom (`_`-prefixed) kinds cannot be resolved to a boolean by the kit;
	// hosts using custom kinds render their own bar. A declared option list is
	// a host constraint: the kit never adds an approval path beyond it, but
	// always preserves a refusal path.
	const declaredOptions = respondToApproval ? approval?.options : undefined;
	const options = declaredOptions?.filter((o) =>
		KNOWN_APPROVAL_KINDS.has(o.kind),
	);

	const respond = (approved: boolean) => {
		if (submitted) return;
		if (
			approval != null &&
			approval.approved === undefined &&
			respondToApproval
		) {
			respondToApproval({ approved });
		} else if (interrupt) {
			resume?.({ approved });
		} else {
			addResult?.(
				approved
					? t("agents.thread.approvedByUser")
					: t("agents.thread.deniedByUser"),
			);
		}
		setSubmitted(true);
	};

	const respondWithOption = (option: ToolApprovalOption) => {
		if (submitted) return;
		respondToApproval?.({ optionId: option.id });
		setSubmitted(true);
		setConfirmingId(null);
	};

	const handleOption = (option: ToolApprovalOption) => {
		if (option.confirm) {
			setConfirmingId(option.id);
		} else {
			respondWithOption(option);
		}
	};

	const confirming =
		confirmingId != null
			? options?.find((o) => o.id === confirmingId)
			: undefined;

	if (confirming) {
		const confirmMeta =
			typeof confirming.confirm === "object" ? confirming.confirm : undefined;
		const confirmDescription =
			confirmMeta?.description ?? confirming.description;
		return (
			<div
				data-slot="tool-fallback-approval-confirm"
				className={cn(
					"aui-tool-fallback-approval-confirm flex flex-col gap-2 pt-1",
					className,
				)}
				{...props}
			>
				<p className="aui-tool-fallback-approval-confirm-title font-semibold">
					{confirmMeta?.title ?? `${approvalOptionLabel(confirming, t)}?`}
				</p>
				{confirmDescription && (
					<p className="aui-tool-fallback-approval-confirm-description text-muted-foreground">
						{confirmDescription}
					</p>
				)}
				{confirming.grants && confirming.grants.length > 0 && (
					<ul className="aui-tool-fallback-approval-confirm-grants flex flex-col gap-1">
						{confirming.grants.map((grant) => (
							<li key={grant}>
								<code className="aui-tool-fallback-approval-confirm-grant bg-muted rounded px-1.5 py-0.5 text-xs">
									{grant}
								</code>
							</li>
						))}
					</ul>
				)}
				<div className="flex items-center gap-2">
					<Button
						size="sm"
						className={pressable}
						onClick={() => respondWithOption(confirming)}
						disabled={submitted}
					>
						{t("agents.thread.confirm")}
					</Button>
					<Button
						size="sm"
						variant="outline"
						className={pressable}
						onClick={() => setConfirmingId(null)}
						disabled={submitted}
					>
						{t("agents.thread.back")}
					</Button>
				</div>
			</div>
		);
	}

	if (declaredOptions && declaredOptions.length > 0) {
		const allowOptions = options?.filter((o) => isAllowKind(o.kind)) ?? [];
		const rejectOptions = options?.filter((o) => !isAllowKind(o.kind)) ?? [];
		return (
			<div
				data-slot="tool-fallback-approval"
				className={cn(
					"aui-tool-fallback-approval flex flex-wrap items-center gap-2 pt-1",
					className,
				)}
				{...props}
			>
				{[...allowOptions, ...rejectOptions].map((option) => (
					<Button
						key={option.id}
						size="sm"
						variant={option === allowOptions[0] ? "default" : "outline"}
						className={pressable}
						onClick={() => handleOption(option)}
						disabled={submitted}
					>
						{approvalOptionLabel(option, t)}
					</Button>
				))}
				{rejectOptions.length === 0 && (
					<Button
						size="sm"
						variant="outline"
						className={pressable}
						onClick={() => respond(false)}
						disabled={submitted}
					>
						{t("agents.thread.deny")}
					</Button>
				)}
			</div>
		);
	}

	return (
		<div
			data-slot="tool-fallback-approval"
			className={cn(
				"aui-tool-fallback-approval flex items-center gap-2 pt-1",
				className,
			)}
			{...props}
		>
			<Button
				size="sm"
				className={pressable}
				onClick={() => respond(true)}
				disabled={submitted}
			>
				{t("agents.thread.allow")}
			</Button>
			<Button
				size="sm"
				variant="outline"
				className={pressable}
				onClick={() => respond(false)}
				disabled={submitted}
			>
				{t("agents.thread.deny")}
			</Button>
		</div>
	);
}

const ToolFallbackImpl: ToolCallMessagePartComponent = ({
	toolName,
	argsText,
	artifact,
	result,
	isError,
	status,
	addResult,
	resume,
	interrupt,
	approval,
	respondToApproval,
}) => {
	const isCancelled =
		status?.type === "incomplete" && status.reason === "cancelled";
	const isRequiresAction = status?.type === "requires-action";
	const diffs = getToolDiffs(artifact);

	const [open, setOpen] = useState(isRequiresAction);
	const [prevRequiresAction, setPrevRequiresAction] =
		useState(isRequiresAction);
	if (isRequiresAction !== prevRequiresAction) {
		setPrevRequiresAction(isRequiresAction);
		if (isRequiresAction) setOpen(true);
	}

	return (
		<ToolFallbackRoot open={open} onOpenChange={setOpen}>
			<ToolFallbackTrigger
				toolName={toolName}
				status={status}
				isError={isError}
			/>
			<ToolFallbackContent>
				<ToolFallbackError status={status} />
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
				{isRequiresAction && (
					<ToolFallbackApproval
						addResult={addResult}
						resume={resume}
						interrupt={interrupt}
						approval={approval}
						respondToApproval={respondToApproval}
					/>
				)}
				{!isCancelled && (
					<ToolFallbackResult result={result} isError={isError} />
				)}
			</ToolFallbackContent>
		</ToolFallbackRoot>
	);
};

const ToolFallback = memo(
	ToolFallbackImpl,
) as unknown as ToolCallMessagePartComponent & {
	Root: typeof ToolFallbackRoot;
	Trigger: typeof ToolFallbackTrigger;
	Content: typeof ToolFallbackContent;
	Args: typeof ToolFallbackArgs;
	Diffs: typeof ToolFallbackDiffs;
	Result: typeof ToolFallbackResult;
	Error: typeof ToolFallbackError;
	Approval: typeof ToolFallbackApproval;
};

ToolFallback.displayName = "ToolFallback";
ToolFallback.Root = ToolFallbackRoot;
ToolFallback.Trigger = ToolFallbackTrigger;
ToolFallback.Content = ToolFallbackContent;
ToolFallback.Args = ToolFallbackArgs;
ToolFallback.Diffs = ToolFallbackDiffs;
ToolFallback.Result = ToolFallbackResult;
ToolFallback.Error = ToolFallbackError;
ToolFallback.Approval = ToolFallbackApproval;

export {
	ToolFallback,
	ToolFallbackApproval,
	ToolFallbackArgs,
	ToolFallbackContent,
	ToolFallbackDiffs,
	ToolFallbackError,
	ToolFallbackResult,
	ToolFallbackRoot,
	ToolFallbackTrigger,
};
