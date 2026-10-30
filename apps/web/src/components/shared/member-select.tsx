import { ChevronDown } from "lucide-react";
import { useState } from "react";
import { SearchableList } from "@/components/projects/interactions/task-detail/property-field/searchable-list";
import { EntityAvatar } from "@/components/shared/entity-avatar";
import { MiddleTruncate } from "@/components/shared/middle-truncate";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import { useMemberSearch } from "@/hooks/use-member-search";
import { useMemberDirectory } from "@/lib/member-directory";
import type { ProjectMemberType } from "@/lib/project-api";
import { memberAvatar } from "@/lib/provider-logos";
import { cn } from "@/lib/utils";

const memberName = (m: { full_name: string; username: string }) =>
	m.full_name || m.username;

/** Single-select member dropdown for forms. The current value is resolved by
 *  id, and the options are searched and paged on the server once the popover
 *  opens, so it works for any team size. `value` is a membership id. */
export function MemberSelect({
	projectId,
	value,
	onChange,
	disabled,
	memberType,
	placeholder,
	size = "default",
	className,
}: {
	projectId: string;
	value: string | null | undefined;
	onChange: (memberId: string) => void;
	disabled?: boolean;
	/** Offer only this kind of member (e.g. "agent"). */
	memberType?: ProjectMemberType;
	placeholder?: string;
	size?: "sm" | "default";
	className?: string;
}) {
	const [open, setOpen] = useState(false);
	const getMember = useMemberDirectory(projectId);
	const search = useMemberSearch(projectId, open && !disabled, { memberType });
	const selected = getMember(value);

	return (
		<Popover open={open} onOpenChange={setOpen}>
			<PopoverTrigger
				type="button"
				disabled={disabled}
				className={cn(
					"flex w-full items-center justify-between gap-1.5 rounded-lg border border-input bg-transparent py-2 pr-2 pl-2.5 text-left whitespace-nowrap transition-colors outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-input/30 dark:hover:bg-input/50",
					size === "sm" ? "h-7 text-xs" : "h-8 text-sm",
					className,
				)}
			>
				{selected ? (
					<MiddleTruncate className="flex-1" text={memberName(selected)} />
				) : (
					<span className="min-w-0 flex-1 truncate text-muted-foreground">
						{placeholder}
					</span>
				)}
				<ChevronDown className="pointer-events-none size-4 shrink-0 text-muted-foreground" />
			</PopoverTrigger>
			<PopoverContent
				className="w-64 p-1 rounded-xl border border-border/40 shadow-lg"
				align="start"
			>
				<SearchableList
					remote={search}
					items={search.members.map((m) => ({
						key: m.id,
						label: memberName(m),
						selected: m.id === value,
						onSelect: () => {
							onChange(m.id);
							setOpen(false);
						},
						content: (
							<>
								<EntityAvatar {...memberAvatar(m)} size="xs" />
								<MiddleTruncate
									className="flex-1 text-left"
									text={memberName(m)}
								/>
							</>
						),
					}))}
				/>
			</PopoverContent>
		</Popover>
	);
}
