package outbox_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/forgequeue/forgequeue/internal/domain"
	"github.com/forgequeue/forgequeue/internal/outbox"
	"github.com/forgequeue/forgequeue/internal/repository"
)

type mockPublisher struct {
	mu        sync.Mutex
	published map[string][]byte
	failIDs   map[string]bool
}

func newMockPublisher() *mockPublisher {
	return &mockPublisher{
		published: make(map[string][]byte),
		failIDs:   make(map[string]bool),
	}
}

func (m *mockPublisher) PublishJob(ctx context.Context, jobID string, payload []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.failIDs[jobID] {
		return errors.New("simulated redis network failure")
	}
	m.published[jobID] = payload
	return nil
}

func TestDispatcher_PublishSuccess(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewMemoryOutboxRepository()
	pub := newMockPublisher()

	// Seed pending outbox event
	event := &domain.OutboxEvent{
		AggregateID: "job-outbox-1",
		EventType:   "job.queued",
		Payload:     []byte(`{"job_id":"job-outbox-1"}`),
		Status:      domain.OutboxStatusPending,
		CreatedAt:   time.Now(),
	}
	if err := repo.Create(ctx, event); err != nil {
		t.Fatalf("failed to create event: %v", err)
	}

	dispatcher := outbox.NewDispatcher(repo, pub, 10, 10*time.Millisecond, nil)
	count, err := dispatcher.DispatchOnce(ctx)
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 event dispatched, got %d", count)
	}

	// Verify publisher received it
	pub.mu.Lock()
	_, found := pub.published["job-outbox-1"]
	pub.mu.Unlock()
	if !found {
		t.Fatalf("expected job-outbox-1 to be published to mock publisher")
	}

	// Verify no pending events remain
	pending, err := repo.FetchPending(ctx, 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("expected 0 pending events after publish, got %d", len(pending))
	}
}

func TestDispatcher_PublisherFailureAndReconciliation(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewMemoryOutboxRepository()
	pub := newMockPublisher()

	// Simulate redis failure for this job
	pub.failIDs["job-fail-1"] = true

	event := &domain.OutboxEvent{
		AggregateID: "job-fail-1",
		EventType:   "job.queued",
		Payload:     []byte(`{"job_id":"job-fail-1"}`),
		Status:      domain.OutboxStatusPending,
		CreatedAt:   time.Now().Add(-15 * time.Second), // older than 10s cutoff
	}
	if err := repo.Create(ctx, event); err != nil {
		t.Fatalf("failed to create event: %v", err)
	}

	dispatcher := outbox.NewDispatcher(repo, pub, 10, 10*time.Millisecond, nil)
	count, err := dispatcher.DispatchOnce(ctx)
	if err != nil {
		t.Fatalf("unexpected error from dispatcher: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 successful publishes, got %d", count)
	}

	// Now heal the simulated redis failure
	pub.mu.Lock()
	pub.failIDs["job-fail-1"] = false
	pub.mu.Unlock()

	// Reconciler should sweep and successfully publish
	reconciler := outbox.NewReconciler(repo, pub, 10*time.Second, 10*time.Millisecond, 10, nil)
	recovered, err := reconciler.ReconcileOnce(ctx)
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("expected 1 recovered event, got %d", recovered)
	}

	pub.mu.Lock()
	_, found := pub.published["job-fail-1"]
	pub.mu.Unlock()
	if !found {
		t.Fatalf("expected job-fail-1 to be published after reconciliation")
	}
}
