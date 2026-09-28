package recovery_test

import (
	"context"
	"testing"
	"time"

	"github.com/forgequeue/forgequeue/internal/domain"
	"github.com/forgequeue/forgequeue/internal/recovery"
	"github.com/forgequeue/forgequeue/internal/repository"
)

func TestJanitor_StaleWorkerDetectionAndJobRequeue(t *testing.T) {
	ctx := context.Background()
	outboxRepo := repository.NewMemoryOutboxRepository()
	jobRepo := repository.NewMemoryJobRepository(outboxRepo)
	workerRepo := repository.NewMemoryWorkerRepository()
	attemptRepo := repository.NewMemoryAttemptRepository()

	// 1. Worker with stale heartbeat (25 seconds ago)
	staleWorkerID := "crashed-worker-1"
	workerModel := &domain.Worker{
		ID:            staleWorkerID,
		Hostname:      "host-1",
		Concurrency:   4,
		Status:        domain.WorkerStatusOnline,
		LastHeartbeat: time.Now().Add(-25 * time.Second),
		StartedAt:     time.Now().Add(-1 * time.Hour),
	}
	_ = workerRepo.UpsertHeartbeat(ctx, workerModel)

	// 2. Orphaned running job assigned to stale worker
	jobID := "job-orphan-1"
	now := time.Now().Add(-25 * time.Second)
	job := &domain.Job{
		ID:             jobID,
		UserID:         "user-1",
		Language:       domain.LanguagePython,
		SourceCode:     "print('orphan')",
		Status:         domain.StatusRunning,
		AssignedWorker: staleWorkerID,
		CurrentAttempt: 1,
		MaxAttempts:    3,
		Limits:         domain.DefaultLimits(),
		StartedAt:      &now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	_ = jobRepo.Create(ctx, job, nil)

	// 3. Run Janitor sweep
	cfg := recovery.JanitorConfig{
		HeartbeatStaleThreshold: 10 * time.Second,
		JobLeaseGracePeriod:     10 * time.Second,
		Interval:                1 * time.Second,
	}
	janitor := recovery.NewJanitor(cfg, jobRepo, workerRepo, attemptRepo, nil)

	recovered, err := janitor.RecoverOnce(ctx)
	if err != nil {
		t.Fatalf("janitor error: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("expected 1 recovered job, got %d", recovered)
	}

	// 4. Verify worker marked OFFLINE
	w, err := workerRepo.GetByID(ctx, staleWorkerID)
	if err != nil || w.Status != domain.WorkerStatusOffline {
		t.Fatalf("expected worker to be OFFLINE, got %s, err: %v", w.Status, err)
	}

	// 5. Verify job requeued to QUEUED with attempt incremented
	updatedJob, err := jobRepo.GetByID(ctx, jobID)
	if err != nil {
		t.Fatalf("failed to get job: %v", err)
	}
	if updatedJob.Status != domain.StatusQueued {
		t.Fatalf("expected job status QUEUED, got %s", updatedJob.Status)
	}
	if updatedJob.CurrentAttempt != 2 {
		t.Fatalf("expected current_attempt 2, got %d", updatedJob.CurrentAttempt)
	}

	// 6. Verify outbox event created for re-dispatch
	pendingOutbox, err := outboxRepo.FetchPending(ctx, 10)
	if err != nil || len(pendingOutbox) != 1 {
		t.Fatalf("expected 1 pending outbox event for re-dispatch, got %d", len(pendingOutbox))
	}
}

func TestJanitor_MaxAttemptsExhaustion(t *testing.T) {
	ctx := context.Background()
	outboxRepo := repository.NewMemoryOutboxRepository()
	jobRepo := repository.NewMemoryJobRepository(outboxRepo)
	workerRepo := repository.NewMemoryWorkerRepository()
	attemptRepo := repository.NewMemoryAttemptRepository()

	// Orphaned running job already at attempt 3 of 3
	jobID := "job-exhausted-1"
	now := time.Now().Add(-30 * time.Second)
	job := &domain.Job{
		ID:             jobID,
		UserID:         "user-1",
		Language:       domain.LanguagePython,
		SourceCode:     "print('orphan')",
		Status:         domain.StatusRunning,
		AssignedWorker: "worker-dead",
		CurrentAttempt: 3,
		MaxAttempts:    3,
		Limits:         domain.DefaultLimits(),
		StartedAt:      &now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	_ = jobRepo.Create(ctx, job, nil)

	cfg := recovery.JanitorConfig{
		HeartbeatStaleThreshold: 10 * time.Second,
		JobLeaseGracePeriod:     10 * time.Second,
		Interval:                1 * time.Second,
	}
	janitor := recovery.NewJanitor(cfg, jobRepo, workerRepo, attemptRepo, nil)

	recovered, err := janitor.RecoverOnce(ctx)
	if err != nil {
		t.Fatalf("janitor error: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("expected 1 recovered job, got %d", recovered)
	}

	// Should be terminal FAILED
	finalJob, _ := jobRepo.GetByID(ctx, jobID)
	if finalJob.Status != domain.StatusFailed {
		t.Fatalf("expected FAILED status for exhausted job, got %s", finalJob.Status)
	}
	if finalJob.FailureCategory != domain.FailureCategoryInfrastructure {
		t.Fatalf("expected FailureCategoryInfrastructure, got %s", finalJob.FailureCategory)
	}
}
