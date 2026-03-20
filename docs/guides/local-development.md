# Local Development

This document describes the intended local development shape for Paca.

## Planned Runtime Stack

- `apps/web`: React + shadcn/ui
- `services/api`: Go + Gin
- `services/ai-agent`: FastAPI + LangGraph
- PostgreSQL
- Redis
- RabbitMQ

## Intent

Local development should eventually support bringing up the full stack in a predictable way.

The early preference is:

- application code lives in `apps` and `services`;
- runtime support assets live in `deploy`;
- environment-specific detail stays out of the root README.

## Not Documented Yet

- exact commands;
- port assignments;
- migration workflow;
- container strategy;
- service startup order.

These should be documented once the first working scaffold exists.