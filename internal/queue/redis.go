package queue

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	DefaultStreamKey   = "forgequeue:jobs:stream"
	DefaultDLQKey      = "forgequeue:jobs:dlq"
	DefaultGroupName   = "forgequeue-workers"
	EventsChannelPrefix = "forgequeue:job:"
)

// RedisQueue implements the Queue interface using Redis Streams and Pub/Sub.
type RedisQueue struct {
	client    *redis.Client
	streamKey string
	dlqKey    string
	groupName string
}

// NewRedisQueue creates a new RedisQueue instance.
func NewRedisQueue(client *redis.Client, streamKey, dlqKey, groupName string) *RedisQueue {
	if streamKey == "" {
		streamKey = DefaultStreamKey
	}
	if dlqKey == "" {
		dlqKey = DefaultDLQKey
	}
	if groupName == "" {
		groupName = DefaultGroupName
	}
	return &RedisQueue{
		client:    client,
		streamKey: streamKey,
		dlqKey:    dlqKey,
		groupName: groupName,
	}
}

// EnsureStreamAndGroup creates the Redis stream and consumer group if not already present.
func (q *RedisQueue) EnsureStreamAndGroup(ctx context.Context) error {
	err := q.client.XGroupCreateMkStream(ctx, q.streamKey, q.groupName, "$").Err()
	if err != nil {
		// Ignore BUSYGROUP error if consumer group already exists
		if strings.Contains(err.Error(), "BUSYGROUP") {
			return nil
		}
		return fmt.Errorf("failed to create redis consumer group: %w", err)
	}
	return nil
}

// PublishJob pushes a job dispatch event to the Redis Stream.
func (q *RedisQueue) PublishJob(ctx context.Context, jobID string, payload []byte) error {
	args := &redis.XAddArgs{
		Stream: q.streamKey,
		Values: map[string]interface{}{
			"job_id":  jobID,
			"payload": string(payload),
		},
	}
	return q.client.XAdd(ctx, args).Err()
}

// ReadJobs reads new undelivered jobs for this consumer from the consumer group.
func (q *RedisQueue) ReadJobs(ctx context.Context, consumerID string, count int64, block time.Duration) ([]Message, error) {
	streams, err := q.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    q.groupName,
		Consumer: consumerID,
		Streams:  []string{q.streamKey, ">"},
		Count:    count,
		Block:    block,
	}).Result()

	if err != nil {
		if err == redis.Nil {
			return []Message{}, nil
		}
		return nil, fmt.Errorf("failed to read from redis stream: %w", err)
	}

	var messages []Message
	for _, stream := range streams {
		for _, xmsg := range stream.Messages {
			jobID, _ := xmsg.Values["job_id"].(string)
			payloadStr, _ := xmsg.Values["payload"].(string)

			messages = append(messages, Message{
				ID:          xmsg.ID,
				JobID:       jobID,
				Payload:     []byte(payloadStr),
				DeliveredAt: time.Now(),
			})
		}
	}

	return messages, nil
}

// AckJob acknowledges and removes the completed message from the stream.
func (q *RedisQueue) AckJob(ctx context.Context, messageID string) error {
	pipe := q.client.Pipeline()
	pipe.XAck(ctx, q.streamKey, q.groupName, messageID)
	pipe.XDel(ctx, q.streamKey, messageID)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to ack message: %w", err)
	}
	return nil
}

// ClaimStaleJobs uses XAutoClaim to acquire pending messages abandoned by crashed workers.
func (q *RedisQueue) ClaimStaleJobs(ctx context.Context, consumerID string, minIdle time.Duration, count int64) ([]Message, error) {
	claimRes, _, err := q.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream:   q.streamKey,
		Group:    q.groupName,
		Consumer: consumerID,
		MinIdle:  minIdle,
		Start:    "0-0",
		Count:    count,
	}).Result()

	if err != nil {
		return nil, fmt.Errorf("failed to autoclaim pending messages: %w", err)
	}

	var messages []Message
	for _, xmsg := range claimRes {
		jobID, _ := xmsg.Values["job_id"].(string)
		payloadStr, _ := xmsg.Values["payload"].(string)

		messages = append(messages, Message{
			ID:          xmsg.ID,
			JobID:       jobID,
			Payload:     []byte(payloadStr),
			DeliveredAt: time.Now(),
		})
	}

	return messages, nil
}

// PublishEvent broadcasts execution stream logs and status via Redis Pub/Sub.
func (q *RedisQueue) PublishEvent(ctx context.Context, jobID string, event []byte) error {
	channel := fmt.Sprintf("%s%s:events", EventsChannelPrefix, jobID)
	return q.client.Publish(ctx, channel, event).Err()
}

// SubscribeEvents returns a Redis PubSub channel for real-time SSE streaming.
func (q *RedisQueue) SubscribeEvents(ctx context.Context, jobID string) *redis.PubSub {
	channel := fmt.Sprintf("%s%s:events", EventsChannelPrefix, jobID)
	return q.client.Subscribe(ctx, channel)
}
