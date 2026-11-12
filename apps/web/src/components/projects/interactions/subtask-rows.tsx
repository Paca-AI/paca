import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Spinner } from "@/components/ui/spinner";

import {
	type ChildTaskFilters,
	childTasksInfiniteQueryOptions,
	createTask,
	type Task,
} from "@/lib/interaction-api";
import type { TaskStatus, TaskType } from "@/lib/project-api";

import { AddTaskRow } from "./add-task-row";
import { TaskRow } from "./task-row";

type RowProps = Omit<
	React.ComponentProps<typeof TaskRow>,
	"task" | "onClick" | "depth" | "subtasks" | "showDragHandle" | "isDragging"
>;

interface SubtaskRowsProps {
	projectId: string;
	parent: Task;
	depth: number;
	filters: ChildTaskFilters;
	filtersActive: boolean;
	rowProps: RowProps;
	statuses: TaskStatus[];
	creatableTypes: TaskType[];
	canCreate: boolean;
	isExpanded: (taskId: string) => boolean;
	onToggle: (taskId: string) => void;
	onTaskClick: (task: Task) => void;
}

/**
 * The children of an expanded parent row, fetched lazily with the view's
 * filters and sort. Recurses for children that have children of their own.
 */
export function SubtaskRows({
	projectId,
	parent,
	depth,
	filters,
	filtersActive,
	rowProps,
	statuses,
	creatableTypes,
	canCreate,
	isExpanded,
	onToggle,
	onTaskClick,
}: SubtaskRowsProps) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const query = useInfiniteQuery(
		childTasksInfiniteQueryOptions(projectId, parent.id, filters),
	);
	const children = useMemo(
		() => query.data?.pages.flatMap((p) => p.items) ?? [],
		[query.data],
	);
	const total = parent.subtask_count ?? 0;
	const indent = { paddingLeft: `${36 + Math.min(depth, 3) * 20}px` };

	const handleAdd = async (title: string, typeId: string | null) => {
		const todo = statuses.find((s) => s.category === "todo") ?? statuses[0];
		await createTask(projectId, {
			title,
			task_type_id: typeId,
			status_id: todo?.id ?? null,
			parent_task_id: parent.id,
			sprint_id: parent.sprint_id ?? null,
		});
		await qc.invalidateQueries({ queryKey: ["projects", projectId, "tasks"] });
	};

	if (query.isPending) {
		return (
			<div role="status" aria-label={t("board.subtasks.loading")}>
				{Array.from({ length: Math.min(Math.max(total, 1), 5) }, (_, i) => (
					<div
						// biome-ignore lint/suspicious/noArrayIndexKey: static skeleton
						key={i}
						className="flex items-center gap-3 border-b border-border/20 py-2.5"
						style={indent}
					>
						<div className="h-3 w-16 animate-pulse rounded bg-muted/60" />
						<div className="h-3 flex-1 max-w-xs animate-pulse rounded bg-muted/60" />
					</div>
				))}
			</div>
		);
	}

	if (query.isError) {
		return (
			<div
				className="flex items-center gap-2 border-b border-border/20 py-2.5 text-xs text-destructive"
				style={indent}
			>
				{t("board.subtasks.loadError")}
				<button
					type="button"
					onClick={() => query.refetch()}
					className="font-semibold underline underline-offset-2"
				>
					{t("board.subtasks.retry")}
				</button>
			</div>
		);
	}

	return (
		<>
			{filtersActive && total > 0 && children.length < total && (
				<div
					className="border-b border-border/20 py-1.5 text-xs text-muted-foreground/70"
					style={indent}
				>
					{t("board.subtasks.matching", {
						n: children.length,
						total,
					})}
				</div>
			)}
			{children.length === 0 && total > 0 && (
				<div
					className="border-b border-border/20 py-2.5 text-xs text-muted-foreground/60"
					style={indent}
				>
					{t("board.subtasks.noMatch")}
				</div>
			)}
			{children.map((child) => {
				const isOpen = isExpanded(child.id);
				return (
					<div key={child.id} data-task-id={child.id}>
						<TaskRow
							{...rowProps}
							task={child}
							depth={depth}
							onClick={() => onTaskClick(child)}
							subtasks={{
								expanded: isOpen,
								onToggle: () => onToggle(child.id),
							}}
						/>
						{isOpen && (child.subtask_count ?? 0) > 0 && (
							<SubtaskRows
								projectId={projectId}
								parent={child}
								depth={depth + 1}
								filters={filters}
								filtersActive={filtersActive}
								rowProps={rowProps}
								statuses={statuses}
								creatableTypes={creatableTypes}
								canCreate={canCreate}
								isExpanded={isExpanded}
								onToggle={onToggle}
								onTaskClick={onTaskClick}
							/>
						)}
					</div>
				);
			})}
			{query.hasNextPage && (
				<button
					type="button"
					onClick={() => query.fetchNextPage()}
					disabled={query.isFetchingNextPage}
					className="flex w-full items-center gap-2 border-b border-border/20 py-2 text-xs font-semibold text-muted-foreground hover:text-foreground"
					style={indent}
				>
					{query.isFetchingNextPage && (
						<Spinner aria-hidden="true" className="size-3" />
					)}
					{t("board.subtasks.loadMore")}
				</button>
			)}
			{canCreate && creatableTypes.length > 0 && (
				<div style={{ paddingLeft: `${40 + Math.min(depth, 3) * 20}px` }}>
					<AddTaskRow
						variant="list"
						taskTypes={creatableTypes}
						onAdd={handleAdd}
						label={t("board.subtasks.add")}
						placeholder={t("board.subtasks.addPlaceholder")}
					/>
				</div>
			)}
		</>
	);
}
