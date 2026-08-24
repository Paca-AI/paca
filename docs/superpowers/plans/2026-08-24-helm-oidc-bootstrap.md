# Helm OIDC Bootstrap Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the chart's existing OIDC startup configuration copyable and verifiable for first-time Helm initialization.

**Architecture:** Keep provider fields in `api.oidc` and the confidential client secret in `secrets.oidcClientSecret` or an externally managed Secret. The API continues to choose environment configuration until the administrator settings page has persisted a database configuration; the chart does not write application tables.

**Tech Stack:** Helm templates, YAML values, Markdown documentation, POSIX shell assertions.

---

### Task 1: Add a copyable OIDC values example

**Files:**
- Create: `deploy/helm/paca/examples/oidc-values.yaml`

- [ ] **Step 1: Write the example values file**

Include `publicUrl`, `api.oidc` provider fields, and the secret key reference
without embedding a real secret. Keep `localLoginEnabled: true` in the example
so a fresh install remains administrable until an SSO user has been promoted.

- [ ] **Step 2: Validate YAML structure**

Run:

```bash
ruby -e 'require "yaml"; YAML.load_file("deploy/helm/paca/examples/oidc-values.yaml"); puts "yaml ok"'
```

Expected: `yaml ok`.

### Task 2: Document the Helm initialization path

**Files:**
- Modify: `deploy/helm/paca/README.md`
- Modify: `deploy/helm/paca/values.yaml`

- [ ] **Step 1: Add the OIDC secret to the Secrets table**

Document `secrets.oidcClientSecret` and the equivalent key required when
`secrets.existingSecret` is used.

- [ ] **Step 2: Add a Helm installation example**

Show the example values file, callback URL registration, and the two-stage
rollout: deploy with password login enabled, bind/promote an SSO administrator,
then set `api.oidc.localLoginEnabled: false` only in a deliberate upgrade.

- [ ] **Step 3: Clarify source ownership in chart values comments**

State that the Helm values initialize the environment source only; saving the
admin SSO page switches the runtime to database-owned settings and subsequent
Helm upgrades do not replace those settings.

### Task 3: Add Helm render assertions

**Files:**
- Create: `deploy/helm/paca/tests/render-oidc.sh`

- [ ] **Step 1: Render the default chart and assert OIDC is absent**

Run `helm template` with chart defaults and fail if the rendered API
Deployment contains `OIDC_ENABLED`.

- [ ] **Step 2: Render the enabled example and assert placement**

Render with `examples/oidc-values.yaml`, assert the Deployment contains the
issuer/client ID/redirect/display/local-login values, and assert the Secret
contains `OIDC_CLIENT_SECRET` while the Deployment does not contain the secret
literal.

- [ ] **Step 3: Make the script fail closed and executable**

Use `set -eu`, require `helm` in `PATH`, clean its temporary directory with a
trap, and exit non-zero for any missing or misplaced assertion.

### Task 4: Verify and commit

**Files:**
- Verify: all files above

- [ ] **Step 1: Run chart lint and render assertions**

```bash
helm lint deploy/helm/paca
deploy/helm/paca/tests/render-oidc.sh
```

Expected: lint succeeds and the script prints both default-disabled and
enabled-placement checks as passing.

- [ ] **Step 2: Check the diff**

```bash
git diff --check
git status --short
```

Expected: no whitespace errors and only the planned files changed.

- [ ] **Step 3: Commit**

```bash
git add deploy/helm/paca/examples/oidc-values.yaml deploy/helm/paca/README.md deploy/helm/paca/values.yaml deploy/helm/paca/tests/render-oidc.sh
git commit -m "docs(helm): document OIDC bootstrap values"
```
