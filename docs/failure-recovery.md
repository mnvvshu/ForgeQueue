# Worker Failure Recovery & Re-claiming

One of ForgeQueue's flagship capabilities is resilient distributed failure recovery. If a worker node crashes mid-execution, jobs are never orphaned in `RUNNING` status.

## Recovery Sequence Diagram

```mermaid
sequenceDiagram
    autonumber
    actor Client
    participant API as ForgeQueue API
    participant PG as PostgreSQL
    participant Stream as Redis Streams
    participant W1 as Worker 1 (Crashes)
    participant Janitor as Recovery Janitor
    participant W2 as Worker 2 (Survivor)

    Client->>API: Submit Job
    API->>PG: Insert Job (QUEUED) + Outbox Event
    API->>Stream: Publish to Stream
    Stream->>W1: XREADGROUP delivers message
    W1->>PG: ClaimJob (status = RUNNING, attempt = 1)
    Note over W1: Worker 1 crashes abruptly (OOM / Node reboot)
    
    loop Every 5 seconds
        Janitor->>PG: Check worker heartbeats
        Janitor->>PG: Mark Worker 1 as OFFLINE
        Janitor->>PG: Query RUNNING jobs with stale worker / exceeded lease
        Janitor->>PG: Requeue Job (status = QUEUED, attempt = 2) + Outbox Event
    end

    API->>Stream: Outbox dispatcher republishes job to Stream
    Stream->>W2: XREADGROUP / XAUTOCLAIM delivers to Worker 2
    W2->>PG: ClaimJob (status = RUNNING, attempt = 2)
    W2->>W2: Execute inside Docker container
    W2->>PG: UpdateResult (COMPLETED, exit 0)
    W2->>Stream: XACK message
    Client->>API: Fetch status -> COMPLETED (Attempts 1 & 2 recorded)
```

## Selective Retry Strategy

ForgeQueue does **not** blindly retry all failures:

| Failure Type | Example | Eligible for Retry? | Outcome |
| :--- | :--- | :--- | :--- |
| **Worker Crash** | Worker host panics / network drops | **Yes** (up to 3 attempts) | Requeued to `QUEUED`, new attempt row |
| **Infrastructure Issue** | Docker daemon restart / disk full | **Yes** (up to 3 attempts) | Requeued to `QUEUED`, new attempt row |
| **User Syntax Error** | Python `SyntaxError: invalid syntax` | **No** | Terminal `FAILED`, exit 1 |
| **Compile Error** | C++ missing semicolon, Go build error | **No** | Terminal `FAILED`, exit 1 |
| **Runtime Exception** | Uncaught zero division, null pointer | **No** | Terminal `FAILED`, exit != 0 |
| **Execution Timeout** | Infinite loop (`while True: pass`) | **No** (unless configured) | Terminal `TIMEOUT`, exit 124 |
| **Retry Exhaustion** | 3 consecutive infrastructure failures | **No** | Terminal `FAILED` (`INFRASTRUCTURE_FAILURE`) |
