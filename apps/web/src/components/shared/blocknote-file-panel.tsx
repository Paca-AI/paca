import { FilePanelExtension } from "@blocknote/core/extensions";
import {
	type FilePanelProps,
	useBlockNoteEditor,
	useExtension,
} from "@blocknote/react";
import { Link2, Paperclip } from "lucide-react";
import { createContext, useContext, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { FilePicker } from "@/components/files/file-picker";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { FileCategory } from "@/lib/files-api";
import { cn } from "@/lib/utils";

/** What the file panel may offer in the editor it sits in. */
export interface FilePanelScope {
	/** The project whose files can be picked; picking is hidden without it. */
	projectId?: string;
	/** Whether the caller may read the project's files. */
	canPickFiles: boolean;
}

export const FilePanelScopeContext = createContext<FilePanelScope>({
	canPickFiles: false,
});

type FileBlockKind = "image" | "video" | "audio" | "file";

const CATEGORIES: Record<FileBlockKind, readonly FileCategory[] | undefined> = {
	image: ["image"],
	video: ["video"],
	audio: ["audio"],
	file: undefined,
};

function nameFromUrl(url: string): string {
	try {
		const last = new URL(url).pathname.split("/").filter(Boolean).pop();
		return last ? decodeURIComponent(last) : url;
	} catch {
		return url;
	}
}

/**
 * The panel that opens when a file, image, video or audio block has no file
 * yet: upload one, pick one that is already in the project's Files, or embed a
 * link. Replaces BlockNote's default panel so it matches the rest of the app.
 */
export function FileBlockPanel({ blockId }: FilePanelProps) {
	const { t } = useTranslation("files");
	const editor = useBlockNoteEditor();
	const filePanel = useExtension(FilePanelExtension);
	const scope = useContext(FilePanelScopeContext);
	const block = editor.getBlock(blockId);
	const rawType = block?.type ?? "file";
	const kind: FileBlockKind =
		rawType === "image" || rawType === "video" || rawType === "audio"
			? rawType
			: "file";
	const canPick = scope.canPickFiles && !!scope.projectId;

	const [tab, setTab] = useState("upload");
	const [uploading, setUploading] = useState(false);
	const [failed, setFailed] = useState(false);
	const [dragging, setDragging] = useState(false);
	const [link, setLink] = useState("");
	const input = useRef<HTMLInputElement>(null);

	const accept = (
		editor.schema.blockSpecs[rawType] as
			| { implementation?: { meta?: { fileBlockAccept?: string[] } } }
			| undefined
	)?.implementation?.meta?.fileBlockAccept;

	const apply = (props: { name: string; url: string }) => {
		editor.updateBlock(blockId, { props } as never);
		filePanel.closeMenu();
		editor.focus();
	};

	const upload = async (file: File | undefined) => {
		if (!file || !editor.uploadFile) return;
		setUploading(true);
		setFailed(false);
		try {
			const result = await editor.uploadFile(file, blockId);
			if (typeof result === "string") apply({ name: file.name, url: result });
			else editor.updateBlock(blockId, result as never);
			if (typeof result !== "string") {
				filePanel.closeMenu();
				editor.focus();
			}
		} catch {
			setFailed(true);
		} finally {
			setUploading(false);
		}
	};

	return (
		<div
			className="w-[26rem] max-w-[calc(100vw-2rem)] rounded-xl border bg-popover p-3 text-popover-foreground shadow-lg"
			data-testid="file-block-panel"
		>
			<Tabs value={tab} onValueChange={(v) => setTab(String(v))}>
				<TabsList className="w-full">
					{editor.uploadFile && (
						<TabsTrigger value="upload">
							{t("filePanel.tabs.upload")}
						</TabsTrigger>
					)}
					{canPick && (
						<TabsTrigger value="files">{t("filePanel.tabs.files")}</TabsTrigger>
					)}
					<TabsTrigger value="embed">{t("filePanel.tabs.embed")}</TabsTrigger>
				</TabsList>

				{editor.uploadFile && (
					<TabsContent value="upload" className="pt-3">
						<button
							type="button"
							disabled={uploading}
							onClick={() => input.current?.click()}
							onDragOver={(e) => {
								e.preventDefault();
								setDragging(true);
							}}
							onDragLeave={() => setDragging(false)}
							onDrop={(e) => {
								e.preventDefault();
								setDragging(false);
								void upload(e.dataTransfer.files[0]);
							}}
							className={cn(
								"group/drop flex w-full flex-col items-center gap-2 rounded-xl border-2 border-dashed p-8 text-center transition-all duration-200",
								dragging
									? "border-primary/50 bg-primary/5 text-primary"
									: "border-border/40 bg-muted/20 text-muted-foreground hover:border-border hover:bg-muted/40",
							)}
						>
							{uploading ? (
								<Spinner className="size-5" />
							) : (
								<Paperclip className="size-5" />
							)}
							<span className="text-sm font-medium text-foreground">
								{uploading
									? t("filePanel.upload.uploading")
									: t(`filePanel.upload.title.${kind}`)}
							</span>
							<span className="text-xs text-muted-foreground">
								{t("filePanel.upload.hint")}
							</span>
						</button>
						<input
							ref={input}
							type="file"
							className="sr-only"
							accept={accept?.length ? accept.join(",") : "*/*"}
							data-testid="file-panel-input"
							onChange={(e) => {
								void upload(e.target.files?.[0]);
								e.target.value = "";
							}}
						/>
						{failed && (
							<p className="mt-2 text-xs text-destructive">
								{t("filePanel.upload.error")}
							</p>
						)}
					</TabsContent>
				)}

				{canPick && scope.projectId && (
					<TabsContent value="files" className="pt-3">
						<p className="mb-2 text-xs text-muted-foreground">
							{t(`filePanel.files.hint.${kind}`)}
						</p>
						<FilePicker
							projectId={scope.projectId}
							categories={CATEGORIES[kind]}
							onPick={(file) =>
								apply({
									name: file.name,
									url: `files://${scope.projectId}/${file.id}`,
								})
							}
						/>
					</TabsContent>
				)}

				<TabsContent value="embed" className="pt-3">
					<form
						className="flex items-center gap-2"
						onSubmit={(e) => {
							e.preventDefault();
							const url = link.trim();
							if (url) apply({ name: nameFromUrl(url), url });
						}}
					>
						<div className="relative min-w-0 flex-1">
							<Link2 className="pointer-events-none absolute top-2 left-2.5 size-4 text-muted-foreground" />
							<Input
								value={link}
								onChange={(e) => setLink(e.target.value)}
								placeholder={t("filePanel.embed.placeholder")}
								aria-label={t("filePanel.embed.placeholder")}
								className="pl-8"
								inputMode="url"
							/>
						</div>
						<Button type="submit" disabled={!link.trim()}>
							{t(`filePanel.embed.button.${kind}`)}
						</Button>
					</form>
				</TabsContent>
			</Tabs>
		</div>
	);
}
