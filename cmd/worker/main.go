package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/forgequeue/forgequeue/internal/config"
	"github.com/forgequeue/forgequeue/internal/db"
	"github.com/forgequeue/forgequeue/internal/executor"
	"github.com/forgequeue/forgequeue/internal/queue"
	"github.com/forgequeue/forgequeue/internal/recovery"
	"github.com/forgequeue/forgequeue/internal/repository"
	"github.com/forgequeue/forgequeue/internal/runtime"
	"github.com/forgequeue/forgequeue/internal/worker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})).With("service", "forgequeue-worker")

	cfg := config.Load()

	// 1. PostgreSQL Connection
	dbCfg := db.Config{
		Host:            cfg.DBHost,
		Port:            cfg.DBPort,
		User:            cfg.DBUser,
		Password:        cfg.DBPassword,
		Database:        cfg.DBName,
		SSLMode:         cfg.DBSSLMode,
		MaxOpenConns:    cfg.DBMaxOpenConns,
		MaxIdleConns:    cfg.DBMaxIdleConns,
		ConnMaxLifetime: cfg.DBConnMaxLifetime,
	}

	database, err := db.Connect(dbCfg)
	if err != nil {
		logger.Error("worker failed to connect to postgresql", "error", err)
		os.Exit(1)
	}
	defer database.Close()
	logger.Info("connected to postgresql successfully")

	// 2. Redis Connection
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	ctxRedis, cancelRedis := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelRedis()
	if err := rdb.Ping(ctxRedis).Err(); err != nil {
		logger.Error("worker failed to ping redis", "error", err)
		os.Exit(1)
	}
	logger.Info("connected to redis successfully")

	// 3. Initialize Repositories
	jobRepo := repository.NewPostgresJobRepository(database)
	attemptRepo := repository.NewPostgresAttemptRepository(database)
	workerRepo := repository.NewPostgresWorkerRepository(database)

	redisQueue := queue.NewRedisQueue(rdb, queue.DefaultStreamKey, queue.DefaultDLQKey, queue.DefaultGroupName)

	// 4. Select Executor (Docker for production, Process fallback)
	var execEngine executor.Executor
	if cfg.WorkerExecutorType == "docker" {
		dockerExec, err := executor.NewDockerExecutor()
		if err != nil {
			logger.Warn("docker binary unavailable, falling back to ProcessExecutor", "error", err)
			execEngine = executor.NewProcessExecutor()
		} else {
			logger.Info("using DockerExecutor for container isolation")
			execEngine = dockerExec
		}
	} else {
		logger.Info("using ProcessExecutor")
		execEngine = executor.NewProcessExecutor()
	}

	// 5. Worker Configuration
	workerCfg := worker.DefaultConfig()
	workerCfg.Concurrency = cfg.WorkerConcurrency
	workerCfg.HeartbeatInterval = cfg.WorkerHeartbeatInterval
	workerCfg.MinIdleClaimTime = cfg.WorkerMinIdleClaimTime
	workerCfg.ClaimBatchSize = cfg.WorkerClaimBatchSize

	w := worker.New(
		workerCfg,
		jobRepo,
		attemptRepo,
		workerRepo,
		redisQueue,
		execEngine,
		runtime.Default(),
		logger,
	)

	// 6. Launch Recovery Janitor in background
	janitorCfg := recovery.JanitorConfig{
		HeartbeatStaleThreshold: cfg.JanitorHeartbeatStaleThreshold,
		JobLeaseGracePeriod:     cfg.JanitorJobLeaseGracePeriod,
		Interval:                cfg.JanitorInterval,
	}
	janitor := recovery.NewJanitor(janitorCfg, jobRepo, workerRepo, attemptRepo, logger)

	appCtx, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()

	go janitor.Start(appCtx)

	// 7. Graceful Shutdown Signal Handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Info("worker received termination signal, initiating shutdown", "signal", sig.String())
		cancelApp()
	}()

	logger.Info("worker starting consumption loop", "worker_id", w.ID(), "concurrency", workerCfg.Concurrency)
	if err := w.Start(appCtx); err != nil {
		logger.Error("worker exited with error", "error", err)
		os.Exit(1)
	}

	logger.Info("worker process exited cleanly")
}
