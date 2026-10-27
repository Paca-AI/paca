// Package bundledskills holds Paca's bundled Agent Skills — served over the
// API in two flavors, selected by the caller:
//
//   - "cli"   (default) — the external-CLI skill set installed by
//     scripts/install-paca-skills.sh into Claude Code, Gemini CLI, Cursor,
//     and AGENTS.md. Assumes no Paca-managed context, so it routes through
//     the Paca MCP server and references things like /paca-setup.
//   - "agent" — the in-product skill set fetched by
//     services/agent-runner's bundledskills.Client for every "llm"-type
//     agent conversation, which already has tools/context auto-configured.
//     agent-runner writes every one of these skills (except `paca` itself)
//     to the sandbox as a real SKILL.md, so Goose's own skill feature
//     discovers and loads them on demand via its built-in `load_skill` tool
//     (see executor/skills.go's package doc comment and
//     https://goose-docs.ai/docs/guides/context-engineering/using-skills/).
//     `paca` is the one exception, folded directly into the initial prompt
//     unconditionally instead (see executor/prompt.go's buildInitialMessage):
//     it's the thing that tells the model which specialized skill to load in
//     the first place, and "always applies, regardless of how you were
//     invoked" — a guarantee load_skill's on-demand loading can't give it.
//
// # Authoring
//
// Each skill is a plain Markdown file under content/, embedded into the
// binary at build time (so it is always available, with no runtime file
// access): content/<name>.md is the cli-flavor file, and also the source of
// the agent flavor, which deriveAgentContent derives mechanically. A skill
// whose agent-flavor content genuinely differs adds content/<name>.agent.md
// and sets AgentVariant in catalog.go — today `paca` (routes with
// load_skill instead of slash commands) and `paca-do` (adds the sandbox's
// clone/push/PR section). `paca-setup` is CLIOnly: the in-product agent's MCP
// server is always auto-configured, so it has nothing to set up.
//
// Adding a skill is: write content/<name>.md, add one line to catalog.go.
package bundledskills

import (
	"sort"
	"strings"
)

// Target selects which bundled skill flavor List returns.
const (
	TargetCLI   = "cli"
	TargetAgent = "agent"
)

// Skill is one bundled skill's raw file content, unparsed — callers
// (scripts/install-paca-skills.sh, agent-runner) already have their own logic
// for turning this into what they need, so there is no reason to duplicate any
// of that parsing here in Go.
type Skill struct {
	Name string `json:"name"`
	// Path is this skill's relative path within its flavor's source layout,
	// e.g. "paca-do/SKILL.md" or (the one legacy-format exception, the agent
	// flavor of "paca") "paca.md". Callers that need to reconstruct an
	// on-disk layout must write content to this exact relative path.
	Path string `json:"path"`
	// Content is the full raw file content, including frontmatter if any.
	Content string `json:"content"`
}

// Route is one extra row for the `paca` router skill's "Route to a
// specialized skill" table, contributed by an installed plugin's skill.
type Route struct {
	// Skill is the plugin skill's name.
	Skill string
	// When completes "If the user wants to…".
	When string
}

// routesMarker marks, in a skill's Markdown, the line directly after the
// router table where plugin routes are inserted. It is always removed, so a
// router served with no plugin routes is unchanged.
const routesMarker = "<!-- paca:plugin-routes -->\n"

// List returns every bundled skill for the given flavor
// (TargetCLI/TargetAgent), sorted by name. Unrecognized or empty target
// values fall back to TargetCLI, matching the API's pre-existing default
// (GET /api/v1/skills with no query param at all).
func List(target string) []Skill { return ListWithRoutes(target, nil) }

// ListWithRoutes is List with plugin routes added to the router table. Callers
// pass only routes for skills that are actually being served alongside, so the
// router never points at a skill that is missing.
func ListWithRoutes(target string, routes []Route) []Skill {
	rows := routeRows(target, routes)
	result := make([]Skill, 0, len(skillEntries))
	for _, e := range skillEntries {
		if target == TargetAgent {
			if e.CLIOnly {
				continue
			}
			result = append(result, Skill{Name: e.Name, Path: e.agentPath(), Content: applyRoutes(e.agentContent(), rows)})
			continue
		}
		result = append(result, Skill{Name: e.Name, Path: e.Name + "/SKILL.md", Content: applyRoutes(e.cliContent, rows)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// IsBuiltinName reports whether name matches one of Paca's own bundled
// skills. A plugin declaring a skill with one of these names would collide
// with — and silently shadow, since neither the API's merge nor
// scripts/install-paca-skills.sh's by-name file writes dedupe against this
// list — a skill users and conversations already trust as Paca's own.
// plugindom.SkillsManifest.validate() calls this to reject such a name at
// install/update time, and pluginrt.ListSkills calls it again at serving
// time as a second line of defense.
func IsBuiltinName(name string) bool {
	for _, e := range skillEntries {
		if e.Name == name {
			return true
		}
	}
	return false
}

// routeRows renders routes as router-table rows in the flavor's own routing
// syntax (slash commands for cli, load_skill for agent). Text is flattened to
// one line and stripped of characters that would break the table.
// SkillsManifest.validate already rejects such text at install time; this
// repeats it as defense in depth for manifests stored before that check.
func routeRows(target string, routes []Route) string {
	var b strings.Builder
	for _, r := range routes {
		when := strings.Join(strings.Fields(strings.NewReplacer("|", "/", "`", "'").Replace(r.When)), " ")
		if when == "" || r.Skill == "" {
			continue
		}
		cmd := "`/" + r.Skill + "`"
		if target == TargetAgent {
			cmd = "`load_skill(name: \"" + r.Skill + "\")`"
		}
		b.WriteString("| " + when + " | " + cmd + " |\n")
	}
	return b.String()
}

// applyRoutes replaces the routes marker in content with rows (possibly empty).
func applyRoutes(content, rows string) string {
	return strings.Replace(content, routesMarker, rows, 1)
}
