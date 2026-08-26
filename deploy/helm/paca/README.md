# paca

Paca — self-hosted AI-powered project management, deployed on Kubernetes.

This chart mirrors [`deploy/docker-compose.prod.yml`](https://github.com/Paca-AI/paca/blob/master/deploy/docker-compose.prod.yml)
component-for-component: PostgreSQL, Valkey (Redis-compatible), MinIO
(S3-compatible), the Go API, the React web app served via Caddy, a
Socket.IO realtime hub, an AI agent runner, and a Caddy gateway in front
of all of it.

For deployment details that go beyond this chart's configuration
reference — the Kubernetes AI agent sandbox, its RBAC scoping, storage
class requirements, and TLS/ingress guidance — see the full
[Kubernetes deployment guide](https://github.com/Paca-AI/paca/blob/master/deploy/helm/README.md).

## Prerequisites

- Kubernetes 1.24+
- Helm 3.8+ (OCI registry support)
- A `ReadWriteMany`-capable StorageClass if you want `api.plugins.persistence`
  enabled (installed plugins shared between the `api` and gateway Pods) —
  see the deployment guide linked above

## Installing

This chart is published as an OCI artifact, not a traditional `index.yaml`
repo — install it directly by reference, no `helm repo add` needed:

```console
helm install paca oci://ghcr.io/paca-ai/charts/paca --version <version>
```

Every value under `secrets.*` is required for a real deployment (there's
no auto-generated default) — set them explicitly or point
`secrets.existingSecret` at a Secret you manage yourself. See the
Configuration table below and the deployment guide for how to generate
them.

## Configuration

### Global

| Key | Description | Default |
|---|---|---|
| `publicUrl` | Externally reachable base URL for this deployment (e.g. `https://paca.example.com`). Leave empty for a ClusterIP/NodePort-only deployment. | `""` |
| `imagePullSecrets` | Image pull secrets applied to every Pod this chart creates | `[]` |

### Secrets

Generate strong values yourself, e.g. `openssl rand -hex 32`.

| Key | Description | Default |
|---|---|---|
| `secrets.existingSecret` | Name of a Secret you manage yourself instead of the fields below (same key names — see `templates/secret.yaml`) | `""` |
| `secrets.jwtSecret` | JWT signing secret | `""` |
| `secrets.adminPassword` | Initial admin account password | `""` |
| `secrets.encryptionKey` | 64-char hex string encrypting plugin secrets and (when `agentRunner.enabled`) LLM API keys at rest | `""` |
| `secrets.internalApiKey` | Pre-shared key authenticating `api` <-> `agent-runner`'s internal routes (only required when `agentRunner.enabled`) | `""` |
| `secrets.agentApiKey` | Agent API key | `""` |
| `secrets.postgresPassword` | PostgreSQL password | `""` |
| `secrets.storageAccessKeyId` | Object storage access key ID | `""` |
| `secrets.storageSecretAccessKey` | Object storage secret access key | `""` |

### PostgreSQL (bundled)

| Key | Description | Default |
|---|---|---|
| `postgres.enabled` | Deploy the bundled PostgreSQL. Set `false` and provide `externalDatabaseUrl` to use your own instead. | `true` |
| `postgres.image.repository` / `.tag` | | `postgres` / `16-alpine` |
| `postgres.database` / `.username` | | `paca` / `paca` |
| `postgres.persistence.enabled` / `.size` / `.storageClassName` | | `true` / `10Gi` / `""` |
| `postgres.resources` | Requests/limits for the PostgreSQL Pod | see `values.yaml` |
| `externalDatabaseUrl` | Full connection string used instead of the bundled postgres when `postgres.enabled: false` | `""` |

### Valkey (bundled cache / pub-sub)

| Key | Description | Default |
|---|---|---|
| `valkey.enabled` | Deploy the bundled Valkey. Set `false` and provide `externalRedisUrl` to use your own instead. | `true` |
| `valkey.image.repository` / `.tag` | | `valkey/valkey` / `8-alpine` |
| `valkey.persistence.enabled` / `.size` / `.storageClassName` | | `true` / `5Gi` / `""` |
| `valkey.resources` | Requests/limits for the Valkey Pod | see `values.yaml` |
| `externalRedisUrl` | Used instead of the bundled valkey when `valkey.enabled: false` | `""` |

### Object storage

| Key | Description | Default |
|---|---|---|
| `minio.enabled` | Deploy bundled MinIO. Set `false`, `storage.provider: s3`, and real AWS credentials to use S3 instead. | `true` |
| `minio.image.repository` / `.tag` | | `minio/minio` / `latest` |
| `minio.persistence.enabled` / `.size` / `.storageClassName` | | `true` / `20Gi` / `""` |
| `minio.resources` | Requests/limits for the MinIO Pod | see `values.yaml` |
| `storage.provider` | `minio` or `s3` | `minio` |
| `storage.endpoint` | Empty defaults to the bundled MinIO's in-cluster Service address | `""` |
| `storage.publicUrl` | Empty defaults to `<publicUrl>/storage` | `""` |
| `storage.region` / `.bucket` / `.useSSL` | | `us-east-1` / `paca` / `false` |

### API (Go backend)

| Key | Description | Default |
|---|---|---|
| `api.replicaCount` | | `1` |
| `api.image.repository` / `.tag` / `.pullPolicy` | | `ghcr.io/paca-ai/paca-api` / `latest` / `IfNotPresent` |
| `api.resources` | Requests/limits for the API Pod | see `values.yaml` |
| `api.admin.username` | | `admin` |
| `api.jwt.accessTtl` / `.refreshTtl` / `.refreshSessionTtl` | | `15m` / `168h` / `24h` |
| `api.cookieSecure` | Set `false` only when TLS isn't terminated anywhere in front of this deployment — otherwise browsers silently drop the auth cookie and login never sticks | `true` |
| `api.plugins.persistence.enabled` | Shared storage for installed plugins (backend WASM, frontend/MCP/skills bundles) — needs a `ReadWriteMany` StorageClass; set `false` to fall back to a per-Pod `emptyDir` (installed plugins then won't survive a restart or be visible to the gateway) | `true` |
| `api.plugins.persistence.accessMode` / `.storageClassName` / `.size` | | `ReadWriteMany` / `""` / `5Gi` |

### Web (React SPA via Caddy)

| Key | Description | Default |
|---|---|---|
| `web.enabled` | Disable to serve the SPA from an external CDN instead | `true` |
| `web.replicaCount` | | `1` |
| `web.image.repository` / `.tag` / `.pullPolicy` | | `ghcr.io/paca-ai/paca-web` / `latest` / `IfNotPresent` |
| `web.resources` | Requests/limits for the web Pod | see `values.yaml` |

### Realtime (Socket.IO event hub)

| Key | Description | Default |
|---|---|---|
| `realtime.replicaCount` | | `1` |
| `realtime.image.repository` / `.tag` / `.pullPolicy` | | `ghcr.io/paca-ai/paca-realtime` / `latest` / `IfNotPresent` |
| `realtime.resources` | Requests/limits for the realtime Pod | see `values.yaml` |
| `realtime.logLevel` | | `info` |
| `realtime.corsOrigins` | Empty defaults to `publicUrl` (or `http://localhost` when that's also empty) | `""` |

### Agent Runner (AI agent conversation executor + sandbox)

| Key | Description | Default |
|---|---|---|
| `agentRunner.enabled` | Disable to run Paca without the AI agent feature | `true` |
| `agentRunner.replicaCount` | | `1` |
| `agentRunner.image.repository` / `.tag` / `.pullPolicy` | | `ghcr.io/paca-ai/paca-agent-runner` / `latest` / `IfNotPresent` |
| `agentRunner.resources` | Requests/limits for the agent-runner Pod | see `values.yaml` |
| `agentRunner.allowedAgentIds` | `"*"` for every agent, or a comma-separated list of agent UUIDs to stage a gradual rollout | `"*"` |
| `agentRunner.workerConcurrency` | | `10` |
| `agentRunner.chatSandboxIdleTimeoutMinutes` | | `3` |
| `agentRunner.logLevel` | | `info` |
| `agentRunner.sandbox.backend` | `kubernetes` (one Job per conversation) or `docker` (needs a mounted Docker socket — most managed clusters don't have one) | `kubernetes` |
| `agentRunner.sandbox.image.repository` / `.tag` | | `ghcr.io/paca-ai/paca-agent-server-goose` / `latest` |
| `agentRunner.sandbox.namespace` | Namespace sandbox Jobs/Pods run in. Empty defaults to this release's own namespace — see the deployment guide's RBAC section before changing this | `""` |
| `agentRunner.sandbox.cpuLimit` / `.memoryLimit` | Requests == limits on every sandbox Pod's primary container | `2` / `4Gi` |
| `agentRunner.sandbox.imagePullSecrets` | Needed when `sandbox.image` is pulled from a private registry | `[]` |

### Gateway (Caddy reverse proxy)

| Key | Description | Default |
|---|---|---|
| `gateway.replicaCount` | | `1` |
| `gateway.image.repository` / `.tag` / `.pullPolicy` | | `caddy` / `2-alpine` / `IfNotPresent` |
| `gateway.resources` | Requests/limits for the gateway Pod | see `values.yaml` |
| `gateway.service.type` | `ClusterIP` (pair with `ingress.enabled`) or `LoadBalancer` (standalone, no ingress controller needed) | `ClusterIP` |
| `gateway.service.port` | | `80` |

### Ingress (optional)

| Key | Description | Default |
|---|---|---|
| `ingress.enabled` | | `false` |
| `ingress.className` / `.annotations` | | `""` / `{}` |
| `ingress.host` | Required when `ingress.enabled` | `""` |
| `ingress.tls.enabled` | | `false` |
| `ingress.tls.secretName` | Defaults to `<release-name>-tls` when empty | `""` |

## Source Code

* <https://github.com/Paca-AI/paca>
