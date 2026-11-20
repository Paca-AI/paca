import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChevronRight, FolderInput } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
	AlertDialog,
	AlertDialogAction,
	AlertDialogCancel,
	AlertDialogContent,
	AlertDialogDescription,
	AlertDialogFooter,
	AlertDialogHeader,
	AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
	Sheet,
	SheetContent,
	SheetHeader,
	SheetTitle,
} from "@/components/ui/sheet";
import { Spinner } from "@/components/ui/spinner";
import {
	type FileNode,
	type FileScope,
	fileCategory,
	fileNodePathQueryOptions,
	fileNodesQueryOptions,
	fileRelationsQueryOptions,
	formatFileSize,
} from "@/lib/files-api";
import { FileTypeIcon } from "./file-icon";

// ── Name (new folder / rename) ────────────────────────────────────────────────

export function NameDialog({
	open,
	title,
	label,
	initial,
	submitLabel,
	pending,
	onSubmit,
	onClose,
}: {
	open: boolean;
	title: string;
	label: string;
	initial: string;
	submitLabel: string;
	pending: boolean;
	onSubmit: (name: string) => void;
	onClose: () => void;
}) {
	const { t } = useTranslation("files");
	const [name, setName] = useState(initial);
	useEffect(() => {
		if (open) setName(initial);
	}, [open, initial]);
	const trimmed = name.trim();
	return (
		<Dialog open={open} onOpenChange={(o) => !o && onClose()}>
			<DialogContent className="sm:max-w-md">
				<form
					onSubmit={(e) => {
						e.preventDefault();
						if (trimmed) onSubmit(trimmed);
					}}
					className="space-y-4"
				>
					<DialogHeader>
						<DialogTitle>{title}</DialogTitle>
					</DialogHeader>
					<div className="space-y-2">
						<Label htmlFor="file-name-input">{label}</Label>
						<Input
							id="file-name-input"
							value={name}
							autoFocus
							maxLength={255}
							onChange={(e) => setName(e.target.value)}
							onFocus={(e) => {
								// Select the name without its extension, like Drive.
								const dot = e.target.value.lastIndexOf(".");
								e.target.setSelectionRange(
									0,
									dot > 0 ? dot : e.target.value.length,
								);
							}}
						/>
					</div>
					<DialogFooter>
						<Button type="button" variant="outline" onClick={onClose}>
							{t("dialogs.cancel")}
						</Button>
						<Button type="submit" disabled={!trimmed || pending}>
							{pending && <Spinner className="size-4" />}
							{submitLabel}
						</Button>
					</DialogFooter>
				</form>
			</DialogContent>
		</Dialog>
	);
}

// ── Move ──────────────────────────────────────────────────────────────────────

/** Folder picker: browse folders of one scope and pick the destination. */
export function MoveDialog({
	open,
	scope,
	scopeName,
	nodes,
	startFolderId,
	pending,
	onMove,
	onClose,
}: {
	open: boolean;
	scope: FileScope;
	scopeName: string;
	nodes: FileNode[];
	startFolderId: string | null;
	pending: boolean;
	onMove: (parentId: string | null) => void;
	onClose: () => void;
}) {
	const { t } = useTranslation("files");
	const [folderId, setFolderId] = useState<string | null>(startFolderId);
	useEffect(() => {
		if (open) setFolderId(startFolderId);
	}, [open, startFolderId]);

	const children = useFolderChildren(scope, folderId, open);
	const path = useQuery({
		...fileNodePathQueryOptions(scope, folderId ?? ""),
		enabled: open && !!folderId,
	});

	const movingIds = new Set(nodes.map((n) => n.id));
	// The default folders take nothing in; a folder cannot go into itself.
	const here = path.data?.at(-1);
	const blocked =
		(here && (here.system || here.locked) && here.area !== "docs") ||
		(path.data ?? []).some((p) => movingIds.has(p.id));
	const unchanged = nodes.every((n) => (n.parent_id ?? null) === folderId);
	const folders = (children.data ?? []).filter(
		(n) =>
			n.kind === "folder" &&
			!movingIds.has(n.id) &&
			(!n.system || n.area === "docs"),
	);

	return (
		<Dialog open={open} onOpenChange={(o) => !o && onClose()}>
			<DialogContent className="sm:max-w-md">
				<DialogHeader>
					<DialogTitle>
						{nodes.length === 1
							? t("dialogs.move.title", { name: nodes[0].name })
							: t("dialogs.move.titleMany", { count: nodes.length })}
					</DialogTitle>
				</DialogHeader>
				<nav
					aria-label={t("columns.location")}
					className="flex flex-wrap items-center gap-1 text-sm"
				>
					<button
						type="button"
						className="rounded px-1 hover:bg-muted"
						onClick={() => setFolderId(null)}
					>
						{scopeName}
					</button>
					{(path.data ?? []).map((p) => (
						<span key={p.id} className="flex items-center gap-1">
							<ChevronRight className="size-3 text-muted-foreground" />
							<button
								type="button"
								className="rounded px-1 hover:bg-muted"
								onClick={() => setFolderId(p.id)}
							>
								{p.name}
							</button>
						</span>
					))}
				</nav>
				<ScrollArea className="h-64 rounded-md border">
					{children.isLoading ? (
						<div className="flex h-full items-center justify-center p-8">
							<Spinner />
						</div>
					) : folders.length === 0 ? (
						<p className="p-6 text-center text-sm text-muted-foreground">
							{t("dialogs.move.noFolders")}
						</p>
					) : (
						<ul>
							{folders.map((f) => (
								<li key={f.id}>
									<button
										type="button"
										className="flex w-full items-center gap-3 px-3 py-2 text-left text-sm hover:bg-muted"
										onClick={() => setFolderId(f.id)}
									>
										<FileTypeIcon node={f} className="size-5" />
										<span className="truncate">{f.name}</span>
									</button>
								</li>
							))}
						</ul>
					)}
				</ScrollArea>
				<DialogFooter>
					<Button variant="outline" onClick={onClose}>
						{t("dialogs.cancel")}
					</Button>
					<Button
						disabled={!!blocked || unchanged || pending}
						onClick={() => onMove(folderId)}
					>
						{pending ? (
							<Spinner className="size-4" />
						) : (
							<FolderInput className="size-4" />
						)}
						{t("dialogs.move.here")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

function useFolderChildren(
	scope: FileScope,
	folderId: string | null,
	enabled: boolean,
) {
	const q = useInfiniteQuery({
		...fileNodesQueryOptions(scope, { parentId: folderId, sort: "name" }),
		enabled,
	});
	return {
		data: q.data?.pages.flatMap((p) => p.items),
		isLoading: q.isLoading,
	};
}

// ── Confirm ───────────────────────────────────────────────────────────────────

export function ConfirmDialog({
	open,
	title,
	body,
	confirmLabel,
	pending,
	onConfirm,
	onClose,
}: {
	open: boolean;
	title: string;
	body: string;
	confirmLabel: string;
	pending: boolean;
	onConfirm: () => void;
	onClose: () => void;
}) {
	const { t } = useTranslation("files");
	return (
		<AlertDialog open={open} onOpenChange={(o) => !o && onClose()}>
			<AlertDialogContent>
				<AlertDialogHeader>
					<AlertDialogTitle>{title}</AlertDialogTitle>
					<AlertDialogDescription>{body}</AlertDialogDescription>
				</AlertDialogHeader>
				<AlertDialogFooter>
					<AlertDialogCancel>{t("dialogs.cancel")}</AlertDialogCancel>
					<AlertDialogAction
						variant="destructive"
						disabled={pending}
						onClick={onConfirm}
					>
						{pending && <Spinner className="size-4" />}
						{confirmLabel}
					</AlertDialogAction>
				</AlertDialogFooter>
			</AlertDialogContent>
		</AlertDialog>
	);
}

// ── Details ───────────────────────────────────────────────────────────────────

export function DetailsSheet({
	node,
	scope,
	scopeName,
	onClose,
}: {
	node: FileNode | null;
	scope: FileScope;
	scopeName: string;
	onClose: () => void;
}) {
	const { t, i18n } = useTranslation("files");
	const path = useQuery({
		...fileNodePathQueryOptions(scope, node?.id ?? ""),
		enabled: !!node,
	});
	const relations = useQuery({
		...fileRelationsQueryOptions(scope, node?.id ?? ""),
		enabled: node?.kind === "file" && !!scope.projectId,
	});
	const date = (iso: string) =>
		new Intl.DateTimeFormat(i18n.language, {
			dateStyle: "medium",
			timeStyle: "short",
		}).format(new Date(iso));
	const location = [
		scopeName,
		...(path.data ?? []).slice(0, -1).map((p) => p.name),
	];
	const rows: [string, string][] = node
		? [
				[t("details.type"), t(`kinds.${fileCategory(node)}`)],
				...(node.kind === "file"
					? ([[t("details.size"), formatFileSize(node.size)]] as [
							string,
							string,
						][])
					: []),
				[t("details.owner"), node.owner_name || t("details.unknownOwner")],
				[t("details.created"), date(node.created_at)],
				[t("details.modified"), date(node.updated_at)],
				[t("details.location"), location.join(" › ")],
				...(node.source
					? ([
							[t("details.source"), t(`source.${node.source as "upload"}`)],
						] as [string, string][])
					: []),
			]
		: [];
	return (
		<Sheet open={!!node} onOpenChange={(o) => !o && onClose()}>
			<SheetContent side="right" className="w-full sm:max-w-sm">
				<SheetHeader>
					<SheetTitle>{t("details.title")}</SheetTitle>
				</SheetHeader>
				{node && (
					<div className="space-y-4 px-4 pb-4">
						<div className="flex items-center gap-3">
							<FileTypeIcon node={node} className="size-8" />
							<span className="min-w-0 break-words font-medium">
								{node.name}
							</span>
						</div>
						{node.system && (
							<p className="rounded-md bg-muted p-3 text-xs text-muted-foreground">
								{t(`system.${node.system as "docs"}`)}
							</p>
						)}
						<dl className="space-y-3 text-sm">
							{rows.map(([k, v]) => (
								<div key={k}>
									<dt className="text-xs text-muted-foreground">{k}</dt>
									<dd className="break-words">{v}</dd>
								</div>
							))}
						</dl>
						{scope.projectId && relations.data && (
							<dl className="space-y-3 border-t pt-3 text-sm">
								<div>
									<dt className="text-xs text-muted-foreground">
										{t("docFolder.task")}
									</dt>
									<dd className="break-words">
										{relations.data.task ? (
											<Link
												to="/projects/$projectId/tasks/$taskId"
												params={{
													projectId: scope.projectId,
													taskId: relations.data.task.id,
												}}
												className="text-primary hover:underline"
											>
												{relations.data.task.key} · {relations.data.task.title}
											</Link>
										) : (
											"—"
										)}
									</dd>
								</div>
								<div>
									<dt className="text-xs text-muted-foreground">
										{t("docFolder.documents")}
									</dt>
									<dd className="space-y-0.5 break-words">
										{relations.data.documents.length > 0
											? relations.data.documents.map((d) => (
													<div key={d.id}>
														<Link
															to="/projects/$projectId/docs/$docId"
															params={{
																projectId: scope.projectId as string,
																docId: d.id,
															}}
															className="text-primary hover:underline"
														>
															{d.title}
														</Link>
													</div>
												))
											: "—"}
									</dd>
								</div>
							</dl>
						)}
					</div>
				)}
			</SheetContent>
		</Sheet>
	);
}
