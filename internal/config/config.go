package config

import (
	"os"
	"strconv"
	"time"
)

// Config aggregates all runtime configuration options from environment variables.
type Config struct {
	// API
	APIPort int
	AppEnv  string

	// Auth
	JWTSecret string
	JWTTTL    time.Duration

	// Database
	DBHost            string
	DBPort            int
	DBUser            string
	DBPassword        string
	DBName            string
	DBSSLMode         string
	DBMaxOpenConns    int
	DBMaxIdleConns    int
	DBConnMaxLifetime time.Duration

	// Redis
	RedisAddr     string
	RedisPassword string
	RedisDB       int

	// Outbox & Dispatcher
	OutboxBatchSize    int
	OutboxPollInterval time.Duration
	OutboxStaleCutoff  time.Duration

	// Worker
	WorkerConcurrency       int
	WorkerHeartbeatInterval time.Duration
	WorkerMinIdleClaimTime  time.Duration
	WorkerClaimBatchSize    int64
	WorkerExecutorType      string // "docker" (production) or "process" (local dev/fallback)

	// Recovery Janitor
	JanitorInterval                time.Duration
	JanitorHeartbeatStaleThreshold time.Duration
	JanitorJobLeaseGracePeriod     time.Duration
}

// Load reads configuration from environment variables with safe defaults.
func Load() Config {
	return Config{
		APIPort: getEnvInt("API_PORT", 8080),
		AppEnv:  getEnv("APP_ENV", "development"),

		JWTSecret: getEnv("JWT_SECRET", "forgequeue_default_secure_secret_key_at_least_32_bytes_long!"),
		JWTTTL:    getEnvDuration("JWT_TTL", 24*time.Hour),

		DBHost:            getEnv("DB_HOST", "localhost"),
		DBPort:            getEnvInt("DB_PORT", 5432),
		DBUser:            getEnv("DB_USER", "postgres"),
		DBPassword:        getEnv("DB_PASSWORD", "postgres"),
		DBName:            getEnv("DB_NAME", "forgequeue"),
		DBSSLMode:         getEnv("DB_SSLMODE", "disable"),
		DBMaxOpenConns:    getEnvInt("DB_MAX_OPEN_CONNS", 25),
		DBMaxIdleConns:    getEnvInt("DB_MAX_IDLE_CONNS", 10),
		DBConnMaxLifetime: getEnvDuration("DB_CONN_MAX_LIFETIME", 5*time.Minute),

		RedisAddr:     getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       getEnvInt("REDIS_DB", 0),

		OutboxBatchSize:    getEnvInt("OUTBOX_BATCH_SIZE", 50),
		OutboxPollInterval: getEnvDuration("OUTBOX_POLL_INTERVAL", 100*time.Millisecond),
		OutboxStaleCutoff:  getEnvDuration("OUTBOX_STALE_CUTOFF", 10*time.Second),

		WorkerConcurrency:       getEnvInt("WORKER_CONCURRENCY", 4),
		WorkerHeartbeatInterval: getEnvDuration("WORKER_HEARTBEAT_INTERVAL", 3*time.Second),
		WorkerMinIdleClaimTime:  getEnvDuration("WORKER_MIN_IDLE_CLAIM_TIME", 20*time.Second),
		WorkerClaimBatchSize:    int64(getEnvInt("WORKER_CLAIM_BATCH_SIZE", 5)),
		WorkerExecutorType:      getEnv("WORKER_EXECUTOR", "docker"),

		JanitorInterval:                getEnvDuration("JANITOR_INTERVAL", 5*time.Second),
		JanitorHeartbeatStaleThreshold: getEnvDuration("JANITOR_STALE_THRESHOLD", 15*time.Second),
		JanitorJobLeaseGracePeriod:     getEnvDuration("JANITOR_LEASE_GRACE", 15*time.Second),
	}
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}
	return defaultVal
}
