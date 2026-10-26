// Structured multiple-choice questions an agent asks the user through the
// `ask_user_question` MCP tool (apps/mcp/src/tools/ask-tools.ts). In a chat
// conversation the question rides on the tool call's input; in a task/doc
// activity comment it is embedded in the comment as a fenced code block whose
// JSON body carries ASK_QUESTION_MARKER — see encode/decode below.

export interface AskOption {
	label: string;
	description?: string;
}

export interface AskQuestion {
	question: string;
	options: AskOption[];
	multiSelect: boolean;
}

export const ASK_QUESTION_MARKER = "paca-ask-question";

const ASK_TOOL_NAME = "ask_user_question";

/** Matches the MCP tool however the agent runtime prefixes it
 *  (`mcp__paca__ask_user_question`, `paca: ask_user_question`, …). */
export function isAskQuestionTool(toolName: string): boolean {
	// Some runtimes render the name as a title: "mcp: paca  ask user question".
	return toolName
		.toLowerCase()
		.replace(/[\s_-]+/g, "_")
		.includes(ASK_TOOL_NAME);
}

/** The question for an ask-tool call: from its input when the runtime
 *  reported one, else from the payload the tool echoes in its result. */
export function parseAskQuestionFromCall(
	argsText: string | undefined,
	result: unknown,
): AskQuestion | null {
	const fromArgs = parseAskQuestionText(argsText);
	if (fromArgs) return fromArgs;
	if (typeof result !== "string") return null;
	const match = result.match(/```paca-ask\n([\s\S]*?)\n```/);
	return match ? parseAskQuestionText(match[1]) : null;
}

function parseOption(raw: unknown): AskOption | null {
	if (typeof raw === "string") {
		return raw.trim() ? { label: raw.trim() } : null;
	}
	if (typeof raw !== "object" || raw === null) return null;
	const { label, description } = raw as Record<string, unknown>;
	if (typeof label !== "string" || !label.trim()) return null;
	return {
		label: label.trim(),
		...(typeof description === "string" && description.trim()
			? { description: description.trim() }
			: {}),
	};
}

export function parseAskQuestion(value: unknown): AskQuestion | null {
	if (typeof value !== "object" || value === null) return null;
	const { question, options, multiSelect } = value as Record<string, unknown>;
	if (typeof question !== "string" || !question.trim()) return null;
	if (!Array.isArray(options)) return null;
	const parsed = options
		.map(parseOption)
		.filter((o): o is AskOption => o !== null);
	if (parsed.length === 0) return null;
	return {
		question: question.trim(),
		options: parsed,
		multiSelect: multiSelect === true,
	};
}

export function parseAskQuestionText(
	argsText: string | undefined,
): AskQuestion | null {
	if (!argsText) return null;
	try {
		return parseAskQuestion(JSON.parse(argsText));
	} catch {
		return null;
	}
}

interface BlockLike {
	type?: unknown;
	content?: unknown;
}

function blockText(block: BlockLike): string {
	if (!Array.isArray(block.content)) return "";
	return block.content
		.map((c) => (typeof c?.text === "string" ? c.text : ""))
		.join("");
}

/** Finds the question embedded in a comment's blocks, if any, along with the
 *  blocks that should still render as ordinary comment text. */
export function extractAskQuestion(
	blocks: unknown[] | null,
): { ask: AskQuestion; rest: unknown[] } | null {
	if (!Array.isArray(blocks)) return null;
	for (let i = 0; i < blocks.length; i++) {
		const block = blocks[i] as BlockLike;
		if (block?.type !== "codeBlock") continue;
		try {
			const data = JSON.parse(blockText(block)) as Record<string, unknown>;
			if (data?.type !== ASK_QUESTION_MARKER) continue;
			const ask = parseAskQuestion(data);
			if (!ask) continue;
			return { ask, rest: blocks.filter((_, j) => j !== i) };
		} catch {
			// not the ask payload
		}
	}
	return null;
}

/** The reply posted when the user picks an answer. */
export function formatAskAnswer(answers: string[]): string {
	return answers.join(", ");
}
