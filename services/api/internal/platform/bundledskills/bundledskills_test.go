package bundledskills

import (
	"regexp"
	"strings"
	"testing"

	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

func TestList_DefaultsToCLI(t *testing.T) {
	if len(List("")) == 0 {
		t.Fatal("expected List(\"\") to return the cli flavor, got none")
	}
	if len(List("bogus")) == 0 {
		t.Fatal("expected List of an unrecognized target to fall back to cli flavor, got none")
	}
	if len(List(TargetCLI)) != len(List("")) {
		t.Fatal("List(\"\") and List(TargetCLI) should be identical")
	}
}

func TestList_SortedAndWellFormed(t *testing.T) {
	for _, target := range []string{TargetCLI, TargetAgent} {
		t.Run(target, func(t *testing.T) {
			skills := List(target)
			if len(skills) == 0 {
				t.Fatal("expected at least one bundled skill")
			}
			for i := 1; i < len(skills); i++ {
				if skills[i-1].Name >= skills[i].Name {
					t.Fatalf("skills not sorted: %q before %q", skills[i-1].Name, skills[i].Name)
				}
			}
			for _, s := range skills {
				if s.Name == "" {
					t.Fatal("skill with empty name")
				}
				if s.Path == "" {
					t.Fatalf("skill %q has empty path", s.Name)
				}
				// Every skill in both flavors declares a frontmatter `name:`
				// field matching its directory name, except the one legacy
				// bare-.md exception (the agent flavor of "paca"), which has
				// no frontmatter at all.
				if s.Path == s.Name+".md" {
					continue
				}
				if !strings.HasPrefix(s.Content, "---\n") {
					t.Errorf("skill %q content does not start with a YAML frontmatter fence", s.Name)
				}
				if !strings.Contains(s.Content, "name: "+s.Name) {
					t.Errorf("skill %q content frontmatter does not contain a matching name field", s.Name)
				}
			}
		})
	}
}

// TestList_CLIOnlySkillHasNoAgentEquivalent guards paca-setup's exclusion
// from the agent flavor — the in-product agent's MCP server is always
// auto-configured, so it has nothing to set up.
func TestList_CLIOnlySkillHasNoAgentEquivalent(t *testing.T) {
	found := false
	for _, s := range List(TargetCLI) {
		if s.Name == "paca-setup" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected paca-setup in the cli flavor")
	}
	for _, s := range List(TargetAgent) {
		if s.Name == "paca-setup" {
			t.Fatal("paca-setup should not appear in the agent flavor")
		}
	}
}

// TestList_DerivedAgentContent guards the mechanical cli->agent transform
// applied to every skill that doesn't hand-author its own AgentContent: the
// `compatibility:` frontmatter line becomes `triggers:`, and the
// "not connected" fallback section is dropped.
func TestList_DerivedAgentContent(t *testing.T) {
	const derivedSkillName = "paca-breakdown"

	var cliContent, agentContent string
	for _, s := range List(TargetCLI) {
		if s.Name == derivedSkillName {
			cliContent = s.Content
		}
	}
	for _, s := range List(TargetAgent) {
		if s.Name == derivedSkillName {
			agentContent = s.Content
		}
	}
	if cliContent == "" || agentContent == "" {
		t.Fatalf("expected %q in both flavors", derivedSkillName)
	}

	if !strings.Contains(cliContent, "compatibility:") {
		t.Errorf("expected cli flavor of %q to have a compatibility: line", derivedSkillName)
	}
	if strings.Contains(agentContent, "compatibility:") {
		t.Errorf("expected agent flavor of %q to have its compatibility: line replaced", derivedSkillName)
	}
	if !strings.Contains(agentContent, "triggers:\n  - /"+derivedSkillName) {
		t.Errorf("expected agent flavor of %q to declare a matching trigger", derivedSkillName)
	}

	if !strings.Contains(cliContent, "If Paca MCP is not connected") {
		t.Errorf("expected cli flavor of %q to have the fallback section", derivedSkillName)
	}
	if strings.Contains(agentContent, "If Paca MCP is not connected") {
		t.Errorf("expected agent flavor of %q to have the fallback section stripped", derivedSkillName)
	}
}

// TestList_HandAuthoredAgentContentDiffersFromCLI guards the two skills
// whose content genuinely diverges by agent type (not just the mechanical
// transform every other skill gets) — paca-do's sandboxed clone/push/PR
// section, and paca's invoke_skill-based routing.
func TestList_HandAuthoredAgentContentDiffersFromCLI(t *testing.T) {
	for _, name := range []string{"paca", "paca-do"} {
		var cliContent, agentContent string
		for _, s := range List(TargetCLI) {
			if s.Name == name {
				cliContent = s.Content
			}
		}
		for _, s := range List(TargetAgent) {
			if s.Name == name {
				agentContent = s.Content
			}
		}
		if cliContent == "" || agentContent == "" {
			t.Fatalf("expected %q in both flavors", name)
		}
		if cliContent == agentContent {
			t.Errorf("expected %q to have hand-authored, differing content per flavor", name)
		}
	}
}

// TestList_PacaAgentFlavorIsLegacyFormat guards the one path exception: the
// agent flavor of "paca" is the bare-.md legacy format (no frontmatter, so
// the OpenHands SDK treats it as always-active), while the cli flavor is a
// standard <name>/SKILL.md.
func TestList_PacaAgentFlavorIsLegacyFormat(t *testing.T) {
	for _, s := range List(TargetCLI) {
		if s.Name == "paca" && s.Path != "paca/SKILL.md" {
			t.Errorf("expected cli paca path %q, got %q", "paca/SKILL.md", s.Path)
		}
	}
	for _, s := range List(TargetAgent) {
		if s.Name == "paca" && s.Path != "paca.md" {
			t.Errorf("expected agent paca path %q, got %q", "paca.md", s.Path)
		}
	}
}

const rolePolicySkillName = "paca-role-policy"

func rolePolicySkill(t *testing.T, target string) Skill {
	t.Helper()
	for _, s := range List(target) {
		if s.Name == rolePolicySkillName {
			return s
		}
	}
	t.Fatalf("expected %q in the %s flavor", rolePolicySkillName, target)
	return Skill{}
}

// TestRolePolicySkill_BothFlavorsAndGuard guards that the draft-only role
// policy skill ships in both flavors, is routed to by the paca router skill,
// and states that it never writes a role.
func TestRolePolicySkill_BothFlavorsAndGuard(t *testing.T) {
	for _, target := range []string{TargetCLI, TargetAgent} {
		s := rolePolicySkill(t, target)
		if s.Path != rolePolicySkillName+"/SKILL.md" {
			t.Errorf("%s: unexpected path %q", target, s.Path)
		}
		if !strings.HasPrefix(s.Content, "---\nname: "+rolePolicySkillName+"\n") {
			t.Errorf("%s: frontmatter must start with the skill name", target)
		}
		if !strings.Contains(s.Content, "description: ") {
			t.Errorf("%s: missing description", target)
		}
		for _, want := range []string{
			"NEVER create, update, attach, assign or delete a role",
			"Do NOT say or imply the role was created",
			"Advanced (JSON)",
			"Do NOT invent actions",
		} {
			if !strings.Contains(s.Content, want) {
				t.Errorf("%s: skill content is missing guard %q", target, want)
			}
		}
	}
	if !strings.Contains(rolePolicySkill(t, TargetAgent).Content, "triggers:\n  - /"+rolePolicySkillName) {
		t.Error("agent flavor should be derived with a matching trigger")
	}
	for _, s := range List(TargetCLI) {
		if s.Name == "paca" && !strings.Contains(s.Content, "/"+rolePolicySkillName) {
			t.Error("cli paca router skill does not route to paca-role-policy")
		}
	}
	for _, s := range List(TargetAgent) {
		if s.Name == "paca" && !strings.Contains(s.Content, `load_skill(name: "`+rolePolicySkillName+`")`) {
			t.Error("agent paca router skill does not route to paca-role-policy")
		}
	}
	if !IsBuiltinName(rolePolicySkillName) {
		t.Error("paca-role-policy should be a reserved builtin name")
	}
}

var (
	quotedActionRe = regexp.MustCompile(`"([a-z][a-z_.]*:[a-z_*]+)"`)
	backtickRe     = regexp.MustCompile("`([a-z][a-z_.]*:[a-z_*]+)`")
	jsonBlockRe    = regexp.MustCompile("(?s)```json\n(.*?)```")
)

// TestRolePolicySkill_ActionsAreReal checks every action the skill mentions
// (in prose or JSON) against the IAM action registry.
func TestRolePolicySkill_ActionsAreReal(t *testing.T) {
	reg := iam.NewRegistry()
	content := rolePolicySkill(t, TargetCLI).Content
	seen := map[string]bool{}
	for _, re := range []*regexp.Regexp{quotedActionRe, backtickRe} {
		for _, m := range re.FindAllStringSubmatch(content, -1) {
			a := m[1]
			if seen[a] || a == "domain:verb" || a == "domain:*" {
				continue // the notation itself, not an action
			}
			seen[a] = true
			if !reg.HasAction(a) {
				t.Errorf("skill mentions %q which is not a registered action", a)
			}
		}
	}
	// Every builtin action should be documented in the skill.
	for _, a := range iam.BuiltinActions() {
		if !strings.Contains(content, "`"+string(a)+"`") {
			t.Errorf("builtin action %q is missing from the skill's action list", a)
		}
	}
}

// TestRolePolicySkill_ExamplesValidate parses every ```json example and runs
// it through the real policy validator.
func TestRolePolicySkill_ExamplesValidate(t *testing.T) {
	reg := iam.NewRegistry()
	schema := iam.NewAttributeSchema()
	blocks := jsonBlockRe.FindAllStringSubmatch(rolePolicySkill(t, TargetCLI).Content, -1)
	if len(blocks) < 5 {
		t.Fatalf("expected at least 5 worked examples, got %d", len(blocks))
	}
	for i, b := range blocks {
		p, err := iam.ParsePolicy([]byte(b[1]))
		if err != nil {
			t.Errorf("example %d does not parse: %v", i+1, err)
			continue
		}
		if issues := iam.Validate(p, reg, schema); len(issues) > 0 {
			t.Errorf("example %d is invalid: %v", i+1, issues)
		}
	}
}

// TestList_EngineeringPractices guards the engineering-practice guidance
// (adapted from mattpocock/skills) folded into the existing skills, in both
// flavors.
func TestList_EngineeringPractices(t *testing.T) {
	want := map[string][]string{
		"paca-do":        {"failing test", "ranked, falsifiable hypotheses", "regression test"},
		"paca-breakdown": {"tracer bullets", "expand → migrate in batches → contract"},
		"paca-clarify":   {"recommended answer", "glossary", "standalone brief"},
		"paca-test":      {"exact reproduction"},
	}
	for _, target := range []string{TargetCLI, TargetAgent} {
		for _, s := range List(target) {
			for _, phrase := range want[s.Name] {
				if !strings.Contains(s.Content, phrase) {
					t.Errorf("%s/%s: missing guidance %q", target, s.Name, phrase)
				}
			}
		}
	}
}

// TestList_PacaFeatureSkills guards the skills built on Paca's own features:
// present in both flavors, routed to by the paca skill, reserved as builtin
// names, and the retired paca-workflow no longer shipped.
func TestList_PacaFeatureSkills(t *testing.T) {
	names := []string{"paca-automation", "paca-review", "paca-triage", "paca-retro", "paca-feedback", "paca-board", "paca-team"}
	for _, target := range []string{TargetCLI, TargetAgent} {
		byName := map[string]Skill{}
		for _, s := range List(target) {
			byName[s.Name] = s
		}
		if _, ok := byName["paca-workflow"]; ok {
			t.Errorf("%s: retired skill paca-workflow is still shipped", target)
		}
		for _, n := range names {
			s, ok := byName[n]
			if !ok {
				t.Errorf("%s: missing skill %q", target, n)
				continue
			}
			if !strings.Contains(s.Content, "description: ") {
				t.Errorf("%s/%s: missing description", target, n)
			}
			route := "/" + n
			if target == TargetAgent {
				route = `load_skill(name: "` + n + `")`
			}
			if !strings.Contains(byName["paca"].Content, route) {
				t.Errorf("%s: paca router does not route to %s", target, n)
			}
		}
	}
	for _, n := range names {
		if !IsBuiltinName(n) {
			t.Errorf("%s should be a reserved builtin name", n)
		}
	}
	if IsBuiltinName("paca-workflow") {
		t.Error("paca-workflow should no longer be reserved")
	}
}

func pacaRouter(t *testing.T, skills []Skill) string {
	t.Helper()
	for _, s := range skills {
		if s.Name == "paca" {
			return s.Content
		}
	}
	t.Fatal("paca router skill not found")
	return ""
}

// TestRouter_NoPluginRoutesLeavesNoTrace guards that, with no plugin routes,
// the router mentions no plugin skill and no marker leaks into the content.
func TestRouter_NoPluginRoutesLeavesNoTrace(t *testing.T) {
	for _, target := range []string{TargetCLI, TargetAgent} {
		c := pacaRouter(t, List(target))
		if strings.Contains(c, "paca:plugin-routes") {
			t.Errorf("%s: routes marker leaked into the router", target)
		}
	}
}

// TestRouter_PluginRoutesJoinTheTable guards that plugin routes become rows of
// the same table (directly after the last bundled row), in each flavor's
// routing syntax, and are flattened so they cannot break the table.
func TestRouter_PluginRoutesJoinTheTable(t *testing.T) {
	routes := []Route{
		{Skill: "paca-bdd-scenarios", When: "Write Given/When/Then scenarios"},
		{Skill: "paca-x", When: "Odd | text with `ticks`\nand a newline"},
	}
	cli := pacaRouter(t, ListWithRoutes(TargetCLI, routes))
	for _, want := range []string{
		"| Write Given/When/Then scenarios | `/paca-bdd-scenarios` |\n",
		"| Odd / text with 'ticks' and a newline | `/paca-x` |\n",
	} {
		if !strings.Contains(cli, want) {
			t.Errorf("cli router missing row %q", want)
		}
	}
	// Directly after the last bundled row: no blank line that would end the table.
	if !strings.Contains(cli, "`/paca-role-policy <what the role should allow>` |\n| Write Given") {
		t.Error("plugin rows are not contiguous with the router table")
	}
	agent := pacaRouter(t, ListWithRoutes(TargetAgent, routes))
	if !strings.Contains(agent, "| Write Given/When/Then scenarios | `load_skill(name: \"paca-bdd-scenarios\")` |\n") {
		t.Error("agent router missing load_skill row")
	}
	if strings.Contains(pacaRouter(t, List(TargetCLI)), "paca-bdd-scenarios") {
		t.Error("List must not mention plugin skills")
	}
}

// TestList_AgentFlavorHasNoCLIOnlyText guards deriveAgentContent: if a content
// file's compatibility line or fallback block drifts, the replace silently
// no-ops and the agent flavor would ship CLI-only instructions.
func TestList_AgentFlavorHasNoCLIOnlyText(t *testing.T) {
	for _, s := range List(TargetAgent) {
		for _, banned := range []string{compatibilityLine, "## If Paca MCP is not connected"} {
			if strings.Contains(s.Content, banned) {
				t.Errorf("agent/%s: contains CLI-only text %q", s.Name, banned)
			}
		}
	}
}
