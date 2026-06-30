import { Download, Trash2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { TaskAttachment } from "@/lib/attachment-api";
import { getAttachmentDownloadURL } from "@/lib/attachment-api";
import { cn } from "@/lib/utils";
import { timeAgo } from "./helpers";

interface AttachmentItemProps {
	attachment: TaskAttachment;
	projectId: string;
	taskId: string;
	canDelete?: boolean;
	onDelete?: (id: string) => void;
}

function formatBytes(bytes: number): string {
	if (bytes < 1024) return `${bytes} B`;
	if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
	return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

export function AttachmentItem({
	attachment,
	projectId,
	taskId,
	canDelete,
	onDelete,
}: AttachmentItemProps) {
	const { t } = useTranslation();
	const [isDownloading, setIsDownloading] = useState(false);
	const ext =
		attachment.file.file_name.split(".").pop()?.toUpperCase() ?? "FILE";

	const handlePreview = async () => {
		try {
			const url = await getAttachmentDownloadURL(
				projectId,
				taskId,
				attachment.id,
			);
			window.open(url, "_blank", "noopener,noreferrer");
		} catch {
			// silently ignore — user can retry by clicking again
		}
	};

	const handleDownload = async () => {
		if (isDownloading) return;
		setIsDownloading(true);
		try {
			const url = await getAttachmentDownloadURL(
				projectId,
				taskId,
				attachment.id,
				{ download: true },
			);
			window.open(url, "_blank", "noopener,noreferrer");
		} finally {
			setIsDownloading(false);
		}
	};

	return (
		<div className="group/att flex items-center gap-3.5 rounded-xl border border-border/20 bg-muted/15 px-4 py-3 transition-all duration-150 hover:bg-muted/25 hover:border-border/35">
			<button
				type="button"
				onClick={handlePreview}
				className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-linear-to-br from-primary/12 to-primary/5 text-primary/80 hover:from-primary/20 hover:to-primary/10 transition-all duration-150"
				aria-label={t("project.interactions.taskDetail.attachments.preview", {
					name: attachment.file.file_name,
				})}
			>
				<span className="text-xs font-bold tracking-tight">{ext}</span>
			</button>
			<button
				type="button"
				onClick={handlePreview}
				className="flex-1 min-w-0 text-left"
			>
				<p className="text-sm font-medium text-foreground truncate">
					{attachment.file.file_name}
				</p>
				<p className="text-xs text-muted-foreground/60 mt-0.5">
					{formatBytes(attachment.file.file_size)} ·{" "}
					{timeAgo(attachment.created_at)}
				</p>
			</button>
			<div
				className={cn(
					"flex items-center gap-1 opacity-0 group-hover/att:opacity-100 transition-opacity duration-150",
				)}
			>
				<button
					type="button"
					onClick={handleDownload}
					disabled={isDownloading}
					className="shrink-0 size-7 flex items-center justify-center rounded-lg text-muted-foreground/45 hover:text-foreground hover:bg-muted/50 transition-all duration-150 disabled:opacity-50"
					aria-label={t("project.interactions.taskDetail.attachments.download", {
						name: attachment.file.file_name,
					})}
				>
					<Download className="size-3.5" />
				</button>
				{canDelete && (
					<button
						type="button"
						onClick={() => onDelete?.(attachment.id)}
						className="shrink-0 size-7 flex items-center justify-center rounded-lg text-muted-foreground/45 hover:text-destructive hover:bg-destructive/8 transition-all duration-150"
						aria-label={t("project.interactions.taskDetail.attachments.delete", {
							name: attachment.file.file_name,
						})}
					>
						<Trash2 className="size-3.5" />
					</button>
				)}
			</div>
		</div>
	);
}
