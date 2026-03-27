# Architecture Overview

Paca is planned as a single open-source monorepo with a small set of clearly separated runtime surfaces.

## Runtime Areas

- `apps/web`: the user-facing application built with React and shadcn/ui.
- `apps/e2e`: the end-to-end test suite built with Playwright; not a deployed runtime, but an external verifier of the full stack.
- `services/api`: the main application backend built with Go and Gin.
- `services/realtime`: the real-time delivery service built with Socket.IO.
- `services/ai-agent`: the AI orchestration runtime built with FastAPI and LangGraph.

## Platform Dependencies

- PostgreSQL stores core transactional product data. See [database-schema.md](database-schema.md) for the full schema.
- Redis supports cache and short-lived coordination state.
- RabbitMQ carries asynchronous workflows and service messages between backend runtimes.

## Interaction Model

- `apps/web` uses HTTP APIs exposed by `services/api` for request-response workflows.
- `apps/web` connects to `services/realtime` over Socket.IO for live updates.
- `apps/e2e` drives a real browser against the full running stack and validates cross-cutting flows that span multiple runtime surfaces.
- `services/api` remains the system of record for product state and publishes real-time relevant domain events to RabbitMQ.
- `services/realtime` consumes RabbitMQ messages from `services/api` and fans out client-safe events to connected Socket.IO rooms and users.
- `services/ai-agent` integrates with the core backend and should publish or consume asynchronous events through explicit contracts rather than direct coupling.

## Architectural Intent

- Keep service boundaries explicit.
- Keep state-changing business logic in `services/api`, not in the real-time edge service.
- Use RabbitMQ to decouple event production from Socket.IO delivery.
- Avoid adding shared layers before reuse is proven.
- Separate product-facing documentation from implementation-facing documentation.
- Keep the repository easy to read in public from the root.

This document is intentionally high level. More detailed decisions should be added only when implementation forces them.