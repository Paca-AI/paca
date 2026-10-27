package bundledskills

import (
	"embed"
	"fmt"
)

//go:embed content/*.md
var contentFS embed.FS

// skillEntry is one bundled skill. Its Markdown lives in content/; see the
// package doc comment for the file naming rules.
type skillEntry struct {
	Name string
	// AgentVariant means content/<name>.agent.md exists and is used verbatim
	// as the agent flavor instead of deriveAgentContent's output.
	AgentVariant bool
	// AgentPath overrides the default "<name>/SKILL.md" agent-flavor path.
	// Needed only for the one legacy bare-file exception, paca.md.
	AgentPath string
	// CLIOnly means this skill has no agent-flavor equivalent at all.
	CLIOnly bool

	cliContent   string
	variantAgent string
}

func (e skillEntry) agentPath() string {
	if e.AgentPath != "" {
		return e.AgentPath
	}
	return e.Name + "/SKILL.md"
}

func (e skillEntry) agentContent() string {
	if e.AgentVariant {
		return e.variantAgent
	}
	return deriveAgentContent(e.Name, e.cliContent)
}

// skillEntries is the bundled catalog. Order is irrelevant: List sorts.
var skillEntries = loadEntries([]skillEntry{
	{Name: "paca", AgentVariant: true, AgentPath: "paca.md"},
	{Name: "paca-automation"},
	{Name: "paca-board"},
	{Name: "paca-breakdown"},
	{Name: "paca-clarify"},
	{Name: "paca-do", AgentVariant: true},
	{Name: "paca-doc"},
	{Name: "paca-epic"},
	{Name: "paca-estimate"},
	{Name: "paca-feedback"},
	{Name: "paca-prioritize"},
	{Name: "paca-retro"},
	{Name: "paca-review"},
	{Name: "paca-role-policy"},
	{Name: "paca-setup", CLIOnly: true},
	{Name: "paca-sprint"},
	{Name: "paca-team"},
	{Name: "paca-test"},
	{Name: "paca-triage"},
})

// loadEntries reads each entry's embedded Markdown. A missing file is a
// build-time authoring mistake, so it panics at init rather than shipping a
// skill with empty content.
func loadEntries(entries []skillEntry) []skillEntry {
	read := func(name string) string {
		b, err := contentFS.ReadFile("content/" + name)
		if err != nil {
			panic(fmt.Sprintf("bundledskills: %v", err))
		}
		return string(b)
	}
	for i := range entries {
		entries[i].cliContent = read(entries[i].Name + ".md")
		if entries[i].AgentVariant {
			entries[i].variantAgent = read(entries[i].Name + ".agent.md")
		}
	}
	return entries
}
