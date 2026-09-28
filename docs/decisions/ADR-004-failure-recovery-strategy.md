# ADR-004: Worker Failure & Execution Recovery Strategy

## Status
Accepted

## Context
In a distributed queue system, workers can crash, lose connectivity, or hang mid-execution. A job claimed by a crashed worker must not remain in `RUNNING` status indefinitely, nor should it be blindly retried if the error was deterministic (e.g. user syntax error or exit code 1).

## Decision
1. **Heartbeats & Leases**:
   - Every active worker updates its heartbeat timestamp in `workers` table (and Redis key `forgequeue:worker:{worker_id}:heartbeat`) every 5 seconds.
   - An active execution attempt has a lease tied to the worker.
2. **Crash & Hang Detection**:
   - A background **Janitor/Recovery Service** runs periodically (every 10s).
   - Queries `jobs` in `RUNNING` status where:
     - Worker heartbeat is older than threshold (e.g. 20s), OR
     - Job execution time exceeds configured timeout + grace period (e.g., timeout + 15s).
3. **Selective Retry Logic**:
   - Infrastructure / Worker crash failure: Re-queueable. Create a new record in `job_attempts` and transition job back to `QUEUED` if `attempt_count < max_attempts` (default: 3). If `attempt_count >= max_attempts`, transition to `FAILED` with failure_category = `INFRASTRUCTURE_FAILURE`.
   - Deterministic User failures: Syntax error, compilation error, exit code != 0 are terminal (`FAILED`). Do NOT retry.
   - Timeout: Terminal (`TIMEOUT`). Do NOT retry unless explicitly configured.
4. **Historical Immutability**:
   - `job_attempts` records are never overwritten. Every attempt records `attempt_number`, `worker_id`, `started_at`, `finished_at`, `status`, `exit_code`, and failure details.
