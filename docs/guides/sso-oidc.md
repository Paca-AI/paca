# SSO / OpenID Connect

Paca can let people sign in with an external identity provider (IdP) that speaks OpenID Connect: Google Workspace, Microsoft Entra ID, Okta, Keycloak, Authentik, Auth0, GitLab and others. Password sign-in keeps working alongside it unless you turn on [SSO-only mode](#sso-only-mode).

## Who can configure it

Providers are managed under **Administration → Settings → Single sign-on (SSO)**, which needs the `settings.sso:write` action. By default only `SUPER_ADMIN` holds it: a provider with *Link existing accounts by email* turned on lets whoever controls that provider sign in as any account with a matching email, so the permission is root-equivalent (see [authorization](../architecture/authorization.md)).

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
| **Require a group or role** | Only users whose verified ID token carries an allowed group or role may sign in — see [Restricting sign-in by IdP group](#restricting-sign-in-by-idp-group). Off by default. |
| **Scopes** | Defaults to `openid profile email`. `openid` is always added. |

## Restricting sign-in by IdP group

To limit a provider to members of specific IdP groups or roles, turn on **Require a group or role** and fill in:

- **Group claim** — the ID token claim that lists the user's groups or roles, such as `groups` or `roles`. A dotted path reads a nested claim (Keycloak realm roles: `realm_access.roles`). A claim whose own name contains dots, such as `https://example.com/groups`, is matched as written first.
- **Allowed groups** — comma-separated values; the user needs at least one. Use stable identifiers (Entra group object IDs, not display names). Matching is exact and case-sensitive.

The check runs on the **verified ID token** after the code exchange and before any account is looked up, linked or created, so it applies to returning users as well as first-time ones: removing someone from the IdP group locks them out at their next SSO sign-in (existing Paca sessions are not revoked). A denied sign-in creates no account and no identity link. The claim may be a string or a list of strings. It fails closed: a missing, `null`, empty, non-string or otherwise malformed claim is denied, and the user sees the generic `group_not_allowed` error. The API log records the reason (`sso: sign-in denied by group policy`) without printing the claim contents. Userinfo-endpoint claims are not consulted. When the policy is off, the claim settings are ignored and sign-in behaves as before. Mapping groups to Paca roles is not supported.

### Microsoft Entra ID

1. In the app registration, open **Token configuration → Add groups claim** and choose the group types you need. To keep tokens small, pick **Groups assigned to the application** and assign only the intended groups to the enterprise application. (Alternatively, define **App roles** and use the `roles` claim.)
2. In Paca, set **Group claim** to `groups` (or `roles`) and **Allowed groups** to the group object IDs (GUIDs) from Entra.

**Groups overage.** Entra stops emitting `groups` when a user belongs to too many groups (200 in a JWT) and sends a reference to Microsoft Graph instead (`_claim_names` / `_claim_sources`). Paca does not resolve that reference, so such users are **denied**. Avoid it by emitting only groups assigned to the application, or by using app roles.

### Other providers

Any OpenID Connect provider that puts groups or roles in the ID token works: configure the provider to include the claim (Okta: a `groups` claim with a filter on the authorization server; Keycloak: a *Group Membership* or *User Realm Role* mapper with *Add to ID token* on, or the built-in `realm_access.roles`), then enter its name and the allowed values in Paca.

## SSO-only mode

By default, an account that signs in through SSO can still sign in with a password if it has one: an account that was linked by email keeps the password it had, and an admin can reset anyone's password. So a user you remove from the IdP (or from the allowed IdP group) could keep signing in with a password. SSO-only mode closes that path.

Turn it on under **Administration → Settings → Login security → SSO-only sign-in** (needs `settings.security:write`). It is off by default, and it can only be turned on while at least one SSO provider is enabled. While it is on:

- The sign-in page shows only the SSO providers. The username and password form is still there for the default super admin, behind a **Sign in as administrator** link (`/?admin=1`).
- The API refuses password sign-in (`POST /auth/login`) for every account except the default super admin, whatever its roles. Other `SUPER_ADMIN` and admin accounts are not exempt. The refusal is the `403` `AUTH_PASSWORD_LOGIN_DISABLED` error, returned only after the password checks out, so a wrong password still reads as one. A login that was waiting on a second factor is refused at that step too.
- Nothing can give another account a working password. The API refuses these requests for such accounts with the same error: an admin password reset (`PATCH /admin/users/{userId}/password`), a set-password link (`POST /auth/password/set`, which leaves the link unused), a password change (`PATCH /users/me/password`), and the password-only MFA enrollment endpoints (`/auth/mfa/setup…`).
- Existing local accounts can't sign in with their password. They need an SSO identity: they sign in through a provider that links accounts by email, or that has already linked them. Linking by email does not open a password path, because the linked account's password is refused like any other.
- An account that still has to change its password (for example, one an admin created with a temporary password) has that requirement dropped when it signs in through SSO, because the requirement could never be met.
- Each password sign-in by the default super admin is logged as a warning (`password sign-in while sso-only mode is on`), with the account and client IP.

SSO sign-in itself is unchanged: the provider options, the [group policy](#restricting-sign-in-by-idp-group) and the login IP allow-lists still apply. Existing sessions are not revoked when the mode is turned on, and API keys and service accounts are not affected. Turning the mode off restores password sign-in for everyone.

### The default super admin

The default super admin is the account Paca creates at startup from `ADMIN_USERNAME` / `ADMIN_PASSWORD`. Every API start marks that account in the database (`users.bootstrap_admin`), and only one account holds the mark. If you change `ADMIN_USERNAME`, the mark moves to the newly configured account at the next start. The mark is never set through the API, and giving another account a role does not move it. If the marked account is renamed, the next start creates a new account named `ADMIN_USERNAME` and moves the mark to it.

Its password sign-in still goes through the rest of the **Login security** policy, so the MFA requirement and IP allow-lists still apply to it. Consider turning them on for its role: in an SSO-only instance it is the only account a password can open.

### Recovery

If the IdP is down or misconfigured, sign in as the default super admin at `/?admin=1` with its username and password. Then fix or disable the provider, or turn SSO-only mode off.

If that password is lost, an admin who can still sign in through SSO and holds `users:write` can reset it: the default super admin's password can still be reset while the mode is on. Changing `ADMIN_PASSWORD` does not change the password of an account that already exists. As a last resort, turn the mode off in the database (`UPDATE auth_security_settings SET sso_only = FALSE;`), which restores password sign-in for every account.

Disabling or deleting the last enabled provider while SSO-only mode is on leaves only the default super admin able to sign in. Re-enable a provider, or turn the mode off.

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
| `group_not_allowed` | The provider requires an IdP group or role and the ID token's group claim is missing, malformed, empty, replaced by an Entra groups-overage reference, or holds none of the allowed values. The API log has the reason. |
| `no_account` | No linked account, and the provider neither links nor creates accounts. |
| `account_exists` | An account already has this email, and linking is off. (In SSO-only mode that account can't use its password either: ask an admin to turn linking on for the provider.) |
| `rate_limited` | Too many SSO sign-in requests from this IP (`AUTH_RATE_LIMIT_PER_MINUTE`). |
