import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Paperclip, Upload } from "lucide-react";
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
	deleteTaskAttachment,
	taskAttachmentsQueryOptions,
	uploadAttachment,
} from "@/lib/attachment-api";
import { cn } from "@/lib/utils";
import { AttachmentItem } from "./attachment-item";

interface AttachmentsSectionProps {
	projectId: string;
	taskId: string;
	canEdit?: boolean;
}

export function AttachmentsSection({
	projectId,
	taskId,
	canEdit = true,
}: AttachmentsSectionProps) {
	const { t } = useTranslation();
	const qc = useQueryClient();
	const fileInputRef = useRef<HTMLInputElement>(null);
	const [isDragOver, setIsDragOver] = useState(false);

	// ── Query ──────────────────────────────────────────────────────────────
	const { data: attachments = [] } = useQuery(
		taskAttachmentsQueryOptions(projectId, taskId),
	);

	// ── Upload mutation ────────────────────────────────────────────────────
	const uploadMutation = useMutation({
		mutationFn: (file: File) => uploadAttachment(projectId, taskId, file),
		onSuccess: () => {
			qc.invalidateQueries({
				queryKey: ["projects", projectId, "tasks", taskId, "attachments"],
			});
		},
	});

	// ── Delete mutation ────────────────────────────────────────────────────
	const deleteMutation = useMutation({
		mutationFn: (attachmentId: string) =>
			deleteTaskAttachment(projectId, taskId, attachmentId),
		onSuccess: () => {
			qc.invalidateQueries({
				queryKey: ["projects", projectId, "tasks", taskId, "attachments"],
			});
		},
	});

	const addFiles = (files: File[]) => {
		if (!canEdit) return;
		for (const file of files) {
			uploadMutation.mutate(file);
		}
	};

	const handleFileDrop = (e: React.DragEvent) => {
		e.preventDefault();
		setIsDragOver(false);
		if (!canEdit) return;
		addFiles(Array.from(e.dataTransfer.files));
	};

	const handleFileInput = (e: React.ChangeEvent<HTMLInputElement>) => {
		addFiles(Array.from(e.target.files ?? []));
		if (e.target) e.target.value = "";
	};

	return (
		<div className="space-y-3">
			<div className="flex items-center justify-between">
				<h3 className="text-xs font-semibold uppercase tracking-[0.08em] text-muted-foreground/70 flex items-center gap-2">
					<span>{t("project.interactions.taskDetail.attachments.title")}</span>
					<div className="flex-1 h-px bg-linear-to-r from-border/40 to-transparent" />
				</h3>
				{canEdit && (
					<>
						<button
							type="button"
							onClick={() => fileInputRef.current?.click()}
							className="flex size-7 items-center justify-center rounded-lg text-muted-foreground/60 hover:text-foreground hover:bg-muted/50 transition-all duration-150"
							aria-label={t(
								"project.interactions.taskDetail.attachments.upload",
							)}
						>
							<Upload className="size-3.5" />
						</button>
						<input
							ref={fileInputRef}
							type="file"
							multiple
							className="sr-only"
							onChange={handleFileInput}
						/>
					</>
				)}
			</div>

			{/* Attachment list */}
			{attachments.length > 0 && (
				<div className="space-y-2">
					{attachments.map((att) => (
						<AttachmentItem
							key={att.id}
							attachment={att}
							projectId={projectId}
							taskId={taskId}
							canDelete={canEdit}
							onDelete={(id) => deleteMutation.mutate(id)}
						/>
					))}
				</div>
			)}

			{/* Drop zone */}
			{canEdit && (
				<button
					type="button"
					onDragOver={(e) => {
						e.preventDefault();
						setIsDragOver(true);
					}}
					onDragLeave={() => setIsDragOver(false)}
					onDrop={handleFileDrop}
					onClick={() => fileInputRef.current?.click()}
					className={cn(
						"w-full rounded-xl border-2 border-dashed p-8 text-center transition-all duration-200 cursor-pointer group/drop",
						isDragOver
							? "border-primary/50 bg-primary/5 text-primary shadow-sm shadow-primary/10"
							: "border-border/20 bg-muted/5 text-muted-foreground/50 hover:border-border/40 hover:bg-muted/10",
					)}
				>
					<div
						className={cn(
							"mx-auto mb-3 flex size-10 items-center justify-center rounded-xl transition-all duration-200",
							isDragOver
								? "bg-primary/10 text-primary"
								: "bg-muted/30 text-muted-foreground/45 group-hover/drop:bg-muted/40 group-hover/drop:text-muted-foreground/70",
						)}
					>
						<Paperclip className="size-5" />
					</div>
					<p className="text-sm font-medium text-muted-foreground/70 group-hover/drop:text-muted-foreground transition-colors">
						{t("project.interactions.taskDetail.attachments.dropPrompt")}
					</p>
					<p className="text-xs mt-1.5 text-muted-foreground/45 transition-colors">
						{t("project.interactions.taskDetail.attachments.browsePrompt")}
					</p>
				</button>
			)}
		</div>
	);
}
