import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
	Check,
	ChevronRight,
	Hash,
	Loader2,
	MoreVertical,
	Share2,
	Trash2,
	X,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
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
import type { Task } from "@/lib/interaction-api";
import { deleteTask } from "@/lib/interaction-api";
import { cn } from "@/lib/utils";
import { formatDate, shortId } from "./helpers";

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
	const { t } = useTranslation();
	const qc = useQueryClient();
	const [linkCopied, setLinkCopied] = useState(false);
	const [confirmDelete, setConfirmDelete] = useState(false);

	const deleteMutation = useMutation({
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
		navigator.clipboard?.writeText(taskUrl)?.catch(() => {});
		setLinkCopied(true);
		setTimeout(() => setLinkCopied(false), 2000);
	};

	return (
		<div className="flex shrink-0 items-center gap-2 lg:gap-3 border-b border-border/30 px-4 lg:px-6 py-2.5 bg-muted/20">
			{/* Breadcrumb (page mode only) */}
			{mode === "page" && projectName && (
				<nav className="hidden md:flex items-center gap-1.5 text-sm text-muted-foreground/80 mr-2">
					<span className="hover:text-foreground transition-colors cursor-pointer">
						{projectName}
					</span>
					<ChevronRight className="size-3 text-muted-foreground/45" />
					{interactionName && (
						<>
							<span className="hover:text-foreground transition-colors cursor-pointer">
								{interactionName}
							</span>
							<ChevronRight className="size-3 text-muted-foreground/45" />
						</>
					)}
					<span className="text-foreground/80 truncate max-w-36 font-medium">
						{task.title}
					</span>
				</nav>
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
				{t("project.interactions.taskDetail.createdAt", {
					date: formatDate(task.created_at),
				})}
			</span>

			<div className="ml-auto flex items-center gap-1">
				<button
					type="button"
					onClick={handleShare}
					className={cn(
						"flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-sm font-medium transition-all duration-150",
						linkCopied
							? "text-emerald-500 bg-emerald-500/10"
							: "text-muted-foreground/70 hover:text-foreground hover:bg-muted/60",
					)}
				>
					{linkCopied ? (
						<>
							<Check className="size-3 text-emerald-500" />
							<span>{t("project.interactions.taskDetail.copied")}</span>
						</>
					) : (
						<>
							<Share2 className="size-3" />
							<span>{t("project.interactions.taskDetail.share")}</span>
						</>
					)}
				</button>

				{canDelete && (
					<DropdownMenu>
						<DropdownMenuTrigger
							className="flex size-7 items-center justify-center rounded-md text-muted-foreground/60 hover:text-foreground hover:bg-muted/60 transition-all duration-150"
							aria-label={t("project.interactions.taskDetail.taskActions")}
						>
							<MoreVertical className="size-4" />
						</DropdownMenuTrigger>
						<DropdownMenuContent align="end" className="w-40">
							<DropdownMenuItem
								className="text-destructive focus:text-destructive"
								onClick={() => setConfirmDelete(true)}
							>
								<Trash2 className="size-3.5 mr-2" />
								{t("common.delete")}
							</DropdownMenuItem>
						</DropdownMenuContent>
					</DropdownMenu>
				)}

				{mode === "modal" && (
					<button
						type="button"
						onClick={onClose}
						className="flex size-7 items-center justify-center rounded-md text-muted-foreground/60 hover:text-foreground hover:bg-muted/60 transition-all duration-150"
						aria-label={t("project.interactions.taskDetail.closeTaskDetail")}
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
							{t("project.interactions.taskDetail.deleteTaskTitle")}
						</DialogTitle>
						<DialogDescription>
							{t("project.interactions.taskDetail.deleteTaskDesc", {
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
							{t("project.interactions.taskDetail.cancel")}
						</Button>
						<Button
							variant="destructive"
							onClick={() => deleteMutation.mutate()}
							disabled={deleteMutation.isPending}
						>
							{deleteMutation.isPending ? (
								<Loader2 className="size-4 animate-spin" />
							) : (
								t("common.delete")
							)}
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</div>
	);
}
