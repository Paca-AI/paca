/** Pure helpers for dragging documents and folders around the docs tree. */

export const DOC_NODE_DRAG_TYPE = "application/x-paca-doc-node";

export interface DocNodeDragPayload {
	kind: "doc" | "folder";
	id: string;
	/** The project the dragged row belongs to. */
	projectId: string;
	/** The folder it currently sits in (null = top level). */
	parentId: string | null;
}

/** Parses and validates a drag payload; null when it is malformed or was
 *  dragged from another project (e.g. a second browser tab). */
export function parseDocNodeDragPayload(
	raw: string,
	projectId: string,
): DocNodeDragPayload | null {
	let value: unknown;
	try {
		value = JSON.parse(raw);
	} catch {
		return null;
	}
	if (typeof value !== "object" || value === null) return null;
	const p = value as Record<string, unknown>;
	if (p.kind !== "doc" && p.kind !== "folder") return null;
	if (typeof p.id !== "string" || p.id === "") return null;
	if (p.projectId !== projectId) return null;
	if (p.parentId !== null && typeof p.parentId !== "string") return null;
	return {
		kind: p.kind,
		id: p.id,
		projectId,
		parentId: p.parentId as string | null,
	};
}

/** True when dropping `payload` into the target would be a no-op or invalid.
 *  `targetPath` is the target folder's id preceded by its ancestors' ids
 *  (empty for the top level), so a folder can't enter itself or a descendant. */
export function isMoveBlocked(
	payload: DocNodeDragPayload,
	targetFolderId: string | null,
	targetPath: readonly string[],
): boolean {
	if (payload.parentId === targetFolderId) return true;
	return payload.kind === "folder" && targetPath.includes(payload.id);
}
