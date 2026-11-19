/**
 * References stored in a description's image and file blocks. A block's URL is
 * one of:
 *   attachment://{projectId}/{taskId}/{attachmentId}  a file uploaded to the task
 *   files://{projectId}/{fileId}                      a file chosen from the project's Files
 * anything else (an https link) is left to BlockNote.
 */
export const ATTACHMENT_SCHEME = "attachment://";
export const FILE_SCHEME = "files://";

export type InlineMediaRef =
	| {
			blockId: string;
			source: "attachment";
			projectId: string;
			taskId: string;
			id: string;
	  }
	| { blockId: string; source: "file"; projectId: string; id: string };

type RefBody = InlineMediaRef extends infer R
	? R extends { blockId: string }
		? Omit<R, "blockId">
		: never
	: never;

/** The URI of a file of the project's Files, for storing in a block. */
export const fileUri = (projectId: string, fileId: string) =>
	`${FILE_SCHEME}${projectId}/${fileId}`;

export function parseMediaUri(url: unknown): RefBody | null {
	if (typeof url !== "string") return null;
	if (url.startsWith(ATTACHMENT_SCHEME)) {
		const [projectId, taskId, id] = url
			.slice(ATTACHMENT_SCHEME.length)
			.split("/");
		return projectId && taskId && id
			? { source: "attachment", projectId, taskId, id }
			: null;
	}
	if (url.startsWith(FILE_SCHEME)) {
		const [projectId, id] = url.slice(FILE_SCHEME.length).split("/");
		return projectId && id ? { source: "file", projectId, id } : null;
	}
	return null;
}

interface BlockLike {
	id: string;
	type: string;
	props?: unknown;
	children?: unknown;
}

/**
 * Every image and file block of the description that points at an attachment
 * or a file of the project, in document order.
 */
export function collectInlineMedia(
	blocks: readonly BlockLike[],
	out: InlineMediaRef[] = [],
): InlineMediaRef[] {
	for (const block of blocks) {
		if (block.type === "image" || block.type === "file") {
			const ref = parseMediaUri((block.props as { url?: unknown })?.url);
			if (ref) out.push({ blockId: block.id, ...ref } as InlineMediaRef);
		}
		if (Array.isArray(block.children))
			collectInlineMedia(block.children as BlockLike[], out);
	}
	return out;
}
