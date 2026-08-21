# Deployment Documentation

> Human OIDC / SSO 登录接入指南见 [oidc-sso.md](oidc-sso.md)。

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
- MinIO (S3-compatible object store for file attachments);
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

All environments ship with MinIO, an S3-compatible object store, so file attachments work out of the box without an AWS account. The API service is storage-provider-agnostic: switching to AWS S3 only requires changing a handful of environment variables.

In production, MinIO runs by default. To suppress the MinIO container when using AWS S3, pass `--scale minio=0` to the `docker compose up` command.

| Scenario | Extra flag | MinIO container |
|---|---|---|
| Self-hosted (default) | _(none)_ | Started |
| AWS S3 | `--scale minio=0` | Not started |

| Variable | Default | Description |
|---|---|---|
| `STORAGE_PROVIDER` | `minio` | `minio` (bundled) or `s3` (AWS S3) |
| `STORAGE_ENDPOINT` | `minio:9000` | Custom endpoint; leave empty for default AWS regional endpoints |
| `STORAGE_REGION` | `us-east-1` | S3 region |
| `STORAGE_BUCKET` | `paca` | Bucket name |
| `STORAGE_ACCESS_KEY_ID` | — | Access key / MinIO root user |
| `STORAGE_SECRET_ACCESS_KEY` | — | Secret key / MinIO root password |
| `STORAGE_USE_SSL` | `false` | Set `true` when connecting over HTTPS |

Presigned URLs are used for both uploads and downloads, so the object store is never exposed publicly. Clients receive short-lived URLs (1 hour for uploads, 15 minutes for downloads) and communicate directly with the storage backend, keeping the API service out of the data plane.