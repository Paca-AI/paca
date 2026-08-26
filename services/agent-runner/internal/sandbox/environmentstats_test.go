package sandbox

import (
	"context"
	"strings"
	"testing"
)

// statsExecKey is the exact joined ExecEnvironment command
// ReadEnvironmentStats issues — fakeEnvironmentBackend.execResults keys off
// strings.Join(cmd, " "), so tests need this to match precisely.
var statsExecKey = strings.Join([]string{"/bin/sh", "-c", environmentStatsScript}, " ")

func TestReadEnvironmentStats_RealCapturedOutput(t *testing.T) {
	// Byte-for-byte the output captured live from a running environment
	// container's own cgroup v2 filesystem (see environmentstats.go's own
	// doc comment) — not invented.
	output := "CPU_USAGE_USEC:534549\n" +
		"CPU_MAX:200000 100000\n" +
		"MEM_CURRENT:19234816\n" +
		"MEM_MAX:4294967296\n" +
		"DISK_USED:12798\n"

	backend := &fakeEnvironmentBackend{
		execResults: map[string]struct {
			output   string
			exitCode int
			err      error
		}{
			statsExecKey: {output: output, exitCode: 0},
		},
	}

	stats, err := ReadEnvironmentStats(context.Background(), backend, "container-123")
	if err != nil {
		t.Fatalf("ReadEnvironmentStats: %v", err)
	}
	if stats.CPUUsageUsec != 534549 {
		t.Errorf("CPUUsageUsec = %d, want 534549", stats.CPUUsageUsec)
	}
	if stats.CPULimitMillicores != 2000 {
		t.Errorf("CPULimitMillicores = %d, want 2000 (2 cores)", stats.CPULimitMillicores)
	}
	if stats.MemoryUsedBytes != 19234816 {
		t.Errorf("MemoryUsedBytes = %d, want 19234816", stats.MemoryUsedBytes)
	}
	if stats.MemoryLimitBytes != 4294967296 {
		t.Errorf("MemoryLimitBytes = %d, want 4294967296 (4GiB)", stats.MemoryLimitBytes)
	}
	if stats.DiskUsedBytes != 12798 {
		t.Errorf("DiskUsedBytes = %d, want 12798", stats.DiskUsedBytes)
	}
}

func TestReadEnvironmentStats_UnlimitedCPUAndMemory(t *testing.T) {
	output := "CPU_USAGE_USEC:100\n" +
		"CPU_MAX:max 100000\n" +
		"MEM_CURRENT:1000\n" +
		"MEM_MAX:max\n" +
		"DISK_USED:500\n"

	backend := &fakeEnvironmentBackend{
		execResults: map[string]struct {
			output   string
			exitCode int
			err      error
		}{
			statsExecKey: {output: output, exitCode: 0},
		},
	}

	stats, err := ReadEnvironmentStats(context.Background(), backend, "container-123")
	if err != nil {
		t.Fatalf("ReadEnvironmentStats: %v", err)
	}
	if stats.CPULimitMillicores != 0 {
		t.Errorf("CPULimitMillicores = %d, want 0 (unlimited)", stats.CPULimitMillicores)
	}
	if stats.MemoryLimitBytes != 0 {
		t.Errorf("MemoryLimitBytes = %d, want 0 (unlimited)", stats.MemoryLimitBytes)
	}
}

func TestReadEnvironmentStats_DiskFailureDegradesToZero(t *testing.T) {
	// DISK_USED absent entirely (a `du` failure the script's own
	// `2>/dev/null | cut -f1` reduced to an empty value) must not fail the
	// whole call — only the CPU/memory fields are load-bearing.
	output := "CPU_USAGE_USEC:100\n" +
		"CPU_MAX:200000 100000\n" +
		"MEM_CURRENT:1000\n" +
		"MEM_MAX:2000\n" +
		"DISK_USED:\n"

	backend := &fakeEnvironmentBackend{
		execResults: map[string]struct {
			output   string
			exitCode int
			err      error
		}{
			statsExecKey: {output: output, exitCode: 0},
		},
	}

	stats, err := ReadEnvironmentStats(context.Background(), backend, "container-123")
	if err != nil {
		t.Fatalf("ReadEnvironmentStats: %v", err)
	}
	if stats.DiskUsedBytes != 0 {
		t.Errorf("DiskUsedBytes = %d, want 0", stats.DiskUsedBytes)
	}
	if stats.CPUUsageUsec != 100 {
		t.Errorf("CPUUsageUsec = %d, want 100 (unaffected by disk failure)", stats.CPUUsageUsec)
	}
}

func TestReadEnvironmentStats_MissingCPUStatIsHardError(t *testing.T) {
	// Empty CPU_USAGE_USEC (the shape a cgroup v1 host — no
	// /sys/fs/cgroup/cpu.stat at all — would produce) must fail loudly,
	// not report a fabricated 0% usage.
	output := "CPU_USAGE_USEC:\n" +
		"CPU_MAX:\n" +
		"MEM_CURRENT:1000\n" +
		"MEM_MAX:2000\n" +
		"DISK_USED:500\n"

	backend := &fakeEnvironmentBackend{
		execResults: map[string]struct {
			output   string
			exitCode int
			err      error
		}{
			statsExecKey: {output: output, exitCode: 0},
		},
	}

	if _, err := ReadEnvironmentStats(context.Background(), backend, "container-123"); err == nil {
		t.Fatal("ReadEnvironmentStats: want error for missing cgroup v2 cpu.stat, got nil")
	}
}

func TestReadEnvironmentStats_NonZeroExitIsError(t *testing.T) {
	backend := &fakeEnvironmentBackend{
		execResults: map[string]struct {
			output   string
			exitCode int
			err      error
		}{
			statsExecKey: {output: "sh: cat: not found", exitCode: 127},
		},
	}

	if _, err := ReadEnvironmentStats(context.Background(), backend, "container-123"); err == nil {
		t.Fatal("ReadEnvironmentStats: want error for non-zero exit code, got nil")
	}
}

func TestParseCPUMax(t *testing.T) {
	cases := []struct {
		raw  string
		want int64
	}{
		{"200000 100000", 2000},
		{"100000 100000", 1000},
		{"50000 100000", 500},
		{"max 100000", 0},
		{"", 0},
		{"garbage", 0},
	}
	for _, tc := range cases {
		if got := parseCPUMax(tc.raw); got != tc.want {
			t.Errorf("parseCPUMax(%q) = %d, want %d", tc.raw, got, tc.want)
		}
	}
}

func TestParseCgroupMax(t *testing.T) {
	cases := []struct {
		raw  string
		want int64
	}{
		{"4294967296", 4294967296},
		{"max", 0},
		{"", 0},
		{"garbage", 0},
	}
	for _, tc := range cases {
		if got := parseCgroupMax(tc.raw); got != tc.want {
			t.Errorf("parseCgroupMax(%q) = %d, want %d", tc.raw, got, tc.want)
		}
	}
}
