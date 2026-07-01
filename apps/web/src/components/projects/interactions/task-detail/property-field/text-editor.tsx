import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { FieldValue } from "../primitives";

export function TextEditor({
	value,
	canEdit,
	onChange,
}: {
	value: string | null;
	canEdit: boolean;
	onChange?: (value: string) => void;
}) {
	const { t } = useTranslation();
	const [editing, setEditing] = useState(false);
	const [draft, setDraft] = useState(value ?? "");
	const ref = useRef<HTMLInputElement>(null);

	if (!canEdit) {
		return <FieldValue empty={!value}>{value}</FieldValue>;
	}

	if (editing) {
		return (
			<input
				ref={ref}
				// biome-ignore lint/a11y/noAutofocus: intentional for inline edit
				autoFocus
				type="text"
				value={draft}
				onChange={(e) => setDraft(e.target.value)}
				onBlur={() => {
					setEditing(false);
					if (draft !== (value ?? "")) onChange?.(draft);
				}}
				onKeyDown={(e) => {
					if (e.key === "Enter") {
						e.preventDefault();
						e.currentTarget.blur();
					}
					if (e.key === "Escape") {
						setEditing(false);
						setDraft(value ?? "");
					}
				}}
				className="w-full min-w-30 rounded-lg border border-border/30 bg-muted/25 px-2.5 py-1 text-sm focus:outline-none focus:ring-2 focus:ring-primary/20 focus:border-primary/40 transition-all duration-150"
			/>
		);
	}

	return (
		<button
			type="button"
			onClick={() => {
				setDraft(value ?? "");
				setEditing(true);
			}}
			className="rounded-lg px-2.5 py-1 -mx-2.5 text-sm font-medium text-foreground hover:bg-muted/30 transition-colors duration-150 min-w-15 text-left"
		>
			{value ? (
				value
			) : (
				<span className="text-muted-foreground/50 italic">
				{t("project.interactions.taskDetail.empty")}
			</span>
			)}
		</button>
	);
}
