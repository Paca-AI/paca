import { Link2, Search, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
	type DisplayLinkType,
	type LinkType,
	listAllTasks,
	type Task,
} from "@/lib/interaction-api";

export interface AddTaskLinkPayload {
	sourceTaskId: string;
	targetTaskId: string;
	linkType: LinkType;
}

interface AddTaskLinkModalProps {
	open: boolean;
	onClose: () => void;
	onAdd: (payload: AddTaskLinkPayload) => void;
	projectId: string;
	currentTaskId: string;
	taskIdPrefix?: string;
}

const LINK_TYPE_OPTIONS = [
	{
		value: "blocks",
		labelKey: "taskDetail.addTaskLinkModal.linkTypes.blocks.label",
		descriptionKey: "taskDetail.addTaskLinkModal.linkTypes.blocks.description",
	},
	{
		value: "is_blocked_by",
		labelKey: "taskDetail.addTaskLinkModal.linkTypes.isBlockedBy.label",
		descriptionKey:
			"taskDetail.addTaskLinkModal.linkTypes.isBlockedBy.description",
	},
	{
		value: "relates_to",
		labelKey: "taskDetail.addTaskLinkModal.linkTypes.relatesTo.label",
		descriptionKey:
			"taskDetail.addTaskLinkModal.linkTypes.relatesTo.description",
	},
	{
		value: "duplicates",
		labelKey: "taskDetail.addTaskLinkModal.linkTypes.duplicates.label",
		descriptionKey:
			"taskDetail.addTaskLinkModal.linkTypes.duplicates.description",
	},
] as const satisfies {
	value: DisplayLinkType;
	labelKey: string;
	descriptionKey: string;
}[];

// Maps the display type chosen in the UI to the canonical (source, target)
// orientation the API stores. "is_blocked_by" is the only option where the
// other task is the source: the API only has "blocks", so "X is blocked by
// Y" is created as source=Y, target=currentTask, link_type=blocks.
const DISPLAY_TO_CANONICAL: Partial<
	Record<DisplayLinkType, { linkType: LinkType; otherTaskIsSource: boolean }>
> = {
	blocks: { linkType: "blocks", otherTaskIsSource: false },
	is_blocked_by: { linkType: "blocks", otherTaskIsSource: true },
	relates_to: { linkType: "relates_to", otherTaskIsSource: false },
	duplicates: { linkType: "duplicates", otherTaskIsSource: false },
};

// The task list API caps page_size at 200, so the search box needs to page
// through the full project rather than fetching a single page - otherwise
// tasks past the first 200 are invisible to the search.
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

export function AddTaskLinkModal({
	open,
	onClose,
	onAdd,
	projectId,
	currentTaskId,
	taskIdPrefix = "",
}: AddTaskLinkModalProps) {
	const { t } = useTranslation("projects");
	const [selectedLinkType, setSelectedLinkType] =
		useState<DisplayLinkType>("blocks");
	const [query, setQuery] = useState("");
	const [tasks, setTasks] = useState<Task[]>([]);
	const [loading, setLoading] = useState(false);
	const searchRef = useRef<HTMLInputElement>(null);

	// Load tasks once when modal opens
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
		return tasks.filter((t) => {
			if (t.id === currentTaskId) return false;
			if (!q) return true;
			const prefix = taskIdPrefix
				? `${taskIdPrefix}-${t.task_number}`
				: String(t.task_number);
			return (
				t.title.toLowerCase().includes(q) || prefix.toLowerCase().includes(q)
			);
		});
	}, [tasks, query, currentTaskId, taskIdPrefix]);

	function handleSelect(task: Task) {
		const canonical = DISPLAY_TO_CANONICAL[selectedLinkType];
		if (!canonical) return;
		onAdd({
			sourceTaskId: canonical.otherTaskIsSource ? task.id : currentTaskId,
			targetTaskId: canonical.otherTaskIsSource ? currentTaskId : task.id,
			linkType: canonical.linkType,
		});
	}

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
				{/* Header */}
				<div className="flex items-center justify-between px-5 py-4 border-b border-border/20">
					<div className="flex items-center gap-2.5">
						<div className="size-7 rounded-lg bg-primary/10 flex items-center justify-center">
							<Link2 className="size-3.5 text-primary" />
						</div>
						<h2 className="text-base font-semibold text-foreground">
							{t("taskDetail.addTaskLinkModal.title")}
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

				{/* Link type selector */}
				<div className="px-5 pt-4 pb-3">
					<p className="text-xs font-semibold uppercase tracking-[0.08em] text-muted-foreground/60 mb-2">
						{t("taskDetail.addTaskLinkModal.relationship")}
					</p>
					<div className="grid grid-cols-2 gap-1.5">
						{LINK_TYPE_OPTIONS.map((opt) => (
							<button
								key={opt.value}
								type="button"
								onClick={() => setSelectedLinkType(opt.value)}
								className={`px-3 py-2 rounded-lg text-left text-sm font-medium transition-all duration-150 border ${
									selectedLinkType === opt.value
										? "bg-primary/10 border-primary/30 text-primary"
										: "bg-muted/20 border-border/20 text-muted-foreground hover:bg-muted/40 hover:text-foreground"
								}`}
								title={t(opt.descriptionKey)}
							>
								{t(opt.labelKey)}
							</button>
						))}
					</div>
				</div>

				{/* Search */}
				<div className="px-5 pb-3">
					<div className="relative">
						<Search className="absolute left-3 top-1/2 -translate-y-1/2 size-3.5 text-muted-foreground/50" />
						<input
							ref={searchRef}
							type="text"
							value={query}
							onChange={(e) => setQuery(e.target.value)}
							placeholder={t("taskDetail.addTaskLinkModal.searchPlaceholder")}
							className="w-full pl-9 pr-3 py-2.5 rounded-lg border border-border/30 bg-muted/20 text-sm placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-primary/20 focus:border-primary/40 transition-all duration-150"
						/>
					</div>
				</div>

				{/* Task list */}
				<div className="mx-5 mb-5 rounded-xl border border-border/20 overflow-hidden max-h-64 overflow-y-auto [scrollbar-gutter:stable]">
					{loading && (
						<div className="flex items-center justify-center py-8 text-muted-foreground/50 text-sm">
							{t("taskDetail.addTaskLinkModal.loadingTasks")}
						</div>
					)}
					{!loading && filteredTasks.length === 0 && (
						<div className="flex items-center justify-center py-8 text-muted-foreground/45 text-sm italic">
							{t("taskDetail.addTaskLinkModal.noTasksFound")}
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
									onClick={() => handleSelect(task)}
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
