package agentdom

import (
	"fmt"
	"strings"
)

// ComposeJevDescription builds the criteria description Jev (the AI
// decision API) sees for this agent when picking which agent should handle
// a chat (Auto mode) or whether to assign it a task. It appends the agent's
// provider/model to its user-authored Description at call time — never
// stored merged — so the composed text stays accurate as LLMProvider/
// LLMModel change and Description stays clean to edit.
func (a Agent) ComposeJevDescription() string {
	provider, model := a.LLMProvider, a.LLMModel
	if a.AgentType == AgentTypeACP && a.ACPProvider != nil {
		provider = *a.ACPProvider
	}
	if a.AgentType == AgentTypeProviderCLI && a.CLIProvider != nil {
		provider, model = *a.CLIProvider, a.CLIModel
	}

	var suffix string
	switch {
	case provider != "" && model != "":
		suffix = fmt.Sprintf("Provider: %s · Model: %s", provider, model)
	case provider != "":
		suffix = fmt.Sprintf("Provider: %s", provider)
	}

	desc := strings.TrimSpace(a.Description)
	switch {
	case desc == "" && suffix == "":
		return a.Name
	case desc == "":
		return suffix
	case suffix == "":
		return desc
	default:
		return desc + "\n" + suffix
	}
}
