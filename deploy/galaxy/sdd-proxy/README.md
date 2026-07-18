# sdd-proxy

Same-origin data proxy that lets the **native** `com.galaxy.sdd` Paca plugin
read SDD fleet telemetry without an iframe and without shipping any secret to
the browser (ADR-038).

```
browser (Paca session cookie)
   │  GET /sdd-api/team/overview        (same origin: tasks.skyplatform.net)
   ▼
paca-edge (Caddy)  handle_path /sdd-api/*  ──►  sdd-proxy:8791
   │
   ├─ 1. session gate:   Cookie ─►  GET api:8080/api/v1/users/me   (2xx? else 401)
   ├─ 2. service token:  POST nexus-identity:8086/internal/mint-service-token
   │                     (X-Service-Secret, iss=galaxy-nexus, RS256, TTL≤900s)
   └─ 3. reverse proxy:  Bearer <token> ─►  GET sdd-server:4830/api/team/overview
                                            └─ JSON streamed back unchanged
```

## Guarantees

- **No session, no data.** Every request is gated behind a live Paca session
  (`/api/v1/users/me`). The proxy holds no user credential of its own.
- **Team-wide READ only.** Only `GET`/`HEAD` pass; the injected token is a
  non-privileged service token, and the human task board lives in Paca now
  (ADR-038 T6). Writes → `405`.
- **No secrets in the browser, none in the logs.** The service secret and the
  minted tokens are never logged.
- **Resilient.** Tokens are cached and refreshed 60 s before expiry. If
  identity is briefly unreachable and `SDD_SHARED_JWT_SECRET` is configured,
  an HS256 token (which SDD's `central/auth.js` also accepts) is minted
  locally as a fallback.

## Config (env)

| var | default | purpose |
|-----|---------|---------|
| `PORT` | `8791` | listen port |
| `SDD_UPSTREAM_URL` | `http://sdd-server:4830` | SDD read API (galaxy_network) |
| `GALAXY_IDENTITY_URL` | `http://nexus-identity:8086` | RS256 mint endpoint |
| `PACA_AUTH_CHECK_URL` | `http://api:8080/api/v1/users/me` | session gate |
| `GALAXY_INTERNAL_SERVICE_SECRET` | — | authenticates the mint (RS256, primary) |
| `SDD_SERVICE_SUB` | `svc-paca-sdd-fleet` | `sub` claim of the service token |
| `SDD_SERVICE_AUD` | `sdd-server` | `aud` claim of the service token |
| `SDD_SHARED_JWT_SECRET` | — | optional HS256 fallback secret |

Wired as the `sdd-proxy` service in `deploy/galaxy/docker-compose.galaxy.yml`
and routed by `deploy/caddy/Caddyfile` (`handle_path /sdd-api/*`).
