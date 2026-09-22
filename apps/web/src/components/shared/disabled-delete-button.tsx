import { Trash2 } from "lucide-react";
import { useId } from "react";

import { Button } from "@/components/ui/button";

interface DisabledDeleteButtonProps {
	/** The button's accessible name, e.g. "Delete role". */
	label: string;
	/** Why it can't be used, e.g. "The default role can't be deleted…". */
	reason: string;
}

/**
 * A delete button that can't be used, and says why. A disabled button gets no
 * hover or focus of its own, so the reason rides on the wrapping span as a
 * tooltip for the mouse and as a visually hidden description that assistive
 * technology reads with the button.
 */
export function DisabledDeleteButton({
	label,
	reason,
}: DisabledDeleteButtonProps) {
	const reasonId = useId();
	return (
		<span title={reason}>
			<Button
				variant="ghost"
				size="icon-sm"
				className="text-destructive"
				disabled
				aria-label={label}
				aria-describedby={reasonId}
			>
				<Trash2 className="size-3.5" />
			</Button>
			<span id={reasonId} className="sr-only">
				{reason}
			</span>
		</span>
	);
}
