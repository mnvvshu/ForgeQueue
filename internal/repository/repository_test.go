package repository_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/forgequeue/forgequeue/internal/domain"
	"github.com/forgequeue/forgequeue/internal/repository"
)

func TestMemoryRepository_JobLifecycle(t *testing.T) {
	ctx := context.Background()
	outboxRepo := repository.NewMemoryOutboxRepository()
	jobRepo := repository.NewMemoryJobRepository(outboxRepo)
	attemptRepo := repository.NewMemoryAttemptRepository()

	// 1. Create Job with Outbox Event
	jobID := "job-123"
	userID := "user-456"
	job := &domain.Job{
		ID:             jobID,
		UserID:         userID,
		Language:       domain.LanguagePython,
		SourceCode:     "print('hello')",
		Status:         domain.StatusQueued,
		CurrentAttempt: 0,
		MaxAttempts:    3,
		Limits:         domain.DefaultLimits(),
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	payload, _ := json.Marshal(map[string]string{"job_id": jobID})
	outbox := &domain.OutboxEvent{
		AggregateID: jobID,
		EventType:   "job.queued",
		Payload:     payload,
		Status:      domain.OutboxStatusPending,
		CreatedAt:   time.Now(),
	}

	if err := jobRepo.Create(ctx, job, outbox); err != nil {
		t.Fatalf("failed to create job: %v", err)
	}

	// Verify Outbox Event created
	pending, err := outboxRepo.FetchPending(ctx, 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("expected 1 pending outbox event, got %d, err: %v", len(pending), err)
	}

	// 2. Claim Job by Worker A
	claimed, err := jobRepo.ClaimJob(ctx, jobID, "worker-1", 1)
	if err != nil || !claimed {
		t.Fatalf("expected job to be claimed, claimed=%v, err=%v", claimed, err)
	}

	// 3. Record Attempt
	attemptID := "attempt-1"
	attempt := &domain.JobAttempt{
		ID:            attemptID,
		JobID:         jobID,
		AttemptNumber: 1,
		WorkerID:      "worker-1",
		Status:        domain.StatusRunning,
		StartedAt:     time.Now(),
		CreatedAt:     time.Now(),
	}
	if err := attemptRepo.Create(ctx, attempt); err != nil {
		t.Fatalf("failed to create attempt: %v", err)
	}

	// 4. Update Result to COMPLETED
	exitCode := 0
	stdout := "hello\n"
	if err := jobRepo.UpdateResult(ctx, jobID, domain.StatusCompleted, &exitCode, stdout, "", false, domain.FailureCategoryNone, ""); err != nil {
		t.Fatalf("failed to update job result: %v", err)
	}
	if err := attemptRepo.UpdateResult(ctx, attemptID, domain.StatusCompleted, &exitCode, stdout, "", false, domain.FailureCategoryNone, ""); err != nil {
		t.Fatalf("failed to update attempt result: %v", err)
	}

	// Verify persisted state
	saved, err := jobRepo.GetByID(ctx, jobID)
	if err != nil {
		t.Fatalf("failed to get job: %v", err)
	}
	if saved.Status != domain.StatusCompleted || saved.Stdout != stdout || *saved.ExitCode != 0 {
		t.Fatalf("unexpected job state: %+v", saved)
	}

	attempts, err := attemptRepo.ListByJobID(ctx, jobID)
	if err != nil || len(attempts) != 1 {
		t.Fatalf("expected 1 attempt, got %d, err=%v", len(attempts), err)
	}
	if attempts[0].Status != domain.StatusCompleted {
		t.Fatalf("expected attempt COMPLETED, got %s", attempts[0].Status)
	}
}

func TestMemoryRepository_CancellationSecurity(t *testing.T) {
	ctx := context.Background()
	outboxRepo := repository.NewMemoryOutboxRepository()
	jobRepo := repository.NewMemoryJobRepository(outboxRepo)

	jobID := "job-sec-1"
	ownerID := "user-alice"
	attackerID := "user-mallory"

	job := &domain.Job{
		ID:             jobID,
		UserID:         ownerID,
		Language:       domain.LanguageGo,
		SourceCode:     "package main",
		Status:         domain.StatusQueued,
		CurrentAttempt: 0,
		MaxAttempts:    3,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	if err := jobRepo.Create(ctx, job, nil); err != nil {
		t.Fatalf("failed to create job: %v", err)
	}

	// Mallory tries to cancel Alice's job
	_, err := jobRepo.CancelJob(ctx, jobID, attackerID)
	if err != domain.ErrForbidden {
		t.Fatalf("expected ErrForbidden for cross-user cancellation, got: %v", err)
	}

	// Alice cancels her own job
	cancelled, err := jobRepo.CancelJob(ctx, jobID, ownerID)
	if err != nil {
		t.Fatalf("expected successful cancellation by owner, got: %v", err)
	}
	if cancelled.Status != domain.StatusCancelled {
		t.Fatalf("expected status CANCELLED, got %s", cancelled.Status)
	}
}

func TestMemoryRepository_Idempotency(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewMemoryIdempotencyRepository()

	userID := "user-1"
	key := "req-idempotent-999"
	jobID := "job-999"

	// Initial store
	idem := &domain.IdempotencyKey{
		Key:       key,
		UserID:    userID,
		JobID:     jobID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	if err := repo.Store(ctx, idem); err != nil {
		t.Fatalf("failed to store idempotency key: %v", err)
	}

	// Second duplicate store should be no-op
	if err := repo.Store(ctx, idem); err != nil {
		t.Fatalf("expected idempotent duplicate store to succeed, got: %v", err)
	}

	// Retrieve
	found, err := repo.Get(ctx, userID, key)
	if err != nil {
		t.Fatalf("failed to get idempotency key: %v", err)
	}
	if found.JobID != jobID {
		t.Fatalf("expected jobID %s, got %s", jobID, found.JobID)
	}

	// Other user should not find this key
	_, err = repo.Get(ctx, "user-2", key)
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound for other user, got: %v", err)
	}
}
