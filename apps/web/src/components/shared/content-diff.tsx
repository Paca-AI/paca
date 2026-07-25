import { GitBranch } from "lucide-react";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import {
	Dialog,
	DialogContent,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { type DiffLine, diffBlockNoteContent } from "@/lib/diff-utils";
import { cn } from "@/lib/utils";

interface ContentDiffProps {
	oldContent: unknown;
	newContent: unknown;
}

export function DiffLineRow({ line }: { line: DiffLine }) {
	const { t } = useTranslation("shared");
	return (
		<div
			className={cn(
				"flex gap-2 px-3 py-0.5 font-mono text-xs leading-5",
				line.type === "added" &&
					"bg-emerald-500/10 text-emerald-700 dark:text-emerald-400",
				line.type === "removed" &&
					"bg-red-500/10 text-red-700 dark:text-red-400",
				line.type === "unchanged" && "text-muted-foreground/50",
			)}
		>
			<span className="select-none w-3 shrink-0 text-center">
				{line.type === "added" ? "+" : line.type === "removed" ? "-" : " "}
			</span>
			<span className="break-all whitespace-pre-wrap min-w-0">
				{line.text || (
					<span className="italic opacity-40">
						{line.type === "unchanged" ? t("contentDiff.emptyLine") : ""}
					</span>
				)}
			</span>
		</div>
	);
}

export function ContentDiff({ oldContent, newContent }: ContentDiffProps) {
	const { t } = useTranslation("shared");
	const lines = useMemo(
		() => diffBlockNoteContent(oldContent, newContent),
		[oldContent, newContent],
	);

	const hasChanges = lines.some((l) => l.type !== "unchanged");

	if (!hasChanges) {
		return (
			<div className="flex flex-col items-center py-8 text-muted-foreground/40">
				<GitBranch className="size-5 mb-2" />
				<p className="text-xs font-medium">{t("contentDiff.noDifferences")}</p>
			</div>
		);
	}

	return (
		<div className="rounded-lg border border-border/30 overflow-hidden bg-muted/5">
			{lines.map((line, idx) => (
				// biome-ignore lint/suspicious/noArrayIndexKey: stable diff output
				<DiffLineRow key={idx} line={line} />
			))}
		</div>
	);
}

interface ContentDiffDialogProps {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	oldContent: unknown;
	newContent: unknown;
	title?: string;
}

export function ContentDiffDialog({
	open,
	onOpenChange,
	oldContent,
	newContent,
	title,
}: ContentDiffDialogProps) {
	const { t } = useTranslation("shared");
	const resolvedTitle = title ?? t("contentDiff.changeDiff");
	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="max-w-2xl max-h-[80vh] flex flex-col gap-0 p-0 overflow-hidden">
				<DialogHeader className="px-5 py-4 border-b border-border/25 shrink-0">
					<DialogTitle className="flex items-center gap-2 text-sm">
						<GitBranch className="size-4 text-muted-foreground/60" />
						{resolvedTitle}
					</DialogTitle>
				</DialogHeader>
				<div className="flex items-center gap-4 px-5 py-2 border-b border-border/25 bg-muted/10 shrink-0">
					<span className="flex items-center gap-1.5 text-xs text-red-600/80 dark:text-red-400/80">
						<span className="font-mono font-bold">-</span>{" "}
						{t("contentDiff.removed")}
					</span>
					<span className="flex items-center gap-1.5 text-xs text-emerald-600/80 dark:text-emerald-400/80">
						<span className="font-mono font-bold">+</span>{" "}
						{t("contentDiff.added")}
					</span>
				</div>
				<div className="flex-1 min-h-0 overflow-y-auto p-4">
					<ContentDiff oldContent={oldContent} newContent={newContent} />
				</div>
			</DialogContent>
		</Dialog>
	);
}
