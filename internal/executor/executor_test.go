package executor_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/forgequeue/forgequeue/internal/domain"
	"github.com/forgequeue/forgequeue/internal/executor"
	"github.com/forgequeue/forgequeue/internal/runtime"
)

func TestBoundedWriter_Truncation(t *testing.T) {
	var collectedChunks []string
	var mu sync.Mutex

	onChunk := func(chunk []byte) {
		mu.Lock()
		collectedChunks = append(collectedChunks, string(chunk))
		mu.Unlock()
	}

	maxBytes := 20
	bw := executor.NewBoundedWriter(maxBytes, onChunk)

	// Write 15 bytes
	n1, err := bw.Write([]byte("123456789012345"))
	if err != nil || n1 != 15 {
		t.Fatalf("unexpected write error: %v", err)
	}
	if bw.IsTruncated() {
		t.Fatalf("expected not truncated yet")
	}

	// Write another 10 bytes (total 25 > 20)
	n2, err := bw.Write([]byte("ABCDEFGHIJ"))
	if err != nil || n2 != 10 {
		t.Fatalf("unexpected write error: %v", err)
	}
	if !bw.IsTruncated() {
		t.Fatalf("expected truncated to be true")
	}

	out := bw.String()
	if len(out) != maxBytes {
		t.Fatalf("expected output length %d, got %d (%s)", maxBytes, len(out), out)
	}
	if out != "123456789012345ABCDE" {
		t.Fatalf("unexpected content: %s", out)
	}
}

func TestProcessExecutor_PythonExecution(t *testing.T) {
	reg := runtime.Default()
	pyDef, err := reg.Get(domain.LanguagePython)
	if err != nil {
		t.Fatalf("failed to get python definition: %v", err)
	}

	exec := executor.NewProcessExecutor()

	var chunks []string
	var mu sync.Mutex

	req := executor.ExecutionRequest{
		JobID:      "test-py-1",
		AttemptID:  "att-1",
		Runtime:    pyDef,
		SourceCode: "print('Hello ForgeQueue Python')",
		Limits:     domain.DefaultLimits(),
		OnStdoutChunk: func(c []byte) {
			mu.Lock()
			chunks = append(chunks, string(c))
			mu.Unlock()
		},
	}

	res, err := exec.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}

	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d, stderr: %s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "Hello ForgeQueue Python") {
		t.Fatalf("expected stdout to contain greeting, got: %s", res.Stdout)
	}
}

func TestProcessExecutor_NodeExecution(t *testing.T) {
	reg := runtime.Default()
	nodeDef, err := reg.Get(domain.LanguageJavascript)
	if err != nil {
		t.Fatalf("failed to get node definition: %v", err)
	}

	exec := executor.NewProcessExecutor()

	req := executor.ExecutionRequest{
		JobID:      "test-node-1",
		AttemptID:  "att-1",
		Runtime:    nodeDef,
		SourceCode: "console.log('Hello ForgeQueue Node');",
		Limits:     domain.DefaultLimits(),
	}

	res, err := exec.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}

	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d, stderr: %s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "Hello ForgeQueue Node") {
		t.Fatalf("expected stdout to contain greeting, got: %s", res.Stdout)
	}
}

func TestProcessExecutor_Stdin(t *testing.T) {
	reg := runtime.Default()
	nodeDef, err := reg.Get(domain.LanguageJavascript)
	if err != nil {
		t.Fatalf("failed to get node definition: %v", err)
	}

	exec := executor.NewProcessExecutor()

	req := executor.ExecutionRequest{
		JobID:      "test-stdin-1",
		AttemptID:  "att-1",
		Runtime:    nodeDef,
		SourceCode: `
			const fs = require('fs');
			const input = fs.readFileSync(0, 'utf-8').trim();
			console.log('RECEIVED:' + input);
		`,
		Stdin:  "Custom Stdin Input 42",
		Limits: domain.DefaultLimits(),
	}

	res, err := exec.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}

	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d, stderr: %s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "RECEIVED:Custom Stdin Input 42") {
		t.Fatalf("expected stdin to be received, got: %s", res.Stdout)
	}
}

func TestProcessExecutor_Timeout(t *testing.T) {
	reg := runtime.Default()
	nodeDef, err := reg.Get(domain.LanguageJavascript)
	if err != nil {
		t.Fatalf("failed to get node definition: %v", err)
	}

	exec := executor.NewProcessExecutor()

	limits := domain.DefaultLimits()
	limits.TimeoutSeconds = 1 // 1 second timeout

	req := executor.ExecutionRequest{
		JobID:      "test-timeout-1",
		AttemptID:  "att-1",
		Runtime:    nodeDef,
		SourceCode: "while(true){}",
		Limits:     limits,
	}

	res, err := exec.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}

	if res.FailureCategory != domain.FailureCategoryTimeout {
		t.Fatalf("expected FailureCategoryTimeout, got %s", res.FailureCategory)
	}
	if res.ExitCode != 124 {
		t.Fatalf("expected exit code 124 for timeout, got %d", res.ExitCode)
	}
}

func TestProcessExecutor_RuntimeError(t *testing.T) {
	reg := runtime.Default()
	nodeDef, err := reg.Get(domain.LanguageJavascript)
	if err != nil {
		t.Fatalf("failed to get node definition: %v", err)
	}

	exec := executor.NewProcessExecutor()

	req := executor.ExecutionRequest{
		JobID:      "test-err-1",
		AttemptID:  "att-1",
		Runtime:    nodeDef,
		SourceCode: "throw new Error('Explosion in user code');",
		Limits:     domain.DefaultLimits(),
	}

	res, err := exec.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}

	if res.FailureCategory != domain.FailureCategoryRuntimeError {
		t.Fatalf("expected FailureCategoryRuntimeError, got %s", res.FailureCategory)
	}
	if res.ExitCode == 0 {
		t.Fatalf("expected non-zero exit code for runtime error")
	}
	if !strings.Contains(res.Stderr, "Explosion in user code") {
		t.Fatalf("expected stderr to contain error trace, got: %s", res.Stderr)
	}
}

func TestProcessExecutor_GoExecution(t *testing.T) {
	reg := runtime.Default()
	goDef, err := reg.Get(domain.LanguageGo)
	if err != nil {
		t.Fatalf("failed to get go definition: %v", err)
	}

	exec := executor.NewProcessExecutor()

	req := executor.ExecutionRequest{
		JobID:      "test-go-1",
		AttemptID:  "att-1",
		Runtime:    goDef,
		SourceCode: "package main\nimport \"fmt\"\nfunc main() {\nfmt.Println(\"Hello ForgeQueue Go\")\n}\n",
		Limits:     domain.DefaultLimits(),
	}

	res, err := exec.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}

	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d, stderr: %q, stdout: %q, reason: %q, cat: %s", res.ExitCode, res.Stderr, res.Stdout, res.FailureReason, res.FailureCategory)
	}
	if !strings.Contains(res.Stdout, "Hello ForgeQueue Go") {
		t.Fatalf("expected stdout to contain greeting, got: %s", res.Stdout)
	}
}
