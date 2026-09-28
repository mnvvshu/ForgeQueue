package executor

import (
	"context"
	"time"

	"github.com/forgequeue/forgequeue/internal/domain"
)

// MockExecutor provides deterministic simulated execution results for testing.
type MockExecutor struct {
	ResultFunc func(ctx context.Context, req ExecutionRequest) (*ExecutionResult, error)
}

// NewMockExecutor creates a MockExecutor with a default success handler.
func NewMockExecutor() *MockExecutor {
	return &MockExecutor{
		ResultFunc: func(ctx context.Context, req ExecutionRequest) (*ExecutionResult, error) {
			if req.OnStdoutChunk != nil {
				req.OnStdoutChunk([]byte("Mock execution output\n"))
			}
			return &ExecutionResult{
				ExitCode:        0,
				Stdout:          "Mock execution output\n",
				Stderr:          "",
				Truncated:       false,
				Duration:        50 * time.Millisecond,
				FailureCategory: domain.FailureCategoryNone,
			}, nil
		},
	}
}

// Execute runs the custom ResultFunc.
func (m *MockExecutor) Execute(ctx context.Context, req ExecutionRequest) (*ExecutionResult, error) {
	return m.ResultFunc(ctx, req)
}
