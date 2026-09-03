import { Server } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import {
	ENVIRONMENT_STATUS_COLORS,
	type Environment,
	type EnvironmentStats,
	getStatsTicket,
	resolveWsUrl,
} from "@/lib/environment-api";
import { cn } from "@/lib/utils";

// EnvironmentStatusRing (+ EnvironmentStatusLine, sharing useIdleFraction
// below) is the one signature visual this feature gets: a ring around the
// environment's icon that depletes toward its own idle timeout while
// running — the auto-sleep mechanic is real, environment-specific state
// (unlike an agent or a task, nothing else in this app turns itself off
// from inactivity), and today it's invisible unless you go read the
// idle_timeout_minutes field in a form. Used in both the detail page's and
// the connect page's header, replacing their plain icon-badge + separately
// composed status text.
//
// Non-"running" statuses render the plain icon badge with no ring at all —
// there's nothing to count down when the environment isn't accumulating
// idle time, and an empty/full ring in those states would just be noise.

const RADIUS = 21;
const CIRCUMFERENCE = 2 * Math.PI * RADIUS;

// How often the ring/expiry text recomputes from the already-fetched
// last_active_at — a local tick, not a refetch. Nothing pushes a fresh
// last_active_at for a long-sitting "running" environment on its own (no
// realtime event fires just from time passing — only an actual heartbeat or
// status change does, see environment-api.ts and use-project-realtime.ts),
// so this just needs *something* to force a re-render periodically so the
// ring/text visibly move over a session left open, not to fetch fresher
// data.
const TICK_MS = 30_000;

// hasActiveSshSession comes from the live stats stream (see
// EnvironmentUsage.hasActiveSshSession) — direct SSH access never touches
// last_active_at, so without this signal the ring/countdown below would
// compute elapsed idle time as if nobody were there and repeatedly count
// down to "sleeps in 0m" / red during a long open SSH session, even though
// the idle reaper defers stopping the environment for exactly as long as
// that session stays open (cmd/agent-runner/main.go's
// reapOneIdleEnvironment). While true, this pins the ring full/green
// instead of depleting it.
function useIdleFraction(
	environment: Environment,
	hasActiveSshSession: boolean,
) {
	const isRunning = environment.status === "running";
	const [, tick] = useState(0);

	useEffect(() => {
		if (!isRunning) return;
		const id = setInterval(() => tick((n) => n + 1), TICK_MS);
		return () => clearInterval(id);
	}, [isRunning]);

	if (!isRunning) {
		return {
			isRunning: false,
			fraction: 1,
			minutesLeft: 0,
			nearIdle: false,
			keptAliveBySSH: false,
		};
	}
	if (hasActiveSshSession) {
		return {
			isRunning: true,
			fraction: 1,
			minutesLeft: environment.idle_timeout_minutes,
			nearIdle: false,
			keptAliveBySSH: true,
		};
	}
	const elapsedMin =
		(Date.now() - new Date(environment.last_active_at).getTime()) / 60_000;
	const fraction = Math.max(
		0,
		Math.min(1, 1 - elapsedMin / environment.idle_timeout_minutes),
	);
	const minutesLeft = Math.max(
		0,
		Math.ceil(environment.idle_timeout_minutes - elapsedMin),
	);
	return {
		isRunning: true,
		fraction,
		minutesLeft,
		nearIdle: fraction < 0.25,
		keptAliveBySSH: false,
	};
}

export function EnvironmentStatusRing({
	environment,
	size = 52,
	hasActiveSshSession = false,
}: {
	environment: Environment;
	size?: number;
	// From the caller's own useEnvironmentUsage subscription — not fetched
	// here, so two sibling header elements (this ring + EnvironmentStatusLine)
	// don't each open their own WebSocket for the same one signal. See
	// useIdleFraction's doc comment for why this matters.
	hasActiveSshSession?: boolean;
}) {
	const { isRunning, fraction, nearIdle } = useIdleFraction(
		environment,
		hasActiveSshSession,
	);

	return (
		<div className="relative shrink-0" style={{ width: size, height: size }}>
			{isRunning && (
				<svg
					viewBox="0 0 52 52"
					className="absolute inset-0 -rotate-90"
					style={{ width: size, height: size }}
					aria-hidden="true"
				>
					<circle
						cx="26"
						cy="26"
						r={RADIUS}
						className="stroke-border"
						strokeWidth="2.5"
						fill="none"
					/>
					<circle
						cx="26"
						cy="26"
						r={RADIUS}
						className={cn(
							nearIdle ? "stroke-amber-500" : "stroke-emerald-500",
							"transition-[stroke-dashoffset] duration-500 ease-out",
						)}
						strokeWidth="2.5"
						strokeLinecap="round"
						fill="none"
						strokeDasharray={CIRCUMFERENCE}
						strokeDashoffset={CIRCUMFERENCE * (1 - fraction)}
					/>
				</svg>
			)}
			<div
				className={cn(
					"absolute flex items-center justify-center rounded-xl bg-primary/10",
					isRunning ? "inset-[6px]" : "inset-0",
				)}
			>
				<Server
					className="text-primary"
					style={{ width: size * 0.34, height: size * 0.34 }}
				/>
			</div>
		</div>
	);
}

// EnvironmentStatusLine renders the dot + status word + (while running) the
// "sleeps in Xm" countdown this feature's idle-timeout mechanic deserves,
// plus an optional backend badge — the text companion to
// EnvironmentStatusRing above, meant to sit directly under an
// environment's name in a header.
export function EnvironmentStatusLine({
	environment,
	showBackendBadge = true,
	showDot = true,
	hasActiveSshSession = false,
}: {
	environment: Environment;
	showBackendBadge?: boolean;
	// When false, the dot is omitted entirely (not just hidden) so it
	// leaves no gap behind — the row's own gap-1.5 only applies between
	// rendered children, not around a hidden-but-present one.
	showDot?: boolean;
	// See EnvironmentStatusRing's identically-named prop doc comment.
	hasActiveSshSession?: boolean;
}) {
	const { t } = useTranslation("projects");
	const { isRunning, minutesLeft, keptAliveBySSH } = useIdleFraction(
		environment,
		hasActiveSshSession,
	);

	return (
		<div className="flex items-center gap-1.5 flex-wrap">
			{showDot && (
				<span
					className={cn(
						"size-2 rounded-full",
						isRunning && "animate-pulse",
						ENVIRONMENT_STATUS_COLORS[environment.status].replace(
							"text-",
							"bg-",
						),
					)}
				/>
			)}
			<span
				className={cn(
					"text-sm font-medium",
					ENVIRONMENT_STATUS_COLORS[environment.status],
				)}
			>
				{t(`environments.status.${environment.status}`)}
			</span>
			{isRunning && (
				<span className="text-sm text-muted-foreground">
					·{" "}
					{keptAliveBySSH
						? t("environments.detail.overview.keptAliveBySSH")
						: t("environments.detail.overview.sleepsIn", {
								count: minutesLeft,
							})}
				</span>
			)}
			{showBackendBadge && (
				<Badge variant="secondary" className="text-xs">
					{environment.backend}
				</Badge>
			)}
		</div>
	);
}

// ─────────────────────────────────────────────────────────────────────────────
// Live CPU/memory/disk usage
// ─────────────────────────────────────────────────────────────────────────────

// parseCPULimitCores reads environment.cpu_limit's own format ("2",
// "500m") — the same strings services/agent-runner's docker/k8s backends
// already parse via k8s.io/apimachinery's resource.ParseQuantity. Only
// consulted as a fallback when the live stats poll's own
// cpu_limit_millicores reads 0 (backend reports no enforced limit — an
// edge case, not the normal path now that both backends actually apply
// this value).
export function parseCPULimitCores(raw: string): number {
	const trimmed = raw.trim();
	if (trimmed.endsWith("m")) {
		return Number(trimmed.slice(0, -1)) / 1000;
	}
	const cores = Number(trimmed);
	return Number.isFinite(cores) ? cores : 0;
}

// parseMemoryLimitBytes reads environment.memory_limit's own format
// ("4Gi", "512Mi") — same fallback role as parseCPULimitCores above.
export function parseMemoryLimitBytes(raw: string): number {
	const match = /^([\d.]+)\s*(Ki|Mi|Gi|Ti|K|M|G|T)?$/.exec(raw.trim());
	if (!match) return 0;
	const value = Number(match[1]);
	if (!Number.isFinite(value)) return 0;
	const multipliers: Record<string, number> = {
		Ki: 1024,
		Mi: 1024 ** 2,
		Gi: 1024 ** 3,
		Ti: 1024 ** 4,
		K: 1000,
		M: 1000 ** 2,
		G: 1000 ** 3,
		T: 1000 ** 4,
	};
	const unit = match[2];
	return unit ? value * multipliers[unit] : value;
}

// formatBytes renders a byte count the same binary-unit way
// environment.memory_limit's own "4Gi"/"512Mi" strings already read — GB/MB
// stay unlocalized the same way the existing vitals.cpuValue/diskValue
// templates already bake "vCPU"/"GB" in as literal, untranslated units
// (see those keys across all 9 locales) rather than separately translated
// tokens.
export function formatBytes(bytes: number): string {
	if (bytes <= 0) return "0 MB";
	const gb = bytes / 1024 ** 3;
	if (gb >= 1) return `${gb.toFixed(gb >= 10 ? 0 : 1)} GB`;
	const mb = bytes / 1024 ** 2;
	return `${mb.toFixed(mb >= 10 ? 0 : 1)} MB`;
}

export interface EnvironmentUsage {
	cpuCoresUsed: number | null;
	cpuLimitCores: number;
	cpuFraction: number | null;
	memoryUsedBytes: number;
	memoryLimitBytes: number;
	memoryFraction: number | null;
	diskUsedBytes: number;
	diskLimitBytes: number;
	diskFraction: number | null;
	// hasActiveSshSession is false until the first message arrives (same
	// as every other field here) — see EnvironmentStats.has_active_ssh_session.
	hasActiveSshSession: boolean;
}

// useEnvironmentUsage opens a WebSocket (via getStatsTicket, connecting
// straight to agent-runner the same way environment-terminal.tsx's own
// terminal connection does) while environment is running, and derives
// per-resource fractions from each pushed message. Only CPU needs real
// work: cpu_usage_usec is a monotonic, cumulative counter (total CPU time
// consumed since the container started), not an instantaneous rate, so
// "cores currently in use" has to come from the *difference* between two
// successive messages divided by the wall-clock time between them — kept
// in a ref rather than component state, since it's bookkeeping for the
// next computation, not something that should itself trigger a render.
// Memory and disk are already point-in-time snapshots from the backend,
// no rate math needed.
// STATS_RECONNECT_DELAY_MS bounds how long the rings sit stale after a
// dropped connection before trying again. This stream is passive and
// read-only in both directions — unlike the terminal, where silently
// reconnecting mid-session could be confusing (did my last keystroke
// land?) — so unconditionally retrying is always safe here.
const STATS_RECONNECT_DELAY_MS = 5_000;

export function useEnvironmentUsage(
	projectId: string,
	environmentId: string,
	// Optional so a caller that hasn't loaded the environment yet (e.g.
	// EnvironmentDetailView, which needs this hook called unconditionally
	// before its own `if (!environment) return <Skeleton />`) can still
	// call this hook every render — isRunning is false and every fallback
	// below defaults out until a real Environment arrives.
	environment: Environment | undefined,
): EnvironmentUsage {
	const isRunning = environment?.status === "running";
	const [stats, setStats] = useState<EnvironmentStats | null>(null);

	useEffect(() => {
		if (!isRunning) {
			setStats(null);
			return;
		}

		let cancelled = false;
		let ws: WebSocket | null = null;
		let reconnectTimer: ReturnType<typeof setTimeout> | null = null;

		async function connect() {
			try {
				const ticket = await getStatsTicket(projectId, environmentId);
				if (cancelled) return;
				const socket = new WebSocket(resolveWsUrl(ticket.ws_url));
				ws = socket;
				socket.onmessage = (event) => {
					if (typeof event.data !== "string") return;
					try {
						setStats(JSON.parse(event.data) as EnvironmentStats);
					} catch {
						// Malformed message — ignore it, the next push recovers.
					}
				};
				socket.onclose = () => {
					if (cancelled) return;
					reconnectTimer = setTimeout(connect, STATS_RECONNECT_DELAY_MS);
				};
				socket.onerror = () => {
					socket.close();
				};
			} catch {
				if (!cancelled) {
					reconnectTimer = setTimeout(connect, STATS_RECONNECT_DELAY_MS);
				}
			}
		}
		connect();

		return () => {
			cancelled = true;
			if (reconnectTimer) clearTimeout(reconnectTimer);
			ws?.close();
		};
	}, [isRunning, projectId, environmentId]);

	const prevSampleRef = useRef<{ usec: number; at: number } | null>(null);
	const [cpuCoresUsed, setCpuCoresUsed] = useState<number | null>(null);

	useEffect(() => {
		if (!stats) return;
		const now = Date.now();
		const prev = prevSampleRef.current;
		if (prev) {
			const deltaUsec = stats.cpu_usage_usec - prev.usec;
			const deltaMs = now - prev.at;
			// deltaUsec can go negative across a container recreate (the
			// counter resets to 0) — skip that one sample rather than show a
			// nonsensical negative usage; the next push recovers normally.
			if (deltaMs > 0 && deltaUsec >= 0) {
				setCpuCoresUsed(deltaUsec / 1000 / deltaMs);
			}
		}
		prevSampleRef.current = { usec: stats.cpu_usage_usec, at: now };
	}, [stats]);

	// Reset so a stopped-then-restarted environment doesn't briefly show
	// its last reading from before it stopped.
	useEffect(() => {
		if (!isRunning) {
			prevSampleRef.current = null;
			setCpuCoresUsed(null);
		}
	}, [isRunning]);

	const cpuLimitCores =
		stats && stats.cpu_limit_millicores > 0
			? stats.cpu_limit_millicores / 1000
			: parseCPULimitCores(environment?.cpu_limit ?? "");
	const memoryLimitBytes =
		stats && stats.memory_limit_bytes > 0
			? stats.memory_limit_bytes
			: parseMemoryLimitBytes(environment?.memory_limit ?? "");
	const diskLimitBytes = (environment?.disk_limit_gb ?? 0) * 1024 ** 3;

	const memoryUsedBytes = stats?.memory_used_bytes ?? 0;
	const diskUsedBytes = stats?.disk_used_bytes ?? 0;

	return {
		cpuCoresUsed,
		cpuLimitCores,
		cpuFraction:
			cpuCoresUsed !== null && cpuLimitCores > 0
				? Math.min(1, cpuCoresUsed / cpuLimitCores)
				: null,
		memoryUsedBytes,
		memoryLimitBytes,
		memoryFraction:
			stats && memoryLimitBytes > 0
				? Math.min(1, memoryUsedBytes / memoryLimitBytes)
				: null,
		diskUsedBytes,
		diskLimitBytes,
		diskFraction:
			stats && diskLimitBytes > 0
				? Math.min(1, diskUsedBytes / diskLimitBytes)
				: null,
		hasActiveSshSession: stats?.has_active_ssh_session ?? false,
	};
}

const USAGE_RING_RADIUS = 14;
const USAGE_RING_CIRCUMFERENCE = 2 * Math.PI * USAGE_RING_RADIUS;

// usageColor mirrors common infra-dashboard convention (Grafana, Datadog,
// ...) — quiet under normal load, amber approaching the limit, red at it —
// self-explanatory without a legend, the same reason
// EnvironmentStatusRing's own idle countdown reuses it.
function usageColor(fraction: number): "emerald" | "amber" | "red" {
	if (fraction >= 0.9) return "red";
	if (fraction >= 0.7) return "amber";
	return "emerald";
}

// UsageRingCell is a VitalCard-shaped cell (see environment-detail.tsx)
// with a small ring standing in for the bare number VitalCard shows for
// everything else — CPU/memory/disk are the only vitals with both a
// "current" and a "limit," which is exactly what a ring is for. Renders a
// flat, uncolored ring with no value line when usage isn't known yet
// (environment not running, or the first poll hasn't landed) rather than
// faking a 0% reading.
export function UsageRingCell({
	label,
	valueText,
	limitText,
	fraction,
}: {
	label: string;
	valueText: string | null;
	limitText: string;
	fraction: number | null;
}) {
	const color = fraction !== null ? usageColor(fraction) : null;
	return (
		<div className="rounded-lg border border-border/60 bg-muted/30 px-3 py-2.5 flex items-center gap-3">
			<div className="relative shrink-0" style={{ width: 36, height: 36 }}>
				<svg
					viewBox="0 0 36 36"
					className="absolute inset-0 -rotate-90"
					style={{ width: 36, height: 36 }}
					aria-hidden="true"
				>
					<circle
						cx="18"
						cy="18"
						r={USAGE_RING_RADIUS}
						className="stroke-border"
						strokeWidth="3"
						fill="none"
					/>
					{fraction !== null && (
						<circle
							cx="18"
							cy="18"
							r={USAGE_RING_RADIUS}
							className={cn(
								color === "red" && "stroke-red-500",
								color === "amber" && "stroke-amber-500",
								color === "emerald" && "stroke-emerald-500",
								"transition-[stroke-dashoffset] duration-500 ease-out",
							)}
							strokeWidth="3"
							strokeLinecap="round"
							fill="none"
							strokeDasharray={USAGE_RING_CIRCUMFERENCE}
							strokeDashoffset={
								USAGE_RING_CIRCUMFERENCE * (1 - Math.max(0.02, fraction))
							}
						/>
					)}
				</svg>
			</div>
			<div className="min-w-0">
				<p className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
					{label}
				</p>
				<p className="text-sm font-medium font-mono tabular-nums truncate">
					{valueText ?? limitText}
				</p>
			</div>
		</div>
	);
}
