# Deploying Paca on Kubernetes

This chart (`deploy/helm/paca`) deploys the same stack as
[`deploy/docker-compose.prod.yml`](../docker-compose.prod.yml) — postgres,
valkey, minio, api, web, realtime, a Caddy gateway, and agent-runner — as
Kubernetes resources. See that file first if you want the non-Kubernetes
picture of how these pieces fit together; this chart mirrors it
component-for-component rather than introducing a different architecture.

## The AI agent sandbox on Kubernetes

`agent-runner`'s AI agent feature runs one disposable sandbox per active
conversation. On Docker Compose that's a Docker container, reached by
mounting the host's `/var/run/docker.sock` — there's no equivalent on a
normal Kubernetes node. This chart instead runs `agent-runner` with
`SANDBOX_BACKEND=kubernetes`, which creates one **Kubernetes Job** per
conversation instead (see
[`services/agent-runner/internal/sandbox/k8s`](../../services/agent-runner/internal/sandbox/k8s)).
The Job is deleted (cascading to its Pod) when the conversation ends or
goes idle, the same lifecycle the Docker container has today.

This needs a small amount of RBAC, granted automatically by this chart:
`agentRunner`'s ServiceAccount gets a `Role` scoped to exactly the sandbox
namespace (`agentRunner.sandbox.namespace`, defaulting to the release's own
namespace) allowing it to create/get/list/watch/delete `batch/jobs`,
get/list/watch `pods`, and use the `pods/log` and `pods/exec` subresources
(needed to write skill files into a sandbox and compute git diffs after a
turn — see that package's own doc comments for why). No cluster-scoped
permissions, and no access to anything outside that one namespace.

If an agent has `docker_enabled: true` in the database, its sandbox Pod
additionally gets a privileged `docker:dind` sidecar so it can run Docker
commands. That's the same trade-off `docker-compose.prod.yml`'s own
per-conversation sidecar makes, just running inside the sandbox's own Pod
instead of a paired container. A namespace enforcing the **Restricted** Pod
Security Standard will reject that Pod outright — see
`agentRunner.sandbox` in `values.yaml` if you plan to use `docker_enabled`
agents.

## Quick start

```bash
kubectl create namespace paca
helm install paca deploy/helm/paca -n paca -f my-values.yaml
```

At minimum, `my-values.yaml` needs the required secrets `values.yaml`
otherwise refuses to render without (there are no guessable defaults —
generate real ones):

```yaml
publicUrl: "https://paca.example.com"

secrets:
  jwtSecret: "<openssl rand -hex 32>"
  adminPassword: "<a strong password>"
  encryptionKey: "<openssl rand -hex 32>"       # must be exactly 64 hex chars
  internalApiKey: "<openssl rand -hex 32>"
  agentApiKey: "<openssl rand -hex 32>"
  postgresPassword: "<a strong password>"
  storageAccessKeyId: "<minio access key, or leave minioadmin for a first try>"
  storageSecretAccessKey: "<minio secret key>"
```

Then either point DNS at your ingress controller and set
`ingress.enabled: true` with `ingress.host`, or set
`gateway.service.type: LoadBalancer` to expose the gateway directly without
an Ingress controller at all.

## What's bundled vs. external

Every stateful dependency mirrors Compose's own `--scale <service>=0`
pattern — disable the bundled version and point at a managed one instead:

| Component | Bundle toggle | External override |
|---|---|---|
| PostgreSQL | `postgres.enabled: false` | `externalDatabaseUrl` |
| Valkey/Redis | `valkey.enabled: false` | `externalRedisUrl` |
| MinIO/S3 | `minio.enabled: false` + `storage.provider: s3` | `storage.endpoint` (+ storage creds) |

`web.enabled: false` drops the bundled frontend entirely if you're serving
the SPA from a CDN instead — the gateway keeps routing `/api`, `/ws`, and
`/storage` regardless.

## Plugin storage (advanced)

Installed plugins (backend WASM, frontend bundles, MCP bundles, Agent
Skills bundles) are written by the `api` Pod and read back out by the
`gateway` Pod to serve over HTTP — the two need concurrent access to the
same files, which on Kubernetes means a **ReadWriteMany**-capable
StorageClass (`api.plugins.persistence.storageClassName` in
`values.yaml`) — examples: `efs-csi` (AWS), `filestore-csi` (GKE),
`azurefile` (AKS), `nfs-subdir-external-provisioner`, `longhorn`,
`rook-ceph`. Most clusters' *default* StorageClass is block storage
(ReadWriteOnce only) and will fail to bind. If you have no RWX class
available and don't need custom plugins yet, set
`api.plugins.persistence.enabled: false` — the app still runs, just
without a working plugin-install flow.

## Bringing your own Secret

Set `secrets.existingSecret` to a Secret name you manage yourself (e.g. via
a GitOps sealed-secrets/external-secrets pipeline) instead of filling in
`secrets.*` — every template then reads from that Secret's keys instead of
one this chart generates. See `templates/secret.yaml` for the exact key
names required. If `postgres.enabled` is also true in this mode, that
Secret's `POSTGRES_PASSWORD` key must match whatever password your own
`DATABASE_URL` key uses — the bundled postgres StatefulSet reads
`POSTGRES_PASSWORD` directly, so the two have to agree.

## Verifying before you install

```bash
helm lint deploy/helm/paca
helm template paca deploy/helm/paca -f my-values.yaml | less
```

Both run entirely offline — no cluster required — and are worth running
after any `values.yaml` change.

## Troubleshooting

- **A sandbox Job never starts a Pod / stays Pending**: check
  `kubectl describe job -n <sandbox namespace> <job-name>` — usually a
  resource quota, an unavailable `agentRunner.sandbox.image`, or (for
  `docker_enabled` agents) a Pod Security Standard rejecting the
  privileged `dind` sidecar (see above).
- **`Forbidden` errors from agent-runner creating/watching Jobs**: the
  Role this chart grants is scoped to `agentRunner.sandbox.namespace` —
  if you changed that value after first install, `helm upgrade` moves the
  `Role`/`RoleBinding` to the new namespace automatically, but a Job left
  over in the *old* namespace won't be cleaned up by this chart and needs
  a manual `kubectl delete job`.
- **Plugin install silently does nothing / 404s from the gateway**: almost
  always the ReadWriteMany StorageClass issue above — check
  `kubectl get pvc -n <namespace> <release>-plugins` for a `Pending`
  status.
