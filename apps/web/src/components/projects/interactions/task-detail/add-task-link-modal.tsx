import { keepPreviousData, useInfiniteQuery } from "@tanstack/react-query";
import { Link2, Search } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
	Dialog,
	DialogContent,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import {
	InputGroup,
	InputGroupAddon,
	InputGroupInput,
} from "@/components/ui/input-group";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { useDebouncedCallback } from "@/hooks/use-debounced-callback";
import {
	type DisplayLinkType,
	type LinkType,
	listAllTasks,
	type Task,
} from "@/lib/interaction-api";
import { createLoadMoreScrollHandler } from "@/lib/scroll-pagination";

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

const TASK_PAGE_SIZE = 20;
const SEARCH_DEBOUNCE_MS = 300;
// Hiding the current task can leave a page with few rows (and nothing to
// scroll), so keep loading until a few rows are visible.
const MIN_VISIBLE_TASK_ROWS = 5;

/** The server matches the title and "#<number>", so a full display id such as
 *  "PRJ-12" is rewritten to "#12" to keep matching what users see in the UI. */
function toServerSearch(
	query: string,
	taskIdPrefix: string | undefined,
): string | undefined {
	if (!query) return undefined;
	if (taskIdPrefix) {
		const m = query.match(/^(.+)-(\d+)$/);
		if (m && m[1].toLowerCase() === taskIdPrefix.toLowerCase()) {
			return `#${m[2]}`;
		}
	}
	return query;
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
	const [debouncedQuery, setDebouncedQuery] = useState("");
	const applyQuery = useDebouncedCallback(
		setDebouncedQuery,
		SEARCH_DEBOUNCE_MS,
	);
	const searchRef = useRef<HTMLInputElement>(null);

	useEffect(() => {
		if (!open) {
			setQuery("");
			setDebouncedQuery("");
			// Supersede any pending debounce so it can't restore the old term.
			applyQuery("");
			return;
		}
		const timer = setTimeout(() => searchRef.current?.focus(), 50);
		return () => clearTimeout(timer);
	}, [open, applyQuery]);

	// Cursor-paginated, server-side search (matches title and "#<number>"),
	// so every task in the project is reachable without loading them all.
	const {
		data,
		isLoading: loading,
		isError,
		isFetchNextPageError,
		isPlaceholderData,
		isFetchingNextPage,
		hasNextPage,
		fetchNextPage,
	} = useInfiniteQuery({
		queryKey: ["projects", projectId, "tasks", "link-picker", debouncedQuery],
		queryFn: ({ pageParam }: { pageParam: string | undefined }) =>
			listAllTasks(projectId, {
				search: toServerSearch(debouncedQuery, taskIdPrefix),

				pageSize: TASK_PAGE_SIZE,
				cursor: pageParam,
			}),
		initialPageParam: undefined as string | undefined,
		getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
		enabled: open && !!projectId,
		placeholderData: keepPreviousData,
	});

	const filteredTasks = useMemo<Task[]>(
		() =>
			(data?.pages.flatMap((page) => page.items) ?? []).filter(
				(t) => t.id !== currentTaskId,
			),
		[data, currentTaskId],
	);

	useEffect(() => {
		if (
			hasNextPage &&
			!isFetchingNextPage &&
			!isFetchNextPageError &&
			!isPlaceholderData &&
			!loading &&
			filteredTasks.length < MIN_VISIBLE_TASK_ROWS
		) {
			void fetchNextPage();
		}
	}, [
		hasNextPage,
		isFetchingNextPage,
		isFetchNextPageError,
		isPlaceholderData,
		loading,
		filteredTasks.length,
		fetchNextPage,
	]);

	const handleListScroll = createLoadMoreScrollHandler({
		hasMore: !!hasNextPage,
		isLoadingMore: isFetchingNextPage,
		onLoadMore: () => void fetchNextPage(),
	});

	function handleSelect(task: Task) {
		const canonical = DISPLAY_TO_CANONICAL[selectedLinkType];
		if (!canonical) return;
		onAdd({
			sourceTaskId: canonical.otherTaskIsSource ? task.id : currentTaskId,
			targetTaskId: canonical.otherTaskIsSource ? currentTaskId : task.id,
			linkType: canonical.linkType,
		});
	}

	return (
		<Dialog open={open} onOpenChange={(next) => !next && onClose()}>
			{/* z-60: above the task-detail overlay (z-50), below the floating chat. */}
			<DialogContent
				className="z-60 gap-0 p-0 sm:max-w-lg"
				overlayClassName="z-60"
			>
				<DialogHeader className="border-b border-border/20 px-5 py-4">
					<DialogTitle className="flex items-center gap-2.5 text-base font-semibold">
						<span className="flex size-7 items-center justify-center rounded-lg bg-primary/10">
							<Link2 className="size-3.5 text-primary" />
						</span>
						{t("taskDetail.addTaskLinkModal.title")}
					</DialogTitle>
				</DialogHeader>

				{/* Link type selector */}
				<div className="px-5 pt-4 pb-3">
					<p className="mb-2 text-xs font-semibold uppercase tracking-[0.08em] text-muted-foreground/60">
						{t("taskDetail.addTaskLinkModal.relationship")}
					</p>
					<ToggleGroup
						aria-label={t("taskDetail.addTaskLinkModal.relationship")}
						variant="outline"
						spacing={1.5}
						className="grid w-full grid-cols-2"
						value={[selectedLinkType]}
						onValueChange={(next) => {
							// Re-pressing the active relationship would empty the group.
							const value = next[0];
							if (value) setSelectedLinkType(value as typeof selectedLinkType);
						}}
					>
						{LINK_TYPE_OPTIONS.map((opt) => (
							<ToggleGroupItem
								key={opt.value}
								value={opt.value}
								title={t(opt.descriptionKey)}
								className="h-auto justify-start px-3 py-2 text-left text-sm"
							>
								{t(opt.labelKey)}
							</ToggleGroupItem>
						))}
					</ToggleGroup>
				</div>

				{/* Search */}
				<div className="px-5 pb-3">
					<InputGroup>
						<InputGroupAddon>
							<Search />
						</InputGroupAddon>
						<InputGroupInput
							ref={searchRef}
							type="text"
							value={query}
							onChange={(e) => {
								setQuery(e.target.value);
								applyQuery(e.target.value.trim());
							}}
							placeholder={t("taskDetail.addTaskLinkModal.searchPlaceholder")}
							aria-label={t("taskDetail.addTaskLinkModal.searchPlaceholder")}
						/>
					</InputGroup>
				</div>

				{/* Task list */}
				<div
					className="mx-5 mb-5 max-h-64 overflow-hidden overflow-y-auto rounded-xl border border-border/20 [scrollbar-gutter:stable]"
					onScroll={handleListScroll}
				>
					{loading && (
						<div className="flex items-center justify-center py-8 text-sm text-muted-foreground/50">
							{t("taskDetail.addTaskLinkModal.loadingTasks")}
						</div>
					)}
					{!loading && isError && filteredTasks.length === 0 && (
						<div className="flex items-center justify-center py-8 text-sm text-destructive/80">
							{t("taskDetail.addTaskLinkModal.loadError")}
						</div>
					)}
					{!loading && !isError && filteredTasks.length === 0 && (
						<div className="flex items-center justify-center py-8 text-sm italic text-muted-foreground/45">
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
									className="flex w-full items-center gap-3 border-b border-border/10 px-4 py-3 text-left transition-colors duration-100 last:border-0 hover:bg-muted/30"
								>
									<span className="min-w-13 shrink-0 font-mono text-xs text-muted-foreground/60">
										{prefix}
									</span>
									<span className="truncate text-sm text-foreground">
										{task.title}
									</span>
								</button>
							);
						})}
					{isFetchingNextPage && (
						<div className="flex items-center justify-center py-3 text-xs text-muted-foreground/50">
							{t("taskDetail.addTaskLinkModal.loadingTasks")}
						</div>
					)}
				</div>
			</DialogContent>
		</Dialog>
	);
}
