import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { ChevronRight, Search } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { LoadMoreSentinel } from "@/components/shared/load-more-sentinel";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { useDebouncedCallback } from "@/hooks/use-debounced-callback";
import {
	type FileCategory,
	type FileNode,
	fileCategory,
	fileNodePathQueryOptions,
	fileNodesQueryOptions,
	formatFileSize,
} from "@/lib/files-api";
import { cn } from "@/lib/utils";
import { FileTypeIcon } from "./file-icon";

/**
 * A compact browser over one project's files, for picking a file to use
 * elsewhere (e.g. embedding it in a description). Folders open in place;
 * files can be narrowed to a category such as images.
 */
export function FilePicker({
	projectId,
	categories,
	accept,
	onPick,
	className,
}: {
	projectId: string;
	/** Only files of these categories are offered; all files when empty. */
	categories?: readonly FileCategory[];
	/** Narrows the files offered further (folders are always shown). */
	accept?: (file: FileNode) => boolean;
	onPick: (file: FileNode) => void;
	className?: string;
}) {
	const { t } = useTranslation("files");
	const scope = { projectId };
	const [folderId, setFolderId] = useState<string | null>(null);
	const [text, setText] = useState("");
	const [query, setQuery] = useState("");
	const pushQuery = useDebouncedCallback(
		(v: string) => setQuery(v.trim()),
		250,
	);

	const list = useInfiniteQuery(
		fileNodesQueryOptions(scope, {
			parentId: folderId,
			q: query || undefined,
			sort: "name",
		}),
	);
	const path = useQuery({
		...fileNodePathQueryOptions(scope, folderId ?? ""),
		enabled: !!folderId && !query,
	});

	const accepted = (n: FileNode) =>
		n.kind !== "doc" &&
		(n.kind === "folder" || !accept || accept(n)) &&
		(n.kind === "folder" ||
			!categories ||
			categories.length === 0 ||
			categories.includes(fileCategory(n) as FileCategory));
	const nodes = (list.data?.pages.flatMap((p) => p.items) ?? []).filter(
		(n) => accepted(n) && !(query && n.kind === "folder"),
	);

	return (
		<div className={cn("flex min-h-0 flex-col gap-2", className)}>
			<div className="relative">
				<Search className="pointer-events-none absolute top-2 left-2.5 size-4 text-muted-foreground" />
				<Input
					value={text}
					placeholder={t("picker.search")}
					aria-label={t("picker.search")}
					className="pl-8"
					onChange={(e) => {
						setText(e.target.value);
						pushQuery(e.target.value);
					}}
				/>
			</div>
			{!query && (
				<nav
					aria-label={t("columns.location")}
					className="flex flex-wrap items-center gap-0.5 text-xs text-muted-foreground"
				>
					<button
						type="button"
						className="rounded px-1 hover:bg-muted hover:text-foreground"
						onClick={() => setFolderId(null)}
					>
						{t("picker.root")}
					</button>
					{(path.data ?? []).map((p) => (
						<span key={p.id} className="flex items-center gap-0.5">
							<ChevronRight className="size-3" />
							<button
								type="button"
								className="max-w-32 truncate rounded px-1 hover:bg-muted hover:text-foreground"
								onClick={() => setFolderId(p.id)}
							>
								{p.name}
							</button>
						</span>
					))}
				</nav>
			)}
			<div className="max-h-60 min-h-32 overflow-y-auto rounded-lg border">
				{list.isLoading ? (
					<div className="space-y-1.5 p-2">
						<Skeleton className="h-8" />
						<Skeleton className="h-8" />
						<Skeleton className="h-8" />
					</div>
				) : nodes.length === 0 ? (
					<p className="p-6 text-center text-sm text-muted-foreground">
						{query ? t("empty.search") : t("picker.empty")}
					</p>
				) : (
					<ul>
						{nodes.map((n) => (
							<li key={n.id}>
								<button
									type="button"
									className="flex w-full items-center gap-3 px-3 py-2 text-left text-sm hover:bg-muted focus-visible:bg-muted focus-visible:outline-none"
									onClick={() =>
										n.kind === "folder" ? setFolderId(n.id) : onPick(n)
									}
								>
									<FileTypeIcon node={n} className="size-5" />
									<span className="min-w-0 flex-1 truncate">{n.name}</span>
									{n.kind === "file" && (
										<span className="shrink-0 text-xs text-muted-foreground">
											{formatFileSize(n.size)}
										</span>
									)}
								</button>
							</li>
						))}
					</ul>
				)}
				<LoadMoreSentinel
					compact
					hasMore={!!list.hasNextPage}
					isLoadingMore={list.isFetchingNextPage}
					isError={list.isFetchNextPageError}
					onLoadMore={() => void list.fetchNextPage()}
				/>
			</div>
		</div>
	);
}
