import { LoadMoreSentinel } from "@/components/shared/load-more-sentinel";
import { Input } from "@/components/ui/input";
import { useAgentSearch } from "@/hooks/use-agent-search";
import "@blocknote/core/fonts/inter.css";
import "@blocknote/shadcn/style.css";

import {
	FilePanelController,
	SideMenuController,
	useCreateBlockNote,
} from "@blocknote/react";
import { BlockNoteView } from "@blocknote/shadcn";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Bot, Sparkles } from "lucide-react";
import {
	lazy,
	Suspense,
	useCallback,
	useEffect,
	useMemo,
	useRef,
	useState,
} from "react";
import { useTranslation } from "react-i18next";
import { createAnnotationPasteHandler } from "@/components/shared/blocknote-annotation-paste-handler";
import { CustomSideMenu } from "@/components/shared/blocknote-custom-side-menu";
import {
	FileBlockPanel,
	FilePanelScopeContext,
} from "@/components/shared/blocknote-file-panel";
import { customSchema } from "@/components/shared/blocknote-schema";
import { normalizeBlockContent } from "@/components/shared/comment-blocknote";
import { MentionSuggestionMenus } from "@/components/shared/mention-suggestion-menus";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { useCollabSession } from "@/hooks/use-collab-session";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import { useThemeMode } from "@/hooks/use-theme-mode";
import { writeTaskDescriptionWithAI } from "@/lib/agent-api";
import {
	attachmentAsFileNode,
	getAttachmentDownloadURL,
	getTaskAttachment,
	uploadAttachment,
} from "@/lib/attachment-api";
import {
	type CollabSession,
	type CollabStatus,
	seedCollabDoc,
} from "@/lib/collab-session";
import { type FileNode, getNode, getNodeDownload } from "@/lib/files-api";
import {
	ATTACHMENT_SCHEME,
	collectInlineMedia,
	type InlineMediaRef,
	parseMediaUri,
} from "@/lib/inline-media";
import { cleanBlocks, cn, isRemoteOnlyChange } from "@/lib/utils";

type UpdateFn = (payload: { description?: unknown[] | null }) => void;

interface DescriptionSectionProps {
	description?: unknown[] | null;
	canEdit?: boolean;
	projectId?: string;
	taskId?: string;
	onUpdate?: UpdateFn;
}

// The viewer (and its heavy document libraries) is fetched only when a preview opens.
const FilePreview = lazy(() => import("@/components/files/file-preview"));

/**
 * Task description. Editors join a real-time Yjs room for the task (see
 * lib/collab-session.ts); when it cannot be joined this falls back to the
 * plain, single-user editor.
 */
export function DescriptionSection(props: DescriptionSectionProps) {
	const collab = useCollabSession(
		"task",
		props.projectId,
		props.taskId,
		(props.canEdit ?? true) && !!props.projectId && !!props.taskId,
	);
	if (collab.phase === "connecting") {
		return (
			<div className="min-h-24 rounded-xl border border-border/25 bg-card/50" />
		);
	}
	return (
		<DescriptionSectionInner
			// The editor is built once with or without the Yjs binding.
			key={collab.session ? "collab" : "plain"}
			{...props}
			collab={collab.session}
			collabStatus={collab.phase === "ready" ? collab.status : null}
		/>
	);
}

function DescriptionSectionInner({
	description,
	canEdit: canEditProp = true,
	projectId,
	taskId,
	onUpdate,
	collab,
	collabStatus,
}: DescriptionSectionProps & {
	collab: CollabSession | null;
	collabStatus: CollabStatus | null;
}) {
	// A revoked session must stop accepting edits: they would never sync.
	const canEdit = canEditProp && collabStatus !== "lost";
	// Set when this user asked an agent to write the description: the agent's
	// REST write is then applied once, by this client, into the shared doc.
	const aiWriteRef = useRef(false);
	// True while the stored content is being written into the shared doc.
	const seedingRef = useRef(false);
	// Whether the component is mounted. A ref flag rather than cancelling the
	// seed in a cleanup: React StrictMode runs that cleanup once on mount and
	// the seed effect does not run again, which would leave the description
	// blank in development.
	const mountedRef = useRef(false);
	const seedUnsubRef = useRef<(() => void) | null>(null);
	useEffect(() => {
		mountedRef.current = true;
		return () => {
			mountedRef.current = false;
			// StrictMode re-mounts synchronously, so only a real unmount is still
			// unmounted by the time this runs: then drop the seed listener.
			setTimeout(() => {
				if (!mountedRef.current) seedUnsubRef.current?.();
			}, 0);
		};
	}, []);
	const { t } = useTranslation("projects");
	const { resolvedMode } = useThemeMode();
	const qc = useQueryClient();
	const [writeWithAIOpen, setWriteWithAIOpen] = useState(false);
	const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null);

	// The dialog's agent list is searched and paged on the server once it opens.
	const agentSearch = useAgentSearch(projectId, !!projectId && writeWithAIOpen);
	const agents = agentSearch.agents;

	const writeWithAIMutation = useMutation({
		meta: { silent: true },
		mutationFn: () => {
			if (!projectId || !taskId || !selectedAgentId)
				throw new Error("missing context");
			return writeTaskDescriptionWithAI(projectId, taskId, selectedAgentId);
		},
		onSuccess: () => {
			aiWriteRef.current = true;
			// Mirrors the assign-to-agent / mention-agent flows: don't rely solely
			// on the realtime websocket event to surface the new "started an AI
			// session" activity — refresh immediately so it shows without a reload.
			if (projectId) {
				qc.invalidateQueries({
					queryKey: ["projects", projectId],
					predicate: (q) => {
						const key = q.queryKey as string[];
						return key.includes("tasks") || key.includes("backlog-tasks");
					},
				});
			}
			setWriteWithAIOpen(false);
			setSelectedAgentId(null);
		},
	});

	// Tracks the last value we wrote to the API, to avoid redundant saves and
	// to skip external refetch updates that match what we already have.
	const lastSavedRef = useRef<string | null>(null);
	// Whether the editor has been initialized with the description content.
	const initializedRef = useRef(false);
	// Whether there are unsaved changes pending a blur-save.
	const pendingRef = useRef(false);
	// Whether the editor has finished initial content population.
	const readyRef = useRef(false);

	// Keep projectId / taskId in refs so the stable editor callbacks always
	// reference the latest prop values without recreating the editor.
	const projectIdRef = useRef(projectId);
	const taskIdRef = useRef(taskId);
	useEffect(() => {
		projectIdRef.current = projectId;
	}, [projectId]);
	useEffect(() => {
		taskIdRef.current = taskId;
	}, [taskId]);

	const editor = useCreateBlockNote(
		{
			schema: customSchema,
			...(collab && {
				collaboration: {
					fragment: collab.fragment,
					provider: collab.provider,
					user: collab.user,
					showCursorLabels: "activity" as const,
				},
			}),
			pasteHandler: createAnnotationPasteHandler(),
			/**
			 * Called by BlockNote when the user inserts an image / file / video / audio.
			 * Uploads via the task attachment API and returns a stable custom URI.
			 */
			uploadFile: async (file: File) => {
				const pId = projectIdRef.current;
				const tId = taskIdRef.current;
				if (!pId || !tId) throw new Error("No project/task context for upload");
				const attachment = await uploadAttachment(pId, tId, file);
				// Store as a stable custom URI; presigned URL is fetched on-demand.
				return `${ATTACHMENT_SCHEME}${pId}/${tId}/${attachment.id}`;
			},

			/**
			 * Called by BlockNote whenever it needs to display a file URL.
			 * Converts our custom `attachment://` URI into a fresh presigned URL.
			 */
			resolveFileUrl: async (url: string) => {
				const ref = parseMediaUri(url);
				if (!ref) return url;
				// attachment://{projectId}/{taskId}/{attachmentId} or
				// files://{projectId}/{fileId} (a file picked from the project's Files).
				return ref.source === "attachment"
					? getAttachmentDownloadURL(ref.projectId, ref.taskId, ref.id)
					: getNodeDownload({ projectId: ref.projectId }, ref.id).then(
							(d) => d.url,
						);
			},
		},
		[collab],
	);

	// Populate the editor from BlockNote JSON whenever description changes
	// externally (initial load or server refetch that differs from what we saved).
	useEffect(() => {
		// Normalize whatever the API returned (including legacy/invalid content
		// that isn't a block array, e.g. a plain string) into a safe block array
		// so BlockNote never receives data it can't render.
		const displayBlocks = normalizeBlockContent(description ?? null);
		const cleanedNormalized = cleanBlocks(
			displayBlocks.length > 0 ? displayBlocks : null,
		);
		// Stringify for stable comparison (array identity changes on every response)
		const normalizedStr = cleanedNormalized
			? JSON.stringify(cleanedNormalized)
			: null;
		if (initializedRef.current && normalizedStr === lastSavedRef.current)
			return;
		if (collab && initializedRef.current && !aiWriteRef.current) {
			// The shared Y.Doc is the source of truth while collaborating; a
			// refetch caused by a peer's save must not be replayed into it by
			// every open editor (concurrent replaceBlocks would duplicate).
			return;
		}
		const firstRun = !initializedRef.current;
		aiWriteRef.current = false;
		initializedRef.current = true;
		lastSavedRef.current = normalizedStr;
		readyRef.current = false;

		// Collaborating: only the client the room picked writes the stored
		// content (once, into the empty room); later we only apply an AI write
		// this user asked for.
		const blocks = displayBlocks as Parameters<typeof editor.replaceBlocks>[1];
		if (!collab) {
			editor.replaceBlocks(editor.document, blocks);
		} else if (firstRun) {
			if (displayBlocks.length > 0) {
				// Runs now when this client was picked to seed, or later if the
				// picked client left or stalled and the grant moves here.
				seedUnsubRef.current = collab.whenSeeder(() => {
					seedCollabDoc(
						collab,
						() => {
							seedingRef.current = true;
							editor.replaceBlocks(editor.document, blocks);
							queueMicrotask(() => {
								seedingRef.current = false;
							});
						},
						{ isActive: () => mountedRef.current },
					);
				});
			}
		} else {
			editor.replaceBlocks(editor.document, blocks);
		}
		queueMicrotask(() => {
			readyRef.current = true;
		});
	}, [description, editor, collab]);

	const handleChange = useCallback(
		(_editor: unknown, ctx?: Parameters<typeof isRemoteOnlyChange>[0]) => {
			if (!canEdit || !readyRef.current || seedingRef.current) return;
			// A peer's edit is saved by that peer, not by every open editor.
			if (collab && isRemoteOnlyChange(ctx)) return;
			// Track dirty state so blur can save without re-reading document
			pendingRef.current = true;
		},
		[canEdit, collab],
	);

	const save = useCallback(() => {
		if (!canEdit || !pendingRef.current) return;
		pendingRef.current = false;
		const blocks = editor.document;
		// Consider empty when there is only one empty paragraph block
		const isEmpty =
			blocks.length === 1 &&
			blocks[0].type === "paragraph" &&
			Array.isArray(blocks[0].content) &&
			blocks[0].content.length === 0;

		const value: unknown[] | null = isEmpty ? null : (blocks as unknown[]);
		const cleanedValue = cleanBlocks(value);
		const valueStr = cleanedValue ? JSON.stringify(cleanedValue) : null;
		if (valueStr !== lastSavedRef.current) {
			lastSavedRef.current = valueStr;
			onUpdate?.({ description: cleanedValue });
		}
	}, [canEdit, editor, onUpdate]);

	// Clicking an inline image that is a task attachment opens the in-app
	// preview, across all such images of the description. Images with any other
	// source keep BlockNote's own behaviour.
	const [preview, setPreview] = useState<{
		files: FileNode[];
		index: number;
		where: Map<string, InlineMediaRef>;
	} | null>(null);
	/**
	 * Opens the preview on the image or file block `blockId`, across all of the
	 * description's attachment and project-file blocks.
	 */
	const openPreview = useCallback(
		async (blockId: string) => {
			const refs = collectInlineMedia(editor.document);
			const clicked = refs.findIndex((r) => r.blockId === blockId);
			if (clicked < 0) return false;
			const results = await Promise.allSettled(
				refs.map((r) =>
					r.source === "attachment"
						? getTaskAttachment(r.projectId, r.taskId, r.id).then((a) =>
								attachmentAsFileNode(a, r.projectId),
							)
						: getNode({ projectId: r.projectId }, r.id),
				),
			);
			// Files that no longer exist are skipped; the clicked one must load.
			if (results[clicked].status !== "fulfilled") return false;
			const files: FileNode[] = [];
			const where = new Map<string, InlineMediaRef>();
			let index = 0;
			results.forEach((res, i) => {
				if (res.status !== "fulfilled") return;
				if (i === clicked) index = files.length;
				files.push(res.value);
				where.set(res.value.id, refs[i]);
			});
			setPreview({ files, index, where });
			return true;
		},
		[editor],
	);

	// Clicking an image or a file block that is a task attachment, or one
	// chosen from the project's Files, opens the in-app preview. Anything else
	// (an external link) keeps BlockNote's own behaviour. Captured so it runs
	// before the block's own click handling.
	const handleMediaClick = useCallback(
		(e: React.MouseEvent<HTMLDivElement>) => {
			const target = e.target as HTMLElement;
			const isImage = target.tagName === "IMG";
			const isFile = !!target.closest(".bn-file-name-with-icon");
			if (!isImage && !isFile) return;
			const blockId = target.closest("[data-id]")?.getAttribute("data-id");
			if (!blockId) return;
			const block = editor.getBlock(blockId);
			if (!block || !parseMediaUri((block.props as { url?: string })?.url))
				return;
			e.preventDefault();
			e.stopPropagation();
			void openPreview(blockId);
		},
		[editor, openPreview],
	);

	// Picking from the project's Files needs files:read; without it the panel
	// only offers upload and embed.
	const { hasProjectPermission } = useProjectPermissions(projectId ?? "");
	const filePanelScope = useMemo(
		() => ({
			projectId,
			canPickFiles: !!projectId && hasProjectPermission("files:read"),
		}),
		[projectId, hasProjectPermission],
	);

	// Save when focus leaves the entire editor container (mirrors title onBlur).
	const handleBlur = useCallback(
		(e: React.FocusEvent<HTMLDivElement>) => {
			// relatedTarget is the element receiving focus next.
			// If it's still inside this container, it's an internal focus move — don't save.
			if (e.currentTarget.contains(e.relatedTarget as Node)) return;
			save();
		},
		[save],
	);

	// Mod+S saves immediately instead of waiting for blur (mirrors doc-editor.tsx).
	const handleKeyDown = useCallback(
		(e: React.KeyboardEvent<HTMLDivElement>) => {
			if ((e.ctrlKey || e.metaKey) && e.key === "s") {
				e.preventDefault();
				save();
			}
		},
		[save],
	);

	return (
		<div className="space-y-3">
			<div className="flex items-center justify-between">
				<h3 className="text-xs font-semibold uppercase tracking-[0.08em] text-muted-foreground/70 flex items-center gap-2">
					<span>{t("taskDetail.description.title")}</span>
					<div className="flex-1 h-px bg-linear-to-r from-border/40 to-transparent" />
				</h3>
				{canEdit && (
					<button
						type="button"
						className="flex items-center gap-1.5 text-xs text-muted-foreground/60 hover:text-muted-foreground transition-colors duration-150 font-medium"
						onClick={() => setWriteWithAIOpen(true)}
					>
						<Sparkles className="size-3" />
						{t("taskDetail.description.writeWithAI")}
					</button>
				)}
			</div>

			{/* biome-ignore lint/a11y/noStaticElementInteractions: wrapper captures blur and image/file clicks from the BlockNote rich-text editor */}
			<div
				className="rounded-xl border border-border/25 bg-card/50 hover:border-border/50 transition-all duration-200 overflow-hidden [&_.bn-editor]:min-h-20 [&_.bn-editor]:py-3 [&_.bn-editor]:text-base [&_.bn-editor]:leading-relaxed"
				onBlur={handleBlur}
				onKeyDown={handleKeyDown}
				onClickCapture={handleMediaClick}
			>
				<BlockNoteView
					editor={editor}
					editable={canEdit}
					onChange={handleChange}
					theme={resolvedMode}
					className="bn-shadcn"
					sideMenu={false}
					// MentionSuggestionMenus below provides the "/" menu (with the
					// extra items); leaving BlockNote's own on made two menus open
					// on top of each other.
					slashMenu={false}
					filePanel={false}
				>
					<SideMenuController sideMenu={CustomSideMenu} />
					<FilePanelScopeContext.Provider value={filePanelScope}>
						<FilePanelController filePanel={FileBlockPanel} />
					</FilePanelScopeContext.Provider>
					{canEdit && (
						<MentionSuggestionMenus editor={editor} projectId={projectId} />
					)}
				</BlockNoteView>
			</div>

			{preview && (
				<Suspense fallback={null}>
					<FilePreview
						files={preview.files}
						index={preview.index}
						scope={{ projectId: preview.files[0]?.project_id ?? null }}
						onIndexChange={(i) =>
							setPreview((p) => (p ? { ...p, index: i } : p))
						}
						onClose={() => setPreview(null)}
						resolveUrl={(f) => {
							const r = preview.where.get(f.id);
							if (!r) return Promise.reject(new Error("unknown file"));
							return r.source === "attachment"
								? getAttachmentDownloadURL(r.projectId, r.taskId, f.id)
								: getNodeDownload({ projectId: r.projectId }, f.id).then(
										(d) => d.url,
									);
						}}
						onDownload={(f) => {
							const r = preview.where.get(f.id);
							if (!r) return;
							const url =
								r.source === "attachment"
									? getAttachmentDownloadURL(r.projectId, r.taskId, f.id, {
											download: true,
										})
									: getNodeDownload({ projectId: r.projectId }, f.id, {
											download: true,
										}).then((d) => d.url);
							void url.then((u) =>
								window.open(u, "_blank", "noopener,noreferrer"),
							);
						}}
					/>
				</Suspense>
			)}

			<Dialog
				open={writeWithAIOpen}
				onOpenChange={(open) => {
					setWriteWithAIOpen(open);
					if (!open) setSelectedAgentId(null);
				}}
			>
				<DialogContent className="sm:max-w-md">
					<DialogHeader>
						<DialogTitle className="flex items-center gap-2">
							<Sparkles className="size-4 text-muted-foreground" />
							{t("taskDetail.description.writeWithAIDialog.title")}
						</DialogTitle>
						<DialogDescription>
							{t("taskDetail.description.writeWithAIDialog.description")}
						</DialogDescription>
					</DialogHeader>

					<div className="space-y-2 py-2">
						<Input
							value={agentSearch.query}
							onChange={(e) => agentSearch.onQueryChange(e.target.value)}
							placeholder={t(
								"taskDetail.description.writeWithAIDialog.searchAgents",
							)}
							aria-label={t(
								"taskDetail.description.writeWithAIDialog.searchAgents",
							)}
						/>
						{agents.length === 0 && !agentSearch.isLoading ? (
							<p className="text-base text-muted-foreground text-center py-4">
								{t("taskDetail.description.writeWithAIDialog.noAgents")}
							</p>
						) : (
							agents.map((agent) => (
								<button
									key={agent.id}
									type="button"
									onClick={() => setSelectedAgentId(agent.id)}
									className={cn(
										"w-full flex items-center gap-3 rounded-lg border px-3 py-2.5 text-left transition-all duration-150",
										selectedAgentId === agent.id
											? "border-primary/60 bg-primary/5 text-foreground"
											: "border-border/40 bg-card/50 hover:border-border/70 hover:bg-muted/30 text-muted-foreground hover:text-foreground",
									)}
								>
									<div className="flex size-7 shrink-0 items-center justify-center rounded-md bg-muted">
										<Bot className="size-3.5" />
									</div>
									<div className="min-w-0">
										<p className="text-base font-medium leading-tight truncate">
											{agent.name}
										</p>
										<p className="text-xs text-muted-foreground/70 mt-0.5 truncate">
											@{agent.handle}
										</p>
									</div>
								</button>
							))
						)}
						<LoadMoreSentinel
							compact
							hasMore={agentSearch.hasMore}
							isLoadingMore={agentSearch.isLoadingMore}
							isError={agentSearch.isError}
							onLoadMore={agentSearch.onLoadMore}
						/>
					</div>

					{writeWithAIMutation.error && (
						<p className="text-sm text-destructive">
							{writeWithAIMutation.error.message}
						</p>
					)}

					<DialogFooter>
						<Button
							variant="outline"
							size="sm"
							onClick={() => {
								setWriteWithAIOpen(false);
								setSelectedAgentId(null);
							}}
						>
							{t("taskDetail.description.writeWithAIDialog.cancel")}
						</Button>
						<Button
							size="sm"
							disabled={!selectedAgentId || writeWithAIMutation.isPending}
							onClick={() => writeWithAIMutation.mutate()}
						>
							{writeWithAIMutation.isPending ? (
								<>
									<Sparkles className="size-3 mr-1.5 animate-pulse" />
									{t("taskDetail.description.writeWithAIDialog.starting")}
								</>
							) : (
								<>
									<Sparkles className="size-3 mr-1.5" />
									{t("taskDetail.description.writeWithAIDialog.writeButton")}
								</>
							)}
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</div>
	);
}
