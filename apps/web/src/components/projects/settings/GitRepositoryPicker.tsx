import { useInfiniteQuery } from "@tanstack/react-query";
import { Loader2, Lock, Search } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { useDebouncedCallback } from "@/hooks/use-debounced-callback";
import {
	type AvailableGitRepository,
	gitProviderMessage,
	listAvailableGitRepositories,
} from "@/lib/git-api";

const SEARCH_DEBOUNCE_MS = 300;

interface GitRepositoryPickerProps {
	projectId: string;
	/** The connection whose repositories are listed. */
	identityId: string;
	/** `host/full_path` (lowercase) of the repositories the project already linked. */
	linked: ReadonlySet<string>;
	onPick: (fullPath: string) => void;
}

export const linkedKey = (host: string, fullPath: string) =>
	`${host}/${fullPath}`.toLowerCase();

/**
 * A searchable, paged list of what a connection sees on the platform. It only
 * helps filling in the repository field: typing a path or URL stays possible,
 * and anything that goes wrong here (a platform that cannot list, an error)
 * leaves the form working as before.
 */
export function GitRepositoryPicker({
	projectId,
	identityId,
	linked,
	onPick,
}: GitRepositoryPickerProps) {
	const { t } = useTranslation("projects");
	const [search, setSearch] = useState("");
	const [query, setQuery] = useState("");
	const applyQuery = useDebouncedCallback(setQuery, SEARCH_DEBOUNCE_MS);

	const list = useInfiniteQuery({
		queryKey: ["git", "projects", projectId, "available", identityId, query],
		queryFn: ({ pageParam }) =>
			listAvailableGitRepositories(projectId, identityId, {
				q: query,
				cursor: pageParam,
			}),
		initialPageParam: "",
		getNextPageParam: (last) => last.next_cursor || undefined,
		retry: false,
	});

	const items: AvailableGitRepository[] =
		list.data?.pages.flatMap((p) => p.items) ?? [];
	const unsupported = list.data ? !list.data.pages[0]?.supported : false;

	// A failed "Load more" keeps the pages already loaded; the error is shown
	// next to the button instead of replacing the list.
	const detail = list.isError ? gitProviderMessage(list.error) : undefined;
	const errorAlert = (
		<div role="alert" className="flex flex-col gap-1 text-sm">
			<p className="text-destructive">{t("project.git.picker.error")}</p>
			{detail ? (
				<p className="text-xs text-muted-foreground">{detail}</p>
			) : null}
		</div>
	);

	let body: React.ReactNode;
	if (list.isPending) {
		body = (
			<div className="flex flex-col gap-1.5" aria-busy="true">
				<Skeleton className="h-8 w-full" />
				<Skeleton className="h-8 w-full" />
			</div>
		);
	} else if (list.isError && !list.data) {
		body = errorAlert;
	} else if (unsupported) {
		body = (
			<p role="status" className="text-sm text-muted-foreground">
				{t("project.git.picker.unsupported")}
			</p>
		);
	} else if (items.length === 0) {
		body = (
			<p role="status" className="text-sm text-muted-foreground">
				{t("project.git.picker.empty")}
			</p>
		);
	} else {
		body = (
			<ul className="flex max-h-64 flex-col overflow-y-auto rounded-md border border-border/60">
				{items.map((item) => {
					const isLinked = linked.has(linkedKey(item.host, item.full_path));
					return (
						<li
							key={`${item.host}/${item.full_path}`}
							className="border-b border-border/60 last:border-b-0"
						>
							<button
								type="button"
								disabled={isLinked}
								className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-muted/60 disabled:cursor-not-allowed disabled:opacity-60"
								onClick={() => onPick(item.full_path)}
							>
								<span className="min-w-0 flex-1">
									<span className="block truncate font-medium">
										{item.full_path}
									</span>
									{item.description ? (
										<span className="block truncate text-xs text-muted-foreground">
											{item.description}
										</span>
									) : null}
								</span>
								{item.private ? (
									<Lock
										className="size-3.5 shrink-0 text-muted-foreground"
										aria-label={t("project.git.picker.private")}
									/>
								) : null}
								{isLinked ? (
									<Badge variant="secondary">
										{t("project.git.picker.linked")}
									</Badge>
								) : null}
							</button>
						</li>
					);
				})}
			</ul>
		);
	}

	return (
		<div className="flex flex-col gap-2 rounded-lg border border-border/60 bg-muted/20 p-3">
			{unsupported ? null : (
				<div className="relative">
					<Search className="pointer-events-none absolute top-2.5 left-2.5 size-4 text-muted-foreground" />
					<Input
						value={search}
						className="pl-8"
						maxLength={200}
						aria-label={t("project.git.picker.search")}
						placeholder={t("project.git.picker.search")}
						autoComplete="off"
						onChange={(e) => {
							setSearch(e.target.value);
							applyQuery(e.target.value.trim());
						}}
					/>
				</div>
			)}
			{body}
			{list.isError && list.data ? errorAlert : null}
			{list.hasNextPage ? (
				<div>
					<Button
						type="button"
						variant="outline"
						size="sm"
						disabled={list.isFetchingNextPage}
						onClick={() => void list.fetchNextPage()}
					>
						{list.isFetchingNextPage ? (
							<Loader2 className="size-4 animate-spin" />
						) : null}
						{t("project.git.picker.loadMore")}
					</Button>
				</div>
			) : null}
		</div>
	);
}
