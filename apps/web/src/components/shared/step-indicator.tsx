import { cn } from "@/lib/utils";

interface StepIndicatorProps {
	/** The step shown now, counting from 1. */
	step: number;
	total: number;
	/** The text beside the dots, e.g. "2 / 3". The caller translates it. */
	label: string;
	className?: string;
}

/** The small "● ● ○  2 / 3" pill in the header of a multi-step dialog. */
export function StepIndicator({
	step,
	total,
	label,
	className,
}: StepIndicatorProps) {
	const steps = Array.from({ length: total }, (_, index) => index + 1);
	return (
		<div
			className={cn(
				"flex shrink-0 items-center gap-1 whitespace-nowrap rounded-full border border-border/60 bg-muted/50 px-2.5 py-1",
				className,
			)}
		>
			<div className="flex items-center gap-1" aria-hidden="true">
				{steps.map((n) => (
					<span
						key={n}
						className={cn(
							"size-1.5 rounded-full transition-colors duration-200",
							step >= n ? "bg-primary" : "bg-muted-foreground/30",
						)}
					/>
				))}
			</div>
			<span className="ml-1 text-xs font-medium text-muted-foreground">
				{label}
			</span>
		</div>
	);
}
