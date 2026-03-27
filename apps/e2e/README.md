# End-to-End Tests

Playwright-based end-to-end tests for the paca web application.

## Prerequisites

- Node.js ≥ 20
- The full application stack must be running before you execute tests
  (see [Local Development](../../docs/guides/local-development.md))

## Setup

```bash
# Install dependencies
npm install

# Install Playwright browsers (first time only)
npx playwright install --with-deps

# Copy and configure environment variables
cp .env.example .env
# Edit .env with your local values if they differ from the defaults
```

## Running Tests

```bash
# Run the full suite (headless, all configured browsers)
npm test

# Run with the Playwright UI for interactive debugging
npm run test:ui

# Run headed (visible browser window)
npm run test:headed

# Run in debug mode (step through tests)
npm run test:debug

# Open the last HTML report
npm run test:report
```

### Running a subset

```bash
# Single file
npx playwright test tests/auth/login.spec.ts

# Single test by title
npx playwright test -g "redirects to dashboard on valid credentials"

# One browser only
npx playwright test --project=chromium

# Mobile only
npx playwright test --project=mobile-chrome
```

## Project Structure

```
apps/e2e/
├── .env.example            # document required environment variables
├── global-setup.ts         # runs once before all tests — saves auth state
├── playwright.config.ts    # Playwright configuration
├── fixtures/
│   └── index.ts            # extended `test` fixture with LoginPage injected
├── pages/
│   └── login.page.ts       # Page Object Model for the login page
└── tests/
    ├── auth/               # core login flows (valid, invalid, empty fields)
    ├── validation/         # client-side form validation
    ├── security/           # injection / XSS payloads rejected at login
    ├── session/            # logout, back-button, session persistence
    └── ux/                 # UX: error display, password toggle, theme, mobile
```

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

Copy `.env.example` to `.env` and adjust these values for your environment.
**Never commit `.env`.**

## CI

On CI (`CI=true`), the test runner uses:

- 1 worker (no parallelism)
- 2 retries on failure
- Chromium, Firefox, WebKit, Pixel 5 (Chrome), iPhone 12 (Safari)

Traces and screenshots are captured on failure and included in the HTML report.
