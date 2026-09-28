package queue

import (
	"context"
	"time"
)

// Message represents a task message retrieved from the queue stream.
type Message struct {
	ID        string    `json:"id"`
	JobID     string    `json:"job_id"`
	Payload   []byte    `json:"payload"`
	DeliveredAt time.Time `json:"delivered_at"`
}

// Queue defines the queue interface for job stream delivery.
type Queue interface {
	EnsureStreamAndGroup(ctx context.Context) error
	PublishJob(ctx context.Context, jobID string, payload []byte) error
	ReadJobs(ctx context.Context, consumerID string, count int64, block time.Duration) ([]Message, error)
	AckJob(ctx context.Context, messageID string) error
	ClaimStaleJobs(ctx context.Context, consumerID string, minIdle time.Duration, count int64) ([]Message, error)
	PublishEvent(ctx context.Context, jobID string, event []byte) error
}
