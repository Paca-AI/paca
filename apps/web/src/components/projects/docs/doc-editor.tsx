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
import { CustomSideMenu } from "@/components/shared/blocknote-custom-side-menu";
import { customSchema } from "@/components/shared/blocknote-schema";
import { normalizeBlockContent } from "@/components/shared/comment-blocknote";
import { MentionSuggestionMenus } from "@/components/shared/mention-suggestion-menus";
import { useDebouncedCallback } from "@/hooks/use-debounced-callback";
import { useThemeMode } from "@/hooks/use-theme-mode";
import { getDocFileDownloadURL, uploadDocFile } from "@/lib/doc-api";
import { useMentionData } from "@/lib/mention-api";
import { cleanBlocks } from "@/lib/utils";

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

export const DocEditor = forwardRef<DocEditorHandle, DocEditorProps>(
	function DocEditor(
		{ content, editable = true, onDirtyChange, onSave, projectId, docId },
		ref,
	) {
		const { resolvedMode } = useThemeMode();
		const { teamMembers, documents } = useMentionData(projectId);

		const lastSavedRef = useRef<string | null>(null);
		const initializedRef = useRef(false);
		const localSaveRef = useRef(false);
		const readyRef = useRef(false);
		const dirtyRef = useRef(false);

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

		const editor = useCreateBlockNote({
			schema: customSchema,
			uploadFile: async (file: File) => {
				const pId = projectIdRef.current;
				const dId = docIdRef.current;
				if (!pId || !dId) throw new Error("No project/doc context for upload");
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
		});

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
		}, [content, editor]);

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

		const handleChange = useCallback(() => {
			if (!editable || !readyRef.current) return;
			dirtyRef.current = true;
			onDirtyChange?.(true);
			debouncedSave();
		}, [editable, debouncedSave, onDirtyChange]);

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
						<MentionSuggestionMenus
							editor={editor}
							teamMembers={teamMembers}
							projectId={projectId}
							documents={documents}
						/>
					)}
				</BlockNoteView>
			</div>
		);
	},
);
