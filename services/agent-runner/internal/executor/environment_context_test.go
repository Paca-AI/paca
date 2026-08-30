package executor

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Paca-AI/agent-runner/internal/repository/postgres"
)

func hostPort(p int) *int { return &p }

// TestSanitizeForwardLabel_ShortLabelUnchanged confirms the common case —
// a normal, short label like "frontend dev server" — passes through
// byte-for-byte, with no truncation marker appended.
func TestSanitizeForwardLabel_ShortLabelUnchanged(t *testing.T) {
	const label = "frontend dev server"
	if got := sanitizeForwardLabel(label); got != label {
		t.Errorf("sanitizeForwardLabel(%q) = %q, want it unchanged", label, got)
	}
}

// TestSanitizeForwardLabel_LongLabelTruncated guards the actual defense:
// services/api enforces no length limit on a port forward's Label (only
// non-empty), and it flows straight into an LLM-facing context message on
// every turn — a pathologically long label must not be allowed to crowd
// out the rest of that message.
func TestSanitizeForwardLabel_LongLabelTruncated(t *testing.T) {
	long := strings.Repeat("a", maxForwardLabelLen*10)
	got := sanitizeForwardLabel(long)
	if utf8.RuneCountInString(got) > maxForwardLabelLen+1 { // +1 for the appended "…"
		t.Errorf("sanitizeForwardLabel truncated to %d runes, want at most %d", utf8.RuneCountInString(got), maxForwardLabelLen+1)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected truncated label to end with an ellipsis marker, got %q", got)
	}
}

// TestSanitizeForwardLabel_TruncatesOnRunesNotBytes guards against
// splitting a multi-byte UTF-8 label mid-character: a label made entirely
// of 3-byte runes has more bytes than maxForwardLabelLen well before it
// has more runes than maxForwardLabelLen, so a byte-length truncation
// would cut it far too short (and could produce invalid UTF-8) compared to
// a rune-aware one.
func TestSanitizeForwardLabel_TruncatesOnRunesNotBytes(t *testing.T) {
	long := strings.Repeat("環", maxForwardLabelLen*2) // each rune is 3 bytes
	got := sanitizeForwardLabel(long)
	if !utf8.ValidString(got) {
		t.Fatalf("sanitizeForwardLabel produced invalid UTF-8: %q", got)
	}
	wantRunes := maxForwardLabelLen + utf8.RuneCountInString("…")
	if gotRunes := utf8.RuneCountInString(got); gotRunes != wantRunes {
		t.Errorf("sanitizeForwardLabel(long multi-byte label) has %d runes, want %d", gotRunes, wantRunes)
	}
}

// TestBuildEnvironmentContext_TruncatesLongLabel confirms the truncation
// is actually wired into the message buildEnvironmentContext produces, not
// just present as an unused helper.
func TestBuildEnvironmentContext_TruncatesLongLabel(t *testing.T) {
	long := strings.Repeat("x", maxForwardLabelLen*5)
	forwards := []*postgres.PortForward{
		{Label: long, ContainerPort: 3000, HostPort: hostPort(31007)},
	}

	got := buildEnvironmentContext(forwards, "example.com", false, false)

	if strings.Contains(got, long) {
		t.Errorf("expected the long label to be truncated before reaching the agent-facing message, got:\n%s", got)
	}
	if !strings.Contains(got, "…") {
		t.Errorf("expected a truncation marker in the output, got:\n%s", got)
	}
}

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
