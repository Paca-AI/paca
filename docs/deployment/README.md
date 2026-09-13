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

To migrate by hand, using [MinIO Client](https://min.io/docs/minio/linux/reference/minio-mc.html) (`mc`, which speaks plain S3 and works against any S3-compatible endpoint, RustFS included) while your existing stack is still running:

1. **Back up every object out of the running MinIO container** to a local directory:
   ```bash
   mc alias set old-minio http://localhost:9000 "$STORAGE_ACCESS_KEY_ID" "$STORAGE_SECRET_ACCESS_KEY"
   mc mirror old-minio/paca ./paca-attachments-backup
   ```
2. **Run the upgrade**, explicitly acknowledging that you've handled the migration yourself:
   ```bash
   PACA_ACKNOWLEDGE_STORAGE_MIGRATION=1 bash upgrade.sh
   ```
3. **Copy the objects back up** into the new RustFS container once it's running:
   ```bash
   mc alias set new-rustfs http://localhost:9000 "$STORAGE_ACCESS_KEY_ID" "$STORAGE_SECRET_ACCESS_KEY"
   mc mirror ./paca-attachments-backup new-rustfs/paca
   ```
4. Spot-check a few existing attachments load correctly in the app, then remove the old `minio_data` volume and the now-unused `mc` aliases.

If you'd rather not migrate right now, that's fine — the check runs before `upgrade.sh` touches anything, so your existing `docker-compose.yml` (still defining the working `minio` service) is left completely untouched. Keep running it until you're ready.