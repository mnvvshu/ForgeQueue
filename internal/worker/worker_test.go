package worker_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/forgequeue/forgequeue/internal/domain"
	"github.com/forgequeue/forgequeue/internal/executor"
	"github.com/forgequeue/forgequeue/internal/queue"
	"github.com/forgequeue/forgequeue/internal/repository"
	"github.com/forgequeue/forgequeue/internal/runtime"
	"github.com/forgequeue/forgequeue/internal/worker"
)

func setupTestWorker(exec executor.Executor) (*worker.Worker, repository.JobRepository, repository.AttemptRepository, *queue.MemoryQueue) {
	outboxRepo := repository.NewMemoryOutboxRepository()
	jobRepo := repository.NewMemoryJobRepository(outboxRepo)
	attemptRepo := repository.NewMemoryAttemptRepository()
	workerRepo := repository.NewMemoryWorkerRepository()
	q := queue.NewMemoryQueue()

	cfg := worker.DefaultConfig()
	cfg.WorkerID = "test-worker-1"
	cfg.Concurrency = 2

	w := worker.New(cfg, jobRepo, attemptRepo, workerRepo, q, exec, runtime.Default(), nil)
	return w, jobRepo, attemptRepo, q
}

func TestWorker_ExecuteSuccess(t *testing.T) {
	ctx := context.Background()
	mockExec := executor.NewMockExecutor()
	mockExec.ResultFunc = func(ctx context.Context, req executor.ExecutionRequest) (*executor.ExecutionResult, error) {
		if req.OnStdoutChunk != nil {
			req.OnStdoutChunk([]byte("Hello ForgeQueue Worker\n"))
		}
		return &executor.ExecutionResult{
			ExitCode:        0,
			Stdout:          "Hello ForgeQueue Worker\n",
			Stderr:          "",
			Duration:        10 * time.Millisecond,
			FailureCategory: domain.FailureCategoryNone,
		}, nil
	}

	w, jobRepo, attemptRepo, q := setupTestWorker(mockExec)

	// Seed job
	jobID := "job-exec-1"
	job := &domain.Job{
		ID:             jobID,
		UserID:         "user-1",
		Language:       domain.LanguagePython,
		SourceCode:     "print('Hello ForgeQueue Worker')",
		Status:         domain.StatusQueued,
		CurrentAttempt: 0,
		MaxAttempts:    3,
		Limits:         domain.DefaultLimits(),
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	if err := jobRepo.Create(ctx, job, nil); err != nil {
		t.Fatalf("failed to create job: %v", err)
	}

	// Listen for SSE / PubSub events
	eventCh, cleanup := q.Subscribe(jobID)
	defer cleanup()

	// Dispatch message
	payload, _ := json.Marshal(worker.JobPayload{JobID: jobID})
	msg := queue.Message{
		ID:      "1-0",
		JobID:   jobID,
		Payload: payload,
	}

	if err := w.ProcessSingleJob(ctx, msg); err != nil {
		t.Fatalf("process job error: %v", err)
	}

	// Verify job status
	updatedJob, err := jobRepo.GetByID(ctx, jobID)
	if err != nil {
		t.Fatalf("failed to get updated job: %v", err)
	}
	if updatedJob.Status != domain.StatusCompleted {
		t.Fatalf("expected COMPLETED status, got %s", updatedJob.Status)
	}
	if !strings.Contains(updatedJob.Stdout, "Hello ForgeQueue Worker") {
		t.Fatalf("expected stdout greeting, got: %s", updatedJob.Stdout)
	}
	if updatedJob.ExitCode == nil || *updatedJob.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %v", updatedJob.ExitCode)
	}

	// Verify attempt history
	attempts, err := attemptRepo.ListByJobID(ctx, jobID)
	if err != nil || len(attempts) != 1 {
		t.Fatalf("expected 1 attempt, got %d, err: %v", len(attempts), err)
	}
	if attempts[0].Status != domain.StatusCompleted {
		t.Fatalf("expected attempt COMPLETED, got %s", attempts[0].Status)
	}

	// Verify events received
	receivedEvent := false
	select {
	case <-eventCh:
		receivedEvent = true
	default:
	}
	if !receivedEvent {
		t.Fatalf("expected to receive real-time pubsub event")
	}
}

func TestWorker_DeterministicFailureNoRetry(t *testing.T) {
	ctx := context.Background()
	mockExec := executor.NewMockExecutor()
	mockExec.ResultFunc = func(ctx context.Context, req executor.ExecutionRequest) (*executor.ExecutionResult, error) {
		return &executor.ExecutionResult{
			ExitCode:        1,
			Stdout:          "",
			Stderr:          "SyntaxError: invalid syntax",
			Duration:        10 * time.Millisecond,
			FailureCategory: domain.FailureCategoryRuntimeError,
			FailureReason:   "SyntaxError: invalid syntax",
		}, nil
	}

	w, jobRepo, _, _ := setupTestWorker(mockExec)

	jobID := "job-syntax-err"
	job := &domain.Job{
		ID:             jobID,
		UserID:         "user-1",
		Language:       domain.LanguagePython,
		SourceCode:     "def bad_syntax(:",
		Status:         domain.StatusQueued,
		CurrentAttempt: 0,
		MaxAttempts:    3,
		Limits:         domain.DefaultLimits(),
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	_ = jobRepo.Create(ctx, job, nil)

	payload, _ := json.Marshal(worker.JobPayload{JobID: jobID})
	_ = w.ProcessSingleJob(ctx, queue.Message{ID: "2-0", JobID: jobID, Payload: payload})

	updated, _ := jobRepo.GetByID(ctx, jobID)
	if updated.Status != domain.StatusFailed {
		t.Fatalf("expected FAILED status for deterministic syntax error, got %s", updated.Status)
	}
	if updated.CurrentAttempt != 1 {
		t.Fatalf("deterministic error should not retry; attempt should be 1, got %d", updated.CurrentAttempt)
	}
}

func TestWorker_CancellationDuringExecution(t *testing.T) {
	ctx := context.Background()
	blockCh := make(chan struct{})
	unblockedCh := make(chan struct{})

	mockExec := executor.NewMockExecutor()
	mockExec.ResultFunc = func(ctx context.Context, req executor.ExecutionRequest) (*executor.ExecutionResult, error) {
		close(blockCh) // signal execution has started
		<-ctx.Done()   // wait for cancellation context
		close(unblockedCh)
		return &executor.ExecutionResult{
			ExitCode:        130,
			FailureCategory: domain.FailureCategorySystemError,
			FailureReason:   "Execution cancelled",
		}, nil
	}

	w, jobRepo, _, _ := setupTestWorker(mockExec)

	jobID := "job-cancel-test"
	job := &domain.Job{
		ID:             jobID,
		UserID:         "user-cancel",
		Language:       domain.LanguagePython,
		SourceCode:     "while True: pass",
		Status:         domain.StatusQueued,
		CurrentAttempt: 0,
		MaxAttempts:    3,
		Limits:         domain.DefaultLimits(),
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	_ = jobRepo.Create(ctx, job, nil)

	payload, _ := json.Marshal(worker.JobPayload{JobID: jobID})
	msg := queue.Message{ID: "3-0", JobID: jobID, Payload: payload}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = w.ProcessSingleJob(ctx, msg)
	}()

	<-blockCh // wait until job is running inside mock executor

	// Cancel job in repository
	_, err := jobRepo.CancelJob(ctx, jobID, "user-cancel")
	if err != nil {
		t.Fatalf("failed to cancel job: %v", err)
	}

	// Signal active worker
	cancelled := w.CancelRunningJob(jobID)
	if !cancelled {
		t.Fatalf("expected CancelRunningJob to signal running execution")
	}

	<-unblockedCh
	wg.Wait()

	finalJob, _ := jobRepo.GetByID(ctx, jobID)
	if finalJob.Status != domain.StatusCancelled {
		t.Fatalf("expected job to remain CANCELLED, got %s", finalJob.Status)
	}
}
