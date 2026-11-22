import { addComment, createTask, updateTask } from "@/lib/interaction-api";
import { createDocument } from "./doc-api";
import type { ProposalKind } from "./meeting-api";

// Performs what a proposal asks for with the clicking person's own session, so
// the existing endpoints judge it by their roles (docs/architecture/
// chat-and-meetings.md, "Why the browser performs the write").

/** Task importance by the names an assistant uses (see priority levels). */
const IMPORTANCE: Record<string, number> = {
	low: 1,
	medium: 2,
	high: 3,
	critical: 4,
};

export type ProposalPayload = Record<string, unknown>;

const str = (v: unknown): string => (typeof v === "string" ? v : "");

function paragraph(text: string) {
	return {
		type: "paragraph",
		content: [{ type: "text", text, styles: {} }],
	};
}

/** Plain text (one paragraph per line) as BlockNote blocks. */
export function textBlocks(text: string): unknown[] {
	const lines = text.split(/\r?\n/).filter((l) => l.trim() !== "");
	return (lines.length ? lines : [""]).map(paragraph);
}

/** Markdown notes as BlockNote blocks: headings, bullets and paragraphs, which
 * is all meeting notes need. Anything fancier stays as text. */
export function markdownBlocks(md: string): unknown[] {
	const blocks: unknown[] = [];
	for (const raw of md.split(/\r?\n/)) {
		const line = raw.trimEnd();
		if (line.trim() === "") continue;
		const heading = /^(#{1,3})\s+(.*)$/.exec(line);
		const bullet = /^\s*[-*]\s+(.*)$/.exec(line);
		const content = (text: string) => [{ type: "text", text, styles: {} }];
		if (heading) {
			blocks.push({
				type: "heading",
				props: { level: (heading[1] ?? "#").length },
				content: content(heading[2] ?? ""),
			});
		} else if (bullet) {
			blocks.push({
				type: "bulletListItem",
				content: content(bullet[1] ?? ""),
			});
		} else {
			blocks.push(paragraph(line));
		}
	}
	return blocks.length ? blocks : [paragraph("")];
}

export interface ProposalResult {
	[key: string]: unknown;
	task_id?: string;
	task_number?: number;
	doc_id?: string;
}

/** Writes a proposal's (possibly edited) payload through the normal endpoints. */
export async function executeProposal(
	projectId: string,
	kind: ProposalKind,
	p: ProposalPayload,
): Promise<ProposalResult> {
	switch (kind) {
		case "task.create": {
			const task = await createTask(projectId, {
				title: str(p.title).trim(),
				...(str(p.sprint_id) ? { sprint_id: str(p.sprint_id) } : {}),
				...(str(p.assignee_id) ? { assignee_ids: [str(p.assignee_id)] } : {}),
			});
			const description = str(p.description).trim();
			const importance = IMPORTANCE[str(p.priority).toLowerCase()];
			if (description || importance !== undefined) {
				await updateTask(projectId, task.id, {
					...(description ? { description: textBlocks(description) } : {}),
					...(importance !== undefined ? { importance } : {}),
				});
			}
			return { task_id: task.id, task_number: task.task_number };
		}
		case "task.update": {
			const taskId = str(p.task_id);
			const payload: Parameters<typeof updateTask>[2] = {};
			if (typeof p.status_id === "string")
				payload.status_id = p.status_id || null;
			if (typeof p.sprint_id === "string")
				payload.sprint_id = p.sprint_id || null;
			if (typeof p.assignee_id === "string")
				payload.assignee_ids = p.assignee_id ? [p.assignee_id] : [];
			if (typeof p.priority === "string") {
				const importance = IMPORTANCE[p.priority.toLowerCase()];
				if (importance !== undefined) payload.importance = importance;
			}
			await updateTask(projectId, taskId, payload);
			return { task_id: taskId };
		}
		case "task.comment": {
			const taskId = str(p.task_id);
			await addComment(projectId, taskId, textBlocks(str(p.body)));
			return { task_id: taskId };
		}
		case "doc.create_from_notes": {
			const doc = await createDocument(projectId, {
				title: str(p.title).trim(),
				content: markdownBlocks(str(p.markdown)),
			});
			return { doc_id: doc.id };
		}
	}
}
