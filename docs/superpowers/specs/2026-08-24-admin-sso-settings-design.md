# Admin SSO Settings Design

## Goal

Add an administrator-facing OIDC configuration surface to the existing
`/admin/settings` page. A valid saved configuration becomes active in the
current API process immediately, without requiring administrators to edit
deployment environment variables or restart the local Paca stack.

## Scope

The first version supports the same single, provider-neutral OIDC integration
that already exists:

- enable or disable OIDC;
- issuer URL, client ID, client secret, scopes, display name, and username
  claim;
- enable or disable local password login;
- OIDC Discovery validation before an enabled configuration is saved;
- encrypted client-secret persistence using the existing `ENCRYPTION_KEY`;
- environment variables as the bootstrap and emergency fallback until an
  administrator first saves database-backed SSO settings.

This version does not add multiple IdPs, configuration history, cluster-wide
live propagation, a separate connection-test action, identity linking, or IdP
group-to-role mapping. Changing an active provider may cause login attempts
already in progress to fail; affected users retry from the login page.

## User Experience

The existing admin settings page gains two tabs: **Branding** and **Single
sign-on**. The SSO tab is governed by the existing global `settings.write`
permission and contains:

- an OIDC enabled switch;
- issuer URL and client ID inputs;
- a password input for replacing the client secret, with a masked
  "configured" state and blank meaning "keep the existing secret";
- scopes, display name, and username-claim inputs;
- a local password login switch;
- one Save command.

Saving an enabled configuration shows a pending state while the API performs
OIDC Discovery. Success updates the login page immediately. Validation,
Discovery, encryption, persistence, or lockout-guard failures remain on the
form and leave the active configuration unchanged.

When OIDC is disabled, provider fields remain editable so an administrator can
prepare a configuration before enabling it. Disabling OIDC forces local login
on, matching the existing backend invariant. Disabling local login remains
blocked until an SSO-bound `ADMIN` or `SUPER_ADMIN` exists for the configured
issuer.

If `ENCRYPTION_KEY` is unavailable or invalid, the page remains readable but
cannot save database-backed SSO settings. It reports that encrypted secret
storage is unavailable; it never falls back to storing an OIDC client secret
in plaintext.

## Persistence And API

The singleton `workspace_settings` row is extended with nullable OIDC fields
plus an `oidc_configured` marker. The marker distinguishes an untouched
installation, which continues to use environment configuration, from an
administrator intentionally saving `enabled=false`, which disables OIDC even
if environment values remain present.

Stored fields are:

- `oidc_configured`;
- `oidc_enabled`;
- `oidc_issuer_url`;
- `oidc_client_id`;
- `oidc_client_secret_enc`;
- `oidc_scopes`;
- `oidc_display_name`;
- `oidc_username_claim`;
- `local_login_enabled`.

The client secret is encrypted with the existing AES-256-GCM encryptor before
the row is written. Neither read endpoint returns it. API responses expose
only `client_secret_configured: boolean` and
`encrypted_secret_storage_available: boolean`.

Two permission-protected endpoints are added beneath the existing admin
settings resource:

```text
GET   /api/v1/admin/settings/sso
PATCH /api/v1/admin/settings/sso
```

The read response returns the effective configuration and its source
(`environment` or `database`). The update request is a complete non-secret
configuration plus an optional `client_secret`. Omitting or submitting an
empty client secret preserves the effective existing secret. A successful
update switches the source to `database`.

## Runtime Architecture

A small OIDC runtime manager owns the current immutable configuration snapshot
and optional OIDC service. It provides three responsibilities:

1. expose dynamic login-page options;
2. delegate OIDC begin-login and callback requests to the active service;
3. expose the current local-login policy to the auth service so password login
   remains enforced below the HTTP layer.

OIDC routes are registered unconditionally. When disabled, the manager returns
the existing generic SSO-unavailable error. The public `/auth/config` handler
reads the manager on every request instead of retaining startup-only booleans.

The update operation is serialized within one API process and follows this
order:

1. normalize and validate fields;
2. resolve the existing or replacement client secret;
3. when enabled, construct a candidate OIDC service and complete Discovery;
4. enforce the SSO-only privileged-user guard;
5. encrypt and persist the new settings under the singleton row lock;
6. atomically replace the runtime snapshot and local-login policy.

No active state changes before persistence succeeds. Runtime activation after
a successful database write is an in-memory pointer swap and has no expected
failure path.

At startup, the manager uses database settings when `oidc_configured=true`;
otherwise it uses the existing validated environment configuration. Failure to
decrypt or initialize a database-backed enabled configuration fails startup
instead of silently weakening authentication.

The live update guarantee applies to the API process that accepts the admin
request. A deployment running multiple API replicas must roll those replicas
after changing SSO settings so every process reloads the database snapshot.
Cross-replica notification is deliberately outside this change.

## Error Handling And Security

- The API validates URL, HTTPS, required-field, scope, display-name, and claim
  constraints before persistence.
- OIDC Discovery uses the existing bounded HTTP client and generic external
  error returned by the handler; provider details remain in server logs only.
- The client secret is write-only, is never returned by an API, and is never
  included in logs or validation errors.
- Missing encryption support rejects updates before persistence.
- Enabling SSO-only mode reuses the existing issuer-specific privileged-user
  guard to prevent administrator lockout.
- Disabling OIDC always enables local login in the persisted and runtime
  snapshots.
- Existing Paca sessions, API keys, Agent credentials, RBAC, and external
  identity bindings are unchanged.

## Testing

Backend tests cover:

- migration/repository round trips without exposing plaintext secrets;
- environment fallback and database precedence at startup;
- valid Discovery followed by persistence and immediate activation;
- failed Discovery, encryption, persistence, and lockout checks preserving the
  prior runtime state;
- disabled OIDC forcing local login on;
- dynamic `/auth/config`, OIDC route availability, and service-level password
  login enforcement;
- permission checks and secret masking on admin endpoints;
- concurrent admin updates being serialized within one process.

Frontend tests cover:

- loading effective settings and source;
- masked secret preservation and replacement;
- enabled/disabled and local-login switch behavior;
- save pending, success, validation failure, and encryption-unavailable states;
- the existing Branding tab remaining unchanged.

Focused Go and Vitest suites run first, followed by the broader API and web
checks. A local smoke test configures a loopback test IdP, saves through the
admin API, verifies `/api/v1/auth/config`, and confirms the login page renders
the SSO command without restarting the API.
