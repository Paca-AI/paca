import { Plus, X } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";

export function TagsEditor({
	tags = [],
	canEdit,
	onChange,
}: {
	tags: string[];
	canEdit: boolean;
	onChange?: (tags: string[]) => void;
}) {
	const { t } = useTranslation();
	const [input, setInput] = useState("");

	function handleAdd(tag: string) {
		const trimmed = tag.trim();
		if (!trimmed || tags.includes(trimmed)) return;
		onChange?.([...tags, trimmed]);
		setInput("");
	}

	function handleRemove(tag: string) {
		onChange?.(tags.filter((tg) => tg !== tag));
	}

	return (
		<div className="flex flex-wrap items-center gap-1.5 min-h-7">
			{tags.map((tag) => (
				<span
					key={tag}
					className="inline-flex items-center gap-1 rounded-md bg-muted/50 px-2 py-0.5 text-xs font-medium text-foreground/80 border border-border/20 hover:border-border/40 transition-colors duration-150"
				>
					{tag}
					{canEdit && (
						<button
							type="button"
							onClick={() => handleRemove(tag)}
							className="text-muted-foreground/60 hover:text-destructive transition-colors duration-150"
						>
							<X className="size-2.5" />
						</button>
					)}
				</span>
			))}
			{canEdit && (
				<Popover>
					<PopoverTrigger
						type="button"
						className="inline-flex items-center gap-1 rounded-md border border-dashed border-border/30 px-2 py-0.5 text-xs text-muted-foreground/60 hover:border-border/60 hover:text-muted-foreground transition-all duration-150"
					>
						<Plus className="size-2.5" />
						Add tag
					</PopoverTrigger>
					<PopoverContent
						className="w-52 p-2 rounded-xl border border-border/40 shadow-lg"
						align="start"
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
								placeholder={t("project.interactions.taskDetail.addTag")}
								className="w-full rounded-lg border border-border/30 bg-muted/25 px-3 py-2 text-sm placeholder:text-muted-foreground/60 focus:outline-none focus:ring-2 focus:ring-primary/20 focus:border-primary/40 transition-all duration-150"
								onKeyDown={(e) => {
									if (e.key === "Enter") {
										e.preventDefault();
										handleAdd(input);
									}
								}}
							/>
						</form>
					</PopoverContent>
				</Popover>
			)}
		</div>
	);
}
