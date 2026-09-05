import { useTranslation } from "react-i18next";
import { FieldValue } from "../primitives";
import { ChipField } from "./chip-field";
import { OptionListButton } from "./option-list-button";
import type { SelectOption } from "./types";

export function MultiSelectEditor({
	value = [],
	options,
	canEdit,
	onChange,
}: {
	value?: string[];
	options: SelectOption[];
	canEdit: boolean;
	onChange?: (values: string[]) => void;
}) {
	const { t } = useTranslation("projects");
	const selected = options.filter((o) => value.includes(o.value));

	function toggle(optValue: string) {
		onChange?.(
			value.includes(optValue)
				? value.filter((v) => v !== optValue)
				: [...value, optValue],
		);
	}

	if (!canEdit) {
		if (selected.length === 0) return <FieldValue empty />;
		return (
			<div className="flex flex-wrap items-center gap-1.5">
				{selected.map((opt) => (
					<span
						key={opt.value}
						className="inline-flex items-center gap-1.5 rounded-full border border-border/30 bg-muted/30 px-2.5 py-0.5 text-xs font-semibold text-muted-foreground"
					>
						{opt.colorDot && (
							<span
								className="size-1.75 rounded-full shrink-0"
								style={{ background: opt.colorDot }}
							/>
						)}
						{opt.label}
					</span>
				))}
			</div>
		);
	}

	return (
		<ChipField
			chips={selected.map((opt) => ({
				key: opt.value,
				label: opt.colorDot ? (
					<span className="inline-flex items-center gap-1.5">
						<span
							className="size-1.75 rounded-full shrink-0"
							style={{ background: opt.colorDot }}
						/>
						{opt.label}
					</span>
				) : (
					opt.label
				),
			}))}
			onRemoveChip={toggle}
			canEdit={canEdit}
			addLabel={t("taskDetail.propertyField.multiSelectEditor.addOption")}
		>
			{options.map((opt) => (
				<OptionListButton
					key={opt.value}
					option={opt}
					isSelected={value.includes(opt.value)}
					onClick={() => toggle(opt.value)}
				/>
			))}
		</ChipField>
	);
}
