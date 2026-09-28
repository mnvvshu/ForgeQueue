package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/forgequeue/forgequeue/internal/domain"
)

// ProcessExecutor runs code directly via local host process for offline/local testing.
type ProcessExecutor struct{}

// NewProcessExecutor creates a ProcessExecutor.
func NewProcessExecutor() *ProcessExecutor {
	return &ProcessExecutor{}
}

// Execute runs the code using local interpreters/compilers.
func (e *ProcessExecutor) Execute(ctx context.Context, req ExecutionRequest) (*ExecutionResult, error) {
	workspaceDir, err := os.MkdirTemp("", fmt.Sprintf("fq-proc-%s-*", req.JobID))
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary workspace: %w", err)
	}
	defer os.RemoveAll(workspaceDir)

	sourceFilePath := filepath.Join(workspaceDir, req.Runtime.SourceFileName)
	if err := os.WriteFile(sourceFilePath, []byte(req.SourceCode), 0644); err != nil {
		return nil, fmt.Errorf("failed to write source code: %w", err)
	}

	timeout := time.Duration(req.Limits.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = req.Runtime.DefaultTimeout
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	startTime := time.Now()

	// Compilation step if needed
	if len(req.Runtime.CompileCmd) > 0 {
		compileCmd := exec.CommandContext(execCtx, req.Runtime.CompileCmd[0], req.Runtime.CompileCmd[1:]...)
		compileCmd.Dir = workspaceDir
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

	// Execution step
	stdoutWriter := NewBoundedWriter(req.Limits.MaxOutputBytes, req.OnStdoutChunk)
	stderrWriter := NewBoundedWriter(req.Limits.MaxOutputBytes, req.OnStderrChunk)

	runCmd := req.Runtime.RunCmd
	// Windows compatibility tweak for python interpreter command if 'py' launcher is installed
	if req.Runtime.Language == domain.LanguagePython {
		if _, err := exec.LookPath("py"); err == nil {
			runCmd = []string{"py", "-3", "-u", req.Runtime.SourceFileName}
		} else if _, err := exec.LookPath("python3"); err == nil {
			runCmd = []string{"python3", "-u", req.Runtime.SourceFileName}
		} else if _, err := exec.LookPath("python"); err == nil {
			runCmd = []string{"python", "-u", req.Runtime.SourceFileName}
		}
	}

	runBinary := runCmd[0]
	if strings.HasPrefix(runBinary, "./") {
		cleanName := strings.TrimPrefix(runBinary, "./")
		binPath := filepath.Join(workspaceDir, cleanName)
		exePath := filepath.Join(workspaceDir, cleanName+".exe")

		if _, err := os.Stat(exePath); err == nil {
			runBinary = exePath
		} else if _, err := os.Stat(binPath); err == nil {
			// Windows requires .exe extension to execute
			if err := os.Rename(binPath, exePath); err == nil {
				runBinary = exePath
			} else {
				runBinary = binPath
			}
		} else {
			runBinary = binPath
		}
	}

	cmd := exec.CommandContext(execCtx, runBinary, runCmd[1:]...)
	cmd.Dir = workspaceDir
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter

	if req.Stdin != "" {
		cmd.Stdin = bytes.NewBufferString(req.Stdin)
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
