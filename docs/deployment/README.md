# Deployment Documentation

Paca ships three Docker Compose entry points under [`deploy/`](../../deploy/README.md):

- `docker-compose.dev.yml` for local development;
- `docker-compose.prod.yml` for production-oriented single-host deployment;
- `docker-compose.e2e.yml` for end-to-end test automation.

## Why They Are Separate

Development and production have different goals:

- development optimizes for fast onboarding, inspectability, and local feedback;
- production optimizes for explicit configuration, image-based rollout, and operator control.

Keeping them separate is the cleaner open-source default. It avoids hard-coding local assumptions into a production path and makes the repository easier for contributors to reason about.

## Development Compose

The development compose file provisions:

- PostgreSQL;
- Valkey;
- RustFS (S3-compatible object store for file attachments);
- optional `api` and `web` service containers that you can run alongside the infra services as needed.

This supports two workflows:

- run only infra in Docker and start application services on the host;
- run the whole stack in Docker for quick end-to-end testing.

## Production Compose

The production compose file is intentionally self-hostable:

- it defines the web and API containers;
- it includes PostgreSQL and Valkey for a complete single-host stack;
- it keeps configuration explicit through environment variables and named volumes;
- it publishes the web and API services by default.

That makes it a better open-source baseline: users can run the full platform immediately, while operators with managed infrastructure can still swap the bundled services for externally hosted equivalents by changing the connection settings.

## Upgrading to IAM authorization (migrations 000064-000068)

The release that introduced IAM-style roles ships five migrations that apply automatically the first time the new API starts: `000064_iam_roles.sql` (converts every role and membership, then self-checks before committing), `000065_plugin_manifest_actions.sql` (rewrites installed plugin manifests), `000066_relax_legacy_role_columns.sql` and `000067_project_role_resources.sql` (rewrites project-owned role policies to name only their own project) and `000068_add_roles_assign.sql` (adds `roles:assign` next to `project.members:write` so roles that could change a member's roles still can). They are up-only: **there is no down migration and no application downgrade**, so take and verify a database dump first (the bundled `db-backup` container's last dump may be hours old; see [Database backups](../../deploy/README.md#database-backups)). If the 000064 self-check finds any difference between the old and new access rules it aborts and rolls back, and the API does not start. Restricted agents and environments are **not** migrated and become open until an administrator recreates the restriction with a `Deny` role.

## Upgrading to unified accounts and service accounts (migration 000066)

Every actor (user, agent, service account) is now one `accounts` row; see [Accounts model](../architecture/database-schema.md#accounts-model) and the [service accounts API](../api/service-accounts.md). Migration `000066_unified_accounts.sql` applies automatically on first start and is **irreversible**: **back up the database first**. It moves name, description, avatar and the soft-delete flag from `users` and `agents` to `accounts`, replaces `role_attachments.principal_type/principal_id` and `project_members.user_id/agent_id/member_type` with `account_id`, and drops the legacy `users.role_id`, `agents.global_role_id` and `project_members.project_role_id`. Role attachments whose principal no longer exists are deleted (reported as a `NOTICE` with the count).

- **`AGENT_API_KEY` is gone** (API, agent-runner `PACA_API_KEY`, Helm `secrets.agentApiKey`, the installer); it is ignored if still set. An agent authenticates with its own API key and is judged by its own roles only; the `X-Agent-ID` and `X-Actor-User-ID` headers are ignored. Anything that called the API with the shared key must use a real account's key.
- **An agent never borrows a person's permissions.** A global agent chatting with someone used to attribute some actions (for example creating a project) to that person; it now acts, and is attributed, as itself, and needs roles that allow what it does.
- agent-runner sandboxes get the agent's own key (`PACA_API_KEY`); existing environments are recreated once to pick it up. The `MCP key` an ACP agent already had keeps working.
- Usernames and emails stay unique among live users, and agent handles unique per project (workspace-wide for global agents), now enforced by a trigger instead of partial indexes.
- Plugins cannot write the new `accounts` table. First-party plugins that read core tables (time-logging, webhook, dashboard's "Open tasks by assignee" preset) queried `users.full_name`, `agents.name` and `project_members.user_id/agent_id`; their current sources use `accounts` and `project_members.account_id` and need a core that includes this migration (`minCoreVersion`). The dashboard plugin migrates saved panels that still hold the unchanged preset query; a panel edited by hand must be adapted. Plugin SQL naming a dropped column fails with a query error.
- Built-in `ADMIN` does not get the new `service_accounts:*` actions; grant them through a role if needed.

## Upgrading: public projects removed (migration 000065)

Projects can no longer be made public: every `/projects/{projectId}/...` route requires a signed-in caller whose roles allow the action. Migration `000065_drop_project_is_public.sql` drops `projects.is_public` and is **irreversible**; formerly public projects become members-only and anonymous visitors get `401`. Before upgrading, list them:

```sql
SELECT id, name FROM projects WHERE is_public AND deleted_at IS NULL;
```

The `is_public` field is gone from the project create/update/read API (clients that still send it are ignored). A `Deny` (for example `tasks:read`) on a project is now honoured for tasks fetched by id and their activities, links and attachments; workspace-wide `projects:read` still lists and opens projects but no longer reads their contents.

## Object Storage

All environments ship with [RustFS](https://rustfs.com), an S3-compatible object store, so file attachments work out of the box without an AWS account. The API service is storage-provider-agnostic: switching to AWS S3 only requires changing a handful of environment variables, and switching the self-hosted backend itself (as happened when this project moved off MinIO — see below) only ever requires changing the bundled container, never application code.

In production, RustFS runs by default. To suppress the RustFS container when using AWS S3, pass `--scale rustfs=0` to the `docker compose up` command.

| Scenario | Extra flag | RustFS container |
|---|---|---|
| Self-hosted (default) | _(none)_ | Started |
| AWS S3 | `--scale rustfs=0` | Not started |

| Variable | Default | Description |
|---|---|---|
| `STORAGE_PROVIDER` | `rustfs` | `rustfs` (bundled) or `s3` (AWS S3) |
| `STORAGE_ENDPOINT` | `rustfs:9000` | Custom endpoint; leave empty for default AWS regional endpoints |
| `STORAGE_REGION` | `us-east-1` | S3 region |
| `STORAGE_BUCKET` | `paca` | Bucket name |
| `STORAGE_ACCESS_KEY_ID` | — | Access key / RustFS root user |
| `STORAGE_SECRET_ACCESS_KEY` | — | Secret key / RustFS root password |
| `STORAGE_USE_SSL` | `false` | Set `true` when connecting over HTTPS |

Presigned URLs are used for both uploads and downloads, so the object store is never exposed publicly. Clients receive short-lived URLs (1 hour for uploads, 15 minutes for downloads) and communicate directly with the storage backend, keeping the API service out of the data plane.

### Switching between RustFS and AWS S3

`scripts/migrate-storage.sh` copies every object in the bucket to the other backend, verifies the object count and total size, then updates `.env` (a `.env.bak.<timestamp>` is kept, and restored if anything fails) and restarts the `api`. The source bucket is never modified. Download it into your install directory (the one holding `docker-compose.yml` and `.env`) and run it from there:

```bash
curl -fsSL https://raw.githubusercontent.com/Paca-AI/paca/master/scripts/migrate-storage.sh -o migrate-storage.sh

# RustFS -> AWS S3 (or any S3-compatible service via DEST_STORAGE_ENDPOINT)
DEST_STORAGE_BUCKET=my-bucket DEST_STORAGE_REGION=eu-west-1 \
DEST_STORAGE_ACCESS_KEY_ID=AKIA... DEST_STORAGE_SECRET_ACCESS_KEY=... \
bash migrate-storage.sh --to s3

# AWS S3 -> bundled RustFS (credentials are generated unless DEST_STORAGE_* are set)
bash migrate-storage.sh --to rustfs
```

Copying is done by [rclone](https://rclone.org) in a throwaway container (`rclone/rclone` from Docker Hub). For `--to s3`, create the bucket first; the script does not create it, so the key needs no `s3:CreateBucket`. Use `--dry-run` to preview. By default the `api` is stopped during the copy so nothing is uploaded mid-migration. After moving to S3, run future `docker compose up` with `--scale rustfs=0`. See the header of the script for all options.

### Migrating from MinIO to RustFS

MinIO removed its own images from Docker Hub and archived its open-source repository, so this project switched its bundled object store to RustFS. New installs are unaffected. **Existing self-hosted installs still running the bundled MinIO container have real attachment data in their `minio_data` volume**, which `scripts/upgrade.sh` does not migrate automatically — it detects this case and refuses to proceed rather than silently pointing the API at a brand-new, empty RustFS container while your old data sits inert in the orphaned `minio_data` volume.

To migrate by hand, using [MinIO Client](https://min.io/docs/minio/linux/reference/minio-mc.html) (`mc`, which speaks plain S3 and works against any S3-compatible endpoint, RustFS included) while your existing stack is still running. Run it as a container on the **same Docker network your compose stack uses** rather than on the host — the compose file never publishes the object store's port to the host (only the gateway's 80/443 are published), so `http://localhost:9000` is not reachable from outside the stack. The network is usually `<project-name>_default` (`paca_default` for a default install; run `docker network ls` if you're not sure).

From the directory holding your `docker-compose.yml` and `.env`:

1. **Export your storage credentials into the shell** — `.env` is read by Compose, not by your shell, so `$STORAGE_ACCESS_KEY_ID`/`$STORAGE_SECRET_ACCESS_KEY` are otherwise empty here:
   ```bash
   set -a; source .env; set +a
   ```
2. **Back up every object out of the running MinIO container** to a local directory (`quay.io/minio/mc` — MinIO's own Docker Hub images are gone, but this one is still published):
   ```bash
   docker run --rm --network paca_default -v "$PWD/paca-attachments-backup:/backup" \
     --entrypoint /bin/sh quay.io/minio/mc -c "
       mc alias set old-minio http://minio:9000 '$STORAGE_ACCESS_KEY_ID' '$STORAGE_SECRET_ACCESS_KEY' &&
       mc mirror old-minio/paca /backup
     "
   ```
3. **Run the upgrade**, explicitly acknowledging that you've handled the migration yourself:
   ```bash
   PACA_ACKNOWLEDGE_STORAGE_MIGRATION=1 bash upgrade.sh
   ```
4. **Copy the objects back up** into the new RustFS container once it's running — note the destination bucket needs creating first, unlike the source bucket above which the app already created:
   ```bash
   docker run --rm --network paca_default -v "$PWD/paca-attachments-backup:/backup" \
     --entrypoint /bin/sh quay.io/minio/mc -c "
       mc alias set new-rustfs http://rustfs:9000 '$STORAGE_ACCESS_KEY_ID' '$STORAGE_SECRET_ACCESS_KEY' &&
       mc mb new-rustfs/paca &&
       mc mirror /backup new-rustfs/paca
     "
   ```
5. Spot-check a few existing attachments load correctly in the app, then remove the backup directory. The old `minio_data` volume is already freed for you to remove — `upgrade.sh`'s final `docker compose up` passes `--remove-orphans`, which stops and removes the now-undefined `minio` container as part of the upgrade itself:
   ```bash
   docker volume rm paca_minio_data
   rm -rf ./paca-attachments-backup
   ```

Verified end-to-end (a real object round-tripped byte-for-byte through this exact sequence, against a MinIO container with no published port) before writing it up here.

**Can't pull `quay.io/minio/mc`?** (for example a `401 UNAUTHORIZED` from quay.io on your host) Steps 2 and 4 work the same with [rclone](https://rclone.org), whose image is on Docker Hub. Remotes are defined through environment variables passed to Docker by name, so the secrets stay out of the process list. Run this after step 1, and use these in place of the `mc` commands:

```bash
# Step 2 — back up out of MinIO
export RCLONE_CONFIG_OLD_TYPE=s3 RCLONE_CONFIG_OLD_PROVIDER=Other RCLONE_CONFIG_OLD_ENDPOINT=http://minio:9000 \
  RCLONE_CONFIG_OLD_ACCESS_KEY_ID="$STORAGE_ACCESS_KEY_ID" RCLONE_CONFIG_OLD_SECRET_ACCESS_KEY="$STORAGE_SECRET_ACCESS_KEY" \
  RCLONE_CONFIG_OLD_FORCE_PATH_STYLE=true
docker run --rm --network paca_default -v "$PWD/paca-attachments-backup:/backup" \
  -e RCLONE_CONFIG_OLD_TYPE -e RCLONE_CONFIG_OLD_PROVIDER -e RCLONE_CONFIG_OLD_ENDPOINT \
  -e RCLONE_CONFIG_OLD_ACCESS_KEY_ID -e RCLONE_CONFIG_OLD_SECRET_ACCESS_KEY -e RCLONE_CONFIG_OLD_FORCE_PATH_STYLE \
  rclone/rclone copy old:paca /backup

# Step 4 — restore into RustFS (after running the upgrade in step 3)
export RCLONE_CONFIG_NEW_TYPE=s3 RCLONE_CONFIG_NEW_PROVIDER=Other RCLONE_CONFIG_NEW_ENDPOINT=http://rustfs:9000 \
  RCLONE_CONFIG_NEW_ACCESS_KEY_ID="$STORAGE_ACCESS_KEY_ID" RCLONE_CONFIG_NEW_SECRET_ACCESS_KEY="$STORAGE_SECRET_ACCESS_KEY" \
  RCLONE_CONFIG_NEW_FORCE_PATH_STYLE=true
RC_NEW=(-e RCLONE_CONFIG_NEW_TYPE -e RCLONE_CONFIG_NEW_PROVIDER -e RCLONE_CONFIG_NEW_ENDPOINT -e RCLONE_CONFIG_NEW_ACCESS_KEY_ID -e RCLONE_CONFIG_NEW_SECRET_ACCESS_KEY -e RCLONE_CONFIG_NEW_FORCE_PATH_STYLE)
docker run --rm --network paca_default "${RC_NEW[@]}" rclone/rclone mkdir new:paca
docker run --rm --network paca_default -v "$PWD/paca-attachments-backup:/backup" "${RC_NEW[@]}" rclone/rclone copy /backup new:paca
```

(Verified against two S3-compatible containers standing in for MinIO and RustFS, not against a real MinIO.)

If you'd rather not migrate right now, that's fine — the check runs before `upgrade.sh` touches anything, so your existing `docker-compose.yml` (still defining the working `minio` service) is left completely untouched. Keep running it until you're ready.

### Prefer to keep running your existing MinIO container for now?

You don't have to migrate to upgrade. Run:

```bash
PACA_KEEP_MINIO=1 bash upgrade.sh
```

(or, run interactively without `PACA_YES=1`, just answer "yes" when asked.) This gets you every other improvement in the release — nothing about your storage setup changes: `STORAGE_PROVIDER`/`STORAGE_ENDPOINT` in `.env` stay pointed at your existing `minio` container exactly as they are, and this run skips `--remove-orphans` so that container is left running completely undisturbed alongside the rest of the upgraded stack (it's no longer defined in the new `docker-compose.yml`, so it becomes an "orphan" Compose no longer manages — but an orphan that's still running stays reachable by its container/service name on the same Docker network exactly as before, which is all the API needs).

This is a stopgap, not a long-term choice: MinIO's own open-source repository is archived, so it receives no more security patches going forward. Migrate whenever you get the chance, using the steps above — nothing about choosing this option now makes that migration any harder later.

### Helm users

Helm has no equivalent of `upgrade.sh`'s hard-stop or its `PACA_KEEP_MINIO` option — a `helm upgrade` onto this chart version, applied to a release still running the bundled MinIO StatefulSet, deletes that StatefulSet (its template is simply gone from the new chart) and creates a new, empty RustFS object store instead — the upstream [rustfs/rustfs](https://artifacthub.io/packages/helm/rustfs/rustfs) chart, pulled in as a real dependency — with no warning if you never explicitly set `storage.provider` yourself (its default just changed out from under you). The PVC behind the deleted StatefulSet is orphaned, not deleted — your data isn't gone, but nothing points the app at it anymore.

**Recommended: migrate first, then upgrade.** Follow the same `mc mirror` approach as the Compose steps above, adapted for Kubernetes — `kubectl port-forward svc/<release>-minio 9000:9000` (or a temporary `mc` Pod on the cluster network) in place of the `docker run --network` step, since there's no Docker network to attach to here, mirroring into `kubectl port-forward svc/<release>-rustfs-svc 9000:9000` once you've upgraded. Then `helm upgrade` normally. Also set `rustfs.secret.rustfs.access_key`/`secret_key` in your values file to whatever `secrets.storageAccessKeyId`/`storageSecretAccessKey` was — the bundled instance's credentials now live there instead (see `deploy/helm/paca/README.md`'s "Object storage" section); getting this one wrong fails loudly rather than silently, since the upstream chart refuses to render at all if both are left empty.

**Not ready to migrate?** Stay on your current chart version until you are — `helm upgrade` only ever changes what you tell it to. You can still pick up other fixes independently in the meantime by bumping just the application image tags (`--reuse-values --set api.image.tag=<newer>` etc.) without bumping the chart itself, as long as the newer images don't depend on other chart-level changes you'd also be skipping — check each release's notes.

We haven't verified a way to keep the bundled MinIO StatefulSet itself running *through* a `helm upgrade` to this chart version (Helm's `helm.sh/resource-policy: keep` annotation is the usual mechanism for protecting a resource from deletion when its template is removed, but we haven't tested it against this specific scenario, and getting it wrong risks the PVC itself, not just a config value — pinning the chart version is the option we can actually stand behind).