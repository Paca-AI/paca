export function LoginFooter() {
	return (
		<footer className="flex flex-wrap items-center justify-center gap-x-5 gap-y-1.5 px-5 py-4 text-xs text-(--sea-ink-soft)">
			<span>© {new Date().getFullYear()} Paca</span>
			<span className="hidden sm:inline opacity-30">·</span>
			<a
				href="https://github.com/Paca-AI/paca"
				target="_blank"
				rel="noopener noreferrer"
				className="hover:text-(--sea-ink)"
			>
				GitHub
			</a>
			<span className="opacity-30">·</span>
			<a
				href="https://github.com/Paca-AI/paca/tree/master/docs"
				target="_blank"
				rel="noopener noreferrer"
				className="hover:text-(--sea-ink)"
			>
				Docs
			</a>
			<span className="opacity-30">·</span>
			<a
				href="https://github.com/Paca-AI/paca/blob/HEAD/LICENSE"
				target="_blank"
				rel="noopener noreferrer"
				className="hover:text-(--sea-ink)"
			>
				Apache-2.0
			</a>
		</footer>
	);
}
