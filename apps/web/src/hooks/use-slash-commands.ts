import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import type { ChatBlock } from "@/lib/chat-api";
import {
	docCreatedBlocks,
	taskCreatedBlocks,
	titleFromText,
} from "@/lib/chat-tasks";
import { createDocument } from "@/lib/doc-api";
import { createTask } from "@/lib/interaction-api";
import { findCommand, type SlashCommand } from "@/lib/slash-commands";

/**
 * The "/" commands every rich editor offers (chat messages, task comments,
 * document comments): `/task <title>` and `/doc <title>` create the item and
 * post a chip linking to it into the conversation the command was typed in.
 *
 * `post` posts blocks there (a chat message, or a comment). `extra` adds
 * commands only one place has (chat's `/new`). A command is listed only when
 * the viewer may do what it does.
 */
export function useSlashCommands({
	projectId,
	post,
	extra,
}: {
	/** Direct messages have no project, so no task or document commands. */
	projectId: string | undefined;
	post: (blocks: ChatBlock[]) => Promise<unknown>;
	extra?: SlashCommand[];
}): SlashCommand[] {
	const { t } = useTranslation("shared");
	const queryClient = useQueryClient();
	const { hasProjectPermission } = useProjectPermissions(projectId ?? "");
	const postRef = useRef(post);
	postRef.current = post;
	const canTask = !!projectId && hasProjectPermission("tasks:write");
	const canDoc = !!projectId && hasProjectPermission("docs:write");

	return useMemo(() => {
		const list: SlashCommand[] = [];
		if (projectId && canTask) {
			list.push({
				name: "task",
				title: t("commands.task.title"),
				hint: "<title>",
				requiresArgs: true,
				description: t("commands.task.description"),
				run: async (args) => {
					const task = await createTask(projectId, {
						title: titleFromText(args),
					});
					await postRef.current(
						taskCreatedBlocks(t("commands.task.created"), task),
					);
					void queryClient.invalidateQueries({
						queryKey: ["projects", projectId, "tasks"],
					});
				},
			});
		}
		if (projectId && canDoc) {
			list.push({
				name: "doc",
				title: t("commands.doc.title"),
				hint: "<title>",
				requiresArgs: true,
				description: t("commands.doc.description"),
				run: async (args) => {
					const doc = await createDocument(projectId, {
						title: titleFromText(args),
					});
					await postRef.current(
						docCreatedBlocks(t("commands.doc.created"), doc),
					);
					void queryClient.invalidateQueries({
						queryKey: ["projects", projectId, "docs"],
					});
				},
			});
		}
		return [...list, ...(extra ?? [])];
	}, [projectId, canTask, canDoc, extra, queryClient, t]);
}

/**
 * Runs a submitted message as a command when it is one. One place for the
 * usage check, the busy state and the error, for chat and for comments.
 */
export function useCommandRunner(
	commands: SlashCommand[],
	describeError?: (err: unknown) => string,
) {
	const { t } = useTranslation("shared");
	const [running, setRunning] = useState(false);
	const [error, setError] = useState<string | null>(null);

	const run = useCallback(
		async (c: SlashCommand, args: string): Promise<boolean> => {
			if (c.requiresArgs && !args) {
				setError(
					t("commands.usage", {
						usage: `/${c.name} ${c.hint ?? ""}`.trim(),
					}),
				);
				return false;
			}
			setRunning(true);
			setError(null);
			try {
				await c.run(args);
				return true;
			} catch (err) {
				setError(describeError ? describeError(err) : t("commands.failed"));
				return false;
			} finally {
				setRunning(false);
			}
		},
		[describeError, t],
	);

	/** "none" when the blocks are not a command; otherwise whether it ran. */
	const tryRun = useCallback(
		async (blocks: ChatBlock[]): Promise<"none" | "ran" | "failed"> => {
			const hit = findCommand(commands, blocks);
			if (!hit) return "none";
			return (await run(hit.command, hit.args)) ? "ran" : "failed";
		},
		[commands, run],
	);

	return { run, tryRun, running, error, clearError: () => setError(null) };
}
