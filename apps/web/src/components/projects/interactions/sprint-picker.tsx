import { BookOpen, KanbanSquare } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import { useSprintSearch } from "@/hooks/use-sprint-search";
import { useSprintDirectory } from "@/lib/sprint-directory";
import { SearchableList } from "./task-detail/property-field/searchable-list";

const PICKABLE_STATUSES = ["active", "planned"] as const;

/** Single-select sprint picker for the task detail panel: shows the task's
 * current sprint (or the product backlog) and, when editable, opens a
 * searchable popover that pages the project's open sprints on the server.
 * Completed sprints can't be assigned to, so they aren't offered. */
export function SprintPicker({
	projectId,
	sprintId,
	canEdit,
	onChange,
}: {
	projectId: string;
	/** The task's sprint; null for the product backlog. */
	sprintId: string | null;
	canEdit: boolean;
	onChange: (sprintId: string | null) => void;
}) {
	const { t } = useTranslation("projects");
	const [open, setOpen] = useState(false);
	const getSprint = useSprintDirectory(projectId);
	const search = useSprintSearch(projectId, open && canEdit, {
		status: [...PICKABLE_STATUSES],
	});
	const current = sprintId ? getSprint(sprintId) : undefined;

	const trigger = (
		<span className="inline-flex items-center gap-2 rounded-full border border-border/50 bg-muted/30 px-3 py-1 text-sm font-semibold text-muted-foreground">
			{sprintId ? (
				<KanbanSquare className="size-3 shrink-0 text-muted-foreground" />
			) : (
				<BookOpen className="size-3 shrink-0 opacity-60" />
			)}
			{sprintId
				? (current?.name ?? sprintId.slice(0, 8))
				: t("taskDetail.properties.productBacklog")}
		</span>
	);

	if (!canEdit) return trigger;

	return (
		<Popover open={open} onOpenChange={setOpen}>
			<PopoverTrigger
				type="button"
				className="transition-opacity duration-150 hover:opacity-80"
			>
				{trigger}
			</PopoverTrigger>
			<PopoverContent
				className="w-56 p-1 rounded-xl border border-border/60 shadow-lg"
				align="start"
			>
				<SearchableList
					remote={search}
					pinned={
						<button
							type="button"
							className="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-sm hover:bg-muted/60 transition-colors duration-100"
							onClick={() => {
								onChange(null);
								setOpen(false);
							}}
						>
							<BookOpen className="size-3.5 opacity-60" />
							<span className="flex-1 text-left">
								{t("taskDetail.properties.productBacklog")}
							</span>
						</button>
					}
					items={search.sprints.map((s) => ({
						key: s.id,
						label: s.name,
						selected: s.id === sprintId,
						onSelect: () => {
							onChange(s.id);
							setOpen(false);
						},
						content: (
							<>
								<KanbanSquare className="size-3.5 shrink-0 text-muted-foreground" />
								<span className="flex-1 truncate text-left">{s.name}</span>
								{s.status === "planned" && (
									<span className="text-[11px] text-muted-foreground/90">
										{t("taskDetail.properties.sprintNotStarted")}
									</span>
								)}
							</>
						),
					}))}
				/>
			</PopoverContent>
		</Popover>
	);
}
