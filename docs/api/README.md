# API Documentation

This section will describe the external contracts of Paca.

## Contents

- [http-design.md](http-design.md): REST API design, path conventions, implemented endpoints, and planned resource endpoints.
- [roles-and-policies.md](roles-and-policies.md): IAM roles and policy documents, role assignment to users, agents and project members, validate/simulate/catalogue helpers, and the "my permissions" endpoints. Concepts: [guide](../guides/roles-and-policies.md), [architecture](../architecture/authorization.md).
- [task-activity.md](task-activity.md): task activity feed and comments.

## Planned Coverage

- HTTP APIs exposed by `services/api`.
- Socket.IO connection and event contracts exposed by `services/realtime`.
- AI-related endpoints exposed by `services/agent-runner`.
- Event boundaries relevant to asynchronous workflows, including Valkey Stream messages from `services/api` to `services/realtime`.
- Cross-service contract conventions once they are stable.

The HTTP API now has an initial concrete design in [http-design.md](http-design.md). Real-time and AI-agent contracts should follow once those services expose stable surfaces.