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

If you'd rather not migrate right now, that's fine — the check runs before `upgrade.sh` touches anything, so your existing `docker-compose.yml` (still defining the working `minio` service) is left completely untouched. Keep running it until you're ready.

### Helm has no equivalent guard

The hard-stop above only protects Docker Compose installs. `helm upgrade` on a release still running the bundled MinIO StatefulSet has no equivalent check: the old StatefulSet is deleted, a new empty RustFS one is created, and the old release's `minio` PVC is orphaned — silently, if you never explicitly set `storage.provider` (its default just changed out from under you). Follow the same `mc mirror` approach above before upgrading — `kubectl port-forward` to each Pod (or a temporary `mc` Pod on the cluster network) in place of the `docker run --network` step, since there's no Docker network to attach to on Kubernetes.