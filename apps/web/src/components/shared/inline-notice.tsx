import { Info, TriangleAlert } from "lucide-react";
import type { ReactNode } from "react";

import { cn } from "@/lib/utils";

const TONES = {
	error: "border-destructive/30 bg-destructive/5 text-destructive",
	warning:
		"border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-400",
	info: "border-border/50 bg-muted/40 text-muted-foreground",
} as const;

interface InlineNoticeProps {
	/** `error` is announced to assistive tech right away; the others politely. */
	tone: keyof typeof TONES;
	children: ReactNode;
	className?: string;
}

/** A short message shown inside a dialog or panel, next to what it is about. */
export function InlineNotice({ tone, children, className }: InlineNoticeProps) {
	const Icon = tone === "info" ? Info : TriangleAlert;
	return (
		<div
			role={tone === "error" ? "alert" : "status"}
			className={cn(
				"flex items-start gap-2 rounded-lg border px-3 py-2 text-sm",
				TONES[tone],
				className,
			)}
		>
			<Icon className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
			<span className="min-w-0">{children}</span>
		</div>
	);
}
