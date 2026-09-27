# SSO / OpenID Connect

Paca can let people sign in with an external identity provider (IdP) that speaks OpenID Connect: Google Workspace, Microsoft Entra ID, Okta, Keycloak, Authentik, Auth0, GitLab and others. Password sign-in keeps working alongside it.

## Who can configure it

Providers are managed under **Administration → Settings → Single sign-on (SSO)**, which needs the `settings.sso.write` global permission. By default only `SUPER_ADMIN` holds it: a provider with *Link existing accounts by email* turned on lets whoever controls that provider sign in as any account with a matching email, so the permission is root-equivalent (see [authorization](../architecture/authorization.md)).

## Before you start

- Set `PUBLIC_URL` on the API to the address users reach Paca at (for example `https://paca.example.com`). The callback URL is built from it. Without it, the API falls back to the request's `Host` header (the bundled Caddy proxy preserves it); `X-Forwarded-Host` is ignored because clients can forge it.
- Set `ENCRYPTION_KEY` so client secrets are encrypted at rest. Without it they are stored in plaintext, the same as every other secret in Paca.
- The API server must be able to reach the IdP's issuer URL: it fetches the discovery document, signing keys, and token endpoint server-side.

## Setting up a provider

1. In Paca, click **Add provider** and enter a display name. The slug is derived from the name; it appears in the callback URL, so keep it stable once in use.
2. Copy the **Callback URL** shown in the dialog (`{PUBLIC_URL}/api/v1/auth/sso/{slug}/callback`).
3. At your IdP, create an OpenID Connect / OAuth 2.0 **web application** client with that URL as its redirect URI. Note the client ID and secret.
4. Back in Paca, fill in the **Issuer URL**, **Client ID** and **Client secret**, then save. An enabled provider is checked against the issuer's discovery document when you save, so a typo shows up immediately.

Common issuer URLs:

| Provider | Issuer URL |
| --- | --- |
| Google | `https://accounts.google.com` |
| Microsoft Entra ID | `https://login.microsoftonline.com/{tenant-id}/v2.0` |
| Okta | `https://{your-org}.okta.com` (or a custom authorization server) |
| Keycloak | `https://{host}/realms/{realm}` |
| GitLab | `https://gitlab.com` |

## Options

| Option | Effect |
| --- | --- |
| **Enabled** | Shows a "Continue with …" button on the sign-in page. A disabled provider cannot be used at all. |
| **Create accounts automatically** | A first-time user gets a new account with the [default global role](../architecture/authorization.md). Their username comes from `preferred_username` or their email (with a numeric suffix if it's taken), and their name and email come from the ID token. The account gets a random password nobody knows, so it signs in only through SSO until an admin resets the password. |
| **Link existing accounts by email** | A first-time user whose *verified* email matches an existing account signs in as that account, and the link is remembered. Only turn this on for a provider you fully trust — and never for a *multi-tenant* issuer (e.g. Microsoft Entra's `common`/`organizations` endpoints), where any tenant's admin can put an arbitrary address in the `email` claim. Emails match case-insensitively. |
| **Allowed email domains** | Only users whose verified email is in one of these domains may sign in. Leave empty to allow any. |
| **Scopes** | Defaults to `openid profile email`. `openid` is always added. |

## How sign-in resolves an account

A returning user is recognised by the provider's stable subject (`sub`) claim, not by email, so changing an email at the IdP does not change which Paca account they land in. For a first-time sign-in:

1. With *Link existing accounts by email* on, a verified email matching an active account signs in as that account.
2. Otherwise, if an account already owns that email, sign-in is refused (`account_exists`) rather than creating a duplicate.
3. Otherwise, with *Create accounts automatically* on, a new account is created.
4. Otherwise sign-in is refused (`no_account`).

Unverified emails (`email_verified` not `true`) are never used for linking, provisioning, or the domain allow-list.

Linking relies on the provider's `email_verified` claim, which is only as trustworthy as the provider itself. Point link-by-email providers at a single-tenant issuer URL.

The API fetches each issuer's discovery document and signing keys itself. To keep an admin-entered issuer URL from reaching cloud metadata endpoints, it refuses link-local (`169.254.0.0/16`, `fe80::/10`), multicast and unspecified addresses; loopback and private addresses are allowed so a self-hosted IdP on the same network works.

Like password login, `/auth/sso/{slug}/login` and `/callback` are rate-limited per client IP — `AUTH_RATE_LIMIT_PER_MINUTE`, default 20 per endpoint (the IP is the rightmost `X-Forwarded-For` entry, which the reverse proxy sets). The limit is per API instance.

Deleting a Paca user keeps them out: user deletion is a soft delete, so their SSO link stays attached to the deleted account and they are not re-provisioned. (`user_identities.user_id` cascades on a hard delete, so if Paca ever gains a hard-delete path, the same identity could then provision a fresh account.) Deleting a provider removes its links but keeps the accounts.

## Security notes

- The flow is the authorization-code flow with PKCE (S256), a `state` bound to the browser that started the sign-in (an `sso_state` cookie), and a `nonce` checked in the ID token. State is single-use and expires after 10 minutes.
- The post-sign-in redirect only accepts in-app paths, so the flow cannot be used as an open redirect.
- A successful SSO sign-in issues the same session cookies as password sign-in; *Keep me signed in* on the login page applies to both.

## Troubleshooting

When SSO sign-in fails, the user is sent back to the sign-in page with `?sso_error=<code>`:

| Code | Meaning |
| --- | --- |
| `provider_not_found` | The provider was deleted or disabled. |
| `provider_unavailable` | The API could not fetch the issuer's discovery document. |
| `provider_denied` | The user cancelled, or the IdP refused, at the IdP. |
| `state_invalid` | The sign-in took longer than 10 minutes, was replayed, or was finished in a different browser. |
| `exchange_failed` | The code exchange or ID-token check failed. Usually a wrong client secret, or a redirect URI that doesn't match exactly. The API log has the details. |
| `email_not_allowed` | The email domain isn't allowed, or the email isn't verified. |
| `no_account` | No linked account, and the provider neither links nor creates accounts. |
| `account_exists` | An account already has this email, and linking is off. |
| `rate_limited` | Too many SSO sign-in requests from this IP (`AUTH_RATE_LIMIT_PER_MINUTE`). |
