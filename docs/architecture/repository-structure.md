# Repository Structure

Paca starts with a documentation-first monorepo layout.

```text
paca/
├── README.md
├── CONTRIBUTING.md
├── CODE_OF_CONDUCT.md
├── SECURITY.md
├── ROADMAP.md
├── .github/
├── docs/
├── apps/
│   ├── web/
│   └── e2e/
├── services/
│   ├── api/
│   ├── realtime/
│   └── ai-agent/
└── deploy/
    └── nginx/
```

## Why This Shape

- `docs` keeps durable technical writing out of the root.
- `apps` holds user-facing surfaces and their test counterparts.
- `apps/e2e` lives under `apps` because it directly exercises `apps/web` and is versioned alongside it; it is not deployed.
- `services` holds backend runtimes with different language stacks.
- `services/realtime` is split out so Socket.IO delivery can scale and evolve independently from the transactional API.
- `deploy` keeps environment and infrastructure assets in one place.
- `deploy/nginx` holds gateway configuration that is mounted read-only into the nginx container at runtime, making it easy to review and modify without rebuilding images.

## What Is Intentionally Missing

- `packages` is deferred until shared code actually appears.
- `scripts` is deferred until recurring automation exists.
- `examples` is deferred because this repository is an application, not a library.

The goal is to keep the public repository easy to scan while leaving room to grow.