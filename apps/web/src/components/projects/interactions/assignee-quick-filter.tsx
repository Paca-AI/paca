import { useTranslation } from "react-i18next";
import { EntityAvatar } from "@/components/shared/entity-avatar";
import { useMemberSearch } from "@/hooks/use-member-search";
import { memberAvatar } from "@/lib/provider-logos";
import { cn } from "@/lib/utils";

/** How many member avatars the strip shows before the rest are left to the
 *  view-settings assignee filter. */
const MAX_AVATARS = 8;

interface AssigneeQuickFilterProps {
	projectId: string;
	/** Member ids currently filtered on; empty means no quick filter. */
	selectedIds: string[];
	onToggle: (memberId: string) => void;
	onClear: () => void;
}

/**
 * A row of member avatars that filters the board by assignee on click. Several
 * can be active at once (tasks assigned to any of them). It is a transient
 * quick filter: it lives in page state and never touches the saved view config.
 */
export function AssigneeQuickFilter({
	projectId,
	selectedIds,
	onToggle,
	onClear,
}: AssigneeQuickFilterProps) {
	const { t } = useTranslation("projects");
	const { members } = useMemberSearch(projectId, true);

	const shown = members.slice(0, MAX_AVATARS);
	if (shown.length === 0) return null;

	return (
		<fieldset className="m-0 flex min-w-0 items-center gap-1.5 border-0 p-0">
			<legend className="sr-only">
				{t("layout.shell.assigneeQuickFilter.label")}
			</legend>
			<div className="flex items-center -space-x-1.5">
				{shown.map((m) => {
					const selected = selectedIds.includes(m.id);
					const name = m.full_name || m.username;
					return (
						<button
							key={m.id}
							type="button"
							aria-pressed={selected}
							title={name}
							aria-label={t("layout.shell.assigneeQuickFilter.filterBy", {
								name,
							})}
							onClick={() => onToggle(m.id)}
							className={cn(
								"relative rounded-full ring-2 ring-background transition-all duration-150 hover:z-10 hover:-translate-y-0.5 focus-visible:z-10 focus-visible:outline-none focus-visible:ring-primary",
								selected
									? "z-10 ring-primary"
									: selectedIds.length > 0 && "opacity-50 hover:opacity-100",
							)}
						>
							<EntityAvatar {...memberAvatar(m)} size="sm" />
						</button>
					);
				})}
			</div>
			{selectedIds.length > 0 && (
				<button
					type="button"
					onClick={onClear}
					className="rounded-md px-1.5 py-0.5 text-xs font-medium text-muted-foreground hover:bg-muted/60 hover:text-foreground transition-all duration-150"
				>
					{t("layout.shell.assigneeQuickFilter.clear")}
				</button>
			)}
		</fieldset>
	);
}
