/**
 * Builds pre-filled GitHub "new issue" URLs for the in-app feedback feature.
 *
 * The field ids below must match the `id`s in the issue forms under
 * `.github/ISSUE_TEMPLATE/*.yml` — GitHub pre-fills a form field from the
 * query-string parameter of the same name.
 */

export const FEEDBACK_REPO_URL = "https://github.com/Paca-AI/paca";

/** GitHub's template chooser: lets the user pick bug report or feature request. */
export const FEEDBACK_NEW_ISSUE_URL = `${FEEDBACK_REPO_URL}/issues/new/choose`;

export type FeedbackKind = "bug" | "feature";

const TEMPLATE_BY_KIND: Record<FeedbackKind, string> = {
	bug: "bug_report.yml",
	feature: "feature_request.yml",
};

const TITLE_PREFIX: Record<FeedbackKind, string> = {
	bug: "[Bug] ",
	feature: "[Feature] ",
};

export interface FeedbackDraft {
	kind: FeedbackKind;
	/** Issue title without the "[Bug] " / "[Feature] " prefix. */
	title?: string;
	/** Issue-form field id -> value. Empty values are skipped. */
	fields?: Record<string, string | undefined>;
}

/** Browsers and GitHub both choke on very long URLs; stay well below ~8 KB. */
export const MAX_ISSUE_URL_LENGTH = 7000;

/**
 * The URL is built from `encodeURIComponent` lengths (`%20` per space) while
 * `URLSearchParams` writes `+`, so the budget is slightly conservative: the
 * link may be a little shorter than necessary, never longer. The shrink loop
 * is bounded; if many large fields remain it can still exit over the limit.
 */

const TRUNCATION_MARKER = "\n… [truncated to fit the link]";

function encodedLength(value: string): number {
	return encodeURIComponent(value).length;
}

function truncateToEncodedLength(value: string, max: number): string {
	if (encodedLength(value) <= max) return value;
	const budget = Math.max(0, max - encodedLength(TRUNCATION_MARKER));
	let end = Math.min(value.length, budget);
	while (end > 0 && encodedLength(value.slice(0, end)) > budget) {
		end = Math.floor(end * 0.9);
	}
	// Don't cut a surrogate pair in half.
	const code = value.charCodeAt(end - 1);
	if (code >= 0xd800 && code <= 0xdbff) end -= 1;
	return value.slice(0, end) + TRUNCATION_MARKER;
}

/**
 * Builds the issue URL. If the result would be too long, the longest field
 * values are truncated first (typically the error stack) so the URL still
 * opens. `truncated` reports whether that happened; the cut text ends with a
 * visible marker so the reader knows the rest was dropped.
 */
export function buildIssueUrl(draft: FeedbackDraft): {
	url: string;
	truncated: boolean;
} {
	const fields: Record<string, string> = {};
	for (const [key, value] of Object.entries(draft.fields ?? {})) {
		if (value?.trim()) fields[key] = value.trim();
	}

	const build = () => {
		const params = new URLSearchParams();
		params.set("template", TEMPLATE_BY_KIND[draft.kind]);
		const title = draft.title?.trim();
		if (title) params.set("title", TITLE_PREFIX[draft.kind] + title);
		for (const [key, value] of Object.entries(fields)) params.set(key, value);
		return `${FEEDBACK_REPO_URL}/issues/new?${params.toString()}`;
	};

	let url = build();
	let truncated = false;
	// Shrink the single longest field until the URL fits (bounded loop).
	for (let i = 0; i < 20 && url.length > MAX_ISSUE_URL_LENGTH; i++) {
		const longest = Object.entries(fields).sort(
			(a, b) => encodedLength(b[1]) - encodedLength(a[1]),
		)[0];
		if (!longest) break;
		const [key, value] = longest;
		const overshoot = url.length - MAX_ISSUE_URL_LENGTH;
		const target = Math.max(200, encodedLength(value) - overshoot);
		const next = truncateToEncodedLength(value, target);
		if (next === value) break;
		fields[key] = next;
		truncated = true;
		url = build();
	}
	return { url, truncated };
}

function errorStatus(error: unknown): number | undefined {
	const status = (error as { response?: { status?: unknown } })?.response
		?.status;
	return typeof status === "number" ? status : undefined;
}

/** Response-body keys that describe the failure without echoing user data. */
const SAFE_RESPONSE_KEYS = [
	"error_code",
	"code",
	"error",
	"message",
	"detail",
	"status",
];

/** Keeps only diagnostic keys of an API error body, each capped in length. */
function redactResponse(data: unknown): unknown {
	if (data === null || typeof data !== "object" || Array.isArray(data)) {
		return typeof data === "string" ? data.slice(0, 200) : undefined;
	}
	const out: Record<string, unknown> = {};
	for (const key of SAFE_RESPONSE_KEYS) {
		const value = (data as Record<string, unknown>)[key];
		if (typeof value === "string") out[key] = value.slice(0, 200);
		else if (typeof value === "number" || typeof value === "boolean")
			out[key] = value;
	}
	return Object.keys(out).length > 0 ? out : undefined;
}

/** Drops the query string and fragment, which can carry tokens or ids. */
function stripQuery(url: string): string {
	return url.replace(/[?#].*$/s, "");
}

/** Everything useful about an error as plain text, for the "logs" field. */
export function formatErrorDetails(error: unknown): string {
	const lines: string[] = [];
	if (error instanceof Error) {
		lines.push(`${error.name}: ${error.message}`);
	} else {
		lines.push(String(error));
	}

	const response = (
		error as {
			response?: { status?: number; data?: unknown };
			config?: { method?: string; url?: string };
		}
	)?.response;
	const config = (error as { config?: { method?: string; url?: string } })
		?.config;
	if (config?.url) {
		lines.push(
			`Request: ${(config.method ?? "GET").toUpperCase()} ${stripQuery(config.url)}`,
		);
	}
	const status = errorStatus(error);
	if (status !== undefined) lines.push(`HTTP status: ${status}`);
	const safeBody = redactResponse(response?.data);
	if (safeBody !== undefined) {
		try {
			lines.push(`Response: ${JSON.stringify(safeBody)}`);
		} catch {
			// Circular or otherwise unserializable payload — skip it.
		}
	}

	if (error instanceof Error && error.stack) {
		lines.push("", error.stack);
	}
	const cause = error instanceof Error ? error.cause : undefined;
	if (cause) lines.push("", `Caused by: ${formatErrorDetails(cause)}`);
	return lines.join("\n");
}

/**
 * Non-identifying environment info: browser, language, and the page the user
 * was on (path only — no query string or hash, which can carry tokens).
 */
export function collectEnvironment(): string {
	if (typeof window === "undefined") return "";
	const lines = [
		`Page: ${window.location.pathname}`,
		`Browser: ${window.navigator.userAgent}`,
		`Language: ${window.navigator.language}`,
		`Viewport: ${window.innerWidth}x${window.innerHeight}`,
	];
	return lines.join("\n");
}

/** Short issue title derived from an error. */
export function errorIssueTitle(error: unknown): string {
	const message =
		error instanceof Error && error.message
			? error.message
			: "Unexpected error";
	const oneLine = message.replace(/\s+/g, " ").trim();
	return oneLine.length > 100 ? `${oneLine.slice(0, 97)}…` : oneLine;
}

/** A bug-report draft pre-filled from an error. */
export function draftFromError(error: unknown): FeedbackDraft {
	return {
		kind: "bug",
		title: errorIssueTitle(error),
		fields: {
			summary: `An error occurred: ${errorIssueTitle(error)}`,
			area: "Frontend",
			environment: collectEnvironment(),
			logs: formatErrorDetails(error),
		},
	};
}

/** Opens the pre-filled issue form in a new tab. */
export function openIssue(draft: FeedbackDraft): void {
	window.open(buildIssueUrl(draft).url, "_blank", "noopener,noreferrer");
}
