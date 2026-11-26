import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { FolderKanban, Loader2, Search } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { TaskTypeMark } from "@/components/projects/task-types/task-type-mark";
import { EntityAvatar } from "@/components/shared/entity-avatar";
import { HighlightMatch } from "@/components/shared/highlight-match";
import {
	Command,
	CommandDialog,
	CommandGroup,
	CommandInput,
	CommandItem,
	CommandList,
} from "@/components/ui/command";
import { KbdChord } from "@/components/ui/kbd";
import { useDebouncedCallback } from "@/hooks/use-debounced-callback";
import { usePermissions } from "@/hooks/use-permissions";
import { projectAvatar } from "@/lib/provider-logos";
import {
	type SearchPerson,
	type SearchProject,
	type SearchTask,
	searchQueryOptions,
} from "@/lib/search-api";
import { useGlobalSearchStore } from "@/lib/shortcuts/global-search-store";

const SEARCH_CHORD = { mod: true, key: "K" } as const;

/**
 * The header search box and the dialog it opens (also on Mod+K, see
 * ShortcutProvider). Searches projects, tasks and people through GET /search,
 * which only returns what the caller may see, so nothing is filtered here.
 */
export function GlobalSearch() {
	const { t } = useTranslation("appShell");
	const open = useGlobalSearchStore((s) => s.open);
	const setOpen = useGlobalSearchStore((s) => s.setOpen);

	return (
		<>
			<button
				type="button"
				onClick={() => setOpen(true)}
				aria-label={t("search.button")}
				className="flex h-8 w-full max-w-xs items-center gap-2 rounded-lg border border-border/70 bg-muted/40 px-2.5 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
			>
				<Search className="size-4 shrink-0" />
				<span className="truncate">{t("search.placeholder")}</span>
				<KbdChord
					chord={SEARCH_CHORD}
					className="ml-auto hidden md:inline-flex"
				/>
			</button>
			<CommandDialog
				open={open}
				onOpenChange={setOpen}
				title={t("search.dialogTitle")}
				description={t("search.dialogDescription")}
				className="sm:max-w-xl"
			>
				{/* Mounted only while open, so each opening starts empty. */}
				{open && <SearchPanel onDone={() => setOpen(false)} />}
			</CommandDialog>
		</>
	);
}

function SearchPanel({ onDone }: { onDone: () => void }) {
	const { t } = useTranslation("appShell");
	const navigate = useNavigate();
	const { hasPermission } = usePermissions();
	const canManageUsers = hasPermission("users:write");

	const [query, setQuery] = useState("");
	const [debounced, setDebounced] = useState("");
	const applyQuery = useDebouncedCallback(setDebounced, 200);
	const term = debounced.trim();

	const { data, isFetching, isError } = useQuery({
		...searchQueryOptions(term),
		placeholderData: keepPreviousData,
	});
	const results = term ? data : undefined;

	// cmdk only picks an active item on its own when items mount with the
	// list; these arrive later, so highlight the first one (what Enter opens)
	// each time new results come in.
	const firstValue = results
		? results.tasks[0]
			? `task:${results.tasks[0].id}`
			: results.projects[0]
				? `project:${results.projects[0].id}`
				: results.people[0]
					? `person:${results.people[0].id}`
					: ""
		: "";
	const [active, setActive] = useState("");
	useEffect(() => {
		setActive(firstValue);
	}, [firstValue]);
	const total = results
		? results.projects.length + results.tasks.length + results.people.length
		: 0;

	const openProject = (p: SearchProject) => {
		onDone();
		void navigate({ to: "/projects/$projectId", params: { projectId: p.id } });
	};
	const openTask = (task: SearchTask) => {
		onDone();
		void navigate({
			to: "/projects/$projectId/tasks/$taskId",
			params: { projectId: task.project_id, taskId: task.id },
		});
	};
	const openPerson = (person: SearchPerson) => {
		onDone();
		if (person.project_id) {
			void navigate({
				to: "/projects/$projectId/team",
				params: { projectId: person.project_id },
			});
		} else {
			void navigate({ to: "/admin/users" });
		}
	};

	return (
		<Command shouldFilter={false} loop value={active} onValueChange={setActive}>
			<CommandInput
				value={query}
				onValueChange={(value) => {
					setQuery(value);
					applyQuery(value);
				}}
				placeholder={t("search.inputPlaceholder")}
				aria-label={t("search.inputPlaceholder")}
			/>
			<CommandList className="max-h-[min(60vh,28rem)]">
				{!query.trim() && (
					<p className="px-3 py-6 text-center text-sm text-muted-foreground">
						{t("search.hint")}
					</p>
				)}
				{query.trim() && isError && (
					<p
						role="alert"
						className="px-3 py-6 text-center text-sm text-destructive"
					>
						{t("search.error")}
					</p>
				)}
				{query.trim() && !isError && !results && (
					<p className="flex items-center justify-center gap-2 px-3 py-6 text-sm text-muted-foreground">
						<Loader2 className="size-4 animate-spin" />
						{t("search.searching")}
					</p>
				)}
				{results && total === 0 && !isFetching && (
					<p className="px-3 py-6 text-center text-sm text-muted-foreground">
						{t("search.empty", { query: results.query })}
					</p>
				)}

				{results && results.tasks.length > 0 && (
					<CommandGroup heading={t("search.groups.tasks")}>
						{results.tasks.map((task) => (
							<CommandItem
								key={task.id}
								value={`task:${task.id}`}
								onSelect={() => openTask(task)}
								className="gap-3"
							>
								<TaskTypeMark
									name={task.type_name}
									icon={task.type_icon}
									color={task.type_color}
								/>
								<span className="w-16 shrink-0 truncate font-mono text-xs text-muted-foreground">
									{task.task_key}
								</span>
								<span className="min-w-0 flex-1 truncate">
									<HighlightMatch text={task.title} query={results.query} />
								</span>
								<span className="ml-2 hidden max-w-52 shrink-0 truncate text-xs text-muted-foreground sm:inline">
									{task.status_name
										? `${task.project_name} · ${task.status_name}`
										: task.project_name}
								</span>
							</CommandItem>
						))}
					</CommandGroup>
				)}

				{results && results.projects.length > 0 && (
					<CommandGroup heading={t("search.groups.projects")}>
						{results.projects.map((project) => (
							<CommandItem
								key={project.id}
								value={`project:${project.id}`}
								onSelect={() => openProject(project)}
								className="gap-3"
							>
								<EntityAvatar
									{...projectAvatar(project)}
									fallback={<FolderKanban className="size-3" />}
									size="sm"
									shape="rounded"
								/>
								<span className="min-w-0 flex-1 truncate">
									<HighlightMatch text={project.name} query={results.query} />
								</span>
								{project.task_id_prefix && (
									<span className="shrink-0 font-mono text-xs text-muted-foreground">
										{project.task_id_prefix.toUpperCase()}
									</span>
								)}
							</CommandItem>
						))}
					</CommandGroup>
				)}

				{results && results.people.length > 0 && (
					<CommandGroup heading={t("search.groups.people")}>
						{results.people.map((person) => (
							<CommandItem
								key={person.id}
								value={`person:${person.id}`}
								onSelect={() => openPerson(person)}
								disabled={!person.project_id && !canManageUsers}
								className="gap-3"
							>
								<EntityAvatar
									name={person.name || person.username}
									avatarUrl={person.avatar_thumb_url}
									size="sm"
								/>
								<span className="min-w-0 flex-1 truncate">
									<HighlightMatch text={person.name} query={results.query} />
								</span>
								<span className="shrink-0 text-xs text-muted-foreground">
									@{person.username}
								</span>
							</CommandItem>
						))}
					</CommandGroup>
				)}
			</CommandList>
		</Command>
	);
}
