import { Checkbox } from "@/components/ui/checkbox";

export function CheckboxEditor({
	checked = false,
	canEdit,
	onChange,
}: {
	checked: boolean;
	canEdit: boolean;
	onChange?: (value: boolean) => void;
}) {
	return (
		<Checkbox
			checked={checked}
			disabled={!canEdit}
			onCheckedChange={(value) => canEdit && onChange?.(value)}
		/>
	);
}
