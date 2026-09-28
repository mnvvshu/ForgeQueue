package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/forgequeue/forgequeue/internal/domain"
)

// DockerExecutor runs user code inside an ephemeral isolated Docker container.
type DockerExecutor struct {
	dockerPath string
}

// NewDockerExecutor creates a DockerExecutor.
func NewDockerExecutor() (*DockerExecutor, error) {
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return nil, fmt.Errorf("docker binary not found in PATH: %w", err)
	}
	return &DockerExecutor{dockerPath: dockerPath}, nil
}

// Execute handles workspace creation, source writing, compilation (if needed), container launch, and cleanup.
func (e *DockerExecutor) Execute(ctx context.Context, req ExecutionRequest) (*ExecutionResult, error) {
	// 1. Create isolated temporary workspace
	workspaceDir, err := os.MkdirTemp("", fmt.Sprintf("forgequeue-%s-*", req.JobID))
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary workspace: %w", err)
	}
	defer os.RemoveAll(workspaceDir)

	// 2. Write source code file
	sourceFilePath := filepath.Join(workspaceDir, req.Runtime.SourceFileName)
	if err := os.WriteFile(sourceFilePath, []byte(req.SourceCode), 0644); err != nil {
		return nil, fmt.Errorf("failed to write source code: %w", err)
	}

	// 3. Write stdin file if present
	var stdinFile *os.File
	if req.Stdin != "" {
		stdinPath := filepath.Join(workspaceDir, "input.stdin")
		if err := os.WriteFile(stdinPath, []byte(req.Stdin), 0644); err != nil {
			return nil, fmt.Errorf("failed to write stdin: %w", err)
		}
		stdinFile, err = os.Open(stdinPath)
		if err != nil {
			return nil, fmt.Errorf("failed to open stdin file: %w", err)
		}
		defer stdinFile.Close()
	}

	// 4. Setup timeout context
	timeout := time.Duration(req.Limits.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = req.Runtime.DefaultTimeout
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	containerName := fmt.Sprintf("fq-%s-%s", req.JobID[:8], req.AttemptID[:8])
	defer func() {
		// Ensure container is forcefully killed and removed upon exit
		cleanupCtx, cCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cCancel()
		_ = exec.CommandContext(cleanupCtx, e.dockerPath, "rm", "-f", containerName).Run()
	}()

	startTime := time.Now()

	// 5. If compilation is required, compile first inside the container
	if len(req.Runtime.CompileCmd) > 0 {
		compileCmdArgs := e.buildDockerArgs(containerName+"-compile", workspaceDir, req.Runtime.Image, req.Limits, req.Runtime.CompileCmd)
		compileCmd := exec.CommandContext(execCtx, e.dockerPath, compileCmdArgs...)
		var compileOut bytes.Buffer
		compileCmd.Stdout = &compileOut
		compileCmd.Stderr = &compileOut

		if err := compileCmd.Run(); err != nil {
			exitCode := 1
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			}
			return &ExecutionResult{
				ExitCode:        exitCode,
				Stderr:          compileOut.String(),
				Duration:        time.Since(startTime),
				FailureCategory: domain.FailureCategoryCompilationError,
				FailureReason:   "Compilation failed",
			}, nil
		}
	}

	// 6. Run execution container
	stdoutWriter := NewBoundedWriter(req.Limits.MaxOutputBytes, req.OnStdoutChunk)
	stderrWriter := NewBoundedWriter(req.Limits.MaxOutputBytes, req.OnStderrChunk)

	runDockerArgs := e.buildDockerArgs(containerName, workspaceDir, req.Runtime.Image, req.Limits, req.Runtime.RunCmd)
	cmd := exec.CommandContext(execCtx, e.dockerPath, runDockerArgs...)
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter
	if stdinFile != nil {
		cmd.Stdin = stdinFile
	}

	runErr := cmd.Run()
	duration := time.Since(startTime)

	result := &ExecutionResult{
		ExitCode:        0,
		Stdout:          stdoutWriter.String(),
		Stderr:          stderrWriter.String(),
		Truncated:       stdoutWriter.IsTruncated() || stderrWriter.IsTruncated(),
		Duration:        duration,
		FailureCategory: domain.FailureCategoryNone,
	}

	if runErr != nil {
		if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			result.FailureCategory = domain.FailureCategoryTimeout
			result.FailureReason = fmt.Sprintf("Execution timed out after %v", timeout)
			result.ExitCode = 124
			return result, nil
		}
		if errors.Is(execCtx.Err(), context.Canceled) {
			result.FailureCategory = domain.FailureCategorySystemError
			result.FailureReason = "Execution cancelled"
			result.ExitCode = 130
			return result, nil
		}

		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			result.FailureCategory = domain.FailureCategoryRuntimeError
			result.FailureReason = fmt.Sprintf("Process exited with status code %d", result.ExitCode)
			return result, nil
		}

		result.FailureCategory = domain.FailureCategorySystemError
		result.FailureReason = runErr.Error()
		result.ExitCode = 1
		return result, nil
	}

	return result, nil
}

func (e *DockerExecutor) buildDockerArgs(containerName, workspaceDir, image string, limits domain.Limits, runCmd []string) []string {
	memMB := limits.MemoryLimitMB
	if memMB <= 0 {
		memMB = 256
	}
	pids := limits.MaxPIDs
	if pids <= 0 {
		pids = 64
	}
	cpu := limits.CPULimit
	if cpu == "" {
		cpu = "1.0"
	}

	args := []string{
		"run",
		"--rm",
		"--name", containerName,
		"--network", "none",
		fmt.Sprintf("--cpus=%s", cpu),
		fmt.Sprintf("--memory=%dm", memMB),
		fmt.Sprintf("--memory-swap=%dm", memMB),
		fmt.Sprintf("--pids-limit=%d", pids),
		"--security-opt", "no-new-privileges",
		"--cap-drop", "ALL",
		"-v", fmt.Sprintf("%s:/workspace:rw", workspaceDir),
		"-w", "/workspace",
		image,
	}

	args = append(args, runCmd...)
	return args
}
