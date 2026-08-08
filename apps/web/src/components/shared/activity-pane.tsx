import {
	type QueryKey,
	useMutation,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import {
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
import { type ReactNode, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
	blocksToText,
	CommentDisplay,
	CommentEditor,
	type CommentEditorHandle,
} from "@/components/shared/comment-blocknote";
import { ContentDiffDialog } from "@/components/shared/content-diff";
import { EntityAvatarContent } from "@/components/shared/entity-avatar";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { ScrollArea } from "@/components/ui/scroll-area";
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

export interface ActivityPaneConfig<T extends ActivityEntry> {
	projectId: string;
	entityId: string;
	queryKey: QueryKey;
	queryFn: () => Promise<T[]>;
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
	sortAscending?: boolean;
	nameMaps?: Record<string, Record<string, string>>;
	currentUserId?: string;
}

export function ActivityPane<T extends ActivityEntry>({
	projectId,
	queryKey,
	queryFn,
	addComment,
	updateComment,
	deleteComment,
	onRevert,
	getDiffContent,
	isRevertable,
	describeActivity,
	getCommentBlocks,
	sortAscending = false,
	currentUserId,
}: ActivityPaneConfig<T>) {
	const { t } = useTranslation("shared");
	const editorRef = useRef<CommentEditorHandle>(null);
	const scrollAreaRef = useRef<HTMLDivElement>(null);
	const [editorFocused, setEditorFocused] = useState(false);
	const [editingCommentId, setEditingCommentId] = useState<string | null>(null);
	const qc = useQueryClient();

	const { data: activities = [] } = useQuery({
		queryKey,
		queryFn,
	});

	const sorted = useMemo(() => {
		if (!sortAscending) return activities;
		return [...activities].sort(
			(a, b) =>
				new Date(a.created_at).getTime() - new Date(b.created_at).getTime(),
		);
	}, [activities, sortAscending]);

	// biome-ignore lint/correctness/useExhaustiveDependencies: activities is needed to scroll when new items are added
	useEffect(() => {
		requestAnimationFrame(() => {
			const viewport = scrollAreaRef.current?.querySelector(
				'[data-slot="scroll-area-viewport"]',
			) as HTMLElement;
			if (viewport) {
				viewport.scrollTop = viewport.scrollHeight;
			}
		});
	}, [sorted]);

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
		<div className="flex w-full lg:w-80 lg:shrink-0 flex-col h-full lg:overflow-hidden border-t lg:border-t-0 lg:border-l border-border/25 bg-muted/10">
			<div className="flex shrink-0 items-center gap-2.5 border-b border-border/25 px-5 py-3 bg-muted/20">
				<MessageSquare className="size-3.5 text-muted-foreground/70" />
				<span className="text-xs font-semibold text-muted-foreground uppercase tracking-wide">
					{t("activityPane.title")}
				</span>
				{sorted.length > 0 && (
					<span className="ml-auto rounded-full bg-muted/60 px-2 py-0.5 text-xs font-bold text-muted-foreground/70 tabular-nums">
						{sorted.length}
					</span>
				)}
			</div>

			<ScrollArea
				ref={scrollAreaRef}
				className="lg:flex-1 lg:min-h-0 px-4 py-4"
			>
				<div className="space-y-3">
					{sorted.length === 0 && (
						<div className="flex flex-col items-center py-8 text-muted-foreground/40">
							<MessageSquare className="size-6 mb-2" />
							<p className="text-xs font-medium">{t("activityPane.empty")}</p>
						</div>
					)}
					{sorted.map((entry) => (
						<ActivityItemInner
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
							"rounded-xl border border-border/30 bg-card/80 transition-all duration-200 overflow-hidden",
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
}

function ActivityItemInner<T extends ActivityEntry>({
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
}: ActivityItemInnerProps<T>) {
	const { t } = useTranslation("shared");
	const { t: tCommon } = useTranslation("common");
	const qc = useQueryClient();
	const commentBlocks = getCommentBlocks(entry.content);
	const [diffOpen, setDiffOpen] = useState(false);
	const [revertPending, setRevertPending] = useState(false);
	const [revertError, setRevertError] = useState<string | null>(null);

	const isComment = entry.activity_type === "comment";
	const displayName =
		entry.actor_name || entry.actor_username || t("activityPane.systemActor");
	const initial = displayName.slice(0, 1).toUpperCase();

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
			<div
				className={cn(
					"flex size-6 shrink-0 items-center justify-center rounded-full text-xs font-bold mt-0.5 ring-1",
					isComment
						? "bg-linear-to-br from-primary/20 to-primary/10 text-primary ring-primary/15"
						: "bg-muted/40 text-muted-foreground/80 ring-border/20",
				)}
			>
				<EntityAvatarContent avatarUrl={entry.actor_avatar_thumb_url}>
					{initial}
				</EntityAvatarContent>
			</div>
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
							<p className="text-sm text-foreground leading-relaxed">
								{blocksToText(commentBlocks ?? [])}
							</p>
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
