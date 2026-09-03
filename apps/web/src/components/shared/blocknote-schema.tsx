import {
	BlockNoteSchema,
	defaultBlockSpecs,
	defaultInlineContentSpecs,
} from "@blocknote/core";
import { AnnotationCardBlock } from "./blocknote-annotation-card-block";
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
		annotationCard: AnnotationCardBlock(),
	},
	inlineContentSpecs: {
		...defaultInlineContentSpecs,
		teamMention: TeamMention,
		taskReference: TaskReference,
		docReference: DocumentationReference,
	},
});
