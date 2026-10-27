package bundledskills

import "strings"

const compatibilityLine = "compatibility: Requires Paca MCP server." +
	" Run /paca-setup if Paca tools are not available.\n"

const fallbackBlock = "\n---\n\n## If Paca MCP is not connected\n\n" +
	"> Paca MCP tools are not available. Run `/paca-setup` to configure the connection.\n\n" +
	"---\n"

// deriveAgentContent mechanically turns cli-flavor content into its
// agent-flavor equivalent for a skill with no .agent.md file: swaps the
// `compatibility:` frontmatter line for a `triggers:` one (a keyword-trigger
// mechanism Claude Code/Gemini CLI/Cursor have no equivalent of), and drops
// the "not connected" fallback section (which cannot happen in the agent
// flavor, since its MCP server is always auto-configured).
func deriveAgentContent(name, content string) string {
	content = strings.Replace(content, compatibilityLine, "triggers:\n  - /"+name+"\n", 1)
	return strings.Replace(content, fallbackBlock, "\n---\n", 1)
}
