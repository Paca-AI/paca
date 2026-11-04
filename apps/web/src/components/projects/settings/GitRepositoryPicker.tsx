import { useInfiniteQuery } from "@tanstack/react-query";
import { Loader2, Lock } from "lucide-react";
import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import {
	type AvailableGitRepository,
	gitProviderMessage,
	listAvailableGitRepositories,
} from "@/lib/git-api";

interface GitRepositoryBrowserDialogProps {
	open: boolean;
	onOpenChange: (open: boolean) => void;
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
 * A dialog with a paged list of what a connection sees on the platform,
 * loaded as the user scrolls. There is no search: not every platform can search
 * all of its list, and a box that only filtered the loaded pages would mislead.
 * It only helps filling in the repository field: typing a path or URL stays possible, and anything
 * that goes wrong here (a platform that cannot list, an error) leaves the form
 * working as before.
 */
export function GitRepositoryBrowserDialog({
	open,
	onOpenChange,
	...props
}: GitRepositoryBrowserDialogProps) {
	const { t } = useTranslation("projects");
	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="flex max-h-[85vh] flex-col sm:max-w-xl">
				<DialogHeader>
					<DialogTitle>{t("project.git.picker.title")}</DialogTitle>
					<DialogDescription>
						{t("project.git.picker.description")}
					</DialogDescription>
				</DialogHeader>
				{open ? <RepositoryList {...props} /> : null}
			</DialogContent>
		</Dialog>
	);
}

function RepositoryList({
	projectId,
	identityId,
	linked,
	onPick,
}: Omit<GitRepositoryBrowserDialogProps, "open" | "onOpenChange">) {
	const { t } = useTranslation("projects");

	const list = useInfiniteQuery({
		queryKey: ["git", "projects", projectId, "available", identityId],
		queryFn: ({ pageParam }) =>
			listAvailableGitRepositories(projectId, identityId, {
				cursor: pageParam,
			}),
		initialPageParam: "",
		getNextPageParam: (last) => last.next_cursor || undefined,
		retry: false,
	});

	const items: AvailableGitRepository[] =
		list.data?.pages.flatMap((p) => p.items) ?? [];
	const unsupported = list.data ? !list.data.pages[0]?.supported : false;

	// A failed next page keeps the pages already loaded; the error is shown
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
				<Skeleton className="h-10 w-full" />
				<Skeleton className="h-10 w-full" />
				<Skeleton className="h-10 w-full" />
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
			<ul className="flex min-h-0 flex-1 flex-col overflow-y-auto rounded-md border border-border/60">
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
				{list.hasNextPage && !list.isError ? (
					<NextPageSentinel
						loading={list.isFetchingNextPage}
						onVisible={() => void list.fetchNextPage()}
					/>
				) : null}
			</ul>
		);
	}

	return (
		<div className="flex min-h-0 flex-col gap-3">
			{body}
			{list.isError && list.data ? errorAlert : null}
			{list.isError && list.data ? (
				<div>
					<Button
						type="button"
						variant="outline"
						size="sm"
						disabled={list.isFetchingNextPage || list.isRefetching}
						onClick={() =>
							void (list.isFetchNextPageError
								? list.fetchNextPage()
								: list.refetch())
						}
					>
						{t("common.retry", { ns: "common" })}
					</Button>
				</div>
			) : null}
		</div>
	);
}

/**
 * The end of the list: asks for the next page when it scrolls into view. It
 * stays out of the way after a failed page (the caller drops it), so a broken
 * request is not retried in a loop.
 */
function NextPageSentinel({
	loading,
	onVisible,
}: {
	loading: boolean;
	onVisible: () => void;
}) {
	const ref = useRef<HTMLLIElement>(null);
	const onVisibleRef = useRef(onVisible);
	onVisibleRef.current = onVisible;

	useEffect(() => {
		const el = ref.current;
		if (!el || loading || typeof IntersectionObserver === "undefined") return;
		const observer = new IntersectionObserver(
			(entries) => {
				if (entries.some((e) => e.isIntersecting)) onVisibleRef.current();
			},
			{ rootMargin: "120px" },
		);
		observer.observe(el);
		return () => observer.disconnect();
	}, [loading]);

	return (
		<li
			ref={ref}
			aria-hidden="true"
			className="flex h-10 items-center justify-center text-muted-foreground"
		>
			<Loader2 className="size-4 animate-spin" />
		</li>
	);
}
