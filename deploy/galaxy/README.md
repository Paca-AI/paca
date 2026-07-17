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
