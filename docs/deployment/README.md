# Deployment Documentation

Paca now ships two separate Docker Compose entry points under [`deploy/`](../../deploy/README.md):

- `docker-compose.dev.yml` for local development;
- `docker-compose.prod.yml` for production-oriented single-host deployment.

## Why They Are Separate

Development and production have different goals:

- development optimizes for fast onboarding, inspectability, and local feedback;
- production optimizes for explicit configuration, image-based rollout, and operator control.

Keeping them separate is the cleaner open-source default. It avoids hard-coding local assumptions into a production path and makes the repository easier for contributors to reason about.

## Development Compose

The development compose file provisions:

- PostgreSQL;
- Valkey;
- RabbitMQ with the management UI;
- optional `api` and `web` service containers that you can run alongside the infra services as needed.

This supports two workflows:

- run only infra in Docker and start application services on the host;
- run the whole stack in Docker for quick end-to-end testing.

## Production Compose

The production compose file is intentionally self-hostable:

- it defines the web and API containers;
- it includes PostgreSQL, Valkey, and RabbitMQ for a complete single-host stack;
- it keeps configuration explicit through environment variables and named volumes;
- it publishes the web and API services by default.

That makes it a better open-source baseline: users can run the full platform immediately, while operators with managed infrastructure can still swap the bundled services for externally hosted equivalents by changing the connection settings.