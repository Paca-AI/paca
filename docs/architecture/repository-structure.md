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
│   └── web/
├── services/
│   ├── api/
│   └── ai-agent/
└── deploy/
```

## Why This Shape

- `docs` keeps durable technical writing out of the root.
- `apps` holds user-facing runtime surfaces.
- `services` holds backend runtimes with different language stacks.
- `deploy` keeps environment and infrastructure assets in one place.

## What Is Intentionally Missing

- `packages` is deferred until shared code actually appears.
- `scripts` is deferred until recurring automation exists.
- `examples` is deferred because this repository is an application, not a library.

The goal is to keep the public repository easy to scan while leaving room to grow.