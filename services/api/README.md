# API Service

This directory is reserved for the Go + Gin backend.

## Planned Responsibilities

- expose core application APIs;
- coordinate product workflows;
- persist product state in PostgreSQL;
- use Redis where appropriate;
- publish real-time relevant domain events to RabbitMQ for `services/realtime`;
- remain the system of record for all state-changing business operations.

The expected internal Go layout should be documented when scaffolding begins.