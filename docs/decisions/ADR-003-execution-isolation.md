# ADR-003: Execution Isolation & Security Model

## Status
Accepted

## Context
ForgeQueue accepts untrusted arbitrary user code across multiple languages (Python, JavaScript/Node.js, Go, C++, Java). The execution model must assume hostile intent: infinite loops, fork bombs, disk fill, secret exfiltration, network scanning, and privilege escalation.

## Decision
1. **Container Isolation (Docker)**:
   - Untrusted code executes inside ephemeral containers built from minimal, pinned base images.
   - Flags applied:
     - `--network none` (network isolation disabled by default)
     - `--read-only` root filesystem (writable workspace mounted via tmpfs or isolated bounded temp directory)
     - `--security-opt no-new-privileges`
     - `--cap-drop ALL`
     - Non-root execution (`--user 1000:1000`)
     - Bounded resources: `--cpus=1.0`, `--memory=256m`, `--memory-swap=256m`, `--pids-limit=64`
   - Workspace isolation: Dedicated temporary directory per attempt, deleted in `defer` block regardless of exit status.
2. **Output Stream Protection**:
   - Program stdout and stderr are consumed via a bounded reader buffer (`io.LimitReader` or custom bounded accumulator, max 1MB).
   - If output exceeds limit, extra bytes are drained/discarded, and `output_truncated = true` is flagged.
3. **Execution Interface**:
   - Define a clean Go interface `Executor`:
     ```go
     type Executor interface {
         Execute(ctx context.Context, req ExecutionRequest) (*ExecutionResult, error)
     }
     ```
   - Primary production implementation: `DockerExecutor`.
   - Test implementation: `MockExecutor` / `LocalProcessExecutor` for unit/integration verification in environments without Docker daemon.
4. **Honest Security Posture**:
   - Standard Docker containerization is *practical local containment*, not hypervisor-grade multi-tenant isolation.
   - For hostile multi-tenant public cloud deployments, gVisor (`runsc`), Kata Containers, or Firecracker microVMs are mandatory next steps.
