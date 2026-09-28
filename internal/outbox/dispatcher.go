package outbox

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/forgequeue/forgequeue/internal/repository"
)

// Publisher defines an interface to publish messages to a message bus (Redis Streams).
type Publisher interface {
	PublishJob(ctx context.Context, jobID string, payload []byte) error
}

// Dispatcher polls for unpublished outbox events and pushes them to Redis Streams.
type Dispatcher struct {
	repo        repository.OutboxRepository
	publisher   Publisher
	batchSize   int
	pollInterval time.Duration
	logger      *slog.Logger
}

// NewDispatcher creates a new outbox Dispatcher.
func NewDispatcher(repo repository.OutboxRepository, publisher Publisher, batchSize int, pollInterval time.Duration, logger *slog.Logger) *Dispatcher {
	if batchSize <= 0 {
		batchSize = 50
	}
	if pollInterval <= 0 {
		pollInterval = 100 * time.Millisecond
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Dispatcher{
		repo:         repo,
		publisher:    publisher,
		batchSize:    batchSize,
		pollInterval: pollInterval,
		logger:       logger,
	}
}

// DispatchOnce processes a single batch of pending outbox events.
func (d *Dispatcher) DispatchOnce(ctx context.Context) (int, error) {
	events, err := d.repo.FetchPending(ctx, d.batchSize)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch pending outbox events: %w", err)
	}

	processed := 0
	for _, event := range events {
		err := d.publisher.PublishJob(ctx, event.AggregateID, event.Payload)
		if err != nil {
			d.logger.Error("failed to publish outbox event to redis", "event_id", event.ID, "job_id", event.AggregateID, "error", err)
			_ = d.repo.MarkFailed(ctx, event.ID, err.Error())
			continue
		}

		if err := d.repo.MarkPublished(ctx, event.ID); err != nil {
			d.logger.Error("failed to mark outbox event published", "event_id", event.ID, "error", err)
			continue
		}
		processed++
	}

	return processed, nil
}

// Start runs the dispatch polling loop until context cancellation.
func (d *Dispatcher) Start(ctx context.Context) {
	ticker := time.NewTicker(d.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			d.logger.Info("outbox dispatcher stopped")
			return
		case <-ticker.C:
			_, _ = d.DispatchOnce(ctx)
		}
	}
}

// Reconciler sweeps stale or un-published outbox records periodically.
type Reconciler struct {
	repo         repository.OutboxRepository
	publisher    Publisher
	staleCutoff  time.Duration
	sweepInterval time.Duration
	batchSize    int
	logger       *slog.Logger
}

// NewReconciler creates an outbox Reconciler.
func NewReconciler(repo repository.OutboxRepository, publisher Publisher, staleCutoff, sweepInterval time.Duration, batchSize int, logger *slog.Logger) *Reconciler {
	if staleCutoff <= 0 {
		staleCutoff = 10 * time.Second
	}
	if sweepInterval <= 0 {
		sweepInterval = 5 * time.Second
	}
	if batchSize <= 0 {
		batchSize = 50
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Reconciler{
		repo:          repo,
		publisher:     publisher,
		staleCutoff:   staleCutoff,
		sweepInterval: sweepInterval,
		batchSize:     batchSize,
		logger:        logger,
	}
}

// ReconcileOnce sweeps stale outbox records and retries publishing them.
func (r *Reconciler) ReconcileOnce(ctx context.Context) (int, error) {
	cutoff := time.Now().Add(-r.staleCutoff)
	events, err := r.repo.FetchStalePending(ctx, cutoff, r.batchSize)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch stale outbox events: %w", err)
	}

	recovered := 0
	for _, event := range events {
		r.logger.Warn("reconciling stale outbox event", "event_id", event.ID, "job_id", event.AggregateID, "retry_count", event.RetryCount)
		err := r.publisher.PublishJob(ctx, event.AggregateID, event.Payload)
		if err != nil {
			_ = r.repo.MarkFailed(ctx, event.ID, fmt.Sprintf("reconcile error: %v", err))
			continue
		}

		if err := r.repo.MarkPublished(ctx, event.ID); err == nil {
			recovered++
		}
	}

	return recovered, nil
}

// Start runs the reconciler loop.
func (r *Reconciler) Start(ctx context.Context) {
	ticker := time.NewTicker(r.sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("outbox reconciler stopped")
			return
		case <-ticker.C:
			_, _ = r.ReconcileOnce(ctx)
		}
	}
}
