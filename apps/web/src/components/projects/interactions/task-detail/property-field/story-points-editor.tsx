import { ChevronDown, ChevronUp } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

const FIBONACCI_SP = [0, 1, 2, 3, 5, 8, 13, 21, 34, 55, 89, 144];

function nextFibSP(current: number | null): number {
	if (current === null || current < 0) return 1;
	return (
		FIBONACCI_SP.find((f) => f > current) ??
		FIBONACCI_SP[FIBONACCI_SP.length - 1]
	);
}

function prevFibSP(current: number | null): number | null {
	if (current === null || current <= 0) return null;
	return [...FIBONACCI_SP].reverse().find((f) => f < current) ?? null;
}

export function StoryPointsEditor({
	value,
	onChange,
}: {
	value: number | null;
	onChange?: (value: number | null) => void;
}) {
	const { t } = useTranslation();
	const [local, setLocal] = useState<string>(
		value != null ? String(value) : "",
	);

	useEffect(() => {
		setLocal(value != null ? String(value) : "");
	}, [value]);

	const commit = (raw: string) => {
		const trimmed = raw.trim();
		if (trimmed === "") {
			if (value !== null) onChange?.(null);
			return;
		}

		const parsed = Number(trimmed);
		if (!Number.isFinite(parsed)) return;

		const next = Math.trunc(Math.max(0, parsed));
		if (next !== value) onChange?.(next);
	};

	const stepUp = (e: React.MouseEvent) => {
		e.stopPropagation();
		const next = nextFibSP(value);
		setLocal(String(next));
		onChange?.(next);
	};

	const stepDown = (e: React.MouseEvent) => {
		e.stopPropagation();
		const prev = prevFibSP(value);
		setLocal(prev != null ? String(prev) : "");
		onChange?.(prev);
	};

	return (
		<div className="flex items-center gap-0.5">
			<button
				type="button"
				onClick={stepDown}
				disabled={value === null || value <= 0}
				aria-label={t("project.interactions.taskDetail.decreaseStoryPoints")}
				className="flex size-6 items-center justify-center rounded-md text-muted-foreground hover:bg-muted/60 hover:text-foreground transition-all disabled:opacity-30 disabled:cursor-not-allowed"
			>
				<ChevronDown className="size-3.5" />
			</button>
			<input
				type="number"
				min="0"
				placeholder="—"
				value={local}
				onChange={(e) => setLocal(e.target.value)}
				onBlur={(e) => commit(e.target.value)}
				onKeyDown={(e) => {
					if (e.key === "Enter") commit(local);
					if (e.key === "ArrowUp") {
						e.preventDefault();
						const next = nextFibSP(value);
						setLocal(String(next));
						onChange?.(next);
					}
					if (e.key === "ArrowDown") {
						e.preventDefault();
						const prev = prevFibSP(value);
						setLocal(prev != null ? String(prev) : "");
						onChange?.(prev);
					}
				}}
				className="w-12 rounded-lg border border-border/30 bg-muted/25 px-1.5 py-1 text-sm text-center tabular-nums font-medium focus:outline-none focus:ring-2 focus:ring-primary/20 focus:border-primary/40 transition-all duration-150 placeholder:text-muted-foreground/40 [appearance:textfield] [&::-webkit-outer-spin-button]:appearance-none [&::-webkit-inner-spin-button]:appearance-none"
			/>
			<button
				type="button"
				onClick={stepUp}
				aria-label={t("project.interactions.taskDetail.increaseStoryPoints")}
				className="flex size-6 items-center justify-center rounded-md text-muted-foreground hover:bg-muted/60 hover:text-foreground transition-all"
			>
				<ChevronUp className="size-3.5" />
			</button>
		</div>
	);
}
