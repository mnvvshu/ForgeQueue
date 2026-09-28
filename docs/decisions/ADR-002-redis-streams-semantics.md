# ADR-002: Redis Streams Semantics & Consumer Group Architecture

## Status
Accepted

## Context
ForgeQueue needs reliable distributed delivery of jobs to horizontally scaled workers. Redis Streams provides native stream persistence, consumer groups, pending entry tracking (PEL), acknowledgment (`XACK`), and re-claiming (`XAUTOCLAIM`).

## Decision
1. **Stream Layout**:
   - Work stream: `forgequeue:jobs:stream`
   - Dead letter stream: `forgequeue:jobs:dlq`
   - Real-time logs/events channel: Redis Pub/Sub `forgequeue:job:{job_id}:logs`
2. **Consumer Group**:
   - Single shared group: `forgequeue-workers`
   - Consumer Name: Unique worker ID (`worker-{hostname}-{uuid}`)
3. **Delivery Semantics**:
   - Strictly **at-least-once**. Exactly-once is impossible in distributed systems with crashes; we achieve effectively-once processing by leveraging PostgreSQL atomic conditional updates (e.g. `UPDATE jobs SET status = 'RUNNING' WHERE id = $1 AND status = 'QUEUED'`).
   - If a duplicate message arrives for a job already running or completed, the worker acknowledges it and discards without re-executing.
4. **Consumer Lifecycle & Pel Cleanup**:
   - Workers read new messages with `XREADGROUP GROUP forgequeue-workers {worker_id} BLOCK 2000 STREAMS forgequeue:jobs:stream >`.
   - Upon completing or failing execution and persisting state in PostgreSQL, worker issues `XACK forgequeue:jobs:stream forgequeue-workers {message_id}` and `XDEL`.
   - Workers run periodic `XAUTOCLAIM` with a configurable min-idle-time (e.g., 30s) to claim orphaned pending messages from dead or hung consumers.
