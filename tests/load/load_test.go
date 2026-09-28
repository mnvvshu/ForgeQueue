package load_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/forgequeue/forgequeue/internal/domain"
	"github.com/forgequeue/forgequeue/internal/executor"
	"github.com/forgequeue/forgequeue/internal/queue"
	"github.com/forgequeue/forgequeue/internal/repository"
	"github.com/forgequeue/forgequeue/internal/runtime"
	"github.com/forgequeue/forgequeue/internal/worker"
)

func TestLoad_100ConcurrentJobsMultipleWorkers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Shared In-Memory Data Store & Queue
	outboxRepo := repository.NewMemoryOutboxRepository()
	jobRepo := repository.NewMemoryJobRepository(outboxRepo)
	attemptRepo := repository.NewMemoryAttemptRepository()
	workerRepo := repository.NewMemoryWorkerRepository()
	q := queue.NewMemoryQueue()

	mockExec := executor.NewMockExecutor()
	mockExec.ResultFunc = func(ctx context.Context, req executor.ExecutionRequest) (*executor.ExecutionResult, error) {
		time.Sleep(5 * time.Millisecond) // simulate container run
		return &executor.ExecutionResult{
			ExitCode:        0,
			Stdout:          fmt.Sprintf("Output for %s\n", req.JobID),
			Duration:        5 * time.Millisecond,
			FailureCategory: domain.FailureCategoryNone,
		}, nil
	}

	// 2. Start 3 Concurrent Workers
	numWorkers := 3
	workerConcurrency := 4
	var workers []*worker.Worker

	for i := 1; i <= numWorkers; i++ {
		cfg := worker.DefaultConfig()
		cfg.WorkerID = fmt.Sprintf("load-worker-%d", i)
		cfg.Concurrency = workerConcurrency
		w := worker.New(cfg, jobRepo, attemptRepo, workerRepo, q, mockExec, runtime.Default(), nil)
		workers = append(workers, w)

		go func(wk *worker.Worker) {
			_ = wk.Start(ctx)
		}(w)
	}

	// Wait briefly for workers to start consumer loops
	time.Sleep(50 * time.Millisecond)

	// 3. Submit 100 Jobs concurrently
	numJobs := 100
	var wg sync.WaitGroup
	var submittedCount int64
	startTime := time.Now()

	for i := 1; i <= numJobs; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			jobID := fmt.Sprintf("job-load-%03d", idx)
			job := &domain.Job{
				ID:             jobID,
				UserID:         "user-benchmark",
				Language:       domain.LanguagePython,
				SourceCode:     fmt.Sprintf("print('benchmark job %d')", idx),
				Status:         domain.StatusQueued,
				CurrentAttempt: 0,
				MaxAttempts:    3,
				Limits:         domain.DefaultLimits(),
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
			}

			if err := jobRepo.Create(ctx, job, nil); err != nil {
				t.Errorf("failed to create job %s: %v", jobID, err)
				return
			}

			if err := q.PublishJob(ctx, jobID, []byte(fmt.Sprintf(`{"job_id":"%s"}`, jobID))); err != nil {
				t.Errorf("failed to publish job %s: %v", jobID, err)
				return
			}

			atomic.AddInt64(&submittedCount, 1)
		}(i)
	}

	wg.Wait()
	t.Logf("Submitted %d jobs in %v", submittedCount, time.Since(startTime))

	// 4. Poll until all 100 jobs are COMPLETED
	deadline := time.Now().Add(15 * time.Second)
	completedAll := false

	for time.Now().Before(deadline) {
		var completedCount int
		for i := 1; i <= numJobs; i++ {
			j, err := jobRepo.GetByID(ctx, fmt.Sprintf("job-load-%03d", i))
			if err == nil && j.Status == domain.StatusCompleted {
				completedCount++
			}
		}

		if completedCount == numJobs {
			completedAll = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	totalDuration := time.Since(startTime)

	if !completedAll {
		t.Fatalf("timed out waiting for 100 jobs to complete")
	}

	t.Logf("=== LOAD TEST RESULTS ===")
	t.Logf("Environment: Local (Go Process / Simulated Container)")
	t.Logf("Workers: %d (Concurrency %d each, Total Capacity: %d)", numWorkers, workerConcurrency, numWorkers*workerConcurrency)
	t.Logf("Total Jobs: %d", numJobs)
	t.Logf("Total Elapsed Time: %v", totalDuration)
	t.Logf("Throughput: %.2f jobs/sec", float64(numJobs)/totalDuration.Seconds())
	t.Logf("Status: PASS (100/100 completed successfully)")
}
