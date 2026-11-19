import { useEffect, useState } from "react";

export function NumberEditor({
	value,
	onChange,
}: {
	value: number;
	onChange?: (value: number) => void;
}) {
	const [local, setLocal] = useState(value);

	useEffect(() => {
		setLocal(value);
	}, [value]);

	return (
		<input
			type="number"
			min="0"
			value={local}
			onChange={(e) => setLocal(Math.max(0, Number(e.target.value)))}
			onBlur={() => {
				const val = Math.max(0, local);
				if (val !== value) onChange?.(val);
			}}
			className="w-16 rounded-lg border border-border/50 bg-muted/25 px-2.5 py-1 text-sm text-center tabular-nums font-medium focus:outline-none focus:ring-2 focus:ring-primary/20 focus:border-primary/40 transition-all duration-150"
		/>
	);
}
