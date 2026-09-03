# Privacy Policy — Paca Browser Extension

_Last updated: 2026-09-03_

Paca is a browser extension that lets you comment directly on elements of
your own self-hosted Paca instance's environment preview pages, and turn
those comments into Paca tasks or agent conversations. This page describes
what data the extension handles and how.

## What the extension does, in one sentence

The extension stays completely inactive on every website except your own
Paca instance's forwarded environment preview pages, where it lets you pin
comments to page elements and sends them directly to your own Paca
instance's API.

## Data the extension collects

- **Website content.** When you create a comment, the extension captures a
  screenshot of the element you clicked, a snapshot of that element (its
  tag name, a short excerpt of its text, its accessible name, and a
  sanitized excerpt of its outer HTML), and the comment text you type.
- **Network activity on the active tab, read-only.** The extension watches
  for failed requests (HTTP 4xx/5xx responses or connection errors) on the
  page you're currently viewing, so that a submitted comment can include
  the failed requests observed since the page loaded. It never blocks,
  modifies, or redirects any request — it only observes. This data is kept
  in memory per tab and discarded when the tab closes; it's only sent
  anywhere if you submit a comment while it's present.

## Data the extension does **not** collect

The extension does not collect or transmit health information, financial
or payment information, authentication credentials (passwords, tokens, or
PINs), personal communications (email, SMS, or chat messages), your
location, or your browsing history. It never reads, stores, or handles
your Paca login token — authentication relies entirely on your browser's
existing, normal session cookies for your own Paca instance.

## Where your data goes

Everything the extension captures is sent only to your own Paca instance's
API, over a request authenticated with your existing Paca login session —
never to any third party, and never to any server operated by the authors
of this extension. Paca is self-hosted: your comments, screenshots, and
captured page context are stored in your own Paca deployment's database
and object storage, under your organization's own control and existing
data-retention practices.

We do not sell or transfer your data to third parties, use it for purposes
unrelated to the extension's stated purpose above, or use it to determine
creditworthiness or for lending purposes.

## Source code

Paca is open source. The extension's full source code, including
everything described above, is available at
[github.com/Paca-AI/paca/tree/master/apps/extension](https://github.com/Paca-AI/paca/tree/master/apps/extension).

## Contact

For questions about this policy, open an issue at
[github.com/Paca-AI/paca/issues](https://github.com/Paca-AI/paca/issues).
