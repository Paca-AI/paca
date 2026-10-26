// Helpers for surfacing a tool call's input in the transcript, so the user can
// see *what* a tool was asked to do (e.g. the exact Bash command) without
// expanding the card and reading raw JSON.

// Ordered by how well each key describes the call on its own.
const SUMMARY_KEYS = [
	"command",
	"cmd",
	"file_path",
	"filePath",
	"path",
	"pattern",
	"query",
	"url",
	"description",
	"title",
	"name",
] as const;

const MAX_SUMMARY_LENGTH = 100;

export function parseToolArgs(
	argsText: string | undefined,
): Record<string, unknown> | undefined {
	if (!argsText) return undefined;
	try {
		const parsed: unknown = JSON.parse(argsText);
		if (typeof parsed === "object" && parsed !== null && !Array.isArray(parsed))
			return parsed as Record<string, unknown>;
	} catch {
		// Not JSON (some agents report the raw command string) — fall through.
	}
	return undefined;
}

/** The shell command a tool call runs, if its input carries one. */
export function getToolCommand(
	argsText: string | undefined,
): string | undefined {
	const args = parseToolArgs(argsText);
	if (!args) return undefined;
	for (const key of ["command", "cmd"]) {
		const value = args[key];
		if (typeof value === "string" && value.trim()) return value;
		if (Array.isArray(value) && value.every((v) => typeof v === "string"))
			return value.join(" ");
	}
	return undefined;
}

/** One-line, truncated description of a tool call's input for its header. */
export function summarizeToolArgs(
	argsText: string | undefined,
): string | undefined {
	const args = parseToolArgs(argsText);
	let raw: string | undefined;
	if (args) {
		raw = getToolCommand(argsText);
		for (const key of SUMMARY_KEYS) {
			if (raw) break;
			const value = args[key];
			if (typeof value === "string" && value.trim()) raw = value;
		}
	} else if (argsText?.trim()) {
		raw = argsText;
	}
	if (!raw) return undefined;
	const oneLine = raw.replace(/\s+/g, " ").trim();
	return oneLine.length > MAX_SUMMARY_LENGTH
		? `${oneLine.slice(0, MAX_SUMMARY_LENGTH)}…`
		: oneLine;
}
