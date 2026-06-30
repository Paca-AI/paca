import { Calendar, Flag, GitBranch, User } from "lucide-react";
import { useTranslation } from "react-i18next";

import {
	Sheet,
	SheetContent,
	SheetHeader,
	SheetTitle,
} from "@/components/ui/sheet";
import type { Task } from "@/lib/interaction-api";
import type { TaskStatus, TaskType } from "@/lib/project-api";

import { getImportanceBucket, getPriority } from "./priority";

/** Maps a priority bucket index (0-4) to its translation key. */
const PRIORITY_LABEL_KEYS = [
	"none",
	"low",
	"medium",
	"high",
	"critical",
] as const;

interface TaskDetailPanelProps {
	task: Task | null;
	open: boolean;
	onOpenChange: (open: boolean) => void;
	statuses: TaskStatus[];
	taskTypes: TaskType[];
}

export function TaskDetailPanel({
	task,
	open,
	onOpenChange,
	statuses,
	taskTypes,
}: TaskDetailPanelProps) {
	const { t, i18n } = useTranslation();
	const status = statuses.find((s) => s.id === task?.status_id);
	const taskType = taskTypes.find((tt) => tt.id === task?.task_type_id);
	const priority = getPriority(task?.importance ?? 0);
	const priorityLabel = t(
		`project.interactions.taskDetail.priorities.${PRIORITY_LABEL_KEYS[getImportanceBucket(task?.importance ?? 0)]}`,
	);

	return (
		<Sheet open={open} onOpenChange={onOpenChange}>
			<SheetContent
				side="right"
				className="sm:max-w-md overflow-y-auto gap-0 p-0"
			>
				{task && (
					<>
						<SheetHeader className="border-b border-border/25 px-5 py-4 bg-muted/20 gap-2">
							{/* Type + Status badges */}
							<div className="flex items-center gap-2 flex-wrap">
								{taskType && (
									<span
										className="inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-bold leading-tight tracking-wide border"
										style={{
											borderColor: taskType.color
												? `${taskType.color}44`
												: "var(--border)",
											backgroundColor: taskType.color
												? `${taskType.color}15`
												: "var(--muted)",
											color: taskType.color ?? "inherit",
										}}
									>
										{taskType.name}
									</span>
								)}
								{status && (
									<span className="inline-flex items-center gap-2 rounded-full border border-border/40 bg-muted/40 px-3 py-1 text-xs font-semibold text-muted-foreground tracking-wide">
										<span
											className="size-1.75 rounded-full shrink-0 ring-2 ring-offset-1 ring-offset-background"
											style={{
												background: status.color ?? "var(--muted-foreground)",
												boxShadow: status.color
													? `0 0 6px ${status.color}40`
													: undefined,
											}}
										/>
										{status.name}
									</span>
								)}
							</div>
							<SheetTitle className="font-[Syne] text-lg font-bold leading-snug pr-6 tracking-tight">
								{task.title}
							</SheetTitle>
						</SheetHeader>

						<div className="px-5 py-4 flex flex-col gap-5">
							{/* Properties */}
							<div>
								<h3 className="text-xs font-semibold uppercase tracking-[0.08em] text-muted-foreground/70 mb-3 flex items-center gap-2">
									<span>{t("project.interactions.taskDetail.properties")}</span>
									<div className="flex-1 h-px bg-linear-to-r from-border/40 to-transparent" />
								</h3>
								<div className="flex flex-col gap-0">
									{/* Priority */}
									<div className="grid grid-cols-[9.5rem_1fr] items-center gap-4 py-2.5 px-1 group/field rounded-lg hover:bg-muted/30 transition-colors duration-150">
										<span className="flex items-center gap-1.5 text-sm font-medium text-muted-foreground leading-snug select-none">
											<Flag className="size-3.5 opacity-70" />
											{t("project.interactions.taskDetail.fields.priority")}
										</span>
										<span
											className="inline-flex items-center gap-1.5 text-sm font-medium"
											style={{ color: priority.color }}
										>
											<span
												className="size-2 rounded-full"
												style={{ background: priority.color }}
											/>
											{priorityLabel}
										</span>
									</div>

									{/* Assignee */}
									<div className="grid grid-cols-[9.5rem_1fr] items-center gap-4 py-2.5 px-1 group/field rounded-lg hover:bg-muted/30 transition-colors duration-150">
										<span className="flex items-center gap-1.5 text-sm font-medium text-muted-foreground leading-snug select-none">
											<User className="size-3.5 opacity-70" />
											{t("project.interactions.taskDetail.fields.assignee")}
										</span>
										{task.assignee_id ? (
											<div className="flex items-center gap-2">
												<div className="flex size-6 items-center justify-center rounded-full bg-linear-to-br from-primary/20 to-primary/10 text-primary text-xs font-bold ring-1 ring-primary/20">
													<User className="size-3" />
												</div>
												<span className="text-sm font-medium text-foreground">
													{t("project.interactions.taskDetail.assigned")}
												</span>
											</div>
										) : (
											<span className="text-sm text-muted-foreground/50 italic">
												{t("project.interactions.taskDetail.unassigned")}
											</span>
										)}
									</div>

									{/* Sprint */}
									{task.sprint_id && (
										<div className="grid grid-cols-[9.5rem_1fr] items-center gap-4 py-2.5 px-1 group/field rounded-lg hover:bg-muted/30 transition-colors duration-150">
											<span className="flex items-center gap-1.5 text-sm font-medium text-muted-foreground leading-snug select-none">
												<GitBranch className="size-3.5 opacity-70" />
												{t("project.interactions.taskDetail.fields.sprint")}
											</span>
											<span className="text-sm font-medium text-muted-foreground font-[JetBrains_Mono,monospace] tracking-wider truncate">
												{task.sprint_id}
											</span>
										</div>
									)}

									{/* Created */}
									<div className="grid grid-cols-[9.5rem_1fr] items-center gap-4 py-2.5 px-1 group/field rounded-lg hover:bg-muted/30 transition-colors duration-150">
										<span className="flex items-center gap-1.5 text-sm font-medium text-muted-foreground leading-snug select-none">
											<Calendar className="size-3.5 opacity-70" />
											{t("project.interactions.taskDetail.fields.created")}
										</span>
										<span className="text-sm font-medium text-muted-foreground">
											{new Date(task.created_at).toLocaleDateString(
												i18n.language,
												{
													year: "numeric",
													month: "short",
													day: "numeric",
												},
											)}
										</span>
									</div>

									{/* Updated */}
									<div className="grid grid-cols-[9.5rem_1fr] items-center gap-4 py-2.5 px-1 group/field rounded-lg hover:bg-muted/30 transition-colors duration-150">
										<span className="flex items-center gap-1.5 text-sm font-medium text-muted-foreground leading-snug select-none">
											<Calendar className="size-3.5 opacity-70" />
											{t("project.interactions.taskDetail.fields.updated")}
										</span>
										<span className="text-sm font-medium text-muted-foreground">
											{new Date(task.updated_at).toLocaleDateString(
												i18n.language,
												{
													year: "numeric",
													month: "short",
													day: "numeric",
												},
											)}
										</span>
									</div>
								</div>
							</div>

							{/* Description */}
							{task.description && task.description.length > 0 && (
								<div>
									<h3 className="text-xs font-semibold uppercase tracking-[0.08em] text-muted-foreground/70 mb-3 flex items-center gap-2">
										<span>{t("project.interactions.taskDetail.description")}</span>
										<div className="flex-1 h-px bg-linear-to-r from-border/40 to-transparent" />
									</h3>
									<p className="text-sm text-foreground/80 whitespace-pre-wrap leading-relaxed">
										{(
											task.description as Array<{
												content?: Array<{ type: string; text?: string }>;
											}>
										)
											.flatMap((b) => b.content ?? [])
											.filter((c) => c.type === "text")
											.map((c) => c.text ?? "")
											.join(" ")}
									</p>
								</div>
							)}
						</div>
					</>
				)}
			</SheetContent>
		</Sheet>
	);
}
