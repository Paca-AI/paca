# Paca Web App

This package contains the Paca web frontend built with TanStack Start, TanStack Router, and ShadCN UI components.

## Run Locally

```bash
bun install
bun --bun run dev
```

## Build

```bash
bun --bun run build
```

## Test

```bash
bun --bun run test
```

## Lint and Format

```bash
bun --bun run lint
bun --bun run format
bun --bun run check
```

## Project Notes

- Routing uses TanStack file-based routes in `src/routes`.
- Shared app shell is defined in `src/routes/__root.tsx`.
- ShadCN primitives are in `src/components/ui`.
- Agent and assignee pickers use `MiddleTruncate` for constrained names: CSS elides the middle as needed and reserves up to eight trailing graphemes, capped at the available label width. Full names remain available to screen readers, search, and native hover tooltips; stored names are unchanged.
- Keeping a page's data fresh: use `services/realtime` (Socket.IO), not `refetchInterval`/`setInterval` polling. See [`src/hooks/use-project-realtime.ts`](src/hooks/use-project-realtime.ts) for the pattern and [`docs/architecture/service-boundaries.md`](../../docs/architecture/service-boundaries.md#convention-real-time-events-over-polling) for the convention and its narrow exceptions (heartbeats, dropped-message safety nets).
