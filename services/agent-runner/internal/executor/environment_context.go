package executor

import (
	"fmt"
	"strings"

	"github.com/Paca-AI/agent-runner/internal/repository/postgres"
)

// buildEnvironmentContext returns the "## Static Environment" section
// telling an environment-attached turn's agent it's running inside a
// persistent, long-lived container rather than a disposable
// per-conversation sandbox, plus which of its ports are currently
// forwarded and at what external address — the in-conversation
// counterpart to what a human already sees on the environment's own
// Connect > "Port forwards" tab (see docs/ai-agent/
// environment-management.md's "Port Forwarding" section).
//
// Concatenated onto buildInitialMessage's own output in Run's
// trigger.EnvironmentID != nil case — never folded into buildInitialMessage
// itself. See that function's own doc comment and prompt_test.go's
// TestBuildInitialMessage_IncludesOnlyPerTurnContentNoSystemPromptNoSkills:
// this needs DB-backed environment/port-forward state a plain agent.Trigger
// doesn't carry, a different category of content from the agent.Config
// (system prompt, skills) that guard exists to keep out of that function.
//
// Always returned first in the combined message, so this carries no
// leading blank-line separator of its own — but always ends with one
// ("\n\n"). buildInitialMessage's own final step is
// strings.TrimLeft(b.String(), "\n"), so its output never starts with a
// blank line; without the trailing "\n\n" here, "## Static Environment"
// would run straight into "## Current Project Context" with no break,
// unlike every other section boundary in that message.
//
// forwards should already be scoped to one environment (coldStartEnvironment
// passes what it fetched via portForwardRepo.ListForEnvironment). A forward
// with no HostPort assigned yet is skipped — mirrors environmentPortMappings'
// own skip, since there's nothing to tell the agent about it yet.
//
// portForwardHost is Options.PortForwardHost (PORT_FORWARD_HOST) — purely
// descriptive, empty on a deployment that hasn't configured a reachable
// hostname. A forward is still listed in that case, just without an
// address, so the agent knows it exists.
//
// portsPendingRestart is env.PortsPendingRestart (services/api's own
// ports_pending_restart column): a forward can have a real, non-nil
// HostPort in the database before the running container/Pod actually
// publishes it — coldStartEnvironment's own attach path never itself
// applies a pending port change, only an explicit "Restart" click on the
// environment's Connect page does (Docker can't add a -p binding to an
// already-running container) — so this is the only available signal that
// a listed address might not be live yet.
func buildEnvironmentContext(forwards []*postgres.PortForward, portForwardHost string, portsPendingRestart bool) string {
	var b strings.Builder
	b.WriteString("## Static Environment\n")
	b.WriteString("You are attached to a persistent, long-lived static environment, not a disposable " +
		"per-conversation sandbox: files on disk and any background process you start (e.g. a dev server) " +
		"are still here the next time any conversation attaches to this same environment.\n")

	var reachable []*postgres.PortForward
	for _, pf := range forwards {
		if pf.HostPort == nil {
			continue
		}
		reachable = append(reachable, pf)
	}

	if len(reachable) == 0 {
		b.WriteString("No port forwards are configured yet, so a process you start here is not reachable " +
			"from outside this container. If the user needs to view something you're running (e.g. a dev " +
			"server), tell them which port it's listening on so they can add a port forward for it from " +
			"the environment's Connect page.\n\n")
		return b.String()
	}

	b.WriteString("The following container ports are forwarded to an external address (prefix one with " +
		"`http://` if what's listening there is a plain web server) — tell the user this address if they " +
		"need to reach something you start on one of these ports:\n")
	for _, pf := range reachable {
		if portForwardHost != "" {
			fmt.Fprintf(&b, "- Port %d (%q) → %s:%d\n", pf.ContainerPort, pf.Label, portForwardHost, *pf.HostPort)
		} else {
			fmt.Fprintf(&b, "- Port %d (%q) → forwarded, but this deployment hasn't configured an external "+
				"hostname; ask the user how they reach it\n", pf.ContainerPort, pf.Label)
		}
	}
	if portsPendingRestart {
		b.WriteString("Note: this environment has a port-forward change pending. The addresses above " +
			"reflect what's actually running right now, which may not match what's currently configured — " +
			"if one doesn't answer, or the user just added a forward that isn't listed here, tell them to " +
			"click Restart on the environment's Connect page to apply it.\n")
	}
	b.WriteString("\n")
	return b.String()
}
