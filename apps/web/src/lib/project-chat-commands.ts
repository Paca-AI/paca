export type ProjectChatCommandKind = "update-description" | "record-conclusion";

export interface ParsedProjectChatCommand {
	kind: ProjectChatCommandKind;
	token: string;
	argument: string;
}

const PRIMARY_COMMAND_TOKENS: Record<ProjectChatCommandKind, string> = {
	"update-description": "/update-description",
	"record-conclusion": "/record-conclusion",
};

const COMMAND_TOKENS: Record<ProjectChatCommandKind, readonly string[]> = {
	"update-description": [
		PRIMARY_COMMAND_TOKENS["update-description"],
		"/更新描述",
	],
	"record-conclusion": [
		PRIMARY_COMMAND_TOKENS["record-conclusion"],
		"/记录结论",
	],
};

export function projectChatCommandToken(kind: ProjectChatCommandKind): string {
	return PRIMARY_COMMAND_TOKENS[kind];
}

export function parseProjectChatCommand(
	input: string,
): ParsedProjectChatCommand | null {
	const value = input.trim();
	for (const [kind, tokens] of Object.entries(COMMAND_TOKENS) as Array<
		[ProjectChatCommandKind, readonly string[]]
	>) {
		for (const token of tokens) {
			if (value === token) return { kind, token, argument: "" };
			if (value.startsWith(`${token} `)) {
				return {
					kind,
					token,
					argument: value.slice(token.length).trim(),
				};
			}
		}
	}
	return null;
}

export function isProjectChatCommandQuery(input: string): boolean {
	return input.startsWith("/") && !/\s/.test(input);
}
