package recovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/forgequeue/forgequeue/internal/domain"
	"github.com/forgequeue/forgequeue/internal/repository"
)

// JanitorConfig configures the failure recovery background janitor.
type JanitorConfig struct {
	HeartbeatStaleThreshold time.Duration
	JobLeaseGracePeriod     time.Duration
	Interval                time.Duration
}

// DefaultJanitorConfig provides sensible recovery parameters.
func DefaultJanitorConfig() JanitorConfig {
	return JanitorConfig{
		HeartbeatStaleThreshold: 15 * time.Second,
		JobLeaseGracePeriod:     15 * time.Second,
		Interval:                5 * time.Second,
	}
}

// Janitor monitors worker heartbeats and recovers orphaned jobs.
type Janitor struct {
	cfg         JanitorConfig
	jobRepo     repository.JobRepository
	workerRepo  repository.WorkerRepository
	attemptRepo repository.AttemptRepository
	logger      *slog.Logger
}

// NewJanitor creates a Janitor instance.
func NewJanitor(cfg JanitorConfig, jobRepo repository.JobRepository, workerRepo repository.WorkerRepository, attemptRepo repository.AttemptRepository, logger *slog.Logger) *Janitor {
	if cfg.HeartbeatStaleThreshold <= 0 {
		cfg.HeartbeatStaleThreshold = 15 * time.Second
	}
	if cfg.JobLeaseGracePeriod <= 0 {
		cfg.JobLeaseGracePeriod = 15 * time.Second
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Janitor{
		cfg:         cfg,
		jobRepo:     jobRepo,
		workerRepo:  workerRepo,
		attemptRepo: attemptRepo,
		logger:      logger,
	}
}

// RecoverOnce performs one sweep of stale workers and orphaned jobs.
func (j *Janitor) RecoverOnce(ctx context.Context) (int, error) {
	now := time.Now()
	staleThreshold := now.Add(-j.cfg.HeartbeatStaleThreshold)

	// 1. Mark stale workers OFFLINE
	workers, err := j.workerRepo.List(ctx)
	if err == nil {
		for _, w := range workers {
			if w.Status != domain.WorkerStatusOffline && w.LastHeartbeat.Before(staleThreshold) {
				j.logger.Warn("detected stale worker, marking OFFLINE", "worker_id", w.ID, "last_heartbeat", w.LastHeartbeat)
				_ = j.workerRepo.MarkOffline(ctx, w.ID)
			}
		}
	}

	// 2. Scan stale running jobs
	staleCutoff := now.Add(-j.cfg.JobLeaseGracePeriod)
	staleJobs, err := j.jobRepo.GetStaleRunningJobs(ctx, staleCutoff)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch stale running jobs: %w", err)
	}

	recoveredCount := 0
	for _, job := range staleJobs {
		j.logger.Warn("recovering orphaned job", "job_id", job.ID, "attempt", job.CurrentAttempt, "worker", job.AssignedWorker)

		if job.CurrentAttempt < job.MaxAttempts {
			// Requeue for new attempt
			nextAttempt := job.CurrentAttempt + 1
			payload, _ := json.Marshal(map[string]string{"job_id": job.ID})
			outbox := &domain.OutboxEvent{
				AggregateID: job.ID,
				EventType:   "job.recovered",
				Payload:     payload,
				Status:      domain.OutboxStatusPending,
				CreatedAt:   time.Now(),
			}

			if err := j.jobRepo.RequeueJob(ctx, job.ID, nextAttempt, outbox); err != nil {
				j.logger.Error("failed to requeue orphaned job", "job_id", job.ID, "error", err)
				continue
			}
			recoveredCount++
			j.logger.Info("requeued orphaned job for retry", "job_id", job.ID, "new_attempt", nextAttempt)
		} else {
			// Max attempts reached -> terminal failure
			exitCode := 1
			reason := fmt.Sprintf("Job failed after %d attempts: worker crashed or execution lease expired", job.CurrentAttempt)
			_ = j.jobRepo.UpdateResult(
				ctx, job.ID, domain.StatusFailed, &exitCode,
				"", reason, false, domain.FailureCategoryInfrastructure, reason,
			)
			recoveredCount++
			j.logger.Warn("orphaned job exhausted max attempts, marked FAILED", "job_id", job.ID)
		}
	}

	return recoveredCount, nil
}

// Start runs the janitor sweep loop.
func (j *Janitor) Start(ctx context.Context) {
	ticker := time.NewTicker(j.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			j.logger.Info("recovery janitor stopped")
			return
		case <-ticker.C:
			_, _ = j.RecoverOnce(ctx)
		}
	}
}
