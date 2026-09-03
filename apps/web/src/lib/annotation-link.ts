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

// Anchored to the entire (trimmed) string rather than "found anywhere" —
// for callers where a match triggers something destructive to the rest of
// the text (replacing a whole paragraph/paste with just a rich card), so it
// must not also fire when the link is merely part of a longer sentence or a
// paste that carries other content around it, which would otherwise
// silently drop that surrounding text.
const ANNOTATION_LINK_ONLY_RE = new RegExp(
	`^\\S*${ANNOTATION_LINK_RE.source}\\S*$`,
);

/** Like matchAnnotationLink, but only matches when the link (allowing
 * surrounding whitespace and, on the same token, a URL scheme/host prefix)
 * is the *entire* text — not merely present somewhere inside a longer
 * paragraph or paste. */
export function matchAnnotationLinkOnly(
	text: string,
): AnnotationLinkMatch | null {
	const trimmed = text.trim();
	if (!ANNOTATION_LINK_ONLY_RE.test(trimmed)) return null;
	return matchAnnotationLink(trimmed);
}
