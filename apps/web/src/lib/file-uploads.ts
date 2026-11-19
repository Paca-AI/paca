import { toast } from "@/components/ui/toast";
import i18n from "@/i18n";
import { getApiErrorMessage } from "./api-error";
import {
	createFolder,
	type FileScope,
	UploadAbortedError,
	uploadFile,
} from "./files-api";
import { showErrorToast } from "./notify";

/** A file picked or dropped, with the folders it sits in below the drop target. */
export interface UploadEntry {
	file: File;
	/** Folder names from the dropped root down to the file's folder. */
	dirPath: string[];
}

const CONCURRENCY = 3;
/** Progress is reported at most this often, so a fast upload does not re-render the toast every few bytes. */
const PROGRESS_INTERVAL_MS = 250;

/**
 * Queues entries for upload into `parentId`, creating the folders of a
 * dropped directory first, and reports the whole batch in one toast: a
 * loading toast with the overall progress and a Cancel action while it runs,
 * then a success or error toast. `onSettled` runs as each file finishes (done
 * or not) so the caller can refresh its listing.
 */
export function enqueueUploads(
	scope: FileScope,
	parentId: string | null,
	entries: UploadEntry[],
	onSettled: () => void,
) {
	if (entries.length === 0) return;
	const total = entries.length;
	const totalBytes = entries.reduce((sum, e) => sum + e.file.size, 0);
	const loaded = new Array<number>(total).fill(0);
	let finished = 0;
	let failures = 0;
	let firstError: unknown;
	const controller = new AbortController();

	const percent = () => {
		const sent = loaded.reduce((a, b) => a + b, 0);
		return totalBytes > 0
			? Math.min(100, Math.round((sent / totalBytes) * 100))
			: 0;
	};
	const toastId = toast.add({
		type: "loading",
		title: i18n.t("files:upload.uploading", { count: total }),
		description: "0%",
		timeout: 0,
		actionProps: {
			children: i18n.t("files:upload.cancel"),
			onClick: () => controller.abort(),
		},
	});
	let lastShown = 0;
	const showProgress = (force = false) => {
		const now = Date.now();
		if (!force && now - lastShown < PROGRESS_INTERVAL_MS) return;
		lastShown = now;
		toast.update(toastId, {
			title: i18n.t("files:upload.uploading", { count: total - finished }),
			description: `${percent()}%`,
		});
	};

	// One folder per distinct path, created once for the whole batch.
	const folderIds = new Map<string, Promise<string | null>>();
	const folderFor = (dirPath: string[]): Promise<string | null> => {
		if (dirPath.length === 0) return Promise.resolve(parentId);
		const key = dirPath.join("/");
		let p = folderIds.get(key);
		if (!p) {
			p = folderFor(dirPath.slice(0, -1)).then(async (parent) => {
				const folder = await createFolder(scope, {
					parent_id: parent,
					name: dirPath[dirPath.length - 1],
				});
				return folder.id;
			});
			folderIds.set(key, p);
		}
		return p;
	};

	let next = 0;
	const worker = async () => {
		while (next < total && !controller.signal.aborted) {
			const index = next++;
			const entry = entries[index];
			try {
				const folder = await folderFor(entry.dirPath);
				await uploadFile(scope, folder, entry.file, {
					signal: controller.signal,
					onProgress: (sent) => {
						loaded[index] = sent;
						showProgress();
					},
				});
				loaded[index] = entry.file.size;
			} catch (error) {
				if (!(error instanceof UploadAbortedError)) {
					failures++;
					firstError ??= error;
				}
			} finally {
				finished++;
				showProgress();
				onSettled();
			}
		}
	};

	void Promise.all(
		Array.from({ length: Math.min(CONCURRENCY, total) }, worker),
	).then(() => {
		toast.close(toastId);
		if (controller.signal.aborted) return;
		if (failures === 0) {
			toast.add({ type: "success", title: i18n.t("files:upload.done") });
		} else {
			showErrorToast({
				title: i18n.t("files:upload.failed", { count: failures }),
				description:
					getApiErrorMessage(firstError) ?? i18n.t("files:upload.error"),
				error: firstError,
			});
		}
	});
}

// ── Collecting files from a drop or a picker ──────────────────────────────────

function readEntries(reader: FileSystemDirectoryReader) {
	return new Promise<FileSystemEntry[]>((resolve, reject) =>
		reader.readEntries(resolve, reject),
	);
}

async function walk(
	entry: FileSystemEntry,
	dirPath: string[],
	out: UploadEntry[],
) {
	if (entry.isFile) {
		const file = await new Promise<File>((resolve, reject) =>
			(entry as FileSystemFileEntry).file(resolve, reject),
		);
		out.push({ file, dirPath });
		return;
	}
	const reader = (entry as FileSystemDirectoryEntry).createReader();
	const path = [...dirPath, entry.name];
	// readEntries returns at most ~100 entries per call; keep reading until empty.
	for (;;) {
		const batch = await readEntries(reader);
		if (batch.length === 0) break;
		for (const child of batch) await walk(child, path, out);
	}
}

/** Files (and the folders they sit in) from a drop, directories included. */
export async function collectDropped(dt: DataTransfer): Promise<UploadEntry[]> {
	const out: UploadEntry[] = [];
	const items = Array.from(dt.items ?? []);
	const entries = items
		.map((i) => (i.kind === "file" ? i.webkitGetAsEntry?.() : null))
		.filter((e): e is FileSystemEntry => !!e);
	if (entries.length > 0) {
		for (const e of entries) await walk(e, [], out);
		return out;
	}
	return Array.from(dt.files).map((file) => ({ file, dirPath: [] }));
}

/** Files from an <input type="file"> (folder pickers set webkitRelativePath). */
export function collectFromInput(files: FileList | File[]): UploadEntry[] {
	return Array.from(files).map((file) => {
		const rel = (file as File & { webkitRelativePath?: string })
			.webkitRelativePath;
		const parts = rel ? rel.split("/").slice(0, -1) : [];
		return { file, dirPath: parts };
	});
}
