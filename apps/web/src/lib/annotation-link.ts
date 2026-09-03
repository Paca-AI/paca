// Shared by the BlockNote paste handler
// (components/shared/blocknote-annotation-paste-handler.ts) and the agent
// conversation composer's onPaste handler
// (components/assistant-ui/thread.tsx) — both need to recognize the exact
// same "copy link" URL a user gets from the extension's pin popover or the
// comment detail page's own Copy button (comment-detail-view.tsx), so the
// regex lives in one place rather than being hand-duplicated twice.

export interface AnnotationLinkMatch {
	projectId: string;
	environmentId: string;
	portForwardId: string;
	annotationId: string;
}

// Deliberately a substring search, not a full-string match — pasted text
// may carry other content around the link (e.g. copied along with a
// sentence, or wrapped in Markdown), and the link should still be
// recognized wherever it appears.
const ANNOTATION_LINK_RE =
	/\/projects\/([\w-]+)\/environments\/([\w-]+)\/port-forwards\/([\w-]+)\/comments\/([\w-]+)/;

/** Finds a comment-detail-page URL anywhere in text and pulls out its four
 * path IDs, or returns null if none is present. */
export function matchAnnotationLink(text: string): AnnotationLinkMatch | null {
	const m = ANNOTATION_LINK_RE.exec(text);
	if (!m) return null;
	const [, projectId, environmentId, portForwardId, annotationId] = m;
	return { projectId, environmentId, portForwardId, annotationId };
}
