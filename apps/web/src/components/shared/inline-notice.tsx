import { Info, TriangleAlert } from "lucide-react";
import type { ReactNode } from "react";

import { Alert, AlertDescription } from "@/components/ui/alert";
import { cn } from "@/lib/utils";

const TONES = {
	error: "border-destructive/30 bg-destructive/5",
	warning: "border-warning/30 bg-warning/10 text-warning",
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
		<Alert
			variant={tone === "error" ? "destructive" : "default"}
			role={tone === "error" ? "alert" : "status"}
			className={cn(TONES[tone], className)}
		>
			<Icon aria-hidden="true" />
			<AlertDescription className="text-current">{children}</AlertDescription>
		</Alert>
	);
}
