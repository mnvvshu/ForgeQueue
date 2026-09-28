package queue

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type pendingEntry struct {
	msg         Message
	consumerID  string
	deliveredAt time.Time
}

// MemoryQueue is a thread-safe in-memory implementation of Queue for testing.
type MemoryQueue struct {
	mu           sync.Mutex
	messages     []Message
	pending      map[string]*pendingEntry // messageID -> pendingEntry
	subscribers  map[string][]chan []byte // channel -> list of listener channels
	nextMsgID    int64
	notifyCh     chan struct{}
}

// NewMemoryQueue creates a new MemoryQueue.
func NewMemoryQueue() *MemoryQueue {
	return &MemoryQueue{
		pending:     make(map[string]*pendingEntry),
		subscribers: make(map[string][]chan []byte),
		notifyCh:    make(chan struct{}, 1),
	}
}

func (q *MemoryQueue) EnsureStreamAndGroup(ctx context.Context) error {
	return nil
}

func (q *MemoryQueue) PublishJob(ctx context.Context, jobID string, payload []byte) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.nextMsgID++
	msgID := fmt.Sprintf("%d-0", q.nextMsgID)
	q.messages = append(q.messages, Message{
		ID:          msgID,
		JobID:       jobID,
		Payload:     payload,
		DeliveredAt: time.Now(),
	})

	select {
	case q.notifyCh <- struct{}{}:
	default:
	}

	return nil
}

func (q *MemoryQueue) ReadJobs(ctx context.Context, consumerID string, count int64, block time.Duration) ([]Message, error) {
	for {
		q.mu.Lock()
		if len(q.messages) > 0 {
			n := int(count)
			if n > len(q.messages) {
				n = len(q.messages)
			}
			batch := q.messages[:n]
			q.messages = q.messages[n:]

			var result []Message
			now := time.Now()
			for _, m := range batch {
				m.DeliveredAt = now
				q.pending[m.ID] = &pendingEntry{
					msg:         m,
					consumerID:  consumerID,
					deliveredAt: now,
				}
				result = append(result, m)
			}
			q.mu.Unlock()
			return result, nil
		}
		q.mu.Unlock()

		if block <= 0 {
			return []Message{}, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(block):
			return []Message{}, nil
		case <-q.notifyCh:
			// message arrived, loop again
		}
	}
}

func (q *MemoryQueue) AckJob(ctx context.Context, messageID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	delete(q.pending, messageID)
	return nil
}

func (q *MemoryQueue) ClaimStaleJobs(ctx context.Context, consumerID string, minIdle time.Duration, count int64) ([]Message, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	var claimed []Message
	now := time.Now()
	for _, entry := range q.pending {
		if entry.consumerID != consumerID && now.Sub(entry.deliveredAt) >= minIdle {
			entry.consumerID = consumerID
			entry.deliveredAt = now
			claimed = append(claimed, entry.msg)
			if int64(len(claimed)) >= count {
				break
			}
		}
	}
	return claimed, nil
}

func (q *MemoryQueue) PublishEvent(ctx context.Context, jobID string, event []byte) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	channel := fmt.Sprintf("%s%s:events", EventsChannelPrefix, jobID)
	for _, ch := range q.subscribers[channel] {
		select {
		case ch <- event:
		default:
		}
	}
	return nil
}

// Subscribe returns a Go channel that receives events for a jobID.
func (q *MemoryQueue) Subscribe(jobID string) (chan []byte, func()) {
	q.mu.Lock()
	defer q.mu.Unlock()

	channel := fmt.Sprintf("%s%s:events", EventsChannelPrefix, jobID)
	ch := make(chan []byte, 100)
	q.subscribers[channel] = append(q.subscribers[channel], ch)

	cleanup := func() {
		q.mu.Lock()
		defer q.mu.Unlock()
		subs := q.subscribers[channel]
		for i, c := range subs {
			if c == ch {
				q.subscribers[channel] = append(subs[:i], subs[i+1:]...)
				close(ch)
				break
			}
		}
	}

	return ch, cleanup
}
