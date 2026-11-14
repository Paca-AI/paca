import type { QueryClient } from "@tanstack/react-query";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import { type Document, docQueryKeys } from "@/lib/doc-api";

/** A write refused because the document is a locked managed copy. */
export function isDocLockedError(error: unknown): boolean {
	return getApiErrorCode(error) === ApiErrorCode.DocLocked;
}

/**
 * After a save is refused with DOC_LOCKED: mark the cached document locked
 * right away, so the editor turns read-only (and no second save can start)
 * before the refetch lands, then refetch it for the real state.
 */
export function markDocLocked(
	qc: QueryClient,
	projectId: string,
	docId: string,
): void {
	const key = docQueryKeys.detail(projectId, docId);
	qc.setQueryData<Document>(key, (doc) =>
		doc ? { ...doc, source_locked: true } : doc,
	);
	void qc.invalidateQueries({ queryKey: key });
}
