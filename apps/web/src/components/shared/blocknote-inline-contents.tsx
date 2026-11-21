import { createReactInlineContentSpec } from "@blocknote/react";
import { useQuery } from "@tanstack/react-query";
import { AtSign, FileText, Hash } from "lucide-react";
import { currentUserQueryOptions } from "@/lib/auth-api";
import { ReferenceChip } from "./reference-chip";

// A reference stored by chat holds ids only (no title). The editor shows its
// live title, read with the viewer's own access, instead of "Unknown".
const UNTITLED = "Unknown";

/** A person's mention. The reader's own stands out, as it did in chat. */
function TeamMentionChip({ id, name }: { id: string; name: string }) {
	const { data: me } = useQuery(currentUserQueryOptions);
	const mine = !!id && me?.id === id;
	return (
		<span
			className={
				mine
					? "inline-flex items-center gap-1 rounded-full bg-primary/20 px-1.5 py-0.5 text-xs font-semibold text-primary transition-colors"
					: "inline-flex items-center gap-1 rounded-full bg-blue-50/80 px-1.5 py-0.5 text-xs font-medium text-blue-700 dark:bg-blue-900/40 dark:text-blue-300 hover:bg-blue-100 dark:hover:bg-blue-900/60 transition-colors"
			}
			data-mention-type="team"
			data-mention={mine ? "me" : "other"}
		>
			<AtSign className="shrink-0" width={12} height={12} />
			{name}
		</span>
	);
}

export const TeamMention = createReactInlineContentSpec(
	{
		type: "teamMention",
		propSchema: {
			id: {
				default: "",
			},
			name: {
				default: "Unknown",
			},
			avatar: {
				default: undefined,
				type: "string" as const,
			},
		},
		content: "none",
	},
	{
		render: (props) => (
			<TeamMentionChip
				id={props.inlineContent.props.id}
				name={props.inlineContent.props.name}
			/>
		),
	},
);

export const TaskReference = createReactInlineContentSpec(
	{
		type: "taskReference",
		propSchema: {
			id: {
				default: "",
			},
			title: {
				default: "Unknown",
			},
			status: {
				default: "open",
			},
			// Set by chat, which stores references as ids (never the title).
			projectId: {
				default: "",
			},
			number: {
				default: 0,
			},
		},
		content: "none",
	},
	{
		render: (props) =>
			props.inlineContent.props.projectId &&
			props.inlineContent.props.title === UNTITLED ? (
				<ReferenceChip
					kind="task"
					id={props.inlineContent.props.id}
					projectId={props.inlineContent.props.projectId}
					number={props.inlineContent.props.number || undefined}
					interactive={!props.editor.isEditable}
				/>
			) : (
				<span
					className="inline-flex items-center gap-1 rounded-full bg-success/10 px-1.5 py-0.5 text-xs font-medium text-success hover:bg-success/10 transition-colors"
					data-mention-type="task"
				>
					<Hash className="shrink-0" width={12} height={12} />
					{props.inlineContent.props.title}
				</span>
			),
	},
);

export const DocumentationReference = createReactInlineContentSpec(
	{
		type: "docReference",
		propSchema: {
			id: {
				default: "",
			},
			title: {
				default: "Unknown",
			},
			projectId: {
				default: "",
			},
		},
		content: "none",
	},
	{
		render: (props) =>
			props.inlineContent.props.projectId &&
			props.inlineContent.props.title === UNTITLED ? (
				<ReferenceChip
					kind="doc"
					id={props.inlineContent.props.id}
					projectId={props.inlineContent.props.projectId}
					interactive={!props.editor.isEditable}
				/>
			) : (
				<span
					className="inline-flex items-center gap-1 rounded-full bg-purple-50/80 px-1.5 py-0.5 text-xs font-medium text-purple-700 dark:bg-purple-900/40 dark:text-purple-300 hover:bg-purple-100 dark:hover:bg-purple-900/60 transition-colors"
					data-mention-type="doc"
				>
					<FileText className="shrink-0" width={12} height={12} />
					{props.inlineContent.props.title}
				</span>
			),
	},
);
