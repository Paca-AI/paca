import type { TFunction } from "i18next";
import {
	Download,
	Eye,
	FolderInput,
	FolderOpen,
	Info,
	Pencil,
	RotateCcw,
	Star,
	StarOff,
	Trash2,
} from "lucide-react";
import { Fragment, type ReactNode } from "react";
import {
	ContextMenuItem,
	ContextMenuSeparator,
} from "@/components/ui/context-menu";
import {
	DropdownMenuItem,
	DropdownMenuSeparator,
} from "@/components/ui/dropdown-menu";
import { deletesForGood, type FileNode } from "@/lib/files-api";

export interface FileActionHandlers {
	open: (n: FileNode) => void;
	download: (n: FileNode) => void;
	toggleStar: (n: FileNode) => void;
	rename: (n: FileNode) => void;
	move: (n: FileNode) => void;
	details: (n: FileNode) => void;
	trash: (n: FileNode) => void;
	restore: (n: FileNode) => void;
	deleteForever: (n: FileNode) => void;
}

export interface FileAction {
	key: string;
	label: string;
	icon: ReactNode;
	run: () => void;
	destructive?: boolean;
	separatorBefore?: boolean;
}

/**
 * The actions of one item, in menu order. `canWrite` gates everything that
 * changes files; managed (locked) items cannot be renamed or moved, and the
 * default folders cannot be deleted at all.
 */
export function buildFileActions(
	n: FileNode,
	opts: { t: TFunction<"files">; inTrash: boolean; canWrite: boolean },
	h: FileActionHandlers,
): FileAction[] {
	const { t, inTrash, canWrite } = opts;
	const icon = "size-4";
	if (inTrash) {
		return canWrite
			? [
					{
						key: "restore",
						label: t("actions.restore"),
						icon: <RotateCcw className={icon} />,
						run: () => h.restore(n),
					},
					{
						key: "deleteForever",
						label: t("actions.deleteForever"),
						icon: <Trash2 className={icon} />,
						run: () => h.deleteForever(n),
						destructive: true,
						separatorBefore: true,
					},
				]
			: [];
	}
	const isFolder = n.kind === "folder";
	const actions: FileAction[] = [
		{
			key: "open",
			label: isFolder ? t("actions.open") : t("actions.preview"),
			icon: isFolder ? (
				<FolderOpen className={icon} />
			) : (
				<Eye className={icon} />
			),
			run: () => h.open(n),
		},
	];
	if (!isFolder) {
		actions.push({
			key: "download",
			label: t("actions.download"),
			icon: <Download className={icon} />,
			run: () => h.download(n),
		});
	}
	actions.push({
		key: "star",
		label: n.starred ? t("actions.unstar") : t("actions.star"),
		icon: n.starred ? <StarOff className={icon} /> : <Star className={icon} />,
		run: () => h.toggleStar(n),
		separatorBefore: true,
	});
	if (canWrite && !n.locked) {
		actions.push(
			{
				key: "rename",
				label: t("actions.rename"),
				icon: <Pencil className={icon} />,
				run: () => h.rename(n),
				separatorBefore: true,
			},
			{
				key: "move",
				label: t("actions.move"),
				icon: <FolderInput className={icon} />,
				run: () => h.move(n),
			},
		);
	}
	actions.push({
		key: "details",
		label: t("actions.details"),
		icon: <Info className={icon} />,
		run: () => h.details(n),
		separatorBefore: true,
	});
	// The default folders cannot go; files inside them can (the files that tasks,
	// documents and annotations use are deleted for good, anything else goes to
	// the trash).
	if (canWrite && !n.system) {
		actions.push({
			key: "trash",
			label: deletesForGood(n) ? t("actions.delete") : t("actions.trash"),
			icon: <Trash2 className={icon} />,
			run: () => h.trash(n),
			destructive: true,
			separatorBefore: true,
		});
	}
	return actions;
}

export function ActionsAsDropdownItems({ actions }: { actions: FileAction[] }) {
	return (
		<>
			{actions.map((a) => (
				<Fragment key={a.key}>
					{a.separatorBefore && <DropdownMenuSeparator />}
					<DropdownMenuItem
						variant={a.destructive ? "destructive" : undefined}
						onClick={a.run}
					>
						{a.icon}
						{a.label}
					</DropdownMenuItem>
				</Fragment>
			))}
		</>
	);
}

export function ActionsAsContextItems({ actions }: { actions: FileAction[] }) {
	return (
		<>
			{actions.map((a) => (
				<Fragment key={a.key}>
					{a.separatorBefore && <ContextMenuSeparator />}
					<ContextMenuItem
						variant={a.destructive ? "destructive" : undefined}
						onClick={a.run}
					>
						{a.icon}
						{a.label}
					</ContextMenuItem>
				</Fragment>
			))}
		</>
	);
}
