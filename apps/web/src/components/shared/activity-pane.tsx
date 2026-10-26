import {
	type QueryKey,
	useInfiniteQuery,
	useMutation,
	useQueryClient,
} from "@tanstack/react-query";
import {
	Cog,
	GitBranch,
	Loader2,
	MessageSquare,
	MoreHorizontal,
	MoreVertical,
	Pencil,
	RotateCcw,
	Send,
	Trash2,
} from "lucide-react";
import {
	type CSSProperties,
	type ReactNode,
	useEffect,
	useLayoutEffect,
	useMemo,
	useRef,
	useState,
} from "react";
import { useTranslation } from "react-i18next";
import {
	ACTIVITY_WIDTH_DEFAULT,
	ACTIVITY_WIDTH_KEY_STEP,
	ACTIVITY_WIDTH_MIN,
	clampActivityWidth,
	maxActivityWidth,
	readStoredActivityWidth,
	storeActivityWidth,
} from "@/components/shared/activity-pane-width";
import { AskQuestionCard } from "@/components/shared/ask-question-card";
import {
	blocksToText,
	CommentDisplay,
	CommentEditor,
	type CommentEditorHandle,
} from "@/components/shared/comment-blocknote";
import { ContentDiffDialog } from "@/components/shared/content-diff";
import { EntityAvatar } from "@/components/shared/entity-avatar";
import { LoadMoreSentinel } from "@/components/shared/load-more-sentinel";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useActorAvatar } from "@/hooks/use-actor-avatar";
import { extractAskQuestion } from "@/lib/ask-question";
import { searchMentionableMembers } from "@/lib/mention-api";
import { timeAgo } from "@/lib/time-ago";
import { cn } from "@/lib/utils";

export interface ActivityEntry {
	id: string;
	actor_id?: string | null;
	actor_name: string;
	actor_username: string;
	actor_avatar_url?: string | null;
	actor_avatar_thumb_url?: string | null;
	activity_type: string;
	content: Record<string, unknown> | unknown[] | string | null;
	created_at: string;
	updated_at: string;
}

/** One page of an entity's timeline: newest first, with the cursor for the
 *  next (older) page, if any. */
export interface ActivityPage<T> {
	items: T[];
	next_cursor?: string | null;
}

export interface ActivityPaneConfig<T extends ActivityEntry> {
	projectId: string;
	entityId: string;
	queryKey: QueryKey;
	/** Loads one page of the timeline; the pane shows the newest page first and
	 *  pages in older entries on demand. */
	fetchPage: (cursor?: string) => Promise<ActivityPage<T>>;
	addComment?: (blocks: unknown[]) => Promise<unknown>;
	updateComment?: (commentId: string, blocks: unknown[]) => Promise<unknown>;
	deleteComment?: (commentId: string) => Promise<void>;
	onRevert?: (entry: T) => Promise<void>;
	getDiffContent?: (
		entry: T,
	) => { old: unknown; new: unknown; title?: string } | null;
	isRevertable?: (entry: T) => boolean;
	describeActivity: (entry: T) => ReactNode;
	getCommentBlocks: (content: T["content"]) => unknown[] | null;
	nameMaps?: Record<string, Record<string, string>>;
	currentUserId?: string;
}

type ActivityFilter = "comments" | "all";

const ACTIVITY_FILTER_STORAGE_KEY = "paca:activity-filter";

function readStoredActivityFilter(): ActivityFilter {
	try {
		const stored = localStorage.getItem(ACTIVITY_FILTER_STORAGE_KEY);
		return stored === "all" ? "all" : "comments";
	} catch {
		return "comments";
	}
}

export function ActivityPane<T extends ActivityEntry>({
	projectId,
	queryKey,
	fetchPage,
	addComment,
	updateComment,
	deleteComment,
	onRevert,
	getDiffContent,
	isRevertable,
	describeActivity,
	getCommentBlocks,
	currentUserId,
}: ActivityPaneConfig<T>) {
	const { t } = useTranslation("shared");
	const editorRef = useRef<CommentEditorHandle>(null);
	const scrollAreaRef = useRef<HTMLDivElement>(null);
	const [editorFocused, setEditorFocused] = useState(false);
	const [editingCommentId, setEditingCommentId] = useState<string | null>(null);
	const [filter, setFilter] = useState<ActivityFilter>(
		readStoredActivityFilter,
	);
	const [viewportWidth, setViewportWidth] = useState(() => window.innerWidth);
	const [width, setWidth] = useState(() =>
		readStoredActivityWidth(window.innerWidth),
	);
	const widthRef = useRef(width);
	const dragRef = useRef<{ startX: number; startWidth: number } | null>(null);
	const maxWidth = maxActivityWidth(viewportWidth);
	const qc = useQueryClient();

	function applyWidth(next: number) {
		const clamped = clampActivityWidth(next, window.innerWidth);
		widthRef.current = clamped;
		setWidth(clamped);
	}

	function endDrag() {
		if (!dragRef.current) return;
		dragRef.current = null;
		storeActivityWidth(widthRef.current);
	}

	// Keep the in-memory width (and aria values) in step with the window so a
	// stale, larger value never lingers after the window shrinks.
	useEffect(() => {
		function onResize() {
			setViewportWidth(window.innerWidth);
			const clamped = clampActivityWidth(widthRef.current, window.innerWidth);
			if (clamped !== widthRef.current) {
				widthRef.current = clamped;
				setWidth(clamped);
			}
		}
		window.addEventListener("resize", onResize);
		return () => window.removeEventListener("resize", onResize);
	}, []);

	const {
		data,
		hasNextPage,
		isFetchingNextPage,
		isFetchNextPageError,
		fetchNextPage,
	} = useInfiniteQuery({
		queryKey,
		queryFn: ({ pageParam }: { pageParam: string | undefined }) =>
			fetchPage(pageParam),
		initialPageParam: undefined as string | undefined,
		getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
	});

	// Pages arrive newest first; the pane reads oldest to newest.
	const sorted = useMemo(
		() =>
			(data?.pages.flatMap((p) => p.items) ?? []).sort(
				(a, b) =>
					new Date(a.created_at).getTime() - new Date(b.created_at).getTime(),
			),
		[data],
	);

	// An agent's multiple-choice comment counts as answered once anyone else
	// has commented after it.
	const answeredAskIds = useMemo(() => {
		const answered = new Set<string>();
		sorted.forEach((entry, i) => {
			if (entry.activity_type !== "comment") return;
			if (!extractAskQuestion(getCommentBlocks(entry.content))) return;
			const replied = sorted
				.slice(i + 1)
				.some(
					(later) =>
						later.activity_type === "comment" &&
						later.actor_id !== entry.actor_id,
				);
			if (replied) answered.add(entry.id);
		});
		return answered;
	}, [sorted, getCommentBlocks]);

	const hasNonCommentActivity = useMemo(
		() => sorted.some((entry) => entry.activity_type !== "comment"),
		[sorted],
	);

	const visible = useMemo(() => {
		if (filter === "all" || !hasNonCommentActivity) return sorted;
		return sorted.filter((entry) => entry.activity_type === "comment");
	}, [sorted, filter, hasNonCommentActivity]);

	const handleFilterChange = (next: ActivityFilter) => {
		setFilter(next);
		try {
			localStorage.setItem(ACTIVITY_FILTER_STORAGE_KEY, next);
		} catch {
			/* ignore */
		}
	};

	const getViewport = () =>
		scrollAreaRef.current?.querySelector(
			'[data-slot="scroll-area-viewport"]',
		) as HTMLElement | null;

	// Stick to the bottom when the newest entry changes (first load, a new
	// comment), but leave the reader where they are when older entries are
	// paged in above.
	const newestId = sorted[sorted.length - 1]?.id;
	// biome-ignore lint/correctness/useExhaustiveDependencies: only a change of the newest entry should scroll
	useEffect(() => {
		requestAnimationFrame(() => {
			const viewport = getViewport();
			if (viewport) viewport.scrollTop = viewport.scrollHeight;
		});
	}, [newestId]);

	// Keep the visible entries in place when older ones are prepended.
	const heightBeforeOlder = useRef<number | null>(null);
	const loadOlder = () => {
		heightBeforeOlder.current = getViewport()?.scrollHeight ?? null;
		void fetchNextPage();
	};
	const oldestId = sorted[0]?.id;
	// biome-ignore lint/correctness/useExhaustiveDependencies: runs when the oldest entry changes
	useLayoutEffect(() => {
		const viewport = getViewport();
		if (viewport && heightBeforeOlder.current !== null) {
			viewport.scrollTop += viewport.scrollHeight - heightBeforeOlder.current;
		}
		heightBeforeOlder.current = null;
	}, [oldestId]);

	const addMutation = useMutation({
		mutationFn: (blocks: unknown[]) => {
			if (!addComment) return Promise.resolve();
			return addComment(blocks);
		},
		onSuccess: () => {
			editorRef.current?.clear();
			setEditingCommentId(null);
			setEditorFocused(false);
			qc.invalidateQueries({ queryKey });
		},
	});

	// Posts the chosen answer as a reply that @-mentions the asking agent, so
	// the comment-mention trigger wakes it up with the answer.
	const answerAsk = async (entry: T, answer: string) => {
		if (!addComment) return;
		const members = await searchMentionableMembers(
			projectId,
			entry.actor_username,
		).catch(() => []);
		const agent = members.find((m) => m.username === entry.actor_username);
		const content: unknown[] = [];
		if (agent) {
			content.push(
				{
					type: "teamMention",
					props: { id: agent.id, name: agent.name, avatar: "" },
				},
				{ type: "text", text: " ", styles: {} },
			);
		}
		content.push({ type: "text", text: answer, styles: {} });
		await addMutation.mutateAsync([
			{
				type: "paragraph",
				props: {
					textColor: "default",
					backgroundColor: "default",
					textAlignment: "left",
				},
				content,
				children: [],
			},
		]);
	};

	const updateMutation = useMutation({
		mutationFn: ({
			commentId,
			blocks,
		}: {
			commentId: string;
			blocks: unknown[];
		}) => {
			if (!updateComment) return Promise.resolve();
			return updateComment(commentId, blocks);
		},
		onSuccess: () => {
			editorRef.current?.clear();
			setEditingCommentId(null);
			setEditorFocused(false);
			qc.invalidateQueries({ queryKey });
		},
	});

	const handleSend = () => {
		const blocks = editorRef.current?.getBlocks();
		if (!blocks || blocks.length === 0) return;
		const text = blocksToText(blocks).trim();
		if (!text) return;
		if (editingCommentId && updateComment) {
			updateMutation.mutate({ commentId: editingCommentId, blocks });
		} else {
			addMutation.mutate(blocks);
		}
	};

	const handleCancelEdit = () => {
		editorRef.current?.clear();
		setEditingCommentId(null);
		setEditorFocused(false);
	};

	const editingComment = editingCommentId
		? sorted.find((e) => e.id === editingCommentId)
		: null;
	const editingCommentBlocks =
		editingCommentId && editingComment
			? (getCommentBlocks(editingComment.content) ?? [])
			: [];

	return (
		<div
			className="relative flex w-full lg:w-(--activity-pane-width) lg:max-w-[50vw] lg:shrink-0 flex-col h-full lg:overflow-hidden border-t lg:border-t-0 lg:border-l border-border/25 bg-muted/10"
			style={{ "--activity-pane-width": `${width}px` } as CSSProperties}
		>
			{/* biome-ignore lint/a11y/useSemanticElements: draggable splitter */}
			<div
				role="separator"
				aria-orientation="vertical"
				aria-label={t("activityPane.resize")}
				aria-valuemin={ACTIVITY_WIDTH_MIN}
				aria-valuemax={maxWidth}
				aria-valuenow={width}
				tabIndex={0}
				data-testid="activity-pane-resize-handle"
				className="absolute inset-y-0 left-0 z-10 hidden w-1.5 cursor-col-resize touch-none select-none transition-colors hover:bg-primary/30 focus-visible:bg-primary/40 focus-visible:outline-none active:bg-primary/40 lg:block"
				onPointerDown={(e) => {
					if (e.button !== 0) return;
					e.currentTarget.setPointerCapture(e.pointerId);
					dragRef.current = { startX: e.clientX, startWidth: widthRef.current };
				}}
				onPointerMove={(e) => {
					const drag = dragRef.current;
					if (drag) applyWidth(drag.startWidth + (drag.startX - e.clientX));
				}}
				onPointerUp={endDrag}
				onPointerCancel={endDrag}
				onLostPointerCapture={endDrag}
				onDoubleClick={() => {
					applyWidth(ACTIVITY_WIDTH_DEFAULT);
					storeActivityWidth(widthRef.current);
				}}
				onKeyDown={(e) => {
					let next: number;
					if (e.key === "ArrowLeft")
						next = widthRef.current + ACTIVITY_WIDTH_KEY_STEP;
					else if (e.key === "ArrowRight")
						next = widthRef.current - ACTIVITY_WIDTH_KEY_STEP;
					else if (e.key === "Home") next = ACTIVITY_WIDTH_MIN;
					else if (e.key === "End") next = Number.POSITIVE_INFINITY;
					else return;
					e.preventDefault();
					applyWidth(next);
				}}
				onKeyUp={(e) => {
					if (["ArrowLeft", "ArrowRight", "Home", "End"].includes(e.key))
						storeActivityWidth(widthRef.current);
				}}
			/>
			<div className="flex shrink-0 flex-col gap-2 border-b border-border/25 px-5 py-3 bg-muted/20">
				<div className="flex items-center gap-2.5">
					<MessageSquare className="size-3.5 text-muted-foreground/70" />
					<span className="text-xs font-semibold text-muted-foreground uppercase tracking-wide">
						{t("activityPane.title")}
					</span>
					{visible.length > 0 && (
						<span className="ml-auto rounded-full bg-muted/60 px-2 py-0.5 text-xs font-bold text-muted-foreground/70 tabular-nums">
							{visible.length}
							{hasNextPage ? "+" : ""}
						</span>
					)}
				</div>
				{hasNonCommentActivity && (
					<fieldset
						aria-label={t("activityPane.filter.label")}
						className="flex items-center gap-0.5 rounded-lg border border-border/30 bg-background/60 p-0.5"
					>
						<button
							type="button"
							aria-pressed={filter === "comments"}
							onClick={() => handleFilterChange("comments")}
							className={cn(
								"flex-1 rounded-md px-2 py-1 text-xs font-medium transition-all duration-150",
								filter === "comments"
									? "bg-card text-foreground shadow-sm"
									: "text-muted-foreground/60 hover:text-foreground/80",
							)}
						>
							{t("activityPane.filter.comments")}
						</button>
						<button
							type="button"
							aria-pressed={filter === "all"}
							onClick={() => handleFilterChange("all")}
							className={cn(
								"flex-1 rounded-md px-2 py-1 text-xs font-medium transition-all duration-150",
								filter === "all"
									? "bg-card text-foreground shadow-sm"
									: "text-muted-foreground/60 hover:text-foreground/80",
							)}
						>
							{t("activityPane.filter.all")}
						</button>
					</fieldset>
				)}
			</div>

			<ScrollArea
				ref={scrollAreaRef}
				className="lg:flex-1 lg:min-h-0 px-4 py-4"
			>
				<div className="space-y-3">
					<LoadMoreSentinel
						compact
						hasMore={!!hasNextPage}
						isLoadingMore={isFetchingNextPage}
						isError={isFetchNextPageError}
						onLoadMore={loadOlder}
					/>
					{sorted.length === 0 && (
						<div className="flex flex-col items-center py-8 text-muted-foreground/40">
							<MessageSquare className="size-6 mb-2" />
							<p className="text-xs font-medium">{t("activityPane.empty")}</p>
						</div>
					)}
					{sorted.length > 0 && visible.length === 0 && (
						<div className="flex flex-col items-center py-8 text-muted-foreground/40">
							<MessageSquare className="size-6 mb-2" />
							<p className="text-xs font-medium">
								{t("activityPane.emptyComments")}
							</p>
							<button
								type="button"
								onClick={() => handleFilterChange("all")}
								className="mt-2 text-xs font-medium text-primary hover:underline"
							>
								{t("activityPane.showAllActivity")}
							</button>
						</div>
					)}
					{visible.map((entry) => (
						<ActivityItemInner
							projectId={projectId}
							key={entry.id}
							entry={entry}
							describeActivity={describeActivity}
							getCommentBlocks={getCommentBlocks}
							updateComment={updateComment}
							deleteComment={deleteComment}
							onRevert={onRevert}
							getDiffContent={getDiffContent}
							isRevertable={isRevertable}
							queryKey={queryKey}
							currentUserId={currentUserId}
							editingCommentId={editingCommentId}
							askAnswered={answeredAskIds.has(entry.id)}
							onAnswerAsk={addComment ? answerAsk : undefined}
							onStartEdit={(commentId) => {
								setEditingCommentId(commentId);
								editorRef.current?.focus();
							}}
						/>
					))}
				</div>
			</ScrollArea>

			{addComment && (
				<div className="shrink-0 border-t border-border/25 p-3 space-y-1 bg-background/50">
					{editingCommentId && (
						<div className="flex items-center gap-2 px-1 pb-1">
							<span className="text-xs font-medium text-foreground/70">
								{t("activityPane.editingComment")}
							</span>
							<Button
								variant="ghost"
								size="sm"
								className="h-5 text-xs rounded-md px-2"
								onClick={handleCancelEdit}
							>
								{t("activityPane.cancel")}
							</Button>
						</div>
					)}
					<fieldset
						className={cn(
							"min-w-0 rounded-xl border border-border/30 bg-card/80 transition-all duration-200 overflow-hidden",
							editorFocused && "border-primary/25 shadow-sm shadow-primary/5",
							"[&_.bn-editor]:min-h-6 [&_.bn-editor]:max-h-48 [&_.bn-editor]:overflow-y-auto [&_.bn-editor]:py-1.5 [&_.bn-editor]:px-3 [&_.bn-editor]:text-sm [&_.bn-editor]:leading-relaxed",
						)}
						onFocus={() => setEditorFocused(true)}
						onBlur={(e) => {
							if (!e.currentTarget.contains(e.relatedTarget as Node)) {
								const blocks = editorRef.current?.getBlocks() ?? [];
								const text = blocksToText(blocks).trim();
								if (!text) setEditorFocused(false);
							}
						}}
					>
						<CommentEditor
							key={editingCommentId}
							ref={editorRef}
							initialBlocks={editingCommentBlocks}
							onSubmit={handleSend}
							projectId={projectId}
						/>
					</fieldset>
					<div className="flex items-center justify-between">
						{editorFocused && (
							<p className="text-xs text-muted-foreground/40 pl-1">
								{t("activityPane.sendHint")}
							</p>
						)}
						<button
							type="button"
							onClick={handleSend}
							disabled={addMutation.isPending || updateMutation.isPending}
							className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-primary text-primary-foreground disabled:opacity-40 hover:bg-primary/90 transition-all duration-150 shadow-sm disabled:shadow-none ml-auto"
						>
							{addMutation.isPending || updateMutation.isPending ? (
								<Loader2 className="size-3 animate-spin" />
							) : (
								<Send className="size-3" />
							)}
						</button>
					</div>
				</div>
			)}
		</div>
	);
}

interface ActivityItemInnerProps<T extends ActivityEntry> {
	projectId: string;
	entry: T;
	describeActivity: (entry: T) => ReactNode;
	getCommentBlocks: (content: T["content"]) => unknown[] | null;
	updateComment?: (commentId: string, blocks: unknown[]) => Promise<unknown>;
	deleteComment?: (commentId: string) => Promise<void>;
	onRevert?: (entry: T) => Promise<void>;
	getDiffContent?: (
		entry: T,
	) => { old: unknown; new: unknown; title?: string } | null;
	isRevertable?: (entry: T) => boolean;
	queryKey: QueryKey;
	currentUserId?: string;
	editingCommentId: string | null;
	onStartEdit: (commentId: string) => void;
	askAnswered: boolean;
	onAnswerAsk?: (entry: T, answer: string) => Promise<void>;
}

function ActivityItemInner<T extends ActivityEntry>({
	projectId,
	entry,
	describeActivity,
	getCommentBlocks,
	updateComment,
	deleteComment,
	onRevert,
	getDiffContent,
	isRevertable,
	queryKey,
	currentUserId,
	editingCommentId,
	onStartEdit,
	askAnswered,
	onAnswerAsk,
}: ActivityItemInnerProps<T>) {
	const { t } = useTranslation("shared");
	const { t: tCommon } = useTranslation("common");
	const qc = useQueryClient();
	const rawCommentBlocks = getCommentBlocks(entry.content);
	const askComment = extractAskQuestion(rawCommentBlocks);
	const commentBlocks = askComment ? askComment.rest : rawCommentBlocks;
	const [diffOpen, setDiffOpen] = useState(false);
	const [revertPending, setRevertPending] = useState(false);
	const [revertError, setRevertError] = useState<string | null>(null);

	const isComment = entry.activity_type === "comment";
	const displayName =
		entry.actor_name || entry.actor_username || t("activityPane.systemActor");
	const resolveActor = useActorAvatar(projectId);

	const isOwnComment =
		isComment &&
		!!currentUserId &&
		!!entry.actor_id &&
		String(entry.actor_id) === String(currentUserId);
	const canEdit = isOwnComment && !!updateComment;
	const canDelete = isOwnComment && !!deleteComment;

	const isEditing = editingCommentId === entry.id;

	const diffContent =
		!isComment && getDiffContent ? getDiffContent(entry) : null;
	const canRevert =
		!isComment && !!onRevert && (isRevertable ? isRevertable(entry) : false);

	const deleteMutation = useMutation({
		mutationFn: () => {
			// biome-ignore lint/style/noNonNullAssertion: guarded by canDelete
			return deleteComment!(entry.id);
		},
		onSuccess: () => {
			qc.invalidateQueries({ queryKey });
		},
	});

	const handleRevert = async () => {
		if (!onRevert) return;
		setRevertPending(true);
		setRevertError(null);
		try {
			await onRevert(entry);
			qc.invalidateQueries({ queryKey });
		} catch {
			setRevertError(t("activityPane.errors.revertFailed"));
		} finally {
			setRevertPending(false);
		}
	};

	return (
		<div className="flex gap-3">
			<EntityAvatar
				{...resolveActor({
					id: entry.actor_id,
					name: entry.actor_name || entry.actor_username,
					avatarUrl: entry.actor_avatar_thumb_url,
				})}
				fallback={<Cog className="size-3" />}
				size="sm"
				className="mt-0.5 shrink-0"
			/>
			<div className="flex-1 min-w-0">
				{isComment ? (
					<div className="group rounded-xl rounded-tl-lg border border-border/25 bg-card/70 px-3.5 py-2.5">
						<div className="mb-1 flex items-center gap-2">
							<span className="text-xs font-semibold text-foreground">
								{displayName}
							</span>
							<span className="text-xs text-muted-foreground/50">
								{timeAgo(entry.created_at, tCommon)}
							</span>
							{isEditing && (
								<span className="ml-auto flex items-center gap-1 text-xs font-medium text-primary">
									<span className="relative flex size-2">
										<span className="animate-ping absolute inline-flex size-full rounded-full bg-primary/40 opacity-75"></span>
										<span className="relative inline-flex size-2 rounded-full bg-primary"></span>
									</span>
									{t("activityPane.editing")}
								</span>
							)}
							{canEdit && canDelete && !isEditing && (
								<DropdownMenu>
									<DropdownMenuTrigger className="inline-flex items-center justify-center ml-auto size-5 rounded-md text-muted-foreground/60 hover:text-foreground hover:bg-muted/60 opacity-0 group-hover:opacity-100 transition-all duration-150">
										<MoreHorizontal className="size-3" />
									</DropdownMenuTrigger>
									<DropdownMenuContent align="end" className="w-36">
										<DropdownMenuItem onClick={() => onStartEdit(entry.id)}>
											<Pencil className="size-3.5 mr-2" />
											{t("activityPane.edit")}
										</DropdownMenuItem>
										<DropdownMenuItem
											className="text-destructive focus:text-destructive"
											onClick={() => deleteMutation.mutate()}
										>
											<Trash2 className="size-3.5 mr-2" />
											{t("activityPane.delete")}
										</DropdownMenuItem>
									</DropdownMenuContent>
								</DropdownMenu>
							)}
						</div>

						{commentBlocks && commentBlocks.length > 0 ? (
							<div className="[&_.bn-editor]:text-sm [&_.bn-editor]:leading-relaxed [&_.bn-editor]:p-0">
								<CommentDisplay blocks={commentBlocks} />
							</div>
						) : (
							!askComment && (
								<p className="text-sm text-foreground leading-relaxed">
									{blocksToText(commentBlocks ?? [])}
								</p>
							)
						)}
						{askComment && (
							<AskQuestionCard
								ask={askComment.ask}
								className="mt-2"
								disabled={askAnswered || !onAnswerAsk}
								onAnswer={(answer) => onAnswerAsk?.(entry, answer)}
							/>
						)}
					</div>
				) : (
					<div className="flex flex-col min-w-0">
						<div className="group flex items-start gap-1 py-0.5">
							<div className="flex-1 min-w-0 flex flex-wrap items-center gap-1.5">
								<span className="text-xs font-medium text-foreground/80">
									{displayName}
								</span>
								<span className="text-xs text-muted-foreground/70">
									{describeActivity(entry)}
								</span>
								<span className="text-xs text-muted-foreground/45">
									{timeAgo(entry.created_at, tCommon)}
								</span>
							</div>
							{(diffContent || canRevert) && (
								<>
									<DropdownMenu>
										<DropdownMenuTrigger className="shrink-0 inline-flex items-center justify-center size-5 rounded-md text-muted-foreground/50 hover:text-foreground hover:bg-muted/60 opacity-0 group-hover:opacity-100 transition-all duration-150">
											<MoreVertical className="size-3" />
										</DropdownMenuTrigger>
										<DropdownMenuContent align="end" className="w-40">
											{diffContent && (
												<DropdownMenuItem onClick={() => setDiffOpen(true)}>
													<GitBranch className="size-3.5 mr-2" />
													{t("activityPane.viewDiff")}
												</DropdownMenuItem>
											)}
											{canRevert && (
												<DropdownMenuItem
													onClick={handleRevert}
													disabled={revertPending}
												>
													{revertPending ? (
														<Loader2 className="size-3.5 mr-2 animate-spin" />
													) : (
														<RotateCcw className="size-3.5 mr-2" />
													)}
													{t("activityPane.revert")}
												</DropdownMenuItem>
											)}
										</DropdownMenuContent>
									</DropdownMenu>
									{diffContent && (
										<ContentDiffDialog
											open={diffOpen}
											onOpenChange={setDiffOpen}
											oldContent={diffContent.old}
											newContent={diffContent.new}
											title={diffContent.title ?? t("contentDiff.changeDiff")}
										/>
									)}
								</>
							)}
						</div>
						{revertError && (
							<p className="text-xs text-destructive/70 pl-0.5">
								{revertError}
							</p>
						)}
					</div>
				)}
			</div>
		</div>
	);
}
