import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { AlertCircle, Check, ChevronRight, MessageSquare } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { DocActivityPane } from "@/components/projects/docs/doc-activity-pane";
import {
	DocEditor,
	type DocEditorHandle,
} from "@/components/projects/docs/doc-editor";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import {
	docFoldersQueryOptions,
	docQueryKeys,
	docQueryOptions,
	updateDocument,
} from "@/lib/doc-api";
import { useCurrentContextStore } from "@/lib/shortcuts/current-context-store";
import { cn } from "@/lib/utils";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/docs/$docId",
)({
	component: DocEditorPage,
});

type RightPanel = "activity" | null;

function DocEditorPage() {
	const { t } = useTranslation("projects");
	const { projectId, docId } = Route.useParams();
	const { hasProjectPermission } = useProjectPermissions(projectId);
	const canWrite = hasProjectPermission("docs.write");
	const qc = useQueryClient();

	const { data: doc, isError } = useQuery(docQueryOptions(projectId, docId));
	const { data: allFolders = [] } = useQuery(docFoldersQueryOptions(projectId));

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
	const [dirty, setDirty] = useState(false);
	const editorRef = useRef<DocEditorHandle>(null);

	const updateMutation = useMutation({
		mutationFn: (payload: { content?: unknown[] | null }) =>
			updateDocument(projectId, docId, payload),
		onSuccess: (updated) => {
			qc.setQueryData(docQueryKeys.detail(projectId, docId), updated);
			qc.invalidateQueries({ queryKey: docQueryKeys.list(projectId) });
			if (updated.folder_id) {
				qc.invalidateQueries({
					queryKey: docQueryKeys.list(projectId, updated.folder_id),
				});
			}
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
			updateMutation.mutate({ content: blocks });
		},
		[updateMutation],
	);

	const handleEditorDirtyChange = useCallback((isDirty: boolean) => {
		setDirty(isDirty);
	}, []);

	const togglePanel = (panel: RightPanel) => {
		setRightPanel((prev) => (prev === panel ? null : panel));
	};

	// Compute folder breadcrumb for this document
	const folderPath = (() => {
		if (!doc?.folder_id) return [];
		const path: typeof allFolders = [];
		let current: string | null = doc.folder_id;
		while (current) {
			const folder = allFolders.find((f) => f.id === current);
			if (!folder) break;
			path.unshift(folder);
			current = folder.parent_id ?? null;
		}
		return path;
	})();

	if (isError) {
		return (
			<div className="flex flex-1 flex-col items-center justify-center gap-4 text-muted-foreground">
				<div className="flex size-14 items-center justify-center rounded-xl bg-muted/50">
					<AlertCircle className="size-7 text-muted-foreground/60" />
				</div>
				<div className="text-center">
					<p className="text-base font-semibold text-foreground/80">
						{t("docs.editorPage.notFound.title")}
					</p>
					<p className="text-sm mt-1.5 text-muted-foreground/70">
						{t("docs.editorPage.notFound.description")}
					</p>
				</div>
			</div>
		);
	}

	return (
		<div className="flex flex-col h-full min-h-0">
			{/* ── Header bar ───────────────────────────────────────────────── */}
			<div className="flex items-center justify-between px-4 py-2 bg-muted/20 border-b border-border/30 shrink-0 gap-3 min-w-0">
				{/* Breadcrumb path */}
				<div className="flex items-center gap-1 min-w-0 text-xs">
					{folderPath.map((folder) => (
						<span key={folder.id} className="flex items-center gap-1 min-w-0">
							<span className="text-muted-foreground/50 truncate max-w-32">
								{folder.name}
							</span>
							<ChevronRight className="size-3.5 text-muted-foreground/30 shrink-0" />
						</span>
					))}
					{doc?.title ? (
						<span className="text-foreground/70 font-medium truncate max-w-60">
							{doc.title}
						</span>
					) : !doc ? (
						<Skeleton className="h-3 w-28" />
					) : null}
				</div>

				{/* Right: save status + panel toggle */}
				<div className="flex items-center gap-2 shrink-0">
					{dirty && !updateMutation.isPending && (
						<span className="text-xs text-muted-foreground/50">
							{t("docs.editorPage.unsaved")}
						</span>
					)}
					{updateMutation.isPending && (
						<span className="text-xs text-muted-foreground/60">
							{t("docs.editorPage.saving")}
						</span>
					)}
					{!dirty && updateMutation.isSuccess && !updateMutation.isPending && (
						<span className="text-xs text-muted-foreground/60 flex items-center gap-1">
							<Check className="size-3 text-emerald-500" />
							{t("docs.editorPage.saved")}
						</span>
					)}

					<Button
						variant="ghost"
						size="icon"
						className={cn(
							"size-7 text-muted-foreground/60 hover:text-foreground hover:bg-muted/60 transition-all duration-150",
							rightPanel === "activity" && "bg-muted/40 text-foreground",
						)}
						title={t("docs.editorPage.commentsAndActivity")}
						onClick={() => togglePanel("activity")}
					>
						<MessageSquare className="size-3.5" />
					</Button>
				</div>
			</div>

			{/* ── Body: editor + optional right panel ──────────────────────── */}
			<div className="flex flex-1 min-h-0 overflow-hidden">
				{/* Editor area */}
				<div className="flex-1 overflow-y-auto [scrollbar-gutter:stable]">
					<div className="max-w-7xl mx-auto px-8 py-7">
						{doc && (
							<DocEditor
								ref={editorRef}
								content={doc.content}
								editable={canWrite}
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
					<div className="w-80 shrink-0 h-full overflow-hidden">
						<DocActivityPane
							projectId={projectId}
							docId={docId}
							canEdit={canWrite}
						/>
					</div>
				)}
			</div>
		</div>
	);
}
