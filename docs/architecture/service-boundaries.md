# Service Boundaries

Paca is planned around one frontend application and two backend services.

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
- publication and consumption of asynchronous events where needed.

## services/ai-agent

Responsible for AI orchestration and agent execution.

Planned concerns:

- agent workflow execution with LangGraph;
- API endpoints for AI-driven actions;
- coordination with the core backend;
- controlled access to runtime context and tools.

## Boundary Rule

Keep ownership clear. Shared code should stay inside the owning runtime until duplication is real.