# Contributing to Paca

Thanks for contributing to Paca.

This repository is being documented before implementation, so the current priority is clarity of structure, product direction, and contributor expectations.

## Current Focus

- Define a repository structure that is simple to navigate in public.
- Document architecture and boundaries before writing production code.
- Keep decisions reversible until implementation starts.

## Repository Shape

- `apps/web`: the React + shadcn/ui frontend.
- `services/api`: the Go + Gin application backend.
- `services/ai-agent`: the FastAPI + LangGraph AI runtime.
- `docs`: architecture, guides, product, API, and deployment notes.
- `deploy`: local and future deployment assets.

## How to Contribute Right Now

- Improve documentation clarity.
- Propose architecture decisions with clear tradeoffs.
- Open issues for missing project structure, contributor workflow, or product concepts.
- Avoid introducing implementation-heavy detail unless it is necessary for a foundational decision.

## Contribution Guidelines

- Keep pull requests focused.
- Prefer documentation-first changes while the repository is still in planning.
- Explain the reasoning behind structure changes.
- Avoid premature abstraction in repo layout or service boundaries.

## Discussion Areas

- Product workflow and user experience.
- Service responsibilities and system boundaries.
- Local development and deployment approach.
- Open-source governance and contributor experience.

## Pull Request Checklist

- The change is scoped to one concern.
- Documentation stays consistent with the planned repository shape.
- New decisions are explained clearly.
- Related docs are updated when needed.

As the codebase becomes runnable, this document should expand to include setup, testing, and review workflow.