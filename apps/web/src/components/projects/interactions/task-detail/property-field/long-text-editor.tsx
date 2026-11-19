import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { FieldValue } from "../primitives";

export const DEFAULT_LONG_TEXT_ROWS = 3;

export function LongTextEditor({
	value,
	canEdit,
	rows = DEFAULT_LONG_TEXT_ROWS,
	onChange,
}: {
	value: string | null;
	canEdit: boolean;
	rows?: number;
	onChange?: (value: string) => void;
}) {
	const { t } = useTranslation("projects");
	const [editing, setEditing] = useState(false);
	// draft is re-seeded from value on every click, so remote changes made while
	// the editor is closed are never lost.
	const [draft, setDraft] = useState(value ?? "");
	const cancelledRef = useRef(false);

	if (!canEdit) {
		return (
			<FieldValue empty={!value}>
				<span className="whitespace-pre-wrap wrap-anywhere">{value}</span>
			</FieldValue>
		);
	}

	if (editing) {
		return (
			<textarea
				// biome-ignore lint/a11y/noAutofocus: intentional for inline edit
				autoFocus
				rows={rows}
				value={draft}
				onChange={(e) => setDraft(e.target.value)}
				onBlur={() => {
					setEditing(false);
					if (cancelledRef.current) {
						cancelledRef.current = false;
						return;
					}
					if (draft !== (value ?? "")) onChange?.(draft);
				}}
				onKeyDown={(e) => {
					// Enter inserts a newline; Ctrl/Cmd+Enter or blur saves; Escape cancels.
					if (e.key === "Escape") {
						cancelledRef.current = true;
						setEditing(false);
						setDraft(value ?? "");
					} else if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
						e.currentTarget.blur();
					}
				}}
				className="w-full min-w-30 resize-y rounded-lg border border-border/50 bg-muted/25 px-2.5 py-1 text-sm focus:outline-none focus:ring-2 focus:ring-primary/20 focus:border-primary/40 transition-all duration-150"
			/>
		);
	}

	return (
		<button
			type="button"
			onClick={() => {
				cancelledRef.current = false;
				setDraft(value ?? "");
				setEditing(true);
			}}
			className="rounded-lg px-2.5 py-1 -mx-2.5 text-sm font-medium text-foreground hover:bg-muted/30 transition-colors duration-150 min-w-15 text-left whitespace-pre-wrap wrap-anywhere"
		>
			{value ? (
				value
			) : (
				<span className="text-muted-foreground/90 italic">
					{t("taskDetail.common.empty")}
				</span>
			)}
		</button>
	);
}
