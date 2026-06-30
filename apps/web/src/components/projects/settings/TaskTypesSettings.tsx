import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Edit2, Plus, Star, Tag, Trash2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { DeleteTaskTypeDialog } from "@/components/projects/task-types/DeleteTaskTypeDialog";
import { TaskTypeFormDialog } from "@/components/projects/task-types/TaskTypeFormDialog";
import { getTaskTypeIconComponent } from "@/components/projects/task-types/task-type-icons";
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
	setDefaultTaskType,
	type TaskType,
	taskTypesQueryOptions,
} from "@/lib/project-api";

export function TaskTypesSettings({
	projectId,
	canWrite,
}: {
	projectId: string;
	canWrite: boolean;
}) {
	const { t } = useTranslation();
	const { data: types, isLoading } = useQuery(taskTypesQueryOptions(projectId));
	const queryClient = useQueryClient();
	const [createOpen, setCreateOpen] = useState(false);
	const [editType, setEditType] = useState<TaskType | null>(null);
	const [deleteType, setDeleteType] = useState<TaskType | null>(null);

	const setDefaultMutation = useMutation({
		mutationFn: (typeId: string) => setDefaultTaskType(projectId, typeId),
		onSuccess: () => {
			queryClient.invalidateQueries({
				queryKey: taskTypesQueryOptions(projectId).queryKey,
			});
		},
	});

	return (
		<div className="rounded-xl border border-border/60 bg-card p-6">
			<div className="flex items-center justify-between mb-1">
				<div>
					<h3 className="font-[Syne] text-base font-semibold">
						{t("project.settings.types.title")}
					</h3>
					<p className="text-xs text-muted-foreground mt-0.5">
						{t("project.settings.types.subtitle")}
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
						{t("project.settings.types.newType")}
					</Button>
				) : null}
			</div>

			{isLoading ? (
				<div className="rounded-xl border overflow-hidden mt-4">
					{["t1", "t2", "t3"].map((k) => (
						<div
							key={k}
							className="flex items-center gap-4 border-b px-5 py-4 last:border-0"
						>
							<Skeleton className="size-3 rounded-full" />
							<Skeleton className="h-4 w-32" />
							<Skeleton className="h-4 w-48 ml-2" />
							<div className="flex gap-1.5 ml-auto">
								<Skeleton className="size-7 rounded-md" />
								<Skeleton className="size-7 rounded-md" />
							</div>
						</div>
					))}
				</div>
			) : !types?.length ? (
				<div className="flex flex-col items-center gap-4 rounded-xl border border-dashed bg-muted/20 py-16 text-center mt-4">
					<div className="flex size-12 items-center justify-center rounded-full bg-muted text-muted-foreground/60">
						<Tag className="size-6" />
					</div>
					<div>
						<p className="text-sm font-medium">
							{t("project.settings.types.emptyTitle")}
						</p>
						<p className="mt-1 text-xs text-muted-foreground">
							{t("project.settings.types.emptyDesc")}
						</p>
					</div>
					{canWrite ? (
						<Button
							size="sm"
							variant="outline"
							onClick={() => setCreateOpen(true)}
						>
							<Plus className="size-4" />
							{t("project.settings.types.createType")}
						</Button>
					) : null}
				</div>
			) : (
				<div className="overflow-x-auto rounded-xl border mt-4">
					<Table>
						<TableHeader>
							<TableRow className="bg-muted/40 hover:bg-muted/40">
								<TableHead className="w-10 px-5 text-xs font-semibold uppercase tracking-wide">
									{t("project.settings.types.colIcon")}
								</TableHead>
								<TableHead className="px-5 text-xs font-semibold uppercase tracking-wide">
									{t("project.settings.types.colName")}
								</TableHead>
								<TableHead className="px-5 text-xs font-semibold uppercase tracking-wide">
									{t("project.settings.types.colDescription")}
								</TableHead>
								<TableHead className="px-5 text-xs font-semibold uppercase tracking-wide">
									{t("project.settings.types.colDefault")}
								</TableHead>
								<TableHead className="w-20 px-5 text-xs font-semibold uppercase tracking-wide" />
							</TableRow>
						</TableHeader>
						<TableBody>
							{types.map((type) => (
								<TableRow key={type.id} className="group">
									<TableCell className="px-5">
										{(() => {
											const IconComp = getTaskTypeIconComponent(type.icon);
											if (IconComp) {
												return (
													<IconComp
														className="size-4"
														style={{ color: type.color ?? "#6366f1" }}
													/>
												);
											}
											return (
												<span
													className="inline-block size-3 rounded-full"
													style={{ backgroundColor: type.color ?? "#6366f1" }}
												/>
											);
										})()}
									</TableCell>
									<TableCell className="px-5">
										<div className="flex items-center gap-2">
											{type.icon && !getTaskTypeIconComponent(type.icon) ? (
												<span
													className="inline-block size-2.5 rounded-full shrink-0"
													style={{ backgroundColor: type.color ?? "#6366f1" }}
												/>
											) : null}
											<span className="text-sm font-medium">{type.name}</span>
										</div>
									</TableCell>
									<TableCell className="px-5 text-sm text-muted-foreground max-w-xs truncate">
										{type.description ?? (
											<span className="italic opacity-50">—</span>
										)}
									</TableCell>
									<TableCell className="px-5">
										{type.is_default ? (
											<span className="inline-flex items-center gap-1 rounded-full bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-400">
												<Star className="size-3 fill-current" />
												{t("project.settings.types.default")}
											</span>
										) : null}
									</TableCell>
									<TableCell className="px-5">
										{canWrite ? (
											<div className="flex items-center justify-end gap-0.5 opacity-100 transition-opacity sm:opacity-0 sm:group-hover:opacity-100">
												{!type.is_default ? (
													<Button
														variant="ghost"
														size="icon-sm"
														onClick={() => setDefaultMutation.mutate(type.id)}
														disabled={setDefaultMutation.isPending}
														title={t("project.settings.types.setDefault")}
														aria-label={t("project.settings.types.setDefault")}
													>
														<Star className="size-3.5" />
													</Button>
												) : null}
												<Button
													variant="ghost"
													size="icon-sm"
													onClick={() => setEditType(type)}
													title={t("project.settings.types.editType")}
													aria-label={t("project.settings.types.editType")}
												>
													<Edit2 className="size-3.5" />
												</Button>
												<Button
													variant="ghost"
													size="icon-sm"
													className="text-destructive hover:text-destructive hover:bg-destructive/10"
													onClick={() => setDeleteType(type)}
													title={t("project.settings.types.deleteType")}
													aria-label={t("project.settings.types.deleteType")}
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

			<TaskTypeFormDialog
				projectId={projectId}
				open={createOpen}
				onOpenChange={setCreateOpen}
			/>
			{editType ? (
				<TaskTypeFormDialog
					projectId={projectId}
					taskType={editType}
					open={!!editType}
					onOpenChange={(o) => {
						if (!o) setEditType(null);
					}}
				/>
			) : null}
			{deleteType ? (
				<DeleteTaskTypeDialog
					projectId={projectId}
					taskType={deleteType}
					open={!!deleteType}
					onOpenChange={(o) => {
						if (!o) setDeleteType(null);
					}}
				/>
			) : null}
		</div>
	);
}
