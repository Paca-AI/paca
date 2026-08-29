package executor

import (
	"strings"
	"testing"

	"github.com/Paca-AI/agent-runner/internal/repository/postgres"
)

func hostPort(p int) *int { return &p }

func TestBuildEnvironmentContext_AlwaysStatesPersistence(t *testing.T) {
	got := buildEnvironmentContext(nil, "", false)
	if !strings.Contains(got, "persistent, long-lived") {
		t.Errorf("expected persistence framing regardless of forwards, got:\n%s", got)
	}
}

func TestBuildEnvironmentContext_NoForwardsAtAllTellsAgentNothingIsReachable(t *testing.T) {
	got := buildEnvironmentContext(nil, "example.com", false)

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

	got := buildEnvironmentContext(forwards, "example.com", false)

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

	got := buildEnvironmentContext(forwards, "", false)

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

	got := buildEnvironmentContext(forwards, "example.com", false)

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

	got := buildEnvironmentContext(forwards, "example.com", true)

	if !strings.Contains(got, "Restart") {
		t.Errorf("expected a pending-restart caveat mentioning Restart, got:\n%s", got)
	}
}

func TestBuildEnvironmentContext_NoPendingRestartNoCaveat(t *testing.T) {
	forwards := []*postgres.PortForward{
		{Label: "frontend", ContainerPort: 3000, HostPort: hostPort(31007)},
	}

	got := buildEnvironmentContext(forwards, "example.com", false)

	if strings.Contains(got, "Restart") {
		t.Errorf("expected no pending-restart caveat when nothing is pending, got:\n%s", got)
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
		"no forwards":       buildEnvironmentContext(nil, "example.com", false),
		"forwards, no host": buildEnvironmentContext([]*postgres.PortForward{{Label: "x", ContainerPort: 1, HostPort: hostPort(2)}}, "", false),
		"forwards, host, pending restart": buildEnvironmentContext(
			[]*postgres.PortForward{{Label: "x", ContainerPort: 1, HostPort: hostPort(2)}}, "example.com", true),
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
