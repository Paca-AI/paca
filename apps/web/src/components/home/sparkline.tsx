import { useId } from "react";
import { cn } from "@/lib/utils";

const WIDTH = 100;
const HEIGHT = 32;
const PAD = 3;

/** SVG polyline points for values, scaled into the sparkline's viewBox. A
 *  flat series is drawn across the middle. */
export function sparklinePoints(values: number[]): [number, number][] {
	if (values.length === 0) return [];
	const min = Math.min(...values);
	const max = Math.max(...values);
	const range = max - min;
	const step = values.length > 1 ? WIDTH / (values.length - 1) : 0;
	return values.map((v, i) => [
		values.length > 1 ? i * step : WIDTH / 2,
		range === 0
			? HEIGHT / 2
			: PAD + (1 - (v - min) / range) * (HEIGHT - PAD * 2),
	]);
}

/** Change over the last 7 days of a daily series, or null when the series is
 *  too short to tell. */
export function weekDelta(values: number[]): number | null {
	if (values.length < 8) return null;
	return values[values.length - 1] - values[values.length - 8];
}

/**
 * A small trend line for a stat card. Purely decorative for screen readers
 * apart from its label, which should state what the line shows.
 */
export function Sparkline({
	values,
	label,
	className,
}: {
	values: number[];
	label: string;
	className?: string;
}) {
	const gradientId = useId();
	const points = sparklinePoints(values);
	if (points.length < 2) return null;

	const line = points.map(([x, y]) => `${x},${y}`).join(" ");
	const area = `0,${HEIGHT} ${line} ${WIDTH},${HEIGHT}`;

	return (
		<svg
			viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
			preserveAspectRatio="none"
			role="img"
			aria-label={label}
			className={cn("h-8 w-24 text-primary", className)}
		>
			<defs>
				<linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
					<stop offset="0%" stopColor="currentColor" stopOpacity={0.25} />
					<stop offset="100%" stopColor="currentColor" stopOpacity={0} />
				</linearGradient>
			</defs>
			<polygon points={area} fill={`url(#${gradientId})`} />
			<polyline
				points={line}
				fill="none"
				stroke="currentColor"
				strokeWidth={1.5}
				strokeLinejoin="round"
				strokeLinecap="round"
				vectorEffect="non-scaling-stroke"
			/>
		</svg>
	);
}
