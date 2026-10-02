import { Check, Sparkles, User } from "lucide-react";
import { useTranslation } from "react-i18next";
import { EntityAvatarContent } from "@/components/shared/entity-avatar";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import type { ProjectMember } from "@/lib/project-api";
import { resolveMemberAvatarUrl } from "@/lib/provider-logos";
import { cn } from "@/lib/utils";
import { SearchableList } from "./task-detail/property-field/searchable-list";

export interface AssigneePatch {
	assignment_mode?: "manual" | "auto";
	assignee_ids?: string[];
}

const AVATAR_SIZE = {
	sm: { box: "size-5", icon: "size-2.5", sparkle: "size-2.5" },
	md: { box: "size-6", icon: "size-3", sparkle: "size-3" },
} as const;

const memberName = (m: ProjectMember) => m.full_name || m.username;

/** Assignee picker shared by the board card, list row, subtask row and task
 * detail panel: an avatar-stack trigger opening a searchable popover with the
 * optional "Auto" row, an "Unassigned" row and one row per member. Picking
 * while in auto mode switches the task back to manual assignment. */
export function AssigneePicker({
	members,
	assigneeIds,
	isAutoAssign = false,
	autoEnabled = false,
	canEdit,
	size = "sm",
	showNames = false,
	align = "start",
	onChange,
}: {
	members: ProjectMember[];
	assigneeIds: string[];
	isAutoAssign?: boolean;
	/** Show the "Auto" row (Jev enabled for the project). */
	autoEnabled?: boolean;
	canEdit: boolean;
	size?: keyof typeof AVATAR_SIZE;
	/** Append the assignees' names to the avatar stack (detail panel). */
	showNames?: boolean;
	align?: "start" | "end" | "center";
	onChange: (patch: AssigneePatch) => void;
}) {
	const { t } = useTranslation("projects");
	const s = AVATAR_SIZE[size];
	const visible = assigneeIds.slice(0, 3);
	const overflow = assigneeIds.length - visible.length;
	const manual: AssigneePatch = isAutoAssign
		? { assignment_mode: "manual" }
		: {};
	const current = isAutoAssign ? [] : assigneeIds;

	const avatarStack = isAutoAssign ? (
		<div
			className={cn(
				s.box,
				"flex items-center justify-center rounded-full bg-linear-to-br from-primary/20 to-primary/10 text-primary ring-1 ring-border/25",
			)}
		>
			<Sparkles className={s.sparkle} />
		</div>
	) : (
		<div className="flex items-center -space-x-1.5">
			{visible.length > 0 ? (
				visible.map((id) => {
					const m = members.find((mm) => mm.id === id);
					return (
						<div
							key={id}
							className={cn(
								s.box,
								"flex items-center justify-center rounded-full bg-linear-to-br from-primary/20 to-primary/10 text-primary text-xs font-bold ring-2 ring-card",
							)}
						>
							<EntityAvatarContent
								avatarUrl={m ? resolveMemberAvatarUrl(m) : undefined}
							>
								{m ? (
									memberName(m).slice(0, 1).toUpperCase()
								) : (
									<User className={s.icon} />
								)}
							</EntityAvatarContent>
						</div>
					);
				})
			) : (
				<div
					className={cn(
						s.box,
						"flex items-center justify-center rounded-full bg-linear-to-br from-muted/80 to-muted/40 text-muted-foreground text-xs font-bold ring-1 ring-border/25",
					)}
				>
					<User className={s.icon} />
				</div>
			)}
			{overflow > 0 && (
				<div
					className={cn(
						s.box,
						"flex items-center justify-center rounded-full bg-muted text-muted-foreground text-[10px] font-bold ring-2 ring-card",
					)}
				>
					+{overflow}
				</div>
			)}
		</div>
	);

	const names = isAutoAssign
		? t("taskDetail.properties.autoAssign")
		: assigneeIds
				.map((id) => members.find((m) => m.id === id))
				.filter((m): m is ProjectMember => !!m)
				.map(memberName)
				.join(", ");
	const trigger = showNames ? (
		<span className="flex min-w-0 items-center gap-2">
			{avatarStack}
			<span
				className={cn(
					"truncate text-sm",
					names
						? "font-medium text-foreground"
						: "italic text-muted-foreground/50",
				)}
			>
				{names || t("taskDetail.common.unassigned")}
			</span>
		</span>
	) : (
		avatarStack
	);

	if (!canEdit || members.length === 0) return trigger;

	const rowClass =
		"flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-sm hover:bg-muted/60 transition-colors duration-100";

	return (
		<Popover>
			<PopoverTrigger
				type="button"
				onClick={(e) => e.stopPropagation()}
				className={cn(
					"nodrag flex min-w-0 items-center rounded-full transition-all duration-150",
					showNames ? "hover:opacity-80" : "hover:ring-2 hover:ring-primary/30",
				)}
			>
				{trigger}
			</PopoverTrigger>
			<PopoverContent
				className="w-52 p-1 rounded-xl border border-border/40 shadow-lg"
				align={align}
			>
				{/* biome-ignore lint/a11y/noStaticElementInteractions: portaled content still bubbles React events to the clickable card/row; this only stops that */}
				<div
					onClick={(e) => e.stopPropagation()}
					onKeyDown={(e) => e.stopPropagation()}
				>
					<SearchableList
						pinned={
							<>
								{autoEnabled && (
									<button
										type="button"
										className={rowClass}
										onClick={() =>
											onChange(
												isAutoAssign
													? { assignment_mode: "manual" }
													: { assignment_mode: "auto", assignee_ids: [] },
											)
										}
									>
										<div className="flex size-5 items-center justify-center rounded-full bg-linear-to-br from-primary/20 to-primary/10 text-primary shrink-0">
											<Sparkles className="size-3" />
										</div>
										<span className="flex-1 text-left truncate">
											{t("taskDetail.properties.autoAssign")}
										</span>
										{isAutoAssign && (
											<Check className="size-3.5 text-primary" />
										)}
									</button>
								)}
								<button
									type="button"
									className={cn(rowClass, "text-muted-foreground")}
									onClick={() => onChange({ ...manual, assignee_ids: [] })}
								>
									<User className="size-3.5 opacity-60" />
									<span className="flex-1 text-left">
										{t("taskDetail.common.unassigned")}
									</span>
									{!isAutoAssign && assigneeIds.length === 0 && (
										<Check className="size-3.5 text-primary" />
									)}
								</button>
							</>
						}
						items={members.map((m) => {
							const isSelected = current.includes(m.id);
							return {
								key: m.id,
								label: memberName(m),
								selected: isSelected,
								onSelect: () =>
									onChange({
										...manual,
										assignee_ids: isSelected
											? current.filter((id) => id !== m.id)
											: [...current, m.id],
									}),
								content: (
									<>
										<div className="flex size-5 items-center justify-center rounded-full bg-linear-to-br from-primary/20 to-primary/10 text-primary text-xs font-bold shrink-0">
											<EntityAvatarContent
												avatarUrl={resolveMemberAvatarUrl(m)}
											>
												{memberName(m).slice(0, 1).toUpperCase()}
											</EntityAvatarContent>
										</div>
										<span className="flex-1 text-left truncate">
											{memberName(m)}
										</span>
									</>
								),
							};
						})}
					/>
				</div>
			</PopoverContent>
		</Popover>
	);
}
