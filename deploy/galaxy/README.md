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
