package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/forgequeue/forgequeue/internal/api"
	"github.com/forgequeue/forgequeue/internal/auth"
	"github.com/forgequeue/forgequeue/internal/domain"
	"github.com/forgequeue/forgequeue/internal/queue"
	"github.com/forgequeue/forgequeue/internal/repository"
	"github.com/forgequeue/forgequeue/internal/runtime"
)

type testHarness struct {
	router       http.Handler
	userRepo     repository.UserRepository
	jobRepo      repository.JobRepository
	tokenManager *auth.TokenManager
}

func setupTestHarness(t *testing.T) *testHarness {
	userRepo := repository.NewMemoryUserRepository()
	outboxRepo := repository.NewMemoryOutboxRepository()
	jobRepo := repository.NewMemoryJobRepository(outboxRepo)
	attemptRepo := repository.NewMemoryAttemptRepository()
	workerRepo := repository.NewMemoryWorkerRepository()
	idempotencyRepo := repository.NewMemoryIdempotencyRepository()
	q := queue.NewMemoryQueue()

	tm, err := auth.NewTokenManager("a_very_secure_test_secret_32_bytes_long!!", "forgequeue-test")
	if err != nil {
		t.Fatalf("failed to init token manager: %v", err)
	}

	authHandler := api.NewAuthHandler(userRepo, tm, 1*time.Hour)
	jobsHandler := api.NewJobsHandler(jobRepo, attemptRepo, idempotencyRepo, q)
	workersHandler := api.NewWorkersHandler(workerRepo)
	runtimesHandler := api.NewRuntimesHandler(runtime.Default())

	router := api.NewRouter(api.RouterConfig{
		AuthHandler:     authHandler,
		JobsHandler:     jobsHandler,
		WorkersHandler:  workersHandler,
		RuntimesHandler: runtimesHandler,
		TokenManager:    tm,
		DBChecker:       func(ctx context.Context) error { return nil },
		RedisChecker:    func(ctx context.Context) error { return nil },
		Logger:          nil,
	})

	return &testHarness{
		router:       router,
		userRepo:     userRepo,
		jobRepo:      jobRepo,
		tokenManager: tm,
	}
}

func registerAndLogin(t *testing.T, h *testHarness, username, email, password string) (string, string) {
	regBody, _ := json.Marshal(api.RegisterRequest{
		Username: username,
		Email:    email,
		Password: password,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(regBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("registration failed with code %d, body: %s", w.Code, w.Body.String())
	}

	var authRes api.AuthResponse
	_ = json.Unmarshal(w.Body.Bytes(), &authRes)
	return authRes.User.ID, authRes.Token
}

func TestAPI_AuthFlow(t *testing.T) {
	h := setupTestHarness(t)

	// 1. Register User A
	userID, token := registerAndLogin(t, h, "alice", "alice@example.com", "Password123!")
	if userID == "" || token == "" {
		t.Fatalf("expected valid userID and token")
	}

	// 2. Login User A
	loginBody, _ := json.Marshal(api.LoginRequest{Username: "alice", Password: "Password123!"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("login failed: %d, body: %s", w.Code, w.Body.String())
	}

	// 3. Authenticated /me endpoint
	meReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+token)
	wMe := httptest.NewRecorder()
	h.router.ServeHTTP(wMe, meReq)

	if wMe.Code != http.StatusOK {
		t.Fatalf("/me failed: %d, body: %s", wMe.Code, wMe.Body.String())
	}

	// 4. Unauthenticated request rejection
	unauthReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	wUnauth := httptest.NewRecorder()
	h.router.ServeHTTP(wUnauth, unauthReq)
	if wUnauth.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", wUnauth.Code)
	}
}

func TestAPI_JobSubmissionAndValidation(t *testing.T) {
	h := setupTestHarness(t)
	_, token := registerAndLogin(t, h, "bob", "bob@example.com", "Password123!")

	// 1. Valid Python Job submission
	jobReq, _ := json.Marshal(api.CreateJobRequest{
		Language:   "python",
		SourceCode: "print('Hello ForgeQueue API')",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", bytes.NewReader(jobReq))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("job creation failed: %d, body: %s", w.Code, w.Body.String())
	}

	var createdJob domain.Job
	_ = json.Unmarshal(w.Body.Bytes(), &createdJob)
	if createdJob.Status != domain.StatusQueued || createdJob.Language != domain.LanguagePython {
		t.Fatalf("unexpected job payload: %+v", createdJob)
	}

	// 2. Invalid Language Rejection
	badLangReq, _ := json.Marshal(api.CreateJobRequest{
		Language:   "fortran",
		SourceCode: "PROGRAM HELLO",
	})
	reqBad := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", bytes.NewReader(badLangReq))
	reqBad.Header.Set("Authorization", "Bearer "+token)
	reqBad.Header.Set("Content-Type", "application/json")
	wBad := httptest.NewRecorder()
	h.router.ServeHTTP(wBad, reqBad)

	if wBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for unsupported language, got %d", wBad.Code)
	}

	// 3. Empty Source Code Rejection
	emptySrcReq, _ := json.Marshal(api.CreateJobRequest{
		Language:   "python",
		SourceCode: "",
	})
	reqEmpty := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", bytes.NewReader(emptySrcReq))
	reqEmpty.Header.Set("Authorization", "Bearer "+token)
	wEmpty := httptest.NewRecorder()
	h.router.ServeHTTP(wEmpty, reqEmpty)

	if wEmpty.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for empty source, got %d", wEmpty.Code)
	}
}

func TestAPI_IdempotentSubmission(t *testing.T) {
	h := setupTestHarness(t)
	_, token := registerAndLogin(t, h, "carol", "carol@example.com", "Password123!")

	jobPayload, _ := json.Marshal(api.CreateJobRequest{
		Language:   "python",
		SourceCode: "print('idempotent')",
	})
	idempotencyKey := "idem-test-key-12345"

	// Request 1
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", bytes.NewReader(jobPayload))
	req1.Header.Set("Authorization", "Bearer "+token)
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Idempotency-Key", idempotencyKey)
	w1 := httptest.NewRecorder()
	h.router.ServeHTTP(w1, req1)

	if w1.Code != http.StatusCreated {
		t.Fatalf("req1 failed: %d", w1.Code)
	}
	var job1 domain.Job
	_ = json.Unmarshal(w1.Body.Bytes(), &job1)

	// Request 2 with same Idempotency-Key
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", bytes.NewReader(jobPayload))
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Idempotency-Key", idempotencyKey)
	w2 := httptest.NewRecorder()
	h.router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("req2 expected 200 OK for idempotent response, got %d", w2.Code)
	}
	var job2 domain.Job
	_ = json.Unmarshal(w2.Body.Bytes(), &job2)

	if job1.ID != job2.ID {
		t.Fatalf("expected identical job ID %s, got %s", job1.ID, job2.ID)
	}
}

func TestAPI_CrossUserAuthorization(t *testing.T) {
	h := setupTestHarness(t)

	// User A creates a job
	_, tokenA := registerAndLogin(t, h, "userA", "userA@example.com", "Password123!")
	// User B registers
	_, tokenB := registerAndLogin(t, h, "userB", "userB@example.com", "Password123!")

	jobReq, _ := json.Marshal(api.CreateJobRequest{
		Language:   "python",
		SourceCode: "print('private job')",
	})
	reqCreate := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", bytes.NewReader(jobReq))
	reqCreate.Header.Set("Authorization", "Bearer "+tokenA)
	reqCreate.Header.Set("Content-Type", "application/json")
	wCreate := httptest.NewRecorder()
	h.router.ServeHTTP(wCreate, reqCreate)

	var job domain.Job
	_ = json.Unmarshal(wCreate.Body.Bytes(), &job)

	// User B attempts to access User A's job
	reqGet := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+job.ID, nil)
	reqGet.Header.Set("Authorization", "Bearer "+tokenB)
	wGet := httptest.NewRecorder()
	h.router.ServeHTTP(wGet, reqGet)

	if wGet.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for cross-user get, got %d", wGet.Code)
	}

	// User B attempts to cancel User A's job
	reqCancel := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+job.ID+"/cancel", nil)
	reqCancel.Header.Set("Authorization", "Bearer "+tokenB)
	wCancel := httptest.NewRecorder()
	h.router.ServeHTTP(wCancel, reqCancel)

	if wCancel.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for cross-user cancel, got %d", wCancel.Code)
	}

	// User A cancels their own job successfully
	reqCancelA := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+job.ID+"/cancel", nil)
	reqCancelA.Header.Set("Authorization", "Bearer "+tokenA)
	wCancelA := httptest.NewRecorder()
	h.router.ServeHTTP(wCancelA, reqCancelA)

	if wCancelA.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for owner cancellation, got %d, body: %s", wCancelA.Code, wCancelA.Body.String())
	}
}

func TestAPI_HealthAndMetrics(t *testing.T) {
	h := setupTestHarness(t)

	// Liveness
	reqLive := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	wLive := httptest.NewRecorder()
	h.router.ServeHTTP(wLive, reqLive)
	if wLive.Code != http.StatusOK || !strings.Contains(wLive.Body.String(), "live") {
		t.Fatalf("expected liveness 200 OK, got %d", wLive.Code)
	}

	// Readiness
	reqReady := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	wReady := httptest.NewRecorder()
	h.router.ServeHTTP(wReady, reqReady)
	if wReady.Code != http.StatusOK || !strings.Contains(wReady.Body.String(), "ready") {
		t.Fatalf("expected readiness 200 OK, got %d", wReady.Code)
	}

	// Prometheus Metrics
	reqMetrics := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	wMetrics := httptest.NewRecorder()
	h.router.ServeHTTP(wMetrics, reqMetrics)
	if wMetrics.Code != http.StatusOK || !strings.Contains(wMetrics.Body.String(), "forgequeue_") {
		t.Fatalf("expected metrics 200 OK with forgequeue metrics, got %d", wMetrics.Code)
	}
}
