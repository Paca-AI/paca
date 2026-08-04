# Plugin Marketplace

This document defines the plugin marketplace used by Paca.

## Overview

Paca uses a public GitHub repository as the marketplace source of truth:

- Repository: `Paca-AI/paca-plugins`
- Catalog file: `catalog/plugins.json`
- Delivery model: plugin developers publish by opening pull requests to that repository.

The API fetches the catalog JSON, shows entries in the admin web UI, downloads selected plugin artifacts, installs them locally, runs migrations, and reloads plugin runtime modules.

## Catalog Schema

The catalog is a JSON document with this shape:

```json
{
  "schema_version": 1,
  "source": "https://github.com/Paca-AI/paca-plugins",
  "generated_at": "2026-05-08T00:00:00Z",
  "plugins": [
    {
      "name": "com.paca.checklist",
      "display_name": "Checklist",
      "description": "Adds named checklists with checkable items to tasks.",
      "version": "0.1.0",
      "avatar_url": "https://raw.githubusercontent.com/Paca-AI/paca-plugins/main/assets/checklist.png",
      "repository_url": "https://github.com/Paca-AI/paca-plugin-checklist",
      "artifacts": {
        "backend_tar_gz_url": "https://github.com/Paca-AI/paca-plugin-checklist/releases/download/v0.1.0/backend.tar.gz",
        "frontend_tar_gz_url": "https://github.com/Paca-AI/paca-plugin-checklist/releases/download/v0.1.0/frontend.tar.gz",
        "migrations_tar_gz_url": "https://github.com/Paca-AI/paca-plugin-checklist/releases/download/v0.1.0/migrations.tar.gz",
        "manifest_tar_gz_url": "https://github.com/Paca-AI/paca-plugin-checklist/releases/download/v0.1.0/manifest.tar.gz"
      }
    }
  ]
}
```

## Required Plugin Fields

Every plugin entry must include:

- `name` (reverse-DNS plugin identifier)
- `description`
- `version`
- `artifacts.backend_tar_gz_url`
- `artifacts.frontend_tar_gz_url`
- `artifacts.migrations_tar_gz_url`
- `artifacts.manifest_tar_gz_url`

Optional but recommended:

- `display_name`
- `avatar_url`
- `repository_url`

## Install Flow

When an admin clicks Install in the web app:

1. Web app calls `POST /api/v1/admin/plugins/marketplace/install`.
2. API resolves the plugin entry from catalog.
3. API downloads all tar.gz artifacts.
4. API extracts artifacts into local plugin stores:
   - Backend: `PLUGINS_WASM_DIR/<plugin-id>/backend.wasm`
   - Manifest: `PLUGINS_WASM_DIR/<plugin-id>/plugin.json`
   - Migrations: `PLUGINS_WASM_DIR/<plugin-id>/migrations/*.sql`
   - Frontend: `PLUGINS_FRONTEND_DIR/<plugin-id>/...`
5. API registers plugin record in database (`plugins` table) — rejecting the install with `PLUGIN_INCOMPATIBLE_HOST_VERSION` if the downloaded manifest's `minCoreVersion` (see [backend-plugin-system.md](backend-plugin-system.md#minimum-host-version-mincoreversion)) is newer than the running Paca build.
6. API runs plugin migrations.
7. API loads/reloads runtime module.

If runtime load fails, DB registration is rolled back.

## API Endpoints

- `GET /api/v1/admin/plugins/marketplace`: list marketplace catalog entries.
- `POST /api/v1/admin/plugins/marketplace/install`: install one plugin by name.

Existing plugin endpoints remain unchanged:

- `GET /api/v1/plugins`
- `POST /api/v1/admin/plugins`
- `PATCH /api/v1/admin/plugins/:pluginId`
- `DELETE /api/v1/admin/plugins/:pluginId`

## Configuration

Environment variables:

- `PLUGINS_MARKETPLACE_CATALOG_URL`
- `PLUGINS_MARKETPLACE_TIMEOUT`
- `PLUGINS_WASM_DIR`
- `PLUGINS_FRONTEND_DIR`

Default catalog URL:

- `https://raw.githubusercontent.com/Paca-AI/paca-plugins/master/catalog/plugins.json`

## Publishing Via PR

Plugin developers publish through PRs to `Paca-AI/paca-plugins`:

1. Build and publish release artifacts in plugin repository.
2. Ensure four artifact tar.gz URLs are public and immutable.
3. Add or update plugin entry in `catalog/plugins.json`.
4. Open PR with version bump and release notes.
5. After merge, the plugin appears automatically in Paca marketplace UI.

## Operational Notes

- The API installer currently targets local plugin stores (`PLUGINS_STORE=local`).
- Gateway must serve frontend bundles from `/plugins/*` so remote entries are reachable.
- Backend store must be writable by API for marketplace installation.
