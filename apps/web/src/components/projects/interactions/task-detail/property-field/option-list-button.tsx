import type { SearchableListItem } from "./searchable-list";
import type { SelectOption } from "./types";

function OptionContent({ option }: { option: SelectOption }) {
	return (
		<>
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
		</>
	);
}

export function toOptionItem(
	option: SelectOption,
	isSelected: boolean,
	onSelect: () => void,
): SearchableListItem {
	return {
		key: option.value,
		label: option.label,
		group: option.group,
		selected: isSelected,
		onSelect,
		content: <OptionContent option={option} />,
	};
}
