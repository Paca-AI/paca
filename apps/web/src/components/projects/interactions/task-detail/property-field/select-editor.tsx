import { useTranslation } from "react-i18next";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import { toOptionItem } from "./option-list-button";
import { SearchableList } from "./searchable-list";
import type { SelectOption } from "./types";

export function SelectEditor({
	value,
	options,
	onChange,
	align = "start",
}: {
	value: string | string[] | null;
	options: SelectOption[];
	onChange: (value: string | null) => void;
	align?: "start" | "end" | "center";
}) {
	const { t } = useTranslation("projects");
	const selectedArr = Array.isArray(value) ? value : value ? [value] : [];
	const firstSelected = options.find((o) => o.value === selectedArr[0]);

	return (
		<Popover>
			<PopoverTrigger
				type="button"
				className={
					firstSelected
						? "inline-flex items-center gap-2 rounded-full border border-border/30 bg-muted/30 px-3 py-1 text-sm font-semibold text-muted-foreground hover:bg-muted/50 hover:border-border/50 transition-all duration-150"
						: "inline-flex items-center gap-1.5 text-sm text-muted-foreground/50 italic hover:text-muted-foreground/80 transition-colors"
				}
			>
				{firstSelected ? (
					<>
						{firstSelected.colorDot && (
							<span
								className="size-1.75 rounded-full shrink-0"
								style={{
									background: firstSelected.colorDot,
									boxShadow: `0 0 6px ${firstSelected.colorDot}30`,
								}}
							/>
						)}
						{firstSelected.icon}
						{firstSelected.label}
					</>
				) : (
					t("taskDetail.properties.none")
				)}
			</PopoverTrigger>
			<PopoverContent
				className="w-52 p-1 rounded-xl border border-border/40 shadow-lg"
				align={align}
			>
				<SearchableList
					items={options.map((opt) => {
						const isSelected = selectedArr.includes(opt.value);
						return toOptionItem(opt, isSelected, () =>
							onChange(isSelected ? null : opt.value),
						);
					})}
				/>
			</PopoverContent>
		</Popover>
	);
}
