import { X } from "lucide-react";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import { COLOR_PRESETS } from "@/lib/color-presets";

// Compact color picker: a small circular trigger (shows the current color, or
// a dashed ring when none is set) that opens a popover with the shared
// preset swatches, a native custom-color input, and a clear button. Reuses
// the same preset palette/interaction as TaskStatusFormDialog and
// TaskTypeFormDialog, just packaged for use inline in a list of rows (e.g.
// one per custom field option) rather than a single full-width field.
export function ColorSwatchPicker({
	value,
	onChange,
	triggerLabel,
	customColorTitle,
	clearLabel,
}: {
	value: string | null | undefined;
	onChange: (color: string | null) => void;
	triggerLabel: string;
	customColorTitle: string;
	clearLabel: string;
}) {
	return (
		<Popover>
			<PopoverTrigger
				type="button"
				title={triggerLabel}
				aria-label={triggerLabel}
				className="relative size-6 shrink-0 rounded-full border-2 border-border/50 transition-transform hover:scale-110"
				style={
					value ? { backgroundColor: value, borderColor: value } : undefined
				}
			>
				{!value && (
					<span className="absolute inset-1 rounded-full border border-dashed border-muted-foreground/40" />
				)}
			</PopoverTrigger>
			<PopoverContent className="w-auto p-2" align="start">
				<div className="flex max-w-44 flex-wrap items-center gap-1.5">
					{COLOR_PRESETS.map((preset) => (
						<button
							key={preset}
							type="button"
							className={`size-5 rounded-full border-2 transition-transform hover:scale-110 ${
								value === preset
									? "border-foreground scale-110"
									: "border-transparent"
							}`}
							style={{ backgroundColor: preset }}
							onClick={() => onChange(preset)}
							aria-label={preset}
						/>
					))}
					<label
						title={customColorTitle}
						className={`relative size-5 shrink-0 cursor-pointer overflow-hidden rounded-full border-2 transition-transform hover:scale-110 ${
							value && !COLOR_PRESETS.includes(value)
								? "border-foreground scale-110"
								: "border-transparent"
						}`}
						style={{
							background:
								"conic-gradient(#ef4444, #f97316, #eab308, #22c55e, #14b8a6, #06b6d4, #3b82f6, #6366f1, #8b5cf6, #ec4899, #ef4444)",
							backgroundSize: "120% 120%",
							backgroundPosition: "center",
						}}
					>
						<input
							type="color"
							value={value ?? "#6366f1"}
							onChange={(e) => onChange(e.target.value)}
							className="sr-only"
						/>
					</label>
					{value && (
						<button
							type="button"
							title={clearLabel}
							aria-label={clearLabel}
							onClick={() => onChange(null)}
							className="flex size-5 shrink-0 items-center justify-center rounded-full border-2 border-transparent text-muted-foreground transition-colors hover:text-destructive"
						>
							<X className="size-3.5" />
						</button>
					)}
				</div>
			</PopoverContent>
		</Popover>
	);
}
