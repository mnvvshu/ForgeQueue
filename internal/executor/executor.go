package executor

import (
	"bytes"
	"context"
	"sync"
	"time"

	"github.com/forgequeue/forgequeue/internal/domain"
	"github.com/forgequeue/forgequeue/internal/runtime"
)

// ExecutionRequest specifies code and constraints for an execution run.
type ExecutionRequest struct {
	JobID         string
	AttemptID     string
	Runtime       runtime.Definition
	SourceCode    string
	Stdin         string
	Limits        domain.Limits
	OnStdoutChunk func(chunk []byte)
	OnStderrChunk func(chunk []byte)
}

// ExecutionResult captures the outcome of an execution attempt.
type ExecutionResult struct {
	ExitCode        int
	Stdout          string
	Stderr          string
	Truncated       bool
	Duration        time.Duration
	FailureCategory domain.FailureCategory
	FailureReason   string
}

// Executor defines the contract for executing code inside an isolated environment.
type Executor interface {
	Execute(ctx context.Context, req ExecutionRequest) (*ExecutionResult, error)
}

// BoundedWriter is an io.Writer that captures up to maxBytes and forwards chunks.
type BoundedWriter struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	maxBytes  int
	truncated bool
	onChunk   func(chunk []byte)
}

// NewBoundedWriter creates a BoundedWriter.
func NewBoundedWriter(maxBytes int, onChunk func(chunk []byte)) *BoundedWriter {
	if maxBytes <= 0 {
		maxBytes = 1024 * 1024 // 1 MB default
	}
	return &BoundedWriter{
		maxBytes: maxBytes,
		onChunk:  onChunk,
	}
}

// Write appends data up to limit and notifies listener.
func (bw *BoundedWriter) Write(p []byte) (n int, err error) {
	bw.mu.Lock()
	defer bw.mu.Unlock()

	n = len(p)
	if n == 0 {
		return 0, nil
	}

	remaining := bw.maxBytes - bw.buf.Len()
	if remaining > 0 {
		toWrite := p
		if len(p) > remaining {
			toWrite = p[:remaining]
			bw.truncated = true
		}
		bw.buf.Write(toWrite)
	} else {
		bw.truncated = true
	}

	if bw.onChunk != nil {
		bw.onChunk(p)
	}

	return n, nil
}

// String returns the captured content.
func (bw *BoundedWriter) String() string {
	bw.mu.Lock()
	defer bw.mu.Unlock()
	return bw.buf.String()
}

// IsTruncated returns whether output exceeded maxBytes.
func (bw *BoundedWriter) IsTruncated() bool {
	bw.mu.Lock()
	defer bw.mu.Unlock()
	return bw.truncated
}
