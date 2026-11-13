import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Check, Hash, MoreVertical, Share2, Trash2, X } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
	Breadcrumb,
	BreadcrumbItem,
	BreadcrumbList,
	BreadcrumbPage,
	BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
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
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Spinner } from "@/components/ui/spinner";
import { copyToClipboard } from "@/lib/clipboard";
import { formatDate } from "@/lib/format-date";
import type { Task } from "@/lib/interaction-api";
import { deleteTask } from "@/lib/interaction-api";
import { cn } from "@/lib/utils";
import { shortId } from "./helpers";

interface TaskHeaderProps {
	task: Task;
	mode: "modal" | "page";
	projectName?: string;
	interactionName?: string;
	projectId?: string;
	taskIdPrefix?: string;
	canDelete?: boolean;
	onClose: () => void;
	onDeleted?: () => void;
}

export function TaskHeader({
	task,
	mode,
	projectName,
	interactionName,
	projectId,
	taskIdPrefix = "",
	canDelete = false,
	onClose,
	onDeleted,
}: TaskHeaderProps) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const [linkCopied, setLinkCopied] = useState(false);
	const [confirmDelete, setConfirmDelete] = useState(false);

	const deleteMutation = useMutation({
		meta: { success: "deleted", subject: "task" },
		mutationFn: () => {
			if (!projectId || !task) throw new Error("missing context");
			return deleteTask(projectId, task.id);
		},
		onSuccess: () => {
			if (projectId) {
				qc.invalidateQueries({
					queryKey: ["projects", projectId],
					predicate: (q) => {
						const key = q.queryKey as string[];
						return key.includes("tasks") || key.includes("backlog-tasks");
					},
				});
			}
			setConfirmDelete(false);
			onDeleted?.();
		},
	});

	const handleShare = () => {
		// Always share the canonical task detail page URL, not modal URLs with query params
		const taskUrl = projectId
			? `${window.location.origin}/projects/${projectId}/tasks/${task.id}`
			: window.location.href;
		void copyToClipboard(taskUrl).then((ok) => {
			if (!ok) return;
			setLinkCopied(true);
			setTimeout(() => setLinkCopied(false), 2000);
		});
	};

	return (
		<div className="flex shrink-0 items-center gap-2 lg:gap-3 border-b border-border/30 px-4 py-2.5 bg-muted/20">
			{/* Breadcrumb (page mode only) */}
			{mode === "page" && projectName && (
				<Breadcrumb className="mr-2 hidden md:flex">
					<BreadcrumbList className="gap-1.5 text-sm">
						<BreadcrumbItem>{projectName}</BreadcrumbItem>
						{interactionName && (
							<>
								<BreadcrumbSeparator />
								<BreadcrumbItem>{interactionName}</BreadcrumbItem>
							</>
						)}
						<BreadcrumbSeparator />
						<BreadcrumbItem className="min-w-0">
							<BreadcrumbPage className="max-w-36 truncate font-medium">
								{task.title}
							</BreadcrumbPage>
						</BreadcrumbItem>
					</BreadcrumbList>
				</Breadcrumb>
			)}

			{/* Task short ID */}
			<div className="flex items-center gap-1.5 rounded-md bg-muted/60 px-2 py-1 border border-border/30">
				<Hash className="size-3 text-muted-foreground/60" />
				<span className="font-[JetBrains_Mono,monospace] text-xs font-semibold text-muted-foreground tracking-wider">
					{taskIdPrefix && task.task_number > 0
						? `${taskIdPrefix}-${task.task_number}`
						: shortId(task.id)}
				</span>
			</div>

			<span className="hidden md:inline text-xs text-muted-foreground/60 font-medium">
				{t("taskDetail.header.created", { date: formatDate(task.created_at) })}
			</span>

			<div className="ml-auto flex items-center gap-1">
				<button
					type="button"
					onClick={handleShare}
					className={cn(
						"flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-sm font-medium transition-all duration-150",
						linkCopied
							? "text-success bg-success/10"
							: "text-muted-foreground/70 hover:text-foreground hover:bg-muted/60",
					)}
				>
					{linkCopied ? (
						<>
							<Check className="size-3 text-success" />
							<span>{t("taskDetail.header.copied")}</span>
						</>
					) : (
						<>
							<Share2 className="size-3" />
							<span>{t("taskDetail.header.share")}</span>
						</>
					)}
				</button>

				{canDelete && (
					<DropdownMenu>
						<DropdownMenuTrigger
							className="flex size-7 items-center justify-center rounded-md text-muted-foreground/60 hover:text-foreground hover:bg-muted/60 transition-all duration-150"
							aria-label={t("taskDetail.header.taskActions")}
						>
							<MoreVertical className="size-4" />
						</DropdownMenuTrigger>
						<DropdownMenuContent align="end" className="w-40">
							<DropdownMenuItem
								className="text-destructive focus:text-destructive"
								onClick={() => setConfirmDelete(true)}
							>
								<Trash2 className="size-3.5 mr-2" />
								{t("taskDetail.header.delete")}
							</DropdownMenuItem>
						</DropdownMenuContent>
					</DropdownMenu>
				)}

				{mode === "modal" && (
					<button
						type="button"
						onClick={onClose}
						className="flex size-7 items-center justify-center rounded-md text-muted-foreground/60 hover:text-foreground hover:bg-muted/60 transition-all duration-150"
						aria-label={t("taskDetail.header.closeTaskDetail")}
					>
						<X className="size-3.5" />
					</button>
				)}
			</div>

			{/* Delete confirmation dialog */}
			<Dialog open={confirmDelete} onOpenChange={setConfirmDelete}>
				<DialogContent className="max-w-sm">
					<DialogHeader>
						<DialogTitle>
							{t("taskDetail.header.deleteDialog.title")}
						</DialogTitle>
						<DialogDescription>
							{t("taskDetail.header.deleteDialog.description", {
								title: task.title,
							})}
						</DialogDescription>
					</DialogHeader>
					<DialogFooter>
						<Button
							variant="outline"
							onClick={() => setConfirmDelete(false)}
							disabled={deleteMutation.isPending}
						>
							{t("taskDetail.header.deleteDialog.cancel")}
						</Button>
						<Button
							variant="destructive"
							onClick={() => deleteMutation.mutate()}
							disabled={deleteMutation.isPending}
						>
							{deleteMutation.isPending ? (
								<Spinner aria-hidden="true" />
							) : (
								t("taskDetail.header.deleteDialog.delete")
							)}
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</div>
	);
}
