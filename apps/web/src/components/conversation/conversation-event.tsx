import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

/**
 * Something that happened, not something said: a status change, a field edit,
 * an agent starting work. One muted line with the actor, what they did and when,
 * and optional detail beneath it.
 */
export function ConversationEvent({
	avatar,
	actor,
	text,
	time,
	actions,
	detail,
	className,
}: {
	avatar?: ReactNode;
	actor: string;
	text: ReactNode;
	time?: ReactNode;
	/** Right of the line, such as a menu. */
	actions?: ReactNode;
	detail?: ReactNode;
	className?: string;
}) {
	return (
		<div className={cn("flex gap-3", className)}>
			{avatar}
			<div className="flex min-w-0 flex-1 flex-col">
				<div className="group flex items-start gap-1 py-0.5">
					<div className="flex min-w-0 flex-1 flex-wrap items-center gap-1.5">
						<span className="text-xs font-medium text-foreground/80">
							{actor}
						</span>
						<span className="text-xs text-muted-foreground/70">{text}</span>
						{time ? (
							<span className="text-xs text-muted-foreground/45">{time}</span>
						) : null}
					</div>
					{actions}
				</div>
				{detail}
			</div>
		</div>
	);
}
