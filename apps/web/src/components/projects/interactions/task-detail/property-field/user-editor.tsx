import { Check, User } from "lucide-react";
import { useTranslation } from "react-i18next";
import { EntityAvatar } from "@/components/shared/entity-avatar";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import { SearchableList } from "./searchable-list";
import type { UserOption } from "./types";

export function UserEditor({
	userValue,
	users = [],
	canEdit,
	showUnassigned = true,
	onChange,
}: {
	userValue?: UserOption | null;
	users?: UserOption[];
	canEdit: boolean;
	showUnassigned?: boolean;
	onChange?: (value: string | null) => void;
}) {
	const { t } = useTranslation("projects");
	if (!canEdit) {
		if (!userValue) {
			return (
				<span className="inline-flex items-center gap-1.5 text-sm text-muted-foreground/90 italic">
					<User className="size-3.5 opacity-60" />
					{t("taskDetail.common.unassigned")}
				</span>
			);
		}
		return (
			<div className="flex items-center gap-2.5">
				<EntityAvatar {...userValue.avatar} size="sm" />
				<span className="text-sm font-medium text-foreground">
					{userValue.label}
				</span>
			</div>
		);
	}

	return (
		<Popover>
			<PopoverTrigger
				type="button"
				className={
					userValue
						? "flex items-center gap-2.5 hover:opacity-80 transition-opacity duration-150"
						: "inline-flex items-center gap-1.5 text-sm text-muted-foreground/90 italic hover:text-muted-foreground transition-colors"
				}
			>
				{userValue ? (
					<>
						<EntityAvatar {...userValue.avatar} size="sm" />
						<span className="text-sm font-medium text-foreground">
							{userValue.label}
						</span>
					</>
				) : (
					<>
						<User className="size-3.5 opacity-60" />
						{t("taskDetail.common.unassigned")}
					</>
				)}
			</PopoverTrigger>
			<PopoverContent
				className="w-56 p-1 rounded-xl border border-border/60 shadow-lg"
				align="start"
			>
				<SearchableList
					pinned={
						showUnassigned && (
							<button
								type="button"
								className="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-sm text-muted-foreground hover:bg-muted/60 transition-colors duration-100"
								onClick={() => onChange?.(null)}
							>
								<User className="size-3.5 opacity-60" />
								<span className="flex-1 text-left">
									{t("taskDetail.common.unassigned")}
								</span>
								{!userValue && <Check className="size-3.5 text-primary" />}
							</button>
						)
					}
					items={users.map((u) => ({
						key: u.value,
						label: u.label,
						selected: u.value === userValue?.value,
						onSelect: () => onChange?.(u.value),
						content: (
							<>
								<EntityAvatar {...u.avatar} size="xs" />
								<span className="flex-1 text-left truncate">{u.label}</span>
							</>
						),
					}))}
				/>
			</PopoverContent>
		</Popover>
	);
}
