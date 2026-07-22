// Package bundledskills holds Paca's bundled Agent Skills — served over the
// API in two flavors, selected by the caller:
//
//   - "cli"   (default) — the external-CLI skill set installed by
//     scripts/install-paca-skills.sh into Claude Code, Gemini CLI, Cursor,
//     and AGENTS.md. Assumes no Paca-managed context, so it routes through
//     the Paca MCP server and references things like /paca-setup.
//   - "agent" — the in-product skill set fetched by services/ai-agent's
//     builder.load_default_skills() for every "llm"-type agent
//     conversation, which already has tools/context auto-configured.
//
// Every skill is authored once, as skillEntry.Content (the cli-flavor
// content). Most skills' agent-flavor content differs only in two
// mechanical ways handled by deriveAgentContent — a frontmatter line, and a
// fallback section irrelevant to a conversation that's always connected —
// so those skills don't set AgentContent at all. A skill sets AgentContent
// explicitly only when its content genuinely differs by agent type, which
// today is exactly two cases: `paca` (the agent flavor uses the SDK's
// invoke_skill tool for routing; the cli flavor suggests slash commands,
// since external CLIs have no equivalent tool) and `paca-do` (the agent
// flavor adds a clone/push/PR workflow section, since the sandboxed agent
// has no local git checkout of its own the way an external CLI session
// already does). `paca-setup` sets CLIOnly — the in-product agent's MCP
// server is always auto-configured, so it has nothing to set up.
//
// Content is hardcoded directly in this file (edit skillEntries below and
// gofmt) rather than read from any skills/ directory at build or run time —
// mirroring how BuiltinSkillTemplates
// (services/api/internal/service/agent/skill_templates.go) hardcodes its
// own skill catalog — so it's always available with no embed/build-context
// wiring, no separate source-of-truth directory to keep in sync, and can't
// be accidentally deleted or corrupted.
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

// Skill is one bundled skill's raw file content, unparsed — both callers
// (scripts/install-paca-skills.sh and services/ai-agent's builder.py)
// already have their own logic for turning this into what they need
// (frontmatter-stripping for the former, the OpenHands SDK's own skill
// loader for the latter), so there is no reason to duplicate any of that
// parsing here in Go.
type Skill struct {
	Name string `json:"name"`
	// Path is this skill's relative path within its flavor's source layout,
	// e.g. "paca-do/SKILL.md" or (the one legacy-format exception, the agent
	// flavor of "paca") "paca.md". Callers that need to reconstruct an
	// on-disk layout matching the original (services/ai-agent, so the
	// OpenHands SDK categorizes it the same way) must write content to this
	// exact relative path, not just "<name>.md" or "<name>/SKILL.md" by
	// default.
	Path string `json:"path"`
	// Content is the full raw file content, including frontmatter if any.
	Content string `json:"content"`
}

// skillEntry is one bundled skill, authored once — see the package doc
// comment for how its two flavors are derived from a single entry.
type skillEntry struct {
	Name string
	// Content is this skill's cli-flavor content (and its only content, for
	// a CLIOnly skill).
	Content string
	// AgentContent, set only when this skill's agent-flavor content
	// genuinely differs rather than being mechanically derivable from
	// Content, is used verbatim instead of deriveAgentContent's output.
	AgentContent string
	// AgentPath overrides the default "<name>/SKILL.md" agent-flavor path.
	// Needed only for the one legacy bare-file exception, paca.md.
	AgentPath string
	// CLIOnly means this skill has no agent-flavor equivalent at all.
	CLIOnly bool
}

const compatibilityLine = "compatibility: Requires Paca MCP server." +
	" Run /paca-setup if Paca tools are not available.\n"

const fallbackBlock = "\n---\n\n## If Paca MCP is not connected\n\n" +
	"> Paca MCP tools are not available. Run `/paca-setup` to configure the connection.\n\n" +
	"---\n"

// deriveAgentContent mechanically turns cli-flavor content into its
// agent-flavor equivalent for a skill with no explicit AgentContent: swaps
// the `compatibility:` frontmatter line for a `triggers:` one (the
// OpenHands SDK's own keyword-trigger mechanism, which Claude Code/Gemini
// CLI/Cursor have no equivalent of), and drops the "not connected" fallback
// section (which cannot happen in the agent flavor, since its MCP server is
// always auto-configured).
func deriveAgentContent(name, content string) string {
	content = strings.Replace(content, compatibilityLine, "triggers:\n  - /"+name+"\n", 1)
	return strings.Replace(content, fallbackBlock, "\n---\n", 1)
}

// List returns every bundled skill for the given flavor
// (TargetCLI/TargetAgent), sorted by name. Unrecognized or empty target
// values fall back to TargetCLI, matching the API's pre-existing default
// (GET /api/v1/skills with no query param at all).
func List(target string) []Skill {
	result := make([]Skill, 0, len(skillEntries))
	for _, e := range skillEntries {
		if target == TargetAgent {
			if e.CLIOnly {
				continue
			}
			path := e.AgentPath
			if path == "" {
				path = e.Name + "/SKILL.md"
			}
			content := e.AgentContent
			if content == "" {
				content = deriveAgentContent(e.Name, e.Content)
			}
			result = append(result, Skill{Name: e.Name, Path: path, Content: content})
			continue
		}
		result = append(result, Skill{Name: e.Name, Path: e.Name + "/SKILL.md", Content: e.Content})
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

var skillEntries = []skillEntry{
	{
		Name: "paca",
		Content: `---
name: paca
description: Interact with Paca project management using MCP tools. Use when tracking tasks, writing docs, planning sprints, managing work items, creating bugs or features, viewing the board, or handling any project-management request involving Paca. Routes to specialized skills for complex workflows like epics, sprint planning, and task execution.
compatibility: Requires Paca MCP server. Run /paca-setup if Paca tools are not available.
---

You have Paca MCP tools. Handle the request by using those tools directly — never create local files for tasks, docs, or to-do lists.

---

## Step 0 — Route to a specialized skill if appropriate

If the request clearly matches one of these, let the user know — they'll get a better result with the specialized skill:

| If the user wants to... | Suggest |
|---|---|
| Turn requirements into a full epic with child stories | ` + "`" + `/paca-epic <requirements>` + "`" + ` |
| Clarify or improve a vague task or spec | ` + "`" + `/paca-clarify #<number>` + "`" + ` |
| Break a task into smaller sub-tasks | ` + "`" + `/paca-breakdown #<number>` + "`" + ` |
| Plan a sprint from the backlog | ` + "`" + `/paca-sprint` + "`" + ` |
| Estimate story points for tasks | ` + "`" + `/paca-estimate #<number>` + "`" + ` |
| Set priorities across the backlog | ` + "`" + `/paca-prioritize` + "`" + ` |
| Execute a task end-to-end | ` + "`" + `/paca-do #<number>` + "`" + ` |
| Test or verify a task | ` + "`" + `/paca-test #<number>` + "`" + ` |
| Write or update documentation | ` + "`" + `/paca-doc #<number>` + "`" + ` |
| Automate a process — auto-assignment, status chaining, task dependencies | ` + "`" + `/paca-workflow <goal>` + "`" + ` |

If it's a simple or mixed request (e.g. "create a task for X", "what's in the sprint", "mark #7 done"), just handle it directly below.

---

## Step 1 — Scan for a task reference

Scan the user's message for any of these patterns, wherever they appear:

| Pattern | Example | How to resolve |
|---|---|---|
| ` + "`" + `#<number>` + "`" + ` or number in task context | ` + "`" + `#42` + "`" + `, ` + "`" + `close #7` + "`" + `, ` + "`" + `task 42 is done` + "`" + ` | ` + "`" + `get_task_by_number(projectId, 42)` + "`" + ` |
| ` + "`" + `PREFIX-<number>` + "`" + ` | ` + "`" + `ABC-42` + "`" + `, ` + "`" + `PAC-7` + "`" + ` | ` + "`" + `list_projects` + "`" + ` → match ` + "`" + `task_id_prefix` + "`" + ` → ` + "`" + `get_task_by_number` + "`" + ` |
| Paca URL | ` + "`" + `http://…/projects/{id}/tasks/{id}` + "`" + ` | parse both IDs → ` + "`" + `get_task(projectId, taskId)` + "`" + ` |
| UUID | ` + "`" + `550e8400-e29b-41d4-a716-446655440000` + "`" + ` | ` + "`" + `get_task(projectId, uuid)` + "`" + ` |

If a reference is found, fetch that task first, then apply the action the user is asking for.

## Step 2 — Infer the action

| What the user wants | Tools to use |
|---|---|
| Track work — bug, feature, to-do, ticket, chore | ` + "`" + `create_task` + "`" + ` / ` + "`" + `update_task` + "`" + ` / ` + "`" + `list_tasks` + "`" + ` |
| Write content — guide, spec, design, BDD, SDD, notes | ` + "`" + `write_doc` + "`" + ` |
| See status — board, sprint, what's in progress | ` + "`" + `list_sprints` + "`" + ` + ` + "`" + `list_tasks` + "`" + ` |
| Plan an iteration — sprint, milestone | ` + "`" + `create_sprint` + "`" + ` / ` + "`" + `update_sprint` + "`" + ` |
| Comment or annotate an existing task | ` + "`" + `add_task_comment` + "`" + ` |
| Close / complete work | ` + "`" + `update_task` + "`" + ` (set to done status) |
| Break work into pieces | ` + "`" + `create_task` + "`" + ` × N, each referencing the parent |
| Write or update documentation | ` + "`" + `write_doc` + "`" + ` |

## Step 3 — Get the project if needed

If no project is in context, call ` + "`" + `list_projects` + "`" + ` and pick the most relevant one based on the message. Only ask the user if it is genuinely ambiguous between two equally plausible candidates.

## Step 4 — Act and confirm

Execute the tool call(s), then report back: task/doc number, title, and any relevant ID or link.

---

## Examples

| User message | What you do |
|---|---|
| ` + "`" + `fix login redirect bug` + "`" + ` | ` + "`" + `create_task` + "`" + ` titled "Fix login redirect bug" |
| ` + "`" + `write the API authentication design doc` + "`" + ` | ` + "`" + `write_doc` + "`" + ` with a structured draft |
| ` + "`" + `do this task ABC-123` + "`" + ` | find project "ABC" → ` + "`" + `get_task_by_number(123)` + "`" + ` → start/act on task |
| ` + "`" + `close #42` + "`" + ` | ` + "`" + `get_task_by_number(42)` + "`" + ` → ` + "`" + `update_task` + "`" + ` status: done |
| ` + "`" + `I finished PAC-99, mark it done` + "`" + ` | ` + "`" + `update_task` + "`" + ` #99 → status: done |
| ` + "`" + `http://…/tasks/uuid — add comment: blocked` + "`" + ` | ` + "`" + `get_task` + "`" + ` from URL → ` + "`" + `add_task_comment` + "`" + ` "blocked" |
| ` + "`" + `what's in the current sprint?` + "`" + ` | ` + "`" + `list_sprints` + "`" + ` → ` + "`" + `list_tasks` + "`" + ` filtered to active sprint |
| ` + "`" + `review PR, update staging, write release notes` + "`" + ` | ` + "`" + `create_task` + "`" + ` × 3, one per item |

---

## If Paca MCP is not connected

> Paca MCP tools are not available. Run ` + "`" + `/paca-setup` + "`" + ` to configure the connection.

Do not create local files as a fallback.

---

## Tool reference

**Tasks:** ` + "`" + `create_task` + "`" + ` · ` + "`" + `list_tasks` + "`" + ` · ` + "`" + `get_task` + "`" + ` · ` + "`" + `get_task_by_number` + "`" + ` · ` + "`" + `update_task` + "`" + ` · ` + "`" + `delete_task` + "`" + `  
**Documents:** ` + "`" + `list_docs` + "`" + ` · ` + "`" + `read_doc` + "`" + ` · ` + "`" + `write_doc` + "`" + ` · ` + "`" + `delete_doc` + "`" + `  
**Sprints:** ` + "`" + `create_sprint` + "`" + ` · ` + "`" + `list_sprints` + "`" + ` · ` + "`" + `get_sprint` + "`" + ` · ` + "`" + `update_sprint` + "`" + ` · ` + "`" + `complete_sprint` + "`" + `  
**Projects:** ` + "`" + `list_projects` + "`" + ` · ` + "`" + `get_project` + "`" + ` · ` + "`" + `create_project` + "`" + ` · ` + "`" + `update_project` + "`" + `  
**Comments:** ` + "`" + `add_task_comment` + "`" + ` · ` + "`" + `update_task_comment` + "`" + ` · ` + "`" + `list_task_activities` + "`" + `
`,
		AgentContent: `---
name: paca
description: Interact with Paca project management using MCP tools. Use when tracking tasks, writing docs, planning sprints, managing work items, creating bugs or features, viewing the board, or handling any project-management request involving Paca. Routes to specialized skills for complex workflows like epics, sprint planning, and task execution.
---

You have Paca MCP tools. Handle the request by using those tools directly — never create local files for tasks, docs, or to-do lists.

This is your default operating procedure for every conversation — it always applies, regardless of how you were invoked (task assignment, a comment mention, direct chat, or a description-write request).

---

## Step 0 — Invoke a specialized skill BEFORE doing any work

**This is a hard requirement, not a suggestion.** Before making any status change, calling any implementation tool, or starting any substantive work, you MUST call ` + "`" + `invoke_skill(name="<skill-name>")` + "`" + ` to load the skill's full instructions and follow them step by step. Reconstructing a skill from memory leads to incomplete or incorrect results.

| If the user wants to... | Invoke this skill |
|---|---|
| Turn requirements into a full epic with child stories | ` + "`" + `invoke_skill(name="paca-epic")` + "`" + ` |
| Clarify or improve a vague task or spec | ` + "`" + `invoke_skill(name="paca-clarify")` + "`" + ` |
| Break a task into smaller sub-tasks | ` + "`" + `invoke_skill(name="paca-breakdown")` + "`" + ` |
| Plan a sprint from the backlog | ` + "`" + `invoke_skill(name="paca-sprint")` + "`" + ` |
| Estimate story points for tasks | ` + "`" + `invoke_skill(name="paca-estimate")` + "`" + ` |
| Set priorities across the backlog | ` + "`" + `invoke_skill(name="paca-prioritize")` + "`" + ` |
| Execute a task end-to-end | ` + "`" + `invoke_skill(name="paca-do")` + "`" + ` |
| Test or verify a task | ` + "`" + `invoke_skill(name="paca-test")` + "`" + ` |
| Write or update documentation | ` + "`" + `invoke_skill(name="paca-doc")` + "`" + ` |
| Automate a process — auto-assignment, status chaining, task dependencies | ` + "`" + `invoke_skill(name="paca-workflow")` + "`" + ` |

A user can also force one of these directly by typing ` + "`" + `/<skill-name>` + "`" + ` (e.g. ` + "`" + `/paca-do #42` + "`" + `) in a chat message or comment.

**The ONLY exception** to invoking a skill: A single trivial action with zero judgment — closing a task when explicitly told "close this", adding a plain comment like "noted", or checking what's in the sprint. If the request involves implementation, planning, analysis, breakdown, estimation, testing, or documentation, you MUST invoke the matching skill first via ` + "`" + `invoke_skill()` + "`" + `.

## Step 0.5 — When a task is in scope, let its status guide you

Whenever your invocation is tied to a specific task (an assignment, a comment on a task, or a description-write request), load it first (` + "`" + `get_task` + "`" + ` / ` + "`" + `get_task_by_number` + "`" + `) and let its **current status** — not just the request wording — help you pick the right specialized skill to invoke:

| Task status | Invoke this skill |
|---|---|
| No acceptance criteria, or description is thin | ` + "`" + `invoke_skill(name="paca-clarify")` + "`" + ` |
| Backlog / not yet sized or split | ` + "`" + `invoke_skill(name="paca-breakdown")` + "`" + ` (if large) or ` + "`" + `invoke_skill(name="paca-estimate")` + "`" + ` (if right-sized) |
| To do / ready, sprint not yet planned | ` + "`" + `invoke_skill(name="paca-sprint")` + "`" + ` |
| In progress | ` + "`" + `invoke_skill(name="paca-do")` + "`" + ` |
| In review / awaiting QA | ` + "`" + `invoke_skill(name="paca-test")` + "`" + ` |
| Done, but the feature has no linked documentation | ` + "`" + `invoke_skill(name="paca-doc")` + "`" + ` |

This table picks *which* skill to invoke — it never excuses you from invoking one (see Step 0). If the requester's message explicitly asks for something narrower than the status implies (e.g. "just estimate this" on an in-progress task), honor that instead — but still invoke the skill that matches the explicit ask (` + "`" + `invoke_skill(name="paca-estimate")` + "`" + `), rather than doing it ad hoc. The invoke_skill call must happen BEFORE any other tool calls related to the work.

## Step 1 — Scan for a task reference

Scan the user's message for any of these patterns, wherever they appear:

| Pattern | Example | How to resolve |
|---|---|---|
| ` + "`" + `#<number>` + "`" + ` or number in task context | ` + "`" + `#42` + "`" + `, ` + "`" + `close #7` + "`" + `, ` + "`" + `task 42 is done` + "`" + ` | ` + "`" + `get_task_by_number(projectId, 42)` + "`" + ` |
| ` + "`" + `PREFIX-<number>` + "`" + ` | ` + "`" + `ABC-42` + "`" + `, ` + "`" + `PAC-7` + "`" + ` | ` + "`" + `list_projects` + "`" + ` → match ` + "`" + `task_id_prefix` + "`" + ` → ` + "`" + `get_task_by_number` + "`" + ` |
| Paca URL | ` + "`" + `http://…/projects/{id}/tasks/{id}` + "`" + ` | parse both IDs → ` + "`" + `get_task(projectId, taskId)` + "`" + ` |
| UUID | ` + "`" + `550e8400-e29b-41d4-a716-446655440000` + "`" + ` | ` + "`" + `get_task(projectId, uuid)` + "`" + ` |

If a reference is found, fetch that task first, then apply the action the user is asking for.

## Step 2 — Infer the action

| What the user wants | Tools to use |
|---|---|
| Track work — bug, feature, to-do, ticket, chore | ` + "`" + `create_task` + "`" + ` / ` + "`" + `update_task` + "`" + ` / ` + "`" + `list_tasks` + "`" + ` |
| Write content — guide, spec, design, BDD, SDD, notes | ` + "`" + `write_doc` + "`" + ` |
| See status — board, sprint, what's in progress | ` + "`" + `list_sprints` + "`" + ` + ` + "`" + `list_tasks` + "`" + ` |
| Plan an iteration — sprint, milestone | ` + "`" + `create_sprint` + "`" + ` / ` + "`" + `update_sprint` + "`" + ` |
| Comment or annotate an existing task | ` + "`" + `add_task_comment` + "`" + ` |
| Close / complete work | ` + "`" + `update_task` + "`" + ` (set to done status) |
| Break work into pieces | ` + "`" + `create_task` + "`" + ` × N, each referencing the parent |
| Write or update documentation | ` + "`" + `write_doc` + "`" + ` (path decides create vs. update) |

## Step 3 — Get the project if needed

If no project is in context, call ` + "`" + `list_projects` + "`" + ` and pick the most relevant one based on the message. Only ask the user if it is genuinely ambiguous between two equally plausible candidates.

## Step 4 — Act and confirm

Execute the tool call(s), then report back: task/doc number, title, and any relevant ID or link.

## Asking for more information

Whenever you get stuck without enough information to proceed — regardless of how you were invoked — **do not just say so in your conversational reply and stop there**. The requester almost never reopens the agent conversation log, and even in direct chat, they may never come back to that exact session. Whichever surface applies, add a comment, then still say something brief in the conversational reply too (e.g. "Asked for clarification — see the comment on #42") — but the comment is what actually reaches the person.

**For a task** (assignment, task comment, description-write, or a task referenced in chat):
1. Call ` + "`" + `add_task_comment` + "`" + ` with your question(s) on the task itself.
2. Include a literal ` + "`" + `@username` + "`" + ` in the comment text (their login username — letters, digits, ` + "`" + `.` + "`" + `, ` + "`" + `_` + "`" + `, ` + "`" + `-` + "`" + ` only — not their display name, and not a ` + "`" + `#` + "`" + `-style task/doc reference). This is parsed as a real mention and sends an in-app notification. Find the right username with ` + "`" + `list_project_members` + "`" + `, matching against whatever name/context you already have (e.g. the commenter's name from ` + "`" + `list_task_activities` + "`" + `, or the task's assignee).
3. Stop there — don't guess and proceed past a genuine blocker. Once they reply (typically by commenting back and mentioning you), a new conversation will pick up from where you left off; check ` + "`" + `list_task_activities` + "`" + ` for that history.

**For a document** (doc comment, or a doc referenced in chat, with no task involved):
- ` + "`" + `add_doc_comment` + "`" + ` exists, but **` + "`" + `@username` + "`" + ` there does not send a notification** (unlike task comments) — the person only sees it if they happen to reopen the document. Use it anyway if there's genuinely no task to fall back on, but call it out as unreliable in your conversational reply.
- If the document is linked to a task, ask there instead with ` + "`" + `add_task_comment` + "`" + ` — that's the reliable path.

**In direct chat** with no task or document in scope: the chat reply is the only channel available — ask there and stop, there's nowhere else to put it.

---

## Examples

| User message | What you do |
|---|---|
| ` + "`" + `fix login redirect bug` + "`" + ` | ` + "`" + `create_task` + "`" + ` titled "Fix login redirect bug" |
| ` + "`" + `write the API authentication design doc` + "`" + ` | ` + "`" + `write_doc` + "`" + ` with a structured draft |
| ` + "`" + `do this task ABC-123` + "`" + ` | find project "ABC" → ` + "`" + `get_task_by_number(123)` + "`" + ` → start/act on task |
| ` + "`" + `close #42` + "`" + ` | ` + "`" + `get_task_by_number(42)` + "`" + ` → ` + "`" + `update_task` + "`" + ` status: done |
| ` + "`" + `I finished PAC-99, mark it done` + "`" + ` | ` + "`" + `update_task` + "`" + ` #99 → status: done |
| ` + "`" + `http://…/tasks/uuid — add comment: blocked` + "`" + ` | ` + "`" + `get_task` + "`" + ` from URL → ` + "`" + `add_task_comment` + "`" + ` "blocked" |
| ` + "`" + `what's in the current sprint?` + "`" + ` | ` + "`" + `list_sprints` + "`" + ` → ` + "`" + `list_tasks` + "`" + ` filtered to active sprint |
| ` + "`" + `review PR, update staging, write release notes` + "`" + ` | ` + "`" + `create_task` + "`" + ` × 3, one per item |

---

## Tool reference

**Tasks:** ` + "`" + `create_task` + "`" + ` · ` + "`" + `list_tasks` + "`" + ` · ` + "`" + `get_task` + "`" + ` · ` + "`" + `get_task_by_number` + "`" + ` · ` + "`" + `update_task` + "`" + ` · ` + "`" + `delete_task` + "`" + `
**Documents:** ` + "`" + `list_docs` + "`" + ` · ` + "`" + `read_doc` + "`" + ` · ` + "`" + `write_doc` + "`" + ` · ` + "`" + `move_doc` + "`" + ` · ` + "`" + `delete_doc` + "`" + `
**Sprints:** ` + "`" + `create_sprint` + "`" + ` · ` + "`" + `list_sprints` + "`" + ` · ` + "`" + `get_sprint` + "`" + ` · ` + "`" + `update_sprint` + "`" + ` · ` + "`" + `complete_sprint` + "`" + `
**Projects:** ` + "`" + `list_projects` + "`" + ` · ` + "`" + `get_project` + "`" + ` · ` + "`" + `create_project` + "`" + ` · ` + "`" + `update_project` + "`" + `
**Comments:** ` + "`" + `add_task_comment` + "`" + ` · ` + "`" + `update_task_comment` + "`" + ` · ` + "`" + `list_task_activities` + "`" + ` · ` + "`" + `add_doc_comment` + "`" + ` · ` + "`" + `update_doc_comment` + "`" + ` · ` + "`" + `list_doc_activities` + "`" + `
`,
		AgentPath: "paca.md",
	},
	{
		Name: "paca-breakdown",
		Content: `---
name: paca-breakdown
description: Break a large Paca task or epic into smaller, actionable sub-tasks with dependency ordering. Use when decomposing work that is too large to estimate or execute in a single session, when creating an implementation plan, or when a task needs to be split into vertical slices before sprint planning.
compatibility: Requires Paca MCP server. Run /paca-setup if Paca tools are not available.
---

You are breaking a task or epic into smaller, actionable sub-tasks in Paca. Use Paca MCP tools throughout — never create local files.

**If no task is specified**, call ` + "`" + `list_tasks` + "`" + ` filtered to the current sprint or backlog and show the user the largest or most complex unbroken tasks as candidates.

---

## Step 1 — Load project context

1. Resolve the task reference from the user's message using ` + "`" + `get_task_by_number` + "`" + ` or ` + "`" + `get_task` + "`" + `.
2. Call ` + "`" + `list_docs` + "`" + ` and read documents that explain the technical landscape — architecture, design decisions, BDD scenarios, API specs. Understanding the tech stack and boundaries is essential to find the right split points.
3. Call ` + "`" + `list_task_types` + "`" + ` and ` + "`" + `list_task_statuses` + "`" + ` to know available types and statuses.
4. Call ` + "`" + `list_tasks` + "`" + ` to see what already exists, so you don't propose sub-tasks that duplicate open work.

## Step 2 — Analyse the task

Read the task title, description, and acceptance criteria. Identify:
- Technical layers involved (frontend, backend, database, infra, tests, docs)
- External dependencies or blockers to flag
- Natural vertical slices (end-to-end thin features) vs. horizontal layers — **prefer vertical slices**
- Any parts that are risky or uncertain and should be isolated as their own sub-task (unknown = own task)

## Step 3 — Propose the breakdown

Present a numbered list of proposed sub-tasks. For each:
- **Title** — clear, imperative ("Add rate-limiting middleware", not "Rate limiting")
- **Size** — fits in 1–2 days
- **Done condition** — one sentence on what "done" looks like
- **Depends on** — note any sub-tasks that must be completed first (e.g. "Depends on #3 above")

Ask the user to confirm, adjust, or remove items before creating anything. This is especially important if the list has more than 6 items.

## Step 4 — Create sub-tasks

For each confirmed sub-task, call ` + "`" + `create_task` + "`" + `:
- Reference the parent in the description: ` + "`" + `Part of #<parent-number>` + "`" + `
- Include the done condition as the acceptance criteria
- Note any dependencies: ` + "`" + `Blocked by #<sibling-number>` + "`" + ` if one sub-task must precede another
- Assign to the same sprint as the parent if one exists (check with ` + "`" + `list_sprints` + "`" + `)

**What's next:** Consider running ` + "`" + `/paca-estimate` + "`" + ` on the newly created sub-tasks to add story points.

Report back: parent task number, list of created sub-task numbers, titles, and any dependency chain noted.

---

## If Paca MCP is not connected

> Paca MCP tools are not available. Run ` + "`" + `/paca-setup` + "`" + ` to configure the connection.

---

## Tool reference

**Tasks:** ` + "`" + `get_task` + "`" + ` · ` + "`" + `get_task_by_number` + "`" + ` · ` + "`" + `list_tasks` + "`" + ` · ` + "`" + `create_task` + "`" + ` · ` + "`" + `update_task` + "`" + ` · ` + "`" + `list_task_types` + "`" + ` · ` + "`" + `list_task_statuses` + "`" + `  
**Documents:** ` + "`" + `list_docs` + "`" + ` · ` + "`" + `read_doc` + "`" + `  
**Sprints:** ` + "`" + `list_sprints` + "`" + `  
**Projects:** ` + "`" + `list_projects` + "`" + `
`,
	},
	{
		Name: "paca-clarify",
		Content: `---
name: paca-clarify
description: Clarify a vague or incomplete Paca task or specification by identifying ambiguities, asking targeted questions, and rewriting the description with explicit acceptance criteria. Use when a task is unclear, missing edge cases, lacks a testable done condition, or when someone asks to improve or flesh out a spec.
compatibility: Requires Paca MCP server. Run /paca-setup if Paca tools are not available.
---

You are clarifying a task or specification in Paca. Use Paca MCP tools throughout — never create local files.

**If no task is specified**, call ` + "`" + `list_tasks` + "`" + ` and surface tasks that have no acceptance criteria or have a very short description — those are the best candidates for clarification. Present them and ask which to work on.

---

## Step 1 — Load project context

1. Resolve the target from the user's message:
   - ` + "`" + `#42` + "`" + ` or ` + "`" + `ABC-42` + "`" + ` → ` + "`" + `get_task_by_number` + "`" + `
   - Paca task URL → parse the task ID → ` + "`" + `get_task` + "`" + `
   - Doc title / keyword / path → ` + "`" + `list_docs` + "`" + ` → ` + "`" + `read_doc` + "`" + `
   - **If the task or document is not found**, tell the user clearly ("Task #99 was not found in project X") and ask them to verify the reference.
2. Call ` + "`" + `list_docs` + "`" + ` and read documents that provide context for this task — requirements, architecture, BDD scenarios, prior decisions. Reading broadly here means you won't ask questions the docs already answer.
3. If it's a task, call ` + "`" + `list_task_activities` + "`" + ` to read prior comments, decisions, and any clarifications already given.

## Step 2 — Identify ambiguities

Read the task description or document carefully and find:
- **Scope gaps** — what is in vs. out is not stated
- **Missing edge cases** — error states, empty states, permission boundaries, concurrency
- **Undefined terms** — domain words that could mean different things in this codebase
- **Unstated assumptions** — things the author assumed but did not write down
- **Acceptance criteria gaps** — no measurable, testable "done" condition

Only surface real ambiguities. Skip things that are clearly inferable from context or docs you just read.

## Step 3 — Ask clarifying questions

Present a numbered list of at most 6 questions, grouped by theme (scope / edge cases / definitions). Err on the side of fewer, better questions over many shallow ones.

**Post these where the requester will actually see them, not just in this conversation** — for a task, that's ` + "`" + `add_task_comment` + "`" + ` with an ` + "`" + `@username` + "`" + ` mention, which sends a real notification. For a document with no linked task, ` + "`" + `add_doc_comment` + "`" + ` exists but ` + "`" + `@mentions` + "`" + ` there do NOT notify anyone — the person only sees it if they reopen the doc; if the doc is linked to a task, ask there instead. Wait for the user's answers — typically a reply that mentions you back — before writing anything.

**Example format:**
` + "`" + "`" + "`" + `
**Scope**
1. Should this cover X, or is X a separate initiative?
2. Does this apply to guest users, or only authenticated users?

**Edge cases**
3. What should happen when Y is empty?

**Acceptance criteria**
4. What does "success" look like — a UI state, an API response, something else?
` + "`" + "`" + "`" + `

## Step 4 — Update the spec in Paca

Once the user answers:
- **Task**: call ` + "`" + `update_task` + "`" + ` with an improved description including explicit acceptance criteria. Don't just append — rewrite the description so it stands alone without this conversation.
- **Document**: call ` + "`" + `write_doc` + "`" + ` with the resolved content and any new decisions recorded as a "Decisions" section.

Do not create a new document — update the existing one.

Report back: what was clarified and the task/doc number and title that was updated.

---

## If Paca MCP is not connected

> Paca MCP tools are not available. Run ` + "`" + `/paca-setup` + "`" + ` to configure the connection.

---

## Tool reference

**Tasks:** ` + "`" + `get_task` + "`" + ` · ` + "`" + `get_task_by_number` + "`" + ` · ` + "`" + `update_task` + "`" + ` · ` + "`" + `list_tasks` + "`" + `  
**Comments:** ` + "`" + `add_task_comment` + "`" + ` · ` + "`" + `list_task_activities` + "`" + ` · ` + "`" + `add_doc_comment` + "`" + ` · ` + "`" + `list_doc_activities` + "`" + `  
**Documents:** ` + "`" + `read_doc` + "`" + ` · ` + "`" + `write_doc` + "`" + ` · ` + "`" + `list_docs` + "`" + `  
**Projects:** ` + "`" + `list_projects` + "`" + `
`,
	},
	{
		Name: "paca-do",
		Content: `---
name: paca-do
description: Execute a Paca task end-to-end — reading context and acceptance criteria, doing the work (code, writing, research, review), updating task status, and commenting results. Use when asked to start, implement, complete, or work on a specific Paca task. Reads project docs first to understand the codebase and tech stack before acting.
compatibility: Requires Paca MCP server. Run /paca-setup if Paca tools are not available.
---

You are executing a task from Paca — reading it, understanding context, doing the work, and updating the record. Use Paca MCP tools throughout — never create local files for task records or documentation.

**If no task is specified**, call ` + "`" + `list_tasks` + "`" + ` filtered to in-progress tasks in the current sprint. Show them and ask which to work on.

---

## Step 1 — Load task and project context

1. Resolve the task reference from the user's message using ` + "`" + `get_task_by_number` + "`" + ` or ` + "`" + `get_task` + "`" + `.
2. **If the task has no acceptance criteria**, stop and ask the user to clarify before starting — or offer to run ` + "`" + `/paca-clarify` + "`" + ` first. Starting work without a clear "done" condition wastes effort.
3. Call ` + "`" + `list_docs` + "`" + ` and search for documents relevant to this task — architecture, design specs, BDD scenarios, API references, integration guides. Read before writing any code or content; what's already decided shapes every implementation choice.
4. Call ` + "`" + `list_task_activities` + "`" + ` to read prior comments and implementation notes — someone may have already investigated this.
5. Note the acceptance criteria from the task description. These are your exit criteria.

## Step 2 — Mark in progress

1. Call ` + "`" + `list_task_statuses` + "`" + ` to find the "in progress" status.
2. Call ` + "`" + `update_task` + "`" + ` to set the status. (No confirmation needed — this is a lightweight, reversible status change.)
3. Call ` + "`" + `add_task_comment` + "`" + `: "Starting work on this task."

## Step 3 — Do the work

Execute based on the task type:

- **Code task**: find the relevant source files, read existing tests to understand the expected behavior, implement the change, run the test suite. If you need to understand what "in scope" looks like, the BDD scenarios you read in Step 1 are authoritative.
- **Writing task**: draft the content in the response, or create/update a Paca document via ` + "`" + `write_doc` + "`" + `. Never write to a local file.
- **Research / investigation task**: investigate, write findings as a comment via ` + "`" + `add_task_comment` + "`" + ` or as a Paca doc, then update the task description with the conclusions.
- **Review task**: analyse the artifact (PR, document, design), post a structured review as ` + "`" + `add_task_comment` + "`" + `.

If you discover a blocker or a genuine sub-task that wasn't anticipated, create it in Paca with ` + "`" + `create_task` + "`" + ` (reference the parent: ` + "`" + `Blocked by #<parent>` + "`" + `). Don't silently skip or work around it.

## Step 4 — Update and close

1. Call ` + "`" + `add_task_comment` + "`" + ` with a completion summary: what was done, what changed, any known caveats or follow-up needed.
2. If any project documentation was affected (README, architecture doc, API reference), update the relevant Paca document with ` + "`" + `write_doc` + "`" + `. Never write new docs as local files.
3. Call ` + "`" + `update_task` + "`" + ` to set the status to done (or the next stage — e.g. "review" — if your workflow has one).

**What's next:** Consider running ` + "`" + `/paca-test #<number>` + "`" + ` to verify the implementation against acceptance criteria.

Report back: task number, title, summary of what was done, and any new tasks or docs created.

---

## If Paca MCP is not connected

> Paca MCP tools are not available. Run ` + "`" + `/paca-setup` + "`" + ` to configure the connection.

---

## Tool reference

**Tasks:** ` + "`" + `get_task` + "`" + ` · ` + "`" + `get_task_by_number` + "`" + ` · ` + "`" + `update_task` + "`" + ` · ` + "`" + `create_task` + "`" + ` · ` + "`" + `list_task_statuses` + "`" + `  
**Comments:** ` + "`" + `add_task_comment` + "`" + ` · ` + "`" + `list_task_activities` + "`" + `  
**Documents:** ` + "`" + `list_docs` + "`" + ` · ` + "`" + `read_doc` + "`" + ` · ` + "`" + `write_doc` + "`" + `  
**Projects:** ` + "`" + `list_projects` + "`" + `
`,
		AgentContent: `---
name: paca-do
description: Execute a Paca task end-to-end — reading context and acceptance criteria, doing the work (code, writing, research, review), updating task status, and commenting results. Use when asked to start, implement, complete, or work on a specific Paca task. Reads project docs first to understand the codebase and tech stack before acting.
triggers:
  - /paca-do
---

You are executing a task from Paca — reading it, understanding context, doing the work, and updating the record. Use Paca MCP tools throughout — never create local files for task records or documentation.

**If no task is specified**, call ` + "`" + `list_tasks` + "`" + ` filtered to in-progress tasks in the current sprint. Show them and ask which to work on.

---

## Step 1 — Load task and project context

1. Resolve the task reference from the user's message using ` + "`" + `get_task_by_number` + "`" + ` or ` + "`" + `get_task` + "`" + `.
2. **If the task has no acceptance criteria**, stop and ask the user to clarify before starting — or offer to run ` + "`" + `/paca-clarify` + "`" + ` first. Starting work without a clear "done" condition wastes effort.
3. Call ` + "`" + `list_docs` + "`" + ` and search for documents relevant to this task — architecture, design specs, BDD scenarios, API references, integration guides. Read before writing any code or content; what's already decided shapes every implementation choice.
4. Call ` + "`" + `list_task_activities` + "`" + ` to read prior comments and implementation notes — someone may have already investigated this.
5. Note the acceptance criteria from the task description. These are your exit criteria.

## Step 2 — Mark in progress

1. Call ` + "`" + `list_task_statuses` + "`" + ` to find the "in progress" status.
2. Call ` + "`" + `update_task` + "`" + ` to set the status. (No confirmation needed — this is a lightweight, reversible status change.)
3. Call ` + "`" + `add_task_comment` + "`" + `: "Starting work on this task."

## Step 3 — Do the work

Execute based on the task type:

- **Code task**: find the relevant source files, read existing tests to understand the expected behavior, implement the change, run the test suite. If you need to understand what "in scope" looks like, the BDD scenarios you read in Step 1 are authoritative.
- **Writing task**: draft the content in the response, or create/update a Paca document via ` + "`" + `write_doc` + "`" + `. Never write to a local file.
- **Research / investigation task**: investigate, write findings as a comment via ` + "`" + `add_task_comment` + "`" + ` or as a Paca doc, then update the task description with the conclusions.
- **Review task**: analyse the artifact (PR, document, design), post a structured review as ` + "`" + `add_task_comment` + "`" + `.

If you discover a blocker or a genuine sub-task that wasn't anticipated, create it in Paca with ` + "`" + `create_task` + "`" + ` (reference the parent: ` + "`" + `Blocked by #<parent>` + "`" + `). Don't silently skip or work around it.

### Code tasks with a linked repository — push and create a PR (MANDATORY)

If this task involves code changes AND the project has a linked repository, completing the work means publishing it — changes left only in the sandbox are NOT done. Follow ALL steps:

1. **Clone and branch first** (before editing):
   - ` + "`" + `list_repositories` + "`" + ` → note the ` + "`" + `plugin_id` + "`" + ` and ` + "`" + `repo_id` + "`" + `.
   - ` + "`" + `clone_repository` + "`" + ` (clones to /workspace/repo).
   - Create and switch to a feature branch: ` + "`" + `git checkout -b feat/<task-number>-<short-description>` + "`" + `.

2. **Make your changes** in the cloned repo. Verify with tests if available.

3. **Commit:**
   ` + "`" + "`" + "`" + `
   git add -A
   git commit -m '<type>: <concise message>'
   ` + "`" + "`" + "`" + `

4. **Push the branch:** Call ` + "`" + `push_branch` + "`" + ` with the ` + "`" + `plugin_id` + "`" + ` and ` + "`" + `repo_id` + "`" + ` from step 1. Do NOT skip this — without a push, the remote has no branch for the PR.

5. **Create a pull request:** Call the repository plugin's PR tool (e.g. ` + "`" + `github_create_pull_request` + "`" + ` for GitHub repos). This is required — a pushed branch without a PR is unfinished work. Pass:
   - ` + "`" + `projectId` + "`" + `, ` + "`" + `taskId` + "`" + `, ` + "`" + `repoId` + "`" + `
   - ` + "`" + `title` + "`" + ` — a clear commit-style title
   - ` + "`" + `head_branch` + "`" + ` — your feature branch name
   - ` + "`" + `base_branch` + "`" + ` — the repository's default branch (e.g. ` + "`" + `main` + "`" + `)
   - ` + "`" + `body` + "`" + ` — a summary of what changed and why, referencing the task

6. **Report the PR URL** in the task comment (Step 4).

If any step fails (push rejected, token error, etc.), report it in a task ` + "`" + `add_task_comment` + "`" + ` rather than silently moving on.

## Step 4 — Update and close

1. Call ` + "`" + `add_task_comment` + "`" + ` with a completion summary: what was done, what changed, any known caveats or follow-up needed.
2. If any project documentation was affected (README, architecture doc, API reference), update the relevant Paca document with ` + "`" + `write_doc` + "`" + `. Never write new docs as local files.
3. Call ` + "`" + `update_task` + "`" + ` to set the status to done (or the next stage — e.g. "review" — if your workflow has one).

**What's next:** Consider running ` + "`" + `/paca-test #<number>` + "`" + ` to verify the implementation against acceptance criteria.

Report back: task number, title, summary of what was done, and any new tasks or docs created. If you pushed code and opened a PR, include the PR URL.

---

## Tool reference

**Tasks:** ` + "`" + `get_task` + "`" + ` · ` + "`" + `get_task_by_number` + "`" + ` · ` + "`" + `update_task` + "`" + ` · ` + "`" + `create_task` + "`" + ` · ` + "`" + `list_task_statuses` + "`" + `
**Comments:** ` + "`" + `add_task_comment` + "`" + ` · ` + "`" + `list_task_activities` + "`" + `
**Documents:** ` + "`" + `list_docs` + "`" + ` · ` + "`" + `read_doc` + "`" + ` · ` + "`" + `write_doc` + "`" + `
**Projects:** ` + "`" + `list_projects` + "`" + `
**Repositories (built-in tools):** ` + "`" + `list_repositories` + "`" + ` · ` + "`" + `clone_repository` + "`" + ` · ` + "`" + `push_branch` + "`" + `
**GitHub PRs/branches (MCP plugin):** ` + "`" + `github_create_pull_request` + "`" + ` · ` + "`" + `github_list_task_prs` + "`" + ` · ` + "`" + `github_create_branch` + "`" + ` · ` + "`" + `github_list_task_branches` + "`" + `
`,
	},
	{
		Name: "paca-doc",
		Content: `---
name: paca-doc
description: Write or update documentation for a feature, task, or topic in Paca Docs. Use when asked to document a completed feature, write a guide or runbook, update existing docs, create a spec or architecture document, or produce BDD scenarios. Documentation is saved in Paca — never created as local files.
compatibility: Requires Paca MCP server. Run /paca-setup if Paca tools are not available.
---

You are writing or updating documentation in Paca Docs. Documentation lives in Paca — never create local files for docs.

**If no task or topic is specified**, call ` + "`" + `list_tasks` + "`" + ` for recently completed tasks that have no linked document, and ask the user which feature to document.

---

## Step 1 — Load project context

1. Resolve any reference from the user's message:
   - ` + "`" + `#42` + "`" + `, ` + "`" + `ABC-42` + "`" + ` → ` + "`" + `get_task_by_number` + "`" + ` to read the feature being documented
   - Doc title or path → ` + "`" + `list_docs` + "`" + ` → ` + "`" + `read_doc` + "`" + ` to load the existing doc
   - Free-text topic → ` + "`" + `list_docs` + "`" + ` first to verify no duplicate already exists; if a similar doc is found, offer to update it instead of creating a new one
2. Call ` + "`" + `list_docs` + "`" + ` and read related docs — architecture, existing guides, BDD scenarios, API references. Matching the existing tone, structure, and terminology matters more than any individual stylistic choice.
3. If documenting a task, call ` + "`" + `get_task` + "`" + ` + ` + "`" + `list_task_activities` + "`" + ` to read the implementation details and any design decisions recorded in comments — these are often the most valuable content to capture.
4. If updating an existing doc, call ` + "`" + `list_doc_activities` + "`" + ` to check for open questions or prior discussion left in its comments.

## Step 2 — Identify doc type and draft outline

Based on context, identify the type:
- **Guide / tutorial** — step-by-step, outcome-oriented, written for someone doing it for the first time
- **Reference** — exhaustive API, config, or CLI reference
- **Architecture / design doc** — decisions, tradeoffs, diagrams (as Markdown)
- **BDD / acceptance spec** — Gherkin-style scenarios
- **Runbook** — operational steps for a known procedure

If the type is obvious from the task or request, proceed directly. Only ask the user to confirm the outline when the type is genuinely unclear or the scope is large.

## Step 3 — Write the documentation

Write complete, clear Markdown:
- Active voice and present tense
- Code examples and command snippets where they aid understanding
- Link to related Paca docs by title or task number
- No "last updated" timestamps, no "Created by Claude" lines — Paca tracks history
- No placeholder sections ("TBD", "coming soon") — write real content or omit the section

## Step 4 — Save to Paca

- **New document**: call ` + "`" + `write_doc` + "`" + ` with a full path (e.g. ` + "`" + `'Architecture/API Design'` + "`" + ` — the last segment is the title, preceding segments are folders) and the Markdown content. Missing folders are created automatically.
- **Existing document**: call ` + "`" + `write_doc` + "`" + ` with the same path. Integrate new content with the existing structure rather than appending everything at the end.
- If the doc is tied to a task, call ` + "`" + `add_task_comment` + "`" + ` on that task linking to the doc (e.g. "Documentation written: [link]") — that also reliably notifies whoever's watching the task. Otherwise, ` + "`" + `add_doc_comment` + "`" + ` works too, but ` + "`" + `@mentions` + "`" + ` there don't send a notification.

Report back: the document's title and the path it was saved at.

---

## If Paca MCP is not connected

> Paca MCP tools are not available. Run ` + "`" + `/paca-setup` + "`" + ` to configure the connection.

---

## Tool reference

**Documents:** ` + "`" + `write_doc` + "`" + ` · ` + "`" + `read_doc` + "`" + ` · ` + "`" + `list_docs` + "`" + ` · ` + "`" + `move_doc` + "`" + `  
**Tasks:** ` + "`" + `get_task` + "`" + ` · ` + "`" + `get_task_by_number` + "`" + ` · ` + "`" + `list_task_activities` + "`" + `  
**Comments:** ` + "`" + `add_task_comment` + "`" + ` · ` + "`" + `add_doc_comment` + "`" + ` · ` + "`" + `list_doc_activities` + "`" + `  
**Projects:** ` + "`" + `list_projects` + "`" + `
`,
	},
	{
		Name: "paca-epic",
		Content: `---
name: paca-epic
description: Turn a product requirement or feature description into a structured epic in Paca, with child user stories and a spec document. Use when asked to plan a new feature, break down a high-level requirement into stories, create an epic, or go from "we need X" to a fully structured backlog ready for sprint planning.
compatibility: Requires Paca MCP server. Run /paca-setup if Paca tools are not available.
---

You are turning requirements into a structured epic in Paca. Use Paca MCP tools throughout — never create local files.

**If no requirement is specified**, ask: "What requirement or feature do you want to turn into an epic? Describe it in a sentence or two."

---

## Step 1 — Load project context

1. Call ` + "`" + `list_projects` + "`" + ` to identify the relevant project (infer from the user's message, or ask if ambiguous).
2. Call ` + "`" + `list_docs` + "`" + ` and search for documents whose titles or descriptions suggest requirements, roadmap, architecture, or BDD scenarios. Read the most relevant ones with ` + "`" + `read_doc` + "`" + `. Understand the domain and existing feature landscape before writing anything.
3. Call ` + "`" + `list_task_types` + "`" + ` to check whether an "Epic" type exists.
4. Call ` + "`" + `list_task_statuses` + "`" + ` to know available statuses.
5. Call ` + "`" + `list_tasks` + "`" + ` to scan for existing epics so you avoid duplicating scope.

## Step 2 — Parse the requirements

Extract from the user's message:
- **Goal** — what user or business outcome this achieves
- **Scope** — what is in / out of scope
- **Stakeholders** — who benefits, who owns
- **Constraints** — tech, time, dependencies

**If requirements are vague**, ask at most 3 targeted questions before proceeding. Focus on what you genuinely cannot infer. Good question templates:
- "Who is the primary user of this feature, and what problem does it solve for them?"
- "Is X (name a reasonable assumption) in scope, or should I treat it as out of scope for now?"
- "Are there existing systems or services this needs to integrate with?"

Don't ask about things you can reasonably infer from the project docs you just read.

## Step 3 — Create the epic task

Call ` + "`" + `create_task` + "`" + `:
- **Type**: "Epic" if available from ` + "`" + `list_task_types` + "`" + `.
- **Title**: concise, outcome-oriented (e.g. ` + "`" + `User Authentication` + "`" + `). The type field already says this is an epic — don't also prefix the title with ` + "`" + `Epic:` + "`" + `. Only fall back to a prefix like ` + "`" + `Epic: User Authentication` + "`" + ` if the project has no Epic type at all, since the title is then the only place that information can live.
- **Description** (Markdown):
  ` + "`" + "`" + "`" + `
  ## Goal
  <one paragraph>

  ## Scope
  **In:** ...
  **Out:** ...

  ## Acceptance Criteria
  - [ ] ...

  ## Open Questions
  - ...
  ` + "`" + "`" + "`" + `

## Step 4 — Break into stories

Derive child tasks from the requirements. Aim for 3–8 stories for a typical epic; go higher if the scope is large, but confirm with the user before creating more than 10. For each story:
- Call ` + "`" + `create_task` + "`" + ` with a ` + "`" + `type` + "`" + ` of "Story" (or the closest match in ` + "`" + `list_task_types` + "`" + `) if one exists, a clear title with no ` + "`" + `Story:` + "`" + ` prefix — same reasoning as the epic's title, the type field already conveys it — a brief description, and 2–3 acceptance criteria.
- Reference the parent epic in the description: ` + "`" + `Part of #<epic-number>` + "`" + `
- Prefer vertical slices (end-to-end thin features) over horizontal layers (all-backend, all-frontend)

## Step 5 — Create a spec document

Call ` + "`" + `write_doc` + "`" + `:
- **Title**: ` + "`" + `Epic: <name> — Specification` + "`" + `
- **Content**: Goal · Background · User Stories (linked by ` + "`" + `#number` + "`" + `) · Acceptance Criteria · Out of Scope · Open Questions

**What's next:** After this, consider running ` + "`" + `/paca-estimate #<epic-number>` + "`" + ` to add story point estimates to the new tasks, and ` + "`" + `/paca-sprint` + "`" + ` to plan them into a sprint.

Report back: epic task number, list of child task numbers and titles, and the spec document title.

---

## If Paca MCP is not connected

> Paca MCP tools are not available. Run ` + "`" + `/paca-setup` + "`" + ` to configure the connection.

---

## Tool reference

**Tasks:** ` + "`" + `create_task` + "`" + ` · ` + "`" + `update_task` + "`" + ` · ` + "`" + `list_tasks` + "`" + ` · ` + "`" + `get_task_by_number` + "`" + ` · ` + "`" + `list_task_types` + "`" + ` · ` + "`" + `list_task_statuses` + "`" + `  
**Documents:** ` + "`" + `write_doc` + "`" + ` · ` + "`" + `list_docs` + "`" + ` · ` + "`" + `read_doc` + "`" + `  
**Projects:** ` + "`" + `list_projects` + "`" + ` · ` + "`" + `get_project` + "`" + `
`,
	},
	{
		Name: "paca-estimate",
		Content: `---
name: paca-estimate
description: Estimate story points for Paca tasks using the Fibonacci scale, calibrated against recently completed reference tasks and project tech stack. Use when tasks are missing estimates, before sprint planning, when the team needs sizing for prioritization, or when asked to size a backlog.
compatibility: Requires Paca MCP server. Run /paca-setup if Paca tools are not available.
---

You are estimating effort for tasks in Paca. Use Paca MCP tools throughout — never create local files.

**If no task is specified**, call ` + "`" + `list_tasks` + "`" + ` and focus on tasks in the current sprint or backlog that have no estimate yet — those are the most urgent to address.

---

## Step 1 — Load project context

1. Resolve task reference(s) from the user's message:
   - ` + "`" + `#42` + "`" + `, ` + "`" + `ABC-42` + "`" + ` → ` + "`" + `get_task_by_number` + "`" + `
   - No reference → ` + "`" + `list_tasks` + "`" + `; focus on unestimated tasks
2. Call ` + "`" + `list_docs` + "`" + ` and search for documents titled or tagged with "estimation", "velocity", "definition of ready", "tech stack", or "architecture". Read the most relevant ones with ` + "`" + `read_doc` + "`" + `. Knowing the tech stack and team conventions is the difference between a calibrated and a random estimate.
3. Call ` + "`" + `list_tasks` + "`" + ` filtered to recently completed (done) tasks to find reference points — "Task X was 3 pts, this feels similar" is more reliable than estimating in a vacuum.

## Step 2 — Estimate each task

For each task, call ` + "`" + `get_task` + "`" + ` and reason through four dimensions:
- **Implementation complexity** — number of layers touched, algorithmic difficulty
- **Uncertainty** — how well-understood the work is; unknown → estimate higher
- **Test surface** — unit tests, integration tests, edge cases to cover
- **Integration points** — external APIs, databases, other services, third-party SDKs

**Scale (Fibonacci):** 1 · 2 · 3 · 5 · 8 · 13
- **1–2 pts**: simple, well-understood, < 1 day
- **3–5 pts**: moderate complexity or some unknowns, 1–3 days
- **8 pts**: complex or high uncertainty — consider whether ` + "`" + `/paca-breakdown` + "`" + ` would reduce risk
- **13 pts**: too large to estimate reliably — strongly recommend breaking down first

Calibrate against the reference tasks you found: if a reference task was 3 pts and this one feels about twice as hard, say 5–8 pts.

Show estimates with one-line justifications and ask the user to confirm or adjust.

## Step 3 — Write estimates back

For each confirmed estimate, call ` + "`" + `update_task` + "`" + `:
- Prepend ` + "`" + `**Estimate:** N pts` + "`" + ` as the first line of the description (or update the custom field if the project has one — check ` + "`" + `list_custom_fields` + "`" + ` to see)
- Include a one-line rationale so the reasoning is preserved

Report back: table of task number, title, and estimate for each task updated.

---

## If Paca MCP is not connected

> Paca MCP tools are not available. Run ` + "`" + `/paca-setup` + "`" + ` to configure the connection.

---

## Tool reference

**Tasks:** ` + "`" + `get_task` + "`" + ` · ` + "`" + `get_task_by_number` + "`" + ` · ` + "`" + `list_tasks` + "`" + ` · ` + "`" + `update_task` + "`" + ` · ` + "`" + `list_task_statuses` + "`" + ` · ` + "`" + `list_custom_fields` + "`" + `  
**Documents:** ` + "`" + `list_docs` + "`" + ` · ` + "`" + `read_doc` + "`" + `  
**Projects:** ` + "`" + `list_projects` + "`" + `
`,
	},
	{
		Name: "paca-prioritize",
		Content: `---
name: paca-prioritize
description: Set or adjust priorities across the Paca backlog, aligned to roadmap goals and business value. Use when the backlog needs sorting, before sprint planning, when tasks need explicit Critical/High/Medium/Low priority labels, or when asked to rank work by importance or urgency.
compatibility: Requires Paca MCP server. Run /paca-setup if Paca tools are not available.
---

You are setting priorities for tasks in Paca. Use Paca MCP tools throughout — never create local files.

**If no task is specified**, operate on the full backlog of the most active project.

---

## Step 1 — Load project context

1. Resolve task references from the user's message if given (` + "`" + `#42` + "`" + `, ` + "`" + `ABC-42` + "`" + ` → ` + "`" + `get_task_by_number` + "`" + `). If no references, operate on the full backlog.
2. Call ` + "`" + `list_docs` + "`" + ` and search for documents titled "roadmap", "goals", "OKR", "strategy", or "release plan". Read the most relevant ones with ` + "`" + `read_doc` + "`" + `. Priority without a goal to align to is just opinion — the docs anchor your reasoning.
   - **If no strategy docs exist**, ask the user: "What are the top 3 outcomes you're trying to achieve this quarter?" Use their answer as the alignment anchor.
3. Call ` + "`" + `list_tasks` + "`" + ` to load the full task set.
4. Call ` + "`" + `list_sprints` + "`" + ` to know which tasks are already committed vs. unscheduled.

## Step 2 — Score and rank

For each task, assess four dimensions:

| Dimension | What to look for |
|---|---|
| **Business value** | How directly it advances the roadmap or improves user outcomes |
| **Urgency / risk** | Customer-facing bugs, blockers, compliance deadlines |
| **Effort** | Estimated size — high-value + low-effort tasks rank up |
| **Dependencies** | Tasks that unblock others rank higher |

Assign one of these labels:
- **Critical** — blocks a release, breaks production, or is a contractual obligation
- **High** — directly advances the top roadmap goal; would be missed by users if delayed
- **Medium** — valuable but not time-sensitive; no clear near-term cost to waiting
- **Low** — nice-to-have, cleanup, or exploratory; can be deprioritized indefinitely without user impact

Present the ranked list with one-line reasoning per task. Ask the user to confirm or adjust before writing.

## Step 3 — Apply priorities

For each confirmed priority, call ` + "`" + `update_task` + "`" + `:
- Set the ` + "`" + `priority` + "`" + ` field to the agreed value
- Optionally append a one-line rationale to the description so the reasoning is preserved and reviewable

Report back: full updated priority list as a table (task number · title · priority).

---

## If Paca MCP is not connected

> Paca MCP tools are not available. Run ` + "`" + `/paca-setup` + "`" + ` to configure the connection.

---

## Tool reference

**Tasks:** ` + "`" + `get_task` + "`" + ` · ` + "`" + `get_task_by_number` + "`" + ` · ` + "`" + `list_tasks` + "`" + ` · ` + "`" + `update_task` + "`" + `  
**Documents:** ` + "`" + `list_docs` + "`" + ` · ` + "`" + `read_doc` + "`" + `  
**Sprints:** ` + "`" + `list_sprints` + "`" + `  
**Projects:** ` + "`" + `list_projects` + "`" + `
`,
	},
	{
		Name: "paca-setup",
		Content: `---
name: paca-setup
description: Configure the Paca MCP server for use with Claude Code or Claude Desktop. Use when setting up Paca for the first time, adding or editing the MCP server config, troubleshooting connectivity, or installing the Paca skills globally. Walks the user through prerequisites, config file generation, verification, and optional global skill install.
compatibility: Requires Node.js 18+ and a running Paca instance.
---

You are helping the user configure the Paca MCP server for their Claude Code environment.

Walk the user through the setup interactively, step by step. Do not dump all instructions at once — confirm each step before proceeding.

---

## Setup flow

### Step 1 — Check prerequisites

Ask the user to confirm:
- [ ] Paca is running (local: ` + "`" + `http://localhost:8080` + "`" + `, or their hosted URL)
- [ ] Node.js 18+ is installed (` + "`" + `node --version` + "`" + `)
- [ ] They have a Paca API key (Settings → API Keys inside Paca UI)

If any prerequisite is missing, guide the user to resolve it before continuing.

### Step 2 — Identify the Claude Code config file

Ask which environment they want to configure:

**A. Claude Code (CLI) — project-level** (recommended for team projects)
→ Create or edit ` + "`" + `.claude/mcp.json` + "`" + ` in the project root.

**B. Claude Code (CLI) — global**
→ Run: ` + "`" + `claude mcp add paca --env PACA_API_KEY=<key> --env PACA_API_URL=<url> -- npx -y @paca-ai/paca-mcp` + "`" + `

**C. Claude Desktop**
→ Edit the config file for your OS:
- **macOS**: ` + "`" + `~/Library/Application Support/Claude/claude_desktop_config.json` + "`" + `
- **Windows**: ` + "`" + `%APPDATA%\Claude\claude_desktop_config.json` + "`" + `
- **Linux**: ` + "`" + `~/.config/Claude/claude_desktop_config.json` + "`" + `

### Step 3 — Generate the config snippet

Based on their choice, generate the exact config block. Example for option A (` + "`" + `.claude/mcp.json` + "`" + `):

` + "`" + "`" + "`" + `json
{
  "mcpServers": {
    "paca": {
      "command": "npx",
      "args": ["-y", "@paca-ai/paca-mcp"],
      "env": {
        "PACA_API_KEY": "<their-api-key>",
        "PACA_API_URL": "<their-paca-url>"
      }
    }
  }
}
` + "`" + "`" + "`" + `

Substitute the actual values the user provides. Remind them:
- **Never commit API keys to git.** Add ` + "`" + `.claude/mcp.json` + "`" + ` to ` + "`" + `.gitignore` + "`" + ` if it contains a key, or use environment variable substitution.

### Step 4 — Apply the config

For option A, write the ` + "`" + `.claude/mcp.json` + "`" + ` file directly (ask permission first).  
For options B and C, show the command/snippet and ask the user to apply it.

### Step 5 — Verify

Once configured, ask the user to restart Claude Code / Claude Desktop, then test with:

> "List my Paca projects"

If Paca tools appear and return results, setup is complete. If not, check:
1. JSON syntax is valid
2. API key is correct (no extra spaces)
3. Paca API URL is reachable (` + "`" + `curl <PACA_API_URL>/api/v1/health` + "`" + `)
4. Node.js / npx is in PATH

### Step 6 — Install the Paca skills globally (optional)

Offer to run the skill installer so ` + "`" + `/paca` + "`" + ` and related skills are always available — not just in Claude Code, but in Gemini CLI, Cursor, and any AGENTS.md-reading tool too. Pass through the ` + "`" + `PACA_API_URL` + "`" + ` / ` + "`" + `PACA_API_KEY` + "`" + ` already collected in Step 1 — the installer fetches skill content from that instance's own API (matching the exact version it's running) rather than GitHub, and ` + "`" + `PACA_API_URL` + "`" + ` is required for this unless the user is running from a local clone of the Paca repo instead:

` + "`" + "`" + "`" + `bash
PACA_API_URL=<their-paca-url> PACA_API_KEY=<their-api-key> \
  curl -fsSL https://raw.githubusercontent.com/Paca-AI/paca/master/scripts/install-paca-skills.sh | bash
` + "`" + "`" + "`" + `

Review the script before running it — ` + "`" + `curl | bash` + "`" + ` executes remote code directly. This also requires ` + "`" + `jq` + "`" + ` to be installed.

If the user has plugins enabled on their Paca instance that contribute their own skills, no extra step is needed — the same ` + "`" + `PACA_API_URL` + "`" + `/` + "`" + `PACA_API_KEY` + "`" + ` above pulls those in automatically alongside Paca's own bundled set.

---

## Environment variables reference

| Variable | Required | Description |
|---|---|---|
| ` + "`" + `PACA_API_KEY` + "`" + ` | Yes | API key from Paca → Settings → API Keys |
| ` + "`" + `PACA_API_URL` + "`" + ` | No (default: ` + "`" + `http://localhost:8080` + "`" + `) | Your Paca instance URL |
`,
		CLIOnly: true,
	},
	{
		Name: "paca-sprint",
		Content: `---
name: paca-sprint
description: Plan a Paca sprint by selecting tasks from the backlog, detecting carryover from the previous sprint, inferring velocity from past sprints, and assigning tasks to the sprint. Use when starting a new sprint, filling an upcoming sprint, reviewing sprint capacity, or setting the sprint goal.
compatibility: Requires Paca MCP server. Run /paca-setup if Paca tools are not available.
---

You are planning a sprint in Paca. Use Paca MCP tools throughout — never create local files.

**If no sprint is specified**, default to planning the next upcoming sprint for the most active project.

---

## Step 1 — Load project context

1. Call ` + "`" + `list_projects` + "`" + ` and identify the relevant project.
2. Call ` + "`" + `list_docs` + "`" + ` and search for documents with keywords like "roadmap", "goals", "OKR", "retrospective", or "planning". Read the most relevant ones with ` + "`" + `read_doc` + "`" + `. What the last sprint achieved and what the project's current goals are will shape which tasks belong in this sprint.
3. Call ` + "`" + `list_sprints` + "`" + ` to see existing sprints (active, upcoming, completed).
4. Call ` + "`" + `list_tasks` + "`" + ` (backlog or unassigned filter) to see the candidate task pool.

## Step 2 — Detect carryover and infer velocity

**Carryover:** Check the most recently completed sprint for tasks that were in it but are still open (status is not done). Flag these — carryover tasks typically get priority in the next sprint.

**Velocity:** Look at the last 2–3 completed sprints. Sum the story point estimates of tasks that were marked done. Use the average as the default capacity if the user hasn't stated one.

## Step 3 — Determine sprint parameters

From the user's message and context:
- **Which sprint** — by number, name, or "next sprint"
- **Capacity** — story points available; use inferred velocity as the default, ask to confirm
- **Goal** — what outcome the sprint should deliver; infer from the roadmap or ask
- **Duration** — start/end dates; infer from the cadence of past sprints or ask

## Step 4 — Select tasks

Rank backlog tasks and recommend a set that fits within capacity:
- **First**: carryover tasks from the previous sprint
- **Then**: tasks ranked by explicit priority → unblocking dependencies → business value → estimate size
- **Flag**: tasks missing estimates or acceptance criteria — offer to run ` + "`" + `/paca-estimate` + "`" + ` or ` + "`" + `/paca-clarify` + "`" + ` on them before committing

Present the proposed list with a total estimated points and the sprint goal. Ask the user to confirm, swap, or adjust before applying.

## Step 5 — Apply the plan

Once confirmed:
1. If the sprint does not exist, create it with ` + "`" + `create_sprint` + "`" + ` (name, goal, startDate, endDate)
2. Assign each task to the sprint with ` + "`" + `update_task` + "`" + ` (set ` + "`" + `sprintId` + "`" + `)
3. Set or update the sprint goal with ` + "`" + `update_sprint` + "`" + `

Optionally create or update a sprint planning note in Paca Docs (` + "`" + `write_doc` + "`" + `) with the sprint goal, task list, capacity, and velocity reference.

Report back: sprint name, dates, task count, total estimate, capacity used, and sprint goal.

---

## If Paca MCP is not connected

> Paca MCP tools are not available. Run ` + "`" + `/paca-setup` + "`" + ` to configure the connection.

---

## Tool reference

**Sprints:** ` + "`" + `list_sprints` + "`" + ` · ` + "`" + `get_sprint` + "`" + ` · ` + "`" + `create_sprint` + "`" + ` · ` + "`" + `update_sprint` + "`" + ` · ` + "`" + `complete_sprint` + "`" + `  
**Tasks:** ` + "`" + `list_tasks` + "`" + ` · ` + "`" + `get_task` + "`" + ` · ` + "`" + `update_task` + "`" + ` · ` + "`" + `bulk_move_tasks` + "`" + `  
**Documents:** ` + "`" + `list_docs` + "`" + ` · ` + "`" + `read_doc` + "`" + ` · ` + "`" + `write_doc` + "`" + `  
**Projects:** ` + "`" + `list_projects` + "`" + `
`,
	},
	{
		Name: "paca-test",
		Content: `---
name: paca-test
description: Verify a completed Paca task against its acceptance criteria — deriving test cases, running or describing tests, and recording pass/fail results with a status update. Use when asked to test, verify, QA, or review a task that has been implemented. Defaults to tasks in "review" status when none is specified.
compatibility: Requires Paca MCP server. Run /paca-setup if Paca tools are not available.
---

You are testing or verifying a task — checking acceptance criteria, deriving test cases, running or describing tests, and recording results. Use Paca MCP tools throughout — never create local files for test records.

**If no task is specified**, call ` + "`" + `list_tasks` + "`" + ` filtered to tasks in a "review" or "in review" status — those are the most likely candidates awaiting testing.

---

## Step 1 — Load task and project context

1. Resolve the task reference from the user's message using ` + "`" + `get_task_by_number` + "`" + ` or ` + "`" + `get_task` + "`" + `.
2. Call ` + "`" + `list_docs` + "`" + ` and search for documents titled or tagged with "BDD", "acceptance criteria", "test plan", "QA", or the feature name. Read the most relevant ones with ` + "`" + `read_doc` + "`" + `. What "done" looks like needs to be established from the spec, not invented.
3. Call ` + "`" + `list_task_activities` + "`" + ` to read implementation notes, prior test runs, and edge cases already flagged.

## Step 2 — Derive test cases

From the task's acceptance criteria and related docs, derive test cases covering:
- **Happy path** — the primary flow works as specified
- **Edge cases** — boundary values, empty inputs, maximum sizes, off-by-one
- **Error cases** — invalid input, missing permissions, external failures, timeouts
- **Regression** — adjacent functionality that must not be broken

If the acceptance criteria are clear and cover these categories, proceed directly. Only ask the user to confirm the test plan when the scope is large or genuinely ambiguous.

## Step 3 — Execute tests

Choose the approach based on what's available:

- **Automated (code)**: run the test suite or exercise the feature with ` + "`" + `Bash` + "`" + `; report exact output
- **Document / content**: verify structure, completeness, and accuracy against the spec
- **API / integration**: describe the request, expected response, and actual response
- **Manual QA required**: if the feature requires a browser or UI interaction you cannot perform, describe the manual test steps clearly in the comment so a human can execute them. Mark the task status as "awaiting QA" rather than done.

Record pass / fail for each test case with a brief note.

## Step 4 — Record results and update status

1. Call ` + "`" + `add_task_comment` + "`" + ` with a markdown table of results:
   ` + "`" + "`" + "`" + `
   | Test case | Result | Notes |
   |---|---|---|
   | Happy path: ... | ✅ Pass | |
   | Edge case: empty list | ❌ Fail | Returns 500, expected empty array |
   ` + "`" + "`" + "`" + `
2. **All pass**: call ` + "`" + `update_task` + "`" + ` to advance the status to the next stage (e.g. "review", "done")
3. **Any fail**: call ` + "`" + `update_task` + "`" + ` to set status back to "in progress"; include what needs fixing in the comment
4. If these test cases represent a repeatable procedure (e.g. release checklist, integration test steps), preserve them in a Paca document with ` + "`" + `write_doc` + "`" + `

**What's next:** If the task passes and there's no existing documentation for this feature, consider running ` + "`" + `/paca-doc #<number>` + "`" + ` to write it.

Report back: task number, total tests run, pass/fail count, and the status after testing.

---

## If Paca MCP is not connected

> Paca MCP tools are not available. Run ` + "`" + `/paca-setup` + "`" + ` to configure the connection.

---

## Tool reference

**Tasks:** ` + "`" + `get_task` + "`" + ` · ` + "`" + `get_task_by_number` + "`" + ` · ` + "`" + `update_task` + "`" + ` · ` + "`" + `list_task_statuses` + "`" + `  
**Comments:** ` + "`" + `add_task_comment` + "`" + ` · ` + "`" + `list_task_activities` + "`" + `  
**Documents:** ` + "`" + `list_docs` + "`" + ` · ` + "`" + `read_doc` + "`" + ` · ` + "`" + `write_doc` + "`" + `  
**Projects:** ` + "`" + `list_projects` + "`" + `
`,
	},
	{
		Name: "paca-workflow",
		Content: `---
name: paca-workflow
description: Plan, design, and build an automation workflow in Paca — a dependency graph over existing tasks that auto-assigns work as statuses change and unlocks downstream tasks once their predecessors finish. Use when asked to automate a process, set up auto-assignment rules, chain task statuses, wire up dependencies so work hands off automatically, or edit/activate/archive an existing automation workflow.
compatibility: Requires Paca MCP server. Run /paca-setup if Paca tools are not available.
---

You are designing and building an automation workflow in Paca — a dependency graph over existing tasks, plus two workflow-level lookup tables (status→assignee rules, and a status-transition chain). Use Paca MCP tools throughout — never create local files.

**If no workflow or goal is specified**, call ` + "`" + `get_workflow` + "`" + ` (without ` + "`" + `workflowId` + "`" + `) to list existing workflows, and ask what process should be automated.

---

## Step 1 — Understand the domain model before touching any tool

A workflow has four parts, all addressed by ` + "`" + `taskId` + "`" + ` / ` + "`" + `statusId` + "`" + ` / ` + "`" + `memberId` + "`" + ` — you never need an internal node/edge/rule/transition ID to build or edit one:

- **Nodes** — existing tasks wrapped into the graph, each with a required canvas position (` + "`" + `posX` + "`" + `, ` + "`" + `posY` + "`" + `). There is no auto-placement; you decide both before calling.
- **Edges** — ` + "`" + `sourceTaskId → targetTaskId` + "`" + ` dependency links. Once the source task reaches the workflow's done status, the target task is re-evaluated against the status rules using **its own current status** — no status is force-changed on the target, only its assignment. A target with multiple incoming edges waits for ALL predecessors to reach done.
- **Status rules** — one shared table for the whole workflow: whenever ANY task in it changes to ` + "`" + `statusId` + "`" + `, auto-assign ` + "`" + `assigneeMemberId` + "`" + `. Creating a workflow seeds one default rule per project status automatically (assigned to you, or the project's first human member if you're the agent, since an agent can't hand work to itself) — these are valid starting points, not placeholders to delete. Change one with ` + "`" + `statusRules.set` + "`" + ` (upserts by ` + "`" + `statusId` + "`" + `); don't remove-then-recreate.
- **Status transitions** (the "status workflow") — for each status, what status comes next once work there is done. The status with no configured "next" is the workflow's done status — this is what edges watch for, and what tells an AI-agent assignee exactly what status to set next. A default chain is auto-generated from the project's statuses ordered by board position (chained sequentially); override entries via ` + "`" + `statusTransitions` + "`" + `.

**Lifecycle:** ` + "`" + `draft` + "`" + ` (freely editable, ignored by the automation engine) → ` + "`" + `active` + "`" + ` (engine runs it; requires ≥1 node and exactly one status with no next configured) → ` + "`" + `archived` + "`" + ` (terminal — can never revert; delete or build a new one instead).

## Step 2 — Gather everything before calling create_workflow or update_workflow

1. Resolve the project (` + "`" + `list_projects` + "`" + ` if not already known).
2. Call ` + "`" + `list_task_statuses` + "`" + ` and ` + "`" + `list_project_members` + "`" + ` — you'll need statusIds and memberIds for rules/transitions.
3. Identify the tasks to automate via ` + "`" + `list_tasks` + "`" + ` (or create them first with ` + "`" + `create_task` + "`" + ` — nodes wrap **existing** tasks only, never invent a task inline here).
4. Work out layout on paper before calling anything:
   - Tasks with no predecessor go in the top row (` + "`" + `posY = 0` + "`" + `); each task depending on another sits in a strictly **lower** row (larger ` + "`" + `posY` + "`" + `) than everything it depends on — this must agree with the edges you declare.
   - Tasks that are parallel/independent at the same stage share the same ` + "`" + `posY` + "`" + ` but get different ` + "`" + `posX` + "`" + ` values.
   - Space lanes ≥300px apart on X or ≥200px apart on Y — e.g. ` + "`" + `posY = 0, 200, 400` + "`" + ` for a 3-stage pipeline, ` + "`" + `posX = 0, 300, 600` + "`" + ` for 3 parallel lanes at one stage. The tool checks this after the call and returns a warning naming any pair still too close — treat that as a required fix, not a suggestion.
5. If editing an existing workflow, call ` + "`" + `get_workflow` + "`" + ` with its ` + "`" + `workflowId` + "`" + ` first to see current nodes/edges/positions, and lay out any new nodes past them rather than guessing or reusing a position.

## Step 3 — Build it

- **New workflow**: call ` + "`" + `create_workflow` + "`" + ` with ` + "`" + `name` + "`" + ` and, ideally, the whole graph (` + "`" + `nodes` + "`" + `, ` + "`" + `edges` + "`" + `, and any ` + "`" + `statusRules` + "`" + `/` + "`" + `statusTransitions` + "`" + ` overrides) in one call — it starts in ` + "`" + `draft` + "`" + `. Pass ` + "`" + `activate: true` + "`" + ` once the graph is complete, or activate later via ` + "`" + `update_workflow` + "`" + `.
- **Existing workflow**: call ` + "`" + `update_workflow` + "`" + `, addressing everything by taskId/statusId — ` + "`" + `nodes: {set, remove}` + "`" + `, ` + "`" + `statusRules: {set, remove}` + "`" + `, ` + "`" + `statusTransitions: {set, remove}` + "`" + `, ` + "`" + `edges: {add, remove}` + "`" + `. Put every node you're touching in **one** ` + "`" + `nodes.set` + "`" + ` array in a single call — never one call per node, even for pure repositioning.
- You do not need to manage the draft/active lock yourself: if you call ` + "`" + `update_workflow` + "`" + ` with graph edits and omit ` + "`" + `status` + "`" + `, an active workflow is automatically reverted to draft, edited, and re-activated within that same call. Only pass ` + "`" + `status` + "`" + ` explicitly when you want a different end state than what it already had (e.g. ` + "`" + `"draft"` + "`" + ` to stay in draft for review, ` + "`" + `"archived"` + "`" + ` to retire it permanently).
- Each node/rule/transition/edge is applied independently — one bad entry (e.g. an edge that would create a cycle) doesn't block the rest. Check the response for any failed items and fix them with a follow-up call.

## Step 4 — Verify and confirm

1. Call ` + "`" + `get_workflow` + "`" + ` with the ` + "`" + `workflowId` + "`" + ` to review the final graph.
2. If activation was the goal, confirm the response says the workflow is now ` + "`" + `active` + "`" + ` (it requires ≥1 node and exactly one status with no next status).
3. Report back: workflow name and ID, node/edge count, the done status, and who gets auto-assigned at each stage.

**Archiving is permanent** — confirm with the user before setting ` + "`" + `status: "archived"` + "`" + ` rather than assuming that's what "pause" or "turn off" means; they may just want ` + "`" + `"draft"` + "`" + `.

---

## Examples

| User message | What you do |
|---|---|
| ` + "`" + `automate the bug-fix process: triage → fix → review → done, assign review to Alice` + "`" + ` | ` + "`" + `create_workflow` + "`" + ` with 4 nodes chained top-to-bottom, edges linking each stage in order, a status rule assigning the review status to Alice |
| ` + "`" + `these 3 tasks can run in parallel, then #9 depends on all of them` + "`" + ` | 3 nodes at the same ` + "`" + `posY` + "`" + ` (different ` + "`" + `posX` + "`" + `) plus 1 node in the row below; 3 edges from each parallel task into #9 |
| ` + "`" + `pause the deploy workflow` + "`" + ` | Ask whether they mean temporarily (` + "`" + `status: "draft"` + "`" + `) or permanently (` + "`" + `status: "archived"` + "`" + `, irreversible) before acting |
| ` + "`" + `reposition these nodes so they don't overlap` + "`" + ` | ` + "`" + `get_workflow` + "`" + ` to see current positions → one ` + "`" + `update_workflow` + "`" + ` call with every affected node in a single ` + "`" + `nodes.set` + "`" + ` array |

---

## If Paca MCP is not connected

> Paca MCP tools are not available. Run ` + "`" + `/paca-setup` + "`" + ` to configure the connection.

---

## Tool reference

**Workflows:** ` + "`" + `get_workflow` + "`" + ` (also lists all workflows when ` + "`" + `workflowId` + "`" + ` is omitted) · ` + "`" + `create_workflow` + "`" + ` · ` + "`" + `update_workflow` + "`" + ` · ` + "`" + `delete_workflow` + "`" + `
**Supporting lookups:** ` + "`" + `list_tasks` + "`" + ` · ` + "`" + `list_task_statuses` + "`" + ` · ` + "`" + `list_project_members` + "`" + ` · ` + "`" + `list_projects` + "`" + `
`,
	},
}
