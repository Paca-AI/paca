import {
	useInfiniteQuery,
	useMutation,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
	ChevronRight,
	Download,
	File as FileIcon,
	FolderInput,
	FolderOpen,
	FolderSearch,
	Info,
	MoreHorizontal,
	Plus,
	Trash2,
	Upload,
} from "lucide-react";
import { lazy, Suspense, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { DetailsSheet } from "@/components/files/file-dialogs";
import { FileTypeIcon } from "@/components/files/file-icon";
import { FilePicker } from "@/components/files/file-picker";
import { LoadMoreSentinel } from "@/components/shared/load-more-sentinel";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Spinner } from "@/components/ui/spinner";
import { toast } from "@/components/ui/toast";
import { getApiErrorMessage } from "@/lib/api-error";
import { docFoldersInfiniteQueryOptions, docQueryKeys } from "@/lib/doc-api";
import {
	type FileNode,
	fileNodesQueryOptions,
	getNodeDownload,
	invalidateFiles,
	moveNode,
	trashNode,
	uploadFile,
} from "@/lib/files-api";
import { importMarkdownFile, isMarkdownFile } from "@/lib/markdown-import";
import { showErrorToast } from "@/lib/notify";
import { projectQueryOptions } from "@/lib/project-api";
import { cn } from "@/lib/utils";

const FilePreview = lazy(() => import("@/components/files/file-preview"));

// Files shown in a Documentation folder are the file manager's own: the folder
// is the file's one location, so listing, upload, move and delete all go
// through the Files API (the server requires the docs permissions on top for
// anything inside Documentation).

function useFolderFileErrors() {
	const { t } = useTranslation("files");
	return (error: unknown) =>
		showErrorToast({
			title: t("docFolder.toast.failed"),
			description: getApiErrorMessage(error) ?? "",
			error,
		});
}

/** The files directly inside one Documentation folder. */
export function useDocFolderFilesQuery(
	projectId: string,
	folderId: string,
	enabled: boolean,
) {
	const query = useInfiniteQuery({
		...fileNodesQueryOptions(
			{ projectId },
			{ parentId: folderId, sort: "name" },
		),
		enabled,
	});
	const files = useMemo(
		() =>
			(query.data?.pages.flatMap((p) => p.items) ?? []).filter(
				(n) => n.kind === "file",
			),
		[query.data],
	);
	return { query, files };
}

// ── Rows ──────────────────────────────────────────────────────────────────────

/**
 * The files of one Documentation folder, listed after its documents. Reading
 * needs files:read as well as docs:read, so the list is only requested when the
 * caller has it.
 */
export function DocFolderFileRows({
	projectId,
	folderId,
	depth,
	canWrite,
	enabled,
}: {
	projectId: string;
	folderId: string;
	depth: number;
	/** docs:write and files:write: the files can be moved and deleted. */
	canWrite: boolean;
	enabled: boolean;
}) {
	const { query, files } = useDocFolderFilesQuery(projectId, folderId, enabled);
	const [previewId, setPreviewId] = useState<string | null>(null);
	const previewIndex = files.findIndex((n) => n.id === previewId);
	const fail = useFolderFileErrors();

	const download = async (n: FileNode) => {
		try {
			const d = await getNodeDownload({ projectId }, n.id, { download: true });
			const a = document.createElement("a");
			a.href = d.url;
			a.download = n.name;
			document.body.appendChild(a);
			a.click();
			a.remove();
		} catch (error) {
			fail(error);
		}
	};

	if (!enabled) return null;
	return (
		<>
			{files.map((f) => (
				<DocFolderFileRow
					key={f.id}
					file={f}
					projectId={projectId}
					depth={depth}
					canWrite={canWrite}
					onPreview={() => setPreviewId(f.id)}
					onDownload={() => void download(f)}
				/>
			))}
			<div style={{ paddingLeft: `${8 + depth * 16 + 16}px` }}>
				<LoadMoreSentinel
					compact
					hasMore={!!query.hasNextPage}
					isLoadingMore={query.isFetchingNextPage}
					isError={query.isFetchNextPageError}
					onLoadMore={() => void query.fetchNextPage()}
				/>
			</div>
			{previewIndex >= 0 && (
				<Suspense fallback={null}>
					<FilePreview
						files={files}
						index={previewIndex}
						scope={{ projectId }}
						onIndexChange={(i) => setPreviewId(files[i]?.id ?? null)}
						onClose={() => setPreviewId(null)}
						onDownload={(n) => void download(n)}
					/>
				</Suspense>
			)}
		</>
	);
}

function DocFolderFileRow({
	file,
	projectId,
	depth,
	canWrite,
	onPreview,
	onDownload,
}: {
	file: FileNode;
	projectId: string;
	depth: number;
	canWrite: boolean;
	onPreview: () => void;
	onDownload: () => void;
}) {
	const { t } = useTranslation("files");
	const qc = useQueryClient();
	const scope = { projectId };
	const fail = useFolderFileErrors();
	const project = useQuery(projectQueryOptions(projectId));
	const [details, setDetails] = useState(false);
	const [moving, setMoving] = useState(false);

	const trash = useMutation({
		mutationFn: () => trashNode(scope, file.id),
		onSuccess: () => {
			void invalidateFiles(qc, scope);
			toast.add({
				type: "success",
				title: t("docFolder.toast.removed"),
				description: t("docFolder.toast.removedHint"),
			});
		},
		onError: fail,
	});
	const move = useMutation({
		mutationFn: (target: string) => moveNode(scope, file.id, target),
		onSuccess: () => {
			setMoving(false);
			void invalidateFiles(qc, scope);
			toast.add({ type: "success", title: t("docFolder.toast.moved") });
		},
		onError: fail,
	});

	return (
		<div
			className="group relative flex items-center gap-1 pr-1"
			style={{ paddingLeft: `${8 + depth * 16 + 16}px` }}
		>
			<button
				type="button"
				title={file.name}
				onClick={onPreview}
				className="flex min-w-0 flex-1 items-center gap-1.5 rounded-md px-1.5 py-1 text-left text-sm text-sidebar-foreground/70 transition-all duration-150 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
			>
				<FileTypeIcon node={file} className="size-3.5" />
				<span className="truncate leading-snug">{file.name}</span>
			</button>
			<DropdownMenu>
				<DropdownMenuTrigger
					aria-label={t("docFolder.actions")}
					className="flex size-5 items-center justify-center rounded text-sidebar-foreground/60 opacity-0 transition-all duration-150 hover:bg-sidebar-accent/60 hover:text-sidebar-foreground focus-visible:opacity-100 group-hover:opacity-100 data-[popup-open]:opacity-100"
					onClick={(e) => e.stopPropagation()}
				>
					<MoreHorizontal className="size-3" />
				</DropdownMenuTrigger>
				<DropdownMenuContent align="start" className="w-48">
					<DropdownMenuItem onClick={onPreview}>
						<FileIcon className="mr-2 size-3.5" />
						{t("docFolder.preview")}
					</DropdownMenuItem>
					<DropdownMenuItem onClick={onDownload}>
						<Download className="mr-2 size-3.5" />
						{t("docFolder.download")}
					</DropdownMenuItem>
					<DropdownMenuItem onClick={() => setDetails(true)}>
						<Info className="mr-2 size-3.5" />
						{t("docFolder.details")}
					</DropdownMenuItem>
					<DropdownMenuItem
						render={
							<Link
								to="/projects/$projectId/files"
								params={{ projectId }}
								search={{ folder: file.parent_id ?? undefined }}
							/>
						}
					>
						<FolderSearch className="mr-2 size-3.5" />
						{t("docFolder.showInFiles")}
					</DropdownMenuItem>
					{canWrite && (
						<>
							<DropdownMenuSeparator />
							<DropdownMenuItem onClick={() => setMoving(true)}>
								<FolderInput className="mr-2 size-3.5" />
								{t("docFolder.moveTo")}
							</DropdownMenuItem>
							<DropdownMenuItem
								disabled={trash.isPending}
								onClick={() => trash.mutate()}
							>
								<Trash2 className="mr-2 size-3.5" />
								{t("docFolder.remove")}
							</DropdownMenuItem>
						</>
					)}
				</DropdownMenuContent>
			</DropdownMenu>
			<DetailsSheet
				node={details ? file : null}
				scope={scope}
				scopeName={project.data?.name ?? ""}
				onClose={() => setDetails(false)}
			/>
			{moving && (
				<MoveToFolderDialog
					projectId={projectId}
					currentFolderId={file.parent_id}
					pending={move.isPending}
					onMove={(target) => move.mutate(target)}
					onClose={() => setMoving(false)}
				/>
			)}
		</div>
	);
}

// ── Move ──────────────────────────────────────────────────────────────────────

/** The Documentation folder's id: where the top of the docs tree lives. */
function useDocumentationRootId(projectId: string) {
	const roots = useInfiniteQuery(
		fileNodesQueryOptions({ projectId }, { parentId: null, sort: "name" }),
	);
	return roots.data?.pages
		.flatMap((p) => p.items)
		.find((n) => n.system === "documentation")?.id;
}

/** Picks a Documentation folder, drilling down one level at a time. */
function MoveToFolderDialog({
	projectId,
	currentFolderId,
	pending,
	onMove,
	onClose,
}: {
	projectId: string;
	currentFolderId: string | null;
	pending: boolean;
	onMove: (folderId: string) => void;
	onClose: () => void;
}) {
	const { t } = useTranslation("files");
	const rootId = useDocumentationRootId(projectId);
	// Breadcrumb of the folders opened so far; the last one is the target (the
	// top of Documentation when none is open).
	const [trail, setTrail] = useState<{ id: string; name: string }[]>([]);
	const here = trail.at(-1) ?? null;
	const target = here?.id ?? rootId ?? null;
	const children = useInfiniteQuery(
		docFoldersInfiniteQueryOptions(projectId, here?.id ?? null),
	);
	const folders = children.data?.pages.flatMap((p) => p.items) ?? [];

	return (
		<Dialog open onOpenChange={(o) => !o && onClose()}>
			<DialogContent className="sm:max-w-md">
				<DialogHeader>
					<DialogTitle>{t("docFolder.moveTitle")}</DialogTitle>
					<DialogDescription>{t("docFolder.moveHint")}</DialogDescription>
				</DialogHeader>
				<nav className="flex flex-wrap items-center gap-1 text-sm">
					<button
						type="button"
						className="rounded px-1 hover:bg-muted"
						onClick={() => setTrail([])}
					>
						{t("docFolder.root")}
					</button>
					{trail.map((p, i) => (
						<span key={p.id} className="flex items-center gap-1">
							<ChevronRight className="size-3" />
							<button
								type="button"
								className="max-w-32 truncate rounded px-1 hover:bg-muted"
								onClick={() => setTrail(trail.slice(0, i + 1))}
							>
								{p.name}
							</button>
						</span>
					))}
				</nav>
				<div className="max-h-60 min-h-24 overflow-y-auto rounded-lg border">
					{children.isLoading ? (
						<div className="flex justify-center p-6">
							<Spinner />
						</div>
					) : folders.length === 0 ? (
						<p className="p-6 text-center text-sm text-muted-foreground">
							{t("docFolder.noSubfolders")}
						</p>
					) : (
						<ul>
							{folders.map((f) => (
								<li key={f.id}>
									<button
										type="button"
										className={cn(
											"flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-muted",
											f.id === currentFolderId && "text-muted-foreground",
										)}
										onClick={() =>
											setTrail([...trail, { id: f.id, name: f.name }])
										}
									>
										<FolderOpen className="size-4 shrink-0" />
										<span className="min-w-0 flex-1 truncate">{f.name}</span>
										{f.id === currentFolderId && (
											<span className="text-xs">{t("docFolder.current")}</span>
										)}
										<ChevronRight className="size-3.5 shrink-0" />
									</button>
								</li>
							))}
						</ul>
					)}
					<LoadMoreSentinel
						compact
						hasMore={!!children.hasNextPage}
						isLoadingMore={children.isFetchingNextPage}
						isError={children.isFetchNextPageError}
						onLoadMore={() => void children.fetchNextPage()}
					/>
				</div>
				<DialogFooter>
					<Button variant="outline" onClick={onClose}>
						{t("dialogs.cancel")}
					</Button>
					<Button
						disabled={!target || target === currentFolderId || pending}
						onClick={() => target && onMove(target)}
					>
						{t("docFolder.moveHere")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

// ── Adding files ──────────────────────────────────────────────────────────────

/**
 * The "Upload file" and "Add from project files" actions of a folder's add
 * menu, plus the hidden input and picker dialog they open. Uploading stores the
 * file straight into the folder; picking moves an existing, unmanaged project
 * file into it (a file has one folder). `items` goes inside the folder's
 * DropdownMenuContent, `dialogs` next to the menu.
 */
export function useDocFolderFileAdd({
	projectId,
	folderId,
	canUpload,
	canPick,
	onAdded,
}: {
	projectId: string;
	folderId: string;
	/** files:write, needed to store a new file. */
	canUpload: boolean;
	/** files:read + files:write, needed to move an existing file here. */
	canPick: boolean;
	/** Called before work starts, e.g. to expand the folder. */
	onAdded: () => void;
}) {
	const { t } = useTranslation("files");
	const qc = useQueryClient();
	const scope = { projectId };
	const fail = useFolderFileErrors();
	const input = useRef<HTMLInputElement>(null);
	const [picking, setPicking] = useState(false);

	const pick = useMutation({
		mutationFn: (fileId: string) => moveNode(scope, fileId, folderId),
		onSuccess: () => {
			setPicking(false);
			void invalidateFiles(qc, scope);
			toast.add({ type: "success", title: t("docFolder.toast.added") });
		},
		onError: fail,
	});

	const upload = async (list: FileList | null) => {
		const files = list ? [...list] : [];
		if (input.current) input.current.value = "";
		if (files.length === 0) return;
		onAdded();
		const id = toast.add({
			type: "loading",
			title: t("upload.uploading", { count: files.length }),
			timeout: 0,
		});
		let failed: unknown;
		let done = 0;
		for (const file of files) {
			try {
				// A Markdown file becomes a document in this folder.
				if (isMarkdownFile(file))
					await importMarkdownFile(projectId, folderId, file);
				else await uploadFile(scope, folderId, file);
				done++;
			} catch (error) {
				failed ??= error;
			}
		}
		toast.close(id);
		void invalidateFiles(qc, scope);
		void qc.invalidateQueries({ queryKey: docQueryKeys.all(projectId) });
		if (failed) fail(failed);
		if (done > 0)
			toast.add({ type: "success", title: t("docFolder.toast.uploaded") });
	};

	const items = (
		<>
			{canUpload && (
				<DropdownMenuItem onClick={() => input.current?.click()}>
					<Upload className="mr-2 size-3.5" />
					{t("docFolder.upload")}
				</DropdownMenuItem>
			)}
			{canPick && (
				<DropdownMenuItem
					onClick={() => {
						onAdded();
						setPicking(true);
					}}
				>
					<Plus className="mr-2 size-3.5" />
					{t("docFolder.fromFiles")}
				</DropdownMenuItem>
			)}
		</>
	);

	const dialogs = (
		<>
			<input
				ref={input}
				type="file"
				multiple
				hidden
				aria-label={t("docFolder.upload")}
				onChange={(e) => void upload(e.target.files)}
			/>
			{picking && (
				<Dialog open onOpenChange={(o) => !o && setPicking(false)}>
					<DialogContent className="sm:max-w-md">
						<DialogHeader>
							<DialogTitle>{t("docFolder.pickTitle")}</DialogTitle>
							<DialogDescription>{t("docFolder.pickHint")}</DialogDescription>
						</DialogHeader>
						<FilePicker
							projectId={projectId}
							// Managed files (task attachments, document files) and
							// anything already here cannot be moved in.
							accept={(n) => !n.locked && n.parent_id !== folderId}
							onPick={(n) => pick.mutate(n.id)}
						/>
					</DialogContent>
				</Dialog>
			)}
		</>
	);

	return { items, dialogs, hasActions: canUpload || canPick };
}
