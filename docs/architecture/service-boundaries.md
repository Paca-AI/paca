# Service Boundaries

Paca is planned around one frontend application and three backend services.

## apps/web

Responsible for the user-facing product experience.

Planned concerns:

- authentication and session-driven UI flow;
- board and task management interfaces;
- human and AI collaboration views;
- product-facing components built with React and shadcn/ui.

## services/api

Responsible for the core application backend.

Planned concerns:

- business workflows;
- task, board, and activity APIs;
- persistence coordination with PostgreSQL and Redis;
- publication of domain events to RabbitMQ for downstream consumers, including the real-time service;
- consumption of asynchronous events where API-owned workflows require it.

## services/realtime

Responsible for real-time delivery to connected clients.

Planned concerns:

- maintain Socket.IO namespaces, rooms, and client connection lifecycle;
- authenticate and authorize socket connections using contracts owned by the core backend;
- consume RabbitMQ messages emitted by `services/api`;
- transform internal domain events into client-safe real-time payloads;
- broadcast updates for boards, tasks, comments, and presence-like collaboration signals.

## services/ai-agent

Responsible for AI orchestration and agent execution.

Planned concerns:

- agent workflow execution with LangGraph;
- API endpoints for AI-driven actions;
- coordination with the core backend;
- controlled access to runtime context and tools.

## Boundary Rule

Keep ownership clear. `services/api` owns business rules and durable state transitions, while `services/realtime` only delivers live updates derived from API-owned events. Shared code should stay inside the owning runtime until duplication is real.