import {
	FileArchive,
	FileAudio,
	FileCode,
	File as FileIcon,
	FileImage,
	FileSpreadsheet,
	FileText,
	FileVideo,
	Folder,
	FolderLock,
	Presentation,
} from "lucide-react";
import {
	type FileCategory,
	type FileNode,
	fileCategory,
} from "@/lib/files-api";
import { cn } from "@/lib/utils";

const ICONS: Record<FileCategory, typeof FileIcon> = {
	folder: Folder,
	image: FileImage,
	video: FileVideo,
	audio: FileAudio,
	pdf: FileText,
	document: FileText,
	spreadsheet: FileSpreadsheet,
	presentation: Presentation,
	archive: FileArchive,
	code: FileCode,
	text: FileText,
	other: FileIcon,
};

const COLORS: Record<FileCategory, string> = {
	folder: "text-muted-foreground",
	image: "text-rose-500",
	video: "text-purple-500",
	audio: "text-fuchsia-500",
	pdf: "text-red-500",
	document: "text-blue-500",
	spreadsheet: "text-emerald-600",
	presentation: "text-orange-500",
	archive: "text-amber-600",
	code: "text-sky-500",
	text: "text-muted-foreground",
	other: "text-muted-foreground",
};

export function FileTypeIcon({
	node,
	className,
}: {
	node: Pick<FileNode, "kind" | "name" | "content_type" | "locked">;
	className?: string;
}) {
	const category = fileCategory(node);
	// Managed folders show a lock so it is clear they cannot be changed.
	const Icon =
		category === "folder" && node.locked ? FolderLock : ICONS[category];
	return (
		<Icon
			aria-hidden="true"
			className={cn("shrink-0", COLORS[category], className)}
		/>
	);
}
