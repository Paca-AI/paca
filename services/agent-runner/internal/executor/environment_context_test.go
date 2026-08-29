package executor

import (
	"strings"
	"testing"

	"github.com/Paca-AI/agent-runner/internal/repository/postgres"
)

func hostPort(p int) *int { return &p }

func TestBuildEnvironmentContext_AlwaysStatesPersistence(t *testing.T) {
	got := buildEnvironmentContext(nil, "", false, false)
	if !strings.Contains(got, "persistent, long-lived") {
		t.Errorf("expected persistence framing regardless of forwards, got:\n%s", got)
	}
}

func TestBuildEnvironmentContext_NoForwardsAtAllTellsAgentNothingIsReachable(t *testing.T) {
	got := buildEnvironmentContext(nil, "example.com", false, false)

	if !strings.Contains(got, "No port forwards are configured yet") {
		t.Errorf("expected the no-forwards fallback, got:\n%s", got)
	}
	if strings.Contains(got, "example.com") {
		t.Errorf("no forwards exist, so no host address should appear, got:\n%s", got)
	}
}

func TestBuildEnvironmentContext_ReachableForwardListedWithLabelAndAddress(t *testing.T) {
	forwards := []*postgres.PortForward{
		{Label: "frontend dev server", ContainerPort: 3000, HostPort: hostPort(31007)},
	}

	got := buildEnvironmentContext(forwards, "example.com", false, false)

	for _, want := range []string{"3000", "frontend dev server", "example.com:31007"} {
		if !strings.Contains(got, want) {
			t.Errorf("message missing %q\n\ngot:\n%s", want, got)
		}
	}
}

func TestBuildEnvironmentContext_NoPortForwardHostConfiguredOmitsAddress(t *testing.T) {
	forwards := []*postgres.PortForward{
		{Label: "backend", ContainerPort: 8080, HostPort: hostPort(31008)},
	}

	got := buildEnvironmentContext(forwards, "", false, false)

	if !strings.Contains(got, "8080") || !strings.Contains(got, "backend") {
		t.Errorf("expected the forward to still be listed by port/label, got:\n%s", got)
	}
	if strings.Contains(got, "31008") {
		t.Errorf("no external hostname is configured, so no host_port address should be printed, got:\n%s", got)
	}
	if !strings.Contains(got, "hasn't configured an external hostname") {
		t.Errorf("expected a fallback explanation for the missing hostname, got:\n%s", got)
	}
}

func TestBuildEnvironmentContext_SkipsForwardWithNilHostPort(t *testing.T) {
	forwards := []*postgres.PortForward{
		{Label: "not assigned yet", ContainerPort: 9000, HostPort: nil},
		{Label: "assigned", ContainerPort: 9001, HostPort: hostPort(31009)},
	}

	got := buildEnvironmentContext(forwards, "example.com", false, false)

	if strings.Contains(got, "9000") || strings.Contains(got, "not assigned yet") {
		t.Errorf("forward with no host_port yet should be skipped entirely, got:\n%s", got)
	}
	if !strings.Contains(got, "9001") {
		t.Errorf("forward with an assigned host_port should still be listed, got:\n%s", got)
	}
}

func TestBuildEnvironmentContext_PendingRestartAddsCaveat(t *testing.T) {
	forwards := []*postgres.PortForward{
		{Label: "frontend", ContainerPort: 3000, HostPort: hostPort(31007)},
	}

	got := buildEnvironmentContext(forwards, "example.com", true, false)

	if !strings.Contains(got, "Restart") {
		t.Errorf("expected a pending-restart caveat mentioning Restart, got:\n%s", got)
	}
}

func TestBuildEnvironmentContext_NoPendingRestartNoCaveat(t *testing.T) {
	forwards := []*postgres.PortForward{
		{Label: "frontend", ContainerPort: 3000, HostPort: hostPort(31007)},
	}

	got := buildEnvironmentContext(forwards, "example.com", false, false)

	if strings.Contains(got, "Restart") {
		t.Errorf("expected no pending-restart caveat when nothing is pending, got:\n%s", got)
	}
}

// TestBuildEnvironmentContext_ForwardsUnknownNeverClaimsNoneConfigured covers
// the gap a stale ListForEnvironment error used to fall into: forwards ==
// nil on a query failure looked identical to "this environment genuinely
// has zero forwards configured" (both produced the same message), so a
// transient DB hiccup would have the agent confidently telling the user
// nothing is reachable when the truth is just "unknown right now". Passing
// non-nil forwards alongside forwardsUnknown=true confirms the unknown
// case truly short-circuits before ever looking at forwards, not just that
// it happens to behave the same on a nil slice.
func TestBuildEnvironmentContext_ForwardsUnknownNeverClaimsNoneConfigured(t *testing.T) {
	forwards := []*postgres.PortForward{
		{Label: "frontend", ContainerPort: 3000, HostPort: hostPort(31007)},
	}

	got := buildEnvironmentContext(forwards, "example.com", false, true)

	if strings.Contains(got, "No port forwards are configured yet") {
		t.Errorf("forwardsUnknown must not claim none are configured, got:\n%s", got)
	}
	if strings.Contains(got, "3000") || strings.Contains(got, "example.com:31007") {
		t.Errorf("forwardsUnknown must not list forwards it couldn't actually confirm, got:\n%s", got)
	}
	if !strings.Contains(got, "couldn't be loaded") {
		t.Errorf("expected an honest couldn't-load message, got:\n%s", got)
	}
}

// TestBuildEnvironmentContext_NoLeadingSeparatorButEndsWithBlankLine guards
// the concatenation this function's own doc comment describes:
// buildInitialMessage's final strings.TrimLeft(b.String(), "\n") means its
// output never starts with a blank line, so this section — always
// prepended before it — must supply the blank-line separator itself, at
// its own end, rather than at its start.
func TestBuildEnvironmentContext_NoLeadingSeparatorButEndsWithBlankLine(t *testing.T) {
	for name, got := range map[string]string{
		"no forwards":       buildEnvironmentContext(nil, "example.com", false, false),
		"forwards, no host": buildEnvironmentContext([]*postgres.PortForward{{Label: "x", ContainerPort: 1, HostPort: hostPort(2)}}, "", false, false),
		"forwards, host, pending restart": buildEnvironmentContext(
			[]*postgres.PortForward{{Label: "x", ContainerPort: 1, HostPort: hostPort(2)}}, "example.com", true, false),
		"forwards unknown": buildEnvironmentContext(nil, "example.com", false, true),
	} {
		t.Run(name, func(t *testing.T) {
			if strings.HasPrefix(got, "\n") {
				t.Errorf("output must not start with a blank line, got:\n%q", got)
			}
			if !strings.HasSuffix(got, "\n\n") {
				t.Errorf("output must end with a blank-line separator (\\n\\n), got:\n%q", got)
			}
		})
	}
}
