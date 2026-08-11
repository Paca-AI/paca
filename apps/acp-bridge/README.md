# paca-acp-bridge

A small local daemon that connects an **ACP-type** Paca AI agent to a coding
CLI running on your own machine — Claude Code, Codex, Gemini CLI,
[Goose](https://github.com/block/goose), or a custom
[Agent Client Protocol](https://docs.openhands.dev/sdk/guides/agent-acp)
server. Run it from your project's source directory; it spawns the ACP server
there and streams the conversation back to Paca. Nothing is cloned into a
cloud sandbox and no source code leaves your machine — Paca only ever sends
task requests and receives responses back.

## Prerequisites

- Python 3.12+ (only needed if you don't use `uvx`, which manages this for you)
- Node.js (most built-in ACP providers are launched via `npx`)
- Your own local auth for whichever provider you pick — e.g. run
  `claude setup-token` for Claude Code, export `OPENAI_API_KEY` /
  `GEMINI_API_KEY` for Codex/Gemini CLI, or run `goose configure` (or export
  `GOOSE_PROVIDER`/the provider's own API key env var) for Goose. This is
  entirely your own local setup, exactly as if you were running that CLI
  yourself — Paca never sees, stores, or forwards this. Likewise, any MCP
  servers or skills you've configured for your local ACP client are used
  as-is; Paca doesn't manage or inject any of that.

## Run it

No install step needed — [uv](https://docs.astral.sh/uv/) fetches and runs it
in one shot:

```sh
cd /path/to/your/project
uvx paca-acp-bridge run \
  --agent-id <agent-id> \
  --token <token> \
  --server https://your-paca-instance.example.com
```

(Or `uv pip install paca-acp-bridge` first, then run `paca-acp-bridge run ...` the same way.)

`--agent-id`, `--token`, and `--server` can also be set via
`PACA_ACP_AGENT_ID`, `PACA_ACP_TOKEN`, and `PACA_ACP_SERVER`. The agent id and
token are shown once when you generate (or regenerate) the local bridge token
for an ACP agent in Paca's Agents UI — copy the run command shown there.

By default the ACP server operates on your current directory; pass
`--workspace <path>` to point at a different one.

## Tools, MCP servers, skills, and git access

ACP agents don't take Paca-managed tool/MCP/skill configuration — the ACP
server you run owns all of that, using your own local setup. This also means
it has full, native access to whatever git/`gh` credentials you already have
configured locally, so it can clone, commit, push, and open pull requests
exactly as if you were driving it yourself in a terminal.

Since Paca doesn't inject anything into an ACP conversation, making your
agent Paca-aware — so it uses Paca's MCP tools and task/doc workflow instead
of local TODOs and stray markdown files — means installing that yourself
into whichever CLI you're running. Paca's Agents UI walks through this for
your specific agent (bridge install, skill install, MCP connection), or run
it directly from a terminal:

```sh
PACA_API_URL=<your-paca-url> \
  curl -fsSL https://raw.githubusercontent.com/Paca-AI/paca/master/scripts/install-paca-skills.sh | bash
```

`PACA_API_URL` is required here — the installer fetches skill content from
that instance's API (matching the exact version it's running) rather than
GitHub. `PACA_API_KEY` (from Paca → Settings → API Keys) is optional, but
also pulls in skills contributed by any plugins enabled on that instance.
Run this from the same project directory you'll run the ACP bridge from —
Claude Code and Gemini CLI pick the skills up globally either way, but
Codex (and anything else that reads `AGENTS.md`) only sees them if the
installer's `AGENTS.md` output landed in that project. See
[docs/guides/install-skills.md](../../docs/guides/install-skills.md) for
details.

## How it works

This daemon runs the OpenHands SDK's `ACPAgent` in its default local mode —
the same as the SDK's own quickstart example — which spawns your chosen ACP
CLI as a real local subprocess against the current directory. Conversation
events stream back to Paca over an authenticated WebSocket
(`/agent-bridge/ws` on your Paca instance) and are stored the same way as any
other agent conversation, so the chat UI works identically regardless of
where the agent actually ran.

Keep this process running for as long as you want the agent to be reachable
from Paca — it reconnects automatically on a dropped connection.

### Assistant text arrives in position

`ACPAgent` streams tool calls as they happen, but buffers the agent's own
text for the entire turn and persists it only at the end, inside the
`FinishAction` of the turn's closing event. On its own that means everything
the agent *said* shows up after everything it *did*.

The daemon subscribes to the conversation's token callbacks and forwards each
run of buffered text as a `MessageEvent` just before whatever event came next,
so narration is interleaved with the tool calls it describes. The turn's
closing `FinishAction`/`FinishObservation` pair is built from the join of
those same chunks, so each is blanked when it would be a trailing duplicate
of what already streamed — the text is still persisted, in the events it was
split into. "Trailing", not just exact, because a turn retried in place after
a transient ACP connection error resets the SDK's own accumulated text but
not this daemon's buffer, so the surviving attempt's text is still an exact
duplicate of the tail of what streamed, not necessarily the whole thing.
