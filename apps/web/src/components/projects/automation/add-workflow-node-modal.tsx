import { Search, Workflow, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { listAllTasks, type Task } from "@/lib/interaction-api";

interface AddWorkflowNodeModalProps {
	open: boolean;
	onClose: () => void;
	onAdd: (task: Task) => void;
	projectId: string;
	taskIdPrefix?: string;
	excludeTaskIds: Set<string>;
}

// The task list API caps page_size at 200, so paginate through the full
// project — mirrors AddTaskLinkModal's fetchAllProjectTasks.
const MAX_TASK_PAGES = 25;

async function fetchAllProjectTasks(projectId: string): Promise<Task[]> {
	const all: Task[] = [];
	let cursor: string | undefined;
	for (let page = 0; page < MAX_TASK_PAGES; page++) {
		const result = await listAllTasks(projectId, { pageSize: 200, cursor });
		all.push(...result.items);
		const next = result.next_cursor;
		if (!next) break;
		cursor = next;
	}
	return all;
}

export function AddWorkflowNodeModal({
	open,
	onClose,
	onAdd,
	projectId,
	taskIdPrefix = "",
	excludeTaskIds,
}: AddWorkflowNodeModalProps) {
	const { t } = useTranslation("projects");
	const [query, setQuery] = useState("");
	const [tasks, setTasks] = useState<Task[]>([]);
	const [loading, setLoading] = useState(false);
	const searchRef = useRef<HTMLInputElement>(null);

	useEffect(() => {
		if (!open) return;
		setLoading(true);
		fetchAllProjectTasks(projectId)
			.then(setTasks)
			.catch(() => setTasks([]))
			.finally(() => setLoading(false));
		setTimeout(() => searchRef.current?.focus(), 50);
	}, [open, projectId]);

	const filteredTasks = useMemo(() => {
		const q = query.trim().toLowerCase();
		return tasks.filter((task) => {
			if (excludeTaskIds.has(task.id)) return false;
			if (!q) return true;
			const prefix = taskIdPrefix
				? `${taskIdPrefix}-${task.task_number}`
				: String(task.task_number);
			return (
				task.title.toLowerCase().includes(q) || prefix.toLowerCase().includes(q)
			);
		});
	}, [tasks, query, excludeTaskIds, taskIdPrefix]);

	if (!open) return null;

	return (
		// biome-ignore lint/a11y/noStaticElementInteractions: backdrop element for modal
		<div
			className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm"
			onClick={(e) => {
				if (e.target === e.currentTarget) onClose();
			}}
			onKeyDown={(e) => {
				if (e.key === "Escape") onClose();
			}}
		>
			<div className="bg-card border border-border/40 rounded-2xl shadow-2xl w-full max-w-lg mx-4 overflow-hidden">
				<div className="flex items-center justify-between px-5 py-4 border-b border-border/20">
					<div className="flex items-center gap-2.5">
						<div className="size-7 rounded-lg bg-primary/10 flex items-center justify-center">
							<Workflow className="size-3.5 text-primary" />
						</div>
						<h2 className="text-base font-semibold text-foreground">
							{t("automation.addNodeModal.title")}
						</h2>
					</div>
					<button
						type="button"
						onClick={onClose}
						className="size-7 rounded-lg flex items-center justify-center text-muted-foreground/60 hover:text-foreground hover:bg-muted/30 transition-all duration-150"
					>
						<X className="size-4" />
					</button>
				</div>

				<div className="px-5 pt-4 pb-3">
					<div className="relative">
						<Search className="absolute left-3 top-1/2 -translate-y-1/2 size-3.5 text-muted-foreground/50" />
						<input
							ref={searchRef}
							type="text"
							value={query}
							onChange={(e) => setQuery(e.target.value)}
							placeholder={t("automation.addNodeModal.searchPlaceholder")}
							className="w-full pl-9 pr-3 py-2.5 rounded-lg border border-border/30 bg-muted/20 text-sm placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-primary/20 focus:border-primary/40 transition-all duration-150"
						/>
					</div>
				</div>

				<div className="mx-5 mb-5 rounded-xl border border-border/20 overflow-hidden max-h-64 overflow-y-auto [scrollbar-gutter:stable]">
					{loading && (
						<div className="flex items-center justify-center py-8 text-muted-foreground/50 text-sm">
							{t("automation.addNodeModal.loadingTasks")}
						</div>
					)}
					{!loading && filteredTasks.length === 0 && (
						<div className="flex items-center justify-center py-8 text-muted-foreground/45 text-sm italic">
							{t("automation.addNodeModal.noTasksFound")}
						</div>
					)}
					{!loading &&
						filteredTasks.map((task) => {
							const prefix = taskIdPrefix
								? `${taskIdPrefix}-${task.task_number}`
								: `#${task.task_number}`;
							return (
								<button
									key={task.id}
									type="button"
									onClick={() => onAdd(task)}
									className="w-full flex items-center gap-3 px-4 py-3 text-left hover:bg-muted/30 transition-colors duration-100 border-b border-border/10 last:border-0"
								>
									<span className="shrink-0 text-xs font-mono text-muted-foreground/60 min-w-13">
										{prefix}
									</span>
									<span className="text-sm text-foreground truncate">
										{task.title}
									</span>
								</button>
							);
						})}
				</div>
			</div>
		</div>
	);
}
