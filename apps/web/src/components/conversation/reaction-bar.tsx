export interface ReactionCount {
	emoji: string;
	count: number;
	mine: boolean;
}

/** The emoji reactions under a message; each is a toggle for the viewer. */
export function ReactionBar({
	reactions,
	disabled,
	onToggle,
}: {
	reactions: ReactionCount[];
	disabled?: boolean;
	onToggle: (emoji: string, on: boolean) => void;
}) {
	if (reactions.length === 0) return null;
	return (
		<div className="mt-1 flex flex-wrap gap-1">
			{reactions.map((r) => (
				<button
					key={r.emoji}
					type="button"
					disabled={disabled}
					aria-pressed={r.mine}
					onClick={() => onToggle(r.emoji, !r.mine)}
					className={`inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs ${
						r.mine
							? "border-primary/50 bg-primary/10"
							: "border-border hover:bg-muted"
					}`}
				>
					<span>{r.emoji}</span>
					<span>{r.count}</span>
				</button>
			))}
		</div>
	);
}
