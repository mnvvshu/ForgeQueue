package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/forgequeue/forgequeue/internal/domain"
)

// UserRepository handles user persistence.
type UserRepository interface {
	Create(ctx context.Context, user *domain.User) error
	GetByID(ctx context.Context, id string) (*domain.User, error)
	GetByUsername(ctx context.Context, username string) (*domain.User, error)
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
}

// JobRepository handles job creation and state queries.
type JobRepository interface {
	Create(ctx context.Context, job *domain.Job, outbox *domain.OutboxEvent) error
	CreateInTx(ctx context.Context, tx *sql.Tx, job *domain.Job, outbox *domain.OutboxEvent) error
	GetByID(ctx context.Context, id string) (*domain.Job, error)
	ListByUserID(ctx context.Context, userID string, limit, offset int) ([]domain.Job, int64, error)
	ClaimJob(ctx context.Context, jobID, workerID string, attemptNum int) (bool, error)
	UpdateResult(ctx context.Context, jobID string, status domain.JobStatus, exitCode *int, stdout, stderr string, truncated bool, cat domain.FailureCategory, reason string) error
	RequeueJob(ctx context.Context, jobID string, newAttempt int, outbox *domain.OutboxEvent) error
	CancelJob(ctx context.Context, jobID, userID string) (*domain.Job, error)
	GetStaleRunningJobs(ctx context.Context, staleCutoff time.Time) ([]domain.Job, error)
}

// AttemptRepository handles recording individual attempts.
type AttemptRepository interface {
	Create(ctx context.Context, attempt *domain.JobAttempt) error
	UpdateResult(ctx context.Context, attemptID string, status domain.JobStatus, exitCode *int, stdout, stderr string, truncated bool, cat domain.FailureCategory, reason string) error
	ListByJobID(ctx context.Context, jobID string) ([]domain.JobAttempt, error)
}

// OutboxRepository handles transactional outbox events.
type OutboxRepository interface {
	Create(ctx context.Context, event *domain.OutboxEvent) error
	CreateInTx(ctx context.Context, tx *sql.Tx, event *domain.OutboxEvent) error
	FetchPending(ctx context.Context, limit int) ([]domain.OutboxEvent, error)
	MarkPublished(ctx context.Context, id int64) error
	MarkFailed(ctx context.Context, id int64, reason string) error
	FetchStalePending(ctx context.Context, cutoff time.Time, limit int) ([]domain.OutboxEvent, error)
}

// WorkerRepository handles worker registration and heartbeat monitoring.
type WorkerRepository interface {
	UpsertHeartbeat(ctx context.Context, worker *domain.Worker) error
	List(ctx context.Context) ([]domain.Worker, error)
	GetByID(ctx context.Context, id string) (*domain.Worker, error)
	IncrementCounters(ctx context.Context, workerID string, completedDelta, failedDelta, activeDelta int) error
	MarkOffline(ctx context.Context, workerID string) error
}

// IdempotencyRepository manages idempotency tokens.
type IdempotencyRepository interface {
	Get(ctx context.Context, userID, key string) (*domain.IdempotencyKey, error)
	Store(ctx context.Context, key *domain.IdempotencyKey) error
}
