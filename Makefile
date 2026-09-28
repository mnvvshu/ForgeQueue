.PHONY: all build dev test test-unit test-integration test-load lint migrate-up migrate-down clean

# Default target
all: build test

# Build API, Worker, and Next.js frontend
build:
	go build -o bin/forgequeue-api ./cmd/api
	go build -o bin/forgequeue-worker ./cmd/worker
	cd web && npm run build

# Start local Docker Compose stack
dev:
	docker compose up --build

# Run all test suites
test: test-unit test-integration test-load

# Run unit tests
test-unit:
	go test -v ./internal/statemachine/... ./internal/auth/... ./internal/runtime/... ./internal/executor/...

# Run integration tests (API, Repository, Queue, Worker, Recovery)
test-integration:
	go test -v ./internal/repository/... ./internal/outbox/... ./internal/queue/... ./internal/worker/... ./internal/recovery/... ./internal/api/... ./tests/failure_demo/...

# Run load test (100 concurrent jobs against 3 workers)
test-load:
	go test -v ./tests/load/...

# Run static analysis and linting
lint:
	go vet ./...
	cd web && npm run lint

# Clean build artifacts
clean:
	rm -rf bin/
	rm -rf web/.next/
	rm -rf web/out/
