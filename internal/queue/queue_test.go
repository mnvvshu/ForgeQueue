package queue_test

import (
	"context"
	"testing"
	"time"

	"github.com/forgequeue/forgequeue/internal/queue"
)

func TestMemoryQueue_PublishReadAck(t *testing.T) {
	ctx := context.Background()
	q := queue.NewMemoryQueue()

	// 1. Publish 2 jobs
	if err := q.PublishJob(ctx, "job-1", []byte(`{"job_id":"job-1"}`)); err != nil {
		t.Fatalf("failed to publish job-1: %v", err)
	}
	if err := q.PublishJob(ctx, "job-2", []byte(`{"job_id":"job-2"}`)); err != nil {
		t.Fatalf("failed to publish job-2: %v", err)
	}

	// 2. Worker 1 reads 1 job
	msgs1, err := q.ReadJobs(ctx, "worker-1", 1, 0)
	if err != nil || len(msgs1) != 1 {
		t.Fatalf("expected 1 message for worker-1, got %d, err: %v", len(msgs1), err)
	}
	if msgs1[0].JobID != "job-1" {
		t.Fatalf("expected job-1, got %s", msgs1[0].JobID)
	}

	// 3. Worker 2 reads next job
	msgs2, err := q.ReadJobs(ctx, "worker-2", 1, 0)
	if err != nil || len(msgs2) != 1 {
		t.Fatalf("expected 1 message for worker-2, got %d, err: %v", len(msgs2), err)
	}
	if msgs2[0].JobID != "job-2" {
		t.Fatalf("expected job-2, got %s", msgs2[0].JobID)
	}

	// 4. Ack job-1
	if err := q.AckJob(ctx, msgs1[0].ID); err != nil {
		t.Fatalf("failed to ack job-1: %v", err)
	}

	// 5. Worker-2 crashes without acking job-2. Simulate idle time and Worker-1 claims it
	claimed, err := q.ClaimStaleJobs(ctx, "worker-1", 0, 10)
	if err != nil {
		t.Fatalf("failed to claim stale jobs: %v", err)
	}
	if len(claimed) != 1 || claimed[0].JobID != "job-2" {
		t.Fatalf("expected worker-1 to claim stale job-2, got %v", claimed)
	}
}

func TestMemoryQueue_PubSubEvents(t *testing.T) {
	ctx := context.Background()
	q := queue.NewMemoryQueue()

	jobID := "job-stream-test"
	subCh, cleanup := q.Subscribe(jobID)
	defer cleanup()

	eventData := []byte(`{"type":"stdout","data":"Hello World\n"}`)
	if err := q.PublishEvent(ctx, jobID, eventData); err != nil {
		t.Fatalf("failed to publish event: %v", err)
	}

	select {
	case received := <-subCh:
		if string(received) != string(eventData) {
			t.Fatalf("expected %s, got %s", string(eventData), string(received))
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for pubsub event")
	}
}
