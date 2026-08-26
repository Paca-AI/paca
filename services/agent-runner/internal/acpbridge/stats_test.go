package acpbridge

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/sandbox"
)

// countingStatsBackend implements sandbox.EnvironmentBackend just enough
// for environmentStatsHub.run's two calls per tick
// (sandbox.ReadEnvironmentStats, sandbox.EnvironmentHasActiveSSHSession):
// every ExecEnvironment call increments execs and, going on the one
// distinguishing substring each of those two issues its command with,
// returns either a fixed valid stats reading or an sshd-session count
// controlled by sshActive. The other interface methods are never
// exercised by environmentStatsHub and just satisfy the type.
type countingStatsBackend struct {
	execs     atomic.Int64
	sshActive atomic.Bool
}

func (b *countingStatsBackend) CreateEnvironment(context.Context, sandbox.EnvironmentConfig) (*sandbox.EnvironmentHandle, error) {
	return nil, nil
}
func (b *countingStatsBackend) StartEnvironment(context.Context, string, sandbox.EnvironmentConfig) (*sandbox.EnvironmentHandle, error) {
	return nil, nil
}
func (b *countingStatsBackend) StopEnvironment(context.Context, string) error { return nil }
func (b *countingStatsBackend) RestartEnvironmentPorts(context.Context, string, string, sandbox.EnvironmentConfig) (*sandbox.EnvironmentHandle, error) {
	return nil, nil
}
func (b *countingStatsBackend) DeleteEnvironment(context.Context, string, string) error { return nil }
func (b *countingStatsBackend) CopyToEnvironment(context.Context, string, string, io.Reader) error {
	return nil
}
func (b *countingStatsBackend) StreamExecEnvironment(context.Context, string, []string, io.Reader, io.Writer, io.Writer, <-chan sandbox.TermSize) error {
	return nil
}

// ExecEnvironment is invoked here for both of environmentStatsHub.run's
// per-tick calls, distinguished by their one telling substring: the ps
// check (sandbox.EnvironmentHasActiveSSHSession) always contains "ps -eo
// args"; anything else is assumed to be the cgroup stats script
// (sandbox.ReadEnvironmentStats).
func (b *countingStatsBackend) ExecEnvironment(_ context.Context, _ string, cmd []string) (string, int, error) {
	b.execs.Add(1)
	if len(cmd) > 0 && strings.Contains(cmd[len(cmd)-1], "ps -eo args") {
		if b.sshActive.Load() {
			return "1\n", 0, nil
		}
		return "0\n", 0, nil
	}
	return "CPU_USAGE_USEC:100\nCPU_MAX:200000 100000\nMEM_CURRENT:1000\nMEM_MAX:max\nDISK_USED:500\n", 0, nil
}

func testLogger() Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestSubscribeEnvironmentStats_SharesOnePollAcrossSubscribers is the
// regression test for the per-tab `du -sb` multiplication a pullfrog
// review flagged on PR #433: N subscribers to the same environment must
// drive exactly one shared sandbox.ReadEnvironmentStats poll, not N
// independent ones.
func TestSubscribeEnvironmentStats_SharesOnePollAcrossSubscribers(t *testing.T) {
	environmentID := uuid.New()
	backend := &countingStatsBackend{}

	ch1, unsub1 := subscribeEnvironmentStats(environmentID, backend, "container-1", testLogger())
	defer unsub1()
	ch2, unsub2 := subscribeEnvironmentStats(environmentID, backend, "container-1", testLogger())
	defer unsub2()

	msg1 := recvOrFatal(t, ch1)
	msg2 := recvOrFatal(t, ch2)
	if msg1 != msg2 {
		t.Fatalf("subscribers got different readings: %+v vs %+v", msg1, msg2)
	}

	// Both subscribers already got a reading. Each tick makes exactly 2
	// ExecEnvironment calls (sandbox.ReadEnvironmentStats, then
	// sandbox.EnvironmentHasActiveSSHSession — see run()'s send()) — one
	// tick must have served both subscribers, not one tick each.
	const execsPerTick = 2
	if got := backend.execs.Load(); got != execsPerTick {
		t.Fatalf("ExecEnvironment called %d times for 2 subscribers' first reading, want %d (one shared tick)", got, execsPerTick)
	}
}

// TestSubscribeEnvironmentStats_LateSubscriberGetsCachedReading verifies a
// subscriber joining after the hub is already running gets the last
// reading immediately rather than waiting for the next tick — the same
// "not blank for up to 5s" guarantee the old per-connection loop gave
// every connection.
func TestSubscribeEnvironmentStats_LateSubscriberGetsCachedReading(t *testing.T) {
	environmentID := uuid.New()
	backend := &countingStatsBackend{}

	ch1, unsub1 := subscribeEnvironmentStats(environmentID, backend, "container-1", testLogger())
	defer unsub1()
	recvOrFatal(t, ch1)

	ch2, unsub2 := subscribeEnvironmentStats(environmentID, backend, "container-1", testLogger())
	defer unsub2()

	select {
	case <-ch2:
	case <-time.After(time.Second):
		t.Fatal("late subscriber did not receive the cached reading immediately")
	}
}

// TestSubscribeEnvironmentStats_ReportsActiveSSHSession is the regression
// test for the frontend idle ring misleadingly counting down to "sleeps
// in 0m" during a live SSH session (PR #433 review): the pushed message
// must carry an accurate has_active_ssh_session so the frontend has a
// signal to act on.
func TestSubscribeEnvironmentStats_ReportsActiveSSHSession(t *testing.T) {
	environmentID := uuid.New()
	backend := &countingStatsBackend{}
	backend.sshActive.Store(true)

	ch, unsub := subscribeEnvironmentStats(environmentID, backend, "container-1", testLogger())
	defer unsub()

	msg := recvOrFatal(t, ch)
	if !msg.HasActiveSSHSession {
		t.Fatal("HasActiveSSHSession = false, want true with an active sshd session")
	}
}

// TestSubscribeEnvironmentStats_StopsPollingAfterLastUnsubscribe verifies
// the shared loop actually stops once every subscriber has left, instead
// of leaking a goroutine that keeps polling a nobody's-watching
// environment forever.
func TestSubscribeEnvironmentStats_StopsPollingAfterLastUnsubscribe(t *testing.T) {
	environmentID := uuid.New()
	backend := &countingStatsBackend{}

	ch, unsub := subscribeEnvironmentStats(environmentID, backend, "container-1", testLogger())
	recvOrFatal(t, ch)
	unsub()

	execsAtUnsubscribe := backend.execs.Load()
	time.Sleep(50 * time.Millisecond)
	if got := backend.execs.Load(); got != execsAtUnsubscribe {
		t.Fatalf("ExecEnvironment still called after last unsubscribe: %d -> %d", execsAtUnsubscribe, got)
	}

	statsHubsMu.Lock()
	_, stillRegistered := statsHubs[environmentID]
	statsHubsMu.Unlock()
	if stillRegistered {
		t.Fatal("hub still registered in statsHubs after last unsubscribe")
	}
}

func recvOrFatal(t *testing.T, ch <-chan environmentStatsMessage) environmentStatsMessage {
	t.Helper()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for a stats reading")
		return environmentStatsMessage{}
	}
}
