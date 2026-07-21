package bundledskills

import (
	"strings"
	"testing"
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
