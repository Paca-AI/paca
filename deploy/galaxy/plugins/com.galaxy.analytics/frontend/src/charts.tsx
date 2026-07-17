import React from "react";
import type { DistributionRow, SprintProgress, VelocityEntry } from "./analytics";
import {
	CATEGORY_LABELS,
	CATEGORY_ORDER,
	formatDay,
	formatPoints,
} from "./analytics";
import { CATEGORY_VAR } from "./theme";

// Dependency-free inline SVG charts. Class components + React.createElement
// only (no hooks): shared resolution normally hands us the host's React 19
// singleton, but class components keep working even if the federation share
// scope ever falls back to the bundled copy — hooks would crash across
// copies. Same rationale as com.galaxy.sdd.
//
// Chart conventions (dataviz method): thin marks (bars <= 24px, 2px lines,
// >= 8px markers with a 2px surface ring), solid hairline grid, 2px surface
// gaps between stacked segments, selective direct labels (endpoint/extreme),
// legend with visible counts for multi-series, tooltips that enhance but
// never gate (every value is also in a label, the legend or the report
// table), text in ink tokens - never in the series color.

// ── Shared tooltip plumbing ──────────────────────────────────────────────────

export interface TipRow {
	/** Swatch color (CSS value); empty string = no key. */
	color: string;
	value: string;
	label: string;
}

export interface TipState {
	x: number;
	y: number;
	title: string;
	rows: TipRow[];
}

interface TipProps {
	tip: TipState | null;
}

/** Stateless (hook-free) tooltip view, absolutely positioned in the panel. */
export function TipView(props: TipProps) {
	const tip = props.tip;
	if (!tip) return null;
	return (
		<div className="gxan-tip" style={{ left: tip.x + 12, top: tip.y + 12 }}>
			<div className="t-title">{tip.title}</div>
			{tip.rows.map((r, i) => (
				<div className="t-row" key={i}>
					{r.color ? <span className="t-key" style={{ background: r.color }} /> : null}
					<span className="t-val">{r.value}</span>
					<span className="t-lbl">{r.label}</span>
				</div>
			))}
		</div>
	);
}

/**
 * Base class handling tooltip state + coordinates. Charts render inside the
 * container div this class provides via renderChart().
 */
abstract class TooltipChart<P> extends React.Component<P, { tip: TipState | null }> {
	protected containerRef = React.createRef<HTMLDivElement>();

	constructor(props: P) {
		super(props);
		this.state = { tip: null };
	}

	protected showTipAt(clientX: number, clientY: number, title: string, rows: TipRow[]) {
		const box = this.containerRef.current?.getBoundingClientRect();
		if (!box) return;
		this.setState({
			tip: {
				// Keep the tooltip inside the panel horizontally.
				x: Math.min(clientX - box.left, Math.max(0, box.width - 190)),
				y: clientY - box.top,
				title,
				rows,
			},
		});
	}

	protected tipFromMouse = (
		evt: React.MouseEvent,
		title: string,
		rows: TipRow[],
	) => {
		this.showTipAt(evt.clientX, evt.clientY, title, rows);
	};

	protected tipFromFocus = (
		evt: React.FocusEvent<SVGElement>,
		title: string,
		rows: TipRow[],
	) => {
		const r = (evt.currentTarget as SVGGraphicsElement).getBoundingClientRect();
		this.showTipAt(r.left + r.width / 2, r.top, title, rows);
	};

	protected clearTip = () => {
		if (this.state.tip) this.setState({ tip: null });
	};

	protected abstract renderChart(): React.ReactNode;

	render() {
		return (
			<div ref={this.containerRef} style={{ position: "relative" }}>
				{this.renderChart()}
				<TipView tip={this.state.tip} />
			</div>
		);
	}
}

// ── Small scale helpers ──────────────────────────────────────────────────────

/** Round a max up to a clean tick ceiling (1/2/2.5/5 x 10^n). */
function niceCeil(v: number): number {
	if (v <= 0) return 1;
	const exp = Math.floor(Math.log10(v));
	const base = 10 ** exp;
	for (const m of [1, 2, 2.5, 5, 10]) {
		if (v <= m * base) return m * base;
	}
	return 10 * base;
}

function truncate(text: string, maxChars: number): string {
	if (text.length <= maxChars) return text;
	return `${text.slice(0, Math.max(1, maxChars - 1))}…`;
}

const INK2 = "var(--gxan-ink-2)";
const MUTED = "var(--gxan-muted)";
const GRID = "var(--gxan-grid)";
const BASELINE = "var(--gxan-baseline)";
const ACCENT = "var(--gxan-accent)";
const SURFACE = "var(--gxan-surface)";

// ── Burndown (honest v1: ideal line + today's actual remaining) ─────────────

interface BurndownProps {
	progress: SprintProgress;
	nowMs: number;
}

export class BurndownChart extends TooltipChart<BurndownProps> {
	protected renderChart(): React.ReactNode {
		const p = this.props.progress;
		const W = 560;
		const H = 190;
		const m = { l: 40, r: 20, t: 14, b: 28 };
		const iw = W - m.l - m.r;
		const ih = H - m.t - m.b;

		// Scale: y in work units (points, or tasks when no points anywhere).
		const total = p.hasPoints ? p.totalPoints : p.totalTasks;
		const unit = p.hasPoints ? "pts" : "tasks";
		const yMax = Math.max(1, total);
		const y = (v: number) => m.t + ih - (v / yMax) * ih;

		if (p.startMs === null || p.endMs === null || p.endMs <= p.startMs) {
			return (
				<div className="gxan-empty">
					Sprint has no start/end dates — set them to see the ideal burndown line.
				</div>
			);
		}
		const x = (t: number) =>
			m.l + ((t - p.startMs!) / (p.endMs! - p.startMs!)) * iw;
		const nowClamped = Math.min(Math.max(this.props.nowMs, p.startMs), p.endMs);
		const nowX = x(nowClamped);
		// Subtract in work units (not via the ratio) to avoid FP noise like
		// 10.000000000000002 leaking into the label.
		const remaining = p.hasPoints
			? p.totalPoints - p.donePoints
			: p.totalTasks - p.doneTasks;
		const idealRemaining = (p.idealRemainingRatio ?? 1) * total;
		const remainingLabel = `${formatPoints(remaining)} ${unit} left`;

		const tipTitle = `Today · ${formatDay(this.props.nowMs)}`;
		const tipRows: TipRow[] = [
			{ color: ACCENT, value: formatPoints(remaining), label: `${unit} remaining (actual)` },
			{ color: MUTED, value: formatPoints(idealRemaining), label: `${unit} remaining (ideal)` },
		];

		return (
			<svg
				className="gxan-svg"
				viewBox={`0 0 ${W} ${H}`}
				role="img"
				aria-label={`Burndown: ${remainingLabel} of ${formatPoints(total)} ${unit}, ideal ${formatPoints(idealRemaining)}`}
			>
				{/* grid: 0 / mid / max — solid hairlines */}
				{[0, yMax / 2, yMax].map((v, i) => (
					<line key={i} x1={m.l} x2={W - m.r} y1={y(v)} y2={y(v)}
						stroke={i === 0 ? BASELINE : GRID} strokeWidth={1} />
				))}
				{[0, yMax / 2, yMax].map((v, i) => (
					<text key={i} x={m.l - 6} y={y(v) + 3.5} textAnchor="end" fontSize={10}
						fill={MUTED} style={{ fontVariantNumeric: "tabular-nums" }}>
						{formatPoints(v)}
					</text>
				))}

				{/* ideal line: total -> 0 across the timebox */}
				<line x1={x(p.startMs)} y1={y(total)} x2={x(p.endMs)} y2={y(0)}
					stroke={MUTED} strokeWidth={2} strokeLinecap="round" />
				<text x={x(p.endMs) - 4} y={y(0) - 6} textAnchor="end" fontSize={10} fill={INK2}>
					Ideal
				</text>

				{/* today: hairline + actual-remaining marker (ring = surface) */}
				<line x1={nowX} x2={nowX} y1={m.t} y2={m.t + ih} stroke={GRID} strokeWidth={1} />
				<circle
					cx={nowX} cy={y(remaining)} r={5}
					fill={ACCENT} stroke={SURFACE} strokeWidth={2}
					tabIndex={0}
					onMouseMove={(e) => this.tipFromMouse(e, tipTitle, tipRows)}
					onMouseLeave={this.clearTip}
					onFocus={(e) => this.tipFromFocus(e, tipTitle, tipRows)}
					onBlur={this.clearTip}
				/>
				{/* generous invisible hit area around the 10px dot */}
				<circle cx={nowX} cy={y(remaining)} r={14} fill="transparent"
					onMouseMove={(e) => this.tipFromMouse(e, tipTitle, tipRows)}
					onMouseLeave={this.clearTip} />
				<text
					x={Math.min(nowX + 10, W - m.r - 4)}
					y={y(remaining) - 8}
					textAnchor={nowX > W - m.r - 90 ? "end" : "start"}
					fontSize={10} fontWeight={600} fill={INK2}>
					{remainingLabel}
				</text>

				{/* x labels: start / today / end */}
				<text x={m.l} y={H - 8} fontSize={10} fill={MUTED}>{formatDay(p.startMs)}</text>
				<text x={nowX} y={H - 8} fontSize={10} fill={INK2} textAnchor="middle">today</text>
				<text x={W - m.r} y={H - 8} fontSize={10} fill={MUTED} textAnchor="end">
					{formatDay(p.endMs)}
				</text>
			</svg>
		);
	}
}

// ── Velocity bars ────────────────────────────────────────────────────────────

interface VelocityProps {
	entries: VelocityEntry[]; // chronological
	averagePoints: number;
}

export class VelocityChart extends TooltipChart<VelocityProps> {
	protected renderChart(): React.ReactNode {
		const { entries, averagePoints } = this.props;
		const W = 560;
		const H = 220;
		// Right margin fits the "avg NN.N" annotation label outside the plot.
		const m = { l: 40, r: 58, t: 16, b: 34 };
		const iw = W - m.l - m.r;
		const ih = H - m.t - m.b;

		const yMax = niceCeil(Math.max(1, ...entries.map((e) => e.donePoints), averagePoints));
		const y = (v: number) => m.t + ih - (v / yMax) * ih;
		const band = iw / entries.length;
		const barW = Math.min(24, Math.max(8, band * 0.5));

		const maxIdx = entries.reduce(
			(best, e, i) => (e.donePoints > entries[best].donePoints ? i : best),
			0,
		);
		const labelIdx = new Set([maxIdx, entries.length - 1]); // extreme + latest

		return (
			<svg
				className="gxan-svg"
				viewBox={`0 0 ${W} ${H}`}
				role="img"
				aria-label={`Velocity per completed sprint, average ${formatPoints(averagePoints)} points`}
			>
				{[0, yMax / 2, yMax].map((v, i) => (
					<line key={i} x1={m.l} x2={W - m.r} y1={y(v)} y2={y(v)}
						stroke={i === 0 ? BASELINE : GRID} strokeWidth={1} />
				))}
				{[0, yMax / 2, yMax].map((v, i) => (
					<text key={i} x={m.l - 6} y={y(v) + 3.5} textAnchor="end" fontSize={10}
						fill={MUTED} style={{ fontVariantNumeric: "tabular-nums" }}>
						{formatPoints(v)}
					</text>
				))}

				{entries.map((e, i) => {
					const cx = m.l + band * i + band / 2;
					const bx = cx - barW / 2;
					const by = y(e.donePoints);
					const bh = m.t + ih - by;
					const r = Math.min(4, barW / 2, bh); // rounded data-end, square baseline
					const title = e.sprint.name;
					const rows: TipRow[] = [
						{ color: ACCENT, value: formatPoints(e.donePoints), label: "points done" },
						{ color: "", value: String(e.doneTasks), label: "tasks done" },
					];
					return (
						<g key={e.sprint.id}>
							{bh > 0 ? (
								<path
									d={`M ${bx} ${by + bh} L ${bx} ${by + r} Q ${bx} ${by} ${bx + r} ${by} L ${bx + barW - r} ${by} Q ${bx + barW} ${by} ${bx + barW} ${by + r} L ${bx + barW} ${by + bh} Z`}
									fill={ACCENT}
								/>
							) : null}
							{labelIdx.has(i) && e.donePoints > 0 ? (
								<text x={cx} y={by - 5} textAnchor="middle" fontSize={10}
									fontWeight={600} fill={INK2}
									style={{ fontVariantNumeric: "tabular-nums" }}>
									{formatPoints(e.donePoints)}
								</text>
							) : null}
							<text x={cx} y={H - 10} textAnchor="middle" fontSize={10} fill={MUTED}>
								{truncate(e.sprint.name, Math.max(4, Math.floor(band / 6.5)))}
							</text>
							{/* full-band hit target (>= mark, min 24px) */}
							<rect
								x={m.l + band * i} y={m.t} width={band} height={ih + 18}
								fill="transparent" tabIndex={0}
								onMouseMove={(evt) => this.tipFromMouse(evt, title, rows)}
								onMouseLeave={this.clearTip}
								onFocus={(evt) => this.tipFromFocus(evt, title, rows)}
								onBlur={this.clearTip}
							/>
						</g>
					);
				})}

				{/* average annotation (solid hairline, labeled) */}
				{entries.length > 1 ? (
					<g>
						<line x1={m.l} x2={W - m.r} y1={y(averagePoints)} y2={y(averagePoints)}
							stroke={MUTED} strokeWidth={1} />
						<text x={W - m.r + 4} y={y(averagePoints) + 3.5} fontSize={10} fill={INK2}
							style={{ fontVariantNumeric: "tabular-nums" }}>
							avg {formatPoints(averagePoints)}
						</text>
					</g>
				) : null}
			</svg>
		);
	}
}

// ── Status distribution (stacked horizontal bars + legend with counts) ──────

interface DistributionProps {
	rows: DistributionRow[];
}

export class DistributionChart extends TooltipChart<DistributionProps> {
	protected renderChart(): React.ReactNode {
		const rows = this.props.rows;
		const W = 560;
		const labelW = 120;
		const totalW = 34;
		const rowH = 30;
		const barH = 14;
		const H = rows.length * rowH + 6;
		const iw = W - labelW - totalW - 10;
		const maxTotal = Math.max(1, ...rows.map((r) => r.total));
		const GAP = 2; // surface gap between segments

		return (
			<svg
				className="gxan-svg"
				viewBox={`0 0 ${W} ${H}`}
				role="img"
				aria-label="Task count by status category for the backlog and live sprints"
			>
				{rows.map((row, ri) => {
					const yTop = ri * rowH + 6;
					const byCat = new Map(row.counts.map((c) => [c.category, c.count]));
					let xCursor = labelW;
					const segs: React.ReactNode[] = [];
					const catsWithCounts = [
						...CATEGORY_ORDER.map((cat) => ({
							key: cat as string,
							label: CATEGORY_LABELS[cat],
							color: CATEGORY_VAR[cat],
							count: byCat.get(cat) ?? 0,
						})),
						{
							key: "none",
							label: "No status",
							color: BASELINE,
							count: row.uncategorized,
						},
					];
					for (const c of catsWithCounts) {
						if (c.count <= 0) continue;
						const w = (c.count / maxTotal) * iw;
						const sx = xCursor;
						xCursor += w; // gap is painted INSIDE the segment's right edge
						const tipRows: TipRow[] = [
							{ color: c.color, value: String(c.count), label: c.label },
							{ color: "", value: String(row.total), label: "total in scope" },
						];
						segs.push(
							<rect
								key={c.key}
								x={sx}
								y={yTop}
								width={Math.max(0.5, w - GAP)}
								height={barH}
								fill={c.color}
								tabIndex={0}
								onMouseMove={(evt) => this.tipFromMouse(evt, row.label, tipRows)}
								onMouseLeave={this.clearTip}
								onFocus={(evt) => this.tipFromFocus(evt, row.label, tipRows)}
								onBlur={this.clearTip}
							/>,
						);
					}
					return (
						<g key={row.label + ri}>
							<text x={labelW - 8} y={yTop + barH - 3} textAnchor="end" fontSize={11}
								fill={INK2}>
								{truncate(row.label, 18)}
							</text>
							{row.total === 0 ? (
								<text x={labelW} y={yTop + barH - 3} fontSize={10} fill={MUTED}>
									no tasks
								</text>
							) : (
								segs
							)}
							<text x={labelW + iw + 8} y={yTop + barH - 3} fontSize={11}
								fill={INK2} fontWeight={600}
								style={{ fontVariantNumeric: "tabular-nums" }}>
								{row.total}
							</text>
						</g>
					);
				})}
			</svg>
		);
	}
}

/** Legend with per-category totals — visible values, not hover-gated. */
export function DistributionLegend(props: { rows: DistributionRow[] }) {
	const totals = new Map<string, number>();
	let none = 0;
	for (const row of props.rows) {
		for (const c of row.counts) {
			totals.set(c.category, (totals.get(c.category) ?? 0) + c.count);
		}
		none += row.uncategorized;
	}
	return (
		<div className="gxan-legend">
			{CATEGORY_ORDER.map((cat) => (
				<span key={cat}>
					<span className="sw" style={{ background: CATEGORY_VAR[cat] }} />
					{CATEGORY_LABELS[cat]} <strong>{totals.get(cat) ?? 0}</strong>
				</span>
			))}
			{none > 0 ? (
				<span>
					<span className="sw" style={{ background: BASELINE }} />
					No status <strong>{none}</strong>
				</span>
			) : null}
		</div>
	);
}
