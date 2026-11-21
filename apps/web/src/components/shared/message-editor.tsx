import { forwardRef } from "react";
import { cn } from "@/lib/utils";
import {
	CommentEditor,
	type CommentEditorHandle,
	type CommentEditorProps,
} from "./comment-blocknote";

type EditorProps = CommentEditorProps;

/**
 * The one rich-text box: task comments, document comments and chat (composer
 * and message edit) all render this, so the editor, its "@" "#" "/" menus and
 * its look are identical everywhere. It differs only by props: `submitKey`
 * ("mod-enter" for comments, "enter" for chat) and `commands`, the same
 * `useSlashCommands` list in every place.
 */
export const MessageEditor = forwardRef<
	CommentEditorHandle,
	EditorProps &
		Pick<
			React.ComponentProps<"fieldset">,
			"className" | "onFocus" | "onBlur"
		> & { focused?: boolean }
>(function MessageEditor(
	{ className, onFocus, onBlur, focused, ...editor },
	ref,
) {
	return (
		<fieldset
			className={cn(
				"min-w-0 flex-1 rounded-xl border border-border/50 bg-card/80 transition-all duration-200",
				"focus-within:border-primary/25 focus-within:shadow-sm focus-within:shadow-primary/5",
				focused && "border-primary/25 shadow-sm shadow-primary/5",
				"[&_.bn-editor]:min-h-6 [&_.bn-editor]:max-h-48 [&_.bn-editor]:overflow-y-auto [&_.bn-editor]:py-1.5 [&_.bn-editor]:px-3 [&_.bn-editor]:text-sm [&_.bn-editor]:leading-relaxed",
				className,
			)}
			onFocus={onFocus}
			onBlur={onBlur}
		>
			<CommentEditor ref={ref} {...editor} />
		</fieldset>
	);
});
