// Package executor runs one llm-type agent conversation end to end: spawn
// a sandbox, drive it over ACP, tear it down. Go analog of
// services/ai-agent/src/agent/executor.py's run_conversation.
package executor

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/acp"
	"github.com/Paca-AI/agent-runner/internal/agent"
	"github.com/Paca-AI/agent-runner/internal/chatsandbox"
	"github.com/Paca-AI/agent-runner/internal/sandbox"
	"github.com/Paca-AI/agent-runner/internal/secret"
)

// defaultMaxToolCalls caps a turn when an agent has no MaxIterations
// configured (<= 0). Goose enforces no turn cap of its own — confirmed in
// the protocol spike, where a non-converging reply produced 600+ tool-call
// cycles in 20 seconds with no backoff — so "unconfigured" must fail safe
// to a bounded default, not to unlimited. See docs/ai-agent/goose-migration.md.
const defaultMaxToolCalls = 50

// defaultTimeoutMinutes bounds a turn when an agent has no TimeoutMinutes
// configured (<= 0) — matches the agents table's own DB default
// (`timeout_minutes INTEGER NOT NULL DEFAULT 30`), so in practice this is
// only a safety net for malformed data, not the normal path.
//
// This exists because of a real, live bug found while building the sandbox
// image: a wrong mcpServers wire format made a real goose serve's
// session/new hang forever rather than return an ACP error (see
// docs/ai-agent/goose-migration.md). acp.Client's calls have no timeout of
// their own — only whatever context.Context the caller supplies — so
// without this, a *different* future downstream failure (a stuck MCP
// subprocess, a wedged provider) could hang a real conversation, and the
// container and goroutine driving it, indefinitely.
const defaultTimeoutMinutes = 30

// sandboxWorkdir is the ACP session's cwd. Must be a directory the
// container's own user can write to and spawn subprocesses from — /root is
// a trap on the stock goose image (uid 1000, user "goose"), confirmed in
// the same spike. /home/goose is that user's home directory.
const sandboxWorkdir = "/home/goose"

// Options holds service-level settings shared across every conversation
// this process runs — the Go analog of services/ai-agent's config.Settings,
// scoped to just the fields the executor itself needs.
type Options struct {
	// Image is the pinned goose image reference — see
	// docs/ai-agent/goose-migration.md's "no current pinned Goose image
	// tag" open risk; callers should pass a digest- or tag-pinned
	// reference, not a floating one.
	Image string
	// PacaAPIKey/PacaAPIURL/PacaGatewayURL configure the built-in Paca MCP
	// server appended to every conversation's MCP server list — mirrors
	// build_mcp_config's "paca" entry in builder.py. Leave PacaAPIKey empty
	// to omit the Paca MCP server entirely (matches builder.py's
	// `if settings.paca_api_key:` gate).
	PacaAPIKey     string
	PacaAPIURL     string
	PacaGatewayURL string
	// MCPDevSourceDir, when set, is forwarded to every sandbox.Config so the
	// Paca MCP server runs from a local apps/mcp checkout instead of the
	// image's globally npm-installed @paca-ai/paca-mcp — see
	// config.Settings.MCPDevSourceDir and buildMCPServers.
	MCPDevSourceDir string
}

// Executor runs conversations for one process — holds the shared sandbox
// manager and secret decryptor, not any per-conversation state (mirrors
// sandbox.Manager: safe to call Run concurrently for different
// conversations, since each gets its own container and acp.Client).
type Executor struct {
	sandboxMgr *sandbox.Manager
	encryptor  *secret.Encryptor
	opts       Options
	log        *slog.Logger
}

// New builds an Executor sharing the given sandbox manager and decryptor
// across every conversation it runs.
func New(sandboxMgr *sandbox.Manager, encryptor *secret.Encryptor, opts Options, log *slog.Logger) *Executor {
	return &Executor{sandboxMgr: sandboxMgr, encryptor: encryptor, opts: opts, log: log}
}

// Result is what one turn produced. Handle/Client/SessionID are always
// populated whenever a sandbox was actually reached (even on a later
// error) — see Run's doc comment on why Run itself no longer decides
// whether to tear the sandbox down.
type Result struct {
	StopReason string
	Handle     *sandbox.Handle
	Client     *acp.Client
	SessionID  string
}

// Run drives one ACP turn — a cold-started sandbox by default, or an
// existing one reattached via resume — mirrors executor.py's
// run_conversation's overall shape (build LLM/skills/MCP config, spin up or
// reattach to a workspace, send a message, run), adapted for Goose's
// ACP-over-HTTP transport instead of the OpenHands SDK.
//
// Unlike earlier versions of this method, Run does NOT tear the sandbox
// down itself, on success or on error — ownership of that decision moved to
// the caller (handler.Handle) once chat conversation continuity required
// it: whether to keep a sandbox alive for the conversation's next reply
// depends on whether the trigger is chat-type and, if it was interrupted,
// whether that was a pause or a full stop — information Run itself doesn't
// have. The caller must call StopSandbox once it decides teardown is
// appropriate. See docs/ai-agent/goose-migration.md's chat-continuity
// section.
//
// resume, when non-nil, mirrors run_conversation's resume_state branch:
// reattach to resume.Handle/resume.Client/resume.SessionID instead of
// starting a new container and ACP session, and send only trigger.Message
// as the turn's prompt (the agent already has full context from earlier
// turns) instead of the full system-prompt/skills/trigger-context message
// buildInitialMessage assembles for a cold start.
//
// onEvent is called for every session/update notification during the turn
// — the caller is responsible for translating each into an
// AgentConversationEvent and publishing/persisting it, the same role
// make_event_callback plays in executor.py. Not done inside this package so
// Run stays testable without a live Valkey/Postgres connection.
func (e *Executor) Run(ctx context.Context, cfg agent.Config, trigger agent.Trigger, resume *chatsandbox.State, onEvent func(acp.Event)) (Result, error) {
	timeoutMinutes := cfg.TimeoutMinutes
	if timeoutMinutes <= 0 {
		timeoutMinutes = defaultTimeoutMinutes
	}
	// Bounds Initialize/NewSession/Prompt specifically, not sandboxMgr.Start
	// below (which has its own internal ready-timeout, see sandbox.go's
	// readyTimeout) — this is deliberately scoped to the ACP protocol calls,
	// which is where the hang defaultTimeoutMinutes's doc comment describes
	// actually happened.
	turnCtx, cancelTurn := context.WithTimeout(ctx, time.Duration(timeoutMinutes)*time.Minute)
	defer cancelTurn()

	var handle *sandbox.Handle
	var client *acp.Client
	var sessionID string
	var message string

	if resume != nil {
		handle, client, sessionID = resume.Handle, resume.Client, resume.SessionID
		message = trigger.Message
	} else {
		var err error
		handle, client, sessionID, err = e.coldStart(ctx, turnCtx, cfg, trigger)
		if err != nil {
			return Result{Handle: handle}, err
		}
		message = buildInitialMessage(cfg, trigger)
	}

	maxToolCalls := cfg.MaxIterations
	if maxToolCalls <= 0 {
		maxToolCalls = defaultMaxToolCalls
	}

	stopReason, err := client.Prompt(turnCtx, sessionID, []acp.ContentBlock{acp.TextBlock(message)}, maxToolCalls, e.attachDiffs(turnCtx, handle, onEvent))
	result := Result{StopReason: stopReason, Handle: handle, Client: client, SessionID: sessionID}
	if err != nil {
		return result, fmt.Errorf("executor: acp session/prompt: %w", err)
	}
	return result, nil
}

// StopSandbox tears down a sandbox previously returned in a Result's Handle
// — a thin pass-through to the sandbox.Manager this Executor was built
// with, so callers (handler.Handle) don't need their own reference to it
// just to finish what Run used to do unconditionally.
func (e *Executor) StopSandbox(ctx context.Context, h *sandbox.Handle) error {
	return e.sandboxMgr.Stop(ctx, h)
}

// coldStart spins up a fresh sandbox and completes the ACP handshake for a
// non-resumed turn — everything Run used to do inline before chat
// continuity split "start" from "decide whether to keep alive or stop".
// ctx bounds sandboxMgr.Start (unaffected by the turn timeout); turnCtx
// bounds Initialize/NewSession, same as it bounds Prompt in Run.
func (e *Executor) coldStart(ctx, turnCtx context.Context, cfg agent.Config, trigger agent.Trigger) (*sandbox.Handle, *acp.Client, string, error) {
	apiKey, err := e.encryptor.Decrypt(cfg.LLMAPIKeySecret)
	if err != nil {
		return nil, nil, "", fmt.Errorf("executor: decrypt llm api key: %w", err)
	}

	gooseProvider, apiKeyEnvVar := resolveProviderEnv(cfg.LLMProvider)
	containerEnv := map[string]string{
		"GOOSE_PROVIDER": gooseProvider,
		"GOOSE_MODEL":    cfg.LLMModel,
		apiKeyEnvVar:     apiKey,
	}
	if cfg.LLMBaseURL != "" {
		// Only meaningful for the openai-routed fallback path (see
		// resolveProviderEnv) — OPENAI_HOST is the env var confirmed
		// against a real goose serve in the protocol spike. A custom base
		// URL for a *named* provider (e.g. a private Anthropic-compatible
		// gateway) isn't wired here; extend this once that case is real.
		containerEnv["OPENAI_HOST"] = cfg.LLMBaseURL
	}

	for _, ev := range cfg.EnvVars {
		val, err := e.encryptor.Decrypt(ev.EncryptedValue)
		if err != nil {
			return nil, nil, "", fmt.Errorf("executor: decrypt env var %q: %w", ev.Key, err)
		}
		containerEnv[ev.Key] = val
	}

	gitName := cfg.GitCommitterName
	if gitName == "" {
		gitName = "paca-agent"
	}
	gitEmail := cfg.GitCommitterEmail
	if gitEmail == "" {
		gitEmail = "280579135+paca-agent@users.noreply.github.com"
	}

	handle, err := e.sandboxMgr.Start(ctx, sandbox.Config{
		ConversationID:    trigger.ConversationID.String(),
		Image:             e.opts.Image,
		Env:               containerEnv,
		GitCommitterName:  gitName,
		GitCommitterEmail: gitEmail,
		MCPDevSourceDir:   e.opts.MCPDevSourceDir,
	})
	if err != nil {
		return nil, nil, "", fmt.Errorf("executor: start sandbox: %w", err)
	}

	client := acp.NewClient(handle.BaseURL, handle.SecretKey, nil)
	if err := client.Initialize(turnCtx); err != nil {
		return handle, nil, "", fmt.Errorf("executor: acp initialize: %w", err)
	}

	sessionID, err := client.NewSession(turnCtx, sandboxWorkdir, e.buildMCPServers(trigger, cfg))
	if err != nil {
		return handle, nil, "", fmt.Errorf("executor: acp session/new: %w", err)
	}

	return handle, client, sessionID, nil
}

// pacaMCPBinPath is the absolute path to the Paca MCP server's own
// executable inside the pinned Goose sandbox image
// (services/agent-server/Dockerfile) — `npm install -g @paca-ai/paca-mcp`
// creates this symlink automatically from the package's own `bin` field
// ({"paca": "./build/index.js"}), since that Dockerfile's npm prefix is
// /usr. ACP's schema requires McpServerStdio.command to be an absolute
// path ("Absolute path to the MCP server executable"), not a bare name
// resolved via PATH lookup. Confirmed by inspecting that exact image; if
// the image's Node install or the package's bin name ever moves, this
// must move with it.
//
// Deliberately NOT invoked via `npx -y @paca-ai/paca-mcp` (an earlier
// version of this constant pointed at /usr/bin/npx with those args) —
// measured directly inside a real sandbox container: npx added ~1.2s of
// its own overhead on top of this binary's own ~1.4s Node/import startup
// cost, entirely from npx's registry version-check, even though the
// package is already installed globally and npx never needed to install
// anything. That's real latency on every single conversation's cold
// start, for a check that can never find anything to do here — the image
// is rebuilt to update this package's version, not resolved freshly per
// session. See docs/ai-agent/goose-migration.md.
const pacaMCPBinPath = "/usr/bin/paca"

// buildMCPServers mirrors builder.py's build_mcp_config: the agent's own
// configured servers first, with the built-in Paca MCP server always
// appended last so it can't be overridden by a same-named user entry.
//
// The wire shape here is verified against ACP's real schema.json
// ($defs.McpServer — a Rust internally-tagged enum requiring a "type"
// discriminator, with env as an array of {name,value} pairs, not a JSON
// object) — an earlier, hand-guessed version of this function that omitted
// both made goose serve's session/new hang forever rather than return an
// error. See docs/ai-agent/goose-migration.md.
//
// User-configured "oauth"-transport servers (agent.MCPServer.Transport ==
// "oauth", the fourth value the agents_mcp_servers DB CHECK allows) have no
// direct equivalent in ACP's three-way stdio/http/sse enum and are skipped
// entirely for now — mapping OAuth to an http entry's bearer-token header
// wasn't attempted this pass.
func (e *Executor) buildMCPServers(trigger agent.Trigger, cfg agent.Config) []acp.MCPServerConfig {
	servers := make([]acp.MCPServerConfig, 0, len(cfg.MCPServers)+1)
	for _, s := range cfg.MCPServers {
		if !s.IsEnabled {
			continue
		}
		switch s.Transport {
		case "stdio":
			args := s.Args
			if args == nil {
				args = []string{}
			}
			userEnv := envMapToList(s.Env)
			servers = append(servers, acp.MCPServerConfig{
				Type:    acp.McpServerStdio,
				Name:    s.ServerName,
				Command: s.Command,
				Args:    &args,
				Env:     &userEnv,
			})
		case "http":
			servers = append(servers, acp.MCPServerConfig{
				Type:    acp.McpServerHTTP,
				Name:    s.ServerName,
				URL:     s.URL,
				Headers: &[]acp.HTTPHeader{},
			})
		case "sse":
			servers = append(servers, acp.MCPServerConfig{
				Type:    acp.McpServerSSE,
				Name:    s.ServerName,
				URL:     s.URL,
				Headers: &[]acp.HTTPHeader{},
			})
			// "oauth": skipped — see doc comment above.
		}
	}

	if e.opts.PacaAPIKey == "" {
		return servers
	}
	env := map[string]string{
		"PACA_API_KEY":     e.opts.PacaAPIKey,
		"PACA_API_URL":     e.opts.PacaAPIURL,
		"PACA_GATEWAY_URL": e.opts.PacaGatewayURL,
		"PACA_AGENT_ID":    cfg.ID.String(),
	}
	if trigger.ProjectID != uuid.Nil {
		env["PACA_PROJECT_ID"] = trigger.ProjectID.String()
	}
	if trigger.ActorMemberID != nil {
		env["PACA_ACTOR_USER_ID"] = trigger.ActorMemberID.String()
	}
	if len(trigger.RepoPluginIDs) > 0 {
		// Read by apps/mcp's repo-tools.ts to scope list_repositories and
		// gate whether the repo tool set is shown to the agent at all — see
		// PacaConfig.repoPluginIds's doc comment there.
		env["PACA_REPO_PLUGIN_IDS"] = strings.Join(trigger.RepoPluginIDs, ",")
	}
	// Dev override: run the Paca MCP server from a locally-mounted apps/mcp
	// checkout (see sandbox.Config.MCPDevSourceDir) instead of the image's
	// globally npm-installed @paca-ai/paca-mcp, so a local source change is
	// live on the next conversation without an npm publish + image rebuild.
	// /usr/bin/node is the same absolute-path requirement pacaMCPBinPath's
	// doc comment explains — ACP rejects a bare command name resolved via
	// PATH lookup.
	command, args := pacaMCPBinPath, []string{}
	if e.opts.MCPDevSourceDir != "" {
		command = "/usr/bin/node"
		args = []string{sandbox.MCPDevMountPath + "/build/index.js"}
	}
	pacaEnv := envMapToList(env)
	servers = append(servers, acp.MCPServerConfig{
		Type:    acp.McpServerStdio,
		Name:    "paca",
		Command: command,
		Args:    &args,
		Env:     &pacaEnv,
	})
	return servers
}

func envMapToList(env map[string]string) []acp.EnvVariable {
	out := make([]acp.EnvVariable, 0, len(env))
	for k, v := range env {
		out = append(out, acp.EnvVariable{Name: k, Value: v})
	}
	return out
}
