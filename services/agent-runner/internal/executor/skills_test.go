package executor

import (
	"archive/tar"
	"io"
	"strings"
	"testing"

	"github.com/Paca-AI/agent-runner/internal/agent"
)

func TestHasSkillFrontmatter(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"well formed", "---\nname: paca-do\ndescription: Execute a task\n---\n\nBody.", true},
		{"leading whitespace", "  \n---\nname: paca-do\n---\nBody.", true},
		{"no frontmatter", "You are an expert software engineer.", false},
		{"unterminated fence", "---\nname: paca-do\nBody with no closing fence.", false},
		{"fence with no name field", "---\ndescription: missing a name\n---\nBody.", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasSkillFrontmatter(c.content); got != c.want {
				t.Errorf("hasSkillFrontmatter(%q) = %v, want %v", c.content, got, c.want)
			}
		})
	}
}

func TestPrepareFileSkills_EveryEnabledSkillGoesToFileDelivery(t *testing.T) {
	// Regression guard for the thing that mattered: no skill — regardless of
	// whether it was authored with proper frontmatter — should ever end up
	// anywhere but the single list Goose's native discovery reads from.
	// There is deliberately no second, "fold this one into the prompt
	// instead" list any more.
	skills := []agent.Skill{
		{SkillName: "paca", SkillContent: "---\nname: paca\ndescription: routes to specialized skills\n---\n\nAlways applies.", IsEnabled: true},
		{SkillName: "paca-do", SkillContent: "---\nname: paca-do\ndescription: Execute a task\n---\n\nDo the work.", IsEnabled: true},
		{SkillName: "custom-no-frontmatter", SkillContent: "Just do the thing, no frontmatter here.", IsEnabled: true},
		{SkillName: "disabled-skill", SkillContent: "---\nname: disabled-skill\ndescription: d\n---\nShould not appear anywhere.", IsEnabled: false},
	}

	fileSkills := prepareFileSkills(skills)

	names := skillNames(fileSkills)
	want := map[string]bool{"paca": true, "paca-do": true, "custom-no-frontmatter": true}
	if len(names) != len(want) {
		t.Fatalf("prepareFileSkills = %v, want %v", names, want)
	}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected skill %q in prepareFileSkills output", n)
		}
	}

	for _, s := range fileSkills {
		if s.SkillName == "disabled-skill" {
			t.Error("disabled-skill should not appear in prepareFileSkills output")
		}
		if !hasSkillFrontmatter(s.SkillContent) {
			t.Errorf("skill %q has no valid frontmatter after prepareFileSkills:\n%s", s.SkillName, s.SkillContent)
		}
	}
}

func TestPrepareFileSkills_SynthesizesFrontmatterInsteadOfDroppingBody(t *testing.T) {
	skills := []agent.Skill{
		{SkillName: "custom-no-frontmatter", SkillContent: "Just do the thing, no frontmatter here.", IsEnabled: true},
	}

	fileSkills := prepareFileSkills(skills)

	if len(fileSkills) != 1 {
		t.Fatalf("prepareFileSkills returned %d skills, want 1", len(fileSkills))
	}
	got := fileSkills[0].SkillContent
	if !hasSkillFrontmatter(got) {
		t.Errorf("expected synthesized frontmatter, got:\n%s", got)
	}
	if !strings.Contains(got, "Just do the thing, no frontmatter here.") {
		t.Errorf("expected the original body to survive synthesis, got:\n%s", got)
	}
}

func TestEnsureFrontmatter_LeavesValidFrontmatterUnchanged(t *testing.T) {
	content := "---\nname: paca-do\ndescription: d\n---\n\nDo the work."
	if got := ensureFrontmatter("paca-do", content); got != content {
		t.Errorf("ensureFrontmatter modified already-valid content:\ngot:  %q\nwant: %q", got, content)
	}
}

func skillNames(skills []agent.Skill) []string {
	names := make([]string, len(skills))
	for i, s := range skills {
		names[i] = s.SkillName
	}
	return names
}

func TestBuildSkillsTar_EmptyInputReturnsNilBuffer(t *testing.T) {
	buf, err := buildSkillsTar(nil)
	if err != nil {
		t.Fatalf("buildSkillsTar(nil): %v", err)
	}
	if buf != nil {
		t.Error("expected a nil buffer for no file-eligible skills")
	}
}

func TestBuildSkillsTar_WritesOneSKILLMdPerSkillUnderSkillsRelDir(t *testing.T) {
	skills := []agent.Skill{
		{SkillName: "paca-do", SkillContent: "---\nname: paca-do\ndescription: d\n---\n\nDo the work."},
		{SkillName: "paca-test", SkillContent: "---\nname: paca-test\ndescription: d\n---\n\nTest the work."},
	}

	buf, err := buildSkillsTar(skills)
	if err != nil {
		t.Fatalf("buildSkillsTar: %v", err)
	}
	if buf == nil {
		t.Fatal("expected a non-nil buffer")
	}

	got := map[string]string{}
	tr := tar.NewReader(buf)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read: %v", err)
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		content, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read tar entry %s: %v", hdr.Name, err)
		}
		got[hdr.Name] = string(content)
	}

	for _, s := range skills {
		wantPath := skillsRelDir + "/" + s.SkillName + "/SKILL.md"
		content, ok := got[wantPath]
		if !ok {
			t.Errorf("expected tar entry %q, got entries %v", wantPath, mapKeys(got))
			continue
		}
		if content != s.SkillContent {
			t.Errorf("entry %q content = %q, want %q", wantPath, content, s.SkillContent)
		}
	}
}

func mapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestBuildSkillsTar_SkillNameNotSubstringMismatched(t *testing.T) {
	// Regression guard: a naive path-join without the trailing "/SKILL.md"
	// segmentation could let "paca-do" and "paca-doc" collide or shadow one
	// another under the same directory tree.
	skills := []agent.Skill{
		{SkillName: "paca-do", SkillContent: "---\nname: paca-do\n---\nA"},
		{SkillName: "paca-doc", SkillContent: "---\nname: paca-doc\n---\nB"},
	}
	buf, err := buildSkillsTar(skills)
	if err != nil {
		t.Fatalf("buildSkillsTar: %v", err)
	}

	names := map[string]bool{}
	tr := tar.NewReader(buf)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read: %v", err)
		}
		names[hdr.Name] = true
	}

	if !names[skillsRelDir+"/paca-do/SKILL.md"] || !names[skillsRelDir+"/paca-doc/SKILL.md"] {
		t.Errorf("expected distinct entries for paca-do and paca-doc, got %v", names)
	}
	if !strings.Contains(skillsRelDir, ".agents/skills") {
		t.Fatalf("sanity check on skillsRelDir failed: %q", skillsRelDir)
	}
}
