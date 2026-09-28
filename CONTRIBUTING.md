# Contributing to ForgeQueue

Thank you for your interest in contributing to ForgeQueue!

## Development Setup

1. Prerequisites:
   - Go 1.22+
   - Node.js 20+
   - Docker & Docker Compose

2. Clone and configure:
   ```bash
   git clone https://github.com/forgequeue/forgequeue.git
   cd forgequeue
   cp .env.example .env
   ```

3. Run test suite:
   ```bash
   go test -v ./...
   ```

4. Code Style & Commit Guidelines:
   - Ensure all Go code is formatted with `gofmt`.
   - Run `go vet ./...` before opening pull requests.
   - For architecture changes, add a lightweight ADR to `docs/decisions/`.
