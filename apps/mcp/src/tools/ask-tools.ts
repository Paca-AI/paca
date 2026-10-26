import type { Tool } from "@modelcontextprotocol/sdk/types.js";
import { z } from "zod";
import type {
	PacaAPIConversationClient,
	PacaAPIDocClient,
	PacaAPITaskExtendedClient,
} from "../api/index.js";

// Must match ASK_QUESTION_MARKER in apps/web/src/lib/ask-question.ts, which
// renders a comment carrying this payload as a clickable question.
const ASK_QUESTION_MARKER = "paca-ask-question";

const OptionSchema = z.union([
	z.string().min(1),
	z.object({
		label: z.string().min(1),
		description: z.string().optional(),
	}),
]);

const AskUserQuestionSchema = z.object({
	question: z.string().min(1),
	options: z.array(OptionSchema).min(1).max(10),
	multiSelect: z.boolean().optional(),
	mention: z.string().optional(),
	projectId: z.string().optional(),
	taskId: z.string().optional(),
	docId: z.string().optional(),
});

export function getAskTools(): Tool[] {
	return [
		{
			name: "ask_user_question",
			description:
				"Ask the user a multiple-choice question and let them pick the answer with a click instead of typing. " +
				"Use it when you need a decision you can't make yourself (which approach, which file, confirm a risky step) and the choices are a short, known list. " +
				"In a chat conversation, omit projectId/taskId/docId: the question is shown in the chat and the user's choice arrives as their next message. " +
				"When working from a task or document, pass projectId plus taskId (or docId): the question is posted as a comment there and the user's choice comes back as a reply comment that @-mentions you. " +
				"The tool returns immediately without an answer — after calling it, stop and wait; do not guess an answer or keep working on the blocked part. " +
				"The user can always type a custom answer too, so don't add an 'Other' option yourself.",
			inputSchema: {
				type: "object",
				properties: {
					question: {
						type: "string",
						description: "The complete question to ask, ending with '?'.",
					},
					options: {
						type: "array",
						description:
							"2-10 short, distinct choices. Each is a label string or an object with a label and an optional description explaining the trade-off.",
						items: {
							anyOf: [
								{ type: "string" },
								{
									type: "object",
									properties: {
										label: { type: "string" },
										description: { type: "string" },
									},
									required: ["label"],
								},
							],
						},
					},
					multiSelect: {
						type: "boolean",
						description:
							"Allow choosing several options (answers are joined with commas). Default false.",
					},
					mention: {
						type: "string",
						description:
							"Username of the person to ask (e.g. 'alice', without '@'). Only used with taskId/docId: they are @-mentioned in the comment so they get notified. Optional — when omitted, the person who started this conversation is mentioned. You do not need list_project_members for this.",
					},
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project. Only when asking on a task or document.",
					},
					taskId: {
						type: "string",
						description:
							"The technical UUID of the task to ask on. Requires projectId.",
					},
					docId: {
						type: "string",
						description:
							"The technical UUID of the document to ask on. Requires projectId.",
					},
				},
				required: ["question", "options"],
			},
		},
	];
}

/**
 * The mention for whoever started the conversation this server runs in, so a
 * question on a task/document reaches them even when the agent does not know
 * their username (and may not be allowed to list project members). Written in
 * the `@Name (id: <member id>)` form the API resolves to a real mention.
 * Best-effort: undefined when there is no such person or the lookup fails.
 */
async function conversationOwnerMention(
	conversationClient?: PacaAPIConversationClient,
): Promise<string | undefined> {
	try {
		const conversation = await conversationClient?.getCurrentConversation();
		const memberId = conversation?.triggered_by_member_id;
		return memberId ? `owner (id: ${memberId})` : undefined;
	} catch {
		return undefined;
	}
}

export const ASK_TOOL_NAMES = ["ask_user_question"] as const;

export async function handleAskTool(
	toolName: string,
	args: unknown,
	taskClient: PacaAPITaskExtendedClient,
	docClient: PacaAPIDocClient,
	conversationClient?: PacaAPIConversationClient,
): Promise<any> {
	if (toolName !== "ask_user_question") {
		throw new Error(`Unknown ask tool: ${toolName}`);
	}
	const input = AskUserQuestionSchema.parse(args);
	const options = input.options.map((o) =>
		typeof o === "string" ? { label: o } : o,
	);
	const multiSelect = input.multiSelect === true;

	if (input.taskId || input.docId) {
		if (!input.projectId) {
			throw new Error("projectId is required with taskId or docId");
		}
		if (input.taskId && input.docId) {
			throw new Error("Pass either taskId or docId, not both");
		}
		const payload = JSON.stringify(
			{
				type: ASK_QUESTION_MARKER,
				question: input.question,
				options,
				multiSelect,
			},
			null,
			2,
		);
		const mention =
			input.mention?.trim().replace(/^@/, "") ||
			(await conversationOwnerMention(conversationClient));
		const block = `\`\`\`paca-ask\n${payload}\n\`\`\``;
		const content = mention ? `@${mention}\n\n${block}` : block;
		if (input.taskId) {
			await taskClient.addTaskComment(input.projectId, input.taskId, {
				content,
			});
		} else if (input.docId) {
			await docClient.addDocumentComment(input.projectId, input.docId, content);
		}
		return {
			content: [
				{
					type: "text",
					text: "Question posted as a comment. Stop now and wait: the user's choice will arrive as a reply comment that mentions you.",
				},
			],
		};
	}

	// Some agent runtimes (e.g. Goose-wrapped CLIs) report tool input as an
	// empty object, so the chat UI could not show the question from the call
	// alone. Echo it in the result too, where it always reaches the client.
	const payload = JSON.stringify({
		type: ASK_QUESTION_MARKER,
		question: input.question,
		options,
		multiSelect,
	});
	return {
		content: [
			{
				type: "text",
				text: `Question shown to the user. Stop now and wait: their choice will arrive as their next message.\n\n\`\`\`paca-ask\n${payload}\n\`\`\``,
			},
		],
	};
}
