import { Check, User } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
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
	const { t } = useTranslation();
	if (!canEdit) {
		if (!userValue) {
			return (
				<span className="inline-flex items-center gap-1.5 text-sm text-muted-foreground/50 italic">
					<User className="size-3.5 opacity-60" />
					{t("project.interactions.taskDetail.unassigned")}
				</span>
			);
		}
		return (
			<div className="flex items-center gap-2.5">
				<div className="flex size-6 items-center justify-center rounded-full bg-linear-to-br from-primary/20 to-primary/10 text-primary text-xs font-bold ring-1 ring-primary/20">
					{userValue.initials}
				</div>
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
						: "inline-flex items-center gap-1.5 text-sm text-muted-foreground/50 italic hover:text-muted-foreground/80 transition-colors"
				}
			>
				{userValue ? (
					<>
						<div className="flex size-6 items-center justify-center rounded-full bg-linear-to-br from-primary/20 to-primary/10 text-primary text-xs font-bold ring-1 ring-primary/20">
							{userValue.initials}
						</div>
						<span className="text-sm font-medium text-foreground">
							{userValue.label}
						</span>
					</>
				) : (
					<>
						<User className="size-3.5 opacity-60" />
						Unassigned
					</>
				)}
			</PopoverTrigger>
			<PopoverContent
				className="w-56 p-1 rounded-xl border border-border/40 shadow-lg"
				align="start"
			>
				{showUnassigned && (
					<button
						type="button"
						className="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-sm text-muted-foreground hover:bg-muted/60 transition-colors duration-100"
						onClick={() => onChange?.(null)}
					>
						<User className="size-3.5 opacity-60" />
						<span className="flex-1 text-left">
							{t("project.interactions.taskDetail.unassigned")}
						</span>
						{!userValue && <Check className="size-3.5 text-primary" />}
					</button>
				)}
				{users.map((u) => (
					<button
						key={u.value}
						type="button"
						className="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-sm hover:bg-muted/60 transition-colors duration-100"
						onClick={() => onChange?.(u.value)}
					>
						<div className="flex size-5 items-center justify-center rounded-full bg-linear-to-br from-primary/20 to-primary/10 text-primary text-xs font-bold">
							{u.initials}
						</div>
						<span className="flex-1 text-left truncate">{u.label}</span>
						{u.value === userValue?.value && (
							<Check className="size-3.5 text-primary" />
						)}
					</button>
				))}
			</PopoverContent>
		</Popover>
	);
}
