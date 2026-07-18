import React from "react";
import { Bars, DataView, Empty, LevelBadge, PhasePill, StatTile, ago, clock, online, shortFile, type ViewProps } from "./base";
import type { ViewKey } from "./config";
import { Icon, type IconName } from "./icons";
import { sddApi } from "./sdd-api";
import type {
	EventsResult,
	FleetResult,
	SddFlagsResult,
	SddOverview,
	SddSpecVersionsResult,
	SessionsResult,
	TasksResult,
	TeamAnalytics,
	TeamCoordination,
	TeamOverview,
} from "./types";

// Status columns shared by Overview's task row and the Task board.
const TASK_COLS: { key: string; label: string }[] = [
	{ key: "todo", label: "Todo" },
	{ key: "assigned", label: "Assigned" },
	{ key: "in_progress", label: "In Progress" },
	{ key: "review", label: "Review" },
	{ key: "done", label: "Done" },
];

// ── 1. Overview (TeamDashboard) ──────────────────────────────────────────────
class OverviewView extends DataView<TeamOverview> {
	protected fetchData(force: boolean) {
		return sddApi.teamOverview({ force });
	}
	protected body(d: TeamOverview) {
		const t = this.props.t;
		return (
			<>
				<div className="gxsd-stats">
					<StatTile icon="server" label={t("overview.machinesOnline")} value={d.machines_online} tone="emerald" />
					<StatTile icon="users" label={t("overview.devActive")} value={d.active_devs} />
					<StatTile icon="folder" label={t("overview.sessionsActive")} value={d.active_sessions} />
					<StatTile icon="activity" label={t("overview.totalEvents")} value={d.total_events} />
					<StatTile icon="shield" label={t("overview.openConflicts")} value={d.open_conflicts} tone={d.open_conflicts > 0 ? "rose" : "default"} />
					<StatTile icon="lock" label={t("overview.gatesPending")} value={d.pending_gates} tone={d.pending_gates > 0 ? "amber" : "default"} />
				</div>

				<div className="gxsd-section">
					<div className="gxsd-card">
						<h3 className="gxsd-card-title">
							<Icon name="list" size={14} />
							{t("overview.tasksByStatus")}
						</h3>
						<div className="gxsd-cols">
							{TASK_COLS.map((c) => (
								<div className="gxsd-col" key={c.key} style={{ padding: "10px 12px" }}>
									<div className="gxsd-stat-value" style={{ fontSize: 20 }}>{d.tasksByStatus[c.key] || 0}</div>
									<div className="gxsd-col-owner" style={{ marginBottom: 0 }}>{c.label}</div>
								</div>
							))}
						</div>
					</div>
				</div>

				<div className="gxsd-card">
					<h3 className="gxsd-card-title">
						<Icon name="activity" size={14} />
						{t("overview.recent")}
					</h3>
					{d.recent.length === 0 ? (
						<Empty>{t("state.empty")}</Empty>
					) : (
						<div className="gxsd-list">
							{d.recent.map((a, i) => (
								<div className="gxsd-act" key={i}>
									<LevelBadge level={a.level} />
									{a.phase ? <PhasePill>{a.phase}</PhasePill> : null}
									<span className="gxsd-act-main">{a.tool_name || "—"}</span>
									<span className="gxsd-act-meta">
										{a.user_name || "?"} @ {a.hostname || "?"}
									</span>
								</div>
							))}
						</div>
					)}
				</div>
			</>
		);
	}
}

// ── 2. Task board (TeamKanban — read-only) ───────────────────────────────────
const PRIORITY_TONE: Record<string, React.CSSProperties> = {
	high: { borderColor: "color-mix(in srgb, var(--gxsd-rose) 45%, var(--gxsd-border))" },
	normal: {},
	low: { opacity: 0.85 },
};
function taskLevelClass(l: number | null) {
	return l === 4 ? "t-rose" : l === 3 ? "t-amber" : l === 2 ? "t-sky" : "";
}
class TasksView extends DataView<TasksResult> {
	protected fetchData(force: boolean) {
		return sddApi.tasks({ force });
	}
	protected body(d: TasksResult) {
		const t = this.props.t;
		return (
			<>
				<div className="gxsd-foot gxsd-flex" style={{ marginBottom: 12, paddingTop: 0 }}>
					<Icon name="lock" size={12} />
					{t("tasks.readonly")}
				</div>
				<div className="gxsd-grid" style={{ gridTemplateColumns: "repeat(auto-fit, minmax(190px, 1fr))" }}>
					{TASK_COLS.map((col) => {
						const items = d.tasks.filter((x) => x.status === col.key);
						return (
							<div className="gxsd-col" key={col.key}>
								<div className="gxsd-col-head">
									<span className="gxsd-col-title">{col.label}</span>
									<span className="gxsd-count">{items.length}</span>
								</div>
								<div className="gxsd-cards">
									{items.length === 0 ? <div className="gxsd-empty" style={{ padding: 4 }}>—</div> : null}
									{items.map((task) => (
										<div className="gxsd-agent" key={task.id} style={PRIORITY_TONE[task.priority] || undefined}>
											<div className="gxsd-agent-name" style={{ fontWeight: 500 }}>{task.title}</div>
											{task.assignee_name || task.assignee_hostname ? (
												<div className="gxsd-agent-meta gxsd-flex">
													<Icon name="user" size={11} />
													{task.assignee_name || task.assignee_email || t("tasks.unassigned")}
													{task.assignee_hostname ? (
														<>
															<Icon name="server" size={11} />
															{task.assignee_hostname}
														</>
													) : null}
												</div>
											) : null}
											{task.repo ? <div className="gxsd-agent-meta">{task.repo}</div> : null}
											{task.live_phase ? (
												<div className="gxsd-flex" style={{ marginTop: 4 }}>
													<PhasePill>{task.live_phase}</PhasePill>
													{task.live_level != null ? (
														<span className={`gxsd-badge ${taskLevelClass(task.live_level)}`}>L{task.live_level}</span>
													) : null}
													<span className="t-emerald" style={{ fontSize: 9 }}>● {t("tasks.live")}</span>
												</div>
											) : null}
										</div>
									))}
								</div>
							</div>
						);
					})}
				</div>
			</>
		);
	}
}

// ── 3. Sessions ──────────────────────────────────────────────────────────────
class SessionsView extends DataView<SessionsResult> {
	protected fetchData(force: boolean) {
		return sddApi.sessions({ force });
	}
	protected body(d: SessionsResult) {
		const t = this.props.t;
		if (!d.sessions.length) return <Empty>{t("state.empty")}</Empty>;
		return (
			<div className="gxsd-card">
				<div className="gxsd-table-wrap">
					<table className="gxsd-table">
						<thead>
							<tr>
								<th>{t("sessions.repo")}</th>
								<th>{t("sessions.host")}</th>
								<th>{t("sessions.user")}</th>
								<th>{t("sessions.status")}</th>
								<th>{t("sessions.updated")}</th>
							</tr>
						</thead>
						<tbody>
							{d.sessions.map((s) => (
								<tr key={s.id}>
									<td>{s.repo || "—"}</td>
									<td className="mut">{s.hostname || "—"}</td>
									<td className="mut">{s.user_name || s.email || s.user_id}</td>
									<td>
										<span className="gxsd-flex">
											<span className={`gxsd-dot ${s.status === "active" ? "on" : ""}`} />
											{s.status}
										</span>
									</td>
									<td className="mut">{ago(s.updated_at)}</td>
								</tr>
							))}
						</tbody>
					</table>
				</div>
			</div>
		);
	}
}

// ── 4. Activity (raw hook-event stream) ──────────────────────────────────────
class ActivityView extends DataView<EventsResult> {
	protected fetchData(force: boolean) {
		return sddApi.events({ force });
	}
	protected body(d: EventsResult) {
		const t = this.props.t;
		if (!d.events.length) return <Empty>{t("state.empty")}</Empty>;
		return (
			<div className="gxsd-card">
				<div className="gxsd-list">
					{d.events.map((e) => (
						<div className="gxsd-act" key={e.id}>
							<span className="gxsd-pill">{e.event_type}</span>
							<span className="gxsd-act-main">{e.tool_name || e.summary || e.event_type}</span>
							<span className="gxsd-act-meta">{e.email || e.user_id}</span>
							<span className="gxsd-act-meta">{clock(e.created_at)}</span>
						</div>
					))}
				</div>
			</div>
		);
	}
}

// ── 5. Analytics (TeamAnalytics) ─────────────────────────────────────────────
const LVL_LABEL = ["", "L1 Read", "L2 Branch", "L3 Shared Core", "L4 Merge/Deploy"];
class AnalyticsView extends DataView<TeamAnalytics> {
	protected fetchData(force: boolean) {
		return sddApi.teamAnalytics({ force });
	}
	protected body(d: TeamAnalytics) {
		const t = this.props.t;
		const lvlMax = Math.max(1, ...d.levelDist.map((x) => x.n));
		const dayMax = Math.max(1, ...d.daily.map((x) => x.n));
		return (
			<>
				<div className="gxsd-grid" style={{ marginBottom: 12 }}>
					<Bars title={t("analytics.byDev")} icon="users" data={d.byUser} />
					<Bars title={t("analytics.byMachine")} icon="server" data={d.byHost} color="c-emerald" />
					<Bars title={t("analytics.byRepo")} icon="git" data={d.byRepo} color="c-sky" />
				</div>
				<div className="gxsd-grid-2" style={{ marginBottom: 12 }}>
					<Bars title={t("analytics.phaseDist")} icon="layers" data={d.phaseDist} color="c-violet" />
					<div className="gxsd-card">
						<h3 className="gxsd-card-title">
							<Icon name="gauge" size={14} />
							{t("analytics.levelDist")}
						</h3>
						{d.levelDist.length === 0 ? (
							<Empty>—</Empty>
						) : (
							<div className="gxsd-bars">
								{d.levelDist.map((l) => (
									<div className="gxsd-bar-row" key={l.level}>
										<span className="gxsd-bar-label">{LVL_LABEL[l.level] || `L${l.level}`}</span>
										<span className="gxsd-bar-track">
											<span className={`gxsd-bar-fill c-l${l.level}`} style={{ width: `${(l.n / lvlMax) * 100}%` }} />
										</span>
										<span className="gxsd-bar-val">{l.n}</span>
									</div>
								))}
							</div>
						)}
					</div>
				</div>
				<div className="gxsd-card">
					<h3 className="gxsd-card-title">
						<Icon name="activity" size={14} />
						{t("analytics.daily")}
					</h3>
					{d.daily.length === 0 ? (
						<Empty>—</Empty>
					) : (
						<div className="gxsd-spark">
							{d.daily.map((x) => (
								<div className="gxsd-spark-col" key={x.day} title={`${x.day}: ${x.n}`}>
									<div className="gxsd-spark-bar" style={{ height: `${(x.n / dayMax) * 68}px` }} />
									<span className="gxsd-spark-lbl">{x.day}</span>
								</div>
							))}
						</div>
					)}
				</div>
			</>
		);
	}
}

// ── 6. Coordination (TeamCoordination) ───────────────────────────────────────
class CoordinationView extends DataView<TeamCoordination> {
	protected fetchData(force: boolean) {
		return sddApi.teamCoordination({ force });
	}
	protected body(d: TeamCoordination) {
		const t = this.props.t;
		return (
			<>
				<div className="gxsd-card gxsd-section">
					<h3 className="gxsd-card-title">
						<Icon name="shield" size={14} />
						{t("coord.openConflicts")}
					</h3>
					{d.conflicts.length === 0 ? (
						<div className="gxsd-ok">
							<Icon name="check" size={15} />
							{t("coord.noConflicts")}
						</div>
					) : (
						<div className="gxsd-list">
							{d.conflicts.map((c) => {
								const users = ((c.detail as { users?: string[] }) || {}).users || [];
								return (
									<div className="gxsd-conflict" key={c.id}>
										<div className="gxsd-flex">
											<span className="gxsd-badge t-rose">{c.kind}</span>
											<span className="gxsd-act-main">{c.conflict_key}</span>
										</div>
										{users.length ? <div className="gxsd-act-meta" style={{ marginTop: 3 }}>{users.join(", ")}</div> : null}
									</div>
								);
							})}
						</div>
					)}
				</div>

				<div className="gxsd-card">
					<h3 className="gxsd-card-title">
						<Icon name="git" size={14} />
						{t("coord.parallelByRepo")}
					</h3>
					{d.byRepo.length === 0 ? (
						<Empty>—</Empty>
					) : (
						<div className="gxsd-grid">
							{d.byRepo.map((r) => (
								<div className="gxsd-agent" key={r.repo}>
									<div className="gxsd-agent-name" style={{ fontWeight: 600, fontSize: 13 }}>{r.repo}</div>
									<div className="gxsd-agent-meta gxsd-flex" style={{ gap: 10 }}>
										<span className="gxsd-flex"><Icon name="users" size={11} />{r.devs}</span>
										<span className="gxsd-flex"><Icon name="server" size={11} />{r.machines}</span>
										<span>{r.sessions}</span>
									</div>
									{r.dev_names?.length ? <div className="gxsd-agent-meta">{r.dev_names.join(", ")}</div> : null}
									{r.phases?.length ? (
										<div className="gxsd-flex" style={{ marginTop: 4 }}>
											{r.phases.map((p) => <PhasePill key={p}>{p}</PhasePill>)}
										</div>
									) : null}
								</div>
							))}
						</div>
					)}
				</div>
			</>
		);
	}
}

// ── 7. SDD phases (Sdd.tsx) ──────────────────────────────────────────────────
interface SddBundle {
	ov: SddOverview;
	specs: SddSpecVersionsResult;
	flags: SddFlagsResult;
}
const LEVELS = [1, 2, 3, 4] as const;
class SddPhasesView extends DataView<SddBundle> {
	protected async fetchData(force: boolean): Promise<SddBundle> {
		const [ov, specs, flags] = await Promise.all([
			sddApi.sdd({ force }),
			sddApi.specVersions({ force }),
			sddApi.flags({ force }),
		]);
		return { ov, specs, flags };
	}
	protected body(d: SddBundle) {
		const t = this.props.t;
		const { ov, specs, flags } = d;
		const totalPhaseAgents = Object.values(ov.board).reduce((a, b) => a + b.length, 0);
		const hasFlags = flags.sharedCore.length > 0 || flags.unapprovedL3.length > 0;
		return (
			<>
				<div className="gxsd-stats">
					<StatTile icon="list" label={t("sdd.phaseBoard")} value={totalPhaseAgents} />
					<StatTile icon="files" label={t("sdd.specVersions")} value={specs.count} tone="sky" />
					<StatTile icon="shield" label={t("sdd.sharedCore")} value={ov.sharedCoreCount} tone={ov.sharedCoreCount > 0 ? "amber" : "default"} />
					<StatTile icon="shield" label={t("sdd.unapprovedL3")} value={ov.unapprovedL3Count} tone={ov.unapprovedL3Count > 0 ? "rose" : "default"} />
				</div>

				<div className="gxsd-card gxsd-section">
					<h3 className="gxsd-card-title"><Icon name="gauge" size={14} />{t("sdd.governance")}</h3>
					<div className="gxsd-cols">
						{LEVELS.map((n) => (
							<div className="gxsd-col" key={n} style={{ padding: "10px 12px" }}>
								<div className={`gxsd-stat-value t-${n === 4 ? "rose" : n === 3 ? "amber" : n === 2 ? "sky" : "default"}`} style={{ fontSize: 20 }}>
									{ov.levelCounts?.[String(n)] ?? 0}
								</div>
								<div className="gxsd-col-owner" style={{ marginBottom: 0 }}>{t(`sdd.l${n}`)}</div>
							</div>
						))}
					</div>
				</div>

				<div className="gxsd-section">
					<h3 className="gxsd-h2"><Icon name="list" size={14} />{t("sdd.phaseBoard")}</h3>
					<div className="gxsd-grid" style={{ gridTemplateColumns: "repeat(auto-fit, minmax(190px, 1fr))" }}>
						{ov.phases.map((p) => {
							const agents = ov.board[p.key] || [];
							return (
								<div className="gxsd-col" key={p.key}>
									<div className="gxsd-col-head">
										<span className="gxsd-col-title">{p.label}</span>
										<span className="gxsd-count">{agents.length}</span>
									</div>
									{p.owner ? <div className="gxsd-col-owner">{p.owner}</div> : null}
									<div className="gxsd-cards">
										{agents.length === 0 ? <div className="gxsd-empty" style={{ padding: 4 }}>{t("sdd.noAgents")}</div> : null}
										{agents.map((a) => (
											<div className="gxsd-agent" key={a.id}>
												<div className="gxsd-agent-top">
													<span className="gxsd-agent-name" title={a.name}>{a.name}</span>
													<LevelBadge level={a.sdd_level} />
												</div>
												{a.spec_version ? <div className="gxsd-agent-meta">{a.spec_doc_id}@{a.spec_version}</div> : null}
											</div>
										))}
									</div>
								</div>
							);
						})}
					</div>
				</div>

				<div className="gxsd-grid-2 gxsd-section">
					<div className="gxsd-card">
						<h3 className="gxsd-card-title"><Icon name="shield" size={14} />{t("sdd.flags")}</h3>
						{hasFlags ? (
							<div className="gxsd-list">
								{flags.unapprovedL3.slice(0, 8).map((a) => (
									<div className="gxsd-act" key={`u${a.id}`}>
										<LevelBadge level={a.level} />
										<span className="gxsd-act-main">{a.tool_name || a.summary || a.hook_type}</span>
										<span className="gxsd-act-meta">{shortFile(a.file_path)}</span>
									</div>
								))}
								{flags.sharedCore.slice(0, 8).map((a) => (
									<div className="gxsd-act" key={`s${a.id}`}>
										<span className="gxsd-badge t-amber">SC</span>
										<span className="gxsd-act-main">{a.tool_name || a.summary || a.hook_type}</span>
										<span className="gxsd-act-meta">{shortFile(a.file_path)}</span>
									</div>
								))}
							</div>
						) : (
							<div className="gxsd-ok"><Icon name="check" size={15} />{t("sdd.noFlags")}</div>
						)}
					</div>

					<div className="gxsd-card">
						<h3 className="gxsd-card-title"><Icon name="files" size={14} />{t("sdd.specVersions")}</h3>
						{specs.count > 0 ? (
							<div className="gxsd-list">
								{Object.entries(specs.docs).map(([docId, versions]) => (
									<div key={docId}>
										<div className="gxsd-agent-name" style={{ fontSize: 12, marginBottom: 3 }} title={docId}>{docId}</div>
										<div className="gxsd-flex">
											{versions.map((v) => (
												<span className="gxsd-badge t-sky" key={v.id} title={v.title || undefined}>
													v{v.version}
													{v.implemented_ref ? <span className="t-emerald">· {t("sdd.implemented")}</span> : null}
												</span>
											))}
										</div>
									</div>
								))}
							</div>
						) : (
							<Empty>{t("sdd.noSpecs")}</Empty>
						)}
					</div>
				</div>

				<div className="gxsd-card">
					<h3 className="gxsd-card-title"><Icon name="activity" size={14} />{t("sdd.recent")}</h3>
					{ov.recent.length === 0 ? (
						<Empty>{t("state.empty")}</Empty>
					) : (
						<div className="gxsd-list">
							{ov.recent.slice(0, 25).map((a) => (
								<div className="gxsd-act" key={a.id}>
									<LevelBadge level={a.level} />
									{a.phase ? <PhasePill>{a.phase}</PhasePill> : null}
									{a.lifecycle ? <span className="gxsd-pill gxsd-life">{a.lifecycle}</span> : null}
									<span className="gxsd-act-main">{a.tool_name || a.summary || a.hook_type}</span>
									{a.file_path ? <span className="gxsd-act-meta">{shortFile(a.file_path)}</span> : null}
								</div>
							))}
						</div>
					)}
				</div>
			</>
		);
	}
}

// ── 8. Fleet (TeamFleet) ─────────────────────────────────────────────────────
class FleetView extends DataView<FleetResult> {
	protected fetchData(force: boolean) {
		return sddApi.teamFleet({ force });
	}
	protected body(d: FleetResult) {
		const t = this.props.t;
		if (!d.machines.length) return <Empty>{t("state.empty")}</Empty>;
		return (
			<div className="gxsd-grid">
				{d.machines.map((m, i) => (
					<div className="gxsd-card" key={i} style={{ padding: "12px 14px" }}>
						<div className="gxsd-flex" style={{ justifyContent: "space-between" }}>
							<span className="gxsd-flex" style={{ fontSize: 13, fontWeight: 500 }}>
								<Icon name="server" size={14} />
								{m.hostname || "?"}
							</span>
							<span className="gxsd-flex" style={{ fontSize: 10, color: "var(--gxsd-muted)" }}>
								<span className={`gxsd-dot ${online(m.last_seen) ? "on" : ""}`} />
								{online(m.last_seen) ? t("fleet.online") : ago(m.last_seen)}
							</span>
						</div>
						<div className="gxsd-agent-meta" style={{ marginTop: 4 }}>{m.user_name || m.email || m.user_id}</div>
						<div className="gxsd-flex" style={{ marginTop: 6 }}>
							<span className="gxsd-act-meta">{m.sessions} {t("fleet.sessions")}</span>
							{m.current_phase ? <PhasePill>{m.current_phase}</PhasePill> : null}
							{m.current_level != null ? <span className="gxsd-act-meta">L{m.current_level}</span> : null}
						</div>
					</div>
				))}
			</div>
		);
	}
}

// ── Registry consumed by SddFleetView ────────────────────────────────────────
export interface ViewDef {
	key: ViewKey;
	icon: IconName;
	Component: React.ComponentType<ViewProps>;
}

export const VIEWS: ViewDef[] = [
	{ key: "overview", icon: "gauge", Component: OverviewView },
	{ key: "tasks", icon: "list", Component: TasksView },
	{ key: "sessions", icon: "folder", Component: SessionsView },
	{ key: "activity", icon: "activity", Component: ActivityView },
	{ key: "analytics", icon: "layers", Component: AnalyticsView },
	{ key: "coordination", icon: "git", Component: CoordinationView },
	{ key: "sdd", icon: "radar", Component: SddPhasesView },
	{ key: "fleet", icon: "server", Component: FleetView },
];
