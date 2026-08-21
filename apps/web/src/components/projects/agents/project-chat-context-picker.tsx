import { useInfiniteQuery } from "@tanstack/react-query";
import { Check, FileText, History, Loader2, Plus, X, Zap } from "lucide-react";
import { useMemo, useState } from "react";
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useTaskPickerSearch } from "@/hooks/use-task-picker-search";
import {
	type ProjectChatContextSourceRef,
	projectChatSessionsQueryOptions,
	projectChatTurnsQueryOptions,
} from "@/lib/agent-api";
import { tasksPickerInfiniteQueryOptions } from "@/lib/interaction-api";
import { mergeRequiredProjectChatSources } from "@/lib/project-chat-navigation";
import { cn } from "@/lib/utils";

export const MAX_USER_CONTEXT_SOURCES = 63;

function sourceKey(source: ProjectChatContextSourceRef): string {
	return `${source.type}:${source.id}`;
}

export function toggleProjectChatContextSource(
	current: ProjectChatContextSourceRef[],
	source: ProjectChatContextSourceRef,
	required: ProjectChatContextSourceRef[] = [],
): ProjectChatContextSourceRef[] {
	const key = sourceKey(source);
	if (required.some((item) => sourceKey(item) === key)) return current;
	if (current.some((item) => sourceKey(item) === key)) {
		return current.filter((item) => sourceKey(item) !== key);
	}
	if (current.length >= MAX_USER_CONTEXT_SOURCES) return current;
	return [...current, source];
}

export function ProjectChatContextPicker({
	projectId,
	value,
	onChange,
	requiredSources = [],
	excludeSessionId,
	canSelectTasks = true,
	compact = false,
	iconOnly = false,
	disabled,
}: {
	projectId: string;
	value: ProjectChatContextSourceRef[];
	onChange: (value: ProjectChatContextSourceRef[]) => void;
	requiredSources?: ProjectChatContextSourceRef[];
	excludeSessionId?: string;
	canSelectTasks?: boolean;
	compact?: boolean;
	iconOnly?: boolean;
	disabled?: boolean;
}) {
	const { t } = useTranslation("projects");
	const [open, setOpen] = useState(false);
	const [draft, setDraft] = useState(value);
	const [runSessionId, setRunSessionId] = useState("");
	const [runSessionSearch, setRunSessionSearch] = useState("");

	const tasksQuery = useInfiniteQuery({
		...tasksPickerInfiniteQueryOptions(projectId),
		enabled: open && canSelectTasks,
	});
	const taskSearch = useTaskPickerSearch(projectId, open && canSelectTasks);
	const sessionsQuery = useInfiniteQuery({
		...projectChatSessionsQueryOptions(projectId),
		enabled: open,
	});
	const runTurnsQuery = useInfiniteQuery({
		...projectChatTurnsQueryOptions(projectId, runSessionId),
		enabled: open && !!runSessionId,
	});

	const tasks = taskSearch.isSearching
		? taskSearch.results
		: (tasksQuery.data?.pages.flatMap((page) => page.items) ?? []);
	const sessions =
		sessionsQuery.data?.pages
			.flatMap((page) => page.items)
			.filter((item) => item.session.id !== excludeSessionId) ?? [];
	const selectedRunSession = sessions.find(
		(item) => item.session.id === runSessionId,
	);
	const runSessions = sessions.filter((item) => {
		const term = runSessionSearch.trim().toLocaleLowerCase();
		if (!term) return true;
		return `${item.session.title ?? ""} ${item.agent_name} @${item.agent_handle}`
			.toLocaleLowerCase()
			.includes(term);
	});
	const runs =
		runTurnsQuery.data?.pages.flatMap((page) =>
			page.items.flatMap((item) =>
				item.runs.map((run) => ({ turn: item.turn, run })),
			),
		) ?? [];

	const selectedKeys = useMemo(() => new Set(draft.map(sourceKey)), [draft]);
	const requiredKeys = useMemo(
		() => new Set(requiredSources.map(sourceKey)),
		[requiredSources],
	);

	const toggle = (source: ProjectChatContextSourceRef) => {
		setDraft((current) =>
			toggleProjectChatContextSource(current, source, requiredSources),
		);
	};

	const openPicker = () => {
		setDraft(mergeRequiredProjectChatSources(value, requiredSources));
		setRunSessionId("");
		setRunSessionSearch("");
		setOpen(true);
	};

	const itemClass = (selected: boolean) =>
		cn(
			"flex w-full items-center gap-3 rounded-lg border px-3 py-2 text-left text-sm transition-colors",
			selected
				? "border-primary/40 bg-primary/5"
				: "border-transparent hover:border-border hover:bg-muted/40",
		);

	return (
		<>
			{iconOnly ? (
				<DropdownMenu>
					<DropdownMenuTrigger
						render={
							<Button
								type="button"
								variant="ghost"
								size="icon-sm"
								className="size-7 rounded-full"
								disabled={disabled}
								aria-label={t("chats.context.add")}
								title={t("chats.context.compact", { count: value.length })}
							/>
						}
					>
						<Plus className="size-4" />
					</DropdownMenuTrigger>
					<DropdownMenuContent
						align="start"
						side="top"
						sideOffset={6}
						className="w-44"
					>
						<DropdownMenuItem onClick={openPicker}>
							<FileText className="size-4" />
							{t("chats.context.add")}
						</DropdownMenuItem>
					</DropdownMenuContent>
				</DropdownMenu>
			) : compact ? (
				<Button
					type="button"
					variant="ghost"
					size="sm"
					className="h-7 gap-1.5 text-xs text-muted-foreground"
					onClick={openPicker}
					disabled={disabled}
				>
					<FileText className="size-3.5" />
					{t("chats.context.compact", { count: value.length })}
				</Button>
			) : (
				<div className="flex flex-wrap items-center gap-1.5">
					{value.map((source) => (
						<span
							key={sourceKey(source)}
							className="inline-flex max-w-56 items-center gap-1 rounded-full border border-border/50 bg-muted/40 px-2 py-1 text-xs text-muted-foreground"
						>
							{source.type === "task" ? (
								<FileText className="size-3" />
							) : source.type === "session" ? (
								<History className="size-3" />
							) : (
								<Zap className="size-3" />
							)}
							<span className="truncate">
								{t(`chats.context.type.${source.type}`)} ·{" "}
								{source.id.slice(0, 8)}
							</span>
							{!disabled && !requiredKeys.has(sourceKey(source)) && (
								<button
									type="button"
									aria-label={t("chats.context.remove")}
									onClick={() =>
										onChange(
											value.filter(
												(item) => sourceKey(item) !== sourceKey(source),
											),
										)
									}
									className="rounded-full p-0.5 hover:bg-muted"
								>
									<X className="size-3" />
								</button>
							)}
						</span>
					))}
					<Button
						type="button"
						variant="outline"
						size="sm"
						className="h-7 gap-1.5 rounded-full text-xs"
						onClick={openPicker}
						disabled={disabled}
					>
						<Plus className="size-3.5" />
						{t("chats.context.add")}
					</Button>
				</div>
			)}

			<Dialog open={open} onOpenChange={setOpen}>
				<DialogContent className="flex max-h-[min(80vh,42rem)] max-w-2xl flex-col overflow-hidden">
					<DialogHeader>
						<DialogTitle>{t("chats.context.title")}</DialogTitle>
						<DialogDescription>
							{t("chats.context.description")}
						</DialogDescription>
						<p className="text-xs text-muted-foreground">
							{t("chats.context.count", {
								count: draft.length,
							})}
						</p>
					</DialogHeader>

					<Tabs
						defaultValue={canSelectTasks ? "task" : "session"}
						className="min-h-0 flex-1"
					>
						<TabsList
							className={cn(
								"grid w-full",
								canSelectTasks ? "grid-cols-3" : "grid-cols-2",
							)}
						>
							{canSelectTasks && (
								<TabsTrigger value="task">
									{t("chats.context.tasks")}
								</TabsTrigger>
							)}
							<TabsTrigger value="session">
								{t("chats.context.sessions")}
							</TabsTrigger>
							<TabsTrigger value="run">{t("chats.context.runs")}</TabsTrigger>
						</TabsList>

						{canSelectTasks && (
							<TabsContent value="task" className="min-h-0">
								<input
									type="search"
									value={taskSearch.search}
									onChange={(event) => taskSearch.setSearch(event.target.value)}
									placeholder={t("chats.context.searchTasks")}
									className="mb-2 w-full rounded-lg border border-border bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-primary/20"
								/>
								<div className="max-h-80 space-y-1 overflow-y-auto">
									{taskSearch.isLoading || tasksQuery.isLoading ? (
										<div className="flex justify-center py-8">
											<Loader2 className="size-4 animate-spin" />
										</div>
									) : (
										tasks.map((task) => {
											const source = { type: "task", id: task.id } as const;
											const selected = selectedKeys.has(sourceKey(source));
											return (
												<button
													type="button"
													key={task.id}
													className={itemClass(selected)}
													onClick={() => toggle(source)}
												>
													<FileText className="size-4 shrink-0" />
													<span className="min-w-0 flex-1 truncate">
														{task.title}
													</span>
													{selected && (
														<Check className="size-4 text-primary" />
													)}
												</button>
											);
										})
									)}
									{!taskSearch.isSearching && tasksQuery.hasNextPage && (
										<Button
											type="button"
											variant="ghost"
											className="w-full"
											onClick={() => void tasksQuery.fetchNextPage()}
											disabled={tasksQuery.isFetchingNextPage}
										>
											{t("chats.context.loadMore")}
										</Button>
									)}
								</div>
							</TabsContent>
						)}

						<TabsContent
							value="session"
							className="max-h-80 space-y-1 overflow-y-auto"
						>
							{sessions.map((item) => {
								const source = {
									type: "session",
									id: item.session.id,
								} as const;
								const selected = selectedKeys.has(sourceKey(source));
								return (
									<button
										type="button"
										key={item.session.id}
										className={itemClass(selected)}
										onClick={() => toggle(source)}
									>
										<History className="size-4 shrink-0" />
										<span className="min-w-0 flex-1 truncate">
											{item.session.title || item.agent_name}
										</span>
										{selected && <Check className="size-4 text-primary" />}
									</button>
								);
							})}
							{sessionsQuery.hasNextPage && (
								<Button
									type="button"
									variant="ghost"
									className="w-full"
									onClick={() => void sessionsQuery.fetchNextPage()}
									disabled={sessionsQuery.isFetchingNextPage}
								>
									{t("chats.context.loadMore")}
								</Button>
							)}
						</TabsContent>

						<TabsContent value="run" className="space-y-2">
							<input
								type="search"
								value={runSessionSearch}
								onChange={(event) => setRunSessionSearch(event.target.value)}
								placeholder={t("chats.search")}
								className="w-full rounded-lg border border-border bg-background px-3 py-2 text-sm"
							/>
							<select
								value={runSessionId}
								onChange={(event) => setRunSessionId(event.target.value)}
								className="w-full rounded-lg border border-border bg-background px-3 py-2 text-sm"
							>
								<option value="">
									{t("chats.context.chooseSessionForRuns")}
								</option>
								{runSessions.map((item) => (
									<option key={item.session.id} value={item.session.id}>
										{item.session.title || item.agent_name}
									</option>
								))}
							</select>
							{sessionsQuery.hasNextPage && (
								<Button
									type="button"
									variant="ghost"
									className="w-full"
									onClick={() => void sessionsQuery.fetchNextPage()}
									disabled={sessionsQuery.isFetchingNextPage}
								>
									{t("chats.context.loadMore")}
								</Button>
							)}
							<div className="max-h-72 space-y-1 overflow-y-auto">
								{runs.map(({ turn, run }) => {
									const source = { type: "run", id: run.id } as const;
									const selected = selectedKeys.has(sourceKey(source));
									return (
										<button
											type="button"
											key={run.id}
											className={itemClass(selected)}
											onClick={() => toggle(source)}
										>
											<Zap className="size-4 shrink-0" />
											<span className="min-w-0 flex-1 truncate">
												{selectedRunSession?.session.title ||
													selectedRunSession?.agent_name}{" "}
												· {t("chats.turnLabel", { index: turn.turn_index })} ·{" "}
												{run.status} · #{run.attempt}
											</span>
											{selected && <Check className="size-4 text-primary" />}
										</button>
									);
								})}
								{runTurnsQuery.hasNextPage && (
									<Button
										type="button"
										variant="ghost"
										className="w-full"
										onClick={() => void runTurnsQuery.fetchNextPage()}
										disabled={runTurnsQuery.isFetchingNextPage}
									>
										{t("chats.context.loadMore")}
									</Button>
								)}
							</div>
						</TabsContent>
					</Tabs>

					<DialogFooter>
						<Button
							type="button"
							variant="outline"
							onClick={() => setOpen(false)}
						>
							{t("chats.context.cancel")}
						</Button>
						<Button
							type="button"
							onClick={() => {
								onChange(
									mergeRequiredProjectChatSources(draft, requiredSources),
								);
								setOpen(false);
							}}
						>
							{t("chats.context.apply", { count: draft.length })}
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</>
	);
}
