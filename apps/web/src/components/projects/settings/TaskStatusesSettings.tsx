import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
	Edit2,
	GripVertical,
	LayoutList,
	Plus,
	Star,
	Trash2,
} from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { DeleteTaskStatusDialog } from "@/components/projects/task-statuses/DeleteTaskStatusDialog";
import { TaskStatusFormDialog } from "@/components/projects/task-statuses/TaskStatusFormDialog";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import {
	reorderTaskStatuses,
	setDefaultTaskStatus,
	type TaskStatus,
	taskStatusesQueryOptions,
} from "@/lib/project-api";
import { cn } from "@/lib/utils";

function StatusCategoryBadge({ category }: { category: string }) {
	const { t } = useTranslation();
	const colors: Record<string, string> = {
		backlog:
			"bg-slate-100 text-slate-700 border-slate-200 dark:bg-slate-900/30 dark:text-slate-400 dark:border-slate-700/30",
		refinement:
			"bg-violet-50 text-violet-700 border-violet-200 dark:bg-violet-900/20 dark:text-violet-400 dark:border-violet-700/30",
		ready:
			"bg-sky-50 text-sky-700 border-sky-200 dark:bg-sky-900/20 dark:text-sky-400 dark:border-sky-700/30",
		todo: "bg-amber-50 text-amber-700 border-amber-200 dark:bg-amber-900/20 dark:text-amber-400 dark:border-amber-700/30",
		inprogress:
			"bg-blue-50 text-blue-700 border-blue-200 dark:bg-blue-900/20 dark:text-blue-400 dark:border-blue-700/30",
		done: "bg-emerald-50 text-emerald-700 border-emerald-200 dark:bg-emerald-900/20 dark:text-emerald-400 dark:border-emerald-700/30",
	};
	const label = t(`project.statusCategories.${category}`, {
		defaultValue: category,
	});
	return (
		<span
			className={`inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium leading-none ${colors[category] ?? "bg-muted text-muted-foreground border-border"}`}
		>
			{label}
		</span>
	);
}

export function TaskStatusesSettings({
	projectId,
	canWrite,
}: {
	projectId: string;
	canWrite: boolean;
}) {
	const { t } = useTranslation();
	const { data: statuses, isLoading } = useQuery(
		taskStatusesQueryOptions(projectId),
	);
	const queryClient = useQueryClient();
	const [createOpen, setCreateOpen] = useState(false);
	const [editStatus, setEditStatus] = useState<TaskStatus | null>(null);
	const [deleteStatus, setDeleteStatus] = useState<TaskStatus | null>(null);
	const [localOrder, setLocalOrder] = useState<TaskStatus[] | null>(null);
	const [dragId, setDragId] = useState<string | null>(null);
	const [dragOverId, setDragOverId] = useState<string | null>(null);
	const [reorderError, setReorderError] = useState<string | null>(null);

	const setDefaultMutation = useMutation({
		mutationFn: (statusId: string) => setDefaultTaskStatus(projectId, statusId),
		onSuccess: () => {
			queryClient.invalidateQueries({
				queryKey: taskStatusesQueryOptions(projectId).queryKey,
			});
		},
	});

	const reorderMutation = useMutation({
		mutationFn: (ordered: TaskStatus[]) =>
			reorderTaskStatuses(
				projectId,
				ordered.map((s) => s.id),
			),
		onSuccess: () => {
			setReorderError(null);
			queryClient.invalidateQueries({
				queryKey: taskStatusesQueryOptions(projectId).queryKey,
			});
		},
		onError: () => {
			setLocalOrder(null);
			setReorderError(t("project.settings.statuses.errReorderFailed"));
		},
	});

	const sorted = [...(statuses ?? [])].sort((a, b) => a.position - b.position);
	const displayed = localOrder ?? sorted;

	// Once the server confirms a reorder (statuses refetched), drop the local
	// override so the table reflects the authoritative order again.
	// biome-ignore lint/correctness/useExhaustiveDependencies: intentionally reset local order only when server statuses refresh
	useEffect(() => {
		if (!dragId) setLocalOrder(null);
	}, [statuses]);

	const handleDrop = (targetId: string) => {
		const srcId = dragId;
		setDragId(null);
		setDragOverId(null);
		if (!srcId || srcId === targetId) return;
		const current = displayed;
		const srcIndex = current.findIndex((s) => s.id === srcId);
		const targetIndex = current.findIndex((s) => s.id === targetId);
		if (srcIndex === -1 || targetIndex === -1) return;
		const next = [...current];
		const [moved] = next.splice(srcIndex, 1);
		next.splice(targetIndex, 0, moved);
		setLocalOrder(next);
		setReorderError(null);
		reorderMutation.mutate(next);
	};

	return (
		<div className="rounded-xl border border-border/60 bg-card p-6">
			<div className="flex items-center justify-between mb-1">
				<div>
					<h3 className="font-[Syne] text-base font-semibold">
						{t("project.settings.statuses.title")}
					</h3>
					<p className="text-xs text-muted-foreground mt-0.5">
						{t("project.settings.statuses.subtitle")}
					</p>
				</div>
				{canWrite ? (
					<Button
						size="sm"
						variant="outline"
						className="gap-1.5 border-border/60 shrink-0"
						onClick={() => setCreateOpen(true)}
					>
						<Plus className="size-3.5" />
						{t("project.settings.statuses.newStatus")}
					</Button>
				) : null}
			</div>

			{reorderError ? (
				<p className="mt-4 text-xs text-destructive bg-destructive/10 rounded-lg px-3 py-2">
					{reorderError}
				</p>
			) : null}

			{isLoading ? (
				<div className="rounded-xl border overflow-hidden mt-4">
					{["s1", "s2", "s3"].map((k) => (
						<div
							key={k}
							className="flex items-center gap-4 border-b px-5 py-4 last:border-0"
						>
							<Skeleton className="size-3 rounded-full" />
							<Skeleton className="h-4 w-32" />
							<Skeleton className="h-5 w-20 rounded-full ml-auto" />
							<div className="flex gap-1.5">
								<Skeleton className="size-7 rounded-md" />
								<Skeleton className="size-7 rounded-md" />
							</div>
						</div>
					))}
				</div>
			) : !sorted.length ? (
				<div className="flex flex-col items-center gap-4 rounded-xl border border-dashed bg-muted/20 py-16 text-center mt-4">
					<div className="flex size-12 items-center justify-center rounded-full bg-muted text-muted-foreground/60">
						<LayoutList className="size-6" />
					</div>
					<div>
						<p className="text-sm font-medium">
							{t("project.settings.statuses.emptyTitle")}
						</p>
						<p className="mt-1 text-xs text-muted-foreground">
							{t("project.settings.statuses.emptyDesc")}
						</p>
					</div>
					{canWrite ? (
						<Button
							size="sm"
							variant="outline"
							onClick={() => setCreateOpen(true)}
						>
							<Plus className="size-4" />
							{t("project.settings.statuses.createStatus")}
						</Button>
					) : null}
				</div>
			) : (
				<div className="overflow-x-auto rounded-xl border mt-4">
					<Table>
						<TableHeader>
							<TableRow className="bg-muted/40 hover:bg-muted/40">
								<TableHead className="w-6 px-2" />
								<TableHead className="w-8 px-5 text-xs font-semibold uppercase tracking-wide">
									#
								</TableHead>
								<TableHead className="px-5 text-xs font-semibold uppercase tracking-wide">
									{t("project.settings.statuses.colName")}
								</TableHead>
								<TableHead className="w-36 px-5 text-xs font-semibold uppercase tracking-wide">
									{t("project.settings.statuses.colCategory")}
								</TableHead>
								<TableHead className="px-5 text-xs font-semibold uppercase tracking-wide">
									{t("project.settings.statuses.colDefault")}
								</TableHead>
								<TableHead className="w-20 px-5 text-xs font-semibold uppercase tracking-wide" />
							</TableRow>
						</TableHeader>
						<TableBody>
							{displayed.map((status, index) => (
								<TableRow
									key={status.id}
									className={cn(
										"group",
										canWrite && "cursor-grab active:cursor-grabbing",
										dragOverId === status.id &&
											dragId !== status.id &&
											"bg-primary/5",
									)}
									draggable={canWrite}
									onDragStart={() => canWrite && setDragId(status.id)}
									onDragOver={(e) => {
										if (!canWrite) return;
										e.preventDefault();
										e.dataTransfer.dropEffect = "move";
										setDragOverId(status.id);
									}}
									onDragLeave={() =>
										setDragOverId((id) => (id === status.id ? null : id))
									}
									onDrop={(e) => {
										e.preventDefault();
										handleDrop(status.id);
									}}
									onDragEnd={() => {
										setDragId(null);
										setDragOverId(null);
									}}
								>
									<TableCell className="px-2">
										{canWrite ? (
											<GripVertical className="size-3.5 shrink-0 text-muted-foreground/40" />
										) : null}
									</TableCell>
									<TableCell className="px-5 text-sm text-muted-foreground tabular-nums">
										{index + 1}
									</TableCell>
									<TableCell className="px-5">
										<div className="flex items-center gap-2">
											<span
												className="inline-block size-2.5 rounded-full shrink-0"
												style={{
													backgroundColor: status.color ?? "#6366f1",
												}}
											/>
											<span className="text-sm font-medium">{status.name}</span>
										</div>
									</TableCell>
									<TableCell className="px-5">
										<StatusCategoryBadge category={status.category} />
									</TableCell>
									<TableCell className="px-5">
										{status.is_default ? (
											<span className="inline-flex items-center gap-1 rounded-full bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-400">
												<Star className="size-3 fill-current" />
												{t("project.settings.statuses.default")}
											</span>
										) : null}
									</TableCell>
									<TableCell className="px-5">
										{canWrite ? (
											<div className="flex items-center justify-end gap-0.5 opacity-100 transition-opacity sm:opacity-0 sm:group-hover:opacity-100">
												{!status.is_default ? (
													<Button
														variant="ghost"
														size="icon-sm"
														onClick={() => setDefaultMutation.mutate(status.id)}
														disabled={setDefaultMutation.isPending}
														title={t("project.settings.statuses.setDefault")}
														aria-label={t("project.settings.statuses.setDefault")}
													>
														<Star className="size-3.5" />
													</Button>
												) : null}
												<Button
													variant="ghost"
													size="icon-sm"
													onClick={() => setEditStatus(status)}
													title={t("project.settings.statuses.editStatus")}
												>
													<Edit2 className="size-3.5" />
												</Button>
												<Button
													variant="ghost"
													size="icon-sm"
													className="text-destructive hover:text-destructive hover:bg-destructive/10"
													onClick={() => setDeleteStatus(status)}
													title={t("project.settings.statuses.deleteStatus")}
												>
													<Trash2 className="size-3.5" />
												</Button>
											</div>
										) : null}
									</TableCell>
								</TableRow>
							))}
						</TableBody>
					</Table>
				</div>
			)}

			<TaskStatusFormDialog
				projectId={projectId}
				defaultPosition={sorted.length}
				open={createOpen}
				onOpenChange={setCreateOpen}
			/>
			{editStatus ? (
				<TaskStatusFormDialog
					projectId={projectId}
					status={editStatus}
					open={!!editStatus}
					onOpenChange={(o) => {
						if (!o) setEditStatus(null);
					}}
				/>
			) : null}
			{deleteStatus ? (
				<DeleteTaskStatusDialog
					projectId={projectId}
					status={deleteStatus}
					open={!!deleteStatus}
					onOpenChange={(o) => {
						if (!o) setDeleteStatus(null);
					}}
				/>
			) : null}
		</div>
	);
}
