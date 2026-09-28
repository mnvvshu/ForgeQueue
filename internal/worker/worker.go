package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/forgequeue/forgequeue/internal/domain"
	"github.com/forgequeue/forgequeue/internal/executor"
	"github.com/forgequeue/forgequeue/internal/queue"
	"github.com/forgequeue/forgequeue/internal/repository"
	"github.com/forgequeue/forgequeue/internal/runtime"
	"github.com/forgequeue/forgequeue/internal/statemachine"
)

// Config holds configuration parameters for a Worker instance.
type Config struct {
	WorkerID          string
	Hostname          string
	Concurrency       int
	HeartbeatInterval time.Duration
	MinIdleClaimTime  time.Duration
	ClaimBatchSize    int64
	Version           string
}

// DefaultConfig generates a sane default worker configuration.
func DefaultConfig() Config {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown-host"
	}
	workerID := fmt.Sprintf("worker-%s-%s", hostname, uuid.New().String()[:8])
	return Config{
		WorkerID:          workerID,
		Hostname:          hostname,
		Concurrency:       4,
		HeartbeatInterval: 3 * time.Second,
		MinIdleClaimTime:  20 * time.Second,
		ClaimBatchSize:    5,
		Version:           "1.0.0",
	}
}

// JobPayload represents the JSON payload dispatched via the queue stream.
type JobPayload struct {
	JobID string `json:"job_id"`
}

// Worker executes jobs pulled from Redis Streams with bounded concurrency and crash recovery.
type Worker struct {
	cfg             Config
	repo            repository.JobRepository
	attemptRepo     repository.AttemptRepository
	workerRepo      repository.WorkerRepository
	queue           queue.Queue
	executor        executor.Executor
	registry        *runtime.Registry
	sm              *statemachine.StateMachine
	semaphore       chan struct{}
	activeExecs     sync.Map // jobID -> context.CancelFunc
	activeCount     int32
	completedCount  int64
	failedCount     int64
	logger          *slog.Logger
	wg              sync.WaitGroup
}

// New creates a new Worker instance.
func New(
	cfg Config,
	repo repository.JobRepository,
	attemptRepo repository.AttemptRepository,
	workerRepo repository.WorkerRepository,
	q queue.Queue,
	exec executor.Executor,
	registry *runtime.Registry,
	logger *slog.Logger,
) *Worker {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 4
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 3 * time.Second
	}
	if registry == nil {
		registry = runtime.Default()
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &Worker{
		cfg:         cfg,
		repo:        repo,
		attemptRepo: attemptRepo,
		workerRepo:  workerRepo,
		queue:       q,
		executor:    exec,
		registry:    registry,
		sm:          statemachine.New(),
		semaphore:   make(chan struct{}, cfg.Concurrency),
		logger:      logger.With("worker_id", cfg.WorkerID),
	}
}

// ID returns the unique identifier of the worker.
func (w *Worker) ID() string {
	return w.cfg.WorkerID
}

// ActiveJobsCount returns the number of currently running executions.
func (w *Worker) ActiveJobsCount() int {
	return int(atomic.LoadInt32(&w.activeCount))
}

// CancelRunningJob attempts to cancel a currently running execution on this worker.
func (w *Worker) CancelRunningJob(jobID string) bool {
	val, ok := w.activeExecs.Load(jobID)
	if !ok {
		return false
	}
	cancelFn, isCancel := val.(context.CancelFunc)
	if isCancel && cancelFn != nil {
		cancelFn()
		return true
	}
	return false
}

// Start runs the worker message consumption, heartbeats, and stale claim loops.
func (w *Worker) Start(ctx context.Context) error {
	w.logger.Info("starting worker", "concurrency", w.cfg.Concurrency, "hostname", w.cfg.Hostname)

	// Ensure stream and consumer group exist
	if err := w.queue.EnsureStreamAndGroup(ctx); err != nil {
		return fmt.Errorf("failed to prepare queue: %w", err)
	}

	// Register worker in repository
	now := time.Now()
	workerModel := &domain.Worker{
		ID:            w.cfg.WorkerID,
		Hostname:      w.cfg.Hostname,
		Concurrency:   w.cfg.Concurrency,
		ActiveJobs:    0,
		CompletedJobs: 0,
		FailedJobs:    0,
		Status:        domain.WorkerStatusOnline,
		LastHeartbeat: now,
		StartedAt:     now,
		Version:       w.cfg.Version,
	}
	if err := w.workerRepo.UpsertHeartbeat(ctx, workerModel); err != nil {
		w.logger.Error("failed initial worker registration", "error", err)
	}

	// Start Heartbeat Loop
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.runHeartbeatLoop(ctx)
	}()

	// Start Stale Pending Claim Loop
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.runClaimLoop(ctx)
	}()

	// Start Queue Consumer Loop
	w.runConsumerLoop(ctx)

	// Wait for in-flight jobs and background tasks to terminate
	w.wg.Wait()
	w.logger.Info("worker gracefully stopped")
	return nil
}

func (w *Worker) runHeartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Mark draining or offline on exit
			_ = w.workerRepo.MarkOffline(context.Background(), w.cfg.WorkerID)
			return
		case <-ticker.C:
			workerModel := &domain.Worker{
				ID:            w.cfg.WorkerID,
				Hostname:      w.cfg.Hostname,
				Concurrency:   w.cfg.Concurrency,
				ActiveJobs:    w.ActiveJobsCount(),
				CompletedJobs: atomic.LoadInt64(&w.completedCount),
				FailedJobs:    atomic.LoadInt64(&w.failedCount),
				Status:        domain.WorkerStatusOnline,
				LastHeartbeat: time.Now(),
				StartedAt:     time.Now(),
				Version:       w.cfg.Version,
			}
			if err := w.workerRepo.UpsertHeartbeat(ctx, workerModel); err != nil {
				w.logger.Warn("failed to update worker heartbeat", "error", err)
			}
		}
	}
}

func (w *Worker) runClaimLoop(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.MinIdleClaimTime / 2)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			claimed, err := w.queue.ClaimStaleJobs(ctx, w.cfg.WorkerID, w.cfg.MinIdleClaimTime, w.cfg.ClaimBatchSize)
			if err != nil {
				w.logger.Warn("failed to claim stale jobs from stream", "error", err)
				continue
			}
			for _, msg := range claimed {
				w.logger.Info("claimed stale job from crashed worker", "msg_id", msg.ID, "job_id", msg.JobID)
				w.dispatchJobExecution(ctx, msg)
			}
		}
	}
}

func (w *Worker) runConsumerLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Read jobs from consumer group
			messages, err := w.queue.ReadJobs(ctx, w.cfg.WorkerID, 1, 2*time.Second)
			if err != nil {
				if errors.Is(ctx.Err(), context.Canceled) {
					return
				}
				w.logger.Error("failed reading from queue stream", "error", err)
				time.Sleep(1 * time.Second)
				continue
			}

			for _, msg := range messages {
				w.dispatchJobExecution(ctx, msg)
			}
		}
	}
}

func (w *Worker) dispatchJobExecution(ctx context.Context, msg queue.Message) {
	// Bounded concurrency semaphore acquisition
	select {
	case w.semaphore <- struct{}{}:
	case <-ctx.Done():
		return
	}

	w.wg.Add(1)
	go func() {
		defer func() {
			<-w.semaphore
			w.wg.Done()
		}()
		w.processJob(ctx, msg)
	}()
}

// ProcessSingleJob processes one message synchronously (used in tests and worker loop).
func (w *Worker) ProcessSingleJob(ctx context.Context, msg queue.Message) error {
	return w.processJob(ctx, msg)
}

func (w *Worker) processJob(ctx context.Context, msg queue.Message) error {
	var payload JobPayload
	if len(msg.Payload) > 0 {
		_ = json.Unmarshal(msg.Payload, &payload)
	}
	jobID := payload.JobID
	if jobID == "" {
		jobID = msg.JobID
	}

	w.logger.Info("processing job message", "job_id", jobID, "msg_id", msg.ID)

	// Fetch job record
	job, err := w.repo.GetByID(ctx, jobID)
	if err != nil {
		w.logger.Error("failed to get job from database", "job_id", jobID, "error", err)
		_ = w.queue.AckJob(ctx, msg.ID)
		return err
	}

	// If job is already in terminal state (e.g. cancelled while queued), ack and terminate
	if job.Status.IsTerminal() {
		w.logger.Info("job already in terminal state, skipping", "job_id", jobID, "status", job.Status)
		_ = w.queue.AckJob(ctx, msg.ID)
		return nil
	}

	// Atomic state transition to RUNNING
	nextAttempt := job.CurrentAttempt + 1
	claimed, err := w.repo.ClaimJob(ctx, job.ID, w.cfg.WorkerID, nextAttempt)
	if err != nil {
		w.logger.Error("error attempting to claim job", "job_id", jobID, "error", err)
		return err
	}
	if !claimed {
		w.logger.Warn("job already claimed or not queued", "job_id", jobID)
		_ = w.queue.AckJob(ctx, msg.ID)
		return nil
	}

	atomic.AddInt32(&w.activeCount, 1)
	defer atomic.AddInt32(&w.activeCount, -1)

	// Create Attempt Record
	attemptID := uuid.New().String()
	attempt := &domain.JobAttempt{
		ID:            attemptID,
		JobID:         job.ID,
		AttemptNumber: nextAttempt,
		WorkerID:      w.cfg.WorkerID,
		Status:        domain.StatusRunning,
		StartedAt:     time.Now(),
		CreatedAt:     time.Now(),
	}
	if err := w.attemptRepo.Create(ctx, attempt); err != nil {
		w.logger.Error("failed to record job attempt", "job_id", jobID, "error", err)
	}

	// Broadcast RUNNING status event
	w.broadcastEvent(ctx, job.ID, domain.ExecutionStreamEvent{
		Type:      "status",
		JobID:     job.ID,
		Status:    domain.StatusRunning,
		Timestamp: time.Now(),
	})

	// Get runtime definition
	runtimeDef, err := w.registry.Get(job.Language)
	if err != nil {
		w.logger.Error("unsupported language", "language", job.Language, "job_id", jobID)
		w.finalizeFailure(ctx, msg, job, attempt, 1, domain.FailureCategorySystemError, "unsupported language")
		return nil
	}

	// Execution context with cancellation hook
	execCtx, cancel := context.WithCancel(ctx)
	w.activeExecs.Store(job.ID, cancel)
	defer func() {
		w.activeExecs.Delete(job.ID)
		cancel()
	}()

	// Setup streaming callbacks
	req := executor.ExecutionRequest{
		JobID:      job.ID,
		AttemptID:  attemptID,
		Runtime:    runtimeDef,
		SourceCode: job.SourceCode,
		Stdin:      job.Stdin,
		Limits:     job.Limits,
		OnStdoutChunk: func(chunk []byte) {
			w.broadcastEvent(ctx, job.ID, domain.ExecutionStreamEvent{
				Type:      "stdout",
				JobID:     job.ID,
				Data:      string(chunk),
				Timestamp: time.Now(),
			})
		},
		OnStderrChunk: func(chunk []byte) {
			w.broadcastEvent(ctx, job.ID, domain.ExecutionStreamEvent{
				Type:      "stderr",
				JobID:     job.ID,
				Data:      string(chunk),
				Timestamp: time.Now(),
			})
		},
	}

	// Execute inside isolated container
	result, execErr := w.executor.Execute(execCtx, req)
	if execErr != nil {
		w.logger.Error("execution engine error", "job_id", job.ID, "error", execErr)
		// Transient infrastructure failure -> check retry policy
		if nextAttempt < job.MaxAttempts {
			w.logger.Warn("infrastructure failure, requeueing job for retry", "job_id", job.ID, "attempt", nextAttempt)
			payload, _ := json.Marshal(JobPayload{JobID: job.ID})
			outbox := &domain.OutboxEvent{
				AggregateID: job.ID,
				EventType:   "job.requeued",
				Payload:     payload,
				Status:      domain.OutboxStatusPending,
				CreatedAt:   time.Now(),
			}
			_ = w.repo.RequeueJob(ctx, job.ID, nextAttempt, outbox)
			_ = w.queue.AckJob(ctx, msg.ID)
			return nil
		}
		w.finalizeFailure(ctx, msg, job, attempt, 1, domain.FailureCategoryInfrastructure, execErr.Error())
		return nil
	}

	// Check if job was cancelled while executing
	currentJob, err := w.repo.GetByID(ctx, job.ID)
	if err == nil && currentJob.Status == domain.StatusCancelled {
		w.logger.Info("job was cancelled during execution", "job_id", job.ID)
		_ = w.queue.AckJob(ctx, msg.ID)
		return nil
	}

	// Determine terminal status based on execution result
	var finalStatus domain.JobStatus
	if result.FailureCategory == domain.FailureCategoryTimeout {
		finalStatus = domain.StatusTimeout
	} else if result.FailureCategory != domain.FailureCategoryNone || result.ExitCode != 0 {
		finalStatus = domain.StatusFailed
	} else {
		finalStatus = domain.StatusCompleted
	}

	// Update Attempt
	_ = w.attemptRepo.UpdateResult(
		ctx, attempt.ID, finalStatus, &result.ExitCode,
		result.Stdout, result.Stderr, result.Truncated,
		result.FailureCategory, result.FailureReason,
	)

	// Update Job
	_ = w.repo.UpdateResult(
		ctx, job.ID, finalStatus, &result.ExitCode,
		result.Stdout, result.Stderr, result.Truncated,
		result.FailureCategory, result.FailureReason,
	)

	// Broadcast terminal event
	w.broadcastEvent(ctx, job.ID, domain.ExecutionStreamEvent{
		Type:      "terminal",
		JobID:     job.ID,
		Status:    finalStatus,
		ExitCode:  &result.ExitCode,
		Truncated: result.Truncated,
		Timestamp: time.Now(),
	})

	// Acknowledge message from queue
	_ = w.queue.AckJob(ctx, msg.ID)

	if finalStatus == domain.StatusCompleted {
		atomic.AddInt64(&w.completedCount, 1)
	} else {
		atomic.AddInt64(&w.failedCount, 1)
	}

	w.logger.Info("job finished", "job_id", job.ID, "status", finalStatus, "exit_code", result.ExitCode, "duration", result.Duration)
	return nil
}

func (w *Worker) finalizeFailure(ctx context.Context, msg queue.Message, job *domain.Job, attempt *domain.JobAttempt, exitCode int, cat domain.FailureCategory, reason string) {
	atomic.AddInt64(&w.failedCount, 1)
	status := domain.StatusFailed

	_ = w.attemptRepo.UpdateResult(ctx, attempt.ID, status, &exitCode, "", reason, false, cat, reason)
	_ = w.repo.UpdateResult(ctx, job.ID, status, &exitCode, "", reason, false, cat, reason)

	w.broadcastEvent(ctx, job.ID, domain.ExecutionStreamEvent{
		Type:      "terminal",
		JobID:     job.ID,
		Status:    status,
		ExitCode:  &exitCode,
		Timestamp: time.Now(),
	})

	_ = w.queue.AckJob(ctx, msg.ID)
}

func (w *Worker) broadcastEvent(ctx context.Context, jobID string, event domain.ExecutionStreamEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	_ = w.queue.PublishEvent(ctx, jobID, data)
}
