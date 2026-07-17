import React from "react";
import type {
	DistributionRow,
	SprintProgress,
	SprintReportRow,
	VelocityResult,
} from "./analytics";
import {
	computeDistribution,
	computeSprintProgress,
	computeSprintReport,
	computeVelocity,
	formatDay,
	formatPct,
	formatPoints,
	statusById,
} from "./analytics";
import { BurndownChart, DistributionChart, DistributionLegend, VelocityChart } from "./charts";
import type { ProjectAnalyticsData, Sprint } from "./paca-api";
import { getProjectAnalyticsData, PacaApiError } from "./paca-api";
import { ensureThemeInjected } from "./theme";

/**
 * "Analytics" — the plugin's single exposed component, registered at two
 * extension points with the same code:
 *  - "view":         selectable board layout (+ Add view -> Analytics); the
 *                    host passes {projectId, tasks, statuses, ...} but the
 *                    host's task list is the CURRENT VIEW's filtered page,
 *                    so we deliberately ignore it and fetch the full project
 *                    snapshot ourselves (all sprints + backlog).
 *  - "project.page": full-page route
 *                    /projects/:projectId/plugins/com.galaxy.analytics/analytics,
 *                    reached from the "Analytics" sidebar nav item; the host
 *                    passes {projectId} only.
 *
 * Class component + classic JSX on purpose — see tsconfig.json.
 */

interface AnalyticsProps {
	projectId?: string;
	/**
	 * TEST-ONLY (smoke.mjs): pre-seeded data + clock so the loader-contract
	 * replay can render every panel without a network. Never set by the host.
	 */
	__testData?: ProjectAnalyticsData;
	__testNowMs?: number;
	[key: string]: unknown;
}

interface AnalyticsState {
	phase: "loading" | "ready" | "error";
	data: ProjectAnalyticsData | null;
	error: Error | null;
	/** Active-sprint selector (only shown when >1 sprint is active). */
	selectedSprintId: string | null;
}

export default class AnalyticsView extends React.Component<AnalyticsProps, AnalyticsState> {
	private alive = false;

	constructor(props: AnalyticsProps) {
		super(props);
		ensureThemeInjected();
		this.state = props.__testData
			? { phase: "ready", data: props.__testData, error: null, selectedSprintId: null }
			: { phase: "loading", data: null, error: null, selectedSprintId: null };
	}

	componentDidMount() {
		this.alive = true;
		if (!this.props.__testData) this.load(false);
	}

	componentDidUpdate(prev: AnalyticsProps) {
		if (prev.projectId !== this.props.projectId && !this.props.__testData) {
			this.setState({ phase: "loading", data: null, error: null, selectedSprintId: null });
			this.load(false);
		}
	}

	componentWillUnmount() {
		this.alive = false;
	}

	private load(force: boolean) {
		const projectId = typeof this.props.projectId === "string" ? this.props.projectId : "";
		if (!projectId) {
			this.setState({
				phase: "error",
				error: new Error("This surface did not provide a projectId."),
			});
			return;
		}
		// Hold the previous render during refetch (no skeleton flash).
		if (this.state.data) this.setState({ phase: "ready" });
		getProjectAnalyticsData(projectId, { force }).then(
			(data) => {
				if (!this.alive || this.props.projectId !== projectId) return;
				this.setState({ phase: "ready", data, error: null });
			},
			(err: unknown) => {
				if (!this.alive || this.props.projectId !== projectId) return;
				// Keep stale data visible if we have it; surface the error text.
				this.setState({
					phase: this.state.data ? "ready" : "error",
					error: err instanceof Error ? err : new Error(String(err)),
				});
			},
		);
	}

	private nowMs(): number {
		return this.props.__testNowMs ?? Date.now();
	}

	// ── Renders ────────────────────────────────────────────────────────────────

	private renderError(err: Error) {
		const auth = err instanceof PacaApiError && err.isAuthError;
		return (
			<div className="gxan-error">
				<div style={{ fontWeight: 600, marginBottom: 4 }}>
					{auth ? "Session expired" : "Could not load analytics"}
				</div>
				<div style={{ marginBottom: 8 }}>
					{auth
						? "Your Paca session is no longer valid for API calls. Reload the app to sign back in, then reopen this page."
						: err.message}
				</div>
				<button type="button" className="gxan-btn" onClick={() => this.load(true)}>
					Retry
				</button>
			</div>
		);
	}

	private renderSprintProgress(progress: SprintProgress | null, activeSprints: Sprint[]) {
		return (
			<div className="gxan-card">
				<h3 className="gxan-card-title">Sprint Progress</h3>
				{progress ? (
					<div>
						<p className="gxan-card-sub">
							{progress.sprint.name}
							{progress.sprint.goal ? ` — ${progress.sprint.goal}` : ""}
						</p>
						<div className="gxan-hero">{formatPct(progress.completionRatio)}</div>
						<div className="gxan-meter" aria-hidden="true">
							<div style={{ width: `${Math.min(100, Math.max(0, progress.completionRatio * 100))}%` }} />
						</div>
						<div className="gxan-kv">
							{progress.hasPoints ? (
								<div>
									<strong>{formatPoints(progress.donePoints)}</strong> /{" "}
									{formatPoints(progress.totalPoints)} pts done
								</div>
							) : null}
							<div>
								<strong>{progress.doneTasks}</strong> / {progress.totalTasks} tasks done
							</div>
							{progress.elapsedRatio !== null ? (
								<div>
									<strong>{formatPct(progress.elapsedRatio)}</strong> of timebox elapsed
								</div>
							) : null}
						</div>
						{!progress.hasPoints && progress.totalTasks > 0 ? (
							<p className="gxan-card-sub" style={{ marginTop: 8 }}>
								No story points on this sprint's tasks — progress uses task counts.
							</p>
						) : null}
						<BurndownChart progress={progress} nowMs={this.nowMs()} />
					</div>
				) : (
					<div className="gxan-empty">
						No active sprint. Start a sprint to track its progress here
						{activeSprints.length === 0 ? "" : "."}
					</div>
				)}
			</div>
		);
	}

	private renderVelocity(velocity: VelocityResult) {
		return (
			<div className="gxan-card">
				<h3 className="gxan-card-title">Velocity</h3>
				<p className="gxan-card-sub">
					Story points on done tasks per completed sprint
				</p>
				{velocity.entries.length === 0 ? (
					<div className="gxan-empty">
						No completed sprints yet — complete a sprint to start the velocity record.
					</div>
				) : (
					<div>
						<VelocityChart
							entries={velocity.entries}
							averagePoints={velocity.averagePoints}
						/>
						<div className="gxan-kv">
							<div>
								<strong>{formatPoints(velocity.averagePoints)}</strong> pts average
							</div>
							<div>
								<strong>{velocity.entries.length}</strong> completed sprint
								{velocity.entries.length === 1 ? "" : "s"}
							</div>
						</div>
					</div>
				)}
			</div>
		);
	}

	private renderDistribution(rows: DistributionRow[]) {
		const anything = rows.some((r) => r.total > 0);
		return (
			<div className="gxan-card">
				<h3 className="gxan-card-title">Status Distribution</h3>
				<p className="gxan-card-sub">
					Tasks by status category — backlog and live sprints (current snapshot)
				</p>
				{anything ? (
					<div>
						<DistributionChart rows={rows} />
						<DistributionLegend rows={rows} />
					</div>
				) : (
					<div className="gxan-empty">No tasks yet.</div>
				)}
			</div>
		);
	}

	private renderReport(rows: SprintReportRow[]) {
		return (
			<div className="gxan-card gxan-card-wide">
				<h3 className="gxan-card-title">Sprint Report</h3>
				<p className="gxan-card-sub">
					Per-sprint delivery — tasks attached now, done, still open, and points
				</p>
				{rows.length === 0 ? (
					<div className="gxan-empty">No sprints in this project yet.</div>
				) : (
					<div className="gxan-table-wrap">
						<table className="gxan-table">
							<thead>
								<tr>
									<th>Sprint</th>
									<th>Status</th>
									<th>Window</th>
									<th className="num">Tasks done</th>
									<th className="num">Open</th>
									<th className="num">Pts done</th>
									<th className="num">Pts total</th>
								</tr>
							</thead>
							<tbody>
								{rows.map((r) => (
									<tr key={r.sprint.id}>
										<td>{r.sprint.name}</td>
										<td>
											<span className="gxan-badge">{r.sprint.status}</span>
										</td>
										<td style={{ color: "var(--gxan-ink-2)" }}>
											{formatDay(r.sprint.start_date ? Date.parse(r.sprint.start_date) : null)}
											{" → "}
											{formatDay(r.sprint.end_date ? Date.parse(r.sprint.end_date) : null)}
										</td>
										<td className="num">
											{r.tasksDone} / {r.tasksAttached}
										</td>
										<td className="num">{r.sprint.status === "completed" ? "—" : r.tasksOpen}</td>
										<td className="num">{formatPoints(r.pointsDone)}</td>
										<td className="num">{formatPoints(r.pointsTotal)}</td>
									</tr>
								))}
							</tbody>
						</table>
					</div>
				)}
			</div>
		);
	}

	render() {
		const { phase, data, error, selectedSprintId } = this.state;

		let body: React.ReactNode = null;
		let activeSprints: Sprint[] = [];
		let asOf = "";

		if (phase === "loading") {
			body = <div className="gxan-empty" style={{ padding: "24px 16px" }}>Loading analytics…</div>;
		} else if (phase === "error" && error) {
			body = this.renderError(error);
		} else if (data) {
			const byId = statusById(data.statuses);
			activeSprints = data.sprints.filter((s) => s.status === "active");
			const selected =
				activeSprints.find((s) => s.id === selectedSprintId) ?? activeSprints[0] ?? null;
			const progress = selected
				? computeSprintProgress(selected, data.tasks, byId, this.nowMs())
				: null;
			const velocity = computeVelocity(data);
			const distribution = computeDistribution(data);
			const report = computeSprintReport(data);
			const d = new Date(data.fetchedAt);
			asOf = `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;

			body = (
				<div>
					{error ? this.renderError(error) : null}
					<div className="gxan-grid">
						{this.renderSprintProgress(progress, activeSprints)}
						{this.renderVelocity(velocity)}
						{this.renderDistribution(distribution)}
						{this.renderReport(report)}
					</div>
					<div className="gxan-foot">
						Data notes: computed client-side from the current snapshot of tasks,
						sprints and statuses ({asOf ? `fetched ${asOf}, ` : ""}cached 60s) using
						your own Paca session. v1 has no task history: the burndown plots the
						ideal line and <em>today's</em> actual remaining only — past days are
						not reconstructed. Completed sprints keep only their done tasks (Paca
						moves unfinished tasks out at completion), so their task counts reflect
						delivery, not the original commitment; "Open" is therefore only
						meaningful for active and planned sprints. "Done" means the task's
						status category is <em>done</em>.
					</div>
				</div>
			);
		}

		return (
			<div className="gxan-root">
				<div className="gxan-head">
					<span className="gxan-title">Analytics</span>
					<span className="gxan-sub">sprint progress · velocity · status · report</span>
					<span className="gxan-spacer" />
					{activeSprints.length > 1 ? (
						<label className="gxan-sub">
							Active sprint{" "}
							<select
								className="gxan-select"
								value={
									(activeSprints.find((s) => s.id === selectedSprintId) ??
										activeSprints[0]).id
								}
								onChange={(e) => this.setState({ selectedSprintId: e.target.value })}
							>
								{activeSprints.map((s) => (
									<option key={s.id} value={s.id}>
										{s.name}
									</option>
								))}
							</select>
						</label>
					) : null}
					{!this.props.__testData ? (
						<button
							type="button"
							className="gxan-btn"
							onClick={() => this.load(true)}
							disabled={phase === "loading"}
						>
							Refresh
						</button>
					) : null}
				</div>
				{body}
			</div>
		);
	}
}
