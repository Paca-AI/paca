import { Plus, X } from "lucide-react";
import type { ReactNode } from "react";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";

export function ChipField({
	chips,
	onRemoveChip,
	canEdit,
	addLabel,
	popoverContentClassName = "w-52 p-1 rounded-xl border border-border/60 shadow-lg",
	popoverAlign = "start",
	children,
}: {
	chips: { key: string; label: ReactNode }[];
	onRemoveChip: (key: string) => void;
	canEdit: boolean;
	addLabel: string;
	popoverContentClassName?: string;
	popoverAlign?: "start" | "end" | "center";
	children: ReactNode;
}) {
	return (
		<div className="flex flex-wrap items-center gap-1.5 min-h-7">
			{chips.map((chip) => (
				<span
					key={chip.key}
					className="inline-flex items-center gap-1 rounded-md bg-muted/50 px-2 py-0.5 text-xs font-medium text-foreground/80 border border-border/50 hover:border-border/60 transition-colors duration-150"
				>
					{chip.label}
					{canEdit && (
						<button
							type="button"
							onClick={() => onRemoveChip(chip.key)}
							className="text-muted-foreground/90 hover:text-destructive transition-colors duration-150"
						>
							<X className="size-2.5" />
						</button>
					)}
				</span>
			))}
			{canEdit && (
				<Popover>
					<PopoverTrigger
						type="button"
						className="inline-flex items-center gap-1 rounded-md border border-dashed border-border/50 px-2 py-0.5 text-xs text-muted-foreground/90 hover:border-border/60 hover:text-muted-foreground transition-all duration-150"
					>
						<Plus className="size-2.5" />
						{addLabel}
					</PopoverTrigger>
					<PopoverContent
						className={popoverContentClassName}
						align={popoverAlign}
					>
						{children}
					</PopoverContent>
				</Popover>
			)}
		</div>
	);
}
