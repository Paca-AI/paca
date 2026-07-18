import type * as React from "react";

import {
	chordKeycaps,
	type DisplayChord,
	isMacPlatform,
} from "@/lib/shortcuts/keymap";
import { cn } from "@/lib/utils";

/** A single keycap, e.g. "⌘" or "F". */
export function Kbd({ className, ...props }: React.ComponentProps<"kbd">) {
	return (
		<kbd
			className={cn(
				"inline-flex min-w-[1.15rem] items-center justify-center rounded-[5px] border border-b-2 border-border/70 bg-muted/60 px-1.5 py-0.5 text-center font-[JetBrains_Mono,monospace] text-[10px] font-semibold leading-none text-muted-foreground/90",
				className,
			)}
			{...props}
		/>
	);
}

/** Renders a full chord (e.g. ⌘ + Shift + F) as adjacent keycaps. */
export function KbdChord({
	chord,
	className,
}: {
	chord: DisplayChord;
	className?: string;
}) {
	const isMac = isMacPlatform();
	const caps = chordKeycaps(chord, isMac);
	return (
		<span className={cn("inline-flex items-center gap-0.5", className)}>
			{caps.map((cap, i) => (
				// biome-ignore lint/suspicious/noArrayIndexKey: static, order-stable list of 1-3 keycaps
				<Kbd key={i}>{cap}</Kbd>
			))}
		</span>
	);
}
