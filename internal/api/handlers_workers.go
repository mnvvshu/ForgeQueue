package api

import (
	"net/http"
	"time"

	"github.com/forgequeue/forgequeue/internal/domain"
	"github.com/forgequeue/forgequeue/internal/repository"
)

type WorkersHandler struct {
	workerRepo repository.WorkerRepository
}

func NewWorkersHandler(workerRepo repository.WorkerRepository) *WorkersHandler {
	return &WorkersHandler{workerRepo: workerRepo}
}

type WorkerResponse struct {
	ID             string              `json:"id"`
	Hostname       string              `json:"hostname"`
	Concurrency    int                 `json:"concurrency"`
	ActiveJobs     int                 `json:"active_jobs"`
	CompletedJobs  int64               `json:"completed_jobs"`
	FailedJobs     int64               `json:"failed_jobs"`
	Status         domain.WorkerStatus `json:"status"`
	LastHeartbeat  time.Time           `json:"last_heartbeat"`
	SecondsSinceHb float64             `json:"seconds_since_heartbeat"`
	StartedAt      time.Time           `json:"started_at"`
	Version        string              `json:"version"`
}

func (h *WorkersHandler) List(w http.ResponseWriter, r *http.Request) {
	workers, err := h.workerRepo.List(r.Context())
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "SYSTEM_ERROR", "Failed to retrieve workers")
		return
	}

	now := time.Now()
	var response []WorkerResponse
	for _, wk := range workers {
		secondsSince := now.Sub(wk.LastHeartbeat).Seconds()
		status := wk.Status
		if secondsSince > 15 {
			status = domain.WorkerStatusOffline
		}

		response = append(response, WorkerResponse{
			ID:             wk.ID,
			Hostname:       wk.Hostname,
			Concurrency:    wk.Concurrency,
			ActiveJobs:     wk.ActiveJobs,
			CompletedJobs:  wk.CompletedJobs,
			FailedJobs:     wk.FailedJobs,
			Status:         status,
			LastHeartbeat:  wk.LastHeartbeat,
			SecondsSinceHb: secondsSince,
			StartedAt:      wk.StartedAt,
			Version:        wk.Version,
		})
	}

	RespondJSON(w, http.StatusOK, response)
}
