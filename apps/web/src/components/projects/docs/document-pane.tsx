import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { AlertCircle, Check, Download, MessageSquare } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { ReportIssueButton } from "@/components/report-issue-button";
import { NoPermissionState } from "@/components/shared/no-permission-state";
import {
	PathBreadcrumb,
	type PathCrumb,
} from "@/components/shared/path-breadcrumb";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { toast } from "@/components/ui/toast";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import { getHttpStatus, isForbiddenError } from "@/lib/api-error";
import {
	docFolderPathQueryOptions,
	docQueryKeys,
	docQueryOptions,
	downloadDocMarkdown,
	updateDocument,
} from "@/lib/doc-api";
import { fileNodePathQueryOptions } from "@/lib/files-api";
import { notifyError } from "@/lib/notify";
import { projectQueryOptions } from "@/lib/project-api";
import { useCurrentContextStore } from "@/lib/shortcuts/current-context-store";
import { cn } from "@/lib/utils";
import { DocActivityPane } from "./doc-activity-pane";
import { DocEditor, type DocEditorHandle } from "./doc-editor";
import { isDocLockedError, markDocLocked } from "./doc-lock";
import { ManagedCopyBanner } from "./managed-copy-banner";

type RightPanel = "activity" | null;

interface DocumentPaneProps {
	projectId: string;
	docId: string;
	/** Embeds the pane in a host that has its own header (the note view's tab
	 *  bar): the pane draws no header bar, and its save status and comments
	 *  and activity toggle are rendered into this element instead (nothing
	 *  until it is non-null). Leave undefined for the pane's own header. */
	headerSlot?: HTMLElement | null;
}

/**
 * One document, open for reading and editing: its breadcrumb and save state,
 * the managed-copy banner, the BlockNote editor with live collaboration, and
 * the comments and activity pane. The docs route shows it for a document of
 * the docs tree, and a note view for its one document.
 */
export function DocumentPane({
	projectId,
	docId,
	headerSlot,
}: DocumentPaneProps) {
	const { t } = useTranslation("projects");
	const navigate = useNavigate();
	const { hasProjectPermission } = useProjectPermissions(projectId);
	const canWrite = hasProjectPermission("docs:write");
	const qc = useQueryClient();

	const {
		data: doc,
		isError,
		error: docError,
	} = useQuery(docQueryOptions(projectId, docId));

	// Register this doc as "currently on screen" for the chat composer's
	// quick-add affordance — see lib/shortcuts/current-context-store.ts.
	useEffect(() => {
		if (!doc) return;
		useCurrentContextStore.getState().setActive({
			type: "doc",
			id: docId,
			projectId,
			title: doc.title,
		});
		return () => {
			useCurrentContextStore.getState().clearActive("doc", docId);
		};
	}, [doc, docId, projectId]);

	const [rightPanel, setRightPanel] = useState<RightPanel>(null);
	const [dirty, setDirtyState] = useState(false);
	// Mirrors `dirty` synchronously, for the lock effect below.
	const dirtyRef = useRef(false);
	const setDirty = useCallback((value: boolean) => {
		dirtyRef.current = value;
		setDirtyState(value);
	}, []);
	const editorRef = useRef<DocEditorHandle>(null);
	// The content save still in flight, if any (never rejects).
	const saveInFlightRef = useRef<Promise<void> | null>(null);
	const docIdRef = useRef(docId);
	docIdRef.current = docId;

	// A locked managed copy is read-only here: it is edited in its repository.
	const locked = !!doc?.source_locked;
	const editable = canWrite && !locked;

	// Said once per lock: the realtime event and a refused save can both
	// report the same lost edit.
	const notSavedShownRef = useRef(false);
	const showEditNotSaved = useCallback(() => {
		if (notSavedShownRef.current) return;
		notSavedShownRef.current = true;
		toast.add({
			type: "error",
			title: t("docs.managedCopy.locked"),
			description: t("docs.managedCopy.editNotSaved"),
		});
	}, [t]);

	// The document was locked while open (by someone else, or by a push): the
	// editor turns read-only and drops what was not saved yet; say so.
	const wasLockedRef = useRef(locked);
	// The page is reused for another doc: start its lock tracking afresh.
	const trackedDocRef = useRef(docId);
	useEffect(() => {
		if (trackedDocRef.current === docId) return;
		trackedDocRef.current = docId;
		notSavedShownRef.current = false;
		wasLockedRef.current = false;
	}, [docId]);
	useEffect(() => {
		if (locked && !wasLockedRef.current && dirtyRef.current) {
			showEditNotSaved();
		}
		if (locked) setDirty(false);
		else notSavedShownRef.current = false;
		wasLockedRef.current = locked;
	}, [locked, setDirty, showEditNotSaved]);

	const invalidateDocLists = useCallback(
		(folderId: string | null | undefined) => {
			qc.invalidateQueries({ queryKey: docQueryKeys.list(projectId) });
			if (folderId) {
				qc.invalidateQueries({
					queryKey: docQueryKeys.list(projectId, folderId),
				});
			}
		},
		[qc, projectId],
	);

	const updateMutation = useMutation({
		// The doc id travels with the request: the page is reused when another
		// doc is opened, and a save still in flight must not land on that one.
		mutationFn: (vars: { docId: string; content: unknown[] | null }) =>
			updateDocument(projectId, vars.docId, { content: vars.content }),
		onSuccess: (updated, vars) => {
			qc.setQueryData(docQueryKeys.detail(projectId, vars.docId), updated);
			invalidateDocLists(updated.folder_id);
		},
		onError: (err, vars) => {
			if (!isDocLockedError(err)) {
				// What the global handler did before this had an onError: a 401
				// goes to the login flow, anything else gets the error toast.
				if (getHttpStatus(err) !== 401) notifyError(err);
				return;
			}
			// Locked after it was opened. Not retried: the edit is dropped and
			// the editor goes read-only.
			if (vars.docId === docIdRef.current) {
				setDirty(false);
				showEditNotSaved();
			}
			markDocLocked(qc, projectId, vars.docId);
		},
	});

	const lockMutation = useMutation({
		// Clicking Lock blurs the editor, which saves. The lock waits for that
		// save, or the server could apply the lock first and refuse the edit.
		mutationFn: async (sourceLocked: boolean) => {
			await saveInFlightRef.current;
			return updateDocument(projectId, docId, {
				source_locked: sourceLocked,
			});
		},
		onSuccess: (updated) => {
			qc.setQueryData(docQueryKeys.detail(projectId, docId), updated);
			// The tree shows a lock glyph on locked copies.
			invalidateDocLists(updated.folder_id);
		},
	});

	useEffect(() => {
		const handler = (e: KeyboardEvent) => {
			if ((e.ctrlKey || e.metaKey) && e.key === "s") {
				e.preventDefault();
				editorRef.current?.save();
			}
		};
		window.addEventListener("keydown", handler);
		return () => window.removeEventListener("keydown", handler);
	}, []);

	const handleContentSave = useCallback(
		(blocks: unknown[] | null) => {
			setDirty(false);
			// Errors are handled by the mutation's onError; this promise only
			// tells the lock when the save has settled.
			const settled = updateMutation
				.mutateAsync({ docId, content: blocks })
				.then(
					() => undefined,
					() => undefined,
				)
				.finally(() => {
					if (saveInFlightRef.current === settled) {
						saveInFlightRef.current = null;
					}
				});
			saveInFlightRef.current = settled;
		},
		[updateMutation, setDirty, docId],
	);

	const handleEditorDirtyChange = useCallback(
		(isDirty: boolean) => {
			setDirty(isDirty);
		},
		[setDirty],
	);

	const togglePanel = (panel: RightPanel) => {
		setRightPanel((prev) => (prev === panel ? null : panel));
	};

	// Folder breadcrumb for this document (root first), fetched as a path for
	// just this folder rather than from the whole folder tree.
	const { data: folderPath = [] } = useQuery(
		docFolderPathQueryOptions(projectId, doc?.folder_id),
	);

	// The same location as in Files (project > Documentation > folders >
	// document), each folder opening in Files, when the caller may read files;
	// otherwise just the docs folders.
	const canReadFiles = hasProjectPermission("files:read");
	const project = useQuery(projectQueryOptions(projectId));
	const filePath = useQuery({
		...fileNodePathQueryOptions({ projectId }, docId),
		enabled: canReadFiles && !!doc,
	});
	const openInFiles = (folder?: string) =>
		void navigate({
			to: "/projects/$projectId/files",
			params: { projectId },
			search: { folder } as never,
		});
	const crumbs: PathCrumb[] =
		canReadFiles && filePath.data
			? [
					{
						key: "project",
						label: project.data?.name ?? "",
						onSelect: () => openInFiles(),
					},
					// The path ends with the document itself, shown by its title.
					...filePath.data.slice(0, -1).map((n) => ({
						key: n.id,
						label: n.name,
						onSelect: () => openInFiles(n.id),
					})),
					{ key: "doc", label: doc?.title ?? "" },
				]
			: [
					...folderPath.map((f) => ({ key: f.id, label: f.name })),
					{ key: "doc", label: doc?.title ?? "" },
				];

	if (isError) {
		if (isForbiddenError(docError)) {
			return (
				<div className="flex flex-1 flex-col items-center justify-center p-6">
					<NoPermissionState
						title={t("docs.editorPage.noPermission.title")}
						description={t("docs.editorPage.noPermission.description")}
					/>
				</div>
			);
		}
		return (
			<div className="flex flex-1 flex-col items-center justify-center gap-4 text-muted-foreground">
				<div className="flex size-14 items-center justify-center rounded-xl bg-muted/50">
					<AlertCircle className="size-7 text-muted-foreground/90" />
				</div>
				<div className="text-center">
					<p className="text-base font-semibold text-foreground/80">
						{t("docs.editorPage.notFound.title")}
					</p>
					<p className="text-sm mt-1.5 text-muted-foreground">
						{t("docs.editorPage.notFound.description")}
					</p>
					<div className="mt-3 flex justify-center">
						<ReportIssueButton error={docError} />
					</div>
				</div>
			</div>
		);
	}

	const headerActions = (
		<div className="flex items-center gap-2 shrink-0">
			{dirty && !updateMutation.isPending && (
				<span className="text-xs text-muted-foreground/90">
					{t("docs.editorPage.unsaved")}
				</span>
			)}
			{updateMutation.isPending && (
				<span className="text-xs text-muted-foreground/90">
					{t("docs.editorPage.saving")}
				</span>
			)}
			{!dirty && updateMutation.isSuccess && !updateMutation.isPending && (
				<span className="text-xs text-muted-foreground/90 flex items-center gap-1">
					<Check className="size-3 text-success" />
					{t("docs.editorPage.saved")}
				</span>
			)}

			<Button
				variant="ghost"
				size="icon"
				className="size-7 text-muted-foreground/90 hover:text-foreground hover:bg-muted/60 transition-all duration-150"
				title={t("docs.editorPage.downloadMarkdown")}
				aria-label={t("docs.editorPage.downloadMarkdown")}
				onClick={() =>
					void downloadDocMarkdown(
						projectId,
						docId,
						`${doc?.title || "Untitled"}.md`,
					).catch((error) => notifyError(error))
				}
			>
				<Download className="size-3.5" />
			</Button>
			<Button
				variant="ghost"
				size="icon"
				className={cn(
					"size-7 text-muted-foreground/90 hover:text-foreground hover:bg-muted/60 transition-all duration-150",
					rightPanel === "activity" && "bg-muted/40 text-foreground",
				)}
				title={t("docs.editorPage.commentsAndActivity")}
				aria-label={t("docs.editorPage.commentsAndActivity")}
				onClick={() => togglePanel("activity")}
			>
				<MessageSquare className="size-3.5" />
			</Button>
		</div>
	);

	return (
		<div className="flex flex-col h-full min-h-0">
			{/* ── Header bar ───────────────────────────────────────────────── */}
			{headerSlot === undefined && (
				<div className="flex items-center justify-between px-4 py-2 bg-muted/20 border-b border-border/50 shrink-0 gap-3 min-w-0">
					<PathBreadcrumb crumbs={crumbs} loading={!doc} />

					{headerActions}
				</div>
			)}
			{headerSlot && createPortal(headerActions, headerSlot)}

			{/* ── Body: editor + optional right panel ──────────────────────── */}
			{/* Below `lg` the row itself scrolls as one column (editor, then the
			    activity pane stacked under it) instead of two independently
			    scrolling panes side by side — same pattern as task-detail's own
			    layout (index.tsx). DocActivityPane is self-responsive
			    (`w-full lg:w-(--activity-pane-width)`, user-resizable), so it needs no
			    extra width wrapper here. */}
			<div className="flex flex-1 min-w-0 min-h-0 flex-col overflow-y-auto lg:flex-row lg:overflow-hidden">
				{/* Editor area */}
				<div className="lg:flex-1 lg:overflow-y-auto [scrollbar-gutter:stable]">
					<div className="max-w-7xl mx-auto px-8 py-7">
						{doc && (
							<ManagedCopyBanner
								source={doc.source}
								locked={locked}
								canWrite={canWrite}
								onToggleLock={(next) => lockMutation.mutate(next)}
								pending={lockMutation.isPending}
							/>
						)}
						{doc && (
							<DocEditor
								// A lock change remounts the editor from the stored content,
								// so an edit the server refused does not stay on screen.
								key={locked ? "locked" : "open"}
								ref={editorRef}
								content={doc.content}
								editable={editable}
								onSave={handleContentSave}
								onDirtyChange={handleEditorDirtyChange}
								projectId={projectId}
								docId={docId}
							/>
						)}

						{!doc && (
							<div className="space-y-6">
								<div className="space-y-3">
									<Skeleton className="h-4 w-full" />
									<Skeleton className="h-4 w-11/12" />
									<Skeleton className="h-4 w-5/6" />
								</div>
								<div className="space-y-3">
									<Skeleton className="h-4 w-full" />
									<Skeleton className="h-4 w-3/4" />
								</div>
								<div className="space-y-3">
									<Skeleton className="h-4 w-full" />
									<Skeleton className="h-4 w-11/12" />
									<Skeleton className="h-4 w-4/5" />
									<Skeleton className="h-4 w-2/3" />
								</div>
							</div>
						)}

						{/* Bottom breathing room */}
						<div className="h-8" />
					</div>
				</div>

				{/* Right panel: activity */}
				{rightPanel === "activity" && doc && (
					<DocActivityPane
						projectId={projectId}
						docId={docId}
						canEdit={canWrite}
						// The lock covers content and title; comments stay open.
						canRevert={canWrite && !locked}
					/>
				)}
			</div>
		</div>
	);
}
