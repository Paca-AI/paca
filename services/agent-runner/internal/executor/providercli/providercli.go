// Package providercli describes, one file per CLI, how a provider_cli
// agent's Paca-configured MCP servers and skills (the same agent_mcp_servers
// / agent_skills rows an llm-type agent's own config comes from) get synced
// into that CLI's OWN on-disk config files.
//
// This exists because of one confirmed fact about Goose's "CLI providers"
// feature (https://goose-docs.ai/docs/guides/cli-providers/): once
// GOOSE_PROVIDER names a coding CLI instead of a raw model API, Goose stops
// managing extensions/skills entirely — "CLI providers do NOT give you
// access to goose's extension ecosystem (MCP servers, third-party
// integrations, etc.). They use their own built-in tools to prevent
// conflicts" — and automatically filters its own extension info out of the
// system prompt it builds around that CLI. Writing Goose's own
// .agents/skills files (see executor/skills.go) for a provider_cli agent
// would therefore be a harmless but pointless no-op; the only way Paca's
// configured skills/MCP servers ever reach the model is by landing in
// whatever config file the underlying CLI itself reads.
//
// Every adapter's SyncFiles is a pure function — no I/O of its own. The
// caller (executor.syncProviderCLIConfig) is responsible for reading each
// MergeableFiles() path's current content from the container (so an
// adapter can merge into an existing config file instead of blindly
// overwriting user- or CLI-set keys it has no business touching) and for
// uploading SyncFiles' output. This keeps every adapter here fully
// unit-testable against fixed input, independent of any container/exec
// plumbing.
//
// Scope, deliberately: MCP-server sync ships for all four providers below.
// Skill sync ships for Claude Code ONLY — Codex/Cursor/Gemini CLI's native
// skill-file formats (if any) aren't confirmed with enough confidence to
// sync blindly; each adapter's SupportsSkillSync reports this, and the
// create-agent UI disables the skills panel for the other three rather
// than silently no-op. Every adapter's AuthStatusCommand/ParseAuthStatus
// (used by the "Verify login" action) and MCP config file shape are
// best-effort based on each CLI's documented conventions as of this
// writing — NEEDS RUNTIME CONFIRMATION against a real logged-in CLI inside
// the actual sandbox image before shipping; confidence level is noted on
// each adapter. Claude Code's is confirmed directly (see claude_code.go):
// an earlier version of this design used a guessed
// ~/.claude/.credentials.json file-existence probe, which turned out not
// to reflect real login state at all — `claude auth login` (the actual
// subcommand; a bare `claude login` doesn't exist and fails silently-ish
// with a generic error) never created that file, so every login looked
// unverified even after a real, working login. `claude auth status`
// (confirmed: prints JSON with a top-level "loggedIn" boolean, no network
// call, safe to run on every click) is the CLI's own authoritative answer
// instead of a guess about its internals.
package providercli

import "github.com/Paca-AI/agent-runner/internal/agent"

// sandboxHomeDir is $HOME inside the sandbox/environment container —
// mirrored, not imported, from executor.sandboxWorkdir (this package is
// imported BY executor, so importing back would cycle; see that const's
// own doc comment for why /home/goose, not environmentHomeRoot, is the
// right value here). Adapters use this to build absolute argv for
// AuthStatusCommand.
const sandboxHomeDir = "/home/goose"

// FileEntry is one file to write into a CLI's own config directory,
// relative to $HOME — not to Adapter.HomeDirName(): the caller's bootstrap
// step already symlinks that one directory onto the environment's
// persistent volume (see executor.syncProviderCLIConfig), so every path
// here is written straight through the real $HOME and the symlink takes
// care of persistence. Mirrors executor/skills.go's tar-writing shape,
// generalized (via executor.buildFileTar) so both Goose's own
// .agents/skills layout and every Adapter's own config format share one
// tar-writing helper instead of four near-duplicates of it.
type FileEntry struct {
	RelPath string
	Content string
}

// Adapter describes one CLI provider's on-disk config conventions.
type Adapter interface {
	// Name is the cli_provider value this adapter handles (matches
	// services/api's agentdom.CLIProvider* constants).
	Name() string
	// HomeDirName is this CLI's own config-home directory name, relative
	// to $HOME (e.g. ".claude") — the caller's bootstrap step symlinks
	// this onto the environment's persistent volume so login state
	// survives container recreation (a Docker "Restart" or any k8s Pod
	// recreate wipes everything NOT on that volume).
	HomeDirName() string
	// APIKeyEnvVar returns the env var this CLI reads for non-interactive
	// API-key auth, and whether one is known to exist at all — this is
	// each CLI's OWN native mechanism, independent of Goose's own
	// provider/API-key plumbing, which doesn't apply once GOOSE_PROVIDER
	// names a CLI provider (see the package doc comment).
	APIKeyEnvVar() (envVar string, ok bool)
	// AuthStatusCommand returns argv for a one-shot, non-interactive,
	// side-effect-free command that determines whether this CLI is
	// currently authenticated — run via ExecEnvironment (no shell, no
	// stdin). Deliberately never a real prompt/print invocation: that
	// would burn API usage, could hang on an interactive permission
	// prompt, and depends on protocol details that vary release to
	// release. Two shapes in practice: a real local status subcommand
	// (e.g. `claude auth status`), authoritative wherever a CLI has one;
	// or, for a CLI with no confirmed equivalent yet, a plain
	// `test -f <absolute-credential-file-path>` existence probe — command
	// argv rather than just a path so both shapes fit the same method.
	AuthStatusCommand() []string
	// ParseAuthStatus interprets AuthStatusCommand's result (exit code and
	// combined stdout+stderr). For a plain `test -f` probe this is just
	// `exitCode == 0`; for a real status subcommand it parses that
	// command's own output.
	ParseAuthStatus(exitCode int, output string) bool
	// SupportsSkillSync reports whether SyncFiles writes skill files at
	// all — see the package doc comment on why this is Claude Code only
	// in this version.
	SupportsSkillSync() bool
	// MergeableFiles lists every $HOME-relative path SyncFiles wants to
	// merge into (as opposed to writing standalone, like a skill's own
	// SKILL.md) — the caller reads each one's current content from the
	// container before calling SyncFiles, so existing user- or CLI-set
	// state in that file survives the sync.
	MergeableFiles() []string
	// SyncFiles renders this CLI's own MCP-server (and, only when
	// SupportsSkillSync is true, skill) config files. existing maps each
	// MergeableFiles() path to its current on-disk content (empty string
	// if the file doesn't exist yet). skills is already filtered to
	// enabled skills with frontmatter ensured (see executor/skills.go's
	// prepareFileSkills) by the caller when SupportsSkillSync is true;
	// nil otherwise — an adapter that doesn't support skill sync should
	// simply ignore this parameter rather than re-checking the flag
	// itself. Returned paths are $HOME-relative, ready for
	// executor.buildFileTar.
	SyncFiles(existing map[string]string, mcpServers []agent.MCPServer, skills []agent.Skill) ([]FileEntry, error)
}

var registry = map[string]Adapter{}

// Register adds a to the registry, keyed by its own Name(). Called from
// each adapter file's init().
func Register(a Adapter) { registry[a.Name()] = a }

// Get returns the registered adapter for name, if any.
func Get(name string) (Adapter, bool) {
	a, ok := registry[name]
	return a, ok
}
