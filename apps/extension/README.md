# Paca Browser Extension

Comment directly on an element of a running [Paca environment](../../docs/ai-agent/environment-management.md)
preview, right on the page itself, and turn a comment into a real Paca task
in one click.

## How it works

- A Paca environment's forwarded port serves your dev server directly, with
  no Paca proxy in the request path (see the port-forwarding docs linked
  above). This extension detects when you're on one of those forwarded
  pages and shows a small toolbar at the bottom of the screen.
- Detection and authentication both rely on one fact: cookies are scoped by
  hostname, not by port. As long as your Paca instance's `PORT_FORWARD_HOST`
  points at the *same hostname* your Paca app itself is served from (just a
  different port for each forwarded environment), the browser already
  attaches your Paca login session to a page on that same host automatically
  — this extension never reads, stores, or handles any login token itself.
- A lightweight, non-sensitive cookie (`paca_port`) that `services/api` sets
  alongside `access_token`/`refresh_token` at login and on every session
  refresh (see `auth_handler.go`'s `portCookieName`) — but, unlike those
  two, deliberately *not* `HttpOnly`, since the extension reads it directly
  via `document.cookie`. Its value is the port the Paca app is actually
  reachable on (it can be anything, not just 443/80 — a local dev server on
  `:3000`, say), which the same-hostname trick above makes visible on a
  forwarded preview page too. The content script always takes the port
  from this cookie rather than trusting an earlier snapshot, so it keeps
  working even if Paca later moves to a different port. Its presence is
  also the *entire* activation signal — there is no separate per-site
  enable step: the extension holds broad host permissions from install
  (see Setup) and simply stays dormant, everywhere, until this cookie
  shows up.
- Everything else — listing comments, creating one, resolving, turning one
  into a task — is a normal authenticated call the content script makes
  directly to your Paca instance's API.

## Setup

1. `npm install && npm run build` (outputs an unpacked extension to `dist/`).
2. In Chrome, open `chrome://extensions`, enable Developer Mode, and "Load
   unpacked" pointing at `dist/`. Chrome will show an install-time prompt
   for reading/changing data on all sites — that's `<all_urls>` in
   `manifest.json`, needed because a self-hosted Paca instance's hostname
   isn't known in advance; the content script itself stays inert
   everywhere except where the `paca_port` cookie above actually shows up.
3. Log into your Paca instance in a normal tab. That's it — no extension
   UI to click through. The next time you open a forwarded environment
   preview on the same hostname, the toolbar appears on its own.
4. Make sure your deployment's `PORT_FORWARD_HOST` (Helm) / `PORT_FORWARD_HOST`
   (Compose) points at that *same hostname* the Paca app itself is served
   from (just a different port per environment). If it points somewhere
   else entirely (a different domain/IP), the extension will stay dormant on
   a forwarded preview — it has no way to authenticate there.

If a forwarded preview doesn't show the toolbar and you expected it to,
open DevTools' console on that page and filter for `[Paca]` — the content
script logs exactly which check it failed (no cookie, or the
`port-forwards/resolve` API call itself, with the underlying error) instead
of failing silently.

## Development

Each of the four build targets (the two content scripts, the background
service worker, and the options page) has its own `vite.config.*.ts` — see
those files' own doc comments for why: MV3 content scripts always run as
classic scripts, never ES modules, so each one is built as a
self-contained IIFE rather than sharing one bundler config with the
ES-module background worker and the plain (script-free) HTML options page.

```bash
npm run build   # tsc -b, then all 4 Vite builds, then copy manifest.json
npm run lint
```
