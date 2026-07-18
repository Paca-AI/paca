import { useTranslation } from "react-i18next";

import {
	Dialog,
	DialogContent,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { KbdChord } from "@/components/ui/kbd";
import {
	SHORTCUT_DISPLAY,
	SHORTCUT_GROUPS,
	type ShortcutDisplayEntry,
} from "@/lib/shortcuts/keymap";

interface ShortcutHelpDialogProps {
	open: boolean;
	onOpenChange: (open: boolean) => void;
}

function ShortcutRow({ entry }: { entry: ShortcutDisplayEntry }) {
	const { t } = useTranslation("shortcuts");
	return (
		<div className="flex items-center justify-between gap-4 py-1">
			<span className="text-sm text-muted-foreground">
				{t(`actions.${entry.labelKey}` as never)}
			</span>
			<div className="flex items-center gap-1 shrink-0">
				{entry.chords.map((chord, chordIdx) => (
					<span key={chord.key} className="flex items-center gap-1">
						{chordIdx > 0 && (
							<span className="text-[10px] text-muted-foreground/40">/</span>
						)}
						<KbdChord chord={chord} />
					</span>
				))}
			</div>
		</div>
	);
}

export function ShortcutHelpDialog({
	open,
	onOpenChange,
}: ShortcutHelpDialogProps) {
	const { t } = useTranslation("shortcuts");

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="sm:max-w-xl">
				<DialogHeader>
					<DialogTitle>{t("dialog.title")}</DialogTitle>
				</DialogHeader>
				<div className="grid grid-cols-1 gap-x-8 sm:grid-cols-2">
					{SHORTCUT_GROUPS.map((group) => (
						<div key={group} className="mb-3">
							<p className="mb-1 text-xs font-semibold uppercase tracking-wide text-muted-foreground/60">
								{t(`groups.${group}`)}
							</p>
							{SHORTCUT_DISPLAY.filter((e) => e.group === group).map(
								(entry) => (
									<ShortcutRow key={entry.id} entry={entry} />
								),
							)}
						</div>
					))}
				</div>
				<p className="border-t border-border/40 pt-2.5 text-xs text-muted-foreground/70">
					{t("dialog.footer")}
				</p>
			</DialogContent>
		</Dialog>
	);
}
