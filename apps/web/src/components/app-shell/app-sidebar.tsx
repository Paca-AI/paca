import {
	useInfiniteQuery,
	useMutation,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import {
	Link,
	useNavigate,
	useParams,
	useRouterState,
} from "@tanstack/react-router";
import {
	Activity,
	ArrowLeft,
	BookOpen,
	Bot,
	CheckCircle2,
	ChevronDown,
	ChevronRight,
	Clock,
	CornerLeftUp,
	File,
	FileCode2,
	FileText,
	Folder,
	FolderKanban,
	FolderOpen,
	GanttChart,
	Home,
	KanbanSquare,
	Lock,
	MessageSquare,
	Monitor,
	Moon,
	MoreHorizontal,
	Pencil,
	Plus,
	Puzzle,
	Server,
	ServerCog,
	Settings,
	Shield,
	Sparkles,
	Sun,
	Trash2,
	Users,
	Workflow,
} from "lucide-react";
import {
	type ComponentType,
	createContext,
	type ReactNode,
	useCallback,
	useContext,
	useEffect,
	useMemo,
	useRef,
	useState,
} from "react";
import { useTranslation } from "react-i18next";
import { useManagedCopyLabel } from "@/components/projects/docs/managed-copy-banner";
import { EntityAvatar } from "@/components/shared/entity-avatar";
import { LoadMoreSentinel } from "@/components/shared/load-more-sentinel";
import { PaneResizeHandle } from "@/components/shared/pane-resize-handle";
import { Badge } from "@/components/ui/badge";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuGroup,
	DropdownMenuItem,
	DropdownMenuLabel,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import {
	Sidebar,
	SidebarContent,
	SidebarFooter,
	SidebarGroup,
	SidebarGroupContent,
	SidebarGroupLabel,
	SidebarHeader,
	SidebarMenu,
	SidebarMenuButton,
	SidebarMenuItem,
	SidebarRail,
	SidebarSeparator,
	useSidebar,
} from "@/components/ui/sidebar";
import { Spinner } from "@/components/ui/spinner";
import { toast } from "@/components/ui/toast";
import { useBranding } from "@/hooks/use-branding";
import { usePermissions } from "@/hooks/use-permissions";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import type { ThemeMode } from "@/hooks/use-theme-mode";
import { useThemeMode } from "@/hooks/use-theme-mode";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import { currentUserQueryOptions } from "@/lib/auth-api";
import {
	createDocument,
	createFolder,
	type DocFolder,
	type Document,
	deleteDocument,
	deleteFolder,
	docFoldersInfiniteQueryOptions,
	docQueryKeys,
	docsInfiniteQueryOptions,
	updateDocument,
	updateFolder,
} from "@/lib/doc-api";
import { sprintsInfiniteQueryOptions, updateTask } from "@/lib/interaction-api";
import { notifyError } from "@/lib/notify";
import { SIDEBAR_PANE, usePaneWidth } from "@/lib/pane-width";
import type { PluginNavRegistration } from "@/lib/plugin-api";
import { ExtensionPoint } from "@/lib/plugins/extension-point";
import { resolvePluginIcon } from "@/lib/plugins/icon-resolver";
import { usePluginRegistry } from "@/lib/plugins/registry";
import {
	projectQueryOptions,
	projectsInfiniteQueryOptions,
} from "@/lib/project-api";
import { projectAvatar } from "@/lib/provider-logos";
import { createLoadMoreScrollHandler } from "@/lib/scroll-pagination";
import { cn } from "@/lib/utils";
import {
	DOC_NODE_DRAG_TYPE,
	type DocNodeDragPayload,
	isMoveBlocked,
	parseDocNodeDragPayload,
} from "./doc-dnd";
import { UserMenu } from "./user-menu";

// Shared by every inactive nav item (top-level, project, plugin, and
// interaction rows) so the sidebar's neutral-grey text color — deliberately
// not tracking --primary, since that now follows the admin-configurable
// brand color — stays consistent without repeating this string at each of
// the many call sites below.
const NAV_ITEM_INACTIVE_CLASS =
	"text-sidebar-foreground/80 dark:text-sidebar-foreground/60 hover:bg-sidebar-accent/60 hover:text-sidebar-foreground";

// ── Docs Tree ─────────────────────────────────────────────────────────────────

/** Tiny inline rename input used in the sidebar tree */
function TreeInlineRename({
	initialValue,
	onConfirm,
	onCancel,
}: {
	initialValue: string;
	onConfirm: (v: string) => void;
	onCancel: () => void;
}) {
	const [value, setValue] = useState(initialValue);
	const confirmedRef = useRef(false);

	const confirm = useCallback(() => {
		if (confirmedRef.current) return;
		confirmedRef.current = true;
		const trimmed = value.trim();
		if (trimmed) onConfirm(trimmed);
		else onCancel();
	}, [value, onConfirm, onCancel]);

	return (
		<Input
			autoFocus
			value={value}
			className="h-6 text-sm px-1.5 rounded border border-primary/30 bg-sidebar focus:ring-1 focus:ring-primary/25 flex-1 min-w-0"
			onChange={(e) => setValue(e.target.value)}
			onFocus={(e) => e.target.select()}
			onKeyDown={(e) => {
				if (e.key === "Enter") confirm();
				else if (e.key === "Escape") onCancel();
				e.stopPropagation();
			}}
			onBlur={confirm}
			onClick={(e) => e.stopPropagation()}
		/>
	);
}

const ROOT_DROP_TARGET = "root";

/** The one drop target currently highlighted ("root" or a folder id), shared
 *  by the whole docs tree so nested targets never highlight together. */
const DocDropTargetContext = createContext<{
	active: string | null;
	setActive: (target: string | null) => void;
}>({ active: null, setActive: () => {} });

/** Drag source props for a document or folder row's main button. They live on
 *  the button itself because Firefox won't start a drag from a button nested
 *  in a draggable parent. */
function docNodeDragProps(
	payload: DocNodeDragPayload,
	enabled: boolean,
): React.HTMLAttributes<HTMLElement> & { draggable?: boolean } {
	if (!enabled) return {};
	return {
		draggable: true,
		onDragStart: (e) => {
			e.stopPropagation();
			e.dataTransfer.effectAllowed = "move";
			e.dataTransfer.setData(DOC_NODE_DRAG_TYPE, JSON.stringify(payload));
		},
	};
}

/** Moves a document or folder into `targetFolderId` (null = top level). Only
 *  the source and target folders' lists (and folder paths) are refetched. */
function useMoveDocNode(projectId: string) {
	const { t } = useTranslation("appShell");
	const qc = useQueryClient();
	return useMutation({
		mutationFn: async (v: {
			payload: DocNodeDragPayload;
			targetFolderId: string | null;
		}): Promise<void> => {
			const { payload: p, targetFolderId } = v;
			if (p.kind === "folder") {
				await updateFolder(projectId, p.id, { parent_id: targetFolderId });
			} else {
				await updateDocument(projectId, p.id, { folder_id: targetFolderId });
			}
		},
		onError: (err: unknown) => {
			const code = getApiErrorCode(err);
			if (
				code === ApiErrorCode.DocFolderCycle ||
				code === ApiErrorCode.DocFolderSelfParent
			) {
				toast.add({ type: "error", title: t("docs.errors.moveIntoSelf") });
			} else {
				notifyError(err, t("docs.errors.moveFailed"));
			}
		},
		onSettled: (_data, _err, v) => {
			const { payload: p, targetFolderId } = v;
			for (const folderId of [p.parentId, targetFolderId]) {
				qc.invalidateQueries({
					queryKey:
						p.kind === "folder"
							? docQueryKeys.folderChildren(projectId, folderId)
							: docQueryKeys.docChildren(projectId, folderId),
				});
			}
			if (p.kind === "folder") {
				// Every descendant's path changes too, so mark all paths stale.
				qc.invalidateQueries({
					queryKey: [...docQueryKeys.all(projectId), "folders", "path"],
				});
			} else {
				qc.invalidateQueries({
					queryKey: docQueryKeys.detail(projectId, p.id),
				});
			}
		},
	});
}

/** Makes an element a drop target that moves dragged documents/folders into
 *  `targetFolderId` (null = the top level of the tree). `targetPath` is the
 *  target's id preceded by its ancestors' ids (empty for the top level). */
function useDocNodeDrop(
	projectId: string,
	targetFolderId: string | null,
	targetPath: readonly string[],
	canWrite: boolean,
	onMoved?: () => void,
) {
	const { active, setActive } = useContext(DocDropTargetContext);
	const targetKey = targetFolderId ?? ROOT_DROP_TARGET;
	const isOver = active === targetKey;
	const moveMutation = useMoveDocNode(projectId);

	const dropProps = canWrite
		? {
				onDragOver: (e: React.DragEvent) => {
					if (!e.dataTransfer.types.includes(DOC_NODE_DRAG_TYPE)) return;
					// The innermost folder under the cursor is the target.
					e.preventDefault();
					e.stopPropagation();
					e.dataTransfer.dropEffect = "move";
					setActive(targetKey);
				},
				onDragLeave: (e: React.DragEvent) => {
					if (!e.currentTarget.contains(e.relatedTarget as Node | null)) {
						setActive(null);
					}
				},
				onDrop: (e: React.DragEvent) => {
					const raw = e.dataTransfer.getData(DOC_NODE_DRAG_TYPE);
					if (!raw) return;
					e.preventDefault();
					e.stopPropagation();
					setActive(null);
					const payload = parseDocNodeDragPayload(raw, projectId);
					if (!payload || isMoveBlocked(payload, targetFolderId, targetPath)) {
						return;
					}
					moveMutation.mutate(
						{ payload, targetFolderId },
						{ onSuccess: onMoved },
					);
				},
			}
		: {};

	return { isOver, dropProps };
}

/** The top level of the docs tree: dropping here moves an item out of its folder. */
function DocsRootDropArea({
	projectId,
	canWrite,
	children,
}: {
	projectId: string;
	canWrite: boolean;
	children: ReactNode;
}) {
	const { isOver, dropProps } = useDocNodeDrop(projectId, null, [], canWrite);
	return (
		<div
			className={cn(
				"py-1 space-y-0.5 rounded-md transition-colors",
				isOver && "bg-primary/10 ring-1 ring-primary/40",
			)}
			{...dropProps}
		>
			{children}
		</div>
	);
}

/** Marks a tree row whose document is a managed copy of a repository file.
 *  The label is part of the row button's accessible name and its tooltip. */
function ManagedCopyMarker({
	label,
	locked,
}: {
	label: string;
	locked: boolean;
}) {
	const Icon = locked ? Lock : FileCode2;
	return (
		<span
			role="img"
			aria-label={label}
			title={label}
			data-testid={locked ? "doc-locked-marker" : "doc-managed-marker"}
			className="ml-auto flex shrink-0 items-center text-sidebar-foreground/40"
		>
			<Icon aria-hidden className="size-3" />
		</span>
	);
}

/** Single document row in the sidebar tree */
function DocsDocRow({
	doc,
	projectId,
	canWrite,
	depth,
}: {
	doc: Document;
	projectId: string;
	canWrite: boolean;
	depth: number;
}) {
	const { t } = useTranslation("appShell");
	const { t: tProjects } = useTranslation("projects");
	const managedCopyLabel = useManagedCopyLabel();
	const location = useRouterState({ select: (s) => s.location.pathname });
	const moveMutation = useMoveDocNode(projectId);
	const navigate = useNavigate();
	const qc = useQueryClient();
	const [renaming, setRenaming] = useState(false);
	const [renameError, setRenameError] = useState<string | null>(null);
	// The title last submitted, so a failed attempt reopens the editor with
	// what the person typed instead of reverting to the original title.
	const [pendingTitle, setPendingTitle] = useState<string | null>(null);
	// Bumped on every failed attempt to remount TreeInlineRename — it tracks
	// "already confirmed" internally to dedupe Enter+blur, so without a fresh
	// instance a retry after an error couldn't re-confirm.
	const [renameAttempt, setRenameAttempt] = useState(0);

	const isActive = location === `/projects/${projectId}/docs/${doc.id}`;

	const renameMutation = useMutation({
		meta: { success: "renamed", subject: "document" },
		mutationFn: (title: string) => updateDocument(projectId, doc.id, { title }),
		onSuccess: (updated) => {
			setRenameError(null);
			setRenaming(false);
			setPendingTitle(null);
			qc.setQueryData(docQueryKeys.detail(projectId, doc.id), updated);
			qc.invalidateQueries({ queryKey: docQueryKeys.list(projectId) });
			if (doc.folder_id) {
				qc.invalidateQueries({
					queryKey: docQueryKeys.list(projectId, doc.folder_id),
				});
			}
		},
		onError: (err: unknown) => {
			setRenameError(
				getApiErrorCode(err) === ApiErrorCode.DocTitleInvalid
					? t("docs.errors.titleInvalid")
					: t("docs.errors.renameFailed"),
			);
			setRenameAttempt((n) => n + 1);
		},
	});

	const deleteMutation = useMutation({
		meta: { success: "deleted", subject: "document" },
		mutationFn: () => deleteDocument(projectId, doc.id),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: docQueryKeys.all(projectId) });
			if (isActive) {
				navigate({ to: "/projects/$projectId", params: { projectId } });
			}
		},
	});

	return (
		<>
			<div
				className="group relative flex items-center gap-1 pr-1"
				style={{ paddingLeft: `${8 + depth * 16 + 16}px` }}
			>
				<button
					type="button"
					{...docNodeDragProps(
						{
							kind: "doc",
							id: doc.id,
							projectId,
							parentId: doc.folder_id ?? null,
						},
						canWrite && !renaming,
					)}
					className={cn(
						"flex flex-1 min-w-0 items-center gap-1.5 rounded-md px-2 py-1 cursor-pointer transition-all duration-150 text-sm",
						isActive
							? "bg-primary/10 text-primary font-medium"
							: "text-sidebar-foreground/70 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground",
					)}
					onClick={() => {
						if (renaming) return;
						navigate({
							to: "/projects/$projectId/docs/$docId",
							params: { projectId, docId: doc.id },
						});
					}}
					onKeyDown={(e) => {
						if (e.key === "Enter" || e.key === " ") {
							navigate({
								to: "/projects/$projectId/docs/$docId",
								params: { projectId, docId: doc.id },
							});
						}
					}}
				>
					<FileText
						className={cn(
							"size-3.5 shrink-0 transition-colors",
							isActive ? "text-primary/70" : "text-sidebar-foreground/40",
						)}
					/>
					{renaming ? (
						<TreeInlineRename
							key={renameAttempt}
							initialValue={pendingTitle ?? (doc.title || t("docs.untitled"))}
							onConfirm={(title) => {
								setPendingTitle(title);
								renameMutation.mutate(title);
							}}
							onCancel={() => {
								setRenaming(false);
								setRenameError(null);
								setPendingTitle(null);
							}}
						/>
					) : (
						<span className="truncate leading-snug">
							{doc.title || (
								<span className="italic text-sidebar-foreground/40">
									{t("docs.untitled")}
								</span>
							)}
						</span>
					)}
					{doc.source && !renaming && (
						<ManagedCopyMarker
							label={
								doc.source_locked
									? `${managedCopyLabel(doc.source)}. ${tProjects("docs.managedCopy.locked")}`
									: managedCopyLabel(doc.source)
							}
							locked={doc.source_locked}
						/>
					)}
				</button>

				{canWrite && !renaming && (
					<DropdownMenu>
						<DropdownMenuTrigger
							className="opacity-0 group-hover:opacity-100 flex size-5 shrink-0 items-center justify-center rounded text-sidebar-foreground/40 hover:text-sidebar-foreground hover:bg-sidebar-accent/60 transition-all duration-150"
							onClick={(e) => e.stopPropagation()}
						>
							<MoreHorizontal className="size-3" />
						</DropdownMenuTrigger>
						<DropdownMenuContent align="start" className="w-36">
							<DropdownMenuItem
								// A locked managed copy's title comes from its repository.
								// It stays focusable and hoverable so the reason can be read.
								disabled={doc.source_locked}
								className={
									doc.source_locked
										? "data-disabled:pointer-events-auto"
										: undefined
								}
								title={
									doc.source_locked
										? tProjects("docs.managedCopy.locked")
										: undefined
								}
								onClick={(e) => {
									e.stopPropagation();
									setRenameError(null);
									setRenaming(true);
								}}
							>
								<Pencil className="size-3.5 mr-2" />
								{t("docs.rename")}
								{doc.source_locked && (
									<span className="sr-only">
										{tProjects("docs.managedCopy.locked")}
									</span>
								)}
							</DropdownMenuItem>
							{doc.folder_id && (
								<DropdownMenuItem
									onClick={(e) => {
										e.stopPropagation();
										moveMutation.mutate({
											payload: {
												kind: "doc",
												id: doc.id,
												projectId,
												parentId: doc.folder_id ?? null,
											},
											targetFolderId: null,
										});
									}}
								>
									<CornerLeftUp className="size-3.5 mr-2" />
									{t("docs.moveToTop")}
								</DropdownMenuItem>
							)}
							<DropdownMenuSeparator />
							<DropdownMenuItem
								className="text-destructive focus:text-destructive"
								onClick={(e) => {
									e.stopPropagation();
									deleteMutation.mutate();
								}}
							>
								<Trash2 className="size-3.5 mr-2" />
								{t("docs.delete")}
							</DropdownMenuItem>
						</DropdownMenuContent>
					</DropdownMenu>
				)}
			</div>
			{renameError && (
				<p
					className="text-xs text-destructive px-2 py-0.5"
					style={{ paddingLeft: `${8 + depth * 16 + 16}px` }}
				>
					{renameError}
				</p>
			)}
		</>
	);
}

/** Folder node — fetches its docs lazily when expanded */
function DocsFolderNode({
	folder,
	projectId,
	canWrite,
	expandedFolders,
	onToggle,
	depth,
	ancestorIds = [],
}: {
	folder: DocFolder;
	projectId: string;
	canWrite: boolean;
	expandedFolders: Set<string>;
	onToggle: (id: string) => void;
	depth: number;
	/** Ids of the folders above this one, outermost first. */
	ancestorIds?: readonly string[];
}) {
	const { t } = useTranslation("appShell");
	const qc = useQueryClient();
	const [renaming, setRenaming] = useState(false);
	const [renameError, setRenameError] = useState<string | null>(null);
	// The name last submitted, so a failed attempt reopens the editor with
	// what the person typed instead of reverting to the original name.
	const [pendingName, setPendingName] = useState<string | null>(null);
	// Bumped on every failed attempt to remount TreeInlineRename — it tracks
	// "already confirmed" internally to dedupe Enter+blur, so without a fresh
	// instance a retry after an error couldn't re-confirm.
	const [renameAttempt, setRenameAttempt] = useState(0);
	const [addingDoc, setAddingDoc] = useState(false);
	const navigate = useNavigate();

	const isExpanded = expandedFolders.has(folder.id);
	const moveMutation = useMoveDocNode(projectId);
	const folderPath = useMemo(
		() => [...ancestorIds, folder.id],
		[ancestorIds, folder.id],
	);
	const { isOver, dropProps } = useDocNodeDrop(
		projectId,
		folder.id,
		folderPath,
		canWrite,
		// A collapsed target would swallow the item; open it so the move shows.
		() => {
			if (!expandedFolders.has(folder.id)) onToggle(folder.id);
		},
	);

	// A folder's subfolders and documents are fetched only once it is
	// expanded, a page at a time.
	const childFoldersQuery = useInfiniteQuery({
		...docFoldersInfiniteQueryOptions(projectId, folder.id),
		enabled: isExpanded,
	});
	const docsQuery = useInfiniteQuery({
		...docsInfiniteQueryOptions(projectId, folder.id),
		enabled: isExpanded,
	});
	const childFolders = useMemo(
		() => childFoldersQuery.data?.pages.flatMap((p) => p.items) ?? [],
		[childFoldersQuery.data],
	);
	const folderDocs = useMemo(
		() => docsQuery.data?.pages.flatMap((p) => p.items) ?? [],
		[docsQuery.data],
	);
	const renameMutation = useMutation({
		meta: { success: "renamed", subject: "folder" },
		mutationFn: (name: string) => updateFolder(projectId, folder.id, { name }),
		onSuccess: () => {
			setRenameError(null);
			setRenaming(false);
			setPendingName(null);
			qc.invalidateQueries({ queryKey: docQueryKeys.folders(projectId) });
		},
		onError: (err: unknown) => {
			setRenameError(
				getApiErrorCode(err) === ApiErrorCode.DocFolderNameInvalid
					? t("docs.errors.folderNameInvalid")
					: t("docs.errors.renameFailed"),
			);
			setRenameAttempt((n) => n + 1);
		},
	});

	const deleteMutation = useMutation({
		meta: { success: "deleted", subject: "folder" },
		mutationFn: () => deleteFolder(projectId, folder.id),
		onSuccess: () =>
			qc.invalidateQueries({ queryKey: docQueryKeys.folders(projectId) }),
	});

	const newDocMutation = useMutation({
		meta: { success: "created", subject: "document" },
		mutationFn: () =>
			createDocument(projectId, {
				title: t("docs.untitled"),
				folder_id: folder.id,
			}),
		onSuccess: (doc) => {
			qc.invalidateQueries({ queryKey: docQueryKeys.all(projectId) });
			setAddingDoc(false);
			navigate({
				to: "/projects/$projectId/docs/$docId",
				params: { projectId, docId: doc.id },
			});
		},
	});

	const newSubfolderMutation = useMutation({
		meta: { success: "created", subject: "folder" },
		mutationFn: (name: string) =>
			createFolder(projectId, { name, parent_id: folder.id }),
		onSuccess: () =>
			qc.invalidateQueries({ queryKey: docQueryKeys.folders(projectId) }),
	});

	return (
		<div
			className={cn(
				"rounded-md transition-colors",
				isOver && "bg-primary/10 ring-1 ring-primary/40",
			)}
			{...dropProps}
		>
			{/* Folder row */}
			<div
				className="group relative flex items-center gap-1 pr-1"
				style={{ paddingLeft: `${8 + depth * 16}px` }}
			>
				<button
					type="button"
					{...docNodeDragProps(
						{
							kind: "folder",
							id: folder.id,
							projectId,
							parentId: folder.parent_id ?? null,
						},
						canWrite && !renaming,
					)}
					className="flex flex-1 min-w-0 items-center gap-1.5 rounded-md px-1.5 py-1 cursor-pointer transition-all duration-150 text-sm text-sidebar-foreground/70 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
					onClick={() => {
						if (!renaming) onToggle(folder.id);
					}}
					onKeyDown={(e) => {
						if ((e.key === "Enter" || e.key === " ") && !renaming)
							onToggle(folder.id);
					}}
				>
					<ChevronRight
						className={cn(
							"size-3 shrink-0 text-sidebar-foreground/30 transition-transform duration-150",
							isExpanded && "rotate-90",
						)}
					/>
					{isExpanded ? (
						<FolderOpen className="size-3.5 shrink-0 text-sidebar-foreground/40" />
					) : (
						<Folder className="size-3.5 shrink-0 text-sidebar-foreground/40" />
					)}
					{renaming ? (
						<TreeInlineRename
							key={renameAttempt}
							initialValue={pendingName ?? folder.name}
							onConfirm={(name) => {
								setPendingName(name);
								renameMutation.mutate(name);
							}}
							onCancel={() => {
								setRenaming(false);
								setRenameError(null);
								setPendingName(null);
							}}
						/>
					) : (
						<span className="truncate leading-snug font-medium">
							{folder.name}
						</span>
					)}
				</button>

				{canWrite && !renaming && (
					<div className="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity duration-150">
						<DropdownMenu>
							<DropdownMenuTrigger
								className="flex size-5 items-center justify-center rounded text-sidebar-foreground/40 hover:text-sidebar-foreground hover:bg-sidebar-accent/60 transition-all duration-150"
								onClick={(e) => e.stopPropagation()}
							>
								<MoreHorizontal className="size-3" />
							</DropdownMenuTrigger>
							<DropdownMenuContent align="start" className="w-36">
								<DropdownMenuItem
									onClick={(e) => {
										e.stopPropagation();
										setRenameError(null);
										setRenaming(true);
									}}
								>
									<Pencil className="size-3.5 mr-2" />
									{t("docs.rename")}
								</DropdownMenuItem>
								{folder.parent_id && (
									<DropdownMenuItem
										onClick={(e) => {
											e.stopPropagation();
											moveMutation.mutate({
												payload: {
													kind: "folder",
													id: folder.id,
													projectId,
													parentId: folder.parent_id ?? null,
												},
												targetFolderId: null,
											});
										}}
									>
										<CornerLeftUp className="size-3.5 mr-2" />
										{t("docs.moveToTop")}
									</DropdownMenuItem>
								)}
								<DropdownMenuSeparator />
								<DropdownMenuItem
									className="text-destructive focus:text-destructive"
									onClick={(e) => {
										e.stopPropagation();
										deleteMutation.mutate();
									}}
								>
									<Trash2 className="size-3.5 mr-2" />
									{t("docs.delete")}
								</DropdownMenuItem>
							</DropdownMenuContent>
						</DropdownMenu>
					</div>
				)}
			</div>

			{renameError && (
				<p
					className="text-xs text-destructive px-2 py-0.5"
					style={{ paddingLeft: `${8 + depth * 16 + 26}px` }}
				>
					{renameError}
				</p>
			)}

			{/* Children */}
			{isExpanded && (
				<div>
					{childFolders.map((cf) => (
						<DocsFolderNode
							key={cf.id}
							folder={cf}
							projectId={projectId}
							canWrite={canWrite}
							expandedFolders={expandedFolders}
							onToggle={onToggle}
							depth={depth + 1}
							ancestorIds={folderPath}
						/>
					))}
					<div style={{ paddingLeft: `${8 + (depth + 1) * 16}px` }}>
						<LoadMoreSentinel
							compact
							hasMore={!!childFoldersQuery.hasNextPage}
							isLoadingMore={childFoldersQuery.isFetchingNextPage}
							isError={childFoldersQuery.isFetchNextPageError}
							onLoadMore={() => void childFoldersQuery.fetchNextPage()}
						/>
					</div>
					{folderDocs.map((doc) => (
						<DocsDocRow
							key={doc.id}
							doc={doc}
							projectId={projectId}
							canWrite={canWrite}
							depth={depth + 1}
						/>
					))}
					<div style={{ paddingLeft: `${8 + (depth + 1) * 16}px` }}>
						<LoadMoreSentinel
							compact
							hasMore={!!docsQuery.hasNextPage}
							isLoadingMore={docsQuery.isFetchingNextPage}
							isError={docsQuery.isFetchNextPageError}
							onLoadMore={() => void docsQuery.fetchNextPage()}
						/>
					</div>
					{folderDocs.length === 0 &&
						childFolders.length === 0 &&
						!addingDoc && (
							<div
								className="text-xs text-sidebar-foreground/30 italic py-1"
								style={{ paddingLeft: `${8 + (depth + 1) * 16 + 26}px` }}
							>
								{t("docs.emptyFolder")}
							</div>
						)}
					{canWrite && (
						<div style={{ paddingLeft: `${8 + (depth + 1) * 16 + 16}px` }}>
							<DropdownMenu>
								<DropdownMenuTrigger className="flex w-full items-center gap-1.5 rounded-md px-2 py-1 text-xs text-sidebar-foreground/35 hover:text-sidebar-foreground hover:bg-sidebar-accent/40 transition-all duration-150">
									<Plus className="size-3 shrink-0" />
									<span>{t("docs.add")}</span>
								</DropdownMenuTrigger>
								<DropdownMenuContent align="start" className="w-40">
									<DropdownMenuItem
										onClick={() => {
											if (!isExpanded) onToggle(folder.id);
											newDocMutation.mutate();
										}}
										disabled={newDocMutation.isPending}
									>
										<File className="size-3.5 mr-2" />
										{t("docs.newDocument")}
									</DropdownMenuItem>
									<DropdownMenuItem
										onClick={() => {
											if (!isExpanded) onToggle(folder.id);
											newSubfolderMutation.mutate(
												t("docs.newFolderDefaultName"),
											);
										}}
										disabled={newSubfolderMutation.isPending}
									>
										<FolderOpen className="size-3.5 mr-2" />
										{t("docs.newSubfolder")}
									</DropdownMenuItem>
								</DropdownMenuContent>
							</DropdownMenu>
						</div>
					)}
				</div>
			)}
		</div>
	);
}

/** The full docs tree sidebar section — shown when in project context */
function DocsSidebarSection({ projectId }: { projectId: string }) {
	const { t } = useTranslation("appShell");
	const qc = useQueryClient();
	const navigate = useNavigate();
	const location = useRouterState({ select: (s) => s.location.pathname });
	const { hasProjectPermission } = useProjectPermissions(projectId);
	const canWrite = hasProjectPermission("docs:write");

	const isDocsSection = location.startsWith(`/projects/${projectId}/docs`);

	const [collapsed, setCollapsed] = useState(() => {
		try {
			return (
				localStorage.getItem(`paca:sidebar-docs-collapsed:${projectId}`) ===
				"true"
			);
		} catch {
			return false;
		}
	});

	// Auto-expand once when first navigating into docs (user can still collapse manually)
	const autoExpandedRef = useRef(false);
	useEffect(() => {
		if (isDocsSection && !autoExpandedRef.current) {
			autoExpandedRef.current = true;
			setCollapsed(false);
			try {
				localStorage.removeItem(`paca:sidebar-docs-collapsed:${projectId}`);
			} catch {
				/* ignore */
			}
		}
		if (!isDocsSection) {
			autoExpandedRef.current = false;
		}
	}, [isDocsSection, projectId]);

	const [expandedFolders, setExpandedFolders] = useState<Set<string>>(() => {
		try {
			const stored = localStorage.getItem(
				`paca:sidebar-docs-expanded:${projectId}`,
			);
			return stored ? new Set(JSON.parse(stored)) : new Set();
		} catch {
			return new Set();
		}
	});

	const toggleFolder = useCallback(
		(folderId: string) => {
			setExpandedFolders((prev) => {
				const next = new Set(prev);
				if (next.has(folderId)) next.delete(folderId);
				else next.add(folderId);
				try {
					localStorage.setItem(
						`paca:sidebar-docs-expanded:${projectId}`,
						JSON.stringify([...next]),
					);
				} catch {
					/* ignore */
				}
				return next;
			});
		},
		[projectId],
	);

	// The top level of the tree, a page of folders and a page of documents at
	// a time; deeper levels load when their folder is expanded.
	const rootFoldersQuery = useInfiniteQuery(
		docFoldersInfiniteQueryOptions(projectId, null),
	);
	const rootDocsQuery = useInfiniteQuery(
		docsInfiniteQueryOptions(projectId, null),
	);
	const rootFolders = useMemo(
		() => rootFoldersQuery.data?.pages.flatMap((p) => p.items) ?? [],
		[rootFoldersQuery.data],
	);
	const rootOnlyDocs = useMemo(
		() => rootDocsQuery.data?.pages.flatMap((p) => p.items) ?? [],
		[rootDocsQuery.data],
	);

	const newDocMutation = useMutation({
		meta: { success: "created", subject: "document" },
		mutationFn: () => createDocument(projectId, { title: t("docs.untitled") }),
		onSuccess: (doc) => {
			qc.invalidateQueries({ queryKey: docQueryKeys.all(projectId) });
			navigate({
				to: "/projects/$projectId/docs/$docId",
				params: { projectId, docId: doc.id },
			});
		},
	});

	const newFolderMutation = useMutation({
		meta: { success: "created", subject: "folder" },
		mutationFn: (name: string) => createFolder(projectId, { name }),
		onSuccess: () =>
			qc.invalidateQueries({ queryKey: docQueryKeys.folders(projectId) }),
	});

	// The single highlighted drop target, cleared whenever a drag ends.
	const [activeDropTarget, setActiveDropTarget] = useState<string | null>(null);
	const dropTargetValue = useMemo(
		() => ({ active: activeDropTarget, setActive: setActiveDropTarget }),
		[activeDropTarget],
	);
	useEffect(() => {
		const clear = () => setActiveDropTarget(null);
		document.addEventListener("dragend", clear);
		return () => document.removeEventListener("dragend", clear);
	}, []);

	const toggleCollapse = () => {
		setCollapsed((prev) => {
			const next = !prev;
			try {
				if (next) {
					localStorage.setItem(
						`paca:sidebar-docs-collapsed:${projectId}`,
						"true",
					);
				} else {
					localStorage.removeItem(`paca:sidebar-docs-collapsed:${projectId}`);
				}
			} catch {
				/* ignore */
			}
			return next;
		});
	};

	const isEmpty = rootFolders.length === 0 && rootOnlyDocs.length === 0;
	const { state: sidebarState, isMobile } = useSidebar();
	const isSidebarCollapsed = sidebarState === "collapsed" && !isMobile;

	// A bare book icon that does nothing is noise on the rail.
	if (isSidebarCollapsed) return null;

	return (
		<SidebarGroup className="px-0">
			{/* Section header */}
			<SidebarGroupLabel
				render={<button type="button" />}
				aria-expanded={!collapsed}
				className="w-full text-left flex cursor-pointer items-center justify-between hover:text-sidebar-foreground transition-colors px-3"
				onClick={toggleCollapse}
			>
				<span>{t("docs.documentation")}</span>
				<ChevronRight
					className={cn(
						"size-3.5 transition-transform duration-200 text-sidebar-foreground/40",
						!collapsed && "rotate-90",
					)}
				/>
			</SidebarGroupLabel>

			{!collapsed && (
				<SidebarGroupContent>
					<DocDropTargetContext.Provider value={dropTargetValue}>
						<DocsRootDropArea projectId={projectId} canWrite={canWrite}>
							{isEmpty ? (
								<div className="px-4 py-2 text-xs text-sidebar-foreground/40 italic">
									{t("docs.noDocumentsYet")}
								</div>
							) : (
								<>
									{rootFolders.map((folder) => (
										<DocsFolderNode
											key={folder.id}
											folder={folder}
											projectId={projectId}
											canWrite={canWrite}
											expandedFolders={expandedFolders}
											onToggle={toggleFolder}
											depth={0}
										/>
									))}
									<div className="px-2">
										<LoadMoreSentinel
											compact
											hasMore={!!rootFoldersQuery.hasNextPage}
											isLoadingMore={rootFoldersQuery.isFetchingNextPage}
											isError={rootFoldersQuery.isFetchNextPageError}
											onLoadMore={() => void rootFoldersQuery.fetchNextPage()}
										/>
									</div>
									{rootOnlyDocs.map((doc) => (
										<DocsDocRow
											key={doc.id}
											doc={doc}
											projectId={projectId}
											canWrite={canWrite}
											depth={0}
										/>
									))}
									<div className="px-2">
										<LoadMoreSentinel
											compact
											hasMore={!!rootDocsQuery.hasNextPage}
											isLoadingMore={rootDocsQuery.isFetchingNextPage}
											isError={rootDocsQuery.isFetchNextPageError}
											onLoadMore={() => void rootDocsQuery.fetchNextPage()}
										/>
									</div>
								</>
							)}
							{canWrite && (
								<div className="px-2 pt-1">
									<DropdownMenu>
										<DropdownMenuTrigger className="flex w-full items-center gap-1.5 rounded-md px-2 py-1 text-xs text-sidebar-foreground/35 hover:text-sidebar-foreground hover:bg-sidebar-accent/40 transition-all duration-150">
											<Plus className="size-3 shrink-0" />
											<span>{t("docs.add")}</span>
										</DropdownMenuTrigger>
										<DropdownMenuContent align="start" className="w-40">
											<DropdownMenuItem
												onClick={() => newDocMutation.mutate()}
												disabled={newDocMutation.isPending}
											>
												<File className="size-3.5 mr-2" />
												{t("docs.newDocument")}
											</DropdownMenuItem>
											<DropdownMenuItem
												onClick={() =>
													newFolderMutation.mutate(
														t("docs.newFolderDefaultName"),
													)
												}
												disabled={newFolderMutation.isPending}
											>
												<FolderOpen className="size-3.5 mr-2" />
												{t("docs.newFolder")}
											</DropdownMenuItem>
										</DropdownMenuContent>
									</DropdownMenu>
								</div>
							)}
						</DocsRootDropArea>
					</DocDropTargetContext.Provider>
				</SidebarGroupContent>
			)}
		</SidebarGroup>
	);
}

// ── Project Switcher ───────────────────────────────────────────────────────────
function ProjectSwitcher({
	currentProjectId,
	canCreate,
}: {
	currentProjectId?: string;
	canCreate: boolean;
}) {
	const { t } = useTranslation("appShell");
	const [open, setOpen] = useState(false);
	const {
		data: projectsData,
		fetchNextPage,
		hasNextPage,
		isFetchingNextPage,
	} = useInfiniteQuery(projectsInfiniteQueryOptions());
	const { data: currentProject } = useQuery({
		...projectQueryOptions(currentProjectId ?? ""),
		enabled: !!currentProjectId,
	});

	const projects = useMemo(
		() => projectsData?.pages.flatMap((p) => p.items) ?? [],
		[projectsData],
	);
	const handleProjectsScroll = createLoadMoreScrollHandler({
		hasMore: !!hasNextPage,
		isLoadingMore: isFetchingNextPage,
		onLoadMore: () => void fetchNextPage(),
	});
	const label = currentProject?.name ?? t("projectSwitcher.projects");

	const { data: user } = useQuery(currentUserQueryOptions);

	if (!user) {
		return (
			<div className="flex w-full items-center gap-2.5 rounded-lg px-2 py-1.5 text-sm font-medium text-sidebar-foreground/80 select-none">
				<EntityAvatar
					{...(currentProject ? projectAvatar(currentProject) : { name: "" })}
					fallback={<FolderKanban className="size-3" />}
					size="xs"
					shape="rounded"
					className="shrink-0"
				/>
				<span className="flex-1 truncate text-left">{label}</span>
			</div>
		);
	}

	return (
		<DropdownMenu open={open} onOpenChange={setOpen}>
			<DropdownMenuTrigger
				className={cn(
					"flex w-full items-center gap-2.5 rounded-lg px-2 py-1.5 text-sm font-medium text-sidebar-foreground/80 transition-all duration-150 select-none cursor-pointer",
					open
						? "bg-primary/10 text-primary"
						: "hover:bg-sidebar-accent/60 hover:text-sidebar-foreground",
				)}
			>
				<EntityAvatar
					{...(currentProject ? projectAvatar(currentProject) : { name: "" })}
					fallback={<FolderKanban className="size-3" />}
					size="xs"
					shape="rounded"
					className="shrink-0"
				/>
				<span className="flex-1 truncate text-left">{label}</span>
				<ChevronDown
					className={cn(
						"size-3.5 shrink-0 opacity-40 transition-transform duration-200",
						open && "rotate-180",
					)}
				/>
			</DropdownMenuTrigger>
			<DropdownMenuContent
				align="start"
				sideOffset={6}
				className="w-60"
				onScroll={handleProjectsScroll}
			>
				<DropdownMenuGroup>
					<DropdownMenuLabel className="text-xs text-muted-foreground pb-1">
						{t("projectSwitcher.yourProjects")}
					</DropdownMenuLabel>
				</DropdownMenuGroup>
				<DropdownMenuSeparator />
				{projects.length > 0 ? (
					<DropdownMenuGroup>
						{projects.map((p) => (
							<DropdownMenuItem
								key={p.id}
								render={
									<Link
										to="/projects/$projectId"
										params={{ projectId: p.id }}
										className="flex items-center gap-2"
									/>
								}
							>
								<EntityAvatar
									{...projectAvatar(p)}
									size="xs"
									shape="rounded"
									className="shrink-0"
								/>
								<span className="truncate">{p.name}</span>
								{p.id === currentProjectId && (
									<span className="ml-auto size-1.5 rounded-full bg-primary" />
								)}
							</DropdownMenuItem>
						))}
						{isFetchingNextPage ? (
							<div className="flex items-center justify-center gap-1.5 py-2 text-xs text-muted-foreground">
								<Spinner aria-hidden="true" className="size-3" />
								{t("projectSwitcher.loadingMore")}
							</div>
						) : null}
					</DropdownMenuGroup>
				) : (
					<div className="flex flex-col items-center gap-1 px-3 py-4">
						<div className="flex size-8 items-center justify-center rounded-md bg-muted">
							<FolderKanban className="size-4 text-muted-foreground" />
						</div>
						<p className="text-xs text-muted-foreground mt-0.5">
							{t("projectSwitcher.noProjectsYet")}
						</p>
					</div>
				)}
				<DropdownMenuSeparator />
				{canCreate ? (
					<DropdownMenuItem render={<Link to="/home" />}>
						<Plus className="size-3.5" />
						{t("projectSwitcher.newProject")}
					</DropdownMenuItem>
				) : null}
			</DropdownMenuContent>
		</DropdownMenu>
	);
}

// ── Nav Item ───────────────────────────────────────────────────────────────────
function NavItem({
	to,
	icon: Icon,
	label,
	badge,
	exact,
}: {
	to: string;
	icon: ComponentType<{ className?: string }>;
	label: string;
	badge?: string;
	/** Match only the exact path, not any sub-route below it. Use this when
	 * a deeper route (e.g. a plugin page under this segment) has its own
	 * distinct nav item and shouldn't also light up this one. */
	exact?: boolean;
}) {
	const location = useRouterState({ select: (s) => s.location.pathname });
	const isActive = exact
		? location === to
		: location === to || location.startsWith(`${to}/`);

	return (
		<SidebarMenuItem>
			<SidebarMenuButton
				isActive={isActive}
				tooltip={label}
				render={<Link to={to} />}
				className={cn(
					"relative transition-all duration-150",
					isActive
						? "bg-primary/10 text-primary font-medium before:absolute before:left-0 before:inset-y-2 before:w-0.75 before:rounded-full before:bg-primary"
						: NAV_ITEM_INACTIVE_CLASS,
				)}
			>
				<Icon className="size-4" />
				<span>{label}</span>
				{badge ? (
					<Badge className="ml-auto text-xs" variant="secondary">
						{badge}
					</Badge>
				) : null}
			</SidebarMenuButton>
		</SidebarMenuItem>
	);
}

// ── Project Nav ───────────────────────────────────────────────────────────────
const PROJECT_NAV_ITEMS = [
	{ segment: "agents", icon: Bot, labelKey: "nav.agents" },
	{ segment: "environments", icon: Server, labelKey: "nav.environments" },
	{
		segment: "conversations",
		icon: MessageSquare,
		labelKey: "nav.conversations",
	},
	{ segment: "automation", icon: Workflow, labelKey: "nav.automation" },
	{ segment: "team", icon: Users, labelKey: "nav.team" },
	{ segment: "activity", icon: Activity, labelKey: "nav.activity" },
	{ segment: "settings", icon: Settings, labelKey: "nav.settings" },
] as const;

// A project nav entry is only listed for members who can open its page: it
// needs the read permission below (any one of them, for the entries that list
// several). Entries not listed here are open to every project member.
const PERMISSION_GATED_SEGMENTS: Partial<Record<string, readonly string[]>> = {
	agents: ["agents:read"],
	environments: ["environments:read"],
	conversations: ["conversations:read"],
	automation: ["workflows:read"],
	team: ["project.members:read"],
	activity: ["project.activities:read"],
	// Settings is a set of tabs, each with its own permission: list it when
	// the member can use at least one of them.
	settings: [
		"projects:write",
		"projects:delete",
		"roles:read",
		"roles:write",
		"project.settings.task_types:write",
		"project.settings.task_statuses:write",
		"project.settings.custom_fields:write",
		"project:export",
	],
};

function ProjectNav() {
	const { t } = useTranslation("appShell");
	return (
		<SidebarGroup>
			<SidebarGroupContent>
				<SidebarMenu>
					<SidebarMenuItem>
						<SidebarMenuButton
							tooltip={t("nav.allProjects")}
							render={<Link to="/home" />}
							className="text-muted-foreground hover:text-foreground hover:bg-sidebar-accent/60 transition-all"
						>
							<ArrowLeft className="size-4" />
							<span>{t("nav.allProjects")}</span>
						</SidebarMenuButton>
					</SidebarMenuItem>
				</SidebarMenu>
			</SidebarGroupContent>
		</SidebarGroup>
	);
}

function ProjectNavItems({ projectId }: { projectId: string }) {
	const { t } = useTranslation("appShell");
	const location = useRouterState({ select: (s) => s.location.pathname });
	const { hasProjectPermission } = useProjectPermissions(projectId);
	const { hasPermission } = usePermissions();
	const { getRegistrations } = usePluginRegistry();
	const canOpen = (action: string) =>
		hasPermission(action) || hasProjectPermission(action);
	// A plugin's own settings tab counts as something to open in Settings.
	const hasVisiblePluginSettingsTab = getRegistrations(
		"project.settings.tab",
	).some(
		(r) =>
			!r.hidden &&
			(!r.requiredPermission || hasProjectPermission(r.requiredPermission)),
	);

	const [collapsed, setCollapsed] = useState(() => {
		try {
			return (
				localStorage.getItem(`paca:sidebar-project-collapsed:${projectId}`) ===
				"true"
			);
		} catch {
			return false;
		}
	});

	const toggle = () => {
		setCollapsed((prev) => {
			const next = !prev;
			try {
				localStorage.setItem(
					`paca:sidebar-project-collapsed:${projectId}`,
					String(next),
				);
			} catch {
				/* ignore */
			}
			return next;
		});
	};

	// The icon rail has no room for section headers, so they are not togglable
	// there (and stay out of the tab order); the section is always expanded.
	const { state: sidebarState, isMobile } = useSidebar();
	const isRail = sidebarState === "collapsed" && !isMobile;
	const isCollapsed = collapsed && !isRail;

	return (
		<SidebarGroup>
			{!isRail && (
				<SidebarGroupLabel
					render={<button type="button" />}
					aria-expanded={!isCollapsed}
					className="w-full text-left flex cursor-pointer items-center justify-between hover:text-sidebar-foreground transition-colors"
					onClick={toggle}
				>
					<span>{t("nav.project")}</span>
					<ChevronRight
						className={cn(
							"size-3.5 transition-transform duration-200 text-sidebar-foreground/40",
							!isCollapsed && "rotate-90",
						)}
					/>
				</SidebarGroupLabel>
			)}

			{!isCollapsed && (
				<SidebarGroupContent>
					<SidebarMenu>
						{PROJECT_NAV_ITEMS.filter((item) => {
							const required = PERMISSION_GATED_SEGMENTS[item.segment];
							if (!required) return true;
							if (item.segment === "settings" && hasVisiblePluginSettingsTab)
								return true;
							return required.some(canOpen);
						}).map(({ segment, icon: Icon, labelKey }) => {
							const href = segment
								? `/projects/${projectId}/${segment}`
								: `/projects/${projectId}`;
							const isActive = segment
								? location.startsWith(href)
								: location === href || location === `${href}/`;
							const label = t(labelKey);
							return (
								<SidebarMenuItem key={labelKey}>
									<SidebarMenuButton
										isActive={isActive}
										tooltip={label}
										render={<Link to={href} />}
										className={cn(
											"relative transition-all duration-150",
											isActive
												? "bg-primary/10 text-primary font-medium before:absolute before:left-0 before:inset-y-2 before:w-0.75 before:rounded-full before:bg-primary"
												: NAV_ITEM_INACTIVE_CLASS,
										)}
									>
										<Icon className="size-4" />
										<span>{label}</span>
									</SidebarMenuButton>
								</SidebarMenuItem>
							);
						})}
					</SidebarMenu>
				</SidebarGroupContent>
			)}
		</SidebarGroup>
	);
}

/** A sidebar group whose label toggles it open/closed, remembering the choice
 * in localStorage under `storageKey` (same behavior as the Project section). */
function CollapsibleSidebarGroup({
	storageKey,
	label,
	children,
}: {
	storageKey: string;
	label: string;
	children: React.ReactNode;
}) {
	const [collapsed, setCollapsed] = useState(() => {
		try {
			return localStorage.getItem(storageKey) === "true";
		} catch {
			return false;
		}
	});

	const toggle = () => {
		setCollapsed((prev) => {
			const next = !prev;
			try {
				localStorage.setItem(storageKey, String(next));
			} catch {
				/* ignore */
			}
			return next;
		});
	};

	// The icon rail has no room for section headers, so they are not togglable
	// there (and stay out of the tab order); the section is always expanded.
	const { state: sidebarState, isMobile } = useSidebar();
	const isRail = sidebarState === "collapsed" && !isMobile;
	const isCollapsed = collapsed && !isRail;

	return (
		<SidebarGroup>
			{!isRail && (
				<SidebarGroupLabel
					render={<button type="button" />}
					aria-expanded={!isCollapsed}
					className="w-full text-left flex cursor-pointer items-center justify-between transition-colors hover:text-sidebar-foreground"
					onClick={toggle}
				>
					<span>{label}</span>
					<ChevronRight
						className={cn(
							"size-3.5 text-sidebar-foreground/40 transition-transform duration-200",
							!isCollapsed && "rotate-90",
						)}
					/>
				</SidebarGroupLabel>
			)}
			{!isCollapsed && <SidebarGroupContent>{children}</SidebarGroupContent>}
		</SidebarGroup>
	);
}

// ── Plugin-contributed pages ────────────────────────────────────────────────

/** Sidebar nav items for plugin `project.page` extension points (e.g. a
 * project-wide time-tracking view), routed to
 * /projects/:projectId/plugins/:pluginId/:slug. */
function PluginProjectPages({ projectId }: { projectId: string }) {
	const { t } = useTranslation("appShell");
	const { getNavItems } = usePluginRegistry();
	const location = useRouterState({ select: (s) => s.location.pathname });
	// A nav item is only listed for members who hold its requiredPermission,
	// like the built-in project nav. (Opening the URL directly still shows a
	// no-permission state, see ProjectPluginPage.)
	const { hasProjectPermission } = useProjectPermissions(projectId);
	const navItems = getNavItems("project").filter(
		(item) =>
			!item.requiredPermission || hasProjectPermission(item.requiredPermission),
	);
	if (navItems.length === 0) return null;

	return (
		<CollapsibleSidebarGroup
			storageKey={`paca:sidebar-project-plugins-collapsed:${projectId}`}
			label={t("nav.plugins")}
		>
			<SidebarMenu>
				{navItems.map((item) => {
					const Icon = resolvePluginIcon(item.icon);
					const to = `/projects/${projectId}/plugins/${item.pluginId}/${item.slug}`;
					const isActive = location === to || location.startsWith(`${to}/`);
					return (
						<SidebarMenuItem key={`${item.pluginId}:${item.slug}`}>
							<SidebarMenuButton
								isActive={isActive}
								tooltip={item.label}
								render={<Link to={to} />}
								className={cn(
									"relative transition-all duration-150",
									isActive
										? "bg-primary/10 text-primary font-medium before:absolute before:left-0 before:inset-y-2 before:w-0.75 before:rounded-full before:bg-primary"
										: NAV_ITEM_INACTIVE_CLASS,
								)}
							>
								<Icon className="size-4" />
								<span>{item.label}</span>
							</SidebarMenuButton>
						</SidebarMenuItem>
					);
				})}
			</SidebarMenu>
		</CollapsibleSidebarGroup>
	);
}

/** Admin-sidebar nav items for plugin `admin.page` extension points (e.g. a
 * cross-project time-tracking summary), routed to
 * /admin/plugins/:pluginId/:slug. Rendered inline in the existing
 * "Administration" SidebarMenu, so no extra group wrapper here. `navItems`
 * is already filtered by permission (see AppSidebar's `adminPluginNavItems`),
 * so a caller who lacks an item's `requiredPermission` never sees the link. */
function PluginAdminPages({ navItems }: { navItems: PluginNavRegistration[] }) {
	return (
		<>
			{navItems.map((item) => {
				const Icon = resolvePluginIcon(item.icon);
				const to = `/admin/plugins/${item.pluginId}/${item.slug}`;
				return (
					<NavItem
						key={`${item.pluginId}:${item.slug}`}
						to={to}
						icon={Icon}
						label={item.label}
					/>
				);
			})}
		</>
	);
}

// ── Project Interactions Section ───────────────────────────────────────────────
function ProjectInteractionsSection({ projectId }: { projectId: string }) {
	const { t } = useTranslation("appShell");
	const location = useRouterState({ select: (s) => s.location.pathname });
	const { hasPermission } = usePermissions();
	const qc = useQueryClient();
	const [collapsed, setCollapsed] = useState(() => {
		try {
			return (
				localStorage.getItem(
					`paca:sidebar-interactions-collapsed:${projectId}`,
				) === "true"
			);
		} catch {
			return false;
		}
	});

	const { hasProjectPermission } = useProjectPermissions(projectId);

	// Check sprints.read via either the global role or the project role so that
	// users with a project-scoped "Editor" / "Viewer" role (global role = User)
	// can still see Timeline, Backlog, and open sprints.
	const canViewSprints =
		hasPermission("sprints:read") || hasProjectPermission("sprints:read");
	const canEditTasks =
		hasPermission("tasks:write") || hasProjectPermission("tasks:write");

	const [dragOverInteractionId, setDragOverInteractionId] = useState<
		string | null
	>(null);
	// Collapsed by default; persisted per-project once the user makes a choice.
	const [completedSprintsCollapsed, setCompletedSprintsCollapsed] = useState(
		() => {
			try {
				const stored = localStorage.getItem(
					`paca:sidebar-completed-sprints-collapsed:${projectId}`,
				);
				return stored === null ? true : stored === "true";
			} catch {
				return true;
			}
		},
	);
	// Expanded by default (planned sprints are near-term actionable items,
	// unlike completed ones); persisted per-project once the user collapses it.
	const [plannedSprintsCollapsed, setPlannedSprintsCollapsed] = useState(() => {
		try {
			const stored = localStorage.getItem(
				`paca:sidebar-planned-sprints-collapsed:${projectId}`,
			);
			return stored === null ? false : stored === "true";
		} catch {
			return false;
		}
	});

	const toggleCompletedSprints = () => {
		setCompletedSprintsCollapsed((prev) => {
			const next = !prev;
			try {
				localStorage.setItem(
					`paca:sidebar-completed-sprints-collapsed:${projectId}`,
					String(next),
				);
			} catch {
				/* ignore */
			}
			return next;
		});
	};

	const togglePlannedSprints = () => {
		setPlannedSprintsCollapsed((prev) => {
			const next = !prev;
			try {
				localStorage.setItem(
					`paca:sidebar-planned-sprints-collapsed:${projectId}`,
					String(next),
				);
			} catch {
				/* ignore */
			}
			return next;
		});
	};

	// Clear the drop-target highlight whenever any drag ends (covers drag-cancel
	// and mouse-release outside a valid target, where dragleave may not fire).
	useEffect(() => {
		const handleDragEnd = () => setDragOverInteractionId(null);
		document.addEventListener("dragend", handleDragEnd);
		return () => document.removeEventListener("dragend", handleDragEnd);
	}, []);

	const updateSprintMutation = useMutation({
		mutationFn: ({
			taskId,
			sprintId,
		}: {
			taskId: string;
			sprintId: string | null;
		}) => updateTask(projectId, taskId, { sprint_id: sprintId }),
		onSuccess: () => {
			qc.invalidateQueries({
				queryKey: ["projects", projectId, "tasks"],
			});
			qc.invalidateQueries({ queryKey: ["projects", projectId, "sprints"] });
		},
	});

	const handleInteractionDragOver = (
		e: React.DragEvent,
		interactionId: string,
	) => {
		if (!canEditTasks) return;
		if (!e.dataTransfer.types.includes("application/x-paca-task-id")) return;
		e.preventDefault();
		e.dataTransfer.dropEffect = "move";
		setDragOverInteractionId(interactionId);
	};

	const handleInteractionDragLeave = (e: React.DragEvent) => {
		// Clear whenever leaving the item. If the cursor moves to a child element
		// within the same item, dragover immediately re-fires on the parent and
		// restores the highlight, so the brief gap is imperceptible.
		if (
			!(e.currentTarget as HTMLElement).contains(e.relatedTarget as Node | null)
		) {
			setDragOverInteractionId(null);
		}
	};

	const handleInteractionDrop = (
		e: React.DragEvent,
		sprintId: string | null,
	) => {
		e.preventDefault();
		setDragOverInteractionId(null);
		if (!canEditTasks) return;
		const taskId = e.dataTransfer.getData("text/plain");
		if (!taskId) return;
		updateSprintMutation.mutate({ taskId, sprintId });
	};

	// No refetchInterval: kept live via useProjectRealtime's socket-driven
	// sprint.* invalidation (see hooks/use-project-realtime.ts) instead of
	// polling.
	// One paged query per status group, so each list loads more on scroll or
	// click and its count badge comes from the server's total_count.
	const openQuery = useInfiniteQuery({
		...sprintsInfiniteQueryOptions(projectId, { status: ["active"] }),
		enabled: canViewSprints,
		retry: false,
	});
	const plannedQuery = useInfiniteQuery({
		...sprintsInfiniteQueryOptions(projectId, { status: ["planned"] }),
		enabled: canViewSprints,
		retry: false,
	});
	const completedQuery = useInfiniteQuery({
		...sprintsInfiniteQueryOptions(projectId, {
			status: ["completed"],
			sort: "recent",
		}),
		enabled: canViewSprints,
		retry: false,
	});

	// The icon rail has no room for section headers, so they are not togglable
	// there (and stay out of the tab order); the section is always expanded.
	const { state: sidebarState, isMobile } = useSidebar();
	const isRail = sidebarState === "collapsed" && !isMobile;
	const isCollapsed = collapsed && !isRail;

	// Hide entire section if user lacks the "View Sprints" permission
	if (!canViewSprints) return null;

	const openSprints = openQuery.data?.pages.flatMap((p) => p.items) ?? [];
	const plannedSprints = plannedQuery.data?.pages.flatMap((p) => p.items) ?? [];
	const completedSprints =
		completedQuery.data?.pages.flatMap((p) => p.items) ?? [];
	const plannedCount =
		plannedQuery.data?.pages[0]?.total_count ?? plannedSprints.length;
	const completedCount =
		completedQuery.data?.pages[0]?.total_count ?? completedSprints.length;

	const backlogHref = `/projects/${projectId}/interactions/backlog`;
	const isBacklogActive = location.startsWith(backlogHref);

	const timelineHref = `/projects/${projectId}/interactions/timeline`;
	const isTimelineActive = location.startsWith(timelineHref);

	const toggle = () => {
		setCollapsed((prev) => {
			const next = !prev;
			try {
				localStorage.setItem(
					`paca:sidebar-interactions-collapsed:${projectId}`,
					String(next),
				);
			} catch {
				/* ignore */
			}
			return next;
		});
	};

	return (
		<SidebarGroup>
			{!isRail && (
				<SidebarGroupLabel
					render={<button type="button" />}
					aria-expanded={!isCollapsed}
					className="w-full text-left flex cursor-pointer items-center justify-between hover:text-sidebar-foreground transition-colors"
					onClick={toggle}
				>
					<span>{t("interactions.title")}</span>
					<ChevronRight
						className={cn(
							"size-3.5 transition-transform duration-200 text-sidebar-foreground/40",
							!isCollapsed && "rotate-90",
						)}
					/>
				</SidebarGroupLabel>
			)}

			{!isCollapsed && (
				<SidebarGroupContent>
					<SidebarMenu>
						{/* Timeline */}
						<SidebarMenuItem>
							<SidebarMenuButton
								isActive={isTimelineActive}
								tooltip={t("interactions.timeline")}
								render={<Link to={timelineHref} />}
								className={cn(
									"relative transition-all duration-150",
									isTimelineActive
										? "bg-primary/10 text-primary font-medium before:absolute before:left-0 before:inset-y-2 before:w-0.75 before:rounded-full before:bg-primary"
										: NAV_ITEM_INACTIVE_CLASS,
								)}
							>
								<GanttChart className="size-4" />
								<span>{t("interactions.timeline")}</span>
							</SidebarMenuButton>
						</SidebarMenuItem>
						{/* Product Backlog — always shown */}
						<SidebarMenuItem
							onDragOver={(e) => handleInteractionDragOver(e, "backlog")}
							onDragLeave={handleInteractionDragLeave}
							onDrop={(e) => handleInteractionDrop(e, null)}
						>
							<SidebarMenuButton
								isActive={isBacklogActive}
								tooltip={t("interactions.productBacklog")}
								render={<Link to={backlogHref} />}
								className={cn(
									"relative transition-all duration-150",
									isBacklogActive
										? "bg-primary/10 text-primary font-medium before:absolute before:left-0 before:inset-y-2 before:w-0.75 before:rounded-full before:bg-primary"
										: NAV_ITEM_INACTIVE_CLASS,
									dragOverInteractionId === "backlog" &&
										"ring-2 ring-primary/40 bg-primary/5 text-primary",
								)}
							>
								<BookOpen className="size-4" />
								<span>{t("interactions.productBacklog")}</span>
							</SidebarMenuButton>
						</SidebarMenuItem>
						{/* Open sprints */}
						{openSprints.map((sprint) => {
							const sprintHref = `/projects/${projectId}/interactions/sprints/${sprint.id}`;
							const isActive = location.startsWith(sprintHref);
							return (
								<SidebarMenuItem
									key={sprint.id}
									onDragOver={(e) => handleInteractionDragOver(e, sprint.id)}
									onDragLeave={handleInteractionDragLeave}
									onDrop={(e) => handleInteractionDrop(e, sprint.id)}
								>
									<SidebarMenuButton
										isActive={isActive}
										tooltip={sprint.name}
										render={<Link to={sprintHref} />}
										className={cn(
											"relative transition-all duration-150",
											isActive
												? "bg-primary/10 text-primary font-medium before:absolute before:left-0 before:inset-y-2 before:w-0.75 before:rounded-full before:bg-primary"
												: NAV_ITEM_INACTIVE_CLASS,
											dragOverInteractionId === sprint.id &&
												"ring-2 ring-primary/40 bg-primary/5 text-primary",
										)}
									>
										<KanbanSquare className="size-4" />
										<span className="flex-1 truncate">{sprint.name}</span>
									</SidebarMenuButton>
								</SidebarMenuItem>
							);
						})}
						<SidebarMenuItem>
							<LoadMoreSentinel
								compact
								hasMore={!!openQuery.hasNextPage}
								isLoadingMore={openQuery.isFetchingNextPage}
								isError={openQuery.isFetchNextPageError}
								onLoadMore={() => void openQuery.fetchNextPage()}
							/>
						</SidebarMenuItem>
						{/* Planned sprints (collapsible) */}
						{plannedSprints.length > 0 && (
							<>
								<SidebarMenuItem>
									<SidebarMenuButton
										tooltip={t("interactions.plannedSprints")}
										onClick={togglePlannedSprints}
										className={NAV_ITEM_INACTIVE_CLASS}
									>
										<ChevronRight
											className={cn(
												"size-3.5 shrink-0 transition-transform duration-200 text-sidebar-foreground/40",
												!plannedSprintsCollapsed && "rotate-90",
											)}
										/>
										<span className="flex-1 truncate text-xs font-medium">
											{t("interactions.plannedSprints")}
										</span>
										<span className="rounded-full bg-sidebar-accent/60 px-1.5 py-0.5 text-[10px] font-semibold tabular-nums">
											{plannedCount}
										</span>
									</SidebarMenuButton>
								</SidebarMenuItem>
								{!plannedSprintsCollapsed &&
									plannedSprints.map((sprint) => {
										const sprintHref = `/projects/${projectId}/interactions/sprints/${sprint.id}`;
										const isActive = location.startsWith(sprintHref);
										return (
											<SidebarMenuItem
												key={sprint.id}
												onDragOver={(e) =>
													handleInteractionDragOver(e, sprint.id)
												}
												onDragLeave={handleInteractionDragLeave}
												onDrop={(e) => handleInteractionDrop(e, sprint.id)}
											>
												<SidebarMenuButton
													isActive={isActive}
													tooltip={sprint.name}
													render={<Link to={sprintHref} />}
													className={cn(
														"relative transition-all duration-150",
														isActive
															? "bg-primary/10 text-primary font-medium before:absolute before:left-0 before:inset-y-2 before:w-0.75 before:rounded-full before:bg-primary"
															: NAV_ITEM_INACTIVE_CLASS,
														dragOverInteractionId === sprint.id &&
															"ring-2 ring-primary/40 bg-primary/5 text-primary",
													)}
												>
													<Clock className="size-4" />
													<span className="flex-1 truncate">{sprint.name}</span>
												</SidebarMenuButton>
											</SidebarMenuItem>
										);
									})}
								{!plannedSprintsCollapsed && (
									<SidebarMenuItem>
										<LoadMoreSentinel
											compact
											hasMore={!!plannedQuery.hasNextPage}
											isLoadingMore={plannedQuery.isFetchingNextPage}
											isError={plannedQuery.isFetchNextPageError}
											onLoadMore={() => void plannedQuery.fetchNextPage()}
										/>
									</SidebarMenuItem>
								)}
							</>
						)}
						{/* Closed sprints (collapsible) */}
						{completedSprints.length > 0 && (
							<>
								<SidebarMenuItem>
									<SidebarMenuButton
										tooltip={t("interactions.completedSprints")}
										onClick={toggleCompletedSprints}
										className={NAV_ITEM_INACTIVE_CLASS}
									>
										<ChevronRight
											className={cn(
												"size-3.5 shrink-0 transition-transform duration-200 text-sidebar-foreground/40",
												!completedSprintsCollapsed && "rotate-90",
											)}
										/>
										<span className="flex-1 truncate text-xs font-medium">
											{t("interactions.completedSprints")}
										</span>
										<span className="rounded-full bg-sidebar-accent/60 px-1.5 py-0.5 text-[10px] font-semibold tabular-nums">
											{completedCount}
										</span>
									</SidebarMenuButton>
								</SidebarMenuItem>
								{!completedSprintsCollapsed &&
									completedSprints.map((sprint) => {
										const sprintHref = `/projects/${projectId}/interactions/sprints/${sprint.id}`;
										const isActive = location.startsWith(sprintHref);
										return (
											<SidebarMenuItem key={sprint.id}>
												<SidebarMenuButton
													isActive={isActive}
													tooltip={sprint.name}
													render={<Link to={sprintHref} />}
													className={cn(
														"relative transition-all duration-150",
														isActive
															? "bg-primary/10 text-primary font-medium before:absolute before:left-0 before:inset-y-2 before:w-0.75 before:rounded-full before:bg-primary"
															: NAV_ITEM_INACTIVE_CLASS,
													)}
												>
													<CheckCircle2 className="size-4" />
													<span className="flex-1 truncate">{sprint.name}</span>
												</SidebarMenuButton>
											</SidebarMenuItem>
										);
									})}
								{!completedSprintsCollapsed && (
									<SidebarMenuItem>
										<LoadMoreSentinel
											compact
											hasMore={!!completedQuery.hasNextPage}
											isLoadingMore={completedQuery.isFetchingNextPage}
											isError={completedQuery.isFetchNextPageError}
											onLoadMore={() => void completedQuery.fetchNextPage()}
										/>
									</SidebarMenuItem>
								)}
							</>
						)}
					</SidebarMenu>
				</SidebarGroupContent>
			)}
		</SidebarGroup>
	);
}

// ── Theme Switcher ─────────────────────────────────────────────────────────────
const THEME_MODES = [
	{ mode: "light" as ThemeMode, Icon: Sun, labelKey: "theme.light" },
	{ mode: "dark" as ThemeMode, Icon: Moon, labelKey: "theme.dark" },
	{ mode: "auto" as ThemeMode, Icon: Monitor, labelKey: "theme.auto" },
] as const;

function ThemeSwitcher() {
	const { t } = useTranslation("appShell");
	const { mode, set } = useThemeMode();
	const cycle = () =>
		set(mode === "light" ? "dark" : mode === "dark" ? "auto" : "light");
	const CurrentIcon = mode === "light" ? Sun : mode === "dark" ? Moon : Monitor;

	return (
		<>
			{/* Collapsed: single cycling icon button with tooltip */}
			<SidebarMenu className="hidden group-data-[collapsible=icon]:flex">
				<SidebarMenuItem>
					<SidebarMenuButton
						tooltip={t("theme.cycleTooltip", { mode })}
						onClick={cycle}
					>
						<CurrentIcon className="size-4" />
					</SidebarMenuButton>
				</SidebarMenuItem>
			</SidebarMenu>

			{/* Expanded: segmented 3-way control */}
			<div className="flex items-center justify-between px-2 py-1.5 group-data-[collapsible=icon]:hidden">
				<span className="text-xs font-medium text-sidebar-foreground/50 tracking-wide">
					{t("theme.label")}
				</span>
				<div className="flex items-center gap-0.5 rounded-md border border-sidebar-border bg-sidebar p-0.5">
					{THEME_MODES.map(({ mode: m, Icon, labelKey }) => (
						<button
							key={m}
							type="button"
							onClick={() => set(m)}
							title={t(labelKey)}
							className={cn(
								"flex size-6 items-center justify-center rounded transition-all duration-150",
								mode === m
									? "bg-sidebar-accent text-sidebar-accent-foreground shadow-sm"
									: "text-sidebar-foreground/40 hover:text-sidebar-foreground/70",
							)}
						>
							<Icon className="size-3.5" />
						</button>
					))}
				</div>
			</div>
		</>
	);
}

// ── App Sidebar ────────────────────────────────────────────────────────────────
export function AppSidebar() {
	const { t } = useTranslation("appShell");
	const { t: tShared } = useTranslation("shared");
	const { state: sidebarState, isMobile } = useSidebar();
	const sidebarPane = usePaneWidth(SIDEBAR_PANE);
	const { hasPermission } = usePermissions();
	const { resolvedMode } = useThemeMode();
	const { projectId } = useParams({ strict: false });
	const { data: user } = useQuery(currentUserQueryOptions);
	const { getNavItems } = usePluginRegistry();
	const branding = useBranding();
	const logoUrl = branding?.logo_thumb_url ?? branding?.logo_url;
	const brandName = branding?.brand_name;

	const canAccessGlobalRoles =
		hasPermission("roles:read") || hasPermission("roles:write");

	const canAccessUsers =
		hasPermission("users:read") || hasPermission("users:write");

	const canAccessGlobalAgents =
		hasPermission("agents:read") || hasPermission("agents:write");

	const canAccessServiceAccounts =
		hasPermission("service_accounts:read") ||
		hasPermission("service_accounts:write");

	// plugins.write replaced users.write as a rough "is this someone
	// important" proxy once it got its own dedicated permission — see authz.
	// PermissionPluginsRead's doc comment on the Go side. This nav-link
	// check was never updated when that happened.
	const canAccessPlugins = hasPermission("plugins:write");

	const canAccessSettings =
		hasPermission("settings:write") ||
		hasPermission("settings.sso:write") ||
		hasPermission("settings.security:write");

	const canCreateProject = hasPermission("projects:create");

	// A plugin admin nav item is only listed for callers who hold its
	// declared `requiredPermission` (the same fallback as AdminPluginPage:
	// plugins:write when it declares none).
	const adminPluginNavItems = getNavItems("admin").filter((item) =>
		hasPermission(item.requiredPermission ?? "plugins:write"),
	);

	// Only shown to someone who can see Administration for another reason: a
	// plugin with an admin page must not reveal the heading by itself.
	const showAdminSection =
		canAccessGlobalRoles ||
		canAccessUsers ||
		canAccessGlobalAgents ||
		canAccessServiceAccounts ||
		canAccessPlugins ||
		canAccessSettings;
	// Plugin-contributed admin pages get their own sidebar section, separate
	// from core workspace administration.
	const showPluginsSection = showAdminSection && adminPluginNavItems.length > 0;
	const isProjectContext = !!projectId;
	// The docs tree is only listed for members who can read docs.
	const { hasProjectPermission } = useProjectPermissions(projectId ?? "");
	const canReadDocs =
		hasPermission("docs:read") || hasProjectPermission("docs:read");

	return (
		<Sidebar collapsible="icon">
			{/* Brand */}
			<SidebarHeader className="gap-2 pb-2">
				<div className="flex items-center gap-2.5 px-2 pt-1">
					{user ? (
						<Link to="/home">
							<img
								src={
									logoUrl ??
									(resolvedMode === "dark"
										? "/paca-logo-dark.svg"
										: "/paca-logo.svg")
								}
								alt={t("brand.logoAlt")}
								className="size-8 shrink-0"
							/>
						</Link>
					) : (
						<img
							src={
								logoUrl ??
								(resolvedMode === "dark"
									? "/paca-logo-dark.svg"
									: "/paca-logo.svg")
							}
							alt={t("brand.logoAlt")}
							className="size-8 shrink-0"
						/>
					)}
					<span className="font-[Syne] font-bold text-base tracking-tight text-sidebar-foreground group-data-[collapsible=icon]:hidden">
						{brandName ?? "paca"}
					</span>
				</div>
				<div className="group-data-[collapsible=icon]:hidden">
					<ProjectSwitcher
						currentProjectId={projectId}
						canCreate={canCreateProject}
					/>
				</div>
			</SidebarHeader>

			<SidebarSeparator />

			{/* Navigation — switches between workspace and project context */}
			{/* Optional sections render nothing when empty, which can leave two
				   dividers back to back; show only the first. */}
			<SidebarContent className="[&_[data-slot=sidebar-separator]+[data-slot=sidebar-separator]]:hidden">
				{isProjectContext ? (
					<>
						{user && <ProjectNav />}
						{user && <SidebarSeparator />}
						<ProjectInteractionsSection projectId={projectId} />
						<SidebarSeparator />
						{canReadDocs && <DocsSidebarSection projectId={projectId} />}
						<SidebarSeparator />
						<ExtensionPoint
							point="sidebar.project.section"
							componentProps={{ projectId }}
						/>
						<SidebarSeparator />
						<PluginProjectPages projectId={projectId} />
						<SidebarSeparator />
						<ProjectNavItems projectId={projectId} />
					</>
				) : (
					<>
						{user && (
							<SidebarGroup>
								<SidebarGroupContent>
									<SidebarMenu>
										<NavItem to="/home" icon={Home} label={t("nav.home")} />
										<NavItem
											to="/conversations"
											icon={MessageSquare}
											label={t("nav.conversations")}
										/>
									</SidebarMenu>
								</SidebarGroupContent>
							</SidebarGroup>
						)}

						<ExtensionPoint point="sidebar.general.section" />
						{/* Admin section */}
						{showAdminSection ? (
							<>
								<SidebarSeparator />
								<SidebarGroup>
									<SidebarGroupLabel>
										{t("nav.administration")}
									</SidebarGroupLabel>
									<SidebarGroupContent>
										<SidebarMenu>
											{canAccessGlobalRoles ? (
												<NavItem
													to="/admin/global-roles"
													icon={Shield}
													label={t("nav.globalRoles")}
												/>
											) : null}
											{canAccessUsers ? (
												<NavItem
													to="/admin/users"
													icon={Users}
													label={t("nav.users")}
												/>
											) : null}
											{canAccessGlobalAgents ? (
												<NavItem
													to="/admin/agents"
													icon={Bot}
													label={t("nav.agents")}
												/>
											) : null}
											{canAccessServiceAccounts ? (
												<NavItem
													to="/admin/service-accounts"
													icon={ServerCog}
													label={t("nav.serviceAccounts")}
												/>
											) : null}
											{canAccessPlugins ? (
												<NavItem
													to="/admin/plugins"
													icon={Puzzle}
													label={t("nav.plugins")}
													exact
												/>
											) : null}
											{canAccessSettings ? (
												<NavItem
													to="/admin/settings"
													icon={Settings}
													label={t("nav.settings")}
												/>
											) : null}
											<NavItem
												to="/admin/changelog"
												icon={Sparkles}
												label={t("nav.changelog")}
											/>
										</SidebarMenu>
									</SidebarGroupContent>
								</SidebarGroup>
							</>
						) : null}
						{/* Plugin-contributed admin pages get their own sidebar section,
						   separate from core workspace administration. */}
						{showPluginsSection ? (
							<>
								<SidebarSeparator />
								<CollapsibleSidebarGroup
									storageKey="paca:sidebar-admin-plugins-collapsed"
									label={t("nav.plugins")}
								>
									<SidebarMenu>
										<PluginAdminPages navItems={adminPluginNavItems} />
									</SidebarMenu>
								</CollapsibleSidebarGroup>
							</>
						) : null}
					</>
				)}
			</SidebarContent>

			{/* Footer: theme toggle + user menu (language selector lives in the user menu) */}
			<SidebarSeparator />
			<SidebarFooter className="gap-1 pb-3">
				<ThemeSwitcher />
				<UserMenu />
			</SidebarFooter>

			{/* One gesture per edge: while open the edge only resizes (collapse
			   with the header toggle); the click-to-toggle rail exists only
			   while collapsed, to bring the sidebar back. */}
			{sidebarState === "collapsed" && <SidebarRail />}
			{!isMobile && sidebarState === "expanded" && (
				<PaneResizeHandle
					config={SIDEBAR_PANE}
					pane={sidebarPane}
					label={tShared("paneResize.sidebar")}
					edge="right"
					testId="sidebar-resize-handle"
				/>
			)}
		</Sidebar>
	);
}
