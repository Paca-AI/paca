# End-to-End Tests

Playwright-based end-to-end tests for the paca web application.

## Prerequisites

- Bun ≥ 1.0
- Docker (for the dedicated E2E stack below), or any other running paca stack
  reachable at `E2E_BASE_URL`

## E2E stack

`deploy/docker-compose.e2e.yml` defines an isolated stack (project name
`paca-e2e`, admin `admin` / `e2e-admin-password`, gateway on `http://localhost`).
Two scripts wrap it:

```bash
# Build the agent-server sandbox image, then build and start the whole stack
bun run stack:up

# Tear it down and delete its volumes (fresh database next time)
bun run stack:down
```

`stack:up` first builds `paca-agent-server-goose:e2e`, which the compose file
references but does not build itself, then runs `docker compose up -d --build --wait`.
The stack has no LLM credentials, so anything that needs a live agent reply is
marked `test.fixme` in the specs.

## Setup

```bash
# Install dependencies
bun install

# Install Playwright browsers (first time only)
bunx playwright install --with-deps

# Copy and configure environment variables
cp .env.example .env
# Edit .env with your local values if they differ from the defaults
```

## Running Tests

```bash
# Run the full suite (headless, every browser one after another, 3 parallel workers each)
bun run test

# Chromium only
bun run test:chromium

# Choose browsers / worker count
E2E_BROWSERS="chromium firefox" E2E_WORKERS=2 bun run test

# Run with the Playwright UI for interactive debugging
bun run test:ui

# Run headed (visible browser window)
bun run test:headed

# Run in debug mode (step through tests)
bun run test:debug

# Open the last HTML report
bun run test:report
```

### Parallel workers

Spec files are distributed across `E2E_WORKERS` workers (default 3 locally, 2 on
CI); the tests inside one file run in order on one worker. Every spec is
self-contained so any file can run on any worker:

- it creates and cleans up its own data under a prefix no other spec shares
  (`E2E_DOCS_`, `E2E_ROLES_`, ...), and signs in with its own session;
- instance-wide state is never assumed to be quiet: branding, the global agent
  list and user totals are stubbed with `page.route` or asserted against what the
  page itself reports.

When adding a spec, pick a new unique prefix and only delete data that starts
with it. Browsers must not run at the same time (the same spec in two browsers
would share its prefix), which is why `bun run test` loops over them; a bare
`bunx playwright test` with several `--project` flags is not safe.

### Running a subset

```bash
# Single file
bunx playwright test tests/auth/login.spec.ts --project=chromium

# Single test by title
bunx playwright test -g "redirects to home on valid credentials"

# One browser only
bunx playwright test --project=chromium

# Mobile only
bunx playwright test --project=mobile-chrome
```

## Project Structure

```
apps/e2e/
├── .env.example            # document required environment variables
├── features/               # Gherkin feature files, one per spec (see below)
├── global-setup.ts         # runs once before all tests — saves auth state
├── playwright.config.ts    # Playwright configuration
├── scripts/
│   ├── stack-up.sh         # build + boot the dedicated E2E docker stack
│   └── stack-down.sh       # tear the stack down (including volumes)
├── fixtures/
│   └── index.ts            # extended `test` fixture with LoginPage injected
├── pages/
│   └── login.page.ts       # Page Object Model for the login page
└── tests/
    ├── helpers/            # shared API seeding/cleanup and limited-user helpers
    ├── admin/              # users, global roles, agents, settings, plugins, changelog
    ├── auth/               # login flows, forced password change
    ├── docs/               # project documentation (folders, editor, history, comments)
    ├── profile/            # profile editing, personal API keys
    ├── projects/           # project management, tasks, sprints, views, roles,
    │                       # agents, automation, conversations, environments
    ├── security/           # injection / XSS payloads rejected at login
    ├── session/            # logout, back-button, session persistence
    ├── ui/                 # sidebar navigation, collapse, theme, user menu
    ├── ux/                 # UX: error display, password toggle, theme, mobile
    └── validation/         # client-side form validation
```

## BDD Feature Files

Every spec starts with a `// spec: features/<area>/<name>.feature` comment
pointing at the Gherkin file it implements. The feature files are the
specification; the runner is plain Playwright, and no Cucumber or
Playwright-BDD adapter is wired in.

| Area | Feature files |
| ---- | ------------- |
| `admin/` | `agents`, `changelog`, `global-roles`, `plugins`, `settings`, `users` |
| `auth/` | `login`, `change-password` |
| `docs/` | `docs` |
| `profile/` | `profile`, `api-keys` |
| `projects/` | `agents`, `automation`, `conversations`, `custom-fields`, `environments`, `interaction-sidebar`, `interaction-views`, `management`, `roles`, `sprint-lifecycle`, `task-detail`, `task-statuses`, `task-types`, `timeline`, `view-settings`, `view-settings-fields` |
| `security/` | `login` |
| `session/` | `management` |
| `ui/` | `sidebar` |
| `ux/` | `login` |
| `validation/` | `form` |

Scenarios that hit a known app limitation or need infrastructure the E2E stack
lacks (a live LLM, a real sandbox terminal) are kept in the feature file and
marked `test.fixme` in the spec with the reason. Live terminal, SSH and
port-forward flows for environments are intentionally out of scope.

Specs that need a user with limited permissions create one through the admin
API (`tests/helpers/e2e-api.ts`) and sign in with an empty `storageState`.

## Authentication State

`global-setup.ts` logs in once with the configured credentials and writes the
browser auth state to `playwright/.auth/user.json` (git-ignored).

Session tests that need to start in an authenticated state use:

```ts
test.use({ storageState: AUTH_FILE });
```

Other test suites start with no stored auth so every login interaction is
fully isolated.

## Environment Variables

| Variable       | Default                | Description                    |
| -------------- | ---------------------- | ------------------------------ |
| `E2E_BASE_URL` | `http://localhost`     | Base URL of the running app    |
| `E2E_USERNAME` | `admin`                | Test user username             |
| `E2E_PASSWORD` | `e2e-admin-password`   | Test user password             |
| `E2E_WORKERS`  | `3` (`2` on CI)        | Parallel workers per browser   |
| `E2E_BROWSERS` | all five projects      | Browsers run by `bun run test` |

Copy `.env.example` to `.env` and adjust these values for your environment.
**Never commit `.env`.**

## CI

On CI (`CI=true`), the test runner uses:

- 2 workers
- 2 retries on failure
- Chromium, Firefox, WebKit, Pixel 5 (Chrome), iPhone 12 (Safari)

Traces and screenshots are captured on failure and included in the HTML report.
