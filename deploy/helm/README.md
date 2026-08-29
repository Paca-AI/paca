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
namespace (`agentRunner.sandbox.namespace`, **defaulting to a dedicated
`<release-name>-sandbox` namespace this chart creates for you** — see
`templates/agent-runner/sandbox-namespace.yaml`) allowing it to
create/get/list/watch/delete `batch/jobs`, create/get/list/watch/patch/delete
`apps/deployments` and `services` (static environments — see below),
get/list/watch `pods`, and use the `pods/log` and `pods/exec` subresources
(needed to write skill files into a sandbox and compute git diffs after a
turn — see that package's own doc comments for why). No cluster-scoped
permissions, and no access to any *other resource type* (Secrets, PVCs not
created by this Role, etc.) in that namespace.

**This does not mean no access to other Pods/Deployments/Services in that
namespace.** Kubernetes RBAC can't scope any of these verbs to only the
objects a Role's own subject created — `get`/`list`/`watch`/`exec`/`log` on
`pods`, and `patch`/`delete` on `deployments`/`services`, grant those verbs
against *every* matching object in the namespace, not just agent-runner's
own. This is exactly why the namespace is dedicated by default instead of
falling back to the release's own: granting this Role there would let
agent-runner's ServiceAccount patch or delete this chart's own
api/gateway/web Deployments/Services, and exec into/read logs from
postgres/valkey/minio, not just sandbox objects. If you set
`agentRunner.sandbox.namespace` explicitly to a namespace you manage
yourself, make sure nothing else you don't want agent-runner touching runs
there — this chart won't create a namespace you've explicitly named (see
`sandbox-namespace.yaml`'s own guard), so you're responsible for isolating
it the same way the default does automatically.

**Upgrading an existing install?** Prior to this default, an unset
`agentRunner.sandbox.namespace` resolved to the release's own namespace.
If your RBAC and any running sandbox Jobs/environments already live there,
set `agentRunner.sandbox.namespace` explicitly to that namespace before
upgrading to preserve current behavior — otherwise `helm upgrade` moves
the `Role`/`RoleBinding` to the new dedicated namespace and anything still
running in the old one becomes unreachable (`Forbidden`) until you migrate
or manually clean it up (see Troubleshooting below).

Every sandbox Pod's primary container runs as root — the same reason
`services/agent-runner/Dockerfile` itself runs as root, just for the
sandbox image instead: an AI coding agent installing packages,
chown/chmod-ing arbitrary repo files, etc. needs it. A namespace enforcing
the **Restricted** Pod Security Standard will reject *every* sandbox Pod
outright on that basis alone, whether or not the agent has `docker_enabled`
— **Baseline** is the strictest Pod Security Standard the kubernetes
sandbox backend supports.

If an agent has `docker_enabled: true` in the database, its sandbox Pod
additionally gets a privileged `docker:dind` sidecar so it can run Docker
commands. That's the same trade-off `docker-compose.prod.yml`'s own
per-conversation sidecar makes, just running inside the sandbox's own Pod
instead of a paired container — see `agentRunner.sandbox` in `values.yaml`
if you plan to use `docker_enabled` agents.

## Quick start

No repository clone required — the chart is published as an OCI artifact to
GHCR alongside every other release image:

```bash
kubectl create namespace paca
helm install paca oci://ghcr.io/paca-ai/charts/paca --version <release-version> -n paca -f my-values.yaml
```

`<release-version>` is a [release](https://github.com/Paca-AI/paca/releases)
tag without its leading `v` (e.g. `0.13.1` for `v0.13.1`); omit `--version` to
install the newest chart published. To install from a repository checkout
instead — e.g. to try an unreleased chart change — point at the chart
directory itself:

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

If you don't have a domain yet and leave `publicUrl` empty for a
ClusterIP/NodePort-only deployment, the gateway has nothing to
auto-provision HTTPS for and serves plain HTTP — also set
`api.cookieSecure: false` in that case, or the auth cookie won't stick
(see that value's own comment in `values.yaml`).

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

One exception to the component-for-component mirror: Compose's `db-backup`
sidecar (the scheduled `pg_dump`) has no Helm equivalent — backups are left
to your cluster's own CronJob/operator tooling instead.

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

## SSH access

Off by default. When enabled, a user can `ssh` directly into a running
static environment's own real `sshd` for pair programming — see
[`docs/ai-agent/environment-management.md`](../../docs/ai-agent/environment-management.md)'s
"Terminal / SSH Access" section for the full design. `agent-runner`
assigns each environment a dedicated external port, published directly on
a **per-environment `NodePort` Service it creates itself** at runtime
(`ensureEnvironmentService`, `internal/sandbox/k8s/environment.go`) — not
a template in this chart, and not anything `agent-runner`'s own process
ever relays through, so a real `ssh` client's full capabilities (agent
forwarding, SFTP, further port forwards, a real exit status) all just
work.

```yaml
agentRunner:
  sshBastion:
    enabled: true
    portRangeStart: 30200   # must fall inside your cluster's own
    portRangeEnd: 30299     # --service-node-port-range (30000-32767 by default)

environments:
  sshBastionHost: "node.paca.example.com"   # any node's address a client can reach
```

`environments.sshBastionHost` is passed to `services/api` as
`SSH_BASTION_HOST` — purely descriptive (`services/api` never itself
routes SSH traffic), used only so `GET /environments/config` can hand the
web app's Connect page a real `ssh -p <port> root@<host>` command instead
of a placeholder host. Point it at any node's own reachable
address/DNS name, or a TCP load balancer in front of your node pool if you
have one — a `NodePort` Service is reachable at `<any-node>:<nodePort>`,
so unlike a single shared Service there's no one Service address to look
up. Leaving it unset doesn't break SSH itself, it just means the web app
shows a placeholder host the user has to fill in themselves.

1. **Register a public key first.** A user adds their SSH public key on
   the environment's own Connect page — it's pushed straight into that
   environment's own `authorized_keys` the moment the environment is next
   running.
2. **Connect.** Once an environment is `running`, its Connect page shows
   the exact port it was assigned:
   ```bash
   ssh -p <port> root@<any-node-address>
   ```
   The port is assigned once, the first time an environment is created,
   and reused across every later stop/start — it never changes for that
   environment's lifetime.
3. **Not fronted by Ingress/Caddy.** Caddy and this chart's own `Ingress`
   template are both HTTP-only and cannot proxy raw SSH/TCP traffic — a
   `NodePort` needs no ingress-for-TCP solution at all, it's already
   reachable directly on every node.
4. **Host key persistence.** Each environment generates its own `sshd`
   host key once, on its own persisted workspace volume (the same one
   `agentRunner.sandbox.environments` provisions) — not a separate
   bastion-wide PVC, so there's nothing extra to lose. A user's SSH client
   only ever sees a "REMOTE HOST IDENTIFICATION HAS CHANGED" warning if
   that specific environment's own volume is lost, exactly like losing any
   other file on its disk.
5. **Port range sizing.** `portRangeEnd - portRangeStart + 1` is the
   maximum number of environments that can have SSH open simultaneously on
   this deployment — widen the range if you expect more, staying inside
   your cluster's own `--service-node-port-range`.
6. **RBAC.** `agentRunner.sandbox.backend: kubernetes` grants agent-runner
   `create`/`get`/`list`/`watch`/`patch`/`delete` on `Service` objects in
   `agentRunner.sandbox.namespace` (see `templates/agent-runner/role.yaml`)
   specifically so it can manage these per-environment Services — no
   further RBAC changes needed to enable this feature. See "The AI agent
   sandbox on Kubernetes" above for why this namespace is dedicated by
   default rather than the release's own.

## Port forwarding

Off by default. The exact same idea as SSH access above — a
per-environment `NodePort` Service entry, natively published, no relay —
but for any container port a user wants to expose (their own dev server,
most commonly), added and removed from the environment's own Connect page
in the web app instead of being a single auto-created port. See
[`docs/ai-agent/environment-management.md`](../../docs/ai-agent/environment-management.md)'s
"Port Forwarding" section.

```yaml
agentRunner:
  portForward:
    enabled: true
    portRangeStart: 30300   # a disjoint range from sshBastion's own, same
    portRangeEnd: 30399     # --service-node-port-range constraint

environments:
  portForwardHost: "node.paca.example.com"   # same idea as sshBastionHost
```

Unlike `sshBastionHost`, `environments.portForwardHost` isn't purely
descriptive for `services/api` alone any more: it's still passed there as
`PORT_FORWARD_HOST` so `GET /environments/config` can hand the web app's
Connect page a real `<host>:<host_port>` address, but it's now *also*
passed to `agent-runner` itself (same env var, same value), which uses it
to tell an environment-attached conversation's agent that same address
in-context — so if the agent starts a dev server on a forwarded port, it
can give the user a real URL without the user having to go find it on the
Connect page first.

Unlike the `docker` backend — where changing a container's published
ports means stopping and recreating it — patching a `NodePort` Service's
port list is a live operation that never touches the Pod at all. Clicking
"Restart" on an environment's Connect page after adding/removing a
forward still applies here (same UI, same button, for cross-backend
consistency), but on `kubernetes` it completes instantly with zero
downtime rather than actually restarting anything.

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
  resource quota, an unavailable `agentRunner.sandbox.image`, or a Pod
  Security Standard stricter than **Baseline** rejecting the sandbox Pod
  outright (or, for `docker_enabled` agents specifically, its privileged
  `dind` sidecar) — see above.
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
- **`ssh` just says "Permission denied (publickey)." with no further
  detail**: a real `sshd` inside the environment's own container is what's
  answering (see the "SSH access" section above), so this is standard
  OpenSSH behavior, not anything `agent-runner` itself produces. Most
  likely cause: the public key wasn't registered on that specific
  environment before it last started — register it on the environment's
  Connect page, then start (or restart) the environment so it gets pushed
  into `authorized_keys`.
- **`ssh` hangs waiting to connect, or connection refused**: confirm
  `agentRunner.sshBastion.enabled` is actually `true` and that the
  environment is `running` — then check
  `kubectl get svc paca-env-<environment-id> -n <sandbox namespace>`
  exists with the expected `nodePort`, and that whatever address
  `environments.sshBastionHost` points at is actually one of your
  cluster's real node addresses (a `NodePort` is reachable at
  `<any-node>:<nodePort>`, not at the Service's own `ClusterIP`).
