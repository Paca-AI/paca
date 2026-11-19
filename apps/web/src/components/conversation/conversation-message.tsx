import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

/**
 * One message of a conversation, whoever is talking: a person in a channel, a
 * commenter on a task, an agent in a direct message. It lays out the avatar,
 * the author line, the body and what hangs under it; what the body is and which
 * actions exist belong to the caller.
 *
 * `flat` is a chat row (avatar column, hover background, a toolbar floating on
 * hover); `bubble` is a card, for a comment among activity. `compact` drops the
 * avatar and the author line of a follow-up from the same author.
 */
export function ConversationMessage({
	avatar,
	name,
	badge,
	time,
	headerExtra,
	toolbar,
	footer,
	compact,
	appearance = "flat",
	className,
	children,
	...rest
}: {
	avatar: ReactNode;
	name: string;
	/** Beside the name, such as an "agent" tag. */
	badge?: ReactNode;
	/** The message time, usually a <time> element. */
	time?: ReactNode;
	/** Right of the author line, such as a menu or an "editing" mark. */
	headerExtra?: ReactNode;
	/** Actions that float over the row on hover or focus (flat only). */
	toolbar?: ReactNode;
	/** Under the body: reactions, a thread link, a run link. */
	footer?: ReactNode;
	compact?: boolean;
	appearance?: "flat" | "bubble";
	className?: string;
	children: ReactNode;
} & Omit<React.HTMLAttributes<HTMLDivElement>, "children" | "className">) {
	if (appearance === "bubble") {
		return (
			<div className={cn("flex gap-3", className)} {...rest}>
				{avatar}
				<div className="min-w-0 flex-1">
					<div className="group rounded-xl rounded-tl-lg border border-border/50 bg-card/70 px-3.5 py-2.5">
						<div className="mb-1 flex items-center gap-2">
							<span className="text-xs font-semibold text-foreground">
								{name}
							</span>
							{badge}
							<span className="text-xs text-muted-foreground/90">{time}</span>
							{headerExtra}
						</div>
						{children}
						{footer}
					</div>
				</div>
			</div>
		);
	}
	return (
		<div
			className={cn(
				"group relative flex gap-3 px-4 py-0.5 hover:bg-muted/40",
				className,
			)}
			{...rest}
		>
			<div className="w-8 shrink-0 pt-0.5">{compact ? null : avatar}</div>
			<div className="min-w-0 flex-1">
				{compact ? null : (
					<div className="flex items-baseline gap-2">
						<span className="text-sm font-semibold">{name}</span>
						{badge}
						<span className="text-xs text-muted-foreground">{time}</span>
						{headerExtra}
					</div>
				)}
				{children}
				{footer}
			</div>
			{toolbar ? (
				<div className="absolute -top-3 right-4 hidden items-center gap-0.5 rounded-md border bg-background p-0.5 shadow-sm group-focus-within:flex group-hover:flex">
					{toolbar}
				</div>
			) : null}
		</div>
	);
}
