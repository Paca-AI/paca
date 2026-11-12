"use client";

import { WrenchIcon } from "lucide-react";
import { type FC, type ReactNode, useState } from "react";
import { useTranslation } from "react-i18next";
import {
	TraceCollapsible,
	TraceCollapsibleContent,
	TraceCollapsibleTrigger,
	TraceIcon,
	TraceLine,
	TraceTitle,
} from "@/components/ui/trace";

/** A run of consecutive tool calls folded under one collapsible line. */
export const ToolGroup: FC<{
	count: number;
	active: boolean;
	children: ReactNode;
}> = ({ count, active, children }) => {
	const { t } = useTranslation("projects");
	const [open, setOpen] = useState(false);
	return (
		<TraceCollapsible open={open} onOpenChange={setOpen}>
			<TraceCollapsibleTrigger>
				<TraceLine status={active ? "running" : undefined}>
					<TraceIcon>
						<WrenchIcon />
					</TraceIcon>
					<TraceTitle>{t("agents.thread.toolCallCount", { count })}</TraceTitle>
				</TraceLine>
			</TraceCollapsibleTrigger>
			<TraceCollapsibleContent className="flex flex-col gap-1">
				{children}
			</TraceCollapsibleContent>
		</TraceCollapsible>
	);
};
