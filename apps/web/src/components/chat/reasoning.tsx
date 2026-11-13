"use client";

import { BrainIcon } from "lucide-react";
import { type FC, useState } from "react";
import { useTranslation } from "react-i18next";
import { MarkdownText } from "@/components/chat/markdown-text";
import {
	TraceCollapsible,
	TraceCollapsibleContent,
	TraceCollapsibleTrigger,
	TraceIcon,
	TraceLine,
	TraceTitle,
} from "@/components/ui/trace";

/**
 * A run of consecutive reasoning parts as one collapsible trace. It opens by
 * itself while the model is still thinking and folds away when it finishes,
 * until the reader toggles it — after which their choice wins.
 */
export const ReasoningGroup: FC<{
	/** Text of each reasoning part, in order. */
	texts: readonly string[];
	streaming: boolean;
}> = ({ texts, streaming }) => {
	const { t } = useTranslation("projects");
	const [userOpen, setUserOpen] = useState<boolean | null>(null);
	const open = userOpen ?? streaming;

	return (
		<TraceCollapsible open={open} onOpenChange={setUserOpen}>
			<TraceCollapsibleTrigger>
				<TraceLine status={streaming ? "running" : undefined}>
					<TraceIcon>
						<BrainIcon />
					</TraceIcon>
					<TraceTitle>{t("agents.thread.reasoning")}</TraceTitle>
				</TraceLine>
			</TraceCollapsibleTrigger>
			<TraceCollapsibleContent aria-busy={streaming}>
				{texts.map((text, i) => (
					// biome-ignore lint/suspicious/noArrayIndexKey: parts only ever append
					<MarkdownText key={i} text={text} />
				))}
			</TraceCollapsibleContent>
		</TraceCollapsible>
	);
};
