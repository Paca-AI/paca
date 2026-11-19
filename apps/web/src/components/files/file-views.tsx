import { ArrowDown, ArrowUp, Lock, MoreVertical, Star } from "lucide-react";
import type { DragEvent, KeyboardEvent, MouseEvent } from "react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
	ContextMenu,
	ContextMenuContent,
	ContextMenuTrigger,
} from "@/components/ui/context-menu";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
	type FileNode,
	type FileSortKey,
	fileCategory,
	formatFileSize,
} from "@/lib/files-api";
import { cn } from "@/lib/utils";
import {
	ActionsAsContextItems,
	ActionsAsDropdownItems,
	type FileAction,
} from "./file-actions";
import { FileTypeIcon } from "./file-icon";

/** MIME type of an in-app drag of file items (as opposed to files from the desktop). */
export const FILE_DRAG_MIME = "application/x-paca-file-items";

export interface FileViewProps {
	nodes: FileNode[];
	selected: ReadonlySet<string>;
	/** Folders and files lose their drag handles when the caller cannot move them. */
	canDrag: (n: FileNode) => boolean;
	inTrash: boolean;
	/** Show the folder an item lives in (search, starred, recent). */
	showLocation?: boolean;
	locationOf?: (n: FileNode) => string;
	actionsFor: (n: FileNode) => FileAction[];
	onSelect: (n: FileNode, mods: { toggle: boolean; range: boolean }) => void;
	onOpen: (n: FileNode) => void;
	onContextSelect: (n: FileNode) => void;
	onDragItems: (n: FileNode) => string[];
	onDropOnFolder: (target: FileNode, ids: string[]) => void;
	sort?: { key: FileSortKey; desc: boolean };
	onSort?: (key: FileSortKey) => void;
}

function formatDate(iso: string, locale: string) {
	return new Intl.DateTimeFormat(locale, { dateStyle: "medium" }).format(
		new Date(iso),
	);
}

/** Drag-and-drop wiring shared by rows and cards. */
function useItemDnd(
	n: FileNode,
	p: Pick<FileViewProps, "canDrag" | "onDragItems" | "onDropOnFolder">,
) {
	const [over, setOver] = useState(false);
	const droppable = n.kind === "folder" && !n.system;
	return {
		over,
		props: {
			draggable: p.canDrag(n),
			onDragStart: (e: DragEvent) => {
				e.dataTransfer.setData(
					FILE_DRAG_MIME,
					JSON.stringify(p.onDragItems(n)),
				);
				e.dataTransfer.effectAllowed = "move";
			},
			onDragOver: (e: DragEvent) => {
				if (droppable && e.dataTransfer.types.includes(FILE_DRAG_MIME)) {
					e.preventDefault();
					e.stopPropagation();
					setOver(true);
				}
			},
			onDragLeave: () => setOver(false),
			onDrop: (e: DragEvent) => {
				setOver(false);
				const raw = e.dataTransfer.getData(FILE_DRAG_MIME);
				if (!droppable || !raw) return;
				e.preventDefault();
				e.stopPropagation();
				p.onDropOnFolder(n, JSON.parse(raw) as string[]);
			},
		},
	};
}

function clickHandlers(n: FileNode, p: FileViewProps) {
	return {
		onClick: (e: MouseEvent) =>
			p.onSelect(n, { toggle: e.ctrlKey || e.metaKey, range: e.shiftKey }),
		onDoubleClick: () => p.onOpen(n),
		onContextMenuCapture: () => p.onContextSelect(n),
		onKeyDown: (e: KeyboardEvent) => {
			if (e.key === "Enter" && e.target === e.currentTarget) p.onOpen(n);
		},
	};
}

function ItemMenuButton({ actions }: { actions: FileAction[] }) {
	const { t } = useTranslation("files");
	if (actions.length === 0) return null;
	return (
		<DropdownMenu>
			<DropdownMenuTrigger
				render={
					<Button
						variant="ghost"
						size="icon-sm"
						aria-label={t("actions.more")}
						onClick={(e) => e.stopPropagation()}
						onDoubleClick={(e) => e.stopPropagation()}
					/>
				}
			>
				<MoreVertical className="size-4" />
			</DropdownMenuTrigger>
			<DropdownMenuContent align="end" className="min-w-48">
				<ActionsAsDropdownItems actions={actions} />
			</DropdownMenuContent>
		</DropdownMenu>
	);
}

// ── List ──────────────────────────────────────────────────────────────────────

function SortHeader({
	label,
	sortKey,
	sort,
	onSort,
	className,
}: {
	label: string;
	sortKey: FileSortKey;
	sort?: { key: FileSortKey; desc: boolean };
	onSort?: (key: FileSortKey) => void;
	className?: string;
}) {
	const active = sort?.key === sortKey;
	return (
		<button
			type="button"
			className={cn(
				"flex items-center gap-1 text-left text-xs font-medium text-muted-foreground hover:text-foreground",
				className,
			)}
			onClick={() => onSort?.(sortKey)}
			disabled={!onSort}
		>
			{label}
			{active &&
				(sort.desc ? (
					<ArrowDown className="size-3" />
				) : (
					<ArrowUp className="size-3" />
				))}
		</button>
	);
}

function ListRow({ node: n, ...p }: FileViewProps & { node: FileNode }) {
	const { i18n } = useTranslation("files");
	const dnd = useItemDnd(n, p);
	const actions = p.actionsFor(n);
	const isSelected = p.selected.has(n.id);
	return (
		<ContextMenu>
			<ContextMenuTrigger
				render={
					// biome-ignore lint/a11y/useSemanticElements: ARIA grid of CSS-grid rows
					<div
						role="row"
						aria-selected={isSelected}
						tabIndex={0}
						data-file-id={n.id}
						{...clickHandlers(n, p)}
						{...dnd.props}
						className={cn(
							"group grid cursor-default items-center gap-3 border-b px-3 py-1.5 text-sm outline-none select-none",
							p.showLocation
								? "grid-cols-[minmax(0,1fr)_8rem_8rem_6rem_2rem] max-md:grid-cols-[minmax(0,1fr)_2rem]"
								: "grid-cols-[minmax(0,1fr)_8rem_8rem_6rem_2rem] max-md:grid-cols-[minmax(0,1fr)_2rem]",
							isSelected
								? "bg-primary/10"
								: "hover:bg-muted/60 focus-visible:bg-muted/60",
							dnd.over && "bg-primary/20 ring-2 ring-primary ring-inset",
						)}
					/>
				}
			>
				<div className="flex min-w-0 items-center gap-3">
					<FileTypeIcon node={n} className="size-5" />
					<div className="min-w-0">
						<div className="flex items-center gap-1.5">
							<span className="truncate font-medium">{n.name}</span>
							{n.locked && (
								<Lock className="size-3 shrink-0 text-muted-foreground" />
							)}
							{n.starred && (
								<Star className="size-3 shrink-0 fill-amber-400 text-amber-400" />
							)}
						</div>
						{p.showLocation && p.locationOf && (
							<div className="truncate text-xs text-muted-foreground">
								{p.locationOf(n)}
							</div>
						)}
					</div>
				</div>
				<span className="truncate text-muted-foreground max-md:hidden">
					{n.owner_name ?? ""}
				</span>
				<span className="text-muted-foreground max-md:hidden">
					{formatDate(n.deleted_at ?? n.updated_at, i18n.language)}
				</span>
				<span className="text-muted-foreground max-md:hidden">
					{n.kind === "file" ? formatFileSize(n.size) : "—"}
				</span>
				<span className="justify-self-end opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 max-md:opacity-100">
					<ItemMenuButton actions={actions} />
				</span>
			</ContextMenuTrigger>
			{actions.length > 0 && (
				<ContextMenuContent className="min-w-48">
					<ActionsAsContextItems actions={actions} />
				</ContextMenuContent>
			)}
		</ContextMenu>
	);
}

export function FileListView(p: FileViewProps) {
	const { t } = useTranslation("files");
	return (
		// biome-ignore lint/a11y/useSemanticElements: ARIA grid of CSS-grid rows
		<div role="grid" aria-label={t("title")} className="min-w-0">
			{/* biome-ignore lint/a11y/useSemanticElements: ARIA grid of CSS-grid rows */}
			{/* biome-ignore lint/a11y/useFocusableInteractive: header row only labels columns */}
			<div
				role="row"
				className="sticky top-0 z-10 grid grid-cols-[minmax(0,1fr)_8rem_8rem_6rem_2rem] items-center gap-3 border-b bg-background px-3 py-2 max-md:hidden"
			>
				<SortHeader
					label={t("columns.name")}
					sortKey="name"
					sort={p.sort}
					onSort={p.onSort}
				/>
				<span className="text-xs font-medium text-muted-foreground">
					{t("columns.owner")}
				</span>
				<SortHeader
					label={p.inTrash ? t("columns.trashed") : t("columns.modified")}
					sortKey="modified"
					sort={p.sort}
					onSort={p.onSort}
				/>
				<SortHeader
					label={t("columns.size")}
					sortKey="size"
					sort={p.sort}
					onSort={p.onSort}
				/>
				<span />
			</div>
			{p.nodes.map((n) => (
				<ListRow key={n.id} node={n} {...p} />
			))}
		</div>
	);
}

// ── Grid ──────────────────────────────────────────────────────────────────────

function GridCard({ node: n, ...p }: FileViewProps & { node: FileNode }) {
	const { t } = useTranslation("files");
	const dnd = useItemDnd(n, p);
	const actions = p.actionsFor(n);
	const isSelected = p.selected.has(n.id);
	const isFolder = n.kind === "folder";
	return (
		<ContextMenu>
			<ContextMenuTrigger
				render={
					// biome-ignore lint/a11y/useSemanticElements: ARIA grid of cards
					<div
						role="gridcell"
						aria-selected={isSelected}
						tabIndex={0}
						data-file-id={n.id}
						{...clickHandlers(n, p)}
						{...dnd.props}
						className={cn(
							"group relative cursor-default rounded-xl border text-sm outline-none select-none",
							isFolder
								? "flex items-center gap-3 px-3 py-2.5"
								: "overflow-hidden",
							isSelected
								? "border-primary bg-primary/10"
								: "bg-card hover:bg-muted/60 focus-visible:ring-2 focus-visible:ring-ring",
							dnd.over && "border-primary bg-primary/20 ring-2 ring-primary",
						)}
					/>
				}
			>
				{isFolder ? (
					<>
						<FileTypeIcon node={n} className="size-5" />
						<span className="min-w-0 flex-1 truncate font-medium">
							{n.name}
						</span>
						{n.starred && (
							<Star className="size-3 shrink-0 fill-amber-400 text-amber-400" />
						)}
						<ItemMenuButton actions={actions} />
					</>
				) : (
					<>
						<div className="flex h-28 items-center justify-center bg-muted/40">
							<FileTypeIcon node={n} className="size-12" />
						</div>
						<div className="flex items-center gap-2 border-t px-3 py-2">
							<FileTypeIcon node={n} className="size-4" />
							<span className="min-w-0 flex-1 truncate font-medium">
								{n.name}
							</span>
							{n.starred && (
								<Star className="size-3 shrink-0 fill-amber-400 text-amber-400" />
							)}
							<ItemMenuButton actions={actions} />
						</div>
						<div className="px-3 pb-2 text-xs text-muted-foreground">
							{t(`kinds.${fileCategory(n)}`)} · {formatFileSize(n.size)}
						</div>
					</>
				)}
			</ContextMenuTrigger>
			{actions.length > 0 && (
				<ContextMenuContent className="min-w-48">
					<ActionsAsContextItems actions={actions} />
				</ContextMenuContent>
			)}
		</ContextMenu>
	);
}

export function FileGridView(p: FileViewProps) {
	const { t } = useTranslation("files");
	const folders = p.nodes.filter((n) => n.kind === "folder");
	const files = p.nodes.filter((n) => n.kind === "file");
	return (
		<div className="space-y-6 p-4">
			{folders.length > 0 && (
				<section>
					{files.length > 0 && (
						<h3 className="mb-2 text-xs font-medium text-muted-foreground">
							{t("sections.folders")}
						</h3>
					)}
					<div className="grid grid-cols-[repeat(auto-fill,minmax(13rem,1fr))] gap-3">
						{folders.map((n) => (
							<GridCard key={n.id} node={n} {...p} />
						))}
					</div>
				</section>
			)}
			{files.length > 0 && (
				<section>
					{folders.length > 0 && (
						<h3 className="mb-2 text-xs font-medium text-muted-foreground">
							{t("sections.files")}
						</h3>
					)}
					<div className="grid grid-cols-[repeat(auto-fill,minmax(13rem,1fr))] gap-3">
						{files.map((n) => (
							<GridCard key={n.id} node={n} {...p} />
						))}
					</div>
				</section>
			)}
		</div>
	);
}
