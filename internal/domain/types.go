package domain

import (
	"encoding/json"
	"errors"
	"time"
)

// JobStatus represents the state of a job in its lifecycle.
type JobStatus string

const (
	StatusQueued    JobStatus = "QUEUED"
	StatusRunning   JobStatus = "RUNNING"
	StatusCompleted JobStatus = "COMPLETED"
	StatusFailed    JobStatus = "FAILED"
	StatusTimeout   JobStatus = "TIMEOUT"
	StatusCancelled JobStatus = "CANCELLED"
)

// IsTerminal returns true if the job status is terminal.
func (s JobStatus) IsTerminal() bool {
	switch s {
	case StatusCompleted, StatusFailed, StatusTimeout, StatusCancelled:
		return true
	default:
		return false
	}
}

// FailureCategory classifies why a job failed.
type FailureCategory string

const (
	FailureCategoryNone              FailureCategory = "NONE"
	FailureCategorySystemError       FailureCategory = "SYSTEM_ERROR"
	FailureCategoryInfrastructure    FailureCategory = "INFRASTRUCTURE_FAILURE"
	FailureCategoryCompilationError FailureCategory = "COMPILATION_ERROR"
	FailureCategoryRuntimeError      FailureCategory = "RUNTIME_ERROR"
	FailureCategoryTimeout           FailureCategory = "TIMEOUT_ERROR"
	FailureCategoryOutputLimit       FailureCategory = "OUTPUT_LIMIT_EXCEEDED"
)

// Language represents a supported runtime language.
type Language string

const (
	LanguagePython     Language = "python"
	LanguageJavascript Language = "javascript"
	LanguageGo         Language = "go"
	LanguageCpp        Language = "cpp"
	LanguageJava       Language = "java"
)

// ValidLanguages returns a slice of all supported languages.
func ValidLanguages() []Language {
	return []Language{
		LanguagePython,
		LanguageJavascript,
		LanguageGo,
		LanguageCpp,
		LanguageJava,
	}
}

// IsValidLanguage checks whether a language string is supported.
func IsValidLanguage(l string) bool {
	switch Language(l) {
	case LanguagePython, LanguageJavascript, LanguageGo, LanguageCpp, LanguageJava:
		return true
	default:
		return false
	}
}

// Limits defines the execution resource boundaries.
type Limits struct {
	CPULimit       string `json:"cpu_limit"`       // e.g. "1.0"
	MemoryLimitMB  int    `json:"memory_limit_mb"` // e.g. 256
	TimeoutSeconds int    `json:"timeout_seconds"` // e.g. 10
	MaxOutputBytes int    `json:"max_output_bytes"`// e.g. 1048576 (1MB)
	MaxPIDs        int    `json:"max_pids"`        // e.g. 64
}

// DefaultLimits returns sensible secure default limits.
func DefaultLimits() Limits {
	return Limits{
		CPULimit:       "1.0",
		MemoryLimitMB:  256,
		TimeoutSeconds: 10,
		MaxOutputBytes: 1024 * 1024, // 1MB
		MaxPIDs:        64,
	}
}

// Job represents a code execution job.
type Job struct {
	ID              string          `json:"id"`
	UserID          string          `json:"user_id"`
	Language        Language        `json:"language"`
	SourceCode      string          `json:"source_code"`
	Stdin           string          `json:"stdin,omitempty"`
	Status          JobStatus       `json:"status"`
	CurrentAttempt  int             `json:"current_attempt"`
	MaxAttempts     int             `json:"max_attempts"`
	Limits          Limits          `json:"limits"`
	Stdout          string          `json:"stdout,omitempty"`
	Stderr          string          `json:"stderr,omitempty"`
	ExitCode        *int            `json:"exit_code,omitempty"`
	OutputTruncated bool            `json:"output_truncated"`
	FailureCategory FailureCategory `json:"failure_category,omitempty"`
	FailureReason   string          `json:"failure_reason,omitempty"`
	AssignedWorker  string          `json:"assigned_worker,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	StartedAt       *time.Time      `json:"started_at,omitempty"`
	FinishedAt      *time.Time      `json:"finished_at,omitempty"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// JobAttempt records an individual execution attempt of a job.
type JobAttempt struct {
	ID              string          `json:"id"`
	JobID           string          `json:"job_id"`
	AttemptNumber   int             `json:"attempt_number"`
	WorkerID        string          `json:"worker_id"`
	Status          JobStatus       `json:"status"`
	ExitCode        *int            `json:"exit_code,omitempty"`
	Stdout          string          `json:"stdout,omitempty"`
	Stderr          string          `json:"stderr,omitempty"`
	OutputTruncated bool            `json:"output_truncated"`
	FailureCategory FailureCategory `json:"failure_category,omitempty"`
	FailureReason   string          `json:"failure_reason,omitempty"`
	StartedAt       time.Time       `json:"started_at"`
	FinishedAt      *time.Time      `json:"finished_at,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
}

// WorkerStatus represents the health and connectivity state of a worker.
type WorkerStatus string

const (
	WorkerStatusOnline  WorkerStatus = "ONLINE"
	WorkerStatusBusy    WorkerStatus = "BUSY"
	WorkerStatusOffline WorkerStatus = "OFFLINE"
	WorkerStatusDraining WorkerStatus = "DRAINING"
)

// Worker records registration and heartbeat of a worker process.
type Worker struct {
	ID             string       `json:"id"`
	Hostname       string       `json:"hostname"`
	Concurrency    int          `json:"concurrency"`
	ActiveJobs     int          `json:"active_jobs"`
	CompletedJobs  int64        `json:"completed_jobs"`
	FailedJobs     int64        `json:"failed_jobs"`
	Status         WorkerStatus `json:"status"`
	LastHeartbeat  time.Time    `json:"last_heartbeat"`
	StartedAt      time.Time    `json:"started_at"`
	Version        string       `json:"version"`
}

// OutboxStatus represents the publishing state of an outbox message.
type OutboxStatus string

const (
	OutboxStatusPending   OutboxStatus = "PENDING"
	OutboxStatusPublished OutboxStatus = "PUBLISHED"
	OutboxStatusFailed    OutboxStatus = "FAILED"
)

// OutboxEvent represents an event persisted in PostgreSQL to be delivered to Redis Streams.
type OutboxEvent struct {
	ID          int64           `json:"id"`
	AggregateID string          `json:"aggregate_id"`
	EventType   string          `json:"event_type"`
	Payload     json.RawMessage `json:"payload"`
	Status      OutboxStatus    `json:"status"`
	CreatedAt   time.Time       `json:"created_at"`
	PublishedAt *time.Time      `json:"published_at,omitempty"`
	RetryCount  int             `json:"retry_count"`
	LastError   string          `json:"last_error,omitempty"`
}

// User represents an authenticated platform user.
type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// IdempotencyKey stores a client's idempotency token and the resulting response.
type IdempotencyKey struct {
	Key          string    `json:"key"`
	UserID       string    `json:"user_id"`
	JobID        string    `json:"job_id"`
	CreatedAt    time.Time `json:"created_at"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// ExecutionStreamEvent is an SSE event emitted to the frontend during execution.
type ExecutionStreamEvent struct {
	Type      string      `json:"type"` // "status", "stdout", "stderr", "terminal", "error"
	JobID     string      `json:"job_id"`
	Data      string      `json:"data,omitempty"`
	Status    JobStatus   `json:"status,omitempty"`
	ExitCode  *int        `json:"exit_code,omitempty"`
	Truncated bool        `json:"truncated,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
}

// Standard errors
var (
	ErrNotFound            = errors.New("resource not found")
	ErrUnauthorized        = errors.New("unauthorized")
	ErrForbidden           = errors.New("forbidden: access denied")
	ErrInvalidStateChange  = errors.New("invalid job state transition")
	ErrJobAlreadyTerminal  = errors.New("job is already in a terminal state")
	ErrInvalidLanguage     = errors.New("unsupported programming language")
	ErrInvalidSource       = errors.New("source code is empty or exceeds size limit")
	ErrInvalidTimeout      = errors.New("timeout exceeds permitted range")
	ErrDuplicateIdempotency = errors.New("duplicate request with existing idempotency key")
	ErrWorkerStale         = errors.New("worker heartbeat is stale")
	ErrStreamExhausted     = errors.New("stream processing completed")
)
