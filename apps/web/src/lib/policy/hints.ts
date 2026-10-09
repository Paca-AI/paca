/** A completion offered while typing inside a JSON string. */
export interface PolicyHint {
	value: string;
	kind: "action" | "attribute";
}

export interface HintContext {
	/** The unfinished string under the caret, without its opening quote. */
	token: string;
	/** Index in the text where the token starts. */
	start: number;
	hints: PolicyHint[];
}

const MAX_HINTS = 8;

/**
 * Completions for the JSON string being typed at `caret`: known actions
 * ("tasks:wr" -> "tasks:write") and attribute keys ("task.sp" ->
 * "task.sprint_id"). Null when the caret is not inside a string, or nothing
 * matches. Plain prefix matching: a lightweight aid, not a language server.
 */
export function policyHints(
	text: string,
	caret: number,
	actions: readonly string[],
	attributeKeys: readonly string[],
): HintContext | null {
	const before = text.slice(0, caret);
	const quote = before.lastIndexOf('"');
	if (quote === -1) return null;
	// An even number of quotes before the caret means it is between strings.
	const quotes = before.split('"').length - 1;
	if (quotes % 2 === 0) return null;
	const token = before.slice(quote + 1);
	if (token === "" || /[\s,\\]/.test(token)) return null;
	const lower = token.toLowerCase();
	const hints: PolicyHint[] = [
		...["*", ...actions].map((value) => ({ value, kind: "action" as const })),
		...attributeKeys.map((value) => ({ value, kind: "attribute" as const })),
	]
		.filter((h) => h.value.toLowerCase().startsWith(lower) && h.value !== token)
		.slice(0, MAX_HINTS);
	if (hints.length === 0) return null;
	return { token, start: quote + 1, hints };
}

/** `text` with the token at `ctx` replaced by `value`, and the new caret. */
export function applyHint(
	text: string,
	caret: number,
	ctx: HintContext,
	value: string,
): { text: string; caret: number } {
	const next = text.slice(0, ctx.start) + value + text.slice(caret);
	return { text: next, caret: ctx.start + value.length };
}
