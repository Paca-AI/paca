# Helm OIDC Bootstrap Design

## Goal

Make the existing OIDC startup configuration easy to initialize from Helm
without introducing a second database-writing control plane.

## Boundary

`api.oidc.*` remains deployment-owned environment configuration. On a fresh
installation, the API uses these values when `workspace_settings.oidc_configured`
is false. After an administrator saves the SSO settings page, the database
configuration becomes authoritative and Helm upgrades do not overwrite it.

The client secret remains under `secrets.oidcClientSecret` (or the
`OIDC_CLIENT_SECRET` key of `secrets.existingSecret`) so it is rendered only in
the Kubernetes Secret. Provider settings remain non-secret values in
`api.oidc` and are injected into the API Deployment.

## Changes

- Add a copyable OIDC values file under the chart's examples directory.
- Expand the chart README quick-start and secret reference with the OIDC
  values, callback URL rule, and staged local-login rollout.
- Add a small render assertion script that verifies enabled OIDC values land
  in the API Deployment and client secret lands in the Secret, while the
  disabled default emits no OIDC environment block.

## Non-goals

- No Helm hook or Job calls the admin login/API endpoints.
- No SQL template writes `workspace_settings` or stores an unencrypted client
  secret.
- No change to the runtime rule that a successful admin-page save switches the
  source from environment to database.

## Validation

Run `helm lint deploy/helm/paca` and
`deploy/helm/paca/tests/render-oidc.sh`. The render script uses a temporary
values file and checks both the enabled and default-disabled chart output.
