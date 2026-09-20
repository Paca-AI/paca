#!/usr/bin/env bash
# Runs every browser project one after another. Inside each browser the spec
# files run in parallel workers (see `workers` in playwright.config.ts), but two
# browsers must never run together: the same spec would share its test data.
#
#   bun run test                       # all browsers
#   bun run test tests/docs            # extra args go to `playwright test`
#   E2E_BROWSERS="chromium firefox" bun run test
#   E2E_WORKERS=2 bun run test
set -uo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

status=0
for project in ${E2E_BROWSERS:-chromium firefox webkit mobile-chrome mobile-safari}; do
  echo "==> $project"
  bunx playwright test --project="$project" "$@" || status=1
done
exit $status
