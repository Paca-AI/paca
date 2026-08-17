// skills.go places every one of an agent's enabled skills on the sandbox's
// real filesystem so Goose discovers all of them through its own native
// skill feature (a SKILL.md per skill under .agents/skills/<name>/,
// surfaced to the model as a one-line "name - description" entry in its
// instructions, and pulled into context on demand via Goose's own
// `load_skill` tool) instead of the executor folding any skill's full
// content into the initial prompt — see
// https://goose-docs.ai/docs/guides/context-engineering/using-skills/ and
// crates/goose/src/skills/{mod,client}.rs in block/goose for the mechanism
// this mirrors (verified directly against that source: the "skills"
// platform extension is default_enabled for every session regardless of
// transport, so this works the same over ACP as it does for the interactive
// CLI).
//
// No skill is exempted from this, `paca` included — an earlier version of
// this file folded paca's full content directly into the prompt
// unconditionally instead, on the reasoning that Goose only loads a
// discovered skill's full body when the model chooses to call `load_skill`
// (a probabilistic, on-demand mechanism), while paca's own content says it
// "always applies, regardless of how you were invoked". That guarantee
// still holds, just delivered differently: hints.go's bootstrapInstruction
// tells the model, via the real system prompt (not a user-turn message —
// see hints.go's doc comment on why that distinction turned out to
// matter), to call load_skill(paca) before anything else. That's a short,
// durable pointer rather than paca's full routing table competing for
// attention on equal footing with the rest of the prompt, including the
// agent's own persona instructions when the two disagree — see hints.go's
// buildGooseHints doc comment.
//
// A skill authored without frontmatter (a per-agent custom skill in
// particular — the agent_skills table's "inline" source has no guaranteed
// shape) doesn't fall back to prompt-folding either any more; an even
// earlier version of this file did, on the reasoning that Goose's own
// loader would silently fail to parse and drop a SKILL.md missing the
// `name:` field it requires. prepareFileSkills synthesizes that minimal
// frontmatter instead, since we control every byte written into the
// sandbox's skills directory — there's no reason a skill missing one YAML
// header should ever bypass Goose's own discovery mechanism, the one place
// in this codebase that's supposed to be the only way any skill's content
// reaches the model.
package executor

import (
	"archive/tar"
	"bytes"
	"fmt"
	"strings"

	"github.com/Paca-AI/agent-runner/internal/agent"
)

// skillsRelDir is where Goose discovers skills relative to the ACP
// session's own cwd (sandboxWorkdir). Since sandboxWorkdir IS the "goose"
// user's home directory, writing here also satisfies Goose's separate
// home-rooted global lookup (~/.agents/skills) — both resolve to the same
// path in this container, so every skill only needs writing once.
const skillsRelDir = ".agents/skills"

// hasSkillFrontmatter reports whether content already carries the YAML
// frontmatter Goose's own skill loader requires to discover a SKILL.md (a
// `name:` field — description is optional in practice, defaulting to "" in
// goose's own parser). Every bundled skill
// (services/api/internal/platform/bundledskills) is authored this way
// already; ensureFrontmatter synthesizes one for anything that isn't.
func hasSkillFrontmatter(content string) bool {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "---") {
		return false
	}
	rest := trimmed[3:]
	end := strings.Index(rest, "---")
	if end < 0 {
		return false
	}
	return strings.Contains(rest[:end], "name:")
}

// ensureFrontmatter returns content unchanged if it already has valid
// frontmatter (see hasSkillFrontmatter), otherwise prepends a minimal
// `name:` block derived from name — see the package doc comment on why
// synthesizing this beats falling back to folding the skill into the
// prompt instead.
func ensureFrontmatter(name, content string) string {
	if hasSkillFrontmatter(content) {
		return content
	}
	return "---\nname: " + name + "\n---\n\n" + content
}

// prepareFileSkills filters an agent's skills down to the enabled ones and
// ensures every one has valid frontmatter (see ensureFrontmatter), ready
// for buildSkillsTar — the single list Goose's native discovery gets, with
// no second, folded-into-the-prompt path for anything to fall into.
func prepareFileSkills(skills []agent.Skill) []agent.Skill {
	out := make([]agent.Skill, 0, len(skills))
	for _, s := range skills {
		if !s.IsEnabled {
			continue
		}
		s.SkillContent = ensureFrontmatter(s.SkillName, s.SkillContent)
		out = append(out, s)
	}
	return out
}

// buildSkillsTar renders fileSkills as an in-memory tar archive laid out
// the way Goose's discovery expects: skillsRelDir/<skill-name>/SKILL.md,
// content written through verbatim (it's already a full SKILL.md — see
// prepareFileSkills). Returns a nil buffer if there's nothing to write,
// so the caller can skip the CopyToContainer call entirely.
func buildSkillsTar(fileSkills []agent.Skill) (*bytes.Buffer, error) {
	if len(fileSkills) == 0 {
		return nil, nil
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	writeDir := func(name string) error {
		return tw.WriteHeader(&tar.Header{Name: name + "/", Typeflag: tar.TypeDir, Mode: 0o755})
	}
	if err := writeDir(".agents"); err != nil {
		return nil, fmt.Errorf("executor: write skills tar: %w", err)
	}
	if err := writeDir(skillsRelDir); err != nil {
		return nil, fmt.Errorf("executor: write skills tar: %w", err)
	}

	for _, s := range fileSkills {
		dir := skillsRelDir + "/" + s.SkillName
		if err := writeDir(dir); err != nil {
			return nil, fmt.Errorf("executor: write skills tar: skill %s: %w", s.SkillName, err)
		}
		if err := tw.WriteHeader(&tar.Header{
			Name: dir + "/SKILL.md",
			Mode: 0o644,
			Size: int64(len(s.SkillContent)),
		}); err != nil {
			return nil, fmt.Errorf("executor: write skills tar: skill %s: %w", s.SkillName, err)
		}
		if _, err := tw.Write([]byte(s.SkillContent)); err != nil {
			return nil, fmt.Errorf("executor: write skills tar: skill %s: %w", s.SkillName, err)
		}
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("executor: close skills tar: %w", err)
	}
	return &buf, nil
}
