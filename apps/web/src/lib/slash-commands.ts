import type { ChatBlock } from "./chat-api";
import { parseCommand } from "./chat-blocks";

/**
 * A "/" command: listed in the editor's "/" menu next to the block types, and
 * run when the message or comment that starts with it is submitted, instead of
 * being posted. The same list serves chat and task and document comments.
 */
export interface SlashCommand {
	name: string;
	/** Menu label; defaults to "/name". */
	title?: string;
	description: string;
	/** Shown after the name in a usage hint, e.g. "<title>". */
	hint?: string;
	/** The command needs text after its name; picking it types the name. */
	requiresArgs?: boolean;
	run: (args: string) => Promise<void>;
}

/** The command a submitted message is, with its arguments, or null. */
export function findCommand(
	commands: SlashCommand[] | undefined,
	blocks: ChatBlock[],
): { command: SlashCommand; args: string } | null {
	if (!commands || commands.length === 0) return null;
	const parsed = parseCommand(blocks);
	const command = parsed && commands.find((c) => c.name === parsed.name);
	return command && parsed ? { command, args: parsed.args } : null;
}
