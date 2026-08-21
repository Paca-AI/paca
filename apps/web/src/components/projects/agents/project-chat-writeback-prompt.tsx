import { useMutation, useQueries, useQueryClient } from "@tanstack/react-query";
import { FileText } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
	confirmProjectConclusion,
	listTaskConclusions,
	type ProjectChatTurnHistoryItem,
	prepareProjectConclusion,
} from "@/lib/agent-api";
import { descriptionFromMarkdown } from "@/lib/conclusion-description";
import { taskQueryOptions } from "@/lib/interaction-api";
import { parseProjectChatCommand } from "@/lib/project-chat-commands";
import {
	ProjectChatPromptCard,
	type ProjectChatPromptChoiceGroup,
} from "./project-chat-prompt-card";

type Action = "writeback" | "revise";
type PendingKey = { fingerprint: string; key: string };
type PendingPreparationKey = PendingKey & { expiresAt: string };

function requestKey(
	ref: React.RefObject<PendingKey | null>,
	fingerprint: string,
) {
	if (ref.current?.fingerprint !== fingerprint) {
		ref.current = { fingerprint, key: crypto.randomUUID() };
	}
	return ref.current.key;
}

function handledStorageKey(sessionId: string) {
	return `paca:chat:${sessionId}:handled-writeback-turns`;
}

function readHandledTurns(sessionId: string): string[] {
	if (typeof window === "undefined") return [];
	try {
		const value = JSON.parse(
			window.sessionStorage.getItem(handledStorageKey(sessionId)) ?? "[]",
		);
		return Array.isArray(value)
			? value.filter((item): item is string => typeof item === "string")
			: [];
	} catch {
		return [];
	}
}

function writeHandledTurn(sessionId: string, turnId: string) {
	if (typeof window === "undefined") return;
	const values = new Set(readHandledTurns(sessionId));
	values.add(turnId);
	window.sessionStorage.setItem(
		handledStorageKey(sessionId),
		JSON.stringify([...values].slice(-50)),
	);
}

export function ProjectChatWritebackPrompt({
	projectId,
	sessionId,
	sourceItem,
	relatedTaskIds,
	onContinue,
}: {
	projectId: string;
	sessionId: string;
	sourceItem: ProjectChatTurnHistoryItem | null;
	relatedTaskIds: string[];
	onContinue: (message: string) => Promise<void>;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const command = sourceItem
		? parseProjectChatCommand(sourceItem.turn.input_text)
		: null;
	const sourceTurnId = sourceItem?.turn.id;
	const [handled, setHandled] = useState(() =>
		sourceTurnId ? readHandledTurns(sessionId).includes(sourceTurnId) : false,
	);
	const [action, setAction] = useState<Action>("writeback");
	const [feedback, setFeedback] = useState("");
	const [targetTaskId, setTargetTaskId] = useState<string>();
	const prepareKeyRef = useRef<PendingPreparationKey | null>(null);
	const confirmKeyRef = useRef<PendingKey | null>(null);

	const relatedQueries = useQueries({
		queries: relatedTaskIds.map((taskId) => ({
			...taskQueryOptions(projectId, taskId),
			enabled: !!command && !handled,
		})),
	});
	const relatedTasks = useMemo(
		() => relatedQueries.flatMap((query) => (query.data ? [query.data] : [])),
		[relatedQueries],
	);
	const relatedTasksLoading = relatedQueries.some((query) => query.isLoading);
	const targetTask = relatedTasks.find((task) => task.id === targetTaskId);
	const publicationQueries = useQueries({
		queries: relatedTaskIds.map((taskId) => ({
			queryKey: [
				"projects",
				projectId,
				"tasks",
				taskId,
				"conclusions",
				"source",
				sourceTurnId,
			] as const,
			queryFn: () => listTaskConclusions(projectId, taskId, { limit: 50 }),
			enabled: !!command && !!sourceTurnId && !handled,
			staleTime: 15_000,
		})),
	});
	const alreadyPublished = publicationQueries.some((query) =>
		query.data?.items.some(
			(publication) => publication.source_turn_id === sourceTurnId,
		),
	);
	const publicationStateLoading = publicationQueries.some(
		(query) => query.isLoading,
	);

	useEffect(() => {
		setHandled(
			sourceTurnId ? readHandledTurns(sessionId).includes(sourceTurnId) : false,
		);
		setAction("writeback");
		setFeedback("");
		setTargetTaskId(
			relatedTaskIds.length === 1 ? relatedTaskIds[0] : undefined,
		);
		prepareKeyRef.current = null;
		confirmKeyRef.current = null;
	}, [relatedTaskIds, sessionId, sourceTurnId]);

	const markHandled = () => {
		if (sourceTurnId) writeHandledTurn(sessionId, sourceTurnId);
		setHandled(true);
	};

	const revisionMutation = useMutation({
		mutationFn: async () => {
			if (!command || !feedback.trim()) throw new Error("missing revision");
			const message = `${command.token} ${feedback.trim()}`;
			await onContinue(message);
		},
		onSuccess: markHandled,
	});

	const writebackMutation = useMutation({
		mutationFn: async () => {
			if (!command || !sourceItem || !targetTask) {
				throw new Error("missing writeback context");
			}
			const content = sourceItem.result?.stable_output?.trim();
			if (!content) throw new Error("missing stable output");
			const updateDescription = command.kind === "update-description";
			const proposedDescription = updateDescription
				? descriptionFromMarkdown(content)
				: undefined;
			const preparationFingerprint = JSON.stringify({
				turnId: sourceItem.turn.id,
				targetTaskId: targetTask.id,
				content,
				updateDescription,
				currentDescription: targetTask.description ?? null,
				proposedDescription,
			});
			if (prepareKeyRef.current?.fingerprint !== preparationFingerprint) {
				prepareKeyRef.current = {
					fingerprint: preparationFingerprint,
					key: crypto.randomUUID(),
					expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(),
				};
			}
			const prepared = await prepareProjectConclusion(
				projectId,
				sourceItem.turn.id,
				{
					target_task_id: targetTask.id,
					summary_override: content,
					update_description: updateDescription,
					...(proposedDescription
						? {
								description_base: targetTask.description ?? null,
								proposed_description: proposedDescription,
							}
						: {}),
					expires_at: prepareKeyRef.current.expiresAt,
				},
				prepareKeyRef.current.key,
			);
			const payload = {
				preparation_id: prepared.preparation.id,
				expected_version: prepared.preparation.summary_version,
				expected_sha256: prepared.preparation.summary_sha256,
			};
			return confirmProjectConclusion(
				projectId,
				payload,
				requestKey(confirmKeyRef, JSON.stringify(payload)),
			);
		},
		onSuccess: ({ publication }) => {
			markHandled();
			void qc.invalidateQueries({
				queryKey: ["projects", projectId, "tasks", publication.target_task_id],
			});
		},
	});

	if (
		!command ||
		!sourceItem ||
		handled ||
		alreadyPublished ||
		publicationStateLoading
	)
		return null;

	const actionOptions = [
		{
			value: "writeback",
			label:
				command.kind === "update-description"
					? t("chats.conclusion.confirmDescription")
					: t("chats.conclusion.confirmSummary"),
		},
	];
	const groups: ProjectChatPromptChoiceGroup[] = [];
	if (relatedTasks.length > 1) {
		groups.push({
			id: "target",
			options: relatedTasks.map((task) => ({
				value: task.id,
				label: (
					<>
						<FileText className="size-3.5 text-primary" />
						<span className="text-xs text-muted-foreground">
							#{task.task_number}
						</span>
						<span className="max-w-52 truncate">{task.title}</span>
					</>
				),
			})),
			selectedValues: targetTaskId ? [targetTaskId] : [],
			onSelectedValuesChange: (values) => setTargetTaskId(values[0]),
		});
	}
	groups.push({
		id: "action",
		options: actionOptions,
		selectedValues: action === "writeback" ? [action] : [],
		onSelectedValuesChange: (values) => {
			if (values[0] !== "writeback") return;
			setAction("writeback");
			setFeedback("");
		},
	});

	const mutationError = revisionMutation.error ?? writebackMutation.error;
	const missingTask = relatedTaskIds.length === 0;
	const taskLoadFailed = relatedQueries.some((query) => query.isError);
	const publicationStateFailed = publicationQueries.some(
		(query) => query.isError,
	);
	const pending = revisionMutation.isPending || writebackMutation.isPending;
	const error = missingTask
		? t("chats.context.add")
		: taskLoadFailed
			? t("chats.notFound")
			: publicationStateFailed
				? t("chats.conclusion.publicationStateUnavailable")
				: mutationError?.message;

	return (
		<ProjectChatPromptCard
			groups={groups}
			input={{
				value: feedback,
				placeholder: t("chats.conclusion.revisionPlaceholder"),
				onFocus: () => setAction("revise"),
				onChange: (value) => {
					setFeedback(value);
					setAction("revise");
				},
			}}
			error={error}
			pending={pending}
			confirmDisabled={
				relatedTasksLoading ||
				publicationStateFailed ||
				(action === "writeback" && !targetTask) ||
				(action === "revise" && !feedback.trim())
			}
			cancelLabel={t("chats.conclusion.cancel")}
			confirmLabel={t("chats.conclusion.confirmAction")}
			onCancel={markHandled}
			onConfirm={() => {
				if (action === "revise") revisionMutation.mutate();
				else writebackMutation.mutate();
			}}
		/>
	);
}
