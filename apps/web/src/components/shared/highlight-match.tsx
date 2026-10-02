import { Fragment } from "react";

function escapeRegExp(s: string): string {
	return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/** Renders `text` with every occurrence of any whitespace-separated word of
 *  `query` wrapped in <mark>, case-insensitively. Returns plain text when
 *  there is nothing to highlight. */
export function HighlightMatch({
	text,
	query,
}: {
	text: string;
	query: string;
}) {
	const words = query.trim().split(/\s+/).filter(Boolean);
	if (words.length === 0) return <>{text}</>;
	const re = new RegExp(`(${words.map(escapeRegExp).join("|")})`, "gi");
	const lower = new Set(words.map((w) => w.toLowerCase()));
	return (
		<>
			{text.split(re).map((part, i) =>
				lower.has(part.toLowerCase()) ? (
					<mark
						// biome-ignore lint/suspicious/noArrayIndexKey: positional split
						key={i}
						className="rounded-sm bg-primary/20 text-inherit"
					>
						{part}
					</mark>
				) : (
					// biome-ignore lint/suspicious/noArrayIndexKey: positional split
					<Fragment key={i}>{part}</Fragment>
				),
			)}
		</>
	);
}
