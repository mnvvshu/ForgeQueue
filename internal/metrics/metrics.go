package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	JobsSubmitted = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "forgequeue_jobs_submitted_total",
			Help: "Total number of code execution jobs submitted",
		},
		[]string{"language"},
	)

	JobsCompleted = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "forgequeue_jobs_completed_total",
			Help: "Total number of code execution jobs completed successfully",
		},
		[]string{"language"},
	)

	JobsFailed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "forgequeue_jobs_failed_total",
			Help: "Total number of code execution jobs failed",
		},
		[]string{"language", "failure_category"},
	)

	JobsTimeout = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "forgequeue_jobs_timeout_total",
			Help: "Total number of code execution jobs timed out",
		},
		[]string{"language"},
	)

	JobDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "forgequeue_job_duration_seconds",
			Help:    "Duration of job execution inside container in seconds",
			Buckets: []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 15, 30},
		},
		[]string{"language"},
	)

	ActiveExecutions = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "forgequeue_active_executions",
			Help: "Current number of actively executing jobs across all workers",
		},
	)

	WorkersActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "forgequeue_workers_active",
			Help: "Current number of online workers sending heartbeats",
		},
	)

	HttpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "forgequeue_http_requests_total",
			Help: "Total number of HTTP requests processed",
		},
		[]string{"method", "path", "status"},
	)

	HttpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "forgequeue_http_request_duration_seconds",
			Help:    "HTTP request latency in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)
)

type statusLoggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusLoggingResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusLoggingResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// HTTPMiddleware instruments HTTP endpoints with Prometheus metrics.
func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &statusLoggingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start).Seconds()
		path := r.URL.Path
		// Low-cardinality sanitization for metric paths
		if len(path) > 36 && path[:15] == "/api/v1/jobs/" {
			path = "/api/v1/jobs/:id"
		}

		HttpRequestsTotal.WithLabelValues(r.Method, path, strconv.Itoa(wrapped.statusCode)).Inc()
		HttpRequestDuration.WithLabelValues(r.Method, path).Observe(duration)
	})
}
