import type { Block } from "@blocknote/core";
import { SideMenuExtension, SuggestionMenu } from "@blocknote/core/extensions";
import {
	DragHandleButton,
	SideMenu,
	useBlockNoteEditor,
	useComponentsContext,
	useDictionary,
	useExtension,
	useExtensionState,
} from "@blocknote/react";
import { Plus } from "lucide-react";
import { useCallback } from "react";

/** True when the block holds exactly the "/" that opened the menu. */
function isOnlySlash(block: Block | undefined): boolean {
	const content = block?.content;
	return (
		Array.isArray(content) &&
		content.length === 1 &&
		content[0].type === "text" &&
		content[0].text === "/"
	);
}

/**
 * The "+" button of an empty block: opens the same menu as typing "/".
 *
 * BlockNote's own button opens the menu without putting a "/" in the block,
 * which in a collaborative editor (the document editor, the task description)
 * makes the menu track its start through a Yjs relative position that lies
 * outside the paragraph. The first cursor update from the collaboration
 * provider resolves it to another block and the menu closes by itself, so the
 * options could be reached only by keyboard before it vanished. Inserting the
 * "/" first anchors the menu inside the paragraph, exactly as when it is typed.
 * Dismissing the menu without choosing removes that "/" again.
 */
function AddBlockButton() {
	const Components = useComponentsContext();
	const dict = useDictionary();
	const editor = useBlockNoteEditor();
	const suggestionMenu = useExtension(SuggestionMenu);
	const block = useExtensionState(SideMenuExtension, {
		selector: (state) => state?.block as Block | undefined,
	});

	const open = useCallback(() => {
		if (!block) return;
		const isEmpty = Array.isArray(block.content) && block.content.length === 0;
		const target: { id: string } = isEmpty
			? block
			: editor.insertBlocks([{ type: "paragraph" }], block, "after")[0];
		editor.setTextCursorPosition(target.id);
		// Only a collaborative editor (the document editor, the task description)
		// has the problem described above; elsewhere BlockNote's own call is
		// correct and leaves no "/" in the block.
		const collaborative = !!(
			editor as unknown as { getExtension: (key: string) => unknown }
		).getExtension("ySync");
		if (!collaborative) {
			suggestionMenu.openSuggestionMenu("/");
			return;
		}

		// Open the menu the way typing "/" does: the character is inserted by one
		// transaction and the menu opened by the next. BlockNote's own
		// openSuggestionMenu() does both in a single transaction, before the
		// collaboration binding has seen the new character, so the menu loses its
		// anchor and closes.
		const view = editor.prosemirrorView;
		const typed =
			!!view &&
			(view.focus(),
			view.someProp("handleTextInput", (handle) => {
				const { from, to } = view.state.selection;
				return handle(view, from, to, "/", () =>
					view.state.tr.insertText("/", from, to),
				);
			}));
		if (!typed) {
			suggestionMenu.openSuggestionMenu("/", { deleteTriggerCharacter: true });
		}

		// Leave no stray "/" behind when the menu is dismissed unused. Choosing an
		// item closes the menu first and then deletes the "/" and applies the item
		// in the same tick, so look again after that tick before cleaning up.
		let seenOpen = suggestionMenu.store.state?.show === true;
		const unsubscribe = suggestionMenu.store.subscribe(() => {
			if (suggestionMenu.store.state?.show) {
				seenOpen = true;
				return;
			}
			if (!seenOpen) return;
			unsubscribe();
			setTimeout(() => {
				const current = editor.getBlock(target.id);
				if (isOnlySlash(current as Block | undefined)) {
					editor.updateBlock(target.id, { content: [] });
				}
			}, 0);
		});
	}, [block, editor, suggestionMenu]);

	if (!block || !Components) return null;
	return (
		<Components.SideMenu.Button
			className="bn-button"
			label={dict.side_menu.add_block_label}
			icon={<Plus size={24} onClick={open} data-test="dragHandleAdd" />}
		/>
	);
}

export const CustomSideMenu = () => {
	const block = useExtensionState(SideMenuExtension, {
		selector: (state) => state?.block as Block | undefined,
	});

	if (!block) {
		return null;
	}

	// Only "inline" content blocks (paragraph, heading, ...) can be empty in a
	// meaningful sense — show "+" for those so the user can pick a block type.
	// Every other content model ("none" or "table": image, video, file, table,
	// divider, mermaid, ...) has no such empty state and always gets the drag
	// handle, since block.content is never an array for them.
	const isEmptyTextBlock =
		Array.isArray(block.content) && block.content.length === 0;

	return (
		<SideMenu>
			{isEmptyTextBlock ? <AddBlockButton /> : <DragHandleButton />}
		</SideMenu>
	);
};
