# Writing a Git provider plugin

A *Git provider* adapts one source-control platform (GitHub, GitLab, Bitbucket, Gitea, …) to Paca's platform-neutral Git integration. Paca core owns triggers, routing, policy, runs, status comments, audit, MCP tools and UI; your plugin only answers platform-specific questions. How the integration works end to end is in [../ai-agent/git-agent-workflow.md](../ai-agent/git-agent-workflow.md); this page is the author's guide. Source: `plugin-sdk-go/gitprovider`.

## 1. Skeleton

```go
package main

import (
	plugin "github.com/Paca-AI/plugin-sdk-go"
	"github.com/Paca-AI/plugin-sdk-go/gitprovider/serve"
)

type myPlugin struct{}

func (p *myPlugin) Init(ctx *plugin.Context) error {
	serve.Register(ctx, newProvider(ctx)) // mounts POST /_git/:op (internal-only)
	return nil
}
func (p *myPlugin) Shutdown() {}

func main() { plugin.Run(&myPlugin{}) }
```

`newProvider` returns a type implementing `gitprovider.Provider` plus whichever optional interfaces your platform supports. Declare the capability `git-provider` in `plugin.json`; everything else about the provider (hosts, identity kinds, credential model, capabilities) is reported by `Describe`. Core refuses a provider whose reported `spi_version` has another major version than the one it implements (`gitprovider.v1`).

## 2. What you implement

**Required** (every platform has them): `Describe`, `BeginIdentitySetup`, `CompleteIdentitySetup`, `ListRepositories`, `ResolveRepository`, `VerifyAndParseWebhook`, `MintCredential`, `GetActorPermission`, `GetBranchProtection`, `GetChangeRequest`, `ListChangedFiles`, `GetDiff`, `ListThreads`, `ListComments`, `GetIssue`, `GetChecks`, `GetCheckLogs`, `OpenChangeRequest`, `UpdateChangeRequest`, `Comment`, `ReplyThread`, `SubmitReview`, `SetStatus`.

**Optional** — implement the Go interface *and* set the capability flag, or neither:

| Interface | Capability |
|---|---|
| `ThreadResolver` | `resolve_threads` |
| `FileReader`, `CodeSearcher` | `file_read`, `code_search` |
| `LabelManager`, `ReviewerRequester`, `AssigneeSetter` | `labels`, `reviewer_requests`, `assignees` |
| `Merger`, `CheckRerunner`, `Reactor` | `merge`, `rerun_checks`, `reactions` |
| `WebhookManager` | `webhook_management` (repo/group-scoped webhooks) |
| `IdentityDeleter` | `identity_deletion` |
| `BranchManager` (`GetBranch`, `CreateBranch`) | `branches` |
| `AccessGranter` | `access_grants` (see below) |

Anything you do not support simply is not implemented; the wire layer answers `unsupported`, and core never calls it because the capability is false.

## 3. Rules a provider follows (enforced by the SDK conformance suite)

* **Honest capabilities** — both directions: a flag without an implementation, or an implementation without a flag, fails.
* **Webhooks**: reject a bad signature with `invalid_signature` and produce no events; normalize to the neutral `Event` (kinds, actions, repo, subject, actor, location, check); report `addressed[]` only for ways of being addressed that you declare in `triggers`; produce **deterministic event ids** (core de-duplicates on them); drop events you cannot map instead of guessing; never put secrets in an event.
* **Errors** are `*gitprovider.Error` with a code from the closed set (`not_found`, `forbidden`, `invalid_signature`, `conflict`, `rate_limited`, `validation`, `unsupported`, `transient`, `internal`). Map platform failures onto them; never include tokens, secrets or raw payloads in a message. Unclassified errors are rejected. The `Message` of a `not_found` or `forbidden` answer from `ResolveRepository` (linking a repository) and of any failed `EnsureWebhook` retry (whatever the code, except `internal`) is shown to the person in the settings page under the generic text (redacted and capped at 500 characters), so make it say what to fix, for example which repository a token covers; core still decides the error code.
* **Credentials**: `ephemeral` platforms must set `ExpiresAt`; when the request names a lifetime (`ttl_seconds`), `ExpiresAt` must not be later than it (a platform that cannot shorten its tokens reports the requested lifetime, which only makes callers stop using the token sooner); clone URLs must be credential-free.
* **Reads**: paging must visit each item exactly once; `MaxBytes`/`TailBytes` limits must set `Truncated` (silent truncation hides content); unknown things are `not_found`; repositories the identity cannot see are `not_found` or `forbidden`.
* **Branches**: a missing branch is `not_found`, a taken name is `conflict`, a missing source is `not_found`, an invalid name is `validation`; a created branch points at the source branch's commit.
* **Writes**: validate (`validation`), reject duplicates (`conflict`), return ids core can index (`Comment`, `ReplyThread`), honour idempotency keys if you set `idempotent_writes`, emulate batch review when `batch_review` is false and say so (`Emulated`), require `ExpectedHeadSHA` for merges.
* **Wire**: every operation survives a malformed body without panicking.

## 4. Conformance

The SDK ships a conformance suite (`plugin-sdk-go/gitprovider/conformance`) and a reference fake provider (`gitprovider/fakeprovider`). A provider must pass the suite against a fake backend of its own platform: recorded webhook deliveries with the events a correct adapter produces, and canned API responses. The suite checks every rule in §3 in both directions, so a provider that passes is one core can drive safely.

## 5. Platform specifics you decide

* **Identity kind and setup**: `redirect` / `form_post` (GitHub's manifest flow) / `token_form` (list the inputs in `Form`: platforms with static credentials usually want a read-only and a write token) / `done`. Report `Identity.ExpiresAt` if credentials expire.
* **Several kinds**: a platform that supports more than one way to connect (GitHub: an App, or a personal access token) lists them in `IdentityKinds` and refines the provider-wide `CredentialModel`, `WebhookScope` and `AccessGrants` per kind in `Capabilities.Kinds` (`Capabilities.Profile(kind)` falls back to the provider-wide values). Core chooses the kind by passing `Params["kind"]` to `BeginIdentitySetup`; absent means the first (default) kind. A kind whose `WebhookScope` is `repo` or `group` is registered per repository by `WebhookManager.EnsureWebhook` (core calls it when a repository is linked and `RemoveWebhook` when the last link goes); `EnsureWebhook` must be idempotent for the same URL, and a provider that cannot register one answers an error (the link still succeeds and the user is told the webhook is missing and can retry); generate the signing secret yourself, as the setup form does not ask for one. `IdentityRef.ID` must be unique per connection (use the provider's own row id, never the platform account: two tokens of the same user are two identities); core never merges two connections that report the same id. Several connections may exist per host, workspace-wide or per project, so `ResolveRepository` receives every candidate the project can use and picks the first that sees the repository.
* **Credential model**: `ephemeral` (mint per use, down-scoped) or `static` (return the stored secret; core never exposes it to agents and does API calls through you).
* **Webhook scope**: `app` (nothing to register), or `group`/`repo` (implement `WebhookManager`; users without rights can paste the URL and secret by hand).
* **Status kind**: check run, commit status, or comment — pick the best your platform offers.
* **CI config globs** (`ci_config_globs`): paths agents must not modify (`.github/workflows/**`, `.gitlab-ci.yml`, …).
* **Terms**: what the UI calls things ("Pull request" / "Merge request").
* **Self-hosted hosts**: list the default hosts and set `self_hosted_hosts`.

## 6. What you do not implement

Trigger configuration, routing, actor policy, run lifecycle, debounce, comment/thread ownership, status comments, audit, MCP tools, workspace protocol, credential-helper plumbing, task linking and every screen except your identity setup component. If you find yourself writing one of those, the SPI is missing something — raise it.

## Access grants (optional)

Some platforms only let an identity see the repositories of accounts it has been granted access to, and a *user* grants that on the platform. GitHub is the example: a GitHub App sees only the accounts and repositories it is installed on. Because each project can use a repository owned by a different user or organization, this cannot happen once, when the identity is created.

A provider declares `access_grants` and implements `AccessGranter`:

| Operation | Purpose |
|---|---|
| `begin_access_grant` | `{identity, owner?, return_url, callback_url}` → a `SetupStep` (redirect or form post) to the platform's grant page. `owner` lets the platform pre-select the account. The step carries a `state`. |
| `complete_access_grant` | `{identity, state, params}` (the platform's redirect parameters) → `{owner}`, the account now covered. |

Rules a provider must follow (the conformance suite checks them): a state works once; a missing, unknown or reused state grants nothing; begin for an unknown identity is `not_found`/`forbidden`; no secret appears in a step or a grant.

Core drives it from the project: linking a repository the identity cannot see answers `GIT_ACCESS_NOT_GRANTED` (only for providers that declare `access_grants`), the UI offers "Install on `<owner>`", and the platform returns to `/git/setup/callback`. Core stores the state against the user and project that started it (`git_access_sessions`) and only that user can complete it, once.

Providers should also cope with the platform changing underneath them: a repository is only reported as resolvable when a credential can actually be minted for it (a public repository answers any token), a revoked cached credential is replaced once, and a grant the platform no longer has is forgotten so the owner reads as "not granted" again.

A grant targets one identity: the host's shared identity from a project's Git tab, or an agent's own identity from the agent's Git tab. The provider only sees an `IdentityRef`; core stores which identity the state was issued for.
