package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/forgequeue/forgequeue/internal/api"
	"github.com/forgequeue/forgequeue/internal/auth"
	"github.com/forgequeue/forgequeue/internal/config"
	"github.com/forgequeue/forgequeue/internal/db"
	"github.com/forgequeue/forgequeue/internal/outbox"
	"github.com/forgequeue/forgequeue/internal/queue"
	"github.com/forgequeue/forgequeue/internal/repository"
	"github.com/forgequeue/forgequeue/internal/runtime"
	"github.com/forgequeue/forgequeue/migrations"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})).With("service", "forgequeue-api")

	cfg := config.Load()
	logger.Info("starting forgequeue API server", "port", cfg.APIPort, "env", cfg.AppEnv)

	// 1. PostgreSQL Connection & Migrations
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
		logger.Error("failed to connect to postgresql", "error", err)
		os.Exit(1)
	}
	defer database.Close()
	logger.Info("connected to postgresql successfully")

	// Run migrations
	ctxMigrate, cancelMigrate := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelMigrate()
	if err := db.RunMigrations(ctxMigrate, database, migrations.Files, "."); err != nil {
		logger.Error("failed to run database migrations", "error", err)
		os.Exit(1)
	}
	logger.Info("database migrations verified and up to date")

	// 2. Redis Connection
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	ctxRedis, cancelRedis := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelRedis()
	if err := rdb.Ping(ctxRedis).Err(); err != nil {
		logger.Warn("redis ping failed; check redis status", "error", err)
	} else {
		logger.Info("connected to redis successfully")
	}

	// 3. Initialize Core Repositories & Services
	userRepo := repository.NewPostgresUserRepository(database)
	outboxRepo := repository.NewPostgresOutboxRepository(database)
	jobRepo := repository.NewPostgresJobRepository(database)
	attemptRepo := repository.NewPostgresAttemptRepository(database)
	workerRepo := repository.NewPostgresWorkerRepository(database)
	idempotencyRepo := repository.NewPostgresIdempotencyRepository(database)

	redisQueue := queue.NewRedisQueue(rdb, queue.DefaultStreamKey, queue.DefaultDLQKey, queue.DefaultGroupName)

	tokenManager, err := auth.NewTokenManager(cfg.JWTSecret, "forgequeue")
	if err != nil {
		logger.Error("failed to initialize token manager", "error", err)
		os.Exit(1)
	}

	// 4. Start Outbox Dispatcher and Reconciler
	appCtx, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()

	dispatcher := outbox.NewDispatcher(outboxRepo, redisQueue, cfg.OutboxBatchSize, cfg.OutboxPollInterval, logger)
	go dispatcher.Start(appCtx)

	reconciler := outbox.NewReconciler(outboxRepo, redisQueue, cfg.OutboxStaleCutoff, 5*time.Second, cfg.OutboxBatchSize, logger)
	go reconciler.Start(appCtx)

	// 5. Construct HTTP Router
	authHandler := api.NewAuthHandler(userRepo, tokenManager, cfg.JWTTTL)
	jobsHandler := api.NewJobsHandler(jobRepo, attemptRepo, idempotencyRepo, redisQueue)
	workersHandler := api.NewWorkersHandler(workerRepo)
	runtimesHandler := api.NewRuntimesHandler(runtime.Default())

	router := api.NewRouter(api.RouterConfig{
		AuthHandler:     authHandler,
		JobsHandler:     jobsHandler,
		WorkersHandler:  workersHandler,
		RuntimesHandler: runtimesHandler,
		TokenManager:    tokenManager,
		DBChecker: func(ctx context.Context) error {
			return database.PingContext(ctx)
		},
		RedisChecker: func(ctx context.Context) error {
			return rdb.Ping(ctx).Err()
		},
		Logger: logger,
	})

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.APIPort),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // 0 for Server-Sent Events long-lived streams
		IdleTimeout:  60 * time.Second,
	}

	// 6. Graceful Shutdown Handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.Info("http server listening", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server failed", "error", err)
			os.Exit(1)
		}
	}()

	sig := <-sigChan
	logger.Info("received shutdown signal, draining connections", "signal", sig.String())

	cancelApp() // stop background outbox dispatcher

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful server shutdown failed", "error", err)
	} else {
		logger.Info("server shutdown complete")
	}
}
