import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { AlertCircle, CheckCircle2, Pencil, X } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { InteractionLayout } from "@/components/projects/interactions/interaction-layout";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import { formatDate } from "@/lib/format-date";
import {
	completeSprint,
	sprintQueryOptions,
	sprintsQueryOptions,
	sprintTasksQueryOptions,
	type UpdateSprintPayload,
	updateSprint,
} from "@/lib/interaction-api";
import { taskStatusesQueryOptions } from "@/lib/project-api";
import { cn } from "@/lib/utils";

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

	const { data: sprint, isError } = useQuery(
		sprintQueryOptions(projectId, sprintId),
	);
	const { data: allSprints = [] } = useQuery(sprintsQueryOptions(projectId));
	const { data: tasksResult } = useQuery(
		sprintTasksQueryOptions(projectId, sprintId),
	);
	const { data: taskStatuses = [] } = useQuery(
		taskStatusesQueryOptions(projectId),
	);

	const canCreate = hasProjectPermission("tasks.write");
	const canEdit = hasProjectPermission("tasks.write");
	const canManageViews = hasProjectPermission("projects.write");
	const canManageSprints = hasProjectPermission("sprints.write");

	const [completeOpen, setCompleteOpen] = useState(false);
	const [moveToSprintId, setMoveToSprintId] = useState<string | null>(null);

	const [editOpen, setEditOpen] = useState(false);
	const [editName, setEditName] = useState("");
	const [editGoal, setEditGoal] = useState("");
	const [editStartDate, setEditStartDate] = useState("");
	const [editEndDate, setEditEndDate] = useState("");
	const [editError, setEditError] = useState<string | null>(null);

	const dateRangeInvalid = Boolean(
		editStartDate && editEndDate && editEndDate < editStartDate,
	);

	// Register a document-level keydown listener while the modal is open so
	// Escape works regardless of which element currently has focus (mirrors
	// StartSprintModal's fix for the same issue).
	useEffect(() => {
		if (!editOpen) return;
		const handleKeyDown = (e: KeyboardEvent) => {
			if (e.key === "Escape") setEditOpen(false);
		};
		document.addEventListener("keydown", handleKeyDown);
		return () => document.removeEventListener("keydown", handleKeyDown);
	}, [editOpen]);

	const sprintTasks = tasksResult?.items ?? [];

	const doneStatusIds = new Set(
		taskStatuses.filter((s) => s.category === "done").map((s) => s.id),
	);
	const incompleteTasks = sprintTasks.filter(
		(task) => !task.status_id || !doneStatusIds.has(task.status_id),
	);

	const otherSprints = allSprints.filter(
		(s) => s.id !== sprintId && s.status !== "completed",
	);

	const completeSprintMutation = useMutation({
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
	});

	const updateSprintMutation = useMutation({
		mutationFn: (payload: UpdateSprintPayload) =>
			updateSprint(projectId, sprintId, payload),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["projects", projectId, "sprints"] });
			qc.invalidateQueries({
				queryKey: ["projects", projectId, "sprints", sprintId],
			});
			setEditError(null);
			setEditOpen(false);
		},
		onError: () => {
			setEditError(t("layout.sprintDetail.editSprintModal.error"));
		},
	});

	if (isError || !sprint) {
		return (
			<div className="flex flex-1 flex-col items-center justify-center gap-3 p-8 text-muted-foreground">
				<AlertCircle className="size-8 opacity-40" />
				<p className="text-sm">{t("layout.sprintDetail.notFound")}</p>
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
									date: formatDate(sprint.start_date),
								})
							: t("layout.sprintDetail.description", { status: statusBadge })
				}
				canCreate={canCreate}
				canEdit={canEdit}
				canManageViews={canManageViews}
				sprintId={sprintId}
				context="sprint"
				headerActions={
					canManageSprints ? (
						<>
							<button
								type="button"
								onClick={() => {
									setEditName(sprint.name);
									setEditGoal(sprint.goal ?? "");
									setEditStartDate(
										sprint.start_date ? sprint.start_date.slice(0, 10) : "",
									);
									setEditEndDate(
										sprint.end_date ? sprint.end_date.slice(0, 10) : "",
									);
									setEditError(null);
									setEditOpen(true);
								}}
								className="flex items-center gap-1.5 rounded-lg border border-border/50 bg-muted/10 px-3 py-1.5 text-xs font-semibold text-muted-foreground hover:bg-muted/30 hover:text-foreground transition-all duration-150"
							>
								<Pencil className="size-3.5 shrink-0" />
								{t("layout.sprintDetail.editSprint")}
							</button>
							{sprint.status === "active" && (
								<button
									type="button"
									onClick={() => setCompleteOpen(true)}
									className="flex items-center gap-1.5 rounded-lg bg-primary/10 px-3 py-1.5 text-xs font-semibold text-primary hover:bg-primary/20 transition-all duration-150"
								>
									<CheckCircle2 className="size-3.5 shrink-0" />
									{t("layout.sprintDetail.completeSprint")}
								</button>
							)}
						</>
					) : undefined
				}
			/>
			{/* Edit Sprint Modal */}
			{editOpen && (
				// biome-ignore lint/a11y/noStaticElementInteractions: modal backdrop
				<div
					className="fixed inset-0 z-50 flex items-center justify-center bg-black/40"
					onClick={(e) => {
						if (e.target === e.currentTarget) setEditOpen(false);
					}}
					onKeyDown={(e) => {
						if (e.key === "Escape") setEditOpen(false);
					}}
				>
					{/* biome-ignore lint/a11y/noStaticElementInteractions: modal panel */}
					<div
						className="relative w-full max-w-md rounded-xl border border-border/50 bg-background p-6 shadow-2xl mx-4"
						onClick={(e) => e.stopPropagation()}
						onKeyDown={(e) => e.stopPropagation()}
					>
						<button
							type="button"
							onClick={() => setEditOpen(false)}
							className="absolute right-4 top-4 flex size-7 items-center justify-center rounded-md text-muted-foreground/60 hover:text-foreground hover:bg-muted/60 transition-all"
						>
							<X className="size-4" />
						</button>
						<h2 className="font-[Syne] text-lg font-bold tracking-tight mb-4">
							{t("layout.sprintDetail.editSprintModal.title")}
						</h2>
						<div className="flex flex-col gap-4">
							<div className="flex flex-col gap-1.5">
								<label htmlFor="es-name" className="text-sm font-medium">
									{t("layout.sprintDetail.editSprintModal.nameLabel")}
								</label>
								<input
									id="es-name"
									value={editName}
									onChange={(e) => setEditName(e.target.value)}
									className="rounded-md border border-input bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-primary/30 placeholder:text-muted-foreground/50"
								/>
							</div>
							<div className="flex flex-col gap-1.5">
								<label
									htmlFor="es-goal"
									className="text-sm font-medium text-muted-foreground"
								>
									{t("layout.sprintDetail.editSprintModal.goalLabel")}{" "}
									<span className="text-xs font-normal">
										{t("layout.sprintDetail.editSprintModal.optional")}
									</span>
								</label>
								<textarea
									id="es-goal"
									value={editGoal}
									onChange={(e) => setEditGoal(e.target.value)}
									rows={2}
									placeholder={t(
										"layout.sprintDetail.editSprintModal.goalPlaceholder",
									)}
									className="rounded-md border border-input bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-primary/30 placeholder:text-muted-foreground/50 resize-none"
								/>
							</div>
							<div className="grid grid-cols-2 gap-3">
								<div className="flex flex-col gap-1.5">
									<label
										htmlFor="es-start"
										className="text-sm font-medium text-muted-foreground"
									>
										{t("layout.sprintDetail.editSprintModal.startDateLabel")}
									</label>
									<input
										id="es-start"
										type="date"
										value={editStartDate}
										onChange={(e) => setEditStartDate(e.target.value)}
										className="rounded-md border border-input bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-primary/30"
									/>
								</div>
								<div className="flex flex-col gap-1.5">
									<label
										htmlFor="es-end"
										className="text-sm font-medium text-muted-foreground"
									>
										{t("layout.sprintDetail.editSprintModal.dueDateLabel")}
									</label>
									<input
										id="es-end"
										type="date"
										value={editEndDate}
										min={editStartDate || undefined}
										onChange={(e) => setEditEndDate(e.target.value)}
										className="rounded-md border border-input bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-primary/30"
									/>
								</div>
							</div>
							{dateRangeInvalid && (
								<p className="text-xs text-destructive">
									{t("layout.sprintDetail.editSprintModal.dateRangeError")}
								</p>
							)}
							{editError && (
								<p className="text-xs text-destructive bg-destructive/10 rounded-lg px-3 py-2">
									{editError}
								</p>
							)}
						</div>
						<div className="mt-6 flex justify-end gap-2">
							<button
								type="button"
								onClick={() => setEditOpen(false)}
								className="rounded-lg border border-border/50 bg-muted/20 px-4 py-2 text-sm font-medium hover:bg-muted/40 transition-all"
							>
								{t("layout.sprintDetail.editSprintModal.cancel")}
							</button>
							<button
								type="button"
								onClick={() =>
									updateSprintMutation.mutate({
										name: editName.trim(),
										goal: editGoal.trim() || null,
										start_date: editStartDate
											? `${editStartDate}T00:00:00Z`
											: null,
										end_date: editEndDate ? `${editEndDate}T00:00:00Z` : null,
									})
								}
								disabled={
									updateSprintMutation.isPending ||
									!editName.trim() ||
									dateRangeInvalid
								}
								className="rounded-lg bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground hover:bg-primary/90 disabled:opacity-50 transition-all"
							>
								{updateSprintMutation.isPending
									? t("layout.sprintDetail.editSprintModal.saving")
									: t("layout.sprintDetail.editSprintModal.save")}
							</button>
						</div>
					</div>
				</div>
			)}
			{/* Complete Sprint Modal */}
			{completeOpen && (
				// biome-ignore lint/a11y/noStaticElementInteractions: modal backdrop
				<div
					className="fixed inset-0 z-50 flex items-center justify-center bg-black/40"
					onClick={(e) => {
						if (e.target === e.currentTarget) setCompleteOpen(false);
					}}
					onKeyDown={(e) => {
						if (e.key === "Escape") setCompleteOpen(false);
					}}
				>
					{/* biome-ignore lint/a11y/noStaticElementInteractions: modal panel */}
					<div
						className="relative w-full max-w-md rounded-xl border border-border/50 bg-background p-6 shadow-2xl mx-4"
						onClick={(e) => e.stopPropagation()}
						onKeyDown={(e) => e.stopPropagation()}
					>
						<button
							type="button"
							onClick={() => setCompleteOpen(false)}
							className="absolute right-4 top-4 flex size-7 items-center justify-center rounded-md text-muted-foreground/60 hover:text-foreground hover:bg-muted/60 transition-all"
						>
							<X className="size-4" />
						</button>
						<h2 className="font-[Syne] text-lg font-bold tracking-tight mb-1">
							{t("layout.sprintDetail.completeSprintModal.title")}
						</h2>
						<p className="text-sm text-muted-foreground mb-5">
							{incompleteTasks.length > 0
								? t("layout.sprintDetail.completeSprintModal.willBeMovedTo", {
										count: incompleteTasks.length,
									})
								: t(
										"layout.sprintDetail.completeSprintModal.noIncompleteTasks",
									)}
						</p>

						{incompleteTasks.length > 0 && (
							<div className="flex flex-col gap-2 mb-5">
								{/* Backlog option */}
								<label
									className={cn(
										"flex items-center gap-3 rounded-lg border p-3 cursor-pointer transition-all",
										moveToSprintId === null
											? "border-primary/50 bg-primary/5"
											: "border-border/40 hover:bg-muted/30",
									)}
								>
									<input
										type="radio"
										name="move-to-sprint"
										value=""
										checked={moveToSprintId === null}
										onChange={() => setMoveToSprintId(null)}
										className="accent-primary"
									/>
									<div>
										<p className="text-sm font-semibold">
											{t(
												"layout.sprintDetail.completeSprintModal.productBacklog",
											)}
										</p>
										<p className="text-xs text-muted-foreground">
											{t(
												"layout.sprintDetail.completeSprintModal.unassignedHint",
											)}
										</p>
									</div>
								</label>

								{/* Other sprints */}
								{otherSprints.map((s) => (
									<label
										key={s.id}
										className={cn(
											"flex items-center gap-3 rounded-lg border p-3 cursor-pointer transition-all",
											moveToSprintId === s.id
												? "border-primary/50 bg-primary/5"
												: "border-border/40 hover:bg-muted/30",
										)}
									>
										<input
											type="radio"
											name="move-to-sprint"
											value={s.id}
											checked={moveToSprintId === s.id}
											onChange={() => setMoveToSprintId(s.id)}
											className="accent-primary"
										/>
										<div>
											<p className="text-sm font-semibold">{s.name}</p>
											<p className="text-xs text-muted-foreground capitalize">
												{s.status}
											</p>
										</div>
									</label>
								))}
							</div>
						)}

						<div className="flex justify-end gap-2">
							<button
								type="button"
								onClick={() => setCompleteOpen(false)}
								className="rounded-lg border border-border/50 bg-muted/20 px-4 py-2 text-sm font-medium hover:bg-muted/40 transition-all"
							>
								{t("layout.sprintDetail.completeSprintModal.cancel")}
							</button>
							<button
								type="button"
								onClick={() => completeSprintMutation.mutate()}
								disabled={completeSprintMutation.isPending}
								className="rounded-lg bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground hover:bg-primary/90 disabled:opacity-50 transition-all"
							>
								{completeSprintMutation.isPending
									? t("layout.sprintDetail.completeSprintModal.completing")
									: t("layout.sprintDetail.completeSprintModal.completeSprint")}
							</button>
						</div>
					</div>
				</div>
			)}
		</>
	);
}
