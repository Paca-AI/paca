// Splits a shell-style command string into argv, honoring single/double
// quotes so a quoted argument (e.g. --arg "hello world") isn't split on its
// internal whitespace. Not a full POSIX shell parser (no pipes, redirects,
// `$()`, etc.) — just enough for the one-line ACP commands entered in the
// agent setup UI.
export function splitShellCommand(command: string): string[] {
	const args: string[] = [];
	let current = "";
	let quote: '"' | "'" | null = null;
	let hasContent = false;

	for (let i = 0; i < command.length; i++) {
		const char = command[i];

		if (quote) {
			if (char === quote) {
				quote = null;
			} else if (char === "\\" && quote === '"' && i + 1 < command.length) {
				current += command[++i];
			} else {
				current += char;
			}
			continue;
		}

		if (char === '"' || char === "'") {
			quote = char;
			hasContent = true;
		} else if (char === "\\" && i + 1 < command.length) {
			current += command[++i];
			hasContent = true;
		} else if (/\s/.test(char)) {
			if (hasContent) {
				args.push(current);
				current = "";
				hasContent = false;
			}
		} else {
			current += char;
			hasContent = true;
		}
	}

	if (hasContent) {
		args.push(current);
	}

	return args;
}
