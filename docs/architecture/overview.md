# Architecture Overview

Paca is planned as a single open-source monorepo with a small set of clearly separated runtime surfaces.

## Runtime Areas

- `apps/web`: the user-facing application built with React and shadcn/ui.
- `services/api`: the main application backend built with Go and Gin.
- `services/ai-agent`: the AI orchestration runtime built with FastAPI and LangGraph.

## Platform Dependencies

- PostgreSQL stores core transactional product data.
- Redis supports cache and short-lived coordination state.
- RabbitMQ carries asynchronous workflows and service messages.

## Architectural Intent

- Keep service boundaries explicit.
- Avoid adding shared layers before reuse is proven.
- Separate product-facing documentation from implementation-facing documentation.
- Keep the repository easy to read in public from the root.

This document is intentionally high level. More detailed decisions should be added only when implementation forces them.