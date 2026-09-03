import {
	BlockNoteSchema,
	defaultBlockSpecs,
	defaultInlineContentSpecs,
} from "@blocknote/core";
import {
	DocumentationReference,
	TaskReference,
	TeamMention,
} from "./blocknote-inline-contents";
import { MermaidBlock } from "./blocknote-mermaid-block";

export const customSchema = BlockNoteSchema.create({
	blockSpecs: {
		...defaultBlockSpecs,
		mermaid: MermaidBlock(),
	},
	inlineContentSpecs: {
		...defaultInlineContentSpecs,
		teamMention: TeamMention,
		taskReference: TaskReference,
		docReference: DocumentationReference,
	},
});
