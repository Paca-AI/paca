import "@eternalheart/react-file-preview/style.css";
import {
	configurePdfjs,
	FilePreviewModal,
	type PreviewFileInput,
} from "@eternalheart/react-file-preview";
import { useQuery } from "@tanstack/react-query";
import { ChevronLeft, ChevronRight, Download, X } from "lucide-react";
import pdfWorkerUrl from "pdfjs-dist/legacy/build/pdf.worker.min.mjs?url";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { useIsDark } from "@/hooks/use-is-dark";
import {
	type FileNode,
	type FileScope,
	formatFileSize,
	getNodeDownload,
	MAX_PREVIEW_BYTES,
	scopeOfNode,
} from "@/lib/files-api";

// Serve the PDF worker from our own origin instead of the library's CDN
// default, so previews work offline and under a strict CSP.
let configured = false;
function ensurePdfConfigured() {
	if (configured) return;
	configure();
	configured = true;
}
function configure() {
	configurePdfjs({ workerSrc: pdfWorkerUrl });
}

const PREVIEW_LOCALES = {
	en: "en-US",
	vi: "en-US",
	ko: "ko-KR",
	"zh-CN": "zh-CN",
	ja: "ja-JP",
	es: "en-US",
	fr: "en-US",
	ru: "en-US",
	"pt-BR": "en-US",
} as const;

/**
 * Full-screen preview of one file of a list, with previous/next across the
 * list. The viewer library is loaded only when a preview opens (this module is
 * imported lazily). The presigned URL is fetched for the file being shown
 * only, not for the whole folder.
 */
export default function FilePreview({
	files,
	index,
	scope,
	onIndexChange,
	onClose,
	onDownload,
	resolveUrl,
}: {
	files: FileNode[];
	index: number;
	scope: FileScope;
	onIndexChange: (i: number) => void;
	onClose: () => void;
	onDownload: (n: FileNode) => void;
	/**
	 * Fetches the URL to show for a file. Defaults to the file manager's own
	 * download URL; other surfaces (task attachments) pass their own so access
	 * is judged by the API they already use.
	 */
	resolveUrl?: (n: FileNode) => Promise<string>;
}) {
	const { t, i18n } = useTranslation("files");
	const isDark = useIsDark();
	const node = files[index];
	const tooLarge = !!node && node.size > MAX_PREVIEW_BYTES;
	const nodeScope = node
		? node.project_id
			? scopeOfNode(node)
			: scope
		: scope;

	useEffect(() => {
		ensurePdfConfigured();
	}, []);

	const download = useQuery({
		queryKey: [
			"files",
			"preview-url",
			resolveUrl ? "custom" : "node",
			node?.id,
		],
		queryFn: async () =>
			resolveUrl
				? { url: await resolveUrl(node) }
				: getNodeDownload(nodeScope, node.id),
		enabled: !!node && !tooLarge,
		// Presigned URLs live 15 minutes; refetch before reusing an old one.
		staleTime: 10 * 60_000,
		gcTime: 10 * 60_000,
	});

	const hasPrev = index > 0;
	const hasNext = index < files.length - 1;
	useEffect(() => {
		const onKey = (e: KeyboardEvent) => {
			if (e.key === "ArrowLeft" && hasPrev) onIndexChange(index - 1);
			if (e.key === "ArrowRight" && hasNext) onIndexChange(index + 1);
		};
		window.addEventListener("keydown", onKey);
		return () => window.removeEventListener("keydown", onKey);
	}, [hasPrev, hasNext, index, onIndexChange]);

	if (!node) return null;

	const previewFile: PreviewFileInput | null = download.data
		? {
				id: node.id,
				name: node.name,
				type: node.content_type || "application/octet-stream",
				url: download.data.url,
				size: node.size,
			}
		: null;

	return (
		<>
			{previewFile && !tooLarge && (
				<FilePreviewModal
					files={[previewFile]}
					currentIndex={0}
					isOpen
					onClose={onClose}
					showNavigation={false}
					theme={isDark ? "dark" : "light"}
					locale={
						PREVIEW_LOCALES[i18n.language as keyof typeof PREVIEW_LOCALES] ??
						"en-US"
					}
					onDownload={() => onDownload(node)}
				/>
			)}
			{(!previewFile || tooLarge) && (
				<div
					role="dialog"
					aria-modal="true"
					aria-label={node.name}
					className="fixed inset-0 z-[100] flex flex-col items-center justify-center gap-4 bg-black/80 p-6 text-white"
				>
					<Button
						variant="ghost"
						size="icon"
						className="absolute top-4 right-4 text-white hover:bg-white/10"
						aria-label={t("preview.close")}
						onClick={onClose}
					>
						<X className="size-5" />
					</Button>
					{tooLarge ? (
						<>
							<p className="max-w-md text-center text-sm">
								{t("preview.tooLarge", { size: formatFileSize(node.size) })}
							</p>
							<Button onClick={() => onDownload(node)}>
								<Download className="size-4" />
								{t("preview.download")}
							</Button>
						</>
					) : download.isError ? (
						<p className="text-sm">{t("preview.failed")}</p>
					) : (
						<div className="flex items-center gap-2 text-sm">
							<Spinner className="size-5" />
							{t("preview.loading")}
						</div>
					)}
				</div>
			)}
			{hasPrev && (
				<Button
					variant="secondary"
					size="icon-lg"
					className="fixed top-1/2 left-4 z-[110] -translate-y-1/2 rounded-full opacity-80 hover:opacity-100"
					aria-label={t("preview.previous")}
					onClick={() => onIndexChange(index - 1)}
				>
					<ChevronLeft className="size-5" />
				</Button>
			)}
			{hasNext && (
				<Button
					variant="secondary"
					size="icon-lg"
					className="fixed top-1/2 right-4 z-[110] -translate-y-1/2 rounded-full opacity-80 hover:opacity-100"
					aria-label={t("preview.next")}
					onClick={() => onIndexChange(index + 1)}
				>
					<ChevronRight className="size-5" />
				</Button>
			)}
		</>
	);
}
