# OIDC SSO for Paca: Identity, Security, and Admin Configuration

**Status:** Proposal for discussion

**Implementation status:** The `feat/oidc-sso` branch already implements the
core OIDC protocol, identity, session, security, Compose, and Helm work
described below. The administrator configuration surface and runtime
reconfiguration described in this proposal are not implemented yet.

## Summary

Paca should support one standards-compliant OpenID Connect provider per
installation for human sign-in. OIDC proves the person's external identity;
Paca remains responsible for users, sessions, authorization, API keys, Agent
credentials, and audit-relevant state.

The current branch configures OIDC through deployment environment variables.
This proposal completes the product experience by adding a protected Single
sign-on section to the admin settings page. A valid configuration is checked
with OIDC Discovery, encrypted, persisted, and activated in the current API
process when the administrator saves it.

### Proposed Decisions

| Area | Decision |
| --- | --- |
| Protocol | OIDC Authorization Code Flow with PKCE S256 and Discovery |
| Provider count | One OIDC provider per Paca installation |
| Identity key | Exact `(issuer, subject)` from the verified ID token |
| Session owner | Paca issues and refreshes its own JWT/cookie session |
| Provisioning | JIT is always enabled; new users receive built-in `USER` |
| Existing-account linking | Never automatic by email or username |
| Local login | Available during staged rollout; may be disabled after an issuer-scoped SSO admin exists |
| Configuration | Admin UI backed by encrypted database settings; environment remains the bootstrap source |
| Activation | Validate first, then activate in the API process that accepted the update |
| Secret storage | AES-256-GCM using the existing `ENCRYPTION_KEY`; never plaintext fallback for OIDC secrets |
| Multi-replica behavior | No live broadcast in this version; other replicas require a rolling restart |

## Motivation

Paca currently authenticates human users with local usernames and passwords.
Organizations commonly need their existing identity provider to enforce
central account lifecycle, MFA, and sign-in policy. A generic OIDC integration
covers providers such as Keycloak, Microsoft Entra ID, Okta, and Authentik
without coupling Paca to one vendor.

The protocol alone is not a complete product experience. Requiring an
administrator to edit container environment variables for every issuer,
client, label, or local-login change makes routine identity configuration an
infrastructure operation. The final experience should be manageable from the
same admin surface as other installation-wide settings while preserving a
safe deployment bootstrap and recovery path.

## Design Principles

1. **Authentication is not authorization.** The IdP proves identity. Paca's
   Global RBAC remains authoritative.
2. **Stable protocol identity beats mutable profile data.** Only the verified
   `(iss, sub)` pair identifies an account. Email, display name, and username
   are attributes.
3. **No external token crosses into the Paca product session.** IdP access and
   ID tokens are not stored, returned to the SPA, or given to Agents.
4. **A security control must not silently weaken on failure.** Invalid
   configuration, missing encryption, and unsafe SSO-only transitions fail
   closed while the last active configuration remains in service.
5. **Keep the first version operationally small.** One provider, one admin
   form, one runtime snapshot, and no distributed configuration protocol.

## Goals

- Let human users sign in through a standard OIDC provider.
- Preserve Paca JWT sessions, authorization, and machine credentials.
- Provision a safe low-privilege Paca user on first successful sign-in.
- Support mixed local-password and SSO login during rollout.
- Allow a verified SSO-only deployment without locking out all administrators.
- Let authorized administrators configure and activate SSO from the web UI.
- Encrypt the client secret at rest and never return it through an API.
- Keep existing environment, Compose, and Helm configuration compatible.

## Non-Goals

This proposal does not include:

- SAML or LDAP;
- SCIM user lifecycle;
- multiple or workspace-specific IdPs;
- social login;
- IdP group-to-Paca-role mapping;
- automatic account linking by email or username;
- administrator-driven external-identity linking or unlinking;
- back-channel logout or real-time IdP session revocation;
- logging the user out of the IdP when they log out of Paca;
- remote MCP OAuth;
- cluster-wide live configuration broadcasting;
- configuration version history or a separate connection-test workflow.

## Trust And Credential Boundaries

The feature applies only to interactive human login.

| Credential or state | Authority | OIDC impact |
| --- | --- | --- |
| External user authentication | IdP | New sign-in proof |
| Paca browser session | Paca | Issued after verified OIDC login |
| Global and project RBAC | Paca | Unchanged |
| Personal API keys | Paca | Unchanged |
| Agent MCP keys | Paca | Unchanged |
| ACP Bridge tokens | Paca | Unchanged |
| IdP access/ID tokens | IdP/Paca callback only | Never persisted or exposed to SPA/Agents |

The SPA does not perform the code exchange and never receives the OIDC client
secret, authorization code, access token, refresh token, or ID token.

## Architecture

```text
Browser
  |
  | GET /api/v1/auth/config
  | GET /api/v1/auth/oidc/login
  v
Paca API -----------------------> OIDC Provider
  |      Authorization Code          |
  |      + PKCE + state + nonce      |
  |                                  |
  | GET /api/v1/auth/oidc/callback <-+
  | verify state cookie, code, ID token, issuer, audience, expiry, nonce
  v
user_external_identities (issuer, subject)
  |
  +--> existing Paca user
  |        or
  +--> atomic JIT user + identity binding
  |
  v
Paca IssueSessionForUser
  |
  v
Paca access/refresh JWTs in HttpOnly cookies
```

Valkey stores only the short-lived, single-use login transaction
`state -> {nonce, PKCE verifier}`. PostgreSQL stores the durable external
identity binding and, after this proposal, encrypted administrative OIDC
configuration.

## Login Flow

### 1. Login Entry-Point Discovery

The login page calls the public endpoint:

```text
GET /api/v1/auth/config
```

It returns display-only state:

```json
{
  "data": {
    "local_login_enabled": true,
    "oidc": {
      "enabled": true,
      "display_name": "Company SSO"
    }
  }
}
```

When OIDC is enabled, the page renders `Continue with Company SSO`. When local
login is disabled, it hides the password form. The backend also enforces that
policy in the auth service; hiding a form is not the security boundary.

### 2. Begin Login

The browser navigates to:

```text
GET /api/v1/auth/oidc/login
```

Paca generates cryptographically random state, nonce, and PKCE verifier
values. It stores the nonce and verifier in Valkey under the state value for
10 minutes, writes the same state to a short-lived HttpOnly `SameSite=Lax`
cookie scoped to the callback path, and redirects to the provider's
authorization endpoint.

### 3. Callback And Token Verification

The provider redirects to:

```text
GET /api/v1/auth/oidc/callback
```

Paca requires the URL state and browser-bound state cookie to match, clears
the cookie on every callback exit, consumes the Valkey transaction with
single-use `GETDEL`, exchanges the code with the PKCE verifier, and verifies
the ID token's signature, issuer, audience, expiry, and nonce.

Provider Discovery, code exchange, JWKS retrieval, and UserInfo requests use a
dedicated HTTP client with a 10-second timeout.

### 4. Identity Resolution And Provisioning

The verified ID token's exact `(issuer, subject)` resolves the Paca user. If no
binding exists, JIT provisioning atomically creates both the user and external
identity. A concurrent first-login unique-index race resolves the binding
created by the winning transaction instead of failing the other login.

### 5. Paca Session Issuance

OIDC and password login converge at `IssueSessionForUser`. Paca reads the
current user and Global Role, issues its normal access and refresh token pair,
and writes the existing HttpOnly cookies. Session refresh re-reads the user's
current role so promotions and demotions apply on the next refresh and do not
survive refresh-token rotation as stale claims.

### 6. Logout

`POST /api/v1/auth/logout` revokes the Paca token family and clears Paca
cookies. It does not end the provider's session, because doing so would affect
the user's other applications and requires provider-specific logout behavior.

## Identity And Account Model

### Durable Identity

```sql
user_external_identities (
    id,
    user_id      references users(id) on delete cascade,
    provider     = 'oidc',
    issuer,
    subject,
    created_at,
    last_login_at,
    unique (issuer, subject)
)
```

The issuer is an identifier, not a URL to normalize. Paca trims surrounding
configuration whitespace but preserves the issuer value, including a trailing
slash, and requires exact agreement among configured issuer, Discovery
metadata, and the ID token `iss` claim.

### Profile Attributes

- The configured username claim defaults to `preferred_username`.
- ID-token profile values win. UserInfo only fills missing username, name, or
  email attributes.
- UserInfo is ignored unless its `sub` matches the ID token's subject.
- Email is stored only when the provider explicitly returns
  `email_verified=true`.
- An invalid or unusable username falls back to
  `sso-<first 12 hex characters of SHA-256(issuer NUL subject)>`.
- Username collisions receive a numeric suffix.
- Email collisions cause the new account to omit email; they never link it to
  the existing account.

### SSO-Provisioned Accounts

JIT users receive an unknown random password hash and
`password_login_enabled=false`. Password login, password change,
administrator reset, and password-set-token flows all reject these accounts.
This prevents an administrator reset from accidentally adding a second
authentication path to an SSO-only identity.

## Authorization Model

- JIT users always receive the built-in `USER` Global Role.
- The default role is not configurable. Paca roles are customizable
  permission sets, so a configurable role name could point to a role carrying
  elevated or wildcard permissions.
- A Paca administrator explicitly promotes an SSO user when required.
- IdP groups and claims do not grant Paca permissions in this version.
- Existing project roles and all machine authorization remain unchanged.

Two independent controls govern password login:

1. `local_login_enabled` is the installation-wide entry-point policy.
2. `users.password_login_enabled` is the account-specific policy for
   SSO-provisioned users.

Both must allow password authentication before a password login can succeed.

## SSO-Only Rollout And Lockout Prevention

A fresh Paca installation starts with a local-password administrator, while
JIT users always begin as `USER`. SSO-only mode therefore requires a staged
rollout:

1. enable OIDC while keeping local login enabled;
2. sign in through SSO to create the target administrator's binding;
3. use the local administrator to grant that user `ADMIN` or `SUPER_ADMIN`;
4. verify the SSO administrator can sign in and administer Paca;
5. disable local login.

Paca rejects step 5 unless an active `ADMIN` or `SUPER_ADMIN` has an OIDC
binding for the exact currently configured issuer. A privileged binding from
an old or different provider does not satisfy the guard. Deleted users do not
satisfy it either.

Disabling OIDC always forces local login enabled. This prevents an
installation with no human login entry point.

## Security Model

| Threat | Control |
| --- | --- |
| Authorization-code interception | Authorization Code Flow with PKCE S256 |
| Login CSRF/session swapping | Random state plus matching browser-bound HttpOnly state cookie |
| Callback replay | Valkey `GETDEL` single-use transaction with 10-minute TTL |
| Forged or replayed ID token | Signature/JWKS, issuer, audience, expiry, and nonce verification |
| Account takeover by matching email | Identity binds only by `(issuer, subject)` |
| UserInfo identity substitution | UserInfo `sub` must match verified ID-token `sub` |
| JIT privilege injection | Fixed built-in `USER` role; no configurable default role |
| Stale elevated authorization | Session refresh re-reads current username and role |
| Administrator lockout | Issuer-scoped privileged-user guard before SSO-only mode |
| Secret disclosure through SPA/API | Client secret is write-only and never returned |
| Secret disclosure at rest | AES-256-GCM with existing `ENCRYPTION_KEY` |
| Slow or unavailable IdP | Dedicated 10-second outbound timeout and validation before activation |
| Callback/code leakage | `Referrer-Policy: no-referrer` and generic browser errors |

Server logs may record the issuer, Paca user ID, acting administrator ID, and
high-level outcome. They must not record client secrets, authorization codes,
PKCE values, access tokens, ID tokens, or raw provider error bodies.

## Current Deployment Configuration

The existing branch supports environment, Compose, and Helm configuration:

```text
OIDC_ENABLED
OIDC_ISSUER_URL
OIDC_CLIENT_ID
OIDC_CLIENT_SECRET
OIDC_SCOPES
OIDC_REDIRECT_URL
OIDC_DISPLAY_NAME
OIDC_USERNAME_CLAIM
LOCAL_LOGIN_ENABLED
```

The redirect defaults to
`<PUBLIC_URL>/api/v1/auth/oidc/callback`. Production issuer and redirect URLs
must use HTTPS; loopback HTTP issuers are allowed for local development. Helm
stores the client secret in a Kubernetes Secret rather than embedding it in a
Deployment value.

Environment configuration remains backward compatible and is the effective
source on installations that have never saved SSO configuration through the
admin UI.

## Proposed Admin Configuration

### Permission Boundary

SSO configuration controls authentication for the whole installation and is
more sensitive than workspace branding. It must not reuse `settings.write`,
whose current contract only grants logo, favicon, name, and color changes.

Add `authentication.write` as a global permission. Grant it to the built-in
`ADMIN` role and, through wildcard permission, `SUPER_ADMIN`. Custom roles do
not gain it automatically. Both reading the non-secret SSO configuration and
updating it require this permission.

### Admin UI

The existing `/admin/settings` page gains permission-aware **Branding** and
**Single sign-on** tabs. The SSO tab contains:

- OIDC enabled switch;
- issuer URL;
- client ID;
- write-only client secret replacement field;
- scopes;
- redirect URL override and the effective callback URL;
- login-button display name;
- username claim;
- local password login switch;
- one Save command.

Provider fields remain editable while OIDC is disabled, allowing an
administrator to prepare the configuration. A blank client-secret field means
"keep the currently configured secret". The API returns only whether a secret
is configured.

Save performs validation and, when enabled, OIDC Discovery. There is no
separate Test Connection action in the first version because Save already has
the same validation requirement. A successful save updates the login page
immediately. A failure keeps the form open and leaves the existing database and
runtime configuration unchanged.

When encrypted storage is unavailable, the page remains readable but saving
database-backed SSO configuration is disabled with a clear error. OIDC client
secrets never use the existing plaintext compatibility fallback used by some
older secret-bearing features.

## Configuration Persistence And Precedence

Extend the singleton `workspace_settings` row with:

```text
oidc_configured                 boolean
oidc_enabled                    boolean
oidc_issuer_url                 text
oidc_client_id                  text
oidc_client_secret_enc          text
oidc_scopes                     text
oidc_redirect_url               text
oidc_display_name               text
oidc_username_claim             text
local_login_enabled             boolean
```

`oidc_configured=false` means the installation has not adopted database-backed
configuration and continues to use deployment configuration. The first
successful admin save sets it to true. From then on the database snapshot is
authoritative, including an intentional `oidc_enabled=false`; stale deployment
variables do not silently re-enable SSO.

The client secret is encrypted before persistence with the existing
AES-256-GCM encryptor. The encryption key remains deployment-owned and is
never stored in PostgreSQL. Losing or changing that key makes the stored secret
undecryptable and fails startup rather than silently disabling or weakening
authentication.

## Admin API

Add two endpoints under the existing admin settings resource:

```text
GET   /api/v1/admin/settings/sso
PATCH /api/v1/admin/settings/sso
```

Example read response:

```json
{
  "data": {
    "source": "environment",
    "enabled": true,
    "issuer_url": "https://id.example.com/realms/company",
    "client_id": "paca",
    "client_secret_configured": true,
    "scopes": ["openid", "profile", "email"],
    "redirect_url": "https://paca.example.com/api/v1/auth/oidc/callback",
    "display_name": "Company SSO",
    "username_claim": "preferred_username",
    "local_login_enabled": true,
    "encrypted_secret_storage_available": true
  }
}
```

Example update request:

```json
{
  "enabled": true,
  "issuer_url": "https://id.example.com/realms/company",
  "client_id": "paca",
  "client_secret": "write-only replacement; omit to preserve",
  "scopes": ["openid", "profile", "email"],
  "redirect_url": "https://paca.example.com/api/v1/auth/oidc/callback",
  "display_name": "Company SSO",
  "username_claim": "preferred_username",
  "local_login_enabled": true
}
```

The response never contains the client secret or encrypted ciphertext.
Submitting an omitted or empty client secret preserves the effective existing
secret. When switching from environment to database configuration, the API may
encrypt and persist the already active environment secret if no replacement is
provided.

## Runtime Reconfiguration

A small runtime manager owns one immutable active snapshot and an optional
OIDC service. It:

1. provides the public login-page options;
2. delegates begin-login and callback requests to the active OIDC service;
3. provides the current installation-wide local-login policy to the auth
   service.

OIDC routes are registered regardless of startup configuration. When OIDC is
disabled, the manager returns the existing generic SSO-unavailable error. The
public `/auth/config` endpoint reads the active snapshot on every request
instead of retaining startup-only booleans.

An admin update is serialized within the API process and follows this order:

1. normalize and validate the submitted fields;
2. resolve the retained or replacement client secret;
3. if enabled, construct a candidate OIDC service and complete Discovery;
4. enforce the issuer-scoped privileged-user guard for SSO-only mode;
5. encrypt and persist the new singleton settings row;
6. atomically replace the active runtime snapshot.

No active state changes before persistence succeeds. The final activation is
an in-memory pointer swap with no expected failure path. Existing Paca browser
sessions are unaffected. A login transaction started against the prior
provider may fail after a configuration change and must be restarted; retaining
multiple provider generations is deliberately outside this version.

At API startup:

1. load the existing deployment configuration;
2. load `workspace_settings` after PostgreSQL is available;
3. use database configuration when `oidc_configured=true`, otherwise use the
   deployment configuration;
4. decrypt the database client secret;
5. initialize the runtime manager and perform Discovery when OIDC is enabled;
6. enforce the SSO-only privileged-user guard.

The auth service consults the manager's local-login policy at request time, so
password login remains blocked below the HTTP presentation layer.

### Multiple API Replicas

The Helm chart defaults to one API replica. This proposal guarantees immediate
activation only in the API process that handles the update. PostgreSQL stores
the new source of truth, but another already-running replica retains its prior
snapshot until restarted.

Deployments with multiple API replicas must perform a rolling restart after an
SSO change. Database notifications, Valkey pub/sub, polling, and snapshot
versioning are deferred until multi-replica live administration is a concrete
requirement.

## Failure And Recovery Semantics

- Field validation failure: reject without Discovery, persistence, or runtime
  change.
- Discovery failure: return a generic admin-facing error; log a sanitized
  server-side cause; keep the prior configuration.
- Missing encryption: reject before persistence.
- Database write failure: keep the prior runtime configuration.
- Runtime activation: occurs only after the database write and cannot perform
  network or storage work.
- Provider change during login: the in-flight login may fail generically; the
  user retries.
- OIDC disable: retain external identity bindings and encrypted configuration
  fields; force local login on.
- IdP outage after activation: existing Paca sessions continue until their
  normal expiry; new OIDC logins fail. Local login remains the staged rollout
  and break-glass path when enabled.
- Startup with an enabled but unreachable IdP or undecryptable database secret:
  fail fast instead of starting with a different authentication policy.

Administrators should keep local login enabled until an SSO administrator is
verified. Database backup and protection of `ENCRYPTION_KEY` are part of the
same recovery boundary; a database backup without its encryption key cannot
restore the OIDC client secret.

## Compatibility And Rollout

- Existing installations remain local-login only by default.
- Existing environment, Compose, and Helm OIDC settings continue to work.
- The database migration adds nullable/defaulted singleton columns and does
  not alter existing external identity bindings or users.
- An installation stays environment-backed until its first successful admin
  save.
- The login page and public auth config retain backward-compatible defaults
  when OIDC is disabled or the config request fails.
- Existing API keys, Agents, ACP bridges, automations, and sessions are not
  migrated.

Recommended rollout:

1. deploy the OIDC-capable API with local login still enabled;
2. configure and save the provider in Admin Settings;
3. verify login and JIT provisioning;
4. promote and verify at least one SSO administrator;
5. optionally disable local login;
6. for multi-replica deployments, roll all API replicas after configuration
   changes.

## Testing Strategy

### Existing OIDC Behavior

- Discovery and redirect construction;
- state-cookie binding and clearing on every callback outcome;
- single-use Valkey login transactions;
- ID-token issuer, audience, expiry, signature, and nonce validation;
- PKCE exchange;
- ID-token-first and subject-matched UserInfo merge behavior;
- verified-email handling;
- JIT provisioning, username collisions, and concurrent first login;
- fixed `USER` role;
- password operations rejected for SSO-only accounts;
- role promotion and demotion reflected at session refresh;
- issuer-scoped SSO-only lockout guard.

### Admin Configuration

- migration and repository round trips without plaintext secret exposure;
- new `authentication.write` permission and denial for branding-only roles;
- environment fallback and database precedence;
- encrypted secret creation, retention, replacement, and decryption failure;
- valid Discovery followed by persistence and immediate activation;
- validation, Discovery, encryption, lockout, and persistence failures
  preserving the prior runtime snapshot;
- disabled OIDC forcing local login on;
- dynamic `/auth/config` and OIDC route behavior;
- service-level dynamic password-login enforcement;
- serialized concurrent updates within one process;
- admin form loading, secret masking, pending, success, and error states;
- permission-aware Branding and Single sign-on tabs.

A local smoke test uses a loopback OIDC test provider, saves configuration
through the admin API, verifies `/api/v1/auth/config`, and confirms the login
page shows the configured SSO action without restarting the API.

## Alternatives Considered

### Keep Deployment-Only Configuration

This preserves the smallest backend but makes identity administration depend
on container, Helm, or host access. It does not provide the requested product
configuration experience.

### Save In The UI But Require Restart

This avoids a runtime manager, but the UI would report saved configuration
that is not yet effective and would still require deployment access. The
single-process atomic snapshot is small enough to avoid that mismatch.

### Distributed Live Configuration

PostgreSQL notifications, Valkey pub/sub, polling, or versioned snapshots could
update every API replica. The chart defaults to one API replica and there is no
demonstrated need for that permanent machinery yet, so this is deferred.

### Automatic Email Linking

Email is mutable, recyclable, and not equivalent across issuers. Treating an
attribute match as proof of account ownership creates an account-takeover path.
Explicit administrator linking can be designed separately.

### IdP-Driven Default Roles

Mapping arbitrary claims or a configurable default role directly to Paca RBAC
can grant elevated custom permissions to every new user. The first version
fixes JIT to built-in `USER`; group-to-role mapping needs a separate policy,
preview, and audit design.

### Pass IdP Tokens Through As Paca Sessions

This would couple every Paca authorization path and machine credential to
provider token semantics. Issuing the existing Paca session keeps one
authorization model and contains OIDC at the human login boundary.

## Discussion Questions

1. Is one OIDC provider per installation sufficient for the first release?
2. Is `authentication.write` the right permission boundary, distinct from
   branding-only `settings.write`?
3. Is database precedence after the first admin save intuitive, or should the
   product provide an explicit "return to deployment configuration" action?
4. Is a rolling restart acceptable for uncommon multi-replica SSO changes, or
   is cross-replica live propagation required immediately?
5. Is retrying an in-flight login after provider reconfiguration an acceptable
   first-version tradeoff?
6. Which explicit account-linking workflow should be designed before allowing
   an existing local account to adopt an external identity?
7. Should future logout support provider logout, and if so, should it be an
   installation policy or a per-user choice?
