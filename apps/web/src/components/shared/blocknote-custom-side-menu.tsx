import type { Block } from "@blocknote/core";
import { SideMenuExtension } from "@blocknote/core/extensions";
import {
	AddBlockButton,
	DragHandleButton,
	SideMenu,
	useExtensionState,
} from "@blocknote/react";

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
