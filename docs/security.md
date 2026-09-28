# ForgeQueue Security Model & Isolation Architecture

## Threat Model & Hostile Code Assumption

ForgeQueue executes untrusted arbitrary user code across multiple programming languages. The platform assumes all submitted code is hostile:
- **Exfiltration Attempts**: Scanning local network, internal VPC services, or querying cloud instance metadata (`169.254.169.254`).
- **Resource Depletion**: Fork bombs (`:(){ :|:& };:`), infinite loops, infinite memory allocations.
- **Disk Fill & Unbounded Output**: Infinite print loops (`while True: print('A' * 100000)`).
- **Privilege Escalation**: Attempting to escape root inside container, access host filesystems, or mount Docker sockets.

---

## Applied Container Protections

ForgeQueue applies the following defense-in-depth isolation controls to every execution container:

```bash
docker run --rm \
  --name fq-{job_id}-{attempt_id} \
  --network none \
  --cpus 1.0 \
  --memory 256m \
  --memory-swap 256m \
  --pids-limit 64 \
  --security-opt no-new-privileges \
  --cap-drop ALL \
  -v {temp_workspace}:/workspace:rw \
  -w /workspace \
  {runtime_image} \
  {command}
```

1. **Network Disabled (`--network none`)**:
   Execution containers have zero network interfaces (except loopback `lo`). Socket calls to external networks or local infrastructure are dropped immediately by the Linux kernel.
2. **No Privileged Capabilities (`--cap-drop ALL`, `--security-opt no-new-privileges`)**:
   All Linux capabilities (e.g. `CAP_SYS_ADMIN`, `CAP_NET_RAW`, `CAP_SYS_PTRACE`) are completely dropped. Processes cannot gain new privileges via setuid binaries.
3. **Hard Resource Limits**:
   - **CPU**: Quota capped (default: 1.0 core).
   - **Memory**: Hard limits with swap equality (`--memory 256m --memory-swap 256m`) ensuring the process cannot consume excess swap to evade OOM kills.
   - **Process & Thread Limit**: Bounded via `--pids-limit 64`, mitigating fork bombs.
4. **Output Stream Protection (`BoundedWriter`)**:
   Output accumulation is bounded to 1 MB. Excess output is drained and discarded without buffering in RAM, and `output_truncated = true` is flagged.
5. **Secret Isolation**:
   No environment variables containing ForgeQueue secrets (database credentials, Redis passwords, JWT signing keys) are ever passed to execution containers.
6. **Ephemeral Workspaces**:
   Every execution creates a unique temporary directory on the host (`/tmp/forgequeue-*`), mounted as `/workspace`. Regardless of exit status, a `defer` block deletes the temporary directory immediately after container termination.

---

## Honest Security Disclaimer & Production Roadmap

Standard Docker containerization with namespaces and cgroups provides **practical local process containment**, but shares the host Linux kernel.

> [!WARNING]
> In an internet-facing, multi-tenant commercial public execution platform, Docker containerization alone is vulnerable to zero-day kernel privilege escalation vulnerabilities.
> 
> Production multi-tenant executors should investigate hypervisor-grade or sandboxed micro-virtualization:
> - **gVisor (`runsc`)**: User-space kernel interception developed by Google.
> - **Firecracker**: Lightweight microVMs running on Linux KVM (used by AWS Lambda and Fargate).
> - **Kata Containers**: Lightweight QEMU/Cloud-Hypervisor containers.
