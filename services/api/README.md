# API Service

Go + Gin backend service for product APIs and core business operations.

## Responsibilities

- Expose HTTP APIs for web and future clients.
- Handle JWT authentication and authorization.
- Coordinate application workflows through domain services.
- Persist system-of-record state in PostgreSQL via GORM.
- Use Redis where appropriate (cache, rate limit, short-lived state).
- Publish real-time domain events to RabbitMQ for `services/realtime`.

## Architecture Principles

- Follow service-repository pattern.
- Service layer owns business logic and orchestration.
- Repository layer owns persistence concerns and GORM interaction.
- Keep handlers thin: transport mapping, validation, and response shaping only.
- Use clear dependency direction: `handler -> service -> repository`.
- Keep domain entities and business rules independent from Gin and GORM.
- Prefer interfaces at service and repository boundaries for testability.

## Proposed Source Layout

```text
services/api/
	README.md
	go.mod
	go.sum
	cmd/
		api/
			main.go
	internal/
		bootstrap/
			app.go                 # wiring: config, logger, db, router, server
			providers.go           # dependency providers and constructors
		config/
			config.go              # typed config model
			load.go                # env/file loading and validation
		platform/
			logger/
				logger.go            # structured logger setup
			database/
				postgres.go          # gorm.Open + connection pool setup
				migrations.go        # migration runner adapter
			cache/
				redis.go             # redis client setup
			messaging/
				rabbitmq.go          # publisher setup for domain events
			token/
				jwt_manager.go       # sign/verify JWT, key rotation strategy
			authz/
				policy.go            # authorization policy abstraction
		domain/
			user/
				entity.go            # aggregate/entity definitions
				repository.go        # repository interfaces only
				service.go           # service interfaces only
				errors.go            # domain-level errors
			auth/
				claims.go            # JWT claims model
				service.go           # auth service interface/contracts
		repository/
			postgres/
				user_repository.go   # GORM implementation of user repository
				tx.go                # transaction helpers / unit-of-work helper
			redis/
				token_blacklist.go   # optional refresh token/session storage
		service/
			auth/
				auth_service.go      # login, refresh, logout, password flows
			user/
				user_service.go      # user use-cases
		transport/
			http/
				router/
					router.go
					middleware.go      # request id, recovery, CORS, logging
				middleware/
					authn.go           # JWT authentication middleware
					authz.go           # role/permission authorization middleware
					validate.go        # request validation helper
				handler/
					health_handler.go
					auth_handler.go
					user_handler.go
				dto/
					auth_dto.go
					user_dto.go
				presenter/
					response.go        # API response envelopes and error mapping
		events/
			publisher.go           # app-level event publishing abstractions
			topics.go
	migrations/
		000001_init.sql
	test/
		integration/
			auth_test.go
			user_test.go
		e2e/
			api_flow_test.go
```

## Request Flow (HTTP)

1. Gin route receives request in `transport/http/handler`.
2. Middleware validates JWT (authentication) and permissions (authorization).
3. Handler maps DTO -> service input.
4. Service executes business rules and transaction boundaries.
5. Repository persists/reads data using GORM.
6. Service may publish domain events to RabbitMQ.
7. Handler returns standardized response DTO.

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

## GORM + PostgreSQL Guidelines

- Keep GORM usage in repository implementations only.
- Do not pass GORM models outside repository boundaries.
- Use explicit transaction handling for multi-step write operations.
- Configure connection pool settings in `platform/database/postgres.go`.
- Keep schema changes in `migrations/` and avoid automatic destructive schema drift.
- Add indexes and constraints at migration level, not ad hoc in runtime code.

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

- Unit tests for services with mocked repositories.
- Repository integration tests against PostgreSQL test database.
- HTTP integration tests for auth and critical user flows.
- E2E smoke tests for login -> authorized action -> event publish flow.

## Open-Source Readiness Checklist

- Keep package names and folder names simple and consistent.
- Document each layer with short package-level comments.
- Avoid circular dependencies by enforcing one-way layer imports.
- Include `Makefile` targets for `run`, `test`, `lint`, and `migrate`.
- Keep examples of env vars in `.env.example`.
- Add API docs (OpenAPI) when public endpoints stabilize.

## Next Scaffolding Milestones

1. Initialize Gin app skeleton under `cmd/api` and `internal/transport/http`.
2. Add config loader and structured logging.
3. Add PostgreSQL connection with GORM + first migration.
4. Implement auth module (JWT issue/verify + authn/authz middleware).
5. Implement first vertical slice (`user`: handler -> service -> repository).
6. Add integration tests and CI checks.