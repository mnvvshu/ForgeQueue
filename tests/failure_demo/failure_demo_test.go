package failure_demo_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/forgequeue/forgequeue/internal/domain"
	"github.com/forgequeue/forgequeue/internal/executor"
	"github.com/forgequeue/forgequeue/internal/queue"
	"github.com/forgequeue/forgequeue/internal/recovery"
	"github.com/forgequeue/forgequeue/internal/repository"
	"github.com/forgequeue/forgequeue/internal/runtime"
	"github.com/forgequeue/forgequeue/internal/worker"
)

func TestFailureDemo_WorkerCrashAndJobRecovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	outboxRepo := repository.NewMemoryOutboxRepository()
	jobRepo := repository.NewMemoryJobRepository(outboxRepo)
	attemptRepo := repository.NewMemoryAttemptRepository()
	workerRepo := repository.NewMemoryWorkerRepository()
	q := queue.NewMemoryQueue()

	// Track executions per worker
	var mu sync.Mutex
	worker1Executed := false
	worker2Executed := false

	// Worker 1: simulates worker running job then crashing mid-execution
	worker1Ctx, killWorker1 := context.WithCancel(ctx)

	worker1Exec := executor.NewMockExecutor()
	worker1Exec.ResultFunc = func(execCtx context.Context, req executor.ExecutionRequest) (*executor.ExecutionResult, error) {
		mu.Lock()
		worker1Executed = true
		mu.Unlock()
		t.Logf("[Worker 1] Claimed and started Job %s (Attempt #%s). Simulating worker crash now...", req.JobID, req.AttemptID)

		// Simulate abrupt worker crash (kill worker context immediately without finishing or updating DB)
		killWorker1()
		<-execCtx.Done()
		return nil, fmt.Errorf("worker process crashed abruptly")
	}

	// Worker 2: survivor worker that picks up recovered job
	worker2Exec := executor.NewMockExecutor()
	worker2Exec.ResultFunc = func(execCtx context.Context, req executor.ExecutionRequest) (*executor.ExecutionResult, error) {
		mu.Lock()
		worker2Executed = true
		mu.Unlock()
		t.Logf("[Worker 2] Picked up recovered Job %s (Attempt #%s) and executing to completion!", req.JobID, req.AttemptID)
		return &executor.ExecutionResult{
			ExitCode:        0,
			Stdout:          "Recovered execution output from Worker 2\n",
			Duration:        10 * time.Millisecond,
			FailureCategory: domain.FailureCategoryNone,
		}, nil
	}

	cfg1 := worker.DefaultConfig()
	cfg1.WorkerID = "worker-node-1"
	w1 := worker.New(cfg1, jobRepo, attemptRepo, workerRepo, q, worker1Exec, runtime.Default(), nil)

	cfg2 := worker.DefaultConfig()
	cfg2.WorkerID = "worker-node-2"
	w2 := worker.New(cfg2, jobRepo, attemptRepo, workerRepo, q, worker2Exec, runtime.Default(), nil)

	// Start Worker 1 first so it claims the initial job
	go func() { _ = w1.Start(worker1Ctx) }()

	// Start Janitor with short thresholds
	janitorCfg := recovery.JanitorConfig{
		HeartbeatStaleThreshold: 200 * time.Millisecond,
		JobLeaseGracePeriod:     100 * time.Millisecond,
		Interval:                100 * time.Millisecond,
	}
	janitor := recovery.NewJanitor(janitorCfg, jobRepo, workerRepo, attemptRepo, nil)

	// 1. Submit Job
	jobID := "demo-crash-job-001"
	job := &domain.Job{
		ID:             jobID,
		UserID:         "demo-user",
		Language:       domain.LanguagePython,
		SourceCode:     "print('Job undergoing failure demo')",
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

	// Dispatch to queue
	_ = q.PublishJob(ctx, jobID, []byte(fmt.Sprintf(`{"job_id":"%s"}`, jobID)))

	// Wait for Worker 1 to claim and crash
	time.Sleep(200 * time.Millisecond)

	// Now start Worker 2 (the survivor worker)
	go func() { _ = w2.Start(ctx) }()

	mu.Lock()
	didW1Run := worker1Executed
	mu.Unlock()
	if !didW1Run {
		t.Fatalf("expected Worker 1 to claim the job first")
	}

	// Make Worker 1's heartbeat artificially stale in repo to simulate unresponsiveness
	staleWorker, _ := workerRepo.GetByID(ctx, "worker-node-1")
	if staleWorker != nil {
		staleWorker.LastHeartbeat = time.Now().Add(-500 * time.Millisecond)
		_ = workerRepo.UpsertHeartbeat(ctx, staleWorker)
	}

	// 2. Janitor sweeps and detects stale worker & orphaned job
	t.Logf("[Janitor] Sweeping cluster for stale workers and orphaned jobs...")
	recoveredCount, err := janitor.RecoverOnce(ctx)
	if err != nil {
		t.Fatalf("janitor recovery error: %v", err)
	}
	t.Logf("[Janitor] Detected and recovered %d orphaned job(s)", recoveredCount)

	// Fetch outbox events created during recovery and republish to queue
	requeuedEvents, err := outboxRepo.FetchPending(ctx, 10)
	if err == nil {
		for _, ev := range requeuedEvents {
			_ = q.PublishJob(ctx, ev.AggregateID, ev.Payload)
			_ = outboxRepo.MarkPublished(ctx, ev.ID)
		}
	}

	// 3. Worker 2 claims and finishes the job
	deadline := time.Now().Add(5 * time.Second)
	completed := false
	for time.Now().Before(deadline) {
		j, err := jobRepo.GetByID(ctx, jobID)
		if err == nil && j.Status == domain.StatusCompleted {
			completed = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !completed {
		t.Fatalf("job failed to complete after failover to Worker 2")
	}

	mu.Lock()
	didW2Run := worker2Executed
	mu.Unlock()
	if !didW2Run {
		t.Fatalf("expected Worker 2 to execute recovered job")
	}

	// Verify attempts history has BOTH attempts preserved
	attempts, err := attemptRepo.ListByJobID(ctx, jobID)
	if err != nil || len(attempts) < 2 {
		t.Fatalf("expected at least 2 preserved attempts, got %d", len(attempts))
	}

	t.Logf("=== FAILURE DEMO VERIFICATION SUCCESSFUL ===")
	t.Logf("Attempt 1: Worker %s (Crashed)", attempts[0].WorkerID)
	t.Logf("Attempt 2: Worker %s (Status: %s)", attempts[1].WorkerID, attempts[1].Status)
	t.Logf("Final Job Status: COMPLETED, Output: Recovered execution output from Worker 2")
}
