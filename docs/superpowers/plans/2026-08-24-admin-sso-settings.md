# Admin SSO Settings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add encrypted, administrator-managed OIDC settings that validate and become active in the current Paca API process immediately.

**Architecture:** Extend the existing singleton `workspace_settings` persistence model, but keep authentication lifecycle in a focused OIDC runtime manager. The manager loads environment configuration until the first database save, validates a candidate provider before persistence, atomically swaps an immutable active snapshot, and supplies dynamic login options and password-login policy to existing handlers/services.

**Tech Stack:** Go 1.26, chi, PostgreSQL/sqlx, AES-256-GCM, coreos/go-oidc, React 19, TanStack Query/Router, Vitest, Testing Library.

---

### Task 1: Persist SSO Settings And Add The Authentication Permission

**Files:**
- Rename: `services/api/migrations/000041_add_oidc_external_identities.sql` to `services/api/migrations/000042_add_oidc_external_identities.sql`
- Create: `services/api/migrations/000043_add_oidc_settings.sql`
- Create: `services/api/migrations/000044_enforce_sso_admin_invariant.sql`
- Modify: `services/api/internal/domain/settings/entity.go`
- Modify: `services/api/internal/repository/postgres/settings_repository.go`
- Create: `services/api/internal/repository/postgres/settings_repository_test.go`
- Modify: `services/api/internal/platform/authz/permissions.go`
- Modify: `services/api/internal/platform/authz/defaults.go`
- Modify: `services/api/internal/platform/authz/defaults_test.go`

- [ ] **Step 1: Write failing repository and permission tests**

Add a repository round-trip test that inserts the singleton row, stores all OIDC columns through `WithLock`, reads it back, and asserts the ciphertext value is stored unchanged without any plaintext API field. Add an authz test asserting built-in `ADMIN` includes `authentication.write` while existing custom permissions are not widened implicitly.

- [ ] **Step 2: Run the focused tests and verify RED**

Run:

```bash
GOTOOLCHAIN=local go test ./internal/repository/postgres ./internal/platform/authz
```

Expected: compilation or assertion failure because OIDC fields and `PermissionAuthenticationWrite` do not exist.

- [ ] **Step 3: Add the migration and persistence fields**

Add nullable/defaulted columns:

```sql
ALTER TABLE workspace_settings
    ADD COLUMN IF NOT EXISTS oidc_configured BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS oidc_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS oidc_issuer_url TEXT,
    ADD COLUMN IF NOT EXISTS oidc_client_id TEXT,
    ADD COLUMN IF NOT EXISTS oidc_client_secret_enc TEXT,
    ADD COLUMN IF NOT EXISTS oidc_scopes TEXT,
    ADD COLUMN IF NOT EXISTS oidc_redirect_url TEXT,
    ADD COLUMN IF NOT EXISTS oidc_display_name TEXT,
    ADD COLUMN IF NOT EXISTS oidc_username_claim TEXT,
    ADD COLUMN IF NOT EXISTS local_login_enabled BOOLEAN NOT NULL DEFAULT TRUE;
```

Mirror these fields on `settingsdom.WorkspaceSettings`, in `settingsColumns`, the SQL record, entity mapping, and the locked update query.

Add a deferred PostgreSQL constraint trigger that rejects any settings, user,
global-role, or external-identity mutation that would leave an enabled
SSO-only installation without an active `ADMIN` or `SUPER_ADMIN` bound to the
configured issuer. Serialize those checks through the singleton settings row
so concurrent writes across API replicas cannot violate the invariant.

- [ ] **Step 4: Add the independent global permission**

Define:

```go
PermissionAuthenticationWrite Permission = "authentication.write"
```

Grant it to the built-in `ADMIN`; `SUPER_ADMIN` continues to receive it through `*`.

- [ ] **Step 5: Run focused tests and verify GREEN**

Run the Task 1 test command and require exit 0.

### Task 2: Extract Pure OIDC Configuration Validation

**Files:**
- Modify: `services/api/internal/config/load.go`
- Modify: `services/api/internal/config/load_test.go`
- Modify: `services/api/internal/domain/externalidentity/entity.go`

- [ ] **Step 1: Write failing table tests for `NormalizeOIDCConfig`**

Cover enabled required fields, HTTPS/loopback rules, redirect derivation,
default scopes/display name/username claim, forced `openid`, issuer trailing
slash preservation, and disabled OIDC forcing local login on.

- [ ] **Step 2: Run tests and verify RED**

```bash
GOTOOLCHAIN=local go test ./internal/config
```

Expected: failure because the pure normalizer is absent.

- [ ] **Step 3: Refactor environment loading through the pure normalizer**

Expose:

```go
func NormalizeOIDCConfig(cfg OIDCConfig, environment, publicURL string) (OIDCConfig, error)
```

Keep `loadOIDCConfig` responsible only for reading/parsing environment values,
then call the pure function. Correct the external-identity comment to state
that issuer values are stored exactly as verified.

- [ ] **Step 4: Run config tests and verify GREEN**

Run the Task 2 test command and require exit 0.

### Task 3: Implement The Runtime Manager Test-First

**Files:**
- Create: `services/api/internal/service/oidc/manager.go`
- Create: `services/api/internal/service/oidc/manager_test.go`
- Modify: `services/api/internal/service/oidc/service.go`

- [ ] **Step 1: Write failing manager tests**

Use a fake settings repository, fake OIDC service factory, real AES-GCM
encryptor, and fake issuer-scoped admin guard. Cover:

```go
func TestManagerUsesEnvironmentUntilDatabaseConfigured(t *testing.T)
func TestManagerUpdateValidatesBeforePersistAndActivates(t *testing.T)
func TestManagerDiscoveryFailurePreservesPriorState(t *testing.T)
func TestManagerPersistenceFailurePreservesPriorState(t *testing.T)
func TestManagerSecretIsEncryptedAndBlankReplacementPreservesIt(t *testing.T)
func TestManagerRejectsSaveWithoutEncryptor(t *testing.T)
func TestManagerRejectsSSOOnlyWithoutIssuerAdmin(t *testing.T)
func TestManagerDisabledOIDCForcesLocalLogin(t *testing.T)
func TestManagerDelegatesLoginToActiveService(t *testing.T)
```

- [ ] **Step 2: Run tests and verify RED**

```bash
GOTOOLCHAIN=local go test ./internal/service/oidc
```

Expected: compilation failure because `Manager` and its contracts do not exist.

- [ ] **Step 3: Implement the minimal manager**

Provide immutable active snapshots through `atomic.Pointer`, serialize updates
with a mutex, and expose:

```go
type AdminConfig struct { /* non-secret effective fields and source */ }
type UpdateConfig struct { /* complete fields plus optional ClientSecret */ }
func NewManager(ctx context.Context, deps ManagerDeps) (*Manager, error)
func (m *Manager) AdminConfig() AdminConfig
func (m *Manager) Update(ctx context.Context, in UpdateConfig, actor uuid.UUID) (AdminConfig, error)
func (m *Manager) LocalLoginEnabled() bool
func (m *Manager) LoginOptions() LoginOptions
func (m *Manager) BeginLogin(ctx context.Context) (string, string, error)
func (m *Manager) Callback(ctx context.Context, code, state string) (*domainauth.TokenPair, error)
```

Candidate construction/Discovery precedes the database row lock. Persistence
precedes the atomic swap. Disabled snapshots contain no active login service.

- [ ] **Step 4: Run manager and race tests and verify GREEN**

```bash
GOTOOLCHAIN=local go test -race ./internal/service/oidc
```

### Task 4: Make Existing Authentication Paths Dynamic

**Files:**
- Modify: `services/api/internal/service/auth/auth_service.go`
- Modify: `services/api/internal/service/auth/auth_service_test.go`
- Modify: `services/api/internal/transport/http/handler/auth_handler.go`
- Modify: `services/api/internal/transport/http/handler/auth_handler_test.go`
- Modify: `services/api/internal/transport/http/handler/oidc_handler.go`
- Modify: `services/api/internal/transport/http/handler/oidc_handler_test.go`

- [ ] **Step 1: Write failing dynamic-policy tests**

Assert a single auth-service instance changes password-login behavior when a
policy callback changes; `/auth/config` reads fresh manager options on every
request; and a disabled OIDC delegate returns the generic SSO-unavailable API
error while routes remain registered.

- [ ] **Step 2: Run focused tests and verify RED**

```bash
GOTOOLCHAIN=local go test ./internal/service/auth ./internal/transport/http/handler
```

- [ ] **Step 3: Add provider interfaces without breaking static callers**

Keep `WithLocalLoginEnabled(bool)` and `WithLoginOptions(LoginOptions)` as
compatibility wrappers. Add callback/provider variants used by bootstrap.
Change `OIDCHandler` to depend on a small `OIDCLoginService` interface so the
runtime manager can delegate the routes.

- [ ] **Step 4: Run focused tests and verify GREEN**

Run the Task 4 test command and require exit 0.

### Task 5: Add Protected Admin SSO Endpoints And Bootstrap Wiring

**Files:**
- Create: `services/api/internal/transport/http/dto/sso_settings_dto.go`
- Create: `services/api/internal/transport/http/handler/sso_settings_handler.go`
- Create: `services/api/internal/transport/http/handler/sso_settings_handler_test.go`
- Modify: `services/api/internal/transport/http/router/router.go`
- Modify: `services/api/internal/transport/http/router/router_test.go`
- Modify: `services/api/internal/apierr/codes.go`
- Modify: `services/api/internal/transport/http/presenter/response.go`
- Modify: `services/api/internal/bootstrap/app.go`

- [ ] **Step 1: Write failing handler/router tests**

Cover secret masking, acting-user propagation, JSON validation,
`authentication.write` protection on both GET/PATCH, and rejection for a role
holding only `settings.write`. Assert both endpoints reject API-key
authentication even when that key's user has `authentication.write`.

- [ ] **Step 2: Run tests and verify RED**

```bash
GOTOOLCHAIN=local go test ./internal/transport/http/handler ./internal/transport/http/router
```

- [ ] **Step 3: Implement DTOs, handler, routing, errors, and bootstrap**

Wire the shared encryptor, settings repository, identity guard, OIDC service
factory, runtime manager, dynamic auth policy, dynamic public config, always-on
OIDC handler, and admin SSO handler. Map invalid config, unavailable encrypted
storage, failed provider validation, and lockout prevention to stable API
errors without exposing provider internals.

- [ ] **Step 4: Run focused API tests and verify GREEN**

Run the Task 5 command and require exit 0.

### Task 6: Build The Admin SSO UI Test-First

**Files:**
- Create: `apps/web/src/lib/sso-settings-api.ts`
- Create: `apps/web/src/lib/sso-settings-api.test.ts`
- Create: `apps/web/src/components/admin/settings/SSOSettings.tsx`
- Create: `apps/web/src/components/admin/settings/SSOSettings.test.tsx`
- Modify: `apps/web/src/routes/_authenticated/admin/settings/index.tsx`
- Modify: `apps/web/src/components/app-shell/app-sidebar.tsx`
- Modify: `apps/web/src/components/admin/global-roles/permissions.ts`
- Modify: `apps/web/src/i18n/locales/en/admin.json`
- Modify: `apps/web/src/i18n/locales/zh-CN/admin.json`

- [ ] **Step 1: Write failing API and component tests**

Cover query/update payloads, source display, secret configured state, blank
secret preservation, enable/local-login switches, save pending/success/error,
encryption-unavailable state, and permission-aware tab rendering.

- [ ] **Step 2: Run tests and verify RED**

```bash
bun run test -- src/lib/sso-settings-api.test.ts src/components/admin/settings/SSOSettings.test.tsx
```

- [ ] **Step 3: Implement the API client, form, tabs, navigation, and permission catalog**

Use existing UI primitives and TanStack Query. Render `BrandingSettings` only
for `settings.write` and `SSOSettings` only for `authentication.write`; allow
the settings route/sidebar entry when either permission exists.

- [ ] **Step 4: Run tests, typecheck, and lint**

```bash
bun run test -- src/lib/sso-settings-api.test.ts src/components/admin/settings/SSOSettings.test.tsx
bun run build
bun run lint
```

### Task 7: Documentation And End-To-End Verification

**Files:**
- Modify: `docs/deployment/oidc-sso.md`
- Modify: `docs/superpowers/specs/2026-08-24-admin-sso-settings-design.md`

- [ ] **Step 1: Document admin configuration precedence and rollout**

Replace deployment-only instructions with both UI and environment bootstrap
paths, write-only secret behavior, SSO-only staging, multi-replica rolling
restart requirement, and recovery ownership of `ENCRYPTION_KEY`.

- [ ] **Step 2: Run the full verification set**

```bash
GOTOOLCHAIN=local go test ./...
bun run test
bun run build
bun run lint
git diff --check
```

- [ ] **Step 3: Verify the bound local development stack**

Wait for the API watcher to rebuild, then verify:

```bash
curl -fsS http://127.0.0.1:3000/api/v1/auth/config
docker logs --tail 100 paca-local-api-1
```

If no test IdP is available, verify the disabled configuration and protected
admin endpoints live, and report that enabled-provider Discovery was covered by
the manager's real HTTP test server rather than a local external IdP smoke.
