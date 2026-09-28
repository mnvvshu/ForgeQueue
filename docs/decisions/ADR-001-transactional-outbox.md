# ADR-001: Transactional Outbox for Reliable Job Publication

## Status
Accepted

## Context
When a user submits code for execution, ForgeQueue creates a job record in PostgreSQL and then must notify workers via Redis Streams.
PostgreSQL and Redis cannot participate in a shared two-phase commit without introducing extreme complexity and brittleness. If the API writes to PostgreSQL and crashes before publishing to Redis, the job is orphaned in the database. Conversely, publishing to Redis before PostgreSQL commit risks workers picking up uncommitted or rolled-back jobs.

## Decision
We implement the **Transactional Outbox Pattern**:
1. Within a single PostgreSQL transaction:
   - Insert job record with status `QUEUED`.
   - Insert an outbox event in the `outbox_events` table (payload containing job ID, language, priority, trace context).
2. The transaction commits atomically.
3. An asynchronous **Dispatcher** process:
   - Polls or listens for unpublished outbox events ordered by ID / timestamp.
   - Publishes the job dispatch message to Redis Streams (`forgequeue:jobs:stream`).
   - Marks the outbox event as published (`published_at = NOW()`).
4. A background **Reconciler** periodically sweeps any outbox events older than a threshold (e.g. 10s) that remain unpublished due to transient Redis outages or dispatcher crashes, ensuring guaranteed at-least-once delivery into Redis Streams.

## Consequences
- Guaranteed at-least-once delivery into Redis Streams.
- Workers must be idempotent because outbox retries can publish duplicate messages.
- Small delivery latency overhead (milliseconds with push-trigger or rapid poll), but rock-solid durability.
