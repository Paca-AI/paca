package projectdom

import "strings"

// JevConfigured reports whether this project has its own Jev API key set.
// Every Jev-dependent feature (task auto-fill, auto-assign, the Jev
// automation condition nodes, chat Auto mode) is gated per-project on this
// — there is no instance-wide Jev toggle; each project brings its own key
// (and optionally its own provider, via JevBaseURL/JevModel).
func (p Project) JevConfigured() bool {
	return p.JevAPIKeySecret != ""
}

// ComposeJevDescription builds the criteria description Jev (the AI
// decision API) sees for this member when deciding whether to assign it a
// task in Auto mode. For a human member this is just Description
// (project_members.description); for an agent member it mirrors
// agentdom.Agent.ComposeJevDescription — the agent's own AgentDescription
// with its provider appended at call time, never stored merged — using the
// agent fields already denormalized onto ProjectMember so this doesn't need
// a second fetch per agent candidate.
func (m ProjectMember) ComposeJevDescription() string {
	if !m.IsAgent() {
		if desc := strings.TrimSpace(m.Description); desc != "" {
			return desc
		}
		return m.DisplayName()
	}

	provider := m.AgentLLMProvider
	if m.AgentACPProvider != nil && *m.AgentACPProvider != "" {
		provider = *m.AgentACPProvider
	}
	desc := strings.TrimSpace(m.AgentDescription)
	switch {
	case desc == "" && provider == "":
		return m.DisplayName()
	case desc == "":
		return "Provider: " + provider
	case provider == "":
		return desc
	default:
		return desc + "\nProvider: " + provider
	}
}
