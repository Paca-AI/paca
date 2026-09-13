# Privacy Policy — Paca Browser Extension

_Last updated: 2026-09-03_

Paca is a browser extension that lets you comment directly on elements of
your own self-hosted Paca instance's environment preview pages, and turn
those comments into Paca tasks or agent conversations. This page describes
what data the extension handles and how.

## What the extension does, in one sentence

The extension's scripts load on every page you visit — a technical
requirement of how browser extensions work, not a choice to run everywhere
— but on any page that isn't your own Paca instance's forwarded environment
preview, all they do is check for a Paca-specific cookie and stop; nothing
is captured or sent anywhere unless that check finds one. Only on a
forwarded preview page does the extension let you pin comments to page
elements and send them directly to your own Paca instance's API.

## Data the extension collects

- **Website content.** When you create a comment, the extension captures a
  screenshot of the element you clicked, a snapshot of that element (its
  tag name, a short excerpt of its text, its accessible name, and a
  sanitized excerpt of its outer HTML), and the comment text you type.
- **Failed network requests, read-only, across your open tabs.** To attach
  useful debugging context to a comment, the extension needs a short
  history of failed requests (HTTP 4xx/5xx responses or connection errors)
  from *before* you started writing it — so this one part of the extension
  necessarily runs in the background across all your open tabs, not just
  Paca preview pages, buffering up to 50 recent failed requests per tab. It
  never blocks, modifies, or redirects any request — it only observes.
  This buffer is discarded per tab as soon as the extension determines
  that tab isn't a Paca preview, discarded again on every new page
  navigation, and discarded entirely when the tab closes; only the tab
  you're actually commenting in ever has its buffer sent anywhere, and
  only when you submit that comment.

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
