import "@blocknote/core/fonts/inter.css";
import "@blocknote/shadcn/style.css";

import { SideMenuController, useCreateBlockNote } from "@blocknote/react";
import { BlockNoteView } from "@blocknote/shadcn";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot, Sparkles } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { createAnnotationPasteHandler } from "@/components/shared/blocknote-annotation-paste-handler";
import { CustomSideMenu } from "@/components/shared/blocknote-custom-side-menu";
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
import { useThemeMode } from "@/hooks/use-theme-mode";
import {
	agentsQueryOptions,
	writeTaskDescriptionWithAI,
} from "@/lib/agent-api";
import {
	getAttachmentDownloadURL,
	uploadAttachment,
} from "@/lib/attachment-api";
import { useMentionData } from "@/lib/mention-api";
import { cleanBlocks, cn } from "@/lib/utils";

type UpdateFn = (payload: { description?: unknown[] | null }) => void;

interface DescriptionSectionProps {
	description?: unknown[] | null;
	canEdit?: boolean;
	projectId?: string;
	taskId?: string;
	onUpdate?: UpdateFn;
}

/** Custom URI scheme used to store attachment references in the markdown. */
const ATTACHMENT_SCHEME = "attachment://";

export function DescriptionSection({
	description,
	canEdit = true,
	projectId,
	taskId,
	onUpdate,
}: DescriptionSectionProps) {
	const { t } = useTranslation("projects");
	const { resolvedMode } = useThemeMode();
	const { teamMembers, documents } = useMentionData(projectId);
	const qc = useQueryClient();
	const [writeWithAIOpen, setWriteWithAIOpen] = useState(false);
	const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null);

	const { data: agents = [] } = useQuery({
		...agentsQueryOptions(projectId ?? ""),
		enabled: !!projectId && writeWithAIOpen,
	});

	const writeWithAIMutation = useMutation({
		mutationFn: () => {
			if (!projectId || !taskId || !selectedAgentId)
				throw new Error("missing context");
			return writeTaskDescriptionWithAI(projectId, taskId, selectedAgentId);
		},
		onSuccess: () => {
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

	const editor = useCreateBlockNote({
		schema: customSchema,
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
			if (!url.startsWith(ATTACHMENT_SCHEME)) return url;
			// URI format: attachment://{projectId}/{taskId}/{attachmentId}
			const path = url.slice(ATTACHMENT_SCHEME.length);
			const [pId, tId, attachmentId] = path.split("/");
			if (!pId || !tId || !attachmentId) return url;
			return getAttachmentDownloadURL(pId, tId, attachmentId);
		},
	});

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
		initializedRef.current = true;
		lastSavedRef.current = normalizedStr;
		readyRef.current = false;

		editor.replaceBlocks(
			editor.document,
			displayBlocks as Parameters<typeof editor.replaceBlocks>[1],
		);
		queueMicrotask(() => {
			readyRef.current = true;
		});
	}, [description, editor]);

	const handleChange = useCallback(() => {
		if (!canEdit || !readyRef.current) return;
		// Track dirty state so blur can save without re-reading document
		pendingRef.current = true;
	}, [canEdit]);

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

			{/* biome-ignore lint/a11y/noStaticElementInteractions: wrapper captures blur from BlockNote rich-text editor */}
			<div
				className="rounded-xl border border-border/25 bg-card/50 hover:border-border/50 transition-all duration-200 overflow-hidden [&_.bn-editor]:min-h-20 [&_.bn-editor]:py-3 [&_.bn-editor]:text-base [&_.bn-editor]:leading-relaxed"
				onBlur={handleBlur}
				onKeyDown={handleKeyDown}
			>
				<BlockNoteView
					editor={editor}
					editable={canEdit}
					onChange={handleChange}
					theme={resolvedMode}
					className="bn-shadcn"
					sideMenu={false}
				>
					<SideMenuController sideMenu={CustomSideMenu} />
					{canEdit && (
						<MentionSuggestionMenus
							editor={editor}
							teamMembers={teamMembers}
							projectId={projectId}
							documents={documents}
						/>
					)}
				</BlockNoteView>
			</div>

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
						{agents.length === 0 ? (
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
