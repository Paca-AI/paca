# Galaxy-Paca — production deploy (pilot)

Fork của [Paca-AI/paca](https://github.com/Paca-AI/paca) (Apache-2.0), branch làm việc `galaxy-main` (bám tag upstream, hiện tại `v0.9.7`). Xem ADR-038.

## Topology

- Stack compose tên `galaxy-paca`, không mở port ra host.
- Caddy gateway join `galaxy_network` với alias `paca-gateway`.
- Cloudflare tunnel routes `tasks.skyplatform.net` → `http://paca-gateway:80` (ingress được quản lý trên Cloudflare dashboard/API, không phải file trên host).
- TLS ở Cloudflare edge; nội bộ plain HTTP (`SITE_ADDRESS=:80`).
- `ai-agent` **scale 0** (giữ nguyên cho đến khi quyết định bật). Khi bật: Docker đi qua `socket-proxy` (`DOCKER_HOST=tcp://socket-proxy:2375`, KHÔNG mount docker.sock trực tiếp) và toàn bộ LLM traffic ép qua platform proxy (`LLM_BASE_URL_OVERRIDE`) — xem ADR-038 P1.
- Backup Postgres hằng đêm 02:00 (giờ VN) vào `/backup/paca-postgres`, giữ 14 ngày.

## Deploy / update

```bash
cd ~/Nexus/Galaxy-Paca && git pull
docker compose \
  --env-file deploy/galaxy/.env.galaxy \
  -f deploy/docker-compose.prod.yml \
  -f deploy/galaxy/docker-compose.galaxy.yml \
  up -d --scale ai-agent=0
```

Lần đầu: `sudo mkdir -p /backup/paca-postgres && sudo chown $(id -u):$(id -g) /backup/paca-postgres`, rồi copy `.env.galaxy.example` → `.env.galaxy` và điền secrets (`openssl rand -hex 32`).

## Verify

```bash
docker compose -p galaxy-paca ps
curl -fsS https://tasks.skyplatform.net/api/healthz
```

## Plugins

- `plugins/com.galaxy.sdd` — **SDD Sensor** (ADR-038 T6): nhúng dashboard SDD sensor (`nexus.8verse.games/sdd-server`) vào Paca qua iframe; v1 frontend-only, không secret, backend chỉ là stub WASM trơ (host bắt buộc phải có file).
- `plugins/com.galaxy.analytics` — **Analytics** (ADR-038 P3.4): 4 panel agile (Sprint Progress/burndown v1, Velocity, Status Distribution, Sprint Report) — component Module Federation THẬT (không iframe, không chart lib), tự fetch REST same-origin `/api/v1` bằng session của chính user (`credentials: "include"`, phân trang tasks theo cursor `page_size=100`), tính toán client-side + cache 60s; backend cũng là stub WASM trơ. Các phép xấp xỉ "không có history" ghi rõ ở footnote UI — xem `plugins/com.galaxy.analytics/README.md`.

Build + cài prod (mỗi plugin cùng một quy trình):

```bash
cd deploy/galaxy/plugins/<plugin-id>   # com.galaxy.sdd | com.galaxy.analytics
./build.sh
API_KEY=<paca-api-key> ./install-prod.sh
```

Prod dùng named volume (`backend_plugins`/`frontend_plugins`) nên KHÔNG dùng bước copy của `scripts/install-local-plugin.sh` (script đó cho dev bind-mount); `install-prod.sh` tự `docker cp` vào container api rồi đăng ký qua admin API. Gateway hiện **không set CSP** nên không phải allowlist `frame-src`; nếu iframe SDD trắng thì chỉnh phía SENSOR (bỏ `X-Frame-Options` / set `frame-ancestors` cho phép `https://tasks.skyplatform.net`) — chi tiết trong `plugins/com.galaxy.sdd/README.md`.

## Agent skills (AI insights)

- `skills/` — 3 skill PM analyst cho Paca agent (ADR-038 P3.4), chuyển thể từ prompt của PM service (`pm_service/routers/ai_service.py`) sang MCP tools của Paca: `galaxy-sprint-health` (sức khoẻ sprint 🟢/🟡/🔴, tiếng Việt mặc định), `galaxy-triage` (gợi ý epic/importance/tags), `galaxy-estimate` (story points Fibonacci kèm lý do). Gắn **inline theo từng agent** qua UI (agent → Skills tab) hoặc API — runbook trong `skills/README.md`. Prod KHÔNG cấp key LLM riêng cho agent: toàn bộ traffic đã ép qua platform proxy bằng `LLM_BASE_URL_OVERRIDE` (ADR-038, xem mục Topology).

## AI Agents qua platform AI role (ADR-038 T3)

- `GALAXY_AI_ROLE=paca-ai` (`.env.galaxy`) ép TOÀN BỘ LLM traffic của ai-agent qua identity `/ai/v1` với `model=paca-ai` — proxy resolve role qua `ai_role_assignments` (đổi model tại `/nexus/admin` → AI Models, mục Roles, KHÔNG cần redeploy Paca). Không agent nào cầm key LLM thô.
- **Attribution:** mỗi conversation mint một token RS256 tại `POST nexus-identity:8086/internal/mint-service-token` (header `X-Service-Secret` = `GALAXY_INTERNAL_SERVICE_SECRET`, chép từ `~/Nexus/Galaxy-Nexus/runtime/galaxy-shared/*.env` lúc deploy — KHÔNG commit). Claims: `sub=paca-service@galaxy.internal.nexus`, `act_as` = email user kích hoạt (assign/mention/chat, tra `project_members`→`users`), `act_as_agent=paca-ai` → hiện trong `ai_usage_logs` (`on_behalf_of`/`agent_id`).
- **TTL 900s (trần của mint endpoint):** token được SDK gửi vào sandbox MỘT LẦN lúc mở conversation (kể cả chat resume không gửi lại) — conversation chạy quá 15 phút sẽ 401 ở proxy và fail. Trade-off đã ghi nhận; nếu thành vấn đề thật thì cần cơ chế refresh secret phía agent-server.
- **Network:** ai-agent join `galaxy_network` (alias tường minh `paca-ai-agent`; alias ngầm `ai-agent` đã scan không đụng 17/07). Sandbox agent-server TỰ gọi LLM nên cũng phải join — `SANDBOX_EXTRA_NETWORKS=galaxy_network` (connect sau create, network này bị LOẠI khỏi slot primary để `api`/`gateway` vẫn resolve trên stack network; sandbox tên ngẫu nhiên, không alias → không đụng DNS).
- Bật/tắt: `up -d --scale ai-agent=1` / `=0`. Fail-closed: thiếu `GALAXY_INTERNAL_SERVICE_SECRET` khi đã set role → container từ chối start.

## Paca MCP bearer — tool write-backs mang danh user (ADR-038)

Agent trong sandbox gọi Paca API (tạo task, comment, đổi status, …) qua MCP
server built-in. Ở galaxy mode nó chạy **`PACA_AUTH_MODE=bearer`**: mỗi
conversation, ai-agent mint token RS256 THỨ HAI (cùng mint endpoint, cùng
`X-Service-Secret`) với `aud=paca-api`, `act_as` = **Vortex OIDC sub** của
user kích hoạt (KHÔNG phải email — API map principal qua `users.oidc_sub`,
điền lúc user login SSO lần đầu), `act_as_agent=paca-ai`, rồi đưa vào sandbox
qua env `PACA_MCP_TOKEN` trong MCP config (kênh serialize sẵn có, không log).
API verify qua JWKS của `GALAXY_TRUSTED_ISSUER` và cần
`GALAXY_TRUSTED_ISSUER_CLAIMS=galaxy-nexus` (identity đóng dấu iss logic
`galaxy-nexus`, không phải URL — chỉ nới string-so-sánh claim `iss`, chữ ký
vẫn kiểm đúng một bộ JWKS đó).

**Nguyên tắc principal — fail-closed, KHÔNG bot user:**

- Agent chỉ viết được với đúng quyền của user kích hoạt (assign/mention/chat
  → `actor_member_id` → `project_members`→`users.oidc_sub`). Comment/task
  hiện tên user, kèm attribution agent (`act_as_agent`) trong log API.
- **User kích hoạt PHẢI là user SSO** (đã từng đăng nhập Vortex để có
  `oidc_sub`). User local-password hoặc trigger không rõ actor → token mint
  KHÔNG có `act_as` → API 401 → MCP server expose **0 tool** với lỗi nói rõ
  lý do, không retry (latch chống MaxIterations loop). Không có fallback bot
  user, không header impersonation (`AGENT_HEADER_IMPERSONATION=disabled`
  giữ nguyên; cặp `X-API-Key`+`X-Agent-ID` vẫn bị 401 by design).
- **TTL:** xin `conversation_timeout + 10 phút` nhưng identity clamp cứng
  900s (chặn C2, xem `identity_service/api/internal.py`) — conversation chạy
  quá ~15 phút sẽ mất quyền write-back MCP với lỗi 401 rõ ràng (cùng
  trade-off với LLM token ở trên).
- Image sandbox: `deploy/galaxy/agent-server/Dockerfile` overlay bundle MCP
  của fork lên image upstream — build trên prod host, pin qua
  `AGENT_SERVER_IMAGE=galaxy-local/paca-agent-server:<tag>-galaxy.N`, bump
  suffix mỗi lần rebuild.

## Upgrade theo upstream (pin-and-roll)

1. Đọc release notes upstream + `deploy/upgrade.sh` diff của bản mới.
2. `git fetch upstream --tags`, merge/rebase tag mới vào `galaxy-main` (giải conflict với patch Galaxy).
3. Bump cả 5 `PACA_*_IMAGE`/`AGENT_SERVER_IMAGE` trong `.env.galaxy` cùng lúc.
4. Backup thủ công trước khi up: `docker compose -p galaxy-paca exec -T postgres pg_dump -U paca paca | gzip > /backup/paca-postgres/pre-upgrade-$(date +%Y%m%d).sql.gz`
5. `up -d` như trên. Migration nhúng trong API binary, idempotent, tự chạy.

## Restore

```bash
gunzip -c /backup/paca-postgres/<file>.sql.gz | docker compose -p galaxy-paca exec -T postgres psql -U paca -d paca
```
