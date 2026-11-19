import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ChipField } from "./chip-field";

export function TagsEditor({
	tags = [],
	canEdit,
	onChange,
}: {
	tags: string[];
	canEdit: boolean;
	onChange?: (tags: string[]) => void;
}) {
	const { t } = useTranslation("projects");
	const [input, setInput] = useState("");

	function handleAdd(tag: string) {
		const trimmed = tag.trim();
		if (!trimmed || tags.includes(trimmed)) return;
		onChange?.([...tags, trimmed]);
		setInput("");
	}

	function handleRemove(tag: string) {
		onChange?.(tags.filter((t) => t !== tag));
	}

	return (
		<ChipField
			chips={tags.map((tag) => ({ key: tag, label: tag }))}
			onRemoveChip={handleRemove}
			canEdit={canEdit}
			addLabel={t("taskDetail.propertyField.tagsEditor.addTag")}
			popoverContentClassName="w-52 p-2 rounded-xl border border-border/60 shadow-lg"
		>
			<form
				onSubmit={(e) => {
					e.preventDefault();
					handleAdd(input);
				}}
			>
				<input
					// biome-ignore lint/a11y/noAutofocus: intentional for popover
					autoFocus
					type="text"
					value={input}
					onChange={(e) => setInput(e.target.value)}
					placeholder={t(
						"taskDetail.propertyField.tagsEditor.addTagPlaceholder",
					)}
					className="w-full rounded-lg border border-border/50 bg-muted/25 px-3 py-2 text-sm placeholder:text-muted-foreground/90 focus:outline-none focus:ring-2 focus:ring-primary/20 focus:border-primary/40 transition-all duration-150"
					onKeyDown={(e) => {
						if (e.key === "Enter") {
							e.preventDefault();
							handleAdd(input);
						}
					}}
				/>
			</form>
		</ChipField>
	);
}
