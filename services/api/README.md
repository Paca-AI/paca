# API Service

Go + Chi backend service for product APIs and core business operations.

## Responsibilities

- Expose HTTP APIs for web and future clients.
- Handle JWT authentication and authorization.
- Coordinate application workflows through domain services.
- Persist system-of-record state in PostgreSQL via sqlx and raw SQL.
- Use Redis where appropriate (cache, rate limit, short-lived state).
- Publish real-time domain events via Valkey Pub/Sub; append durable events to Valkey Streams for `services/realtime`.

## Architecture Principles

- Follow service-repository pattern.
- Service layer owns business logic and orchestration.
- Repository layer owns persistence concerns and raw SQL queries via sqlx.
- Keep handlers thin: transport mapping, validation, and response shaping only.
- Use clear dependency direction: `handler -> service -> repository`.
- Keep domain entities and business rules independent from Chi and sqlx.
- Prefer interfaces at service and repository boundaries for testability.

## Source Layout

```text
services/api/
    README.md
    go.mod
    go.sum
    cmd/
        api/
            main.go
    internal/
        apierr/
            errors.go              # typed API error codes and HTTP status mapping
        bootstrap/
            app.go                 # wiring: config, logger, db, router, server
            providers.go           # dependency providers and constructors
        config/
            config.go              # typed config model
            load.go                # env/file loading and validation
        platform/
            logger/
                logger.go          # structured logger setup
            database/
                postgres.go        # sqlx.Open + connection pool setup
                migrations.go      # migration runner (embed.FS)
            cache/
                redis.go           # redis client setup
            messaging/
                publisher.go       # Valkey Streams publisher for domain events
            token/
                jwt_manager.go     # sign/verify JWT, key rotation strategy
            authz/
                policy.go          # authorization policy abstraction
            storage/               # S3-compatible file storage (RustFS / AWS S3)
            secret/                # encryption helpers (AES-GCM)
            plugin/                # WASM plugin runtime (wazero)
        domain/
            user/
            auth/
            globalrole/
            project/
            task/
            sprint/
            doc/
            agent/
            attachment/
            notification/
            plugin/
            apikey/
                # each domain package: entity.go, repository.go, service.go, errors.go
        repository/
            postgres/              # sqlx implementations of domain repository interfaces
            redis/                 # redis-backed repository implementations
        service/
            auth/
            user/
            globalrole/
            project/
            task/
            sprint/
            doc/
            agent/
            attachment/
            notification/
            plugin/
            apikey/
        transport/
            http/
                httpx/
                    httpx.go       # shared WriteJSON / DecodeJSON helpers
                router/
                    router.go      # Chi router wiring and route registration
                middleware/
                    authn.go       # JWT authentication middleware
                    authz.go       # role/permission authorization middleware
                    validate.go    # request body binding helper
                    must_change_password.go
                handler/
                    health_handler.go
                    auth_handler.go
                    user_handler.go
                    global_role_handler.go
                    project_handler.go
                    task_handler.go
                    sprint_handler.go
                    document_handler.go
                    agent_handler.go
                    attachment_handler.go
                    plugin_handler.go
                    notification_handler.go
                dto/               # request/response structs (no binding tags)
                presenter/
                    response.go    # API response envelopes and error mapping
        events/
            publisher.go           # app-level event publishing abstractions
            topics.go
        worker/                    # background workers (e.g. event consumers)
    migrations/
        000001_init.sql
    test/
        e2e/
            api_flow_test.go
```

## Request Flow (HTTP)

1. Chi router matches the request path and invokes the handler.
2. Middleware validates JWT (authentication) and permissions (authorization).
3. Handler decodes the JSON body with `encoding/json` and validates required fields explicitly.
4. Handler maps the validated DTO to a service input struct.
5. Service executes business rules and transaction boundaries.
6. Repository persists or reads data using sqlx and raw SQL.
7. Service may publish real-time domain events via Valkey Pub/Sub or append durable events to Valkey Streams.
8. Handler returns a standardized response DTO via the presenter layer.

## JWT Authentication and Authorization

- Authentication: access token and refresh token flow.
- JWT manager in `internal/platform/token` owns signing and verification.
- Middleware (`authn.go`) validates token, expiration, and required claims.
- Authorization is policy-based in `internal/platform/authz`.
- Middleware (`authz.go`) enforces role/permission checks per route.
- Services can perform additional domain-level authorization checks.

Recommended claims include:

- `sub` (subject/user id)
- `exp`, `iat`, `nbf`
- `role` and/or `permissions`
- `jti` (token id) for revocation support
- `fid` (family ID) linking all tokens from the same login session
- `mcp` (`must_change_password`) flag — when true, all endpoints except `PATCH /users/me/password` are blocked with `403 AUTH_PASSWORD_CHANGE_REQUIRED`

## PostgreSQL + sqlx Guidelines

- Keep sqlx usage in repository implementations only.
- Do not pass raw `*sqlx.DB` outside the repository layer.
- Write explicit SQL queries; avoid implicit schema inference.
- Use `sqlx.GetContext` / `sqlx.SelectContext` for single-row and multi-row reads.
- Use `sqlx.NamedExecContext` for inserts and updates with named parameters.
- Map `sql.ErrNoRows` to domain `ErrNotFound` at the repository boundary.
- Use explicit transaction handling (`db.BeginTxx`) for multi-step write operations.
- Configure connection pool settings in `platform/database/postgres.go`.
- Keep schema changes in `migrations/` and avoid runtime schema modifications.
- Add indexes and constraints at migration level, not ad hoc in runtime code.

## Request Validation

Because `encoding/json` does not honor `binding:` struct tags, handlers perform explicit field checks after decoding:

```go
if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
    presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, err.Error()))
    return
}
if req.Name == "" {
    presenter.Error(w, r, apierr.New(apierr.CodeNameInvalid, "name is required"))
    return
}
```

Keep validation in the handler, not in DTO struct tags.

## Service-Repository Conventions

- Domain package declares repository interfaces.
- Repository implementations live in `internal/repository/*`.
- Service implementations live in `internal/service/*`.
- Handlers depend on service interfaces, not concrete types.
- Constructor-based dependency injection throughout the app.

## Error and Response Strategy

- Domain errors remain transport-agnostic.
- Presenter layer maps domain/infrastructure errors to HTTP status codes.
- Return consistent JSON envelope for success and error responses.
- Attach request IDs for traceability.

## Testing Strategy

- Unit tests for handlers with lightweight in-process fakes (no network, no DB).
- Unit tests for services with mocked repositories.
- Repository integration tests against PostgreSQL test database.
- E2E smoke tests for login → authorized action → event publish flow.

## Open-Source Readiness Checklist

- Keep package names and folder names simple and consistent.
- Document each layer with short package-level comments.
- Avoid circular dependencies by enforcing one-way layer imports.
- Include `Makefile` targets for `run`, `test`, `lint`, and `migrate`.
- Keep examples of env vars in `.env.example`.
- Add API docs (OpenAPI) when public endpoints stabilize.
