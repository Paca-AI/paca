import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
	Link,
	useNavigate,
	useParams,
	useRouterState,
} from "@tanstack/react-router";
import {
	ArrowLeft,
	BookOpen,
	Bot,
	ChevronDown,
	ChevronRight,
	File,
	FileText,
	Folder,
	FolderKanban,
	FolderOpen,
	GanttChart,
	Home,
	KanbanSquare,
	Monitor,
	Moon,
	MoreHorizontal,
	Pencil,
	Plus,
	Puzzle,
	Settings,
	Shield,
	Sun,
	Trash2,
	Users,
} from "lucide-react";
import {
	type ComponentType,
	useCallback,
	useEffect,
	useRef,
	useState,
} from "react";
import { useTranslation } from "react-i18next";

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
import { usePermissions } from "@/hooks/use-permissions";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import type { ThemeMode } from "@/hooks/use-theme-mode";
import { useThemeMode } from "@/hooks/use-theme-mode";
import { currentUserOptionalQueryOptions } from "@/lib/auth-api";
import {
	createDocument,
	createFolder,
	type DocFolder,
	type Document,
	deleteDocument,
	deleteFolder,
	docFoldersQueryOptions,
	docListQueryOptions,
	docQueryKeys,
	updateDocument,
	updateFolder,
} from "@/lib/doc-api";
import { sprintsQueryOptions, updateTask } from "@/lib/interaction-api";
import { ExtensionPoint } from "@/lib/plugins/extension-point";
import { projectQueryOptions, projectsQueryOptions } from "@/lib/project-api";
import { cn } from "@/lib/utils";
import { LanguageSwitcher } from "./language-switcher";
import { UserMenu } from "./user-menu";

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
	const location = useRouterState({ select: (s) => s.location.pathname });
	const navigate = useNavigate();
	const qc = useQueryClient();
	const [renaming, setRenaming] = useState(false);

	const isActive = location === `/projects/${projectId}/docs/${doc.id}`;

	const renameMutation = useMutation({
		mutationFn: (title: string) => updateDocument(projectId, doc.id, { title }),
		onSuccess: (updated) => {
			qc.setQueryData(docQueryKeys.detail(projectId, doc.id), updated);
			qc.invalidateQueries({ queryKey: docQueryKeys.list(projectId) });
			if (doc.folder_id) {
				qc.invalidateQueries({
					queryKey: docQueryKeys.list(projectId, doc.folder_id),
				});
			}
		},
	});

	const deleteMutation = useMutation({
		mutationFn: () => deleteDocument(projectId, doc.id),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: docQueryKeys.all(projectId) });
			if (isActive) {
				navigate({ to: "/projects/$projectId", params: { projectId } });
			}
		},
	});

	return (
		<div
			className="group relative flex items-center gap-1 pr-1"
			style={{ paddingLeft: `${8 + depth * 16 + 16}px` }}
		>
			<button
				type="button"
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
						initialValue={doc.title || "Untitled"}
						onConfirm={(title) => {
							renameMutation.mutate(title);
							setRenaming(false);
						}}
						onCancel={() => setRenaming(false)}
					/>
				) : (
					<span className="truncate leading-snug">
						{doc.title || (
							<span className="italic text-sidebar-foreground/40">
								Untitled
							</span>
						)}
					</span>
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
							onClick={(e) => {
								e.stopPropagation();
								setRenaming(true);
							}}
						>
							<Pencil className="size-3.5 mr-2" />
							Rename
						</DropdownMenuItem>
						<DropdownMenuSeparator />
						<DropdownMenuItem
							className="text-destructive focus:text-destructive"
							onClick={(e) => {
								e.stopPropagation();
								deleteMutation.mutate();
							}}
						>
							<Trash2 className="size-3.5 mr-2" />
							Delete
						</DropdownMenuItem>
					</DropdownMenuContent>
				</DropdownMenu>
			)}
		</div>
	);
}

/** Folder node — fetches its docs lazily when expanded */
function DocsFolderNode({
	folder,
	projectId,
	allFolders,
	canWrite,
	expandedFolders,
	onToggle,
	depth,
}: {
	folder: DocFolder;
	projectId: string;
	allFolders: DocFolder[];
	canWrite: boolean;
	expandedFolders: Set<string>;
	onToggle: (id: string) => void;
	depth: number;
}) {
	const qc = useQueryClient();
	const [renaming, setRenaming] = useState(false);
	const [addingDoc, setAddingDoc] = useState(false);
	const navigate = useNavigate();

	const isExpanded = expandedFolders.has(folder.id);

	const { data: folderDocs = [] } = useQuery({
		...docListQueryOptions(projectId, folder.id),
		enabled: isExpanded,
	});

	const childFolders = allFolders.filter((f) => f.parent_id === folder.id);
	const renameMutation = useMutation({
		mutationFn: (name: string) => updateFolder(projectId, folder.id, { name }),
		onSuccess: () =>
			qc.invalidateQueries({ queryKey: docQueryKeys.folders(projectId) }),
	});

	const deleteMutation = useMutation({
		mutationFn: () => deleteFolder(projectId, folder.id),
		onSuccess: () =>
			qc.invalidateQueries({ queryKey: docQueryKeys.folders(projectId) }),
	});

	const newDocMutation = useMutation({
		mutationFn: () =>
			createDocument(projectId, { title: "Untitled", folder_id: folder.id }),
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
		mutationFn: (name: string) =>
			createFolder(projectId, { name, parent_id: folder.id }),
		onSuccess: () =>
			qc.invalidateQueries({ queryKey: docQueryKeys.folders(projectId) }),
	});

	return (
		<div>
			{/* Folder row */}
			<div
				className="group relative flex items-center gap-1 pr-1"
				style={{ paddingLeft: `${8 + depth * 16}px` }}
			>
				<button
					type="button"
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
							initialValue={folder.name}
							onConfirm={(name) => {
								renameMutation.mutate(name);
								setRenaming(false);
							}}
							onCancel={() => setRenaming(false)}
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
										setRenaming(true);
									}}
								>
									<Pencil className="size-3.5 mr-2" />
									Rename
								</DropdownMenuItem>
								<DropdownMenuSeparator />
								<DropdownMenuItem
									className="text-destructive focus:text-destructive"
									onClick={(e) => {
										e.stopPropagation();
										deleteMutation.mutate();
									}}
								>
									<Trash2 className="size-3.5 mr-2" />
									Delete
								</DropdownMenuItem>
							</DropdownMenuContent>
						</DropdownMenu>
					</div>
				)}
			</div>

			{/* Children */}
			{isExpanded && (
				<div>
					{childFolders.map((cf) => (
						<DocsFolderNode
							key={cf.id}
							folder={cf}
							projectId={projectId}
							allFolders={allFolders}
							canWrite={canWrite}
							expandedFolders={expandedFolders}
							onToggle={onToggle}
							depth={depth + 1}
						/>
					))}
					{folderDocs.map((doc) => (
						<DocsDocRow
							key={doc.id}
							doc={doc}
							projectId={projectId}
							canWrite={canWrite}
							depth={depth + 1}
						/>
					))}
					{folderDocs.length === 0 &&
						childFolders.length === 0 &&
						!addingDoc && (
							<div
								className="text-xs text-sidebar-foreground/30 italic py-1"
								style={{ paddingLeft: `${8 + (depth + 1) * 16 + 26}px` }}
							>
								Empty folder
							</div>
						)}
					{canWrite && (
						<div style={{ paddingLeft: `${8 + (depth + 1) * 16 + 16}px` }}>
							<DropdownMenu>
								<DropdownMenuTrigger className="flex w-full items-center gap-1.5 rounded-md px-2 py-1 text-xs text-sidebar-foreground/35 hover:text-sidebar-foreground hover:bg-sidebar-accent/40 transition-all duration-150">
									<Plus className="size-3 shrink-0" />
									<span>Add</span>
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
										New Document
									</DropdownMenuItem>
									<DropdownMenuItem
										onClick={() => {
											if (!isExpanded) onToggle(folder.id);
											newSubfolderMutation.mutate("New Folder");
										}}
										disabled={newSubfolderMutation.isPending}
									>
										<FolderOpen className="size-3.5 mr-2" />
										New Subfolder
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
	const { t } = useTranslation();
	const qc = useQueryClient();
	const navigate = useNavigate();
	const location = useRouterState({ select: (s) => s.location.pathname });
	const { hasProjectPermission } = useProjectPermissions(projectId);
	const canWrite = hasProjectPermission("docs.write");

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

	const { data: allFolders = [] } = useQuery(docFoldersQueryOptions(projectId));
	const { data: rootDocs = [] } = useQuery(docListQueryOptions(projectId));

	// Use loose null check — backend omits parent_id for root folders (omitempty)
	const rootFolders = allFolders.filter((f) => !f.parent_id);
	const rootOnlyDocs = rootDocs.filter((d) => !d.folder_id);

	const newDocMutation = useMutation({
		mutationFn: () => createDocument(projectId, { title: "Untitled" }),
		onSuccess: (doc) => {
			qc.invalidateQueries({ queryKey: docQueryKeys.all(projectId) });
			navigate({
				to: "/projects/$projectId/docs/$docId",
				params: { projectId, docId: doc.id },
			});
		},
	});

	const newFolderMutation = useMutation({
		mutationFn: (name: string) => createFolder(projectId, { name }),
		onSuccess: () =>
			qc.invalidateQueries({ queryKey: docQueryKeys.folders(projectId) }),
	});

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
	const { state: sidebarState } = useSidebar();
	const isSidebarCollapsed = sidebarState === "collapsed";

	if (isSidebarCollapsed) {
		return (
			<SidebarGroup>
				<SidebarGroupContent>
					<SidebarMenu>
						<SidebarMenuItem>
							<SidebarMenuButton tooltip={t("common.documentation")}>
								<BookOpen className="size-4" />
							</SidebarMenuButton>
						</SidebarMenuItem>
					</SidebarMenu>
				</SidebarGroupContent>
			</SidebarGroup>
		);
	}

	return (
		<SidebarGroup className="px-0">
			{/* Section header */}
			<SidebarGroupLabel
				className="flex cursor-pointer items-center justify-between hover:text-sidebar-foreground transition-colors px-3"
				onClick={toggleCollapse}
			>
				<span>{t("common.documentation")}</span>
				<ChevronRight
					className={cn(
						"size-3.5 transition-transform duration-200 text-sidebar-foreground/40",
						!collapsed && "rotate-90",
					)}
				/>
			</SidebarGroupLabel>

			{!collapsed && (
				<SidebarGroupContent>
					<div className="py-1 space-y-0.5">
						{isEmpty ? (
							<div className="px-4 py-2 text-xs text-sidebar-foreground/40 italic">
								{t("common.noDocumentsYet")}
							</div>
						) : (
							<>
								{rootFolders.map((folder) => (
									<DocsFolderNode
										key={folder.id}
										folder={folder}
										projectId={projectId}
										allFolders={allFolders}
										canWrite={canWrite}
										expandedFolders={expandedFolders}
										onToggle={toggleFolder}
										depth={0}
									/>
								))}
								{rootOnlyDocs.map((doc) => (
									<DocsDocRow
										key={doc.id}
										doc={doc}
										projectId={projectId}
										canWrite={canWrite}
										depth={0}
									/>
								))}
							</>
						)}
						{canWrite && (
							<div className="px-2 pt-1">
								<DropdownMenu>
									<DropdownMenuTrigger className="flex w-full items-center gap-1.5 rounded-md px-2 py-1 text-xs text-sidebar-foreground/35 hover:text-sidebar-foreground hover:bg-sidebar-accent/40 transition-all duration-150">
										<Plus className="size-3 shrink-0" />
										<span>Add</span>
									</DropdownMenuTrigger>
									<DropdownMenuContent align="start" className="w-40">
										<DropdownMenuItem
											onClick={() => newDocMutation.mutate()}
											disabled={newDocMutation.isPending}
										>
											<File className="size-3.5 mr-2" />
											New Document
										</DropdownMenuItem>
										<DropdownMenuItem
											onClick={() => newFolderMutation.mutate("New Folder")}
											disabled={newFolderMutation.isPending}
										>
											<FolderOpen className="size-3.5 mr-2" />
											New Folder
										</DropdownMenuItem>
									</DropdownMenuContent>
								</DropdownMenu>
							</div>
						)}
					</div>
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
	const { t } = useTranslation();
	const [open, setOpen] = useState(false);
	const { data: projectsResult } = useQuery(projectsQueryOptions());
	const { data: currentProject } = useQuery({
		...projectQueryOptions(currentProjectId ?? ""),
		enabled: !!currentProjectId,
	});

	const projects = projectsResult?.items ?? [];
	const label = currentProject?.name ?? t("common.projects");
	const initials = currentProject?.name
		? currentProject.name.slice(0, 2).toUpperCase()
		: null;

	const { data: user } = useQuery(currentUserOptionalQueryOptions);

	if (!user) {
		return (
			<div className="flex w-full items-center gap-2.5 rounded-lg px-2 py-1.5 text-sm font-medium text-sidebar-foreground/80 select-none">
				<div className="flex size-5 shrink-0 items-center justify-center rounded-md bg-primary/15 text-primary text-xs font-bold">
					{initials ?? <FolderKanban className="size-3" />}
				</div>
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
				<div className="flex size-5 shrink-0 items-center justify-center rounded-md bg-primary/15 text-primary text-xs font-bold">
					{initials ?? <FolderKanban className="size-3" />}
				</div>
				<span className="flex-1 truncate text-left">{label}</span>
				<ChevronDown
					className={cn(
						"size-3.5 shrink-0 opacity-40 transition-transform duration-200",
						open && "rotate-180",
					)}
				/>
			</DropdownMenuTrigger>
			<DropdownMenuContent align="start" sideOffset={6} className="w-60">
				<DropdownMenuGroup>
					<DropdownMenuLabel className="text-xs text-muted-foreground pb-1">
						{t("common.yourProjects")}
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
								<div className="flex size-5 shrink-0 items-center justify-center rounded bg-primary/15 text-primary text-xs font-bold">
									{p.name.slice(0, 2).toUpperCase()}
								</div>
								<span className="truncate">{p.name}</span>
								{p.id === currentProjectId && (
									<span className="ml-auto size-1.5 rounded-full bg-primary" />
								)}
							</DropdownMenuItem>
						))}
					</DropdownMenuGroup>
				) : (
					<div className="flex flex-col items-center gap-1 px-3 py-4">
						<div className="flex size-8 items-center justify-center rounded-md bg-muted">
							<FolderKanban className="size-4 text-muted-foreground" />
						</div>
						<p className="text-xs text-muted-foreground mt-0.5">
							{t("common.noProjectsYet")}
						</p>
					</div>
				)}
				<DropdownMenuSeparator />
				{canCreate ? (
					<DropdownMenuItem render={<Link to="/home" />}>
						<Plus className="size-3.5" />
						{t("common.newProject")}
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
}: {
	to: string;
	icon: ComponentType<{ className?: string }>;
	label: string;
	badge?: string;
}) {
	const location = useRouterState({ select: (s) => s.location.pathname });
	const isActive = location === to || location.startsWith(`${to}/`);

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
						: "hover:bg-sidebar-accent/60",
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
	{ segment: "agents", icon: Bot, label: "Agents" },
	{ segment: "team", icon: Users, label: "Team" },
	{ segment: "settings", icon: Settings, label: "Settings" },
] as const;

function ProjectNav() {
	const { t } = useTranslation();
	return (
		<SidebarGroup>
			<SidebarGroupContent>
				<SidebarMenu>
					<SidebarMenuItem>
						<SidebarMenuButton
							tooltip={t("common.allProjects")}
							render={<Link to="/home" />}
							className="text-muted-foreground hover:text-foreground hover:bg-sidebar-accent/60 transition-all"
						>
							<ArrowLeft className="size-4" />
							<span>{t("common.allProjects")}</span>
						</SidebarMenuButton>
					</SidebarMenuItem>
				</SidebarMenu>
			</SidebarGroupContent>
		</SidebarGroup>
	);
}

const ANON_HIDDEN_SEGMENTS = new Set(["agents", "team", "settings"]);

function ProjectNavItems({
	projectId,
	isAnonymous,
}: {
	projectId: string;
	isAnonymous?: boolean;
}) {
	const { t } = useTranslation();
	const location = useRouterState({ select: (s) => s.location.pathname });

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

	return (
		<SidebarGroup>
			<SidebarGroupLabel
				className="flex cursor-pointer items-center justify-between hover:text-sidebar-foreground transition-colors"
				onClick={toggle}
			>
				<span>{t("common.project")}</span>
				<ChevronRight
					className={cn(
						"size-3.5 transition-transform duration-200 text-sidebar-foreground/40",
						!collapsed && "rotate-90",
					)}
				/>
			</SidebarGroupLabel>

			{!collapsed && (
				<SidebarGroupContent>
					<SidebarMenu>
						{PROJECT_NAV_ITEMS.filter(
							(item) => !isAnonymous || !ANON_HIDDEN_SEGMENTS.has(item.segment),
						).map(({ segment, icon: Icon, label }) => {
							const href = segment
								? `/projects/${projectId}/${segment}`
								: `/projects/${projectId}`;
							const isActive = segment
								? location.startsWith(href)
								: location === href || location === `${href}/`;
							return (
								<SidebarMenuItem key={label}>
									<SidebarMenuButton
										isActive={isActive}
										tooltip={label}
										render={<Link to={href} />}
										className={cn(
											"relative transition-all duration-150",
											isActive
												? "bg-primary/10 text-primary font-medium before:absolute before:left-0 before:inset-y-2 before:w-0.75 before:rounded-full before:bg-primary"
												: "hover:bg-sidebar-accent/60",
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

// ── Project Interactions Section ───────────────────────────────────────────────
function ProjectInteractionsSection({
	projectId,
	isAnonymous,
}: {
	projectId: string;
	isAnonymous?: boolean;
}) {
	const { t } = useTranslation();
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
		hasPermission("sprints.read") || hasProjectPermission("sprints.read");
	const canEditTasks =
		hasPermission("tasks.write") || hasProjectPermission("tasks.write");

	const [dragOverInteractionId, setDragOverInteractionId] = useState<
		string | null
	>(null);

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
		if (!canEditTasks || isAnonymous) return;
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

	const { data: sprints = [] } = useQuery({
		...sprintsQueryOptions(projectId),
		enabled: canViewSprints,
		retry: false,
		refetchInterval: 30_000,
	});

	// Hide entire section if user lacks the "View Sprints" permission
	// (anonymous visitors on public projects can always view interactions)
	if (!canViewSprints && !isAnonymous) return null;

	const openSprints = sprints
		.filter((s) => s.status === "active")
		.sort((a, b) => a.name.localeCompare(b.name));

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
			<SidebarGroupLabel
				className="flex cursor-pointer items-center justify-between hover:text-sidebar-foreground transition-colors"
				onClick={toggle}
			>
				<span>{t("common.interactions")}</span>
				<ChevronRight
					className={cn(
						"size-3.5 transition-transform duration-200 text-sidebar-foreground/40",
						!collapsed && "rotate-90",
					)}
				/>
			</SidebarGroupLabel>

			{!collapsed && (
				<SidebarGroupContent>
					<SidebarMenu>
						{/* Timeline */}
						<SidebarMenuItem>
							<SidebarMenuButton
								isActive={isTimelineActive}
								tooltip={t("common.timeline")}
								render={<Link to={timelineHref} />}
								className={cn(
									"relative transition-all duration-150",
									isTimelineActive
										? "bg-primary/10 text-primary font-medium before:absolute before:left-0 before:inset-y-2 before:w-0.75 before:rounded-full before:bg-primary"
										: "hover:bg-sidebar-accent/60",
								)}
							>
								<GanttChart className="size-4" />
								<span>{t("common.timeline")}</span>
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
								tooltip={t("common.productBacklog")}
								render={<Link to={backlogHref} />}
								className={cn(
									"relative transition-all duration-150",
									isBacklogActive
										? "bg-primary/10 text-primary font-medium before:absolute before:left-0 before:inset-y-2 before:w-0.75 before:rounded-full before:bg-primary"
										: "hover:bg-sidebar-accent/60",
									dragOverInteractionId === "backlog" &&
										"ring-2 ring-primary/40 bg-primary/5 text-primary",
								)}
							>
								<BookOpen className="size-4" />
								<span>{t("common.productBacklog")}</span>
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
												: "hover:bg-sidebar-accent/60",
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
					</SidebarMenu>
				</SidebarGroupContent>
			)}
		</SidebarGroup>
	);
}

// ── Theme Switcher ─────────────────────────────────────────────────────────────
const THEME_MODES = [
	{ mode: "light" as ThemeMode, Icon: Sun, label: "Light" },
	{ mode: "dark" as ThemeMode, Icon: Moon, label: "Dark" },
	{ mode: "auto" as ThemeMode, Icon: Monitor, label: "Auto" },
] as const;

function ThemeSwitcher() {
	const { t } = useTranslation();
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
						tooltip={`Theme: ${mode} — click to cycle`}
						onClick={cycle}
					>
						<CurrentIcon className="size-4" />
					</SidebarMenuButton>
				</SidebarMenuItem>
			</SidebarMenu>

			{/* Expanded: segmented 3-way control */}
			<div className="flex items-center justify-between px-2 py-1.5 group-data-[collapsible=icon]:hidden">
				<span className="text-xs font-medium text-sidebar-foreground/50 tracking-wide">
					{t("common.theme")}
				</span>
				<div className="flex items-center gap-0.5 rounded-md border border-sidebar-border bg-sidebar p-0.5">
					{THEME_MODES.map(({ mode: m, Icon }) => (
						<button
							key={m}
							type="button"
							onClick={() => set(m)}
							title={t(`common.${m}`)}
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
	const { t } = useTranslation();
	const { hasPermission } = usePermissions();
	const { resolvedMode } = useThemeMode();
	const { projectId } = useParams({ strict: false });
	const { data: user } = useQuery(currentUserOptionalQueryOptions);

	const canAccessGlobalRoles =
		hasPermission("global_roles.read") || hasPermission("global_roles.write");

	const canAccessUsers =
		hasPermission("users.read") || hasPermission("users.write");

	const canAccessPlugins = hasPermission("users.write");

	const canCreateProject = hasPermission("projects.create");

	const showAdminSection =
		canAccessGlobalRoles || canAccessUsers || canAccessPlugins;
	const isProjectContext = !!projectId;
	const isAnonymous = !user;

	return (
		<Sidebar collapsible="icon">
			{/* Brand */}
			<SidebarHeader className="gap-2 pb-2">
				<div className="flex items-center gap-2.5 px-2 pt-1">
					{user ? (
						<Link to="/home">
							<img
								src={
									resolvedMode === "dark"
										? "/paca-logo-dark.svg"
										: "/paca-logo.svg"
								}
								alt="Paca Logo"
								className="size-8 shrink-0"
							/>
						</Link>
					) : (
						<img
							src={
								resolvedMode === "dark"
									? "/paca-logo-dark.svg"
									: "/paca-logo.svg"
							}
							alt="Paca Logo"
							className="size-8 shrink-0"
						/>
					)}
					<span className="font-[Syne] font-bold text-base tracking-tight text-sidebar-foreground group-data-[collapsible=icon]:hidden">
						paca
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
			<SidebarContent>
				{isProjectContext ? (
					<>
						{user && <ProjectNav />}
						{user && <SidebarSeparator />}
						<ProjectInteractionsSection
							projectId={projectId}
							isAnonymous={isAnonymous}
						/>
						<SidebarSeparator />
						<DocsSidebarSection projectId={projectId} />
						<SidebarSeparator />
						<ExtensionPoint
							point="sidebar.project.section"
							componentProps={{ projectId }}
						/>
						<SidebarSeparator />
						<ProjectNavItems projectId={projectId} isAnonymous={isAnonymous} />
					</>
				) : (
					<>
						{user && (
							<SidebarGroup>
								<SidebarGroupContent>
									<SidebarMenu>
										<NavItem to="/home" icon={Home} label={t("common.home")} />
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
										{t("common.administration")}
									</SidebarGroupLabel>
									<SidebarGroupContent>
										<SidebarMenu>
											{canAccessGlobalRoles ? (
												<NavItem
													to="/admin/global-roles"
													icon={Shield}
													label={t("common.globalRoles")}
												/>
											) : null}
											{canAccessUsers ? (
												<NavItem
													to="/admin/users"
													icon={Users}
													label={t("common.users")}
												/>
											) : null}
											{canAccessPlugins ? (
												<NavItem
													to="/admin/plugins"
													icon={Puzzle}
													label={t("common.plugins")}
												/>
											) : null}
										</SidebarMenu>
									</SidebarGroupContent>
								</SidebarGroup>
							</>
						) : null}
					</>
				)}
			</SidebarContent>

			{/* Footer: theme toggle + user menu */}
			<SidebarSeparator />
			<SidebarFooter className="gap-1 pb-3">
				<ThemeSwitcher />
				<LanguageSwitcher />
				<UserMenu />
			</SidebarFooter>

			<SidebarRail />
		</Sidebar>
	);
}
