import {
	useInfiniteQuery,
	useMutation,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { AlertCircle, CheckCircle2, Pencil, Play, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { InteractionLayout } from "@/components/projects/interactions/interaction-layout";
import { SprintFormModal } from "@/components/projects/interactions/sprint-form-modal";
import { SprintStatusBadge } from "@/components/projects/interactions/sprint-status-badge";
import { StartSprintModal } from "@/components/projects/interactions/start-sprint-modal";
import { ReportIssueButton } from "@/components/report-issue-button";
import { InlineNotice } from "@/components/shared/inline-notice";
import { LoadMoreSentinel } from "@/components/shared/load-more-sentinel";
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
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import { formatDate } from "@/lib/format-date";
import {
	completeSprint,
	deleteSprint,
	listSprints,
	sprintQueryOptions,
	sprintsInfiniteQueryOptions,
	sprintTaskCountQueryOptions,
	type UpdateSprintPayload,
	updateSprint,
} from "@/lib/interaction-api";
import { taskStatusesQueryOptions } from "@/lib/project-api";
import { cn } from "@/lib/utils";

/** RadioGroup value standing for "no sprint" (the product backlog). */
const MOVE_TO_BACKLOG = "__backlog__";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/interactions/sprints/$sprintId",
)({
	loader: async ({
		context: { queryClient },
		params: { projectId, sprintId },
	}) => {
		const user = queryClient.getQueryData(["auth", "me-optional"]);
		await queryClient
			.ensureQueryData(sprintQueryOptions(projectId, sprintId))
			.catch(() => {
				throw redirect(
					user
						? {
								to: "/projects/$projectId/interactions/backlog",
								params: { projectId },
							}
						: { to: "/" },
				);
			});
	},
	component: SprintPage,
});

function SprintPage() {
	const { t } = useTranslation("projects");
	const { projectId, sprintId } = Route.useParams();
	const { hasProjectPermission } = useProjectPermissions(projectId);
	const qc = useQueryClient();
	const navigate = useNavigate();

	const {
		data: sprint,
		isError,
		error: sprintError,
	} = useQuery(sprintQueryOptions(projectId, sprintId));
	const { data: taskStatuses = [] } = useQuery(
		taskStatusesQueryOptions(projectId),
	);

	const canCreate = hasProjectPermission("tasks:write");
	const canEdit = hasProjectPermission("tasks:write");
	// views.* was split out from sprints.write as its own permission (see
	// authz.PermissionViewsWrite's doc comment) — projects.write governs
	// only the project entity itself (name/description), an unrelated
	// permission that happened to be reused here.
	const canManageViews = hasProjectPermission("views:write");
	const canManageSprints = hasProjectPermission("sprints:write");

	const [completeOpen, setCompleteOpen] = useState(false);
	const [completeSprintError, setCompleteSprintError] = useState<string | null>(
		null,
	);
	const [moveToSprintId, setMoveToSprintId] = useState<string | null>(null);

	const [startSprintOpen, setStartSprintOpen] = useState(false);
	const [startSprintError, setStartSprintError] = useState<string | null>(null);

	const [editOpen, setEditOpen] = useState(false);
	const [editError, setEditError] = useState<string | null>(null);

	const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false);

	// Register a document-level keydown listener while the delete-confirmation
	// modal is open so Escape works regardless of which element currently has
	// focus (mirrors SprintFormModal's own such fallback for the edit dialog
	// stacked underneath it — SprintFormModal suppresses its own Escape
	// handling via `suppressEscape` while this one is open, so only the
	// topmost dialog ever closes).
	useEffect(() => {
		if (!deleteConfirmOpen) return;
		const handleKeyDown = (e: KeyboardEvent) => {
			if (e.key === "Escape") setDeleteConfirmOpen(false);
		};
		document.addEventListener("keydown", handleKeyDown);
		return () => document.removeEventListener("keydown", handleKeyDown);
	}, [deleteConfirmOpen]);

	const doneStatusIds = taskStatuses
		.filter((s) => s.category === "done")
		.map((s) => s.id);

	// Only the dialog's count is needed, so ask the server for totals instead
	// of paging through the sprint's tasks.
	const { data: sprintTaskCount = 0 } = useQuery({
		...sprintTaskCountQueryOptions(projectId, sprintId),
		enabled: completeOpen,
	});
	const { data: doneTaskCount = 0 } = useQuery({
		...sprintTaskCountQueryOptions(projectId, sprintId, doneStatusIds),
		enabled: completeOpen && doneStatusIds.length > 0,
	});
	const incompleteCount = Math.max(0, sprintTaskCount - doneTaskCount);

	// Open sprints the unfinished tasks can move to; paged as the dialog
	// scrolls. The sprint being completed is dropped client-side.
	const openSprintsQuery = useInfiniteQuery({
		...sprintsInfiniteQueryOptions(projectId, {
			status: ["active", "planned"],
		}),
		enabled: completeOpen,
	});
	const otherSprints = (
		openSprintsQuery.data?.pages.flatMap((p) => p.items) ?? []
	).filter((s) => s.id !== sprintId);

	// Surfaced as a non-blocking warning in the Start Sprint dialog — Scrum
	// favors one active sprint at a time, but this doesn't prevent starting
	// a second one. At most two active sprints are fetched: enough to find one
	// other than this sprint.
	const { data: activeSprints } = useQuery({
		queryKey: ["projects", projectId, "sprints", "active-pair"],
		queryFn: () => listSprints(projectId, { status: ["active"], pageSize: 2 }),
		enabled: startSprintOpen,
		staleTime: 30_000,
	});
	const otherActiveSprint =
		activeSprints?.items.find((s) => s.id !== sprintId) ?? null;

	const completeSprintMutation = useMutation({
		meta: { success: "completed", subject: "sprint" },
		mutationFn: () =>
			completeSprint(projectId, sprintId, {
				move_to_sprint_id: moveToSprintId ?? null,
			}),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["projects", projectId, "sprints"] });
			qc.invalidateQueries({
				queryKey: ["projects", projectId, "tasks"],
			});
			qc.invalidateQueries({
				queryKey: ["projects", projectId, "sprints", sprintId, "tasks"],
			});
			setCompleteOpen(false);
			navigate({
				to: "/projects/$projectId/interactions/backlog",
				params: { projectId },
			});
		},
		onError: (err: unknown) => {
			setCompleteSprintError(
				getApiErrorCode(err) === ApiErrorCode.SprintAlreadyComplete
					? t("layout.sprintDetail.completeSprintModal.alreadyComplete")
					: t("layout.sprintDetail.completeSprintModal.completeFailed"),
			);
		},
	});

	const updateSprintMutation = useMutation({
		meta: { success: "updated", subject: "sprint" },
		mutationFn: (payload: UpdateSprintPayload) =>
			updateSprint(projectId, sprintId, payload),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["projects", projectId, "sprints"] });
			qc.invalidateQueries({
				queryKey: ["projects", projectId, "sprints", sprintId],
			});
			setEditError(null);
		},
		onError: () => {
			setEditError(t("layout.sprintDetail.editSprintModal.error"));
		},
	});

	const deleteSprintMutation = useMutation({
		meta: { success: "deleted", subject: "sprint" },
		mutationFn: () => deleteSprint(projectId, sprintId),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["projects", projectId, "sprints"] });
			qc.invalidateQueries({ queryKey: ["projects", projectId, "tasks"] });
			// Drop the deleted sprint's own cached detail/tasks queries so the
			// still-warm cache (30s/15s staleTime) can't render a phantom sprint
			// if its URL is revisited before the loader's 404 → backlog redirect.
			qc.removeQueries({
				queryKey: ["projects", projectId, "sprints", sprintId],
			});
			setDeleteConfirmOpen(false);
			setEditOpen(false);
			navigate({
				to: "/projects/$projectId/interactions/backlog",
				params: { projectId },
			});
		},
		onError: () => {
			setDeleteConfirmOpen(false);
			setEditError(t("layout.sprintDetail.editSprintModal.deleteError"));
		},
	});

	if (isError || !sprint) {
		return (
			<div className="flex flex-1 flex-col items-center justify-center gap-3 p-8 text-muted-foreground">
				<AlertCircle className="size-8 opacity-40" />
				<p className="text-sm">{t("layout.sprintDetail.notFound")}</p>
				{isError && <ReportIssueButton error={sprintError} />}
			</div>
		);
	}

	const statusBadge =
		sprint.status === "active"
			? t("layout.sprintDetail.statusActive")
			: sprint.status === "planned"
				? t("layout.sprintDetail.statusPlanned")
				: t("layout.sprintDetail.statusCompleted");

	return (
		<>
			<InteractionLayout
				projectId={projectId}
				interactionKey={`sprint:${sprintId}`}
				title={sprint.name}
				description={
					sprint.goal
						? sprint.goal
						: sprint.start_date
							? t("layout.sprintDetail.descriptionWithStartDate", {
									status: statusBadge,
									date: formatDate(sprint.start_date, undefined, {
										dateOnly: true,
									}),
								})
							: t("layout.sprintDetail.description", { status: statusBadge })
				}
				canCreate={canCreate}
				canEdit={canEdit}
				canManageViews={canManageViews}
				canManageSprints={canManageSprints}
				sprintId={sprintId}
				context="sprint"
				headerActions={
					<>
						<SprintStatusBadge status={sprint.status} />
						{canManageSprints && (
							<>
								<button
									type="button"
									onClick={() => {
										setEditError(null);
										setEditOpen(true);
									}}
									className="flex items-center gap-1.5 rounded-lg border border-border/50 bg-muted/10 px-3 py-1.5 text-xs font-semibold text-muted-foreground hover:bg-muted/30 hover:text-foreground transition-all duration-150"
								>
									<Pencil className="size-3.5 shrink-0" />
									{t("layout.sprintDetail.editSprint")}
								</button>
								{sprint.status === "planned" && (
									<button
										type="button"
										onClick={() => {
											setStartSprintError(null);
											setStartSprintOpen(true);
										}}
										className="flex items-center gap-1.5 rounded-lg bg-success px-3 py-1.5 text-xs font-semibold text-white shadow-sm hover:bg-success transition-all duration-150"
									>
										<Play className="size-3.5 shrink-0 fill-white" />
										{t("layout.sprintDetail.startSprint")}
									</button>
								)}
								{sprint.status === "active" && (
									<button
										type="button"
										onClick={() => {
											setCompleteSprintError(null);
											setCompleteOpen(true);
										}}
										className="flex items-center gap-1.5 rounded-lg bg-primary/10 px-3 py-1.5 text-xs font-semibold text-primary hover:bg-primary/20 transition-all duration-150"
									>
										<CheckCircle2 className="size-3.5 shrink-0" />
										{t("layout.sprintDetail.completeSprint")}
									</button>
								)}
							</>
						)}
					</>
				}
			/>
			<StartSprintModal
				sprint={sprint}
				open={startSprintOpen}
				onOpenChange={setStartSprintOpen}
				onSubmit={async (_sid, payload) => {
					try {
						await updateSprintMutation.mutateAsync(payload);
					} catch (err) {
						setStartSprintError(t("layout.startSprintModal.error"));
						throw err;
					}
				}}
				otherActiveSprint={otherActiveSprint}
				errorMessage={startSprintError}
			/>
			<SprintFormModal
				mode="edit"
				sprint={sprint}
				open={editOpen}
				onOpenChange={setEditOpen}
				onSubmit={async (_sid, payload) => {
					await updateSprintMutation.mutateAsync(payload);
				}}
				errorMessage={editError}
				onDelete={() => setDeleteConfirmOpen(true)}
				suppressEscape={deleteConfirmOpen}
			/>
			{/* Complete Sprint Modal */}
			<Dialog open={completeOpen} onOpenChange={setCompleteOpen}>
				<DialogContent className="sm:max-w-md">
					<DialogHeader>
						<DialogTitle className="font-[Syne] text-lg font-bold tracking-tight">
							{t("layout.sprintDetail.completeSprintModal.title")}
						</DialogTitle>
						<DialogDescription>
							{incompleteCount > 0
								? t("layout.sprintDetail.completeSprintModal.willBeMovedTo", {
										count: incompleteCount,
									})
								: t(
										"layout.sprintDetail.completeSprintModal.noIncompleteTasks",
									)}
						</DialogDescription>
					</DialogHeader>

					{incompleteCount > 0 && (
						<RadioGroup
							value={moveToSprintId ?? MOVE_TO_BACKLOG}
							onValueChange={(v) =>
								setMoveToSprintId(v === MOVE_TO_BACKLOG ? null : String(v))
							}
							className="max-h-[50vh] overflow-y-auto"
						>
							{/* Backlog option */}
							<Label
								className={cn(
									"flex cursor-pointer items-center gap-3 rounded-lg border p-3 transition-all",
									moveToSprintId === null
										? "border-primary/50 bg-primary/5"
										: "border-border/40 hover:bg-muted/30",
								)}
							>
								<RadioGroupItem value={MOVE_TO_BACKLOG} />
								<div>
									<p className="text-sm font-semibold">
										{t(
											"layout.sprintDetail.completeSprintModal.productBacklog",
										)}
									</p>
									<p className="text-xs font-normal text-muted-foreground">
										{t(
											"layout.sprintDetail.completeSprintModal.unassignedHint",
										)}
									</p>
								</div>
							</Label>

							{/* Other sprints */}
							{otherSprints.map((s) => (
								<Label
									key={s.id}
									className={cn(
										"flex cursor-pointer items-center gap-3 rounded-lg border p-3 transition-all",
										moveToSprintId === s.id
											? "border-primary/50 bg-primary/5"
											: "border-border/40 hover:bg-muted/30",
									)}
								>
									<RadioGroupItem value={s.id} />
									<div className="flex flex-col gap-1">
										<p className="text-sm font-semibold">{s.name}</p>
										<SprintStatusBadge status={s.status} />
									</div>
								</Label>
							))}
							<LoadMoreSentinel
								hasMore={!!openSprintsQuery.hasNextPage}
								isLoadingMore={openSprintsQuery.isFetchingNextPage}
								isError={openSprintsQuery.isFetchNextPageError}
								onLoadMore={() => void openSprintsQuery.fetchNextPage()}
							/>
						</RadioGroup>
					)}

					{completeSprintError && (
						<InlineNotice tone="error">{completeSprintError}</InlineNotice>
					)}

					<DialogFooter>
						<Button variant="outline" onClick={() => setCompleteOpen(false)}>
							{t("layout.sprintDetail.completeSprintModal.cancel")}
						</Button>
						<Button
							onClick={() => completeSprintMutation.mutate()}
							disabled={completeSprintMutation.isPending}
						>
							{completeSprintMutation.isPending
								? t("layout.sprintDetail.completeSprintModal.completing")
								: t("layout.sprintDetail.completeSprintModal.completeSprint")}
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
			{/* Delete Sprint Confirmation Modal */}
			<AlertDialog open={deleteConfirmOpen} onOpenChange={setDeleteConfirmOpen}>
				<AlertDialogContent>
					<AlertDialogHeader>
						<AlertDialogTitle className="font-[Syne] text-lg font-bold tracking-tight">
							{t("layout.sprintDetail.deleteSprintModal.title")}
						</AlertDialogTitle>
						<AlertDialogDescription>
							{t("layout.sprintDetail.deleteSprintModal.message", {
								name: sprint.name,
							})}
						</AlertDialogDescription>
					</AlertDialogHeader>
					<AlertDialogFooter>
						<AlertDialogCancel>
							{t("layout.sprintDetail.deleteSprintModal.cancel")}
						</AlertDialogCancel>
						<AlertDialogAction
							variant="destructive"
							onClick={() => deleteSprintMutation.mutate()}
							disabled={deleteSprintMutation.isPending}
						>
							<Trash2 data-icon="inline-start" />
							{deleteSprintMutation.isPending
								? t("layout.sprintDetail.deleteSprintModal.deleting")
								: t("layout.sprintDetail.deleteSprintModal.confirm")}
						</AlertDialogAction>
					</AlertDialogFooter>
				</AlertDialogContent>
			</AlertDialog>
		</>
	);
}
