import type { ChatBlock } from "./chat-api";
import { mentionNode } from "./chat-content";
import type { Document } from "./doc-api";
import type { Task } from "./interaction-api";

const MAX_TITLE = 120;

/** A task title from a message's text: its first non-empty line, whitespace
 * collapsed, cut at a readable length. */
export function titleFromText(text: string, max = MAX_TITLE): string {
	const line = text.split("\n").find((l) => l.trim() !== "") ?? "";
	const collapsed = line.trim().replace(/\s+/g, " ");
	return collapsed.length <= max
		? collapsed
		: `${collapsed.slice(0, max - 1)}…`;
}

/** The message that reports a created task in the conversation it came from:
 * a lead-in and a chip for the task, which is also the link back. */
export function taskCreatedBlocks(lead: string, task: Task): ChatBlock[] {
	return [
		{
			type: "paragraph",
			content: [
				{ type: "text", text: `${lead} `, styles: {} },
				mentionNode({
					id: task.id,
					kind: "task",
					projectId: task.project_id,
					number: task.task_number,
					name: String(task.task_number),
				}),
			],
		},
	];
}

/** The message that reports a created document, the same shape as a task's. */
export function docCreatedBlocks(lead: string, doc: Document): ChatBlock[] {
	return [
		{
			type: "paragraph",
			content: [
				{ type: "text", text: `${lead} `, styles: {} },
				mentionNode({
					id: doc.id,
					kind: "doc",
					projectId: doc.project_id ?? "",
					name: doc.title,
				}),
			],
		},
	];
}
