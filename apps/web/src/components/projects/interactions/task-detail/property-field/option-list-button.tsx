import { Check } from "lucide-react";
import type { SelectOption } from "./types";

export function OptionListButton({
	option,
	isSelected,
	onClick,
}: {
	option: SelectOption;
	isSelected: boolean;
	onClick: () => void;
}) {
	return (
		<button
			type="button"
			className="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-sm hover:bg-muted/60 transition-colors duration-100"
			onClick={onClick}
		>
			{option.colorDot ? (
				<span
					className="size-2 rounded-full shrink-0"
					style={{ background: option.colorDot }}
				/>
			) : option.icon ? (
				<span className="shrink-0">{option.icon}</span>
			) : null}
			<span className="flex-1 text-left">
				{option.label}
				{option.hint && (
					<span className="ml-1.5 text-xs text-muted-foreground/60">
						{option.hint}
					</span>
				)}
			</span>
			{isSelected && <Check className="size-3.5 text-primary" />}
		</button>
	);
}
