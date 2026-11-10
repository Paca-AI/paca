import "@blocknote/core/fonts/inter.css";
import "@blocknote/shadcn/style.css";

import { SideMenuController, useCreateBlockNote } from "@blocknote/react";
import { BlockNoteView } from "@blocknote/shadcn";
import {
	forwardRef,
	useCallback,
	useEffect,
	useImperativeHandle,
	useRef,
} from "react";
import { createAnnotationPasteHandler } from "@/components/shared/blocknote-annotation-paste-handler";
import { CustomSideMenu } from "@/components/shared/blocknote-custom-side-menu";
import { customSchema } from "@/components/shared/blocknote-schema";
import { normalizeBlockContent } from "@/components/shared/comment-blocknote";
import { MentionSuggestionMenus } from "@/components/shared/mention-suggestion-menus";
import { useCollabSession } from "@/hooks/use-collab-session";
import { useDebouncedCallback } from "@/hooks/use-debounced-callback";
import { useThemeMode } from "@/hooks/use-theme-mode";
import {
	type CollabSession,
	type CollabStatus,
	seedCollabDoc,
} from "@/lib/collab-session";
import { getDocFileDownloadURL, uploadDocFile } from "@/lib/doc-api";
import { cleanBlocks, isRemoteOnlyChange } from "@/lib/utils";

/** Custom URI scheme used to store doc file references in the block content. */
const DOC_FILE_SCHEME = "docfile://";

interface DocEditorProps {
	/** BlockNote block array loaded from the server. */
	content?: unknown[] | null;
	/** Whether the editor is interactive. */
	editable?: boolean;
	/** Called when unsaved-changes status changes. */
	onDirtyChange?: (dirty: boolean) => void;
	/** Called when content is saved (debounced, blur, or Ctrl+S). Receives the new block array (null = empty). */
	onSave?: (blocks: unknown[] | null) => void;
	/** Project ID — required for file uploads. */
	projectId?: string;
	/** Document ID — required for file uploads. */
	docId?: string;
}

export interface DocEditorHandle {
	save: () => void;
}

/**
 * The doc editor. Editors join a real-time Yjs room for the doc (see
 * lib/collab-session.ts) so several people can type at once; when the room
 * cannot be joined it falls back to the plain, single-user editor.
 */
export const DocEditor = forwardRef<DocEditorHandle, DocEditorProps>(
	function DocEditor(props, ref) {
		const collab = useCollabSession(
			"doc",
			props.projectId,
			props.docId,
			!!props.editable && !!props.projectId && !!props.docId,
		);
		if (collab.phase === "connecting") {
			return (
				<div
					data-testid="blocknote-editor"
					className="min-h-80 rounded-xl border border-border/25 bg-card/50"
				/>
			);
		}
		return (
			<DocEditorInner
				// Remount when switching between collaborative and plain mode: the
				// BlockNote editor is built once with or without the Yjs binding.
				key={collab.session ? "collab" : "plain"}
				{...props}
				ref={ref}
				collab={collab.session}
				collabStatus={collab.phase === "ready" ? collab.status : null}
			/>
		);
	},
);

interface DocEditorInnerProps extends DocEditorProps {
	collab: CollabSession | null;
	collabStatus: CollabStatus | null;
}

const DocEditorInner = forwardRef<DocEditorHandle, DocEditorInnerProps>(
	function DocEditorInner(
		{
			content,
			editable: editableProp = true,
			onDirtyChange,
			onSave,
			projectId,
			docId,
			collab,
			collabStatus,
		},
		ref,
	) {
		// A revoked session must stop accepting edits: they would never sync.
		const editable = editableProp && collabStatus !== "lost";
		const { resolvedMode } = useThemeMode();

		const lastSavedRef = useRef<string | null>(null);
		const initializedRef = useRef(false);
		const localSaveRef = useRef(false);
		const readyRef = useRef(false);
		const dirtyRef = useRef(false);
		// True while the stored content is being written into the shared doc.
		const seedingRef = useRef(false);
		// Whether the component is mounted. A ref flag rather than cancelling
		// the seed in a cleanup: React StrictMode runs that cleanup once on
		// mount and the seed effect does not run again, so cancelling would
		// leave the doc blank in development.
		const mountedRef = useRef(false);
		useEffect(() => {
			mountedRef.current = true;
			return () => {
				mountedRef.current = false;
			};
		}, []);

		// Keep projectId / docId in refs so stable editor callbacks always
		// reference the latest prop values without recreating the editor.
		const projectIdRef = useRef(projectId);
		const docIdRef = useRef(docId);
		useEffect(() => {
			projectIdRef.current = projectId;
		}, [projectId]);
		useEffect(() => {
			docIdRef.current = docId;
		}, [docId]);

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
				uploadFile: async (file: File) => {
					const pId = projectIdRef.current;
					const dId = docIdRef.current;
					if (!pId || !dId)
						throw new Error("No project/doc context for upload");
					const uploaded = await uploadDocFile(pId, dId, file);
					// Store as a stable custom URI; presigned URL is fetched on-demand.
					return `${DOC_FILE_SCHEME}${pId}/${dId}/${uploaded.id}`;
				},
				resolveFileUrl: async (url: string) => {
					if (!url.startsWith(DOC_FILE_SCHEME)) return url;
					// URI format: docfile://{projectId}/{docId}/{fileId}
					const path = url.slice(DOC_FILE_SCHEME.length);
					const [pId, dId, fileId] = path.split("/");
					if (!pId || !dId || !fileId) return url;
					return getDocFileDownloadURL(pId, dId, fileId);
				},
			},
			[collab],
		);

		// Populate / re-populate editor from server content
		useEffect(() => {
			// Normalize whatever the server returned (including legacy/invalid
			// content that isn't a block array, e.g. a plain string) into a safe
			// block array so BlockNote never receives data it can't render.
			const displayBlocks = normalizeBlockContent(content ?? null);
			const cleanedNormalized = cleanBlocks(
				displayBlocks.length > 0 ? displayBlocks : null,
			);
			const normalizedStr = cleanedNormalized
				? JSON.stringify(cleanedNormalized)
				: null;

			if (collab) {
				// The shared Y.Doc is the source of truth while collaborating:
				// peers' saves must not overwrite it (they would fight over
				// replaceBlocks). Only the client the room picked writes the
				// stored content, once, into the empty room.
				if (initializedRef.current) return;
				initializedRef.current = true;
				lastSavedRef.current = normalizedStr;
				dirtyRef.current = false;
				readyRef.current = false;
				if (collab.seed && displayBlocks.length > 0) {
					seedCollabDoc(
						collab,
						() => {
							// Writing the stored content is not a user edit.
							seedingRef.current = true;
							editor.replaceBlocks(
								editor.document,
								displayBlocks as Parameters<typeof editor.replaceBlocks>[1],
							);
							queueMicrotask(() => {
								seedingRef.current = false;
							});
						},
						{ isActive: () => mountedRef.current },
					);
				}
				queueMicrotask(() => {
					readyRef.current = true;
				});
				return;
			}

			if (initializedRef.current && localSaveRef.current) {
				localSaveRef.current = false;
				lastSavedRef.current = normalizedStr;
				dirtyRef.current = false;
				return;
			}

			if (initializedRef.current && normalizedStr === lastSavedRef.current)
				return;
			initializedRef.current = true;
			lastSavedRef.current = normalizedStr;
			readyRef.current = false;
			dirtyRef.current = false;

			editor.replaceBlocks(
				editor.document,
				displayBlocks as Parameters<typeof editor.replaceBlocks>[1],
			);
			queueMicrotask(() => {
				readyRef.current = true;
			});
		}, [content, editor, collab]);

		const save = useCallback(() => {
			if (!editable || !dirtyRef.current) return;
			const blocks = editor.document;
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
				localSaveRef.current = true;
				dirtyRef.current = false;
				onDirtyChange?.(false);
				onSave?.(cleanedValue);
			} else {
				dirtyRef.current = false;
				onDirtyChange?.(false);
			}
		}, [editable, editor, onSave, onDirtyChange]);

		useImperativeHandle(ref, () => ({ save }), [save]);

		const debouncedSave = useDebouncedCallback(save, 3000);

		const handleChange = useCallback(
			(_editor: unknown, ctx?: Parameters<typeof isRemoteOnlyChange>[0]) => {
				if (!editable || !readyRef.current) return;
				// A peer's edit is saved by that peer, not by every open editor.
				if (collab && isRemoteOnlyChange(ctx)) return;
				dirtyRef.current = true;
				onDirtyChange?.(true);
				debouncedSave();
			},
			[editable, collab, debouncedSave, onDirtyChange],
		);

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
			// biome-ignore lint/a11y/noStaticElementInteractions: wrapper captures keydown from BlockNote rich-text editor
			<div
				data-testid="blocknote-editor"
				className="rounded-xl border border-border/25 bg-card/50 hover:border-border/50 transition-all duration-200 overflow-hidden [&_.bn-editor]:min-h-80 [&_.bn-editor]:py-4 [&_.bn-editor]:px-6 [&_.bn-editor]:text-sm [&_.bn-editor]:leading-relaxed"
				onBlur={save}
				onKeyDown={handleKeyDown}
			>
				<BlockNoteView
					editor={editor}
					editable={editable}
					theme={resolvedMode}
					onChange={handleChange}
					sideMenu={false}
					slashMenu={false}
				>
					<SideMenuController sideMenu={CustomSideMenu} />
					{editable && (
						<MentionSuggestionMenus editor={editor} projectId={projectId} />
					)}
				</BlockNoteView>
			</div>
		);
	},
);
