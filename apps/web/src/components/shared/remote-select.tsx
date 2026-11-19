import { ChevronDown } from "lucide-react";
import type { ReactNode } from "react";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import { cn } from "@/lib/utils";

/** Trigger + popover shell for a single-select whose options are searched and
 *  paged on the server (a `SearchableList` in `remote` mode goes in as
 *  children). The owner holds `open` so it can enable its search hook only
 *  once the popover is opened — nothing loads before that. `triggerClassName`
 *  replaces the default form-field look (e.g. for a pill in the composer). */
export function RemoteSelect({
	open,
	onOpenChange,
	label,
	placeholder,
	disabled,
	triggerClassName,
	className,
	contentClassName,
	children,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	/** Text for the current value; empty shows the placeholder. */
	label: string | undefined;
	placeholder?: string;
	disabled?: boolean;
	triggerClassName?: string;
	/** Extra classes on the trigger (e.g. a width). */
	className?: string;
	contentClassName?: string;
	children: ReactNode;
}) {
	return (
		<Popover open={open} onOpenChange={onOpenChange}>
			<PopoverTrigger
				type="button"
				disabled={disabled}
				className={cn(
					triggerClassName ??
						"flex h-8 w-full items-center justify-between gap-1.5 rounded-lg border border-input bg-transparent py-2 pr-2 pl-2.5 text-left text-sm whitespace-nowrap transition-colors outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-input/30 dark:hover:bg-input/50",
					className,
				)}
			>
				<span
					className={cn(
						"min-w-0 flex-1 truncate",
						!label && "text-muted-foreground",
					)}
				>
					{label || placeholder}
				</span>
				<ChevronDown className="pointer-events-none size-4 shrink-0 text-muted-foreground" />
			</PopoverTrigger>
			<PopoverContent
				className={cn(
					"w-64 p-1 rounded-xl border border-border/60 shadow-lg",
					contentClassName,
				)}
				align="start"
			>
				{children}
			</PopoverContent>
		</Popover>
	);
}
