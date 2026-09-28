package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/forgequeue/forgequeue/internal/domain"
	"github.com/forgequeue/forgequeue/internal/statemachine"
)

// MemoryUserRepository is an in-memory thread-safe UserRepository.
type MemoryUserRepository struct {
	mu    sync.RWMutex
	users map[string]*domain.User
}

func NewMemoryUserRepository() *MemoryUserRepository {
	return &MemoryUserRepository{
		users: make(map[string]*domain.User),
	}
}

func (r *MemoryUserRepository) Create(ctx context.Context, u *domain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, existing := range r.users {
		if existing.Username == u.Username || existing.Email == u.Email {
			return fmt.Errorf("user with this username or email already exists")
		}
	}
	cp := *u
	r.users[u.ID] = &cp
	return nil
}

func (r *MemoryUserRepository) GetByID(ctx context.Context, id string) (*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	u, ok := r.users[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *u
	return &cp, nil
}

func (r *MemoryUserRepository) GetByUsername(ctx context.Context, username string) (*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, u := range r.users {
		if u.Username == username {
			cp := *u
			return &cp, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *MemoryUserRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, u := range r.users {
		if u.Email == email {
			cp := *u
			return &cp, nil
		}
	}
	return nil, domain.ErrNotFound
}

// MemoryJobRepository is an in-memory thread-safe JobRepository.
type MemoryJobRepository struct {
	mu     sync.RWMutex
	jobs   map[string]*domain.Job
	outbox OutboxRepository
	sm     *statemachine.StateMachine
}

func NewMemoryJobRepository(outbox OutboxRepository) *MemoryJobRepository {
	return &MemoryJobRepository{
		jobs:   make(map[string]*domain.Job),
		outbox: outbox,
		sm:     statemachine.New(),
	}
}

func (r *MemoryJobRepository) Create(ctx context.Context, job *domain.Job, outbox *domain.OutboxEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	cp := *job
	r.jobs[job.ID] = &cp

	if outbox != nil && r.outbox != nil {
		if err := r.outbox.Create(ctx, outbox); err != nil {
			return err
		}
	}
	return nil
}

func (r *MemoryJobRepository) CreateInTx(ctx context.Context, tx *sql.Tx, job *domain.Job, outbox *domain.OutboxEvent) error {
	return r.Create(ctx, job, outbox)
}

func (r *MemoryJobRepository) GetByID(ctx context.Context, id string) (*domain.Job, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	j, ok := r.jobs[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *j
	return &cp, nil
}

func (r *MemoryJobRepository) ListByUserID(ctx context.Context, userID string, limit, offset int) ([]domain.Job, int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var userJobs []domain.Job
	for _, j := range r.jobs {
		if j.UserID == userID {
			userJobs = append(userJobs, *j)
		}
	}

	total := int64(len(userJobs))
	if offset >= len(userJobs) {
		return []domain.Job{}, total, nil
	}
	end := offset + limit
	if end > len(userJobs) {
		end = len(userJobs)
	}

	return userJobs[offset:end], total, nil
}

func (r *MemoryJobRepository) ClaimJob(ctx context.Context, jobID, workerID string, attemptNum int) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	j, ok := r.jobs[jobID]
	if !ok {
		return false, domain.ErrNotFound
	}

	if j.Status == domain.StatusQueued || (j.Status == domain.StatusRunning && j.AssignedWorker == workerID) {
		j.Status = domain.StatusRunning
		j.AssignedWorker = workerID
		j.CurrentAttempt = attemptNum
		now := time.Now()
		if j.StartedAt == nil {
			j.StartedAt = &now
		}
		j.UpdatedAt = now
		return true, nil
	}

	return false, nil
}

func (r *MemoryJobRepository) UpdateResult(ctx context.Context, jobID string, status domain.JobStatus, exitCode *int, stdout, stderr string, truncated bool, cat domain.FailureCategory, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	j, ok := r.jobs[jobID]
	if !ok {
		return domain.ErrNotFound
	}
	if j.Status != domain.StatusRunning {
		return fmt.Errorf("job %s not in running state", jobID)
	}

	j.Status = status
	j.ExitCode = exitCode
	j.Stdout = stdout
	j.Stderr = stderr
	j.OutputTruncated = truncated
	j.FailureCategory = cat
	j.FailureReason = reason
	now := time.Now()
	j.FinishedAt = &now
	j.UpdatedAt = now

	return nil
}

func (r *MemoryJobRepository) RequeueJob(ctx context.Context, jobID string, newAttempt int, outbox *domain.OutboxEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	j, ok := r.jobs[jobID]
	if !ok {
		return domain.ErrNotFound
	}

	j.Status = domain.StatusQueued
	j.CurrentAttempt = newAttempt
	j.AssignedWorker = ""
	j.StartedAt = nil
	j.UpdatedAt = time.Now()

	if outbox != nil && r.outbox != nil {
		return r.outbox.Create(ctx, outbox)
	}
	return nil
}

func (r *MemoryJobRepository) CancelJob(ctx context.Context, jobID, userID string) (*domain.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	j, ok := r.jobs[jobID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	if j.UserID != userID {
		return nil, domain.ErrForbidden
	}

	if err := r.sm.Transition(j.Status, domain.StatusCancelled); err != nil {
		return nil, err
	}

	j.Status = domain.StatusCancelled
	now := time.Now()
	j.FinishedAt = &now
	j.UpdatedAt = now

	cp := *j
	return &cp, nil
}

func (r *MemoryJobRepository) GetStaleRunningJobs(ctx context.Context, staleCutoff time.Time) ([]domain.Job, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var stale []domain.Job
	for _, j := range r.jobs {
		if j.Status == domain.StatusRunning {
			if j.StartedAt != nil && j.StartedAt.Before(staleCutoff) {
				stale = append(stale, *j)
			}
		}
	}
	return stale, nil
}

// MemoryAttemptRepository stores attempts in memory.
type MemoryAttemptRepository struct {
	mu       sync.RWMutex
	attempts map[string][]domain.JobAttempt
}

func NewMemoryAttemptRepository() *MemoryAttemptRepository {
	return &MemoryAttemptRepository{
		attempts: make(map[string][]domain.JobAttempt),
	}
}

func (r *MemoryAttemptRepository) Create(ctx context.Context, a *domain.JobAttempt) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	list := r.attempts[a.JobID]
	list = append(list, *a)
	r.attempts[a.JobID] = list
	return nil
}

func (r *MemoryAttemptRepository) UpdateResult(ctx context.Context, attemptID string, status domain.JobStatus, exitCode *int, stdout, stderr string, truncated bool, cat domain.FailureCategory, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for jobID, list := range r.attempts {
		for i, a := range list {
			if a.ID == attemptID {
				now := time.Now()
				list[i].Status = status
				list[i].ExitCode = exitCode
				list[i].Stdout = stdout
				list[i].Stderr = stderr
				list[i].OutputTruncated = truncated
				list[i].FailureCategory = cat
				list[i].FailureReason = reason
				list[i].FinishedAt = &now
				r.attempts[jobID] = list
				return nil
			}
		}
	}
	return domain.ErrNotFound
}

func (r *MemoryAttemptRepository) ListByJobID(ctx context.Context, jobID string) ([]domain.JobAttempt, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	list, ok := r.attempts[jobID]
	if !ok {
		return []domain.JobAttempt{}, nil
	}
	cp := make([]domain.JobAttempt, len(list))
	copy(cp, list)
	return cp, nil
}

// MemoryOutboxRepository stores outbox events in memory.
type MemoryOutboxRepository struct {
	mu     sync.RWMutex
	events []*domain.OutboxEvent
	nextID int64
}

func NewMemoryOutboxRepository() *MemoryOutboxRepository {
	return &MemoryOutboxRepository{
		nextID: 1,
	}
}

func (r *MemoryOutboxRepository) Create(ctx context.Context, event *domain.OutboxEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	event.ID = r.nextID
	r.nextID++
	cp := *event
	r.events = append(r.events, &cp)
	return nil
}

func (r *MemoryOutboxRepository) CreateInTx(ctx context.Context, tx *sql.Tx, event *domain.OutboxEvent) error {
	return r.Create(ctx, event)
}

func (r *MemoryOutboxRepository) FetchPending(ctx context.Context, limit int) ([]domain.OutboxEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var pending []domain.OutboxEvent
	for _, e := range r.events {
		if e.Status == domain.OutboxStatusPending {
			pending = append(pending, *e)
			if len(pending) >= limit {
				break
			}
		}
	}
	return pending, nil
}

func (r *MemoryOutboxRepository) MarkPublished(ctx context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, e := range r.events {
		if e.ID == id {
			now := time.Now()
			e.Status = domain.OutboxStatusPublished
			e.PublishedAt = &now
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *MemoryOutboxRepository) MarkFailed(ctx context.Context, id int64, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, e := range r.events {
		if e.ID == id {
			e.Status = domain.OutboxStatusFailed
			e.RetryCount++
			e.LastError = reason
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *MemoryOutboxRepository) FetchStalePending(ctx context.Context, cutoff time.Time, limit int) ([]domain.OutboxEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var stale []domain.OutboxEvent
	for _, e := range r.events {
		if (e.Status == domain.OutboxStatusPending || e.Status == domain.OutboxStatusFailed) && e.CreatedAt.Before(cutoff) && e.RetryCount < 10 {
			stale = append(stale, *e)
			if len(stale) >= limit {
				break
			}
		}
	}
	return stale, nil
}

// MemoryWorkerRepository stores worker heartbeats.
type MemoryWorkerRepository struct {
	mu      sync.RWMutex
	workers map[string]*domain.Worker
}

func NewMemoryWorkerRepository() *MemoryWorkerRepository {
	return &MemoryWorkerRepository{
		workers: make(map[string]*domain.Worker),
	}
}

func (r *MemoryWorkerRepository) UpsertHeartbeat(ctx context.Context, w *domain.Worker) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	cp := *w
	r.workers[w.ID] = &cp
	return nil
}

func (r *MemoryWorkerRepository) List(ctx context.Context) ([]domain.Worker, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var list []domain.Worker
	for _, w := range r.workers {
		list = append(list, *w)
	}
	return list, nil
}

func (r *MemoryWorkerRepository) GetByID(ctx context.Context, id string) (*domain.Worker, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	w, ok := r.workers[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *w
	return &cp, nil
}

func (r *MemoryWorkerRepository) IncrementCounters(ctx context.Context, workerID string, completedDelta, failedDelta, activeDelta int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	w, ok := r.workers[workerID]
	if !ok {
		return domain.ErrNotFound
	}
	w.CompletedJobs += int64(completedDelta)
	w.FailedJobs += int64(failedDelta)
	w.ActiveJobs += activeDelta
	if w.ActiveJobs < 0 {
		w.ActiveJobs = 0
	}
	w.LastHeartbeat = time.Now()
	return nil
}

func (r *MemoryWorkerRepository) MarkOffline(ctx context.Context, workerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	w, ok := r.workers[workerID]
	if !ok {
		return domain.ErrNotFound
	}
	w.Status = domain.WorkerStatusOffline
	return nil
}

// MemoryIdempotencyRepository stores idempotency keys in memory.
type MemoryIdempotencyRepository struct {
	mu   sync.RWMutex
	keys map[string]*domain.IdempotencyKey
}

func NewMemoryIdempotencyRepository() *MemoryIdempotencyRepository {
	return &MemoryIdempotencyRepository{
		keys: make(map[string]*domain.IdempotencyKey),
	}
}

func (r *MemoryIdempotencyRepository) Get(ctx context.Context, userID, key string) (*domain.IdempotencyKey, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	compoundKey := fmt.Sprintf("%s:%s", userID, key)
	k, ok := r.keys[compoundKey]
	if !ok {
		return nil, domain.ErrNotFound
	}
	if time.Now().After(k.ExpiresAt) {
		return nil, domain.ErrNotFound
	}
	cp := *k
	return &cp, nil
}

func (r *MemoryIdempotencyRepository) Store(ctx context.Context, k *domain.IdempotencyKey) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	compoundKey := fmt.Sprintf("%s:%s", k.UserID, k.Key)
	if _, exists := r.keys[compoundKey]; exists {
		return nil // idempotent no-op
	}
	cp := *k
	r.keys[compoundKey] = &cp
	return nil
}
