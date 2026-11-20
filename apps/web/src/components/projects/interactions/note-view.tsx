import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
	ArrowDown,
	ArrowUp,
	FileText,
	MoreHorizontal,
	NotebookPen,
	Pencil,
	Plus,
	Trash2,
} from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { DocumentPane } from "@/components/projects/docs/document-pane";
import { NoPermissionState } from "@/components/shared/no-permission-state";
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
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import {
	createDocument,
	type Document,
	deleteDocument,
	docQueryKeys,
	updateDocument,
	viewNotesQueryOptions,
} from "@/lib/doc-api";
import { notifyError } from "@/lib/notify";
import { cn } from "@/lib/utils";

interface NoteViewProps {
	projectId: string;
	viewId: string;
}

/** The new order of `notes` after moving the note at `index` one step. */
export function moveNote(
	notes: Document[],
	index: number,
	direction: -1 | 1,
): Document[] {
	const target = index + direction;
	if (index < 0 || target < 0 || target >= notes.length) return notes;
	const next = [...notes];
	[next[index], next[target]] = [next[target], next[index]];
	return next;
}

const activeNoteKey = (viewId: string) => `paca:note-view:${viewId}:active`;

/**
 * A note view: the documents that belong to one view (sprint goals, meeting
 * minutes, a checklist), listed on the left and opened on the right in the
 * same editor as the project's docs. Notes are documents, so reading them
 * needs docs:read and changing them docs:write.
 */
export function NoteView({ projectId, viewId }: NoteViewProps) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const { hasProjectPermission, isLoading: permissionsLoading } =
		useProjectPermissions(projectId);
	const canRead = hasProjectPermission("docs:read");
	const canWrite = hasProjectPermission("docs:write");

	const notesKey = docQueryKeys.viewNotes(projectId, viewId);
	const notesQuery = useQuery({
		...viewNotesQueryOptions(projectId, viewId),
		enabled: canRead,
	});
	const notes = notesQuery.data ?? [];

	const [selectedId, setSelectedId] = useState<string | null>(() => {
		try {
			return localStorage.getItem(activeNoteKey(viewId));
		} catch {
			return null;
		}
	});
	const selected = notes.find((n) => n.id === selectedId) ?? notes[0];
	const select = useCallback(
		(id: string) => {
			setSelectedId(id);
			try {
				localStorage.setItem(activeNoteKey(viewId), id);
			} catch {
				/* ignore */
			}
		},
		[viewId],
	);

	const [renamingId, setRenamingId] = useState<string | null>(null);
	const [renameError, setRenameError] = useState<string | null>(null);
	// Bumped on every failed rename, so the title field takes another try.
	const [renameAttempt, setRenameAttempt] = useState(0);
	const [deleteTarget, setDeleteTarget] = useState<Document | null>(null);

	const createMutation = useMutation({
		meta: { success: "created", subject: "note" },
		mutationFn: () =>
			createDocument(projectId, {
				title: t("layout.noteView.untitled"),
				view_id: viewId,
			}),
		onSuccess: (note) => {
			qc.setQueryData<Document[]>(notesKey, (prev) =>
				prev ? [...prev, note] : [note],
			);
			qc.invalidateQueries({ queryKey: notesKey });
			select(note.id);
			setRenameError(null);
			setRenamingId(note.id);
		},
	});

	const renameMutation = useMutation({
		meta: { success: "renamed", subject: "note" },
		mutationFn: (vars: { id: string; title: string }) =>
			updateDocument(projectId, vars.id, { title: vars.title }),
		onSuccess: (updated) => {
			setRenamingId(null);
			setRenameError(null);
			qc.setQueryData(docQueryKeys.detail(projectId, updated.id), updated);
			qc.invalidateQueries({ queryKey: notesKey });
		},
		onError: (err: unknown) => {
			setRenameError(
				getApiErrorCode(err) === ApiErrorCode.DocTitleInvalid
					? t("layout.noteView.errors.titleInvalid")
					: t("layout.noteView.errors.renameFailed"),
			);
			setRenameAttempt((n) => n + 1);
		},
	});

	const deleteMutation = useMutation({
		meta: { success: "deleted", subject: "note" },
		mutationFn: (id: string) => deleteDocument(projectId, id),
		onSuccess: (_, id) => {
			setDeleteTarget(null);
			qc.setQueryData<Document[]>(notesKey, (prev) =>
				prev?.filter((n) => n.id !== id),
			);
			qc.invalidateQueries({ queryKey: notesKey });
		},
	});

	// Reordering renumbers every note 0..n-1 in its new order and saves the
	// positions that changed (new notes all start at 0, so a swap of two
	// positions would not be enough). The list shows the new order, with the
	// new positions, right away, so a second move compares against them.
	const reorderMutation = useMutation({
		mutationFn: async (ordered: Document[]) => {
			await Promise.all(
				ordered.map((n, position) =>
					n.position === position
						? null
						: updateDocument(projectId, n.id, { position }),
				),
			);
		},
		onMutate: async (ordered) => {
			await qc.cancelQueries({ queryKey: notesKey });
			const previous = qc.getQueryData<Document[]>(notesKey);
			qc.setQueryData(
				notesKey,
				ordered.map((n, position) => ({ ...n, position })),
			);
			return { previous };
		},
		onError: (err, _ordered, context) => {
			if (context?.previous) qc.setQueryData(notesKey, context.previous);
			notifyError(err, t("layout.noteView.errors.reorderFailed"));
		},
		onSettled: () => qc.invalidateQueries({ queryKey: notesKey }),
	});

	if (!permissionsLoading && !canRead) {
		return (
			<div className="flex flex-1 items-center justify-center p-6">
				<NoPermissionState
					title={t("layout.noteView.noPermission.title")}
					description={t("layout.noteView.noPermission.description")}
				/>
			</div>
		);
	}

	const newNoteButton = canWrite && (
		<Button
			size="sm"
			variant="ghost"
			className="h-7 gap-1 px-2 text-xs"
			onClick={() => createMutation.mutate()}
			disabled={createMutation.isPending}
		>
			{createMutation.isPending ? (
				<Spinner aria-hidden="true" />
			) : (
				<Plus className="size-3.5" />
			)}
			{t("layout.noteView.newNote")}
		</Button>
	);

	return (
		<div className="flex flex-1 min-h-0 overflow-hidden">
			<aside
				aria-label={t("layout.noteView.title")}
				className="flex w-56 shrink-0 flex-col border-r border-border/50 bg-muted/10 sm:w-64"
			>
				<div className="flex items-center justify-between gap-2 border-b border-border/50 px-3 py-2">
					<p className="text-xs font-semibold uppercase tracking-[0.08em] text-muted-foreground">
						{t("layout.noteView.title")}
					</p>
					{newNoteButton}
				</div>
				<div className="flex-1 overflow-y-auto p-1.5">
					{notesQuery.isPending ? (
						<div className="space-y-2 p-1.5">
							<Skeleton className="h-5 w-full" />
							<Skeleton className="h-5 w-4/5" />
							<Skeleton className="h-5 w-3/5" />
						</div>
					) : notesQuery.isError ? (
						<p className="px-2 py-1.5 text-xs text-destructive">
							{t("layout.noteView.errors.loadFailed")}
						</p>
					) : (
						<ul className="space-y-0.5">
							{notes.map((note, index) => (
								<NoteListItem
									key={note.id}
									note={note}
									active={note.id === selected?.id}
									canWrite={canWrite}
									renaming={renamingId === note.id}
									renameError={renamingId === note.id ? renameError : null}
									renameAttempt={renameAttempt}
									isFirst={index === 0}
									isLast={index === notes.length - 1}
									onSelect={() => select(note.id)}
									onStartRename={() => {
										setRenameError(null);
										setRenamingId(note.id);
									}}
									onRename={(title) =>
										renameMutation.mutate({ id: note.id, title })
									}
									onCancelRename={() => {
										setRenamingId(null);
										setRenameError(null);
									}}
									onMove={(direction) =>
										reorderMutation.mutate(moveNote(notes, index, direction))
									}
									onDelete={() => setDeleteTarget(note)}
								/>
							))}
						</ul>
					)}
				</div>
			</aside>

			<div className="flex flex-1 min-w-0 flex-col">
				{selected ? (
					<DocumentPane
						key={selected.id}
						projectId={projectId}
						docId={selected.id}
					/>
				) : notesQuery.isSuccess ? (
					<div className="flex flex-1 flex-col items-center justify-center gap-3 p-6 text-center text-muted-foreground">
						<div className="flex size-14 items-center justify-center rounded-xl bg-muted/50">
							<NotebookPen className="size-7 text-muted-foreground/90" />
						</div>
						<div>
							<p className="text-base font-semibold text-foreground/80">
								{t("layout.noteView.empty.title")}
							</p>
							<p className="mt-1.5 max-w-sm text-sm">
								{canWrite
									? t("layout.noteView.empty.description")
									: t("layout.noteView.empty.readOnly")}
							</p>
						</div>
						{newNoteButton}
					</div>
				) : null}
			</div>

			<Dialog
				open={!!deleteTarget}
				onOpenChange={(open) => {
					if (!open) setDeleteTarget(null);
				}}
			>
				<DialogContent className="max-w-sm">
					<DialogHeader>
						<DialogTitle>{t("layout.noteView.deleteDialog.title")}</DialogTitle>
						<DialogDescription>
							{t("layout.noteView.deleteDialog.description", {
								title: deleteTarget?.title ?? "",
							})}
						</DialogDescription>
					</DialogHeader>
					<DialogFooter>
						<Button
							variant="outline"
							onClick={() => setDeleteTarget(null)}
							disabled={deleteMutation.isPending}
						>
							{t("layout.noteView.deleteDialog.cancel")}
						</Button>
						<Button
							variant="destructive"
							onClick={() =>
								deleteTarget && deleteMutation.mutate(deleteTarget.id)
							}
							disabled={deleteMutation.isPending}
						>
							{deleteMutation.isPending ? (
								<Spinner aria-hidden="true" />
							) : (
								t("layout.noteView.deleteDialog.delete")
							)}
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</div>
	);
}

interface NoteListItemProps {
	note: Document;
	active: boolean;
	canWrite: boolean;
	renaming: boolean;
	renameError: string | null;
	renameAttempt: number;
	isFirst: boolean;
	isLast: boolean;
	onSelect: () => void;
	onStartRename: () => void;
	onRename: (title: string) => void;
	onCancelRename: () => void;
	onMove: (direction: -1 | 1) => void;
	onDelete: () => void;
}

function NoteListItem({
	note,
	active,
	canWrite,
	renaming,
	renameError,
	renameAttempt,
	isFirst,
	isLast,
	onSelect,
	onStartRename,
	onRename,
	onCancelRename,
	onMove,
	onDelete,
}: NoteListItemProps) {
	const { t } = useTranslation("projects");
	const title = note.title || t("layout.noteView.untitled");
	// A locked managed copy's title comes from its repository.
	const canRename = canWrite && !note.source_locked;

	return (
		<li>
			<div className="group flex items-center gap-1">
				{renaming ? (
					<div className="flex flex-1 min-w-0 items-center gap-1.5 px-2 py-1">
						<FileText className="size-3.5 shrink-0 text-primary/70" />
						<NoteTitleInput
							initialValue={title}
							label={t("layout.noteView.titleLabel")}
							error={renameError}
							attempt={renameAttempt}
							onConfirm={onRename}
							onCancel={onCancelRename}
						/>
					</div>
				) : (
					<button
						type="button"
						onClick={onSelect}
						aria-current={active ? "true" : undefined}
						className={cn(
							"flex flex-1 min-w-0 items-center gap-1.5 rounded-md px-2 py-1 text-left text-sm transition-all duration-150",
							active
								? "bg-primary/10 font-medium text-primary"
								: "text-foreground/80 hover:bg-muted/60 hover:text-foreground",
						)}
					>
						<FileText
							className={cn(
								"size-3.5 shrink-0",
								active ? "text-primary/70" : "text-muted-foreground",
							)}
						/>
						<span className="truncate">{title}</span>
					</button>
				)}
				{canWrite && !renaming && (
					<DropdownMenu>
						<DropdownMenuTrigger
							aria-label={t("layout.noteView.actions", { title })}
							className="flex size-6 shrink-0 items-center justify-center rounded-md text-muted-foreground opacity-0 transition-all duration-150 hover:bg-muted/60 hover:text-foreground focus-visible:opacity-100 group-hover:opacity-100 data-popup-open:opacity-100"
						>
							<MoreHorizontal className="size-3.5" />
						</DropdownMenuTrigger>
						<DropdownMenuContent align="start" className="w-40">
							<DropdownMenuItem disabled={!canRename} onClick={onStartRename}>
								<Pencil className="mr-2 size-3.5" />
								{t("layout.noteView.rename")}
							</DropdownMenuItem>
							<DropdownMenuItem disabled={isFirst} onClick={() => onMove(-1)}>
								<ArrowUp className="mr-2 size-3.5" />
								{t("layout.noteView.moveUp")}
							</DropdownMenuItem>
							<DropdownMenuItem disabled={isLast} onClick={() => onMove(1)}>
								<ArrowDown className="mr-2 size-3.5" />
								{t("layout.noteView.moveDown")}
							</DropdownMenuItem>
							<DropdownMenuSeparator />
							<DropdownMenuItem
								className="text-destructive focus:text-destructive"
								onClick={onDelete}
							>
								<Trash2 className="mr-2 size-3.5" />
								{t("layout.noteView.delete")}
							</DropdownMenuItem>
						</DropdownMenuContent>
					</DropdownMenu>
				)}
			</div>
			{renameError && (
				<p role="alert" className="px-2 py-0.5 text-xs text-destructive">
					{renameError}
				</p>
			)}
		</li>
	);
}

/** The inline title editor: Enter or leaving the field saves, Escape cancels.
 *  A failed save (error) keeps what was typed and lets it be saved again. */
function NoteTitleInput({
	initialValue,
	label,
	error,
	attempt,
	onConfirm,
	onCancel,
}: {
	initialValue: string;
	label: string;
	error: string | null;
	attempt: number;
	onConfirm: (title: string) => void;
	onCancel: () => void;
}) {
	const [value, setValue] = useState(initialValue);
	// Enter is followed by a blur: save once per attempt.
	const doneRef = useRef(false);
	const inputRef = useRef<HTMLInputElement>(null);
	// biome-ignore lint/correctness/useExhaustiveDependencies: each failed attempt reopens the field for the next one
	useEffect(() => {
		doneRef.current = false;
		inputRef.current?.focus();
		inputRef.current?.select();
	}, [attempt]);

	const confirm = () => {
		if (doneRef.current) return;
		doneRef.current = true;
		const trimmed = value.trim();
		if (trimmed && trimmed !== initialValue) onConfirm(trimmed);
		else onCancel();
	};

	return (
		<Input
			ref={inputRef}
			aria-label={label}
			value={value}
			aria-invalid={error ? true : undefined}
			className="h-6 min-w-0 flex-1 rounded border border-primary/30 px-1.5 text-sm"
			onChange={(e) => setValue(e.target.value)}
			onKeyDown={(e) => {
				if (e.key === "Enter") confirm();
				else if (e.key === "Escape") {
					doneRef.current = true;
					onCancel();
				}
			}}
			onBlur={confirm}
		/>
	);
}
