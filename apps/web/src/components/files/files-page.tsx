import {
	useInfiniteQuery,
	useMutation,
	useQueries,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
	ChevronRight,
	FilePlus2,
	FolderPlus,
	FolderUp,
	Grid2x2,
	List,
	Lock,
	Plus,
	Search,
	Trash2,
	X,
} from "lucide-react";
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
import { LoadMoreSentinel } from "@/components/shared/load-more-sentinel";
import { NoPermissionState } from "@/components/shared/no-permission-state";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
	Empty,
	EmptyDescription,
	EmptyHeader,
	EmptyMedia,
	EmptyTitle,
} from "@/components/ui/empty";
import { Input } from "@/components/ui/input";
import {
	NativeSelect,
	NativeSelectOption,
} from "@/components/ui/native-select";
import { Skeleton } from "@/components/ui/skeleton";
import { toast } from "@/components/ui/toast";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { useDebouncedCallback } from "@/hooks/use-debounced-callback";
import { useFileWriteAccess } from "@/hooks/use-file-write-access";
import {
	ApiErrorCode,
	getApiErrorCode,
	getApiErrorMessage,
	isForbiddenError,
} from "@/lib/api-error";
import {
	collectDropped,
	collectFromInput,
	enqueueUploads,
	type UploadEntry,
} from "@/lib/file-uploads";
import {
	createFolder,
	deleteNodePermanently,
	FILE_TYPE_FILTERS,
	type FileNode,
	type FileRoot,
	type FileScope,
	type FileSection,
	type FileSortKey,
	type FileTypeFilter,
	fileNodePathQueryOptions,
	fileNodesQueryOptions,
	fileRootsQueryOptions,
	fileSectionQueryOptions,
	GENERAL_SCOPE,
	getNodeDownload,
	invalidateFiles,
	moveNode,
	renameNode,
	restoreNode,
	scopeOfNode,
	setNodeStarred,
	trashNode,
} from "@/lib/files-api";
import { showErrorToast } from "@/lib/notify";
import { projectQueryOptions } from "@/lib/project-api";
import { cn } from "@/lib/utils";
import { buildFileActions, type FileActionHandlers } from "./file-actions";
import {
	ConfirmDialog,
	DetailsSheet,
	MoveDialog,
	NameDialog,
} from "./file-dialogs";
import { FileTypeIcon } from "./file-icon";
import {
	FILE_DRAG_MIME,
	FileGridView,
	FileListView,
	type FileViewProps,
} from "./file-views";

const FilePreview = lazy(() => import("./file-preview"));

export type FilesPageSection = "browse" | FileSection;

/** The URL state of the page (all optional, all strings so links stay plain). */
export interface FilesSearch {
	/** Folder being browsed; the scope root when absent. */
	folder?: string;
	/** Global Files only: "general" or a project id; the roots view when absent. */
	scope?: string;
	q?: string;
	type?: string;
}

export function parseFilesSearch(raw: Record<string, unknown>): FilesSearch {
	const str = (v: unknown) => (typeof v === "string" && v ? v : undefined);
	return {
		folder: str(raw.folder),
		scope: str(raw.scope),
		q: str(raw.q),
		type: str(raw.type),
	};
}

const SKELETON_ROWS = ["a", "b", "c", "d", "e", "f"] as const;

const VIEW_KEY = "paca:files-view";

function readView(): "grid" | "list" {
	try {
		return localStorage.getItem(VIEW_KEY) === "grid" ? "grid" : "list";
	} catch {
		return "list";
	}
}

function isTypeFilter(v: string | undefined): v is FileTypeFilter {
	return !!v && (FILE_TYPE_FILTERS as readonly string[]).includes(v);
}

export function FilesPage({
	projectId,
	section,
	search,
}: {
	/** Set on the project routes; absent on the global Files pages. */
	projectId?: string;
	section: FilesPageSection;
	search: FilesSearch;
}) {
	const { t } = useTranslation("files");
	const qc = useQueryClient();
	const navigate = useNavigate();
	const inTrash = section === "trash";

	// ── Scope ────────────────────────────────────────────────────────────────
	const roots = useQuery({
		...fileRootsQueryOptions,
		enabled: !projectId,
	});
	const project = useQuery({
		...projectQueryOptions(projectId ?? ""),
		enabled: !!projectId,
	});

	/** The scope being browsed; null shows the roots (global Files only). */
	const browseScope: FileScope | null = projectId
		? { projectId }
		: search.scope === "general"
			? GENERAL_SCOPE
			: search.scope
				? { projectId: search.scope }
				: null;

	const rootScopes: FileScope[] = useMemo(
		() =>
			(roots.data ?? []).map((r) => ({
				projectId: r.kind === "project" ? (r.project_id ?? null) : null,
			})),
		[roots.data],
	);
	/** Every scope the current section reads from. */
	const sectionScopes: FileScope[] = projectId
		? [{ projectId }]
		: section === "browse"
			? browseScope
				? [browseScope]
				: []
			: rootScopes;

	const scopeName = (s: FileScope): string =>
		s.projectId
			? s.projectId === projectId
				? (project.data?.name ?? "")
				: ((roots.data ?? []).find((r) => r.project_id === s.projectId)?.name ??
					"")
			: t("roots.general");

	const canWriteScope = useFileWriteAccess(sectionScopes);

	// ── Data ─────────────────────────────────────────────────────────────────
	const [sort, setSort] = useState<{ key: FileSortKey; desc: boolean }>({
		key: "name",
		desc: false,
	});
	const typeFilter = isTypeFilter(search.type) ? search.type : undefined;
	const query = search.q?.trim() || undefined;
	const folderId = search.folder ?? null;

	const browse = useInfiniteQuery({
		...fileNodesQueryOptions(browseScope ?? GENERAL_SCOPE, {
			parentId: folderId,
			q: query,
			type: typeFilter,
			sort: sort.key,
			order: sort.desc ? "desc" : "asc",
		}),
		enabled: section === "browse" && !!browseScope,
	});

	const sectionQueries = useQueries({
		queries: sectionScopes.map((s) => ({
			...fileSectionQueryOptions(s, section === "browse" ? "recent" : section),
			enabled: section !== "browse",
		})),
	});

	const nodes: FileNode[] = useMemo(() => {
		if (section === "browse") {
			return browse.data?.pages.flatMap((p) => p.items) ?? [];
		}
		const all = sectionQueries.flatMap((q) => q.data ?? []);
		const byTime = (n: FileNode, key: "deleted_at" | "accessed_at") =>
			new Date(n[key] ?? 0).getTime();
		if (section === "trash") {
			return [...all].sort(
				(a, b) => byTime(b, "deleted_at") - byTime(a, "deleted_at"),
			);
		}
		if (section === "recent") {
			return [...all].sort(
				(a, b) => byTime(b, "accessed_at") - byTime(a, "accessed_at"),
			);
		}
		return [...all].sort((a, b) => a.name.localeCompare(b.name));
	}, [section, browse.data, sectionQueries]);

	const isLoading =
		section === "browse"
			? !!browseScope && browse.isLoading
			: !projectId && roots.isLoading
				? true
				: sectionQueries.some((q) => q.isLoading);
	const forbidden =
		(section === "browse" &&
			browse.isError &&
			isForbiddenError(browse.error)) ||
		(section !== "browse" &&
			sectionQueries.length > 0 &&
			sectionQueries.every((q) => q.isError && isForbiddenError(q.error)));

	const pathQuery = useQuery({
		...fileNodePathQueryOptions(browseScope ?? GENERAL_SCOPE, folderId ?? ""),
		enabled: section === "browse" && !!browseScope && !!folderId,
	});
	const path = pathQuery.data ?? [];
	const currentFolder = folderId ? path.at(-1) : undefined;

	// ── Navigation ───────────────────────────────────────────────────────────
	const setSearch = useCallback(
		(patch: Partial<FilesSearch>) =>
			navigate({
				search: ((prev: FilesSearch) => ({ ...prev, ...patch })) as never,
			}),
		[navigate],
	);

	/** Opens a folder in the browser, from any section. */
	const goToFolder = useCallback(
		(scope: FileScope | null, id: string | null) => {
			if (projectId || scope?.projectId) {
				const pid = projectId ?? (scope?.projectId as string);
				if (projectId && section === "browse") {
					void setSearch({
						folder: id ?? undefined,
						q: undefined,
						type: undefined,
					});
				} else if (projectId) {
					void navigate({
						to: "/projects/$projectId/files",
						params: { projectId: pid },
						search: { folder: id ?? undefined },
					});
				} else {
					void navigate({
						to: "/files",
						search: { scope: pid, folder: id ?? undefined },
					});
				}
				return;
			}
			void navigate({
				to: "/files",
				search: scope ? { scope: "general", folder: id ?? undefined } : {},
			});
		},
		[navigate, projectId, section, setSearch],
	);

	// ── Selection ────────────────────────────────────────────────────────────
	const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());
	const anchor = useRef<string | null>(null);
	// biome-ignore lint/correctness/useExhaustiveDependencies: reset on navigation
	useEffect(
		() => setSelected(new Set()),
		[folderId, section, browseScope?.projectId, query, typeFilter],
	);

	const onSelect = (n: FileNode, mods: { toggle: boolean; range: boolean }) => {
		setSelected((prev) => {
			if (mods.range && anchor.current) {
				const ids = nodes.map((x) => x.id);
				const a = ids.indexOf(anchor.current);
				const b = ids.indexOf(n.id);
				if (a >= 0 && b >= 0) {
					const [lo, hi] = a < b ? [a, b] : [b, a];
					return new Set(ids.slice(lo, hi + 1));
				}
			}
			anchor.current = n.id;
			if (mods.toggle) {
				const next = new Set(prev);
				if (next.has(n.id)) next.delete(n.id);
				else next.add(n.id);
				return next;
			}
			return new Set([n.id]);
		});
	};
	const targetsFor = (n: FileNode) =>
		selected.has(n.id) ? nodes.filter((x) => selected.has(x.id)) : [n];

	// ── Mutations ────────────────────────────────────────────────────────────
	const refresh = (scopes: FileScope[]) => {
		const seen = new Set<string>();
		for (const s of scopes) {
			const k = s.projectId ?? "general";
			if (!seen.has(k)) {
				seen.add(k);
				void invalidateFiles(qc, s);
			}
		}
	};
	const fail = (error: unknown) => {
		const code = getApiErrorCode(error);
		const known =
			code && (Object.values(ApiErrorCode) as string[]).includes(code);
		showErrorToast({
			title: t("toast.failed"),
			description:
				known && code.startsWith("FILE_")
					? t(`errors.${code as "FILE_NODE_LOCKED"}`)
					: (getApiErrorMessage(error) ?? ""),
			error,
		});
	};
	const ok = (title: string, action?: { label: string; run: () => void }) =>
		toast.add({
			type: "success",
			title,
			actionProps: action
				? { children: action.label, onClick: action.run }
				: undefined,
		});

	const [dialog, setDialog] = useState<
		| { kind: "newFolder" }
		| { kind: "rename"; node: FileNode }
		| { kind: "move"; nodes: FileNode[] }
		| { kind: "trash"; nodes: FileNode[] }
		| { kind: "deleteForever"; nodes: FileNode[] }
		| null
	>(null);
	const [detailsNode, setDetailsNode] = useState<FileNode | null>(null);

	const createFolderM = useMutation({
		mutationFn: (name: string) =>
			createFolder(browseScope as FileScope, { parent_id: folderId, name }),
		onSuccess: () => {
			ok(t("toast.folderCreated"));
			setDialog(null);
		},
		onError: fail,
		onSettled: () => refresh(sectionScopes),
	});
	const renameM = useMutation({
		mutationFn: ({ node, name }: { node: FileNode; name: string }) =>
			renameNode(scopeOfNode(node), node.id, name),
		onSuccess: () => {
			ok(t("toast.renamed"));
			setDialog(null);
		},
		onError: fail,
		onSettled: () => refresh(sectionScopes),
	});
	const moveM = useMutation({
		mutationFn: async ({
			items,
			parentId,
		}: {
			items: FileNode[];
			parentId: string | null;
		}) => {
			for (const n of items) await moveNode(scopeOfNode(n), n.id, parentId);
			return items.length;
		},
		onSuccess: (count) => {
			ok(count === 1 ? t("toast.moved") : t("toast.movedMany", { count }));
			setDialog(null);
			setSelected(new Set());
		},
		onError: fail,
		onSettled: () => refresh(sectionScopes),
	});
	const trashM = useMutation({
		mutationFn: async (items: FileNode[]) => {
			for (const n of items) await trashNode(scopeOfNode(n), n.id);
			return items;
		},
		onSuccess: (items) => {
			setDialog(null);
			setSelected(new Set());
			// Task attachments are deleted for good, so there is nothing to undo.
			const undoable = items.filter((n) => n.source !== "task_attachment");
			ok(
				items.length === 1
					? t("toast.trashed")
					: t("toast.trashedMany", { count: items.length }),
				undoable.length > 0
					? {
							label: t("actions.restore"),
							run: () =>
								void Promise.all(
									undoable.map((n) => restoreNode(scopeOfNode(n), n.id)),
								).then(() => refresh(items.map(scopeOfNode))),
						}
					: undefined,
			);
		},
		onError: fail,
		onSettled: (_d, _e, items) => refresh((items ?? []).map(scopeOfNode)),
	});
	const restoreM = useMutation({
		mutationFn: async (items: FileNode[]) => {
			for (const n of items) await restoreNode(scopeOfNode(n), n.id);
		},
		onSuccess: () => {
			ok(t("toast.restored"));
			setSelected(new Set());
		},
		onError: fail,
		onSettled: (_d, _e, items) => refresh((items ?? []).map(scopeOfNode)),
	});
	const deleteForeverM = useMutation({
		mutationFn: async (items: FileNode[]) => {
			for (const n of items) await deleteNodePermanently(scopeOfNode(n), n.id);
		},
		onSuccess: () => {
			ok(t("toast.deleted"));
			setDialog(null);
			setSelected(new Set());
		},
		onError: fail,
		onSettled: (_d, _e, items) => refresh((items ?? []).map(scopeOfNode)),
	});
	const starM = useMutation({
		mutationFn: (n: FileNode) =>
			setNodeStarred(scopeOfNode(n), n.id, !n.starred),
		onSuccess: (_d, n) =>
			ok(n.starred ? t("toast.unstarred") : t("toast.starred")),
		onError: fail,
		onSettled: (_d, _e, n) => n && refresh([scopeOfNode(n)]),
	});

	// ── Open / preview / download ────────────────────────────────────────────
	const [previewId, setPreviewId] = useState<string | null>(null);
	const previewable = useMemo(
		() => nodes.filter((n) => n.kind === "file"),
		[nodes],
	);
	const previewIndex = previewable.findIndex((n) => n.id === previewId);

	const download = async (n: FileNode) => {
		try {
			const d = await getNodeDownload(scopeOfNode(n), n.id, {
				download: true,
			});
			const a = document.createElement("a");
			a.href = d.url;
			a.download = n.name;
			document.body.appendChild(a);
			a.click();
			a.remove();
			refresh([scopeOfNode(n)]);
		} catch (error) {
			fail(error);
		}
	};

	const open = (n: FileNode) => {
		if (inTrash) return;
		if (n.kind === "folder") goToFolder(scopeOfNode(n), n.id);
		else setPreviewId(n.id);
	};

	const handlers: FileActionHandlers = {
		open,
		download: (n) => void download(n),
		toggleStar: (n) => starM.mutate(n),
		rename: (n) => setDialog({ kind: "rename", node: n }),
		move: (n) => setDialog({ kind: "move", nodes: targetsFor(n) }),
		details: (n) => setDetailsNode(n),
		trash: (n) => {
			const items = targetsFor(n);
			// Only attachments of tasks need a confirmation: that delete is final.
			if (items.some((x) => x.source === "task_attachment"))
				setDialog({ kind: "trash", nodes: items });
			else trashM.mutate(items);
		},
		restore: (n) => restoreM.mutate(targetsFor(n)),
		deleteForever: (n) =>
			setDialog({ kind: "deleteForever", nodes: targetsFor(n) }),
	};

	const actionsFor = (n: FileNode) =>
		buildFileActions(
			n,
			{ t, inTrash, canWrite: canWriteScope(scopeOfNode(n)) },
			handlers,
		);

	// ── Upload (picker + drop) ───────────────────────────────────────────────
	const canUploadHere =
		section === "browse" &&
		!!browseScope &&
		canWriteScope(browseScope) &&
		!(currentFolder?.system || currentFolder?.locked);
	const startUpload = (entries: UploadEntry[]) => {
		if (!browseScope || entries.length === 0) return;
		enqueueUploads(browseScope, folderId, entries, () =>
			refresh([browseScope]),
		);
	};
	const fileInput = useRef<HTMLInputElement>(null);
	const folderInput = useRef<HTMLInputElement>(null);
	const [dropping, setDropping] = useState(false);
	const dragDepth = useRef(0);
	const isFileDrag = (e: React.DragEvent) =>
		e.dataTransfer.types.includes("Files");

	const moveByDrag = (ids: string[], parentId: string | null) => {
		const items = nodes.filter((n) => ids.includes(n.id) && !n.locked);
		if (items.length === 0) return;
		moveM.mutate({ items, parentId });
	};

	// ── Search box ───────────────────────────────────────────────────────────
	const [searchText, setSearchText] = useState(search.q ?? "");
	useEffect(() => setSearchText(search.q ?? ""), [search.q]);
	const pushSearch = useDebouncedCallback((v: string) => {
		void setSearch({ q: v.trim() || undefined });
	}, 300);

	const [view, setView] = useState<"grid" | "list">(readView);
	const changeView = (v: "grid" | "list") => {
		setView(v);
		try {
			localStorage.setItem(VIEW_KEY, v);
		} catch {
			/* ignore */
		}
	};

	// ── Keyboard ─────────────────────────────────────────────────────────────
	const onKeyDown = (e: React.KeyboardEvent) => {
		if ((e.target as HTMLElement).closest("input, textarea, [role=dialog]"))
			return;
		const picked = nodes.filter((n) => selected.has(n.id));
		if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "a") {
			e.preventDefault();
			setSelected(new Set(nodes.map((n) => n.id)));
		} else if (e.key === "Escape") {
			setSelected(new Set());
		} else if (e.key === "F2" && picked.length === 1 && !picked[0].locked) {
			if (canWriteScope(scopeOfNode(picked[0]))) handlers.rename(picked[0]);
		} else if (
			(e.key === "Delete" || e.key === "Backspace") &&
			picked.length > 0 &&
			!inTrash
		) {
			const items = picked.filter(
				(n) => !n.system && canWriteScope(scopeOfNode(n)),
			);
			if (items.length > 0) handlers.trash(items[0]);
		}
	};

	// ── Render ───────────────────────────────────────────────────────────────
	const title =
		section === "browse"
			? t("nav.files")
			: t(`sectionTitle.${section}` as "sectionTitle.trash");
	const atRoots = section === "browse" && !projectId && !browseScope;
	const searching = !!query || !!typeFilter;
	const writableHere =
		section === "browse" && !!browseScope && canWriteScope(browseScope);

	const locationOf = (n: FileNode) => {
		const where = scopeName(scopeOfNode(n));
		return projectId ? "" : where;
	};

	const viewProps: FileViewProps = {
		nodes,
		selected,
		canDrag: (n) =>
			section === "browse" && !n.locked && canWriteScope(scopeOfNode(n)),
		inTrash,
		showLocation: !projectId && section !== "browse",
		locationOf,
		actionsFor,
		onSelect,
		onOpen: open,
		onContextSelect: (n) =>
			setSelected((prev) => (prev.has(n.id) ? prev : new Set([n.id]))),
		onDragItems: (n) => (selected.has(n.id) ? [...selected] : [n.id]),
		onDropOnFolder: (target, ids) =>
			moveByDrag(
				ids.filter((id) => id !== target.id),
				target.id,
			),
		sort: section === "browse" ? sort : undefined,
		onSort:
			section === "browse"
				? (key) =>
						setSort((s) => ({ key, desc: s.key === key ? !s.desc : false }))
				: undefined,
	};

	const emptyKey = searching
		? "search"
		: section === "browse"
			? "folder"
			: section;

	return (
		// biome-ignore lint/a11y/noStaticElementInteractions: drop zone for files dragged in from the desktop; uploads also work from the New menu
		<div
			className="relative flex h-full min-h-0 flex-1 flex-col"
			onDragEnter={(e) => {
				if (!canUploadHere || !isFileDrag(e)) return;
				dragDepth.current++;
				setDropping(true);
			}}
			onDragOver={(e) => {
				if (canUploadHere && isFileDrag(e)) e.preventDefault();
			}}
			onDragLeave={(e) => {
				if (!isFileDrag(e)) return;
				dragDepth.current = Math.max(0, dragDepth.current - 1);
				if (dragDepth.current === 0) setDropping(false);
			}}
			onDrop={(e) => {
				if (!canUploadHere || !isFileDrag(e)) return;
				e.preventDefault();
				dragDepth.current = 0;
				setDropping(false);
				void collectDropped(e.dataTransfer).then(startUpload);
			}}
		>
			{/* Header. The selection bar below covers it instead of adding a row, so
			    selecting an item never moves the list (which would break a double click). */}
			<div className="relative">
				<header className="flex flex-wrap items-center gap-2 border-b px-4 py-3">
					<nav
						aria-label={title}
						className="flex min-w-0 flex-1 flex-wrap items-center gap-1 text-lg font-semibold"
					>
						{section !== "browse" ? (
							<h1>{title}</h1>
						) : (
							<Breadcrumbs
								rootLabel={
									projectId
										? (project.data?.name ?? t("nav.files"))
										: t("nav.files")
								}
								showScope={!projectId && !!browseScope}
								scopeLabel={browseScope ? scopeName(browseScope) : ""}
								path={path}
								onRoots={() => goToFolder(null, null)}
								onScopeRoot={() => goToFolder(browseScope, null)}
								onFolder={(id) => goToFolder(browseScope, id)}
								onDropOn={(ids, id) => moveByDrag(ids, id)}
								canDrop={writableHere}
							/>
						)}
					</nav>

					{section === "browse" && !atRoots && (
						<div className="relative w-56 max-sm:w-full">
							<Search className="pointer-events-none absolute top-2 left-2.5 size-4 text-muted-foreground" />
							<Input
								value={searchText}
								placeholder={t("header.search", {
									where:
										currentFolder?.name ??
										scopeName(browseScope ?? GENERAL_SCOPE) ??
										"",
								})}
								aria-label={t("header.search", { where: "" })}
								className="pl-8"
								onChange={(e) => {
									setSearchText(e.target.value);
									pushSearch(e.target.value);
								}}
							/>
						</div>
					)}
					{section === "browse" && !atRoots && (
						<NativeSelect
							aria-label={t("header.typeFilter")}
							value={typeFilter ?? ""}
							onChange={(e) =>
								void setSearch({ type: e.target.value || undefined })
							}
						>
							<NativeSelectOption value="">
								{t("header.allTypes")}
							</NativeSelectOption>
							{FILE_TYPE_FILTERS.map((f) => (
								<NativeSelectOption key={f} value={f}>
									{t(`types.${f}`)}
								</NativeSelectOption>
							))}
						</NativeSelect>
					)}
					{!atRoots && (
						<ToggleGroup
							aria-label={t("header.view.list")}
							variant="outline"
							size="sm"
							spacing={0}
							value={[view]}
							onValueChange={(next) => {
								const v = next[0];
								if (v === "grid" || v === "list") changeView(v);
							}}
						>
							<ToggleGroupItem value="list" aria-label={t("header.view.list")}>
								<List className="size-4" />
							</ToggleGroupItem>
							<ToggleGroupItem value="grid" aria-label={t("header.view.grid")}>
								<Grid2x2 className="size-4" />
							</ToggleGroupItem>
						</ToggleGroup>
					)}
					{/* New — top right of the page */}
					{writableHere &&
						!(currentFolder?.system || currentFolder?.locked) && (
							<DropdownMenu>
								<DropdownMenuTrigger render={<Button />}>
									<Plus className="size-4" />
									{t("header.new")}
								</DropdownMenuTrigger>
								<DropdownMenuContent align="end" className="min-w-48">
									<DropdownMenuItem
										onClick={() => setDialog({ kind: "newFolder" })}
									>
										<FolderPlus className="size-4" />
										{t("header.newFolder")}
									</DropdownMenuItem>
									<DropdownMenuItem onClick={() => fileInput.current?.click()}>
										<FilePlus2 className="size-4" />
										{t("header.uploadFile")}
									</DropdownMenuItem>
									<DropdownMenuItem
										onClick={() => folderInput.current?.click()}
									>
										<FolderUp className="size-4" />
										{t("header.uploadFolder")}
									</DropdownMenuItem>
								</DropdownMenuContent>
							</DropdownMenu>
						)}
					<input
						ref={fileInput}
						type="file"
						multiple
						hidden
						onChange={(e) => {
							if (e.target.files) startUpload(collectFromInput(e.target.files));
							e.target.value = "";
						}}
					/>
					<input
						ref={folderInput}
						type="file"
						hidden
						{...({ webkitdirectory: "" } as object)}
						onChange={(e) => {
							if (e.target.files) startUpload(collectFromInput(e.target.files));
							e.target.value = "";
						}}
					/>
				</header>

				{/* Selection bar */}
				{selected.size > 0 && (
					<div className="absolute inset-0 z-20 flex items-center gap-2 border-b bg-background px-4 text-sm">
						<Button
							variant="ghost"
							size="icon-sm"
							aria-label={t("dialogs.cancel")}
							onClick={() => setSelected(new Set())}
						>
							<X className="size-4" />
						</Button>
						<span className="font-medium">
							{t("selected", { count: selected.size })}
						</span>
						{(() => {
							const picked = nodes.filter((n) => selected.has(n.id));
							const first = picked[0];
							const writable =
								picked.length > 0 &&
								picked.every((n) => canWriteScope(scopeOfNode(n)));
							if (!first || !writable) return null;
							if (inTrash) {
								return (
									<>
										<Button
											size="sm"
											variant="outline"
											onClick={() => restoreM.mutate(picked)}
										>
											{t("actions.restore")}
										</Button>
										<Button
											size="sm"
											variant="outline"
											onClick={() =>
												setDialog({ kind: "deleteForever", nodes: picked })
											}
										>
											{t("actions.deleteForever")}
										</Button>
									</>
								);
							}
							return (
								<>
									{picked.every((n) => !n.locked) && (
										<Button
											size="sm"
											variant="outline"
											onClick={() => setDialog({ kind: "move", nodes: picked })}
										>
											{t("actions.move")}
										</Button>
									)}
									{picked.every((n) => !n.system) && (
										<Button
											size="sm"
											variant="outline"
											onClick={() => handlers.trash(first)}
										>
											<Trash2 className="size-4" />
											{t("actions.trash")}
										</Button>
									)}
								</>
							);
						})()}
					</div>
				)}
			</div>

			{/* Content */}
			{/* biome-ignore lint/a11y/noStaticElementInteractions: keyboard shortcuts (Ctrl+A, Delete, F2) for the selection; every action is also in a menu */}
			<div
				role="presentation"
				tabIndex={-1}
				onKeyDown={onKeyDown}
				onClick={(e) => {
					if (e.target === e.currentTarget) setSelected(new Set());
				}}
				className="min-h-0 flex-1 overflow-y-auto outline-none"
			>
				{forbidden ? (
					<div className="p-6">
						<NoPermissionState
							title={t("noAccess.title")}
							description={t("noAccess.body")}
						/>
					</div>
				) : atRoots ? (
					<RootsView
						roots={roots.data}
						loading={roots.isLoading}
						onOpen={(r) =>
							goToFolder(
								r.kind === "general"
									? GENERAL_SCOPE
									: { projectId: r.project_id as string },
								null,
							)
						}
					/>
				) : isLoading ? (
					<div className="space-y-2 p-4">
						{SKELETON_ROWS.map((id) => (
							<Skeleton key={id} className="h-9 w-full" />
						))}
					</div>
				) : nodes.length === 0 ? (
					<Empty className="py-20">
						<EmptyHeader>
							<EmptyMedia variant="icon">
								<Lock className="size-5" />
							</EmptyMedia>
							<EmptyTitle>
								{t(`empty.${emptyKey}` as "empty.folder")}
							</EmptyTitle>
							{emptyKey !== "search" && (
								<EmptyDescription>
									{t(`empty.${emptyKey}Hint` as "empty.folderHint")}
								</EmptyDescription>
							)}
						</EmptyHeader>
					</Empty>
				) : view === "grid" ? (
					<FileGridView {...viewProps} />
				) : (
					<FileListView {...viewProps} />
				)}
				{section === "browse" && (
					<LoadMoreSentinel
						hasMore={!!browse.hasNextPage}
						isLoadingMore={browse.isFetchingNextPage}
						isError={browse.isFetchNextPageError}
						onLoadMore={() => void browse.fetchNextPage()}
					/>
				)}
			</div>

			{dropping && (
				<div className="pointer-events-none absolute inset-2 z-30 flex items-center justify-center rounded-xl border-2 border-dashed border-primary bg-primary/10 text-lg font-medium text-primary">
					{t("upload.dropHere", {
						where:
							currentFolder?.name ?? scopeName(browseScope ?? GENERAL_SCOPE),
					})}
				</div>
			)}

			{/* Dialogs */}
			<NameDialog
				open={dialog?.kind === "newFolder"}
				title={t("dialogs.newFolder.title")}
				label={t("dialogs.newFolder.label")}
				initial={t("dialogs.newFolder.default")}
				submitLabel={t("dialogs.newFolder.create")}
				pending={createFolderM.isPending}
				onSubmit={(name) => createFolderM.mutate(name)}
				onClose={() => setDialog(null)}
			/>
			<NameDialog
				open={dialog?.kind === "rename"}
				title={t("dialogs.rename.title")}
				label={t("dialogs.rename.label")}
				initial={dialog?.kind === "rename" ? dialog.node.name : ""}
				submitLabel={t("dialogs.rename.save")}
				pending={renameM.isPending}
				onSubmit={(name) =>
					dialog?.kind === "rename" &&
					renameM.mutate({ node: dialog.node, name })
				}
				onClose={() => setDialog(null)}
			/>
			{dialog?.kind === "move" && (
				<MoveDialog
					open
					scope={scopeOfNode(dialog.nodes[0])}
					scopeName={scopeName(scopeOfNode(dialog.nodes[0]))}
					nodes={dialog.nodes}
					startFolderId={folderId}
					pending={moveM.isPending}
					onMove={(parentId) => moveM.mutate({ items: dialog.nodes, parentId })}
					onClose={() => setDialog(null)}
				/>
			)}
			<ConfirmDialog
				open={dialog?.kind === "trash"}
				title={t("dialogs.deleteTaskAttachment.title")}
				body={
					dialog?.kind === "trash"
						? t("dialogs.deleteTaskAttachment.body", {
								name: dialog.nodes[0]?.name,
							})
						: ""
				}
				confirmLabel={t("dialogs.deleteTaskAttachment.confirm")}
				pending={trashM.isPending}
				onConfirm={() =>
					dialog?.kind === "trash" && trashM.mutate(dialog.nodes)
				}
				onClose={() => setDialog(null)}
			/>
			<ConfirmDialog
				open={dialog?.kind === "deleteForever"}
				title={t("dialogs.deleteForever.title")}
				body={
					dialog?.kind === "deleteForever"
						? dialog.nodes.length === 1
							? t("dialogs.deleteForever.body", { name: dialog.nodes[0].name })
							: t("dialogs.deleteForever.bodyMany", {
									count: dialog.nodes.length,
								})
						: ""
				}
				confirmLabel={t("dialogs.deleteForever.confirm")}
				pending={deleteForeverM.isPending}
				onConfirm={() =>
					dialog?.kind === "deleteForever" &&
					deleteForeverM.mutate(dialog.nodes)
				}
				onClose={() => setDialog(null)}
			/>
			<DetailsSheet
				node={detailsNode}
				scope={detailsNode ? scopeOfNode(detailsNode) : GENERAL_SCOPE}
				scopeName={detailsNode ? scopeName(scopeOfNode(detailsNode)) : ""}
				onClose={() => setDetailsNode(null)}
			/>
			{previewIndex >= 0 && (
				<Suspense fallback={null}>
					<FilePreview
						files={previewable}
						index={previewIndex}
						scope={browseScope ?? GENERAL_SCOPE}
						onIndexChange={(i) => setPreviewId(previewable[i]?.id ?? null)}
						onClose={() => setPreviewId(null)}
						onDownload={(n) => void download(n)}
					/>
				</Suspense>
			)}
		</div>
	);
}

// ── Breadcrumbs ───────────────────────────────────────────────────────────────

function Crumb({
	label,
	current,
	onClick,
	onDropOn,
	canDrop,
}: {
	label: string;
	current?: boolean;
	onClick: () => void;
	onDropOn?: (ids: string[]) => void;
	canDrop?: boolean;
}) {
	const [over, setOver] = useState(false);
	return (
		<button
			type="button"
			aria-current={current ? "page" : undefined}
			onClick={onClick}
			onDragOver={(e) => {
				if (canDrop && e.dataTransfer.types.includes(FILE_DRAG_MIME)) {
					e.preventDefault();
					setOver(true);
				}
			}}
			onDragLeave={() => setOver(false)}
			onDrop={(e) => {
				setOver(false);
				const raw = e.dataTransfer.getData(FILE_DRAG_MIME);
				if (canDrop && raw && onDropOn) {
					e.preventDefault();
					onDropOn(JSON.parse(raw) as string[]);
				}
			}}
			className={cn(
				"max-w-56 truncate rounded-md px-2 py-0.5 hover:bg-muted",
				current ? "font-semibold" : "font-normal text-muted-foreground",
				over && "bg-primary/20 ring-2 ring-primary",
			)}
		>
			{label}
		</button>
	);
}

function Breadcrumbs({
	rootLabel,
	showScope,
	scopeLabel,
	path,
	onRoots,
	onScopeRoot,
	onFolder,
	onDropOn,
	canDrop,
}: {
	rootLabel: string;
	/** Global Files: the first crumb is "Files", the second the scope. */
	showScope: boolean;
	scopeLabel: string;
	path: FileNode[];
	onRoots: () => void;
	onScopeRoot: () => void;
	onFolder: (id: string) => void;
	onDropOn: (ids: string[], folderId: string | null) => void;
	canDrop: boolean;
}) {
	const sep = (
		<ChevronRight className="size-4 shrink-0 text-muted-foreground" />
	);
	const last = path.length - 1;
	return (
		<>
			{showScope ? (
				<>
					<Crumb label={rootLabel} onClick={onRoots} />
					{sep}
					<Crumb
						label={scopeLabel}
						current={path.length === 0}
						onClick={onScopeRoot}
						canDrop={canDrop}
						onDropOn={(ids) => onDropOn(ids, null)}
					/>
				</>
			) : (
				<Crumb
					label={rootLabel}
					current={path.length === 0}
					onClick={onScopeRoot}
					canDrop={canDrop}
					onDropOn={(ids) => onDropOn(ids, null)}
				/>
			)}
			{path.map((p, i) => (
				<span key={p.id} className="flex items-center gap-1">
					{sep}
					<Crumb
						label={p.name}
						current={i === last}
						onClick={() => onFolder(p.id)}
						canDrop={canDrop && !p.locked}
						onDropOn={(ids) => onDropOn(ids, p.id)}
					/>
				</span>
			))}
		</>
	);
}

// ── Roots ─────────────────────────────────────────────────────────────────────

function RootsView({
	roots,
	loading,
	onOpen,
}: {
	roots?: FileRoot[];
	loading: boolean;
	onOpen: (r: FileRoot) => void;
}) {
	const { t } = useTranslation("files");
	if (loading) {
		return (
			<div className="grid grid-cols-[repeat(auto-fill,minmax(13rem,1fr))] gap-3 p-4">
				{SKELETON_ROWS.slice(0, 4).map((id) => (
					<Skeleton key={id} className="h-12" />
				))}
			</div>
		);
	}
	if (!roots || roots.length === 0) {
		return (
			<Empty className="py-20">
				<EmptyHeader>
					<EmptyMedia variant="icon">
						<Lock className="size-5" />
					</EmptyMedia>
					<EmptyTitle>{t("roots.empty")}</EmptyTitle>
					<EmptyDescription>{t("roots.emptyHint")}</EmptyDescription>
				</EmptyHeader>
			</Empty>
		);
	}
	return (
		<div className="grid grid-cols-[repeat(auto-fill,minmax(13rem,1fr))] gap-3 p-4">
			{roots.map((r) => (
				<button
					key={r.project_id ?? "general"}
					type="button"
					onClick={() => onOpen(r)}
					className="flex items-center gap-3 rounded-xl border bg-card px-3 py-2.5 text-left text-sm hover:bg-muted/60"
				>
					<FileTypeIcon
						node={{
							kind: "folder",
							name: r.name,
							content_type: "",
							locked: false,
						}}
						className="size-5"
					/>
					<span className="min-w-0 flex-1 truncate font-medium">
						{r.kind === "general" ? t("roots.general") : r.name}
					</span>
				</button>
			))}
		</div>
	);
}
