export function LoginFooter() {
	return (
		<footer className="flex flex-wrap items-center justify-center gap-x-4 gap-y-1.5 px-5 pb-5 pt-2 text-xs text-(--sea-ink-soft)/60">
			<span>© {new Date().getFullYear()} Paca</span>
			<span className="opacity-40">·</span>
			<a
				href="https://github.com/Paca-AI/paca"
				target="_blank"
				rel="noopener noreferrer"
				className="transition-colors hover:text-(--sea-ink)"
			>
				GitHub
			</a>
			<span className="opacity-40">·</span>
			<a
				href="https://github.com/Paca-AI/paca/tree/HEAD/docs"
				target="_blank"
				rel="noopener noreferrer"
				className="transition-colors hover:text-(--sea-ink)"
			>
				Docs
			</a>
			<span className="opacity-40">·</span>
			<a
				href="https://github.com/Paca-AI/paca/blob/HEAD/LICENSE"
				target="_blank"
				rel="noopener noreferrer"
				className="transition-colors hover:text-(--sea-ink)"
			>
				Apache-2.0
			</a>
		</footer>
	);
}
