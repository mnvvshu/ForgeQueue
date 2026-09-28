# ADR-005: Log Persistence and Real-Time Streaming

## Status
Accepted

## Context
Clients require real-time output streaming as code executes, as well as durable historical access when refreshing or reviewing past jobs.

## Decision
1. **Real-Time Stream**:
   - As workers stream stdout/stderr chunks from execution containers, they publish log events to Redis Pub/Sub channel `forgequeue:job:{job_id}:events`.
   - The API server exposes an HTTP Server-Sent Events (SSE) endpoint:
     `GET /api/v1/jobs/{id}/events`
   - The SSE handler subscribes to the Redis Pub/Sub channel for live jobs and forwards events (`status`, `stdout`, `stderr`, `terminal`).
2. **Durable Persistence**:
   - While streaming chunks, the worker buffers the complete aggregated stdout and stderr (up to 1MB limit).
   - Upon completion, the worker writes the final output, exit code, duration, and terminal status directly into PostgreSQL `job_attempts` and `jobs`.
3. **Replay & Refresh Guarantee**:
   - When a browser connects or refreshes, the SSE handler first queries PostgreSQL for already-recorded output and current state. If the job is already terminal, it immediately sends the complete output and closes the stream. If still running, it replays captured output and then attaches to the live Redis stream.
   - This ensures refreshing the browser never permanently loses output.
