import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
	ChevronRight,
	FolderCheck,
	Folder as FolderIcon,
	Loader2,
} from "lucide-react";
import { useState } from "react";
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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
	addFolder,
	type EnvironmentFolder,
	type EnvironmentStatus,
	environmentBrowseQueryOptions,
	environmentFoldersQueryOptions,
	environmentQueryOptions,
	environmentsQueryOptions,
} from "@/lib/environment-api";
import { cn } from "@/lib/utils";

// Shared by both call sites that create a folder — the environment
// detail page's Folders tab, and the conversation composer's folder
// picker ("Create new folder…") — genuinely shared, not duplicated, since
// this owns real logic (directory-browse navigation state, loading/error
// handling), unlike the tiny leaf components elsewhere in this feature
// area that stay duplicated per-file on purpose.
//
// Folders are path-only: no name/repo-clone-URL/branch (those fields were
// dropped before ever shipping — see environment-api.ts's EnvironmentFolder
// doc comment). ENVIRONMENT_HOME_ROOT mirrors agent-runner's own
// environmentHomeRoot constant (internal/acpbridge/environment_handlers.go)
// — the fixed root the browse endpoint is scoped to.
const ENVIRONMENT_HOME_ROOT = "/home/paca/workspaces";

export function FolderCreateDialog({
	projectId,
	environmentId,
	environmentStatus,
	open,
	onOpenChange,
	onCreated,
}: {
	projectId: string;
	environmentId: string;
	environmentStatus: EnvironmentStatus;
	open: boolean;
	onOpenChange: (open: boolean) => void;
	onCreated?: (folder: EnvironmentFolder) => void;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const [path, setPath] = useState("");
	const [browseOpen, setBrowseOpen] = useState(false);
	const [browsePath, setBrowsePath] = useState(ENVIRONMENT_HOME_ROOT);

	const isRunning = environmentStatus === "running";

	const browseQuery = useQuery({
		...environmentBrowseQueryOptions(projectId, environmentId, browsePath),
		enabled: open && browseOpen && isRunning,
	});

	const reset = () => {
		setPath("");
		setBrowseOpen(false);
		setBrowsePath(ENVIRONMENT_HOME_ROOT);
	};

	const handleClose = (v: boolean) => {
		if (!v) reset();
		onOpenChange(v);
	};

	const addMutation = useMutation({
		mutationFn: () =>
			addFolder(projectId, environmentId, { path: path.trim() }),
		onSuccess: (folder) => {
			qc.invalidateQueries({
				queryKey: environmentsQueryOptions(projectId).queryKey,
			});
			qc.invalidateQueries({
				queryKey: environmentFoldersQueryOptions(projectId, environmentId)
					.queryKey,
			});
			qc.invalidateQueries({
				queryKey: environmentQueryOptions(projectId, environmentId).queryKey,
			});
			handleClose(false);
			onCreated?.(folder);
		},
	});

	const trimmedPath = path.trim();
	const isValidPath = trimmedPath.startsWith("/");

	const relSegments = browsePath.startsWith(ENVIRONMENT_HOME_ROOT)
		? browsePath.slice(ENVIRONMENT_HOME_ROOT.length).split("/").filter(Boolean)
		: [];
	const directoryEntries = (browseQuery.data?.entries ?? []).filter(
		(e) => e.is_dir,
	);

	return (
		<Dialog open={open} onOpenChange={handleClose}>
			<DialogContent className="sm:max-w-lg">
				<DialogHeader>
					<DialogTitle className="flex items-center gap-2">
						<FolderIcon className="size-4 text-primary" />
						{t("environments.detail.folders.addDialog.title")}
					</DialogTitle>
					<DialogDescription>
						{t("environments.detail.folders.addDialog.description")}
					</DialogDescription>
				</DialogHeader>

				<div className="space-y-4 py-2">
					<div className="space-y-1.5">
						<Label>
							{t("environments.detail.folders.addDialog.pathLabel")}
						</Label>
						<Input
							placeholder={t(
								"environments.detail.folders.addDialog.pathPlaceholder",
							)}
							className="font-mono text-xs"
							value={path}
							onChange={(e) => setPath(e.target.value)}
							autoFocus
						/>
						<p className="text-xs text-muted-foreground">
							{t("environments.detail.folders.addDialog.pathHint")}
						</p>
					</div>

					<div>
						<button
							type="button"
							onClick={() => setBrowseOpen((s) => !s)}
							className="flex items-center gap-1 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors"
						>
							<ChevronRight
								className={cn(
									"size-3.5 transition-transform duration-150",
									browseOpen && "rotate-90",
								)}
							/>
							{t("environments.detail.folders.addDialog.browse")}
						</button>

						{browseOpen &&
							(!isRunning ? (
								<p className="mt-3 text-xs text-muted-foreground">
									{t(
										"environments.detail.folders.addDialog.browsing.notRunning",
									)}
								</p>
							) : (
								<div className="mt-3 space-y-2">
									<div className="flex flex-wrap items-center gap-1 text-xs text-muted-foreground">
										<button
											type="button"
											className="hover:text-foreground underline-offset-2 hover:underline"
											onClick={() => setBrowsePath(ENVIRONMENT_HOME_ROOT)}
										>
											{t("environments.detail.folders.addDialog.browsing.root")}
										</button>
										{relSegments.map((seg, i) => {
											const segmentPath = `${ENVIRONMENT_HOME_ROOT}/${relSegments.slice(0, i + 1).join("/")}`;
											return (
												<span
													key={segmentPath}
													className="flex items-center gap-1"
												>
													<span>/</span>
													<button
														type="button"
														className="hover:text-foreground underline-offset-2 hover:underline"
														onClick={() => setBrowsePath(segmentPath)}
													>
														{seg}
													</button>
												</span>
											);
										})}
									</div>

									<div className="max-h-48 overflow-y-auto rounded-md border border-border/60">
										{browseQuery.isLoading ? (
											<div className="flex items-center justify-center p-4">
												<Loader2 className="size-4 animate-spin text-muted-foreground" />
											</div>
										) : directoryEntries.length === 0 ? (
											<p className="p-3 text-xs text-muted-foreground">
												{t(
													"environments.detail.folders.addDialog.browsing.empty",
												)}
											</p>
										) : (
											directoryEntries.map((entry) => (
												<button
													key={entry.name}
													type="button"
													onClick={() =>
														setBrowsePath(`${browsePath}/${entry.name}`)
													}
													className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-accent"
												>
													<FolderIcon className="size-3.5 text-muted-foreground shrink-0" />
													<span className="truncate">{entry.name}</span>
												</button>
											))
										)}
									</div>

									<Button
										type="button"
										size="sm"
										variant="outline"
										onClick={() => setPath(browsePath)}
									>
										<FolderCheck className="size-3.5 mr-1.5" />
										{t(
											"environments.detail.folders.addDialog.browsing.useThisFolder",
										)}
									</Button>
								</div>
							))}
					</div>

					{addMutation.isError && (
						<p className="text-sm text-destructive rounded-md bg-destructive/10 px-3 py-2">
							{t("environments.detail.folders.addDialog.addFailed")}
						</p>
					)}
				</div>

				<DialogFooter>
					<Button variant="outline" onClick={() => handleClose(false)}>
						{t("environments.detail.folders.addDialog.cancel")}
					</Button>
					<Button
						onClick={() => addMutation.mutate()}
						disabled={!isValidPath || addMutation.isPending}
					>
						{addMutation.isPending ? (
							<Loader2 className="size-4 animate-spin" />
						) : (
							t("environments.detail.folders.addDialog.addFolder")
						)}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
