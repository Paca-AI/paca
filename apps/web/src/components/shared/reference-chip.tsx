import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { FileText, Hash, Lock } from "lucide-react";
import { useTranslation } from "react-i18next";
import { docQueryOptions } from "@/lib/doc-api";
import { taskQueryOptions } from "@/lib/interaction-api";

const CHIP =
	"inline-flex max-w-full items-center gap-1 rounded-full px-1.5 py-0.5 align-baseline text-xs font-medium";

/**
 * A task or document a message links to. The message stores ids only; the
 * title is read here with the reader's own access, so someone who may not open
 * it sees a locked chip and never learns its name. A task chip shares the
 * task query the rest of the app uses, so it follows the task's live updates.
 */
export function ReferenceChip({
	kind,
	id,
	projectId,
	number,
	interactive = true,
}: {
	/** False inside an editor, where a click must place the caret, not navigate. */
	interactive?: boolean;
	kind: "task" | "doc";
	id: string;
	projectId: string;
	number?: number;
}) {
	const { t } = useTranslation("chat");
	const task = useQuery({
		...taskQueryOptions(projectId, id),
		enabled: kind === "task" && !!projectId && !!id,
		retry: false,
	});
	const doc = useQuery({
		...docQueryOptions(projectId, id),
		enabled: kind === "doc" && !!projectId && !!id,
		retry: false,
	});
	const q = kind === "task" ? task : doc;
	const label = kind === "task" && number ? `#${number}` : "#";

	if (q.isError) {
		return (
			<span
				data-reference="locked"
				title={t("reference.locked")}
				className={`${CHIP} bg-muted text-muted-foreground`}
			>
				<Lock className="size-3 shrink-0" aria-hidden />
				<span>
					{label === "#" ? t("reference.doc") : label}
					<span className="sr-only"> {t("reference.locked")}</span>
				</span>
			</span>
		);
	}

	const title = (kind === "task" ? task.data?.title : doc.data?.title) ?? "";
	const body = (
		<>
			{kind === "task" ? (
				<Hash className="size-3 shrink-0" aria-hidden />
			) : (
				<FileText className="size-3 shrink-0" aria-hidden />
			)}
			<span className="truncate">
				{kind === "task" && number ? <span>{number} </span> : null}
				{title}
			</span>
		</>
	);
	const tone =
		kind === "task"
			? "bg-success/10 text-success hover:bg-success/20"
			: "bg-purple-50/80 text-purple-700 hover:bg-purple-100 dark:bg-purple-900/40 dark:text-purple-300";
	if (!title) {
		return (
			<span data-reference={kind} className={`${CHIP} ${tone} opacity-60`}>
				{body}
				<span>{label}</span>
			</span>
		);
	}
	if (!interactive) {
		return (
			<span data-reference={kind} className={`${CHIP} ${tone}`}>
				{body}
			</span>
		);
	}
	return kind === "task" ? (
		<Link
			to="/projects/$projectId/tasks/$taskId"
			params={{ projectId, taskId: id }}
			data-reference="task"
			className={`${CHIP} ${tone}`}
		>
			{body}
		</Link>
	) : (
		<Link
			to="/projects/$projectId/docs/$docId"
			params={{ projectId, docId: id }}
			data-reference="doc"
			className={`${CHIP} ${tone}`}
		>
			{body}
		</Link>
	);
}
