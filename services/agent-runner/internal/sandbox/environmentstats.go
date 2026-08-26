// environmentstats.go reads a static environment's live CPU/memory/disk
// usage straight from its own cgroup v2 filesystem — the same
// backend-agnostic pattern environmentssh.go already established for SSH
// bootstrap/session-detection: one function, driven entirely through the
// shared EnvironmentBackend.ExecEnvironment interface, with no docker- or
// kubernetes-specific code at all. Confirmed live against a real running
// environment container, not assumed: /sys/fs/cgroup/{cpu.stat,cpu.max,
// memory.current,memory.max} are ordinary cgroup v2 files any container
// runtime (Docker, containerd, CRI-O) exposes identically inside the
// container/Pod it's running, regardless of which orchestrator created it.
//
// cgroup v1 is out of scope for this pass — its layout is a different set
// of paths entirely (cpuacct.usage, memory.usage_in_bytes, ...) — a host
// still on v1 gets a clear parse error here rather than silently wrong
// numbers, the same "documented Phase-1 gap, not silently guessed at"
// treatment this codebase already gives the Docker backend's advisory-only
// disk quota (see EnvironmentConfig.DiskLimitGB's own doc comment).
package sandbox

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// EnvironmentStats is one point-in-time reading. CPUUsageUsec is a
// monotonic, cumulative counter (total CPU time consumed since the
// container/Pod started) — turning it into a current-utilization rate
// needs two samples and the wall-clock time between them, which this
// package deliberately leaves to the caller (services/api's polling
// frontend already samples on its own interval; doing the rate math
// server-side here would just mean caching a previous sample somewhere for
// no benefit). CPULimitMillicores/MemoryLimitBytes are 0 when the cgroup
// itself reports no limit ("max") — see parseCPUMax's doc comment — a
// caller should fall back to the environment's own configured
// cpu_limit/memory_limit in that case rather than divide by zero.
type EnvironmentStats struct {
	CPUUsageUsec       int64
	CPULimitMillicores int64
	MemoryUsedBytes    int64
	MemoryLimitBytes   int64
	DiskUsedBytes      int64
}

// environmentStatsScript prints one KEY:value line per stat so a single
// ExecEnvironment call (not five) can gather everything this needs.
// environmentWorkspaceRoot is the same shared constant
// BootstrapEnvironmentSSH's own authorized_keys push targets
// (environmentssh.go) — this environment's actual persisted data, not the
// container's whole (mostly base-image) root filesystem, which is what
// `du` on / would otherwise measure. DISK_USED's `du` is piped through
// `2>/dev/null | cut -f1` deliberately: a transient failure there (an odd
// permission edge case) should degrade to "disk unknown," not take the
// CPU/memory numbers down with it — see the parse loop below.
var environmentStatsScript = strings.Join([]string{
	`echo "CPU_USAGE_USEC:$(awk '/^usage_usec/{print $2}' /sys/fs/cgroup/cpu.stat)"`,
	`echo "CPU_MAX:$(cat /sys/fs/cgroup/cpu.max)"`,
	`echo "MEM_CURRENT:$(cat /sys/fs/cgroup/memory.current)"`,
	`echo "MEM_MAX:$(cat /sys/fs/cgroup/memory.max)"`,
	fmt.Sprintf(`echo "DISK_USED:$(du -sb %s 2>/dev/null | cut -f1)"`, environmentWorkspaceRoot),
}, "\n")

// ReadEnvironmentStats execs environmentStatsScript inside backendRef and
// parses its output. A malformed/missing CPU or memory field is a hard
// error (almost always means this host is on cgroup v1, not a real
// backend fault); a malformed/missing disk field just reads as 0 — see
// environmentStatsScript's own doc comment on why disk gets the softer
// treatment.
func ReadEnvironmentStats(ctx context.Context, backend EnvironmentBackend, backendRef string) (EnvironmentStats, error) {
	output, exitCode, err := backend.ExecEnvironment(ctx, backendRef, []string{"/bin/sh", "-c", environmentStatsScript})
	if err != nil {
		return EnvironmentStats{}, fmt.Errorf("sandbox: read environment stats: %w", err)
	}
	if exitCode != 0 {
		return EnvironmentStats{}, fmt.Errorf("sandbox: read environment stats: exit code %d: %s", exitCode, output)
	}

	fields := make(map[string]string, 5)
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		fields[key] = strings.TrimSpace(value)
	}

	cpuUsageUsec, err := strconv.ParseInt(fields["CPU_USAGE_USEC"], 10, 64)
	if err != nil {
		return EnvironmentStats{}, fmt.Errorf("sandbox: parse cpu usage_usec %q (cgroup v2 required): %w", fields["CPU_USAGE_USEC"], err)
	}
	memoryUsedBytes, err := strconv.ParseInt(fields["MEM_CURRENT"], 10, 64)
	if err != nil {
		return EnvironmentStats{}, fmt.Errorf("sandbox: parse memory.current %q (cgroup v2 required): %w", fields["MEM_CURRENT"], err)
	}
	diskUsedBytes, err := strconv.ParseInt(fields["DISK_USED"], 10, 64)
	if err != nil {
		diskUsedBytes = 0
	}

	return EnvironmentStats{
		CPUUsageUsec:       cpuUsageUsec,
		CPULimitMillicores: parseCPUMax(fields["CPU_MAX"]),
		MemoryUsedBytes:    memoryUsedBytes,
		MemoryLimitBytes:   parseCgroupMax(fields["MEM_MAX"]),
		DiskUsedBytes:      diskUsedBytes,
	}, nil
}

// parseCPUMax converts cpu.max's "<quota> <period>" (both microseconds —
// e.g. "200000 100000" is 2 cores' worth of quota per period) into
// millicores. cpu.max's quota field reads literal "max" when nothing
// constrains this cgroup — 0 signals that back to the caller, the same
// convention parseCgroupMax uses for memory.max's identical "max" case.
func parseCPUMax(raw string) int64 {
	quotaStr, periodStr, ok := strings.Cut(raw, " ")
	if !ok || quotaStr == "max" {
		return 0
	}
	quota, err := strconv.ParseInt(quotaStr, 10, 64)
	if err != nil {
		return 0
	}
	period, err := strconv.ParseInt(periodStr, 10, 64)
	if err != nil || period == 0 {
		return 0
	}
	return quota * 1000 / period
}

// parseCgroupMax parses a plain cgroup "max"-or-integer value (memory.max
// today; this file's other numeric fields are never "max") — 0 for "max"
// or anything unparseable, the "no limit known, caller should fall back"
// signal EnvironmentStats' own doc comment describes.
func parseCgroupMax(raw string) int64 {
	if raw == "max" {
		return 0
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return v
}
