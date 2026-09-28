package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/forgequeue/forgequeue/internal/auth"
	"github.com/forgequeue/forgequeue/internal/domain"
	"github.com/forgequeue/forgequeue/internal/metrics"
	"github.com/forgequeue/forgequeue/internal/queue"
	"github.com/forgequeue/forgequeue/internal/repository"
)

type JobsHandler struct {
	jobRepo         repository.JobRepository
	attemptRepo     repository.AttemptRepository
	idempotencyRepo repository.IdempotencyRepository
	queue           queue.Queue
}

func NewJobsHandler(
	jobRepo repository.JobRepository,
	attemptRepo repository.AttemptRepository,
	idempotencyRepo repository.IdempotencyRepository,
	q queue.Queue,
) *JobsHandler {
	return &JobsHandler{
		jobRepo:         jobRepo,
		attemptRepo:     attemptRepo,
		idempotencyRepo: idempotencyRepo,
		queue:           q,
	}
}

type CreateJobRequest struct {
	Language   string         `json:"language"`
	SourceCode string         `json:"source_code"`
	Stdin      string         `json:"stdin,omitempty"`
	Limits     *domain.Limits `json:"limits,omitempty"`
}

func (h *JobsHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.UserFromContext(r.Context())
	if !ok || claims == nil {
		RespondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Not authenticated")
		return
	}

	// 1. Idempotency Check
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey != "" {
		existing, err := h.idempotencyRepo.Get(r.Context(), claims.UserID, idempotencyKey)
		if err == nil && existing != nil {
			// Return already created job
			existingJob, err := h.jobRepo.GetByID(r.Context(), existing.JobID)
			if err == nil && existingJob != nil {
				RespondJSON(w, http.StatusOK, existingJob)
				return
			}
		}
	}

	// 2. Parse & Validate Payload
	var req CreateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON payload")
		return
	}

	req.Language = strings.TrimSpace(strings.ToLower(req.Language))
	if !domain.IsValidLanguage(req.Language) {
		RespondError(w, http.StatusBadRequest, "INVALID_LANGUAGE", fmt.Sprintf("Unsupported language '%s'. Supported: %v", req.Language, domain.ValidLanguages()))
		return
	}

	if len(req.SourceCode) == 0 {
		RespondError(w, http.StatusBadRequest, "INVALID_SOURCE", "Source code cannot be empty")
		return
	}
	if len(req.SourceCode) > 64*1024 { // 64KB max source
		RespondError(w, http.StatusBadRequest, "SOURCE_TOO_LARGE", "Source code exceeds 64KB maximum limit")
		return
	}
	if len(req.Stdin) > 256*1024 { // 256KB max stdin
		RespondError(w, http.StatusBadRequest, "STDIN_TOO_LARGE", "Stdin exceeds 256KB maximum limit")
		return
	}

	limits := domain.DefaultLimits()
	if req.Limits != nil {
		if req.Limits.TimeoutSeconds >= 1 && req.Limits.TimeoutSeconds <= 60 {
			limits.TimeoutSeconds = req.Limits.TimeoutSeconds
		}
		if req.Limits.MemoryLimitMB >= 32 && req.Limits.MemoryLimitMB <= 512 {
			limits.MemoryLimitMB = req.Limits.MemoryLimitMB
		}
		if req.Limits.MaxOutputBytes > 0 && req.Limits.MaxOutputBytes <= 5*1024*1024 {
			limits.MaxOutputBytes = req.Limits.MaxOutputBytes
		}
	}

	// 3. Create Job and Outbox Event Atomically
	now := time.Now()
	jobID := uuid.New().String()
	job := &domain.Job{
		ID:              jobID,
		UserID:          claims.UserID,
		Language:        domain.Language(req.Language),
		SourceCode:      req.SourceCode,
		Stdin:           req.Stdin,
		Status:          domain.StatusQueued,
		CurrentAttempt:  0,
		MaxAttempts:     3,
		Limits:          limits,
		CreatedAt:       now,
		UpdatedAt:       now,
		FailureCategory: domain.FailureCategoryNone,
	}

	payload, _ := json.Marshal(map[string]string{"job_id": jobID})
	outbox := &domain.OutboxEvent{
		AggregateID: jobID,
		EventType:   "job.queued",
		Payload:     payload,
		Status:      domain.OutboxStatusPending,
		CreatedAt:   now,
	}

	if err := h.jobRepo.Create(r.Context(), job, outbox); err != nil {
		RespondError(w, http.StatusInternalServerError, "SYSTEM_ERROR", "Failed to create job")
		return
	}

	// 4. Record Idempotency Mapping
	if idempotencyKey != "" {
		_ = h.idempotencyRepo.Store(r.Context(), &domain.IdempotencyKey{
			Key:       idempotencyKey,
			UserID:    claims.UserID,
			JobID:     jobID,
			CreatedAt: now,
			ExpiresAt: now.Add(24 * time.Hour),
		})
	}

	metrics.JobsSubmitted.WithLabelValues(string(job.Language)).Inc()
	RespondJSON(w, http.StatusCreated, job)
}

func (h *JobsHandler) List(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.UserFromContext(r.Context())
	if !ok || claims == nil {
		RespondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Not authenticated")
		return
	}

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	jobs, total, err := h.jobRepo.ListByUserID(r.Context(), claims.UserID, limit, offset)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "SYSTEM_ERROR", "Failed to query jobs")
		return
	}

	RespondJSON(w, http.StatusOK, PaginatedResponse{
		Items:  jobs,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

func (h *JobsHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.UserFromContext(r.Context())
	if !ok || claims == nil {
		RespondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Not authenticated")
		return
	}

	jobID := chi.URLParam(r, "id")
	job, err := h.jobRepo.GetByID(r.Context(), jobID)
	if err != nil {
		if err == domain.ErrNotFound {
			RespondError(w, http.StatusNotFound, "NOT_FOUND", "Job not found")
			return
		}
		RespondError(w, http.StatusInternalServerError, "SYSTEM_ERROR", "Failed to retrieve job")
		return
	}

	// Authorization: prevent cross-user job access
	if job.UserID != claims.UserID {
		RespondError(w, http.StatusForbidden, "FORBIDDEN", "You do not have permission to view this job")
		return
	}

	RespondJSON(w, http.StatusOK, job)
}

func (h *JobsHandler) ListAttempts(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.UserFromContext(r.Context())
	if !ok || claims == nil {
		RespondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Not authenticated")
		return
	}

	jobID := chi.URLParam(r, "id")
	job, err := h.jobRepo.GetByID(r.Context(), jobID)
	if err != nil {
		RespondError(w, http.StatusNotFound, "NOT_FOUND", "Job not found")
		return
	}

	if job.UserID != claims.UserID {
		RespondError(w, http.StatusForbidden, "FORBIDDEN", "Access denied")
		return
	}

	attempts, err := h.attemptRepo.ListByJobID(r.Context(), jobID)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "SYSTEM_ERROR", "Failed to retrieve attempts")
		return
	}

	RespondJSON(w, http.StatusOK, attempts)
}

func (h *JobsHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.UserFromContext(r.Context())
	if !ok || claims == nil {
		RespondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Not authenticated")
		return
	}

	jobID := chi.URLParam(r, "id")
	cancelledJob, err := h.jobRepo.CancelJob(r.Context(), jobID, claims.UserID)
	if err != nil {
		if err == domain.ErrForbidden {
			RespondError(w, http.StatusForbidden, "FORBIDDEN", "You do not have permission to cancel this job")
			return
		}
		if err == domain.ErrNotFound {
			RespondError(w, http.StatusNotFound, "NOT_FOUND", "Job not found")
			return
		}
		if err == domain.ErrJobAlreadyTerminal {
			RespondError(w, http.StatusConflict, "JOB_ALREADY_TERMINAL", "Job is already completed, failed, or cancelled")
			return
		}
		RespondError(w, http.StatusBadRequest, "INVALID_STATE_CHANGE", err.Error())
		return
	}

	// Broadcast cancelled event
	eventBytes, _ := json.Marshal(domain.ExecutionStreamEvent{
		Type:      "terminal",
		JobID:     jobID,
		Status:    domain.StatusCancelled,
		Timestamp: time.Now(),
	})
	_ = h.queue.PublishEvent(r.Context(), jobID, eventBytes)

	RespondJSON(w, http.StatusOK, cancelledJob)
}

// EventsSSE streams Server-Sent Events to the client.
func (h *JobsHandler) EventsSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	jobID := chi.URLParam(r, "id")

	// Set SSE Headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// 1. Initial State Snapshot from Database
	job, err := h.jobRepo.GetByID(r.Context(), jobID)
	if err == nil && job != nil {
		// Emit current status snapshot
		snapEvent, _ := json.Marshal(domain.ExecutionStreamEvent{
			Type:      "status",
			JobID:     job.ID,
			Status:    job.Status,
			Timestamp: job.UpdatedAt,
		})
		fmt.Fprintf(w, "data: %s\n\n", snapEvent)

		// If output already exists (e.g. browser refreshed), replay it
		if job.Stdout != "" {
			stdoutEvent, _ := json.Marshal(domain.ExecutionStreamEvent{
				Type:      "stdout",
				JobID:     job.ID,
				Data:      job.Stdout,
				Timestamp: job.UpdatedAt,
			})
			fmt.Fprintf(w, "data: %s\n\n", stdoutEvent)
		}
		if job.Stderr != "" {
			stderrEvent, _ := json.Marshal(domain.ExecutionStreamEvent{
				Type:      "stderr",
				JobID:     job.ID,
				Data:      job.Stderr,
				Timestamp: job.UpdatedAt,
			})
			fmt.Fprintf(w, "data: %s\n\n", stderrEvent)
		}

		// If already in terminal state, emit terminal event and close connection
		if job.Status.IsTerminal() {
			termEvent, _ := json.Marshal(domain.ExecutionStreamEvent{
				Type:      "terminal",
				JobID:     job.ID,
				Status:    job.Status,
				ExitCode:  job.ExitCode,
				Truncated: job.OutputTruncated,
				Timestamp: job.UpdatedAt,
			})
			fmt.Fprintf(w, "data: %s\n\n", termEvent)
			flusher.Flush()
			return
		}
	}
	flusher.Flush()

	// 2. Subscribe to live stream events
	// Check if queue provides Subscribe channel (MemoryQueue or Redis PubSub)
	type directSubscriber interface {
		Subscribe(jobID string) (chan []byte, func())
	}

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	if subQueue, ok := h.queue.(directSubscriber); ok {
		eventCh, cleanup := subQueue.Subscribe(jobID)
		defer cleanup()

		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				fmt.Fprintf(w, ":keepalive\n\n")
				flusher.Flush()
			case data, ok := <-eventCh:
				if !ok {
					return
				}
				fmt.Fprintf(w, "data: %s\n\n", string(data))
				flusher.Flush()

				var ev domain.ExecutionStreamEvent
				if err := json.Unmarshal(data, &ev); err == nil && ev.Type == "terminal" {
					return
				}
			}
		}
	} else {
		// Fallback polling loop if pure Redis client without direct Go channel wrapper
		pollTicker := time.NewTicker(500 * time.Millisecond)
		defer pollTicker.Stop()

		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				fmt.Fprintf(w, ":keepalive\n\n")
				flusher.Flush()
			case <-pollTicker.C:
				current, err := h.jobRepo.GetByID(r.Context(), jobID)
				if err == nil && current != nil && current.Status.IsTerminal() {
					termEvent, _ := json.Marshal(domain.ExecutionStreamEvent{
						Type:      "terminal",
						JobID:     current.ID,
						Status:    current.Status,
						ExitCode:  current.ExitCode,
						Truncated: current.OutputTruncated,
						Timestamp: current.UpdatedAt,
					})
					fmt.Fprintf(w, "data: %s\n\n", termEvent)
					flusher.Flush()
					return
				}
			}
		}
	}
}
