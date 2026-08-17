package executor

import (
	"archive/tar"
	"io"
	"strings"
	"testing"

	"github.com/Paca-AI/agent-runner/internal/agent"
)

func TestBuildGooseHints_SystemPromptOnlyWhenPacaAbsent(t *testing.T) {
	cfg := agent.Config{SystemPrompt: "Skip skill-routing analysis and go straight to paca-do."}
	fileSkills := []agent.Skill{{SkillName: "paca-do", SkillContent: "---\nname: paca-do\n---\nDo the work."}}

	got := buildGooseHints(cfg, fileSkills)

	if !strings.Contains(got, cfg.SystemPrompt) {
		t.Errorf("hints missing system prompt:\n%s", got)
	}
	if strings.Contains(got, "load_skill") {
		t.Errorf("hints should not mention load_skill(paca) when paca isn't among fileSkills:\n%s", got)
	}
}

func TestBuildGooseHints_AddsBootstrapWhenPacaPresent(t *testing.T) {
	cfg := agent.Config{SystemPrompt: "You are a test agent."}
	fileSkills := []agent.Skill{
		{SkillName: "paca", SkillContent: "---\nname: paca\n---\nAlways applies."},
		{SkillName: "paca-do", SkillContent: "---\nname: paca-do\n---\nDo the work."},
	}

	got := buildGooseHints(cfg, fileSkills)

	if !strings.Contains(got, cfg.SystemPrompt) {
		t.Errorf("hints missing system prompt:\n%s", got)
	}
	if !strings.Contains(got, bootstrapInstruction) {
		t.Errorf("hints missing the paca bootstrap instruction:\n%s", got)
	}
	if strings.Index(got, cfg.SystemPrompt) > strings.Index(got, bootstrapInstruction) {
		t.Errorf("system prompt should come before the bootstrap instruction:\n%s", got)
	}
}

func TestBuildGooseHints_EmptyWhenNoSystemPromptAndNoPaca(t *testing.T) {
	got := buildGooseHints(agent.Config{}, nil)
	if got != "" {
		t.Errorf("expected empty hints, got:\n%s", got)
	}
}

func TestBuildGooseHints_BootstrapOnlyWhenNoSystemPrompt(t *testing.T) {
	fileSkills := []agent.Skill{{SkillName: "paca", SkillContent: "---\nname: paca\n---\nAlways applies."}}

	got := buildGooseHints(agent.Config{}, fileSkills)

	if got != bootstrapInstruction {
		t.Errorf("got %q, want exactly the bootstrap instruction with no leading separator", got)
	}
}

func TestBuildHintsTar_EmptyContentReturnsNilBuffer(t *testing.T) {
	buf, err := buildHintsTar("")
	if err != nil {
		t.Fatalf("buildHintsTar(\"\"): %v", err)
	}
	if buf != nil {
		t.Errorf("expected a nil buffer for empty content, got %v", buf)
	}
}

func TestBuildHintsTar_WritesDotGoosehintsAtRoot(t *testing.T) {
	buf, err := buildHintsTar("be helpful")
	if err != nil {
		t.Fatalf("buildHintsTar: %v", err)
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
		content, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read tar entry %s: %v", hdr.Name, err)
		}
		got[hdr.Name] = string(content)
	}

	if len(got) != 1 {
		t.Fatalf("tar entries = %v, want exactly [.goosehints]", mapKeys(got))
	}
	if got[".goosehints"] != "be helpful" {
		t.Errorf(".goosehints content = %q, want %q", got[".goosehints"], "be helpful")
	}
}
