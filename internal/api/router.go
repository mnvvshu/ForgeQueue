package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/forgequeue/forgequeue/internal/auth"
	"github.com/forgequeue/forgequeue/internal/metrics"
)

type RouterConfig struct {
	AuthHandler     *AuthHandler
	JobsHandler     *JobsHandler
	WorkersHandler  *WorkersHandler
	RuntimesHandler *RuntimesHandler
	TokenManager    *auth.TokenManager
	DBChecker       func(ctx context.Context) error
	RedisChecker    func(ctx context.Context) error
	Logger          *slog.Logger
}

// NewRouter constructs the complete HTTP handler for ForgeQueue API.
func NewRouter(cfg RouterConfig) http.Handler {
	r := chi.NewRouter()

	// Global Middleware
	r.Use(metrics.HTTPMiddleware)
	r.Use(CORSMiddleware)
	r.Use(RequestLogger(cfg.Logger))
	r.Use(Recoverer(cfg.Logger))

	// Prometheus Metrics
	r.Handle("/metrics", promhttp.Handler())

	// Liveness Probe
	r.Get("/health/live", func(w http.ResponseWriter, r *http.Request) {
		RespondJSON(w, http.StatusOK, map[string]string{"status": "live"})
	})

	// Readiness Probe
	r.Get("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		status := map[string]string{
			"status":   "ready",
			"database": "ok",
			"redis":    "ok",
		}

		isReady := true
		if cfg.DBChecker != nil {
			if err := cfg.DBChecker(r.Context()); err != nil {
				status["database"] = err.Error()
				isReady = false
			}
		}

		if cfg.RedisChecker != nil {
			if err := cfg.RedisChecker(r.Context()); err != nil {
				status["redis"] = err.Error()
				isReady = false
			}
		}

		if !isReady {
			status["status"] = "degraded"
			RespondJSON(w, http.StatusServiceUnavailable, status)
			return
		}

		RespondJSON(w, http.StatusOK, status)
	})

	// API v1 Subrouter
	r.Route("/api/v1", func(v1 chi.Router) {
		// Public Auth
		v1.Post("/auth/register", cfg.AuthHandler.Register)
		v1.Post("/auth/login", cfg.AuthHandler.Login)

		// Public Discovery
		v1.Get("/runtimes", cfg.RuntimesHandler.List)
		v1.Get("/workers", cfg.WorkersHandler.List)

		// Protected Endpoints
		v1.Group(func(protected chi.Router) {
			protected.Use(RequireAuth(cfg.TokenManager))

			// Auth User Profile
			protected.Get("/auth/me", cfg.AuthHandler.Me)

			// Jobs Management
			protected.Post("/jobs", cfg.JobsHandler.Create)
			protected.Get("/jobs", cfg.JobsHandler.List)
			protected.Get("/jobs/{id}", cfg.JobsHandler.Get)
			protected.Get("/jobs/{id}/attempts", cfg.JobsHandler.ListAttempts)
			protected.Post("/jobs/{id}/cancel", cfg.JobsHandler.Cancel)
			protected.Get("/jobs/{id}/events", cfg.JobsHandler.EventsSSE)
		})
	})

	return r
}
