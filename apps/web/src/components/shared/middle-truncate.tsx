import { cn } from "@/lib/utils";

const segmenter = new Intl.Segmenter(undefined, { granularity: "grapheme" });
const TAIL_LENGTH = 8;

/** Let CSS elide the middle only when needed, keeping a name's distinguishing
 * suffix visible. Screen readers and the native tooltip retain the full name. */
export function MiddleTruncate({
	text,
	className,
}: {
	text: string;
	className?: string;
}) {
	const graphemes = Array.from(
		segmenter.segment(text),
		({ segment }) => segment,
	);
	if (graphemes.length <= TAIL_LENGTH) {
		return (
			<span className={cn("min-w-0 truncate", className)} title={text}>
				{text}
			</span>
		);
	}

	return (
		<span
			className={cn("inline-flex min-w-0 max-w-full", className)}
			title={text}
		>
			<span className="sr-only">{text}</span>
			<span aria-hidden="true" className="min-w-0 truncate">
				{graphemes.slice(0, -TAIL_LENGTH).join("")}
			</span>
			<span
				aria-hidden="true"
				className="flex max-w-full shrink-0 justify-end overflow-hidden whitespace-pre"
			>
				<span className="shrink-0">
					{graphemes.slice(-TAIL_LENGTH).join("")}
				</span>
			</span>
		</span>
	);
}
