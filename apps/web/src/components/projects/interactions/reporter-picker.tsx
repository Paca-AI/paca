import { Check, User } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { EntityAvatar } from "@/components/shared/entity-avatar";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import { useMemberSearch } from "@/hooks/use-member-search";
import type { ProjectMember } from "@/lib/project-api";
import { memberAvatar } from "@/lib/provider-logos";
import { SearchableList } from "./task-detail/property-field/searchable-list";

const memberName = (m: ProjectMember) => m.full_name || m.username;

/** Single-select reporter picker for the task detail panel: shows the current
 * reporter (or "None") and, when editable, opens a searchable popover that
 * pages the project's members on the server. */
export function ReporterPicker({
	projectId,
	reporter,
	reporterId,
	canEdit,
	onChange,
}: {
	projectId: string;
	/** The resolved reporter, when already known. */
	reporter?: ProjectMember;
	reporterId: string | null;
	canEdit: boolean;
	onChange: (reporterId: string | null) => void;
}) {
	const { t } = useTranslation("projects");
	const [open, setOpen] = useState(false);
	const search = useMemberSearch(projectId, open && canEdit);

	const trigger = reporter ? (
		<span className="flex min-w-0 items-center gap-2.5">
			<EntityAvatar {...memberAvatar(reporter)} size="sm" />
			<span className="truncate text-sm font-medium text-foreground">
				{memberName(reporter)}
			</span>
		</span>
	) : (
		<span className="inline-flex items-center gap-1.5 text-sm italic text-muted-foreground/90">
			<User className="size-3.5 opacity-60" />
			{t("taskDetail.common.none")}
		</span>
	);

	if (!canEdit) return trigger;

	return (
		<Popover open={open} onOpenChange={setOpen}>
			<PopoverTrigger
				type="button"
				className="flex min-w-0 items-center transition-opacity duration-150 hover:opacity-80"
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
							className="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-sm text-muted-foreground hover:bg-muted/60 transition-colors duration-100"
							onClick={() => {
								onChange(null);
								setOpen(false);
							}}
						>
							<User className="size-3.5 opacity-60" />
							<span className="flex-1 text-left">
								{t("taskDetail.common.none")}
							</span>
							{!reporterId && <Check className="size-3.5 text-primary" />}
						</button>
					}
					items={search.members.map((m) => ({
						key: m.id,
						label: memberName(m),
						selected: m.id === reporterId,
						onSelect: () => {
							onChange(m.id);
							setOpen(false);
						},
						content: (
							<>
								<EntityAvatar {...memberAvatar(m)} size="xs" />
								<span className="flex-1 text-left truncate">
									{memberName(m)}
								</span>
							</>
						),
					}))}
				/>
			</PopoverContent>
		</Popover>
	);
}
