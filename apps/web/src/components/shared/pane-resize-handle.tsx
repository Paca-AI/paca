import { useRef } from "react";
import type { PaneWidth, PaneWidthConfig } from "@/lib/pane-width";
import { cn } from "@/lib/utils";

interface PaneResizeHandleProps {
	config: PaneWidthConfig;
	pane: PaneWidth;
	label: string;
	/** Which edge of the pane the handle sits on: dragging right grows a
	 *  pane whose handle is on its `right` edge. */
	edge: "left" | "right";
	testId?: string;
	className?: string;
	/** Draw a grip line at rest so the splitter is discoverable where the
	 *  surrounding border is faint. */
	showGrip?: boolean;
}

/** Keyboard-accessible vertical splitter (WAI-ARIA window splitter pattern).
 *  Double-click or Enter resets the pane to its default width. */
export function PaneResizeHandle({
	config,
	pane,
	label,
	edge,
	testId,
	className,
	showGrip,
}: PaneResizeHandleProps) {
	const dragRef = useRef<{ startX: number; startWidth: number } | null>(null);
	const sign = edge === "right" ? 1 : -1;

	function endDrag() {
		if (!dragRef.current) return;
		dragRef.current = null;
		pane.setDragging(false);
		pane.commit();
	}

	return (
		// biome-ignore lint/a11y/useSemanticElements: draggable splitter
		<div
			role="separator"
			aria-orientation="vertical"
			aria-label={label}
			aria-valuemin={config.minWidth}
			aria-valuemax={pane.maxWidth}
			aria-valuenow={pane.width}
			tabIndex={0}
			data-testid={testId}
			className={cn(
				"group/handle absolute inset-y-0 z-40 w-2 cursor-col-resize touch-none select-none transition-colors hover:bg-primary/30 focus-visible:bg-primary/40 focus-visible:outline-none active:bg-primary/40",
				edge === "right" ? "-right-1" : "-left-1",
				className,
			)}
			onPointerDown={(e) => {
				if (e.button !== 0) return;
				e.currentTarget.setPointerCapture(e.pointerId);
				dragRef.current = { startX: e.clientX, startWidth: pane.width };
				pane.setDragging(true);
			}}
			onPointerMove={(e) => {
				const drag = dragRef.current;
				if (drag)
					pane.setWidth(drag.startWidth + sign * (e.clientX - drag.startX));
			}}
			onPointerUp={endDrag}
			onPointerCancel={endDrag}
			onLostPointerCapture={endDrag}
			onDoubleClick={pane.reset}
			onKeyDown={(e) => {
				let next: number;
				if (e.key === "ArrowRight") next = pane.width + sign * config.keyStep;
				else if (e.key === "ArrowLeft")
					next = pane.width - sign * config.keyStep;
				else if (e.key === "Home") next = config.minWidth;
				else if (e.key === "End") next = Number.POSITIVE_INFINITY;
				else if (e.key === "Enter") {
					e.preventDefault();
					pane.reset();
					return;
				} else return;
				e.preventDefault();
				pane.setWidth(next);
			}}
			onKeyUp={(e) => {
				if (["ArrowLeft", "ArrowRight", "Home", "End"].includes(e.key))
					pane.commit();
			}}
		>
			{showGrip && (
				<span
					aria-hidden
					className="pointer-events-none absolute inset-y-2 left-1/2 w-0.5 -translate-x-1/2 rounded-full bg-muted-foreground/40 transition-colors group-hover/handle:bg-primary group-focus-visible/handle:bg-primary group-active/handle:bg-primary"
				/>
			)}
		</div>
	);
}
