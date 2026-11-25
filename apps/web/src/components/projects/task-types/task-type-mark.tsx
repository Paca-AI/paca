import { CircleDashed } from "lucide-react";

import { cn } from "@/lib/utils";
import { getTaskTypeIconComponent } from "./task-type-icons";

/**
 * A task's type drawn the way task cards draw it: the type's icon in its
 * color, or, for a type without an icon, a small pill with its name tinted in
 * that color. A task with no type gets a faint placeholder icon so rows stay
 * aligned.
 */
export function TaskTypeMark({
	name,
	icon,
	color,
	className,
}: {
	name?: string | null;
	icon?: string | null;
	color?: string | null;
	className?: string;
}) {
	if (!name) {
		return (
			<CircleDashed
				aria-hidden
				className={cn("size-4 shrink-0 text-muted-foreground/50", className)}
			/>
		);
	}
	const Icon = getTaskTypeIconComponent(icon);
	if (Icon) {
		return (
			<Icon
				role="img"
				aria-label={name}
				className={cn(
					"size-4 shrink-0",
					!color && "text-muted-foreground",
					className,
				)}
				style={color ? { color } : undefined}
			>
				<title>{name}</title>
			</Icon>
		);
	}
	return (
		<span
			className={cn(
				"inline-flex shrink-0 items-center rounded-md border px-1.5 py-0.5 text-[10px] font-bold leading-tight",
				className,
			)}
			style={{
				borderColor: color ? `${color}44` : "var(--border)",
				backgroundColor: color ? `${color}15` : "var(--muted)",
				color: color ?? "inherit",
			}}
		>
			{name}
		</span>
	);
}
