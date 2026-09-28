package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/forgequeue/forgequeue/internal/domain"
	"github.com/forgequeue/forgequeue/internal/statemachine"
	"github.com/lib/pq"
)

// PostgresUserRepository implements UserRepository for PostgreSQL.
type PostgresUserRepository struct {
	db *sql.DB
}

func NewPostgresUserRepository(db *sql.DB) *PostgresUserRepository {
	return &PostgresUserRepository{db: db}
}

func (r *PostgresUserRepository) Create(ctx context.Context, u *domain.User) error {
	query := `
		INSERT INTO users (id, username, email, password_hash, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := r.db.ExecContext(ctx, query, u.ID, u.Username, u.Email, u.PasswordHash, u.CreatedAt, u.UpdatedAt)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" { // unique_violation
			return fmt.Errorf("user with this username or email already exists")
		}
		return fmt.Errorf("failed to create user: %w", err)
	}
	return nil
}

func (r *PostgresUserRepository) GetByID(ctx context.Context, id string) (*domain.User, error) {
	query := `SELECT id, username, email, password_hash, created_at, updated_at FROM users WHERE id = $1`
	var u domain.User
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get user by id: %w", err)
	}
	return &u, nil
}

func (r *PostgresUserRepository) GetByUsername(ctx context.Context, username string) (*domain.User, error) {
	query := `SELECT id, username, email, password_hash, created_at, updated_at FROM users WHERE username = $1`
	var u domain.User
	err := r.db.QueryRowContext(ctx, query, username).Scan(
		&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get user by username: %w", err)
	}
	return &u, nil
}

func (r *PostgresUserRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	query := `SELECT id, username, email, password_hash, created_at, updated_at FROM users WHERE email = $1`
	var u domain.User
	err := r.db.QueryRowContext(ctx, query, email).Scan(
		&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get user by email: %w", err)
	}
	return &u, nil
}

// PostgresJobRepository implements JobRepository for PostgreSQL.
type PostgresJobRepository struct {
	db *sql.DB
	sm *statemachine.StateMachine
}

func NewPostgresJobRepository(db *sql.DB) *PostgresJobRepository {
	return &PostgresJobRepository{
		db: db,
		sm: statemachine.New(),
	}
}

func (r *PostgresJobRepository) Create(ctx context.Context, job *domain.Job, outbox *domain.OutboxEvent) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to start tx: %w", err)
	}
	defer tx.Rollback()

	if err := r.CreateInTx(ctx, tx, job, outbox); err != nil {
		return err
	}

	return tx.Commit()
}

func (r *PostgresJobRepository) CreateInTx(ctx context.Context, tx *sql.Tx, job *domain.Job, outbox *domain.OutboxEvent) error {
	jobQuery := `
		INSERT INTO jobs (
			id, user_id, language, source_code, stdin, status, current_attempt, max_attempts,
			cpu_limit, memory_limit_mb, timeout_seconds, max_output_bytes, max_pids,
			stdout, stderr, exit_code, output_truncated, failure_category, failure_reason,
			assigned_worker, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, $10, $11, $12, $13,
			$14, $15, $16, $17, $18, $19,
			$20, $21, $22
		)
	`
	_, err := tx.ExecContext(ctx, jobQuery,
		job.ID, job.UserID, string(job.Language), job.SourceCode, job.Stdin, string(job.Status), job.CurrentAttempt, job.MaxAttempts,
		job.Limits.CPULimit, job.Limits.MemoryLimitMB, job.Limits.TimeoutSeconds, job.Limits.MaxOutputBytes, job.Limits.MaxPIDs,
		job.Stdout, job.Stderr, job.ExitCode, job.OutputTruncated, string(job.FailureCategory), job.FailureReason,
		job.AssignedWorker, job.CreatedAt, job.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert job: %w", err)
	}

	if outbox != nil {
		outboxQuery := `
			INSERT INTO outbox_events (aggregate_id, event_type, payload, status, created_at)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING id
		`
		err = tx.QueryRowContext(ctx, outboxQuery,
			outbox.AggregateID, outbox.EventType, outbox.Payload, string(outbox.Status), outbox.CreatedAt,
		).Scan(&outbox.ID)
		if err != nil {
			return fmt.Errorf("failed to insert outbox event: %w", err)
		}
	}

	return nil
}

func (r *PostgresJobRepository) GetByID(ctx context.Context, id string) (*domain.Job, error) {
	query := `
		SELECT
			id, user_id, language, source_code, stdin, status, current_attempt, max_attempts,
			cpu_limit, memory_limit_mb, timeout_seconds, max_output_bytes, max_pids,
			stdout, stderr, exit_code, output_truncated, failure_category, failure_reason,
			assigned_worker, created_at, started_at, finished_at, updated_at
		FROM jobs
		WHERE id = $1
	`
	var j domain.Job
	var langStr, statusStr, failCatStr string
	var assignedWorker, exitCode sql.NullInt64
	var workerStr sql.NullString
	var startedAt, finishedAt sql.NullTime

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&j.ID, &j.UserID, &langStr, &j.SourceCode, &j.Stdin, &statusStr, &j.CurrentAttempt, &j.MaxAttempts,
		&j.Limits.CPULimit, &j.Limits.MemoryLimitMB, &j.Limits.TimeoutSeconds, &j.Limits.MaxOutputBytes, &j.Limits.MaxPIDs,
		&j.Stdout, &j.Stderr, &exitCode, &j.OutputTruncated, &failCatStr, &j.FailureReason,
		&workerStr, &j.CreatedAt, &startedAt, &finishedAt, &j.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("failed to query job: %w", err)
	}

	j.Language = domain.Language(langStr)
	j.Status = domain.JobStatus(statusStr)
	j.FailureCategory = domain.FailureCategory(failCatStr)
	if exitCode.Valid {
		ec := int(exitCode.Int64)
		j.ExitCode = &ec
	}
	if workerStr.Valid {
		j.AssignedWorker = workerStr.String
	}
	_ = assignedWorker
	if startedAt.Valid {
		j.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		j.FinishedAt = &finishedAt.Time
	}

	return &j, nil
}

func (r *PostgresJobRepository) ListByUserID(ctx context.Context, userID string, limit, offset int) ([]domain.Job, int64, error) {
	countQuery := `SELECT COUNT(*) FROM jobs WHERE user_id = $1`
	var total int64
	if err := r.db.QueryRowContext(ctx, countQuery, userID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count jobs: %w", err)
	}

	query := `
		SELECT
			id, user_id, language, source_code, stdin, status, current_attempt, max_attempts,
			cpu_limit, memory_limit_mb, timeout_seconds, max_output_bytes, max_pids,
			stdout, stderr, exit_code, output_truncated, failure_category, failure_reason,
			assigned_worker, created_at, started_at, finished_at, updated_at
		FROM jobs
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.db.QueryContext(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list jobs: %w", err)
	}
	defer rows.Close()

	var jobs []domain.Job
	for rows.Next() {
		var j domain.Job
		var langStr, statusStr, failCatStr string
		var exitCode sql.NullInt64
		var workerStr sql.NullString
		var startedAt, finishedAt sql.NullTime

		if err := rows.Scan(
			&j.ID, &j.UserID, &langStr, &j.SourceCode, &j.Stdin, &statusStr, &j.CurrentAttempt, &j.MaxAttempts,
			&j.Limits.CPULimit, &j.Limits.MemoryLimitMB, &j.Limits.TimeoutSeconds, &j.Limits.MaxOutputBytes, &j.Limits.MaxPIDs,
			&j.Stdout, &j.Stderr, &exitCode, &j.OutputTruncated, &failCatStr, &j.FailureReason,
			&workerStr, &j.CreatedAt, &startedAt, &finishedAt, &j.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan job row: %w", err)
		}

		j.Language = domain.Language(langStr)
		j.Status = domain.JobStatus(statusStr)
		j.FailureCategory = domain.FailureCategory(failCatStr)
		if exitCode.Valid {
			ec := int(exitCode.Int64)
			j.ExitCode = &ec
		}
		if workerStr.Valid {
			j.AssignedWorker = workerStr.String
		}
		if startedAt.Valid {
			j.StartedAt = &startedAt.Time
		}
		if finishedAt.Valid {
			j.FinishedAt = &finishedAt.Time
		}
		jobs = append(jobs, j)
	}

	return jobs, total, nil
}

func (r *PostgresJobRepository) ClaimJob(ctx context.Context, jobID, workerID string, attemptNum int) (bool, error) {
	// Atomic conditional update: can only claim if QUEUED or already RUNNING for this worker attempt
	query := `
		UPDATE jobs
		SET status = 'RUNNING',
		    assigned_worker = $2,
		    current_attempt = $3,
		    started_at = COALESCE(started_at, NOW()),
		    updated_at = NOW()
		WHERE id = $1 AND (status = 'QUEUED' OR (status = 'RUNNING' AND assigned_worker = $2))
	`
	res, err := r.db.ExecContext(ctx, query, jobID, workerID, attemptNum)
	if err != nil {
		return false, fmt.Errorf("failed to claim job: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (r *PostgresJobRepository) UpdateResult(ctx context.Context, jobID string, status domain.JobStatus, exitCode *int, stdout, stderr string, truncated bool, cat domain.FailureCategory, reason string) error {
	query := `
		UPDATE jobs
		SET status = $2,
		    exit_code = $3,
		    stdout = $4,
		    stderr = $5,
		    output_truncated = $6,
		    failure_category = $7,
		    failure_reason = $8,
		    finished_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1 AND status = 'RUNNING'
	`
	res, err := r.db.ExecContext(ctx, query, jobID, string(status), exitCode, stdout, stderr, truncated, string(cat), reason)
	if err != nil {
		return fmt.Errorf("failed to update job result: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("job %s not in running state or not found", jobID)
	}
	return nil
}

func (r *PostgresJobRepository) RequeueJob(ctx context.Context, jobID string, newAttempt int, outbox *domain.OutboxEvent) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
		UPDATE jobs
		SET status = 'QUEUED',
		    current_attempt = $2,
		    assigned_worker = NULL,
		    started_at = NULL,
		    updated_at = NOW()
		WHERE id = $1 AND status = 'RUNNING'
	`
	res, err := tx.ExecContext(ctx, query, jobID, newAttempt)
	if err != nil {
		return fmt.Errorf("failed to requeue job: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("job %s not running", jobID)
	}

	if outbox != nil {
		outboxQuery := `
			INSERT INTO outbox_events (aggregate_id, event_type, payload, status, created_at)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING id
		`
		err = tx.QueryRowContext(ctx, outboxQuery,
			outbox.AggregateID, outbox.EventType, outbox.Payload, string(outbox.Status), outbox.CreatedAt,
		).Scan(&outbox.ID)
		if err != nil {
			return fmt.Errorf("failed to insert outbox event on requeue: %w", err)
		}
	}

	return tx.Commit()
}

func (r *PostgresJobRepository) CancelJob(ctx context.Context, jobID, userID string) (*domain.Job, error) {
	job, err := r.GetByID(ctx, jobID)
	if err != nil {
		return nil, err
	}
	if job.UserID != userID {
		return nil, domain.ErrForbidden
	}

	if err := r.sm.Transition(job.Status, domain.StatusCancelled); err != nil {
		return nil, err
	}

	query := `
		UPDATE jobs
		SET status = 'CANCELLED',
		    finished_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND status IN ('QUEUED', 'RUNNING')
	`
	res, err := r.db.ExecContext(ctx, query, jobID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to cancel job: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if rows == 0 {
		return nil, domain.ErrJobAlreadyTerminal
	}

	job.Status = domain.StatusCancelled
	now := time.Now()
	job.FinishedAt = &now
	return job, nil
}

func (r *PostgresJobRepository) GetStaleRunningJobs(ctx context.Context, staleCutoff time.Time) ([]domain.Job, error) {
	query := `
		SELECT
			id, user_id, language, source_code, stdin, status, current_attempt, max_attempts,
			cpu_limit, memory_limit_mb, timeout_seconds, max_output_bytes, max_pids,
			stdout, stderr, exit_code, output_truncated, failure_category, failure_reason,
			assigned_worker, created_at, started_at, finished_at, updated_at
		FROM jobs
		WHERE status = 'RUNNING' AND (
			started_at < $1 OR
			assigned_worker IN (SELECT id FROM workers WHERE status = 'OFFLINE')
		)
	`
	rows, err := r.db.QueryContext(ctx, query, staleCutoff)
	if err != nil {
		return nil, fmt.Errorf("failed to get stale running jobs: %w", err)
	}
	defer rows.Close()

	var jobs []domain.Job
	for rows.Next() {
		var j domain.Job
		var langStr, statusStr, failCatStr string
		var exitCode sql.NullInt64
		var workerStr sql.NullString
		var startedAt, finishedAt sql.NullTime

		if err := rows.Scan(
			&j.ID, &j.UserID, &langStr, &j.SourceCode, &j.Stdin, &statusStr, &j.CurrentAttempt, &j.MaxAttempts,
			&j.Limits.CPULimit, &j.Limits.MemoryLimitMB, &j.Limits.TimeoutSeconds, &j.Limits.MaxOutputBytes, &j.Limits.MaxPIDs,
			&j.Stdout, &j.Stderr, &exitCode, &j.OutputTruncated, &failCatStr, &j.FailureReason,
			&workerStr, &j.CreatedAt, &startedAt, &finishedAt, &j.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan stale job: %w", err)
		}

		j.Language = domain.Language(langStr)
		j.Status = domain.JobStatus(statusStr)
		j.FailureCategory = domain.FailureCategory(failCatStr)
		if exitCode.Valid {
			ec := int(exitCode.Int64)
			j.ExitCode = &ec
		}
		if workerStr.Valid {
			j.AssignedWorker = workerStr.String
		}
		if startedAt.Valid {
			j.StartedAt = &startedAt.Time
		}
		if finishedAt.Valid {
			j.FinishedAt = &finishedAt.Time
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}

// PostgresAttemptRepository records attempts.
type PostgresAttemptRepository struct {
	db *sql.DB
}

func NewPostgresAttemptRepository(db *sql.DB) *PostgresAttemptRepository {
	return &PostgresAttemptRepository{db: db}
}

func (r *PostgresAttemptRepository) Create(ctx context.Context, a *domain.JobAttempt) error {
	query := `
		INSERT INTO job_attempts (
			id, job_id, attempt_number, worker_id, status, exit_code, stdout, stderr,
			output_truncated, failure_category, failure_reason, started_at, finished_at, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, $10, $11, $12, $13, $14
		)
	`
	_, err := r.db.ExecContext(ctx, query,
		a.ID, a.JobID, a.AttemptNumber, a.WorkerID, string(a.Status), a.ExitCode, a.Stdout, a.Stderr,
		a.OutputTruncated, string(a.FailureCategory), a.FailureReason, a.StartedAt, a.FinishedAt, a.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create job attempt: %w", err)
	}
	return nil
}

func (r *PostgresAttemptRepository) UpdateResult(ctx context.Context, attemptID string, status domain.JobStatus, exitCode *int, stdout, stderr string, truncated bool, cat domain.FailureCategory, reason string) error {
	query := `
		UPDATE job_attempts
		SET status = $2,
		    exit_code = $3,
		    stdout = $4,
		    stderr = $5,
		    output_truncated = $6,
		    failure_category = $7,
		    failure_reason = $8,
		    finished_at = NOW()
		WHERE id = $1
	`
	_, err := r.db.ExecContext(ctx, query, attemptID, string(status), exitCode, stdout, stderr, truncated, string(cat), reason)
	if err != nil {
		return fmt.Errorf("failed to update attempt: %w", err)
	}
	return nil
}

func (r *PostgresAttemptRepository) ListByJobID(ctx context.Context, jobID string) ([]domain.JobAttempt, error) {
	query := `
		SELECT
			id, job_id, attempt_number, worker_id, status, exit_code, stdout, stderr,
			output_truncated, failure_category, failure_reason, started_at, finished_at, created_at
		FROM job_attempts
		WHERE job_id = $1
		ORDER BY attempt_number ASC
	`
	rows, err := r.db.QueryContext(ctx, query, jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to list attempts: %w", err)
	}
	defer rows.Close()

	var attempts []domain.JobAttempt
	for rows.Next() {
		var a domain.JobAttempt
		var statusStr, failCatStr string
		var exitCode sql.NullInt64
		var finishedAt sql.NullTime

		if err := rows.Scan(
			&a.ID, &a.JobID, &a.AttemptNumber, &a.WorkerID, &statusStr, &exitCode, &a.Stdout, &a.Stderr,
			&a.OutputTruncated, &failCatStr, &a.FailureReason, &a.StartedAt, &finishedAt, &a.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan attempt: %w", err)
		}

		a.Status = domain.JobStatus(statusStr)
		a.FailureCategory = domain.FailureCategory(failCatStr)
		if exitCode.Valid {
			ec := int(exitCode.Int64)
			a.ExitCode = &ec
		}
		if finishedAt.Valid {
			a.FinishedAt = &finishedAt.Time
		}
		attempts = append(attempts, a)
	}
	return attempts, nil
}

// PostgresOutboxRepository handles transactional outbox events.
type PostgresOutboxRepository struct {
	db *sql.DB
}

func NewPostgresOutboxRepository(db *sql.DB) *PostgresOutboxRepository {
	return &PostgresOutboxRepository{db: db}
}

func (r *PostgresOutboxRepository) Create(ctx context.Context, event *domain.OutboxEvent) error {
	query := `
		INSERT INTO outbox_events (aggregate_id, event_type, payload, status, created_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`
	return r.db.QueryRowContext(ctx, query, event.AggregateID, event.EventType, event.Payload, string(event.Status), event.CreatedAt).Scan(&event.ID)
}

func (r *PostgresOutboxRepository) CreateInTx(ctx context.Context, tx *sql.Tx, event *domain.OutboxEvent) error {
	query := `
		INSERT INTO outbox_events (aggregate_id, event_type, payload, status, created_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`
	return tx.QueryRowContext(ctx, query, event.AggregateID, event.EventType, event.Payload, string(event.Status), event.CreatedAt).Scan(&event.ID)
}

func (r *PostgresOutboxRepository) FetchPending(ctx context.Context, limit int) ([]domain.OutboxEvent, error) {
	query := `
		SELECT id, aggregate_id, event_type, payload, status, created_at, retry_count, last_error
		FROM outbox_events
		WHERE status = 'PENDING'
		ORDER BY id ASC
		LIMIT $1
	`
	rows, err := r.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pending outbox events: %w", err)
	}
	defer rows.Close()

	var events []domain.OutboxEvent
	for rows.Next() {
		var e domain.OutboxEvent
		var statusStr string
		if err := rows.Scan(&e.ID, &e.AggregateID, &e.EventType, &e.Payload, &statusStr, &e.CreatedAt, &e.RetryCount, &e.LastError); err != nil {
			return nil, fmt.Errorf("failed to scan outbox event: %w", err)
		}
		e.Status = domain.OutboxStatus(statusStr)
		events = append(events, e)
	}
	return events, nil
}

func (r *PostgresOutboxRepository) MarkPublished(ctx context.Context, id int64) error {
	query := `UPDATE outbox_events SET status = 'PUBLISHED', published_at = NOW() WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

func (r *PostgresOutboxRepository) MarkFailed(ctx context.Context, id int64, reason string) error {
	query := `UPDATE outbox_events SET status = 'FAILED', retry_count = retry_count + 1, last_error = $2 WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id, reason)
	return err
}

func (r *PostgresOutboxRepository) FetchStalePending(ctx context.Context, cutoff time.Time, limit int) ([]domain.OutboxEvent, error) {
	query := `
		SELECT id, aggregate_id, event_type, payload, status, created_at, retry_count, last_error
		FROM outbox_events
		WHERE (status = 'PENDING' OR status = 'FAILED') AND created_at < $1 AND retry_count < 10
		ORDER BY id ASC
		LIMIT $2
	`
	rows, err := r.db.QueryContext(ctx, query, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch stale outbox events: %w", err)
	}
	defer rows.Close()

	var events []domain.OutboxEvent
	for rows.Next() {
		var e domain.OutboxEvent
		var statusStr string
		if err := rows.Scan(&e.ID, &e.AggregateID, &e.EventType, &e.Payload, &statusStr, &e.CreatedAt, &e.RetryCount, &e.LastError); err != nil {
			return nil, fmt.Errorf("failed to scan stale outbox event: %w", err)
		}
		e.Status = domain.OutboxStatus(statusStr)
		events = append(events, e)
	}
	return events, nil
}

// PostgresWorkerRepository handles worker heartbeats.
type PostgresWorkerRepository struct {
	db *sql.DB
}

func NewPostgresWorkerRepository(db *sql.DB) *PostgresWorkerRepository {
	return &PostgresWorkerRepository{db: db}
}

func (r *PostgresWorkerRepository) UpsertHeartbeat(ctx context.Context, w *domain.Worker) error {
	query := `
		INSERT INTO workers (id, hostname, concurrency, active_jobs, completed_jobs, failed_jobs, status, last_heartbeat, started_at, version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO UPDATE SET
			hostname = EXCLUDED.hostname,
			concurrency = EXCLUDED.concurrency,
			active_jobs = EXCLUDED.active_jobs,
			completed_jobs = EXCLUDED.completed_jobs,
			failed_jobs = EXCLUDED.failed_jobs,
			status = EXCLUDED.status,
			last_heartbeat = EXCLUDED.last_heartbeat,
			version = EXCLUDED.version
	`
	_, err := r.db.ExecContext(ctx, query,
		w.ID, w.Hostname, w.Concurrency, w.ActiveJobs, w.CompletedJobs, w.FailedJobs,
		string(w.Status), w.LastHeartbeat, w.StartedAt, w.Version,
	)
	return err
}

func (r *PostgresWorkerRepository) List(ctx context.Context) ([]domain.Worker, error) {
	query := `SELECT id, hostname, concurrency, active_jobs, completed_jobs, failed_jobs, status, last_heartbeat, started_at, version FROM workers ORDER BY id ASC`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list workers: %w", err)
	}
	defer rows.Close()

	var workers []domain.Worker
	for rows.Next() {
		var w domain.Worker
		var statusStr string
		if err := rows.Scan(&w.ID, &w.Hostname, &w.Concurrency, &w.ActiveJobs, &w.CompletedJobs, &w.FailedJobs, &statusStr, &w.LastHeartbeat, &w.StartedAt, &w.Version); err != nil {
			return nil, fmt.Errorf("failed to scan worker: %w", err)
		}
		w.Status = domain.WorkerStatus(statusStr)
		workers = append(workers, w)
	}
	return workers, nil
}

func (r *PostgresWorkerRepository) GetByID(ctx context.Context, id string) (*domain.Worker, error) {
	query := `SELECT id, hostname, concurrency, active_jobs, completed_jobs, failed_jobs, status, last_heartbeat, started_at, version FROM workers WHERE id = $1`
	var w domain.Worker
	var statusStr string
	err := r.db.QueryRowContext(ctx, query, id).Scan(&w.ID, &w.Hostname, &w.Concurrency, &w.ActiveJobs, &w.CompletedJobs, &w.FailedJobs, &statusStr, &w.LastHeartbeat, &w.StartedAt, &w.Version)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get worker: %w", err)
	}
	w.Status = domain.WorkerStatus(statusStr)
	return &w, nil
}

func (r *PostgresWorkerRepository) IncrementCounters(ctx context.Context, workerID string, completedDelta, failedDelta, activeDelta int) error {
	query := `
		UPDATE workers
		SET completed_jobs = completed_jobs + $2,
		    failed_jobs = failed_jobs + $3,
		    active_jobs = GREATEST(0, active_jobs + $4),
		    last_heartbeat = NOW()
		WHERE id = $1
	`
	_, err := r.db.ExecContext(ctx, query, workerID, completedDelta, failedDelta, activeDelta)
	return err
}

func (r *PostgresWorkerRepository) MarkOffline(ctx context.Context, workerID string) error {
	query := `UPDATE workers SET status = 'OFFLINE' WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, workerID)
	return err
}

// PostgresIdempotencyRepository stores idempotency keys.
type PostgresIdempotencyRepository struct {
	db *sql.DB
}

func NewPostgresIdempotencyRepository(db *sql.DB) *PostgresIdempotencyRepository {
	return &PostgresIdempotencyRepository{db: db}
}

func (r *PostgresIdempotencyRepository) Get(ctx context.Context, userID, key string) (*domain.IdempotencyKey, error) {
	query := `SELECT key, user_id, job_id, created_at, expires_at FROM idempotency_keys WHERE user_id = $1 AND key = $2 AND expires_at > NOW()`
	var k domain.IdempotencyKey
	err := r.db.QueryRowContext(ctx, query, userID, key).Scan(&k.Key, &k.UserID, &k.JobID, &k.CreatedAt, &k.ExpiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get idempotency key: %w", err)
	}
	return &k, nil
}

func (r *PostgresIdempotencyRepository) Store(ctx context.Context, k *domain.IdempotencyKey) error {
	query := `
		INSERT INTO idempotency_keys (key, user_id, job_id, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id, key) DO NOTHING
	`
	_, err := r.db.ExecContext(ctx, query, k.Key, k.UserID, k.JobID, k.CreatedAt, k.ExpiresAt)
	return err
}
