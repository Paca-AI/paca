// Package provider resolves an ACP agent's provider id (as sent by Paca in
// a start_turn message) into the subprocess command to launch and, where
// applicable, the session mode that disables interactive permission
// prompts. Mirrors the old Python bridge's resolve_acp_command, whose
// built-in provider data came from the OpenHands SDK's own acp_providers
// registry (apps/acp-bridge/.venv/.../openhands/sdk/settings/acp_providers.py
// — kept only as a historical reference now that this package is the
// source of truth).
package provider

import "fmt"

// info is one built-in provider's static metadata.
type info struct {
	// defaultCommand is the subprocess argv used when the caller doesn't
	// (or, for a named built-in, can't) supply its own — see ResolveCommand.
	defaultCommand []string
	// permissionMode is the ACP session mode id that suppresses interactive
	// permission prompts for this provider, requested via session/set_mode
	// right after session/new — best-effort, only sent if the session
	// actually offers it (see acpclient.SessionModeState.Offers). Empty
	// string means this provider has no known bypass mode; the daemon
	// falls back to auto-approving each session/request_permission
	// individually (which happens unconditionally regardless of mode, so
	// nothing hangs either way).
	permissionMode string
}

// registry mirrors ACP_PROVIDERS in the OpenHands SDK reference
// (apps/acp-bridge/.venv/.../openhands/sdk/settings/acp_providers.py) for
// the three built-ins, plus "goose" — not an OpenHands built-in; the old
// Python bridge added it as a local override since `goose acp` already
// speaks ACP correctly as a plain subprocess with nothing provider-specific
// left for a registry entry to add beyond the launch command itself.
//
// The npm package versions pinned here will drift over time (upstream ACP
// adapters release independently of this repo) and need occasional manual
// bumps — the same maintenance burden OpenHands carried for these same
// pins.
var registry = map[string]info{
	"claude-code": {
		defaultCommand: []string{"npx", "-y", "@agentclientprotocol/claude-agent-acp@0.44.0"},
		permissionMode: "bypassPermissions",
	},
	"codex": {
		defaultCommand: []string{"npx", "-y", "@agentclientprotocol/codex-acp@1.1.2"},
		permissionMode: "agent-full-access",
	},
	"gemini-cli": {
		defaultCommand: []string{"npx", "-y", "@google/gemini-cli@0.46.0", "--acp"},
		permissionMode: "default",
	},
	"goose": {
		defaultCommand: []string{"goose", "acp"},
	},
}

// ResolveCommand returns the subprocess argv to launch for acpProvider,
// mirroring the old bridge's resolve_acp_command precedence exactly:
//   - a named built-in's default command always wins, even over an
//     explicitly supplied one (a named provider is a promise about which
//     CLI is running; only "custom" lets the caller override that)
//   - "custom" (or any provider not in the registry) requires an explicit
//     command and returns it verbatim
//   - an empty/unset acpProvider with no explicit command is an error
func ResolveCommand(acpProvider string, explicit []string) ([]string, error) {
	if p, ok := registry[acpProvider]; ok {
		return append([]string(nil), p.defaultCommand...), nil
	}
	if len(explicit) == 0 {
		return nil, fmt.Errorf("no default command for acp_provider=%q; a custom acp_command is required", acpProvider)
	}
	return explicit, nil
}

// PermissionMode returns the session mode id that suppresses interactive
// permission prompts for acpProvider, or "" if none is known — callers
// should treat "" as "don't bother calling session/set_mode" rather than an
// error; auto-approving session/request_permission covers the same need
// regardless.
func PermissionMode(acpProvider string) string {
	return registry[acpProvider].permissionMode
}
