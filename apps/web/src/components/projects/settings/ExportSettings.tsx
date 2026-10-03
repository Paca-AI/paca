import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Download, FileArchive, Loader2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { NoPermissionState } from "@/components/shared/no-permission-state";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import {
	getExportDownloadUrl,
	isExportActive,
	type ProjectExport,
	projectExportsQueryKey,
	projectExportsQueryOptions,
	requestProjectExport,
	triggerDownload,
} from "@/lib/export-api";
import { formatDate } from "@/lib/format-date";

const DATE_TIME: Intl.DateTimeFormatOptions = {
	year: "numeric",
	month: "short",
	day: "numeric",
	hour: "numeric",
	minute: "2-digit",
};

function formatFileSize(bytes: number): string {
	if (bytes < 1024) return `${bytes} B`;
	if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
	return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

type DisplayStatus = ProjectExport["status"] | "expired";

function displayStatus(e: ProjectExport): DisplayStatus {
	return e.status === "completed" && e.expired ? "expired" : e.status;
}

export function ExportSettings({
	projectId,
	canExport,
}: {
	projectId: string;
	canExport: boolean;
}) {
	const { t } = useTranslation("projects");
	const queryClient = useQueryClient();
	const { data: exports, isLoading } = useQuery({
		...projectExportsQueryOptions(projectId),
		enabled: canExport,
	});
	const [error, setError] = useState<string | null>(null);

	const hasActive = exports?.some(isExportActive) ?? false;

	const requestMutation = useMutation({
		mutationFn: () => requestProjectExport(projectId),
		onSuccess: async () => {
			setError(null);
			await queryClient.invalidateQueries({
				queryKey: projectExportsQueryKey(projectId),
			});
		},
		onError: async (err) => {
			if (getApiErrorCode(err) === ApiErrorCode.ProjectExportInProgress) {
				setError(t("settings.export.errors.inProgress"));
				// Someone else's export is running: show it so this tab polls it.
				await queryClient.invalidateQueries({
					queryKey: projectExportsQueryKey(projectId),
				});
				return;
			}
			setError(t("settings.export.errors.requestFailed"));
		},
	});

	const downloadMutation = useMutation({
		mutationFn: (exportId: string) => getExportDownloadUrl(projectId, exportId),
		onSuccess: (url) => {
			setError(null);
			triggerDownload(url);
		},
		onError: async (err) => {
			setError(
				getApiErrorCode(err) === ApiErrorCode.ProjectExportExpired
					? t("settings.export.errors.expired")
					: t("settings.export.errors.downloadFailed"),
			);
			await queryClient.invalidateQueries({
				queryKey: projectExportsQueryKey(projectId),
			});
		},
	});

	if (!canExport) {
		return (
			<NoPermissionState
				icon={FileArchive}
				title={t("settings.export.noPermission.title")}
				description={t("settings.export.noPermission.description")}
			/>
		);
	}

	const busy = requestMutation.isPending || hasActive;

	return (
		<div className="space-y-6">
			<div className="rounded-xl border border-border/60 bg-card p-6">
				<h3 className="font-[Syne] text-base font-semibold">
					{t("settings.export.title")}
				</h3>
				<p className="text-xs text-muted-foreground mt-0.5">
					{t("settings.export.description")}
				</p>

				<div className="mt-5 flex items-center justify-between gap-4 rounded-lg border border-border/60 p-4">
					<div className="flex items-start gap-3 min-w-0">
						<FileArchive className="size-5 text-muted-foreground mt-0.5 shrink-0" />
						<div className="min-w-0">
							<p className="text-sm font-medium">
								{t("settings.export.archive.title")}
							</p>
							<p className="text-xs text-muted-foreground mt-0.5">
								{t("settings.export.archive.description")}
							</p>
						</div>
					</div>
					<Button
						size="sm"
						className="gap-1.5 shrink-0"
						disabled={busy}
						onClick={() => {
							setError(null);
							requestMutation.mutate();
						}}
					>
						{busy ? (
							<Loader2 className="size-3.5 animate-spin" />
						) : (
							<Download className="size-3.5" />
						)}
						{busy
							? t("settings.export.archive.inProgress")
							: t("settings.export.archive.button")}
					</Button>
				</div>

				{error ? (
					<p
						role="alert"
						className="mt-3 text-xs text-destructive bg-destructive/10 rounded-lg px-3 py-2"
					>
						{error}
					</p>
				) : null}
			</div>

			<div className="rounded-xl border border-border/60 bg-card p-6">
				<h3 className="font-[Syne] text-base font-semibold">
					{t("settings.export.recent.title")}
				</h3>
				{isLoading ? (
					<div className="mt-4 space-y-3">
						<Skeleton className="h-12 w-full" />
						<Skeleton className="h-12 w-full" />
					</div>
				) : !exports || exports.length === 0 ? (
					<p className="text-sm text-muted-foreground mt-3">
						{t("settings.export.recent.empty")}
					</p>
				) : (
					<ul className="mt-4 divide-y divide-border/60 rounded-lg border border-border/60">
						{exports.map((e) => (
							<ExportRow
								key={e.id}
								item={e}
								downloading={
									downloadMutation.isPending &&
									downloadMutation.variables === e.id
								}
								onDownload={() => downloadMutation.mutate(e.id)}
							/>
						))}
					</ul>
				)}
			</div>
		</div>
	);
}

function ExportRow({
	item,
	downloading,
	onDownload,
}: {
	item: ProjectExport;
	downloading: boolean;
	onDownload: () => void;
}) {
	const { t } = useTranslation("projects");
	const status = displayStatus(item);
	const canDownload = status === "completed";

	return (
		<li className="flex items-center justify-between gap-4 px-4 py-3">
			<div className="min-w-0">
				<div className="flex items-center gap-2">
					<span className="text-sm font-medium truncate">
						{item.file_name ?? t("settings.export.archive.title")}
					</span>
					<Badge
						variant={
							status === "failed"
								? "destructive"
								: status === "completed"
									? "default"
									: "secondary"
						}
					>
						{t(`settings.export.status.${status}`)}
					</Badge>
				</div>
				<p className="text-xs text-muted-foreground mt-0.5">
					{formatDate(item.created_at, DATE_TIME)}
					{item.row_count != null
						? ` · ${t("settings.export.rows", { count: item.row_count })}`
						: ""}
					{item.file_size != null ? ` · ${formatFileSize(item.file_size)}` : ""}
					{canDownload && item.expires_at
						? ` · ${t("settings.export.availableUntil", {
								date: formatDate(item.expires_at, DATE_TIME),
							})}`
						: ""}
				</p>
				{status === "failed" ? (
					<p className="text-xs text-destructive mt-1">
						{t("settings.export.failedMessage")}
					</p>
				) : null}
			</div>
			{canDownload ? (
				<Button
					size="sm"
					variant="outline"
					className="gap-1.5 shrink-0"
					disabled={downloading}
					onClick={onDownload}
				>
					{downloading ? (
						<Loader2 className="size-3.5 animate-spin" />
					) : (
						<Download className="size-3.5" />
					)}
					{t("settings.export.download")}
				</Button>
			) : null}
		</li>
	);
}
