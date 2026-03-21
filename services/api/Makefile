BINARY   := api
CMD_PATH := ./cmd/api
MIGRATE  := ./migrations
DB_DSN   ?= $(DATABASE_URL)

.PHONY: run build test lint migrate-up migrate-down tidy

run:
	go run $(CMD_PATH)/main.go

build:
	go build -o bin/$(BINARY) $(CMD_PATH)/main.go

test:
	go test ./... -race -timeout 60s

lint:
	golangci-lint run ./...

tidy:
	go mod tidy

migrate-up:
	@echo "Running migrations against $$DATABASE_URL"
	psql "$(DB_DSN)" -f $(MIGRATE)/000001_init.sql

migrate-down:
	@echo "Dropping schema (destructive!) — set DB_DSN before running"
	psql "$(DB_DSN)" -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"
