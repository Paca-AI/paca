# API Documentation

This section will describe the external contracts of Paca.

## Contents

- [http-design.md](http-design.md): REST API design, path conventions, implemented endpoints, and planned resource endpoints.

## Planned Coverage

- HTTP APIs exposed by `services/api`.
- Socket.IO connection and event contracts exposed by `services/realtime`.
- AI-related endpoints exposed by `services/ai-agent`.
- Event boundaries relevant to asynchronous workflows, including RabbitMQ messages from `services/api` to `services/realtime`.
- Cross-service contract conventions once they are stable.

The HTTP API now has an initial concrete design in [http-design.md](http-design.md). Real-time and AI-agent contracts should follow once those services expose stable surfaces.