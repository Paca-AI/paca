import React from "react";
import type { T } from "./i18n";
import { Icon, type IconName } from "./icons";
import { SddApiError } from "./sdd-api";
import type { TeamBar } from "./types";

// Class components + classic JSX (React.createElement) only — no hooks. Shared
// resolution normally hands us the host's React 19 singleton, but class
// components keep working even if the federation share scope ever falls back to
// the bundled copy (hooks would crash across copies). Same rationale as
// com.galaxy.analytics. Stateless presentational helpers are plain functions
// (also hook-free), which are safe across copies too.

// ── Formatting helpers ───────────────────────────────────────────────────────
export function ago(iso: string | null): string {
	if (!iso) return "—";
	const s = Math.floor((Date.now() - new Date(iso).getTime()) / 1000);
	if (s < 0) return "0s";
	if (s < 60) return `${s}s`;
	if (s < 3600) return `${Math.floor(s / 60)}m`;
	if (s < 86400) return `${Math.floor(s / 3600)}h`;
	return `${Math.floor(s / 86400)}d`;
}
export function online(iso: string | null): boolean {
	return !!iso && Date.now() - new Date(iso).getTime() < 15 * 60 * 1000;
}
export function clock(iso: string | null): string {
	if (!iso) return "—";
	const d = new Date(iso);
	if (Number.isNaN(d.getTime())) return "—";
	return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}
export function shortFile(p: string | null): string {
	if (!p) return "";
	return p.split("/").slice(-2).join("/");
}

// ── Data-loading base (each view extends this) ───────────────────────────────
export interface ViewProps {
	t: T;
	/** Parent bumps this to force a fresh (cache-bypassing) refetch. */
	refreshNonce: number;
	/** TEST-ONLY (smoke.mjs): pre-seeded data so views render without a network. */
	__testData?: unknown;
}

interface DVState<D> {
	phase: "loading" | "ready" | "error";
	data: D | null;
	error: Error | null;
}

export abstract class DataView<D> extends React.Component<ViewProps, DVState<D>> {
	private alive = false;

	constructor(props: ViewProps) {
		super(props);
		this.state =
			props.__testData !== undefined
				? { phase: "ready", data: props.__testData as D, error: null }
				: { phase: "loading", data: null, error: null };
	}

	/** Fetch this view's data (force bypasses the 60 s cache). */
	protected abstract fetchData(force: boolean): Promise<D>;
	/** Render the view body from loaded data. */
	protected abstract body(data: D): React.ReactNode;

	componentDidMount() {
		this.alive = true;
		if (this.props.__testData === undefined) this.load(false);
	}
	componentDidUpdate(prev: ViewProps) {
		if (prev.refreshNonce !== this.props.refreshNonce && this.props.__testData === undefined) {
			this.load(true);
		}
	}
	componentWillUnmount() {
		this.alive = false;
	}

	protected reload = () => this.load(true);

	private load(force: boolean) {
		this.fetchData(force).then(
			(d) => {
				if (this.alive) this.setState({ phase: "ready", data: d, error: null });
			},
			(e) => {
				if (!this.alive) return;
				const err = e instanceof Error ? e : new Error(String(e));
				// Keep stale data visible on refresh failures; surface the error text.
				this.setState((s) => ({ phase: s.data ? "ready" : "error", error: err }));
			}
		);
	}

	render() {
		const { phase, data, error } = this.state;
		const t = this.props.t;
		if (phase === "loading" && !data) return <Loading t={t} />;
		if (phase === "error" && error && !data)
			return <ErrorState t={t} error={error} onRetry={this.reload} />;
		if (!data) return null;
		return (
			<>
				{error ? <ErrorState t={t} error={error} onRetry={this.reload} /> : null}
				{this.body(data)}
			</>
		);
	}
}

// ── Presentational primitives (hook-free functions) ──────────────────────────
export function Loading(props: { t: T }) {
	return <div className="gxsd-loading">{props.t("state.loading")}</div>;
}

export function ErrorState(props: { t: T; error: Error; onRetry: () => void }) {
	const auth = props.error instanceof SddApiError && props.error.isAuthError;
	return (
		<div className="gxsd-error">
			<div className="gxsd-error-title">
				<Icon name="alert" size={15} />
				{auth ? props.t("state.sessionExpired") : props.t("state.error")}
			</div>
			<div style={{ marginBottom: 8 }}>
				{auth ? props.t("state.sessionExpiredBody") : props.error.message}
			</div>
			<button type="button" className="gxsd-btn" onClick={props.onRetry}>
				{props.t("state.retry")}
			</button>
		</div>
	);
}

export function Empty(props: { children: React.ReactNode }) {
	return <div className="gxsd-empty">{props.children}</div>;
}

export type Tone = "default" | "emerald" | "amber" | "rose" | "sky";
const TONE_CLASS: Record<Tone, string> = {
	default: "",
	emerald: "t-emerald",
	amber: "t-amber",
	rose: "t-rose",
	sky: "t-sky",
};

export function StatTile(props: {
	icon: IconName;
	label: string;
	value: number | string;
	tone?: Tone;
}) {
	return (
		<div className="gxsd-stat">
			<div className="gxsd-stat-label">
				<Icon name={props.icon} size={13} />
				{props.label}
			</div>
			<div className={`gxsd-stat-value ${TONE_CLASS[props.tone ?? "default"]}`}>{props.value}</div>
		</div>
	);
}

export function LevelBadge(props: { level: number | null }) {
	if (props.level == null) return null;
	const l = Math.min(4, Math.max(1, props.level));
	return <span className={`gxsd-badge lvl${l}`}>L{props.level}</span>;
}

export function PhasePill(props: { children: React.ReactNode }) {
	return <span className="gxsd-pill">{props.children}</span>;
}

/** Horizontal labeled bars (analytics "by X" panels). */
export function Bars(props: { title: string; icon?: IconName; data: TeamBar[]; color?: string }) {
	const max = Math.max(1, ...props.data.map((d) => d.n));
	return (
		<div className="gxsd-card">
			<h3 className="gxsd-card-title">
				{props.icon ? <Icon name={props.icon} size={14} /> : null}
				{props.title}
			</h3>
			{props.data.length === 0 ? (
				<Empty>—</Empty>
			) : (
				<div className="gxsd-bars">
					{props.data.map((d, i) => (
						<div className="gxsd-bar-row" key={`${d.label}-${i}`}>
							<span className="gxsd-bar-label" title={d.label}>
								{d.label}
							</span>
							<span className="gxsd-bar-track">
								<span
									className={`gxsd-bar-fill ${props.color ?? ""}`}
									style={{ width: `${(d.n / max) * 100}%` }}
								/>
							</span>
							<span className="gxsd-bar-val">{d.n}</span>
						</div>
					))}
				</div>
			)}
		</div>
	);
}
