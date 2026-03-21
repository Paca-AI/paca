# Realtime Service

This directory is reserved for the Socket.IO real-time delivery service.

## Planned Responsibilities

- accept authenticated Socket.IO client connections;
- manage rooms, subscriptions, and connection lifecycle;
- consume RabbitMQ messages emitted by `services/api`;
- fan out client-safe real-time events to web clients;
- keep transient connection state out of the transactional API.

The expected Node.js service layout should be documented when scaffolding begins.