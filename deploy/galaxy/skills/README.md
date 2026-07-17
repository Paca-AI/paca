# Galaxy AI insight skills for Paca agents (ADR-038 P3.4)

Three agent skills that turn the Paca agent into a lightweight PM analyst,
adapted from the Galaxy PM service's AI endpoints
(`Galaxy-AI-Project-Management/pm_service/routers/ai_service.py`:
sprint-health / triage / estimate) onto **Paca's data model** — the agent
reads tasks/sprints/statuses through its own `paca` MCP tools instead of
receiving JSON payloads, and writes results back with `update_task`.

| Skill | What it does | Slash |
|---|---|---|
| [`galaxy-sprint-health`](galaxy-sprint-health/SKILL.md) | Đánh giá sức khoẻ sprint 🟢/🟡/🔴 từ dữ liệu task + sprint hiện tại: 3 câu (tiến độ, rủi ro/quá tải, một khuyến nghị) + dòng verdict. Tiếng Việt mặc định. | `/galaxy-sprint-health` |
| [`galaxy-triage`](galaxy-triage/SKILL.md) | Phân loại task mới: gợi ý epic/parent, importance (bucket số của Paca), tags từ vocabulary sẵn có; đề xuất → xác nhận → `update_task`. | `/galaxy-triage` |
| [`galaxy-estimate`](galaxy-estimate/SKILL.md) | Ước lượng story points Fibonacci (1,2,3,5,8,13,21) kèm độ tin cậy + lý do một câu, hiệu chuẩn theo task done của chính dự án. | `/galaxy-estimate` |

All three are **bilingual by design**: they answer in Vietnamese by default
and mirror the requester's language when it clearly isn't Vietnamese. They
propose before writing — nothing is mutated without confirmation.

The `galaxy-` prefix keeps them clear of the bundled defaults (`paca-*`) and
of the reserved `paca-trigger-*` names (the API rejects those). They
COEXIST with the defaults: `galaxy-estimate` is the quick structured
estimator, while the default `paca-estimate` remains the deeper workshop
flow. To *replace* a default instead, attach the inline skill under the
default's exact name — on a name collision the agent-configured skill wins
(`merge_skills_by_name`).

## How an admin attaches them (inline skill per agent)

Skills are configured **per agent** (`agent_skills` table). Two ways:

### UI

Project → **Agents** → open the agent → **Skills** tab → add skill with
source **inline** → name it (e.g. `galaxy-sprint-health`), paste the skill
**body**, add the trigger (e.g. `/galaxy-sprint-health`), enable.

### API

```sh
# Body = the SKILL.md content BELOW the frontmatter (see note), JSON-escaped.
BODY=$(awk 'c>=2{print} /^---$/{c++}' galaxy-sprint-health/SKILL.md | jq -Rs .)
curl -X POST "https://tasks.skyplatform.net/api/v1/projects/$PROJECT_ID/agents/$AGENT_ID/skills" \
  -H "X-API-Key: $API_KEY" -H "Content-Type: application/json" \
  -d "{\"skill_name\":\"galaxy-sprint-health\",\"skill_source\":\"inline\",
       \"skill_content\":$BODY,\"triggers\":[\"/galaxy-sprint-health\"]}"
```

Repeat per skill and per agent. Notes that matter (verified in
`services/ai-agent/src/agent/builder.py`):

- **Paste the body, not the frontmatter.** Inline skills take their name and
  triggers from the DB columns; `skill_content` is injected verbatim, so
  leading `--- name: ... ---` would just leak metadata text into the prompt.
  The frontmatter in these files exists so the same directories can later be
  dropped into `services/ai-agent/src/skills/` as bundled defaults (that path
  parses it; requires an ai-agent image rebuild).
- **Triggers**: with a trigger list the skill is on-demand and
  `/{skill_name}` is auto-added as a keyword; with an empty list the whole
  content is always-active in every conversation (costs context — prefer
  triggers for these three; the model can also self-select them by
  description).
- Names must be unique per agent (duplicates hard-error at conversation
  start) and must not use the `paca-trigger-` prefix (API-rejected).

## No per-agent LLM keys in prod (ADR-038 T3)

On the `galaxy-paca` prod stack ALL agent LLM traffic is forced through the
platform AI proxy: `deploy/galaxy/docker-compose.galaxy.yml` sets
`LLM_BASE_URL_OVERRIDE` (and the ai-agent's builder then **ignores** each
agent's stored `llm_base_url`; `LLM_API_KEY_OVERRIDE` likewise replaces
per-agent keys when set). So when configuring an agent for these skills you
do NOT hand it an OpenAI/Anthropic key — leave the per-agent credentials
empty, pick the model role, and the platform proxy does auth, routing and
usage attribution centrally. See `deploy/galaxy/README.md` and ADR-038.
