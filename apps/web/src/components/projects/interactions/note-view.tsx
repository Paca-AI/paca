import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { NotebookPen } from "lucide-react";
import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { DocumentPane } from "@/components/projects/docs/document-pane";
import { NoPermissionState } from "@/components/shared/no-permission-state";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import {
	createDocument,
	type Document,
	docQueryKeys,
	viewNotesQueryOptions,
} from "@/lib/doc-api";

interface NoteViewProps {
	projectId: string;
	viewId: string;
	/** Where the document's save status and comments and activity toggle go:
	 *  the view header, next to where other views keep their settings. */
	headerSlot: HTMLElement | null;
}

/**
 * A note view: one editable document that belongs to the view (sprint goals,
 * meeting minutes, a checklist), opened in the same editor and comments and
 * activity pane as the project's docs, with no title and no list of notes. Its
 * save status and activity toggle sit in the view header.
 * The document is created the first time someone who can write opens the
 * view. Reading it needs docs:read and changing it docs:write.
 */
export function NoteView({ projectId, viewId, headerSlot }: NoteViewProps) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const { hasProjectPermission, isLoading: permissionsLoading } =
		useProjectPermissions(projectId);
	const canRead = hasProjectPermission("docs:read");
	const canWrite = hasProjectPermission("docs:write");

	const docKey = docQueryKeys.viewNotes(projectId, viewId);
	const docQuery = useQuery({
		...viewNotesQueryOptions(projectId, viewId),
		enabled: canRead,
	});
	// A view holds one document; extras from before migration 000095 stay
	// stored but are not shown.
	const doc = docQuery.data?.[0];

	const createMutation = useMutation({
		mutationFn: () =>
			createDocument(projectId, {
				title: t("layout.noteView.untitled"),
				view_id: viewId,
			}),
		onSuccess: (created) => {
			qc.setQueryData<Document[]>(docKey, (prev) =>
				prev?.length ? prev : [created],
			);
			qc.invalidateQueries({ queryKey: docKey });
		},
		// Someone else created the view's document first: show theirs.
		onError: (err: unknown) => {
			if (getApiErrorCode(err) === ApiErrorCode.DocNoteViewTaken) {
				qc.invalidateQueries({ queryKey: docKey });
			}
		},
	});

	// Create the document on first open, once per view: a failed attempt is
	// retried from the button below, not in a loop.
	const attemptedRef = useRef<string | null>(null);
	const empty = docQuery.isSuccess && !doc;
	useEffect(() => {
		if (!empty || !canWrite || attemptedRef.current === viewId) return;
		attemptedRef.current = viewId;
		createMutation.mutate();
	}, [empty, canWrite, viewId, createMutation]);

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

	if (doc) {
		return (
			<div className="flex flex-1 min-h-0 min-w-0 flex-col overflow-hidden">
				<DocumentPane
					key={doc.id}
					projectId={projectId}
					docId={doc.id}
					headerSlot={headerSlot}
				/>
			</div>
		);
	}

	// A refused creation because the document exists is not an error: the
	// refetch above is about to show it.
	const createFailed =
		createMutation.isError &&
		getApiErrorCode(createMutation.error) !== ApiErrorCode.DocNoteViewTaken;
	if (docQuery.isError || createFailed) {
		return (
			<div className="flex flex-1 flex-col items-center justify-center gap-3 p-6 text-center">
				<p role="alert" className="text-sm text-destructive">
					{docQuery.isError
						? t("layout.noteView.errors.loadFailed")
						: t("layout.noteView.errors.createFailed")}
				</p>
				<Button
					size="sm"
					variant="outline"
					onClick={() =>
						docQuery.isError ? docQuery.refetch() : createMutation.mutate()
					}
				>
					{t("layout.noteView.retry")}
				</Button>
			</div>
		);
	}

	if (empty && !canWrite) {
		return (
			<div className="flex flex-1 flex-col items-center justify-center gap-3 p-6 text-center text-muted-foreground">
				<div className="flex size-14 items-center justify-center rounded-xl bg-muted/50">
					<NotebookPen className="size-7 text-muted-foreground/90" />
				</div>
				<p className="max-w-sm text-sm">
					{t("layout.noteView.empty.readOnly")}
				</p>
			</div>
		);
	}

	// Loading the document, or creating it.
	return (
		<div
			className="flex flex-1 flex-col gap-3 p-8"
			aria-busy="true"
			aria-live="polite"
		>
			<Skeleton className="h-4 w-full" />
			<Skeleton className="h-4 w-11/12" />
			<Skeleton className="h-4 w-5/6" />
			{createMutation.isPending && <Spinner aria-hidden="true" />}
		</div>
	);
}
