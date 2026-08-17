// hints.go delivers this conversation's operator-level, must-not-be-
// forgotten instructions — the agent's own system prompt, plus a mandatory
// pointer that guarantees the `paca` meta-skill gets loaded first — via a
// .goosehints file written into the sandbox, instead of folding either into
// buildInitialMessage's one-time first-turn message the way an earlier
// version of this code did.
//
// This is NOT GOOSE_MOIM_MESSAGE_TEXT. An earlier version of this file set
// that env var instead, on the strength of Goose's own documentation (which
// describes it as injected "into every turn" and "more effective than
// system prompt instructions for critical guardrails") and a literal
// `moim_system_prompt_block` Jinja variable found by grepping the pinned
// goose binary's compiled-in strings. Both of those turned out to be true
// but *irrelevant*: a live e2e probe driving a real `goose serve` container
// against a scripted fake LLM (test/e2e's
// TestExecutorRun_SystemPromptDeliveredViaGooseHints, and the throwaway
// investigation that preceded it) showed a unique marker set via
// GOOSE_MOIM_MESSAGE_TEXT reaching neither the system-role message nor the
// user-role message — not on the first turn, not on any turn. Whatever
// gates that Jinja variable being defined, it isn't satisfied by this
// service's ACP transport, and nothing in the docs or the binary's strings
// said so.
//
// .goosehints, by contrast, was directly observed doing exactly what
// https://goose-docs.ai/docs/guides/context-engineering/using-goosehints/
// describes: content dropped in the sandbox's cwd as a plain file shows up
// verbatim in the real system-role message goose sends the model, under a
// "# Additional Instructions" / "### Project Hints" section its own
// default system.md template renders unconditionally — confirmed against
// the same pinned image this service actually runs, not just against
// documentation. Moral, twice-earned in this file's history: verify
// empirically against the pinned binary before trusting either the docs or
// what a binary's compiled-in strings merely suggest is possible.
package executor

import (
	"archive/tar"
	"bytes"
	"fmt"
	"strings"

	"github.com/Paca-AI/agent-runner/internal/agent"
)

// pacaSkillName is the bundled routing meta-skill bootstrapInstruction
// exists to guarantee gets loaded — see its own doc comment.
const pacaSkillName = "paca"

// bootstrapInstruction is a short, mandatory pointer ensuring the `paca`
// meta-skill gets loaded before anything else. paca is delivered exactly
// like every other skill now — a real SKILL.md under the sandbox's
// .agents/skills/ dir, discoverable via Goose's own `load_skill` tool but
// not preloaded (see skills.go's package doc comment) — but its own
// content says it "always applies, regardless of how you were invoked" and
// is the very thing that decides which other skill to load next. Nothing
// else guarantees load_skill(paca) actually happens rather than being left
// to the same on-demand, model-judgement-gated mechanism every other skill
// relies on — an earlier version of this codebase handled that by folding
// paca's full content directly into the prompt unconditionally instead of
// treating it as discoverable at all (see git history), which worked but
// meant paca's content competed for the model's attention on equal footing
// with everything else in that one-time message, including — critically —
// the agent's own persona instructions when the two disagreed (see
// buildGooseHints's doc comment on why the persona has to win that). A
// short pointer in the real system prompt is enough; the routing table
// itself doesn't need to be unconditionally present, only reachable.
const bootstrapInstruction = "Before doing anything else in this conversation — before any other tool call, " +
	"before replying to the user — call `load_skill` with `paca` and follow its instructions. This is " +
	"mandatory for every conversation, regardless of how you were invoked or what the request looks like."

// buildGooseHints assembles the .goosehints content for one conversation:
// the agent's own system prompt, then (only when `paca` is actually among
// fileSkills — never unconditionally, so a deployment or test with no
// bundled skills wired up doesn't tell the model to load something that
// isn't there) bootstrapInstruction. The system prompt comes first and
// un-prefixed so an agent persona that says things like "skip
// skill-routing analysis, go straight to paca-do" reads as the operator's
// own voice, with the paca-loading requirement stated after it as a
// separate, still-mandatory step — not a rule the persona has to fight
// through preamble to override. Returns "" (buildHintsTar's cue to write
// nothing) when there's neither a system prompt nor a paca skill to point
// at.
func buildGooseHints(cfg agent.Config, fileSkills []agent.Skill) string {
	var b strings.Builder
	b.WriteString(cfg.SystemPrompt)
	if hasSkillNamed(fileSkills, pacaSkillName) {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(bootstrapInstruction)
	}
	return b.String()
}

func hasSkillNamed(skills []agent.Skill, name string) bool {
	for _, s := range skills {
		if s.SkillName == name {
			return true
		}
	}
	return false
}

// buildHintsTar renders content as a single-file tar archive containing
// just `.goosehints` at the sandbox cwd's root — where Goose's own hints
// loader looks for project-level hints, per buildGooseHints's doc comment.
// Returns a nil buffer for empty content so the caller can skip the
// CopyToContainer call entirely, the same convention buildSkillsTar uses.
func buildHintsTar(content string) (*bytes.Buffer, error) {
	if content == "" {
		return nil, nil
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name: ".goosehints",
		Mode: 0o644,
		Size: int64(len(content)),
	}); err != nil {
		return nil, fmt.Errorf("executor: write goosehints tar: %w", err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		return nil, fmt.Errorf("executor: write goosehints tar: %w", err)
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("executor: close goosehints tar: %w", err)
	}
	return &buf, nil
}
