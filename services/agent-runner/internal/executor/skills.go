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
	"path"
	"sort"
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
// so the caller can skip the CopyToContainer call entirely. A thin
// convenience wrapper over the more general buildFileTar below.
func buildSkillsTar(fileSkills []agent.Skill) (*bytes.Buffer, error) {
	if len(fileSkills) == 0 {
		return nil, nil
	}
	entries := make([]fileEntry, 0, len(fileSkills))
	for _, s := range fileSkills {
		entries = append(entries, fileEntry{RelPath: skillsRelDir + "/" + s.SkillName + "/SKILL.md", Content: s.SkillContent})
	}
	return buildFileTar(entries, nil)
}

// fileEntry is one file to write into a sandbox/environment container,
// relative to the destPath a CopyToContainer/CopyToEnvironment call
// targets. Structurally identical to providercli.FileEntry (that package
// can't depend on this one, nor vice versa, without a cycle) — converted
// at the one call site that needs both, syncProviderCLIConfig.
type fileEntry struct {
	RelPath string
	Content string
}

// buildFileTar renders entries as an in-memory tar archive, with an
// explicit directory entry written for every parent directory any entry's
// RelPath implies (same explicit-directory-entry approach the original,
// skills-only version of this function used — kept rather than relying on
// the extraction side to auto-create missing parent directories, since
// that isn't a documented guarantee of either backend's CopyToContainer/
// CopyToEnvironment implementation). Directories are written shallowest
// first so a nested path's parent always exists before the child entry
// that needs it. Returns a nil buffer for an empty entries slice, so the
// caller can skip the CopyToContainer/CopyToEnvironment call entirely.
// Generalized out of buildSkillsTar's original body so both Goose's own
// .agents/skills layout (above) and every providercli.Adapter's own
// config-file layout (syncProviderCLIConfig) share one tar-writing
// implementation instead of duplicating it.
//
// excludeDirs skips writing a directory header for any path present in it
// — needed by syncProviderCLIConfig, whose entries can sit under a path
// (a providercli.Adapter's HomeDirName(), e.g. ".claude") that's already a
// symlink on disk by the time this tar is uploaded (see that function's
// own comment on the bootstrap step). Docker's tar extraction refuses to
// overwrite a non-directory (the symlink) with a literal directory entry
// from the archive — confirmed directly: omitting this exclusion produced
// a live "cannot overwrite non-directory ... with directory ..." error the
// very first time a Claude Code agent's skill (nested under
// .claude/skills/<name>/SKILL.md, implying a .claude directory header)
// synced against an environment whose ~/.claude bootstrap symlink had
// already been created. Excluding the symlinked path itself is safe and
// sufficient: extraction still creates any DEEPER directory (e.g.
// .claude/skills) correctly, since a regular mkdir one level under an
// existing symlink transparently follows it to the real target — only a
// tar entry whose name exactly matches the symlink's own path conflicts.
// nil is equivalent to an empty set (every other caller, i.e.
// buildSkillsTar, has nothing to exclude).
func buildFileTar(entries []fileEntry, excludeDirs map[string]bool) (*bytes.Buffer, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	dirSet := map[string]struct{}{}
	for _, e := range entries {
		for dir := path.Dir(e.RelPath); dir != "." && dir != "/" && dir != ""; dir = path.Dir(dir) {
			if excludeDirs[dir] {
				continue
			}
			dirSet[dir] = struct{}{}
		}
	}
	dirs := make([]string, 0, len(dirSet))
	for d := range dirSet {
		dirs = append(dirs, d)
	}
	sort.Slice(dirs, func(i, j int) bool {
		return strings.Count(dirs[i], "/") < strings.Count(dirs[j], "/")
	})

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, d := range dirs {
		if err := tw.WriteHeader(&tar.Header{Name: d + "/", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
			return nil, fmt.Errorf("executor: write file tar: dir %s: %w", d, err)
		}
	}
	for _, e := range entries {
		if err := tw.WriteHeader(&tar.Header{
			Name: e.RelPath,
			Mode: 0o644,
			Size: int64(len(e.Content)),
		}); err != nil {
			return nil, fmt.Errorf("executor: write file tar: %s: %w", e.RelPath, err)
		}
		if _, err := tw.Write([]byte(e.Content)); err != nil {
			return nil, fmt.Errorf("executor: write file tar: %s: %w", e.RelPath, err)
		}
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("executor: close file tar: %w", err)
	}
	return &buf, nil
}
