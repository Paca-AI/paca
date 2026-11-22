# meeting-agent

The LiveKit worker behind Paca's **meeting assistant**: when someone adds a meeting agent to a call, this service joins the room as that agent, transcribes each participant, and proposes tasks and notes for people to approve. It never speaks. Design: [docs/architecture/chat-and-meetings.md](../../docs/architecture/chat-and-meetings.md) ("Meeting agent type", "Forwarded tool calls", "Phase 3b status").

## How it fits

```
API ──CreateDispatch(paca-meeting, {session})──▶ LiveKit ──job──▶ this worker
worker ─POST /internal/meeting-agent/sessions/{id}/bootstrap (worker secret)─▶ API
       ◀── the agent's decrypted LLM/STT keys + a short-lived API key of the agent's account
worker ─audio per participant─▶ segmenter ─▶ STT provider ─▶ captions (data channel) + transcript (API)
worker ─POST /projects/{p}/proposals (the agent's own key)─▶ API ──RPC paca.proposal.show──▶ reviewer's browser
```

The worker holds the LiveKit API secret (it can join any room), so treat it as infrastructure. It authorizes through the API with the agent's own account, never from dispatch metadata, and never holds `ENCRYPTION_KEY`.

## Configuration

| Variable | Meaning |
|---|---|
| `LIVEKIT_URL` | LiveKit signalling URL the worker connects to (`ws://livekit:7880`). A trailing `/` is added if missing |
| `LIVEKIT_API_KEY` / `LIVEKIT_API_SECRET` | The key pair LiveKit was started with |
| `PACA_API_URL` | The API including `/api/v1` (`http://api:8080/api/v1`) |
| `MEETING_AGENT_TOKEN` | Must equal the API's `MEETING_AGENT_TOKEN`; assistants are off in the API until it is set |
| `MEETING_AGENT_NAME` | Name registered with LiveKit (default `paca-meeting`; the API dispatches to this name) |

Each agent's own settings (reasoning model and key, transcription provider, model and key, languages, mode, retention) are set in the app, on the meeting agent's page, not here.

## Providers

- **Transcription:** `openai` (also any OpenAI-compatible endpoint, such as a self-hosted Whisper, through the agent's endpoint setting) and `deepgram`. One request per utterance, so captions appear when a turn ends.
- **Reasoning:** Anthropic (`anthropic`) over the Messages API, and everything else over OpenAI-compatible chat completions (`openai`, `groq`, `deepseek`, `gemini`, `fireworks_ai`, `cerebras`, or any provider with a base URL).

## Run

```bash
npm ci
npm test            # typecheck + 33 unit tests
npm run lint
npm run build && npm start
```

With Compose: `docker compose -f deploy/docker-compose.dev.yml --profile meeting up --build meeting-agent`.

### Smoke test against a real LiveKit

`npm run smoke` starts fake Paca API, STT and LLM servers on `localhost:18080`, dispatches the worker into a fresh room, speaks (a tone) from a second participant and checks the caption, the proposal RPC, the transcript flush and the notes message. Start the worker first, pointing `PACA_API_URL` at `http://localhost:18080/api/v1` and `MEETING_AGENT_TOKEN` at anything, with `LIVEKIT_*` matching the server:

```bash
LIVEKIT_URL=ws://localhost:3000/livekit LIVEKIT_API_KEY=devkey LIVEKIT_API_SECRET=... \
PACA_API_URL=http://localhost:18080/api/v1 MEETING_AGENT_TOKEN=smoke npm start
npm run smoke   # same LIVEKIT_* variables
```

Give a freshly started worker about ten seconds to register before dispatching.

## Notes

- Node 22+. The image is glibc-based (`node:22-bookworm-slim`) because `@livekit/rtc-node` ships glibc binaries; `node_modules` is large (about 450 MB with `@livekit/agents`).
- The agents CLI reads `LIVEKIT_URL`, `LIVEKIT_API_KEY` and `LIVEKIT_API_SECRET` itself and lets them override the options in code, so `src/index.ts` writes the normalised values back into the environment.
- The agents library listens on port 8081 for health checks.
