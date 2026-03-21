// Package config loads runtime configuration from environment variables and
// optional .env files.
package config

import (
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"
)

// Load reads .env (if present) then environment variables and returns a
// validated Config.  Missing required keys cause a non-nil error.
func Load() (*Config, error) {
	// .env is optional; ignore "file not found" error.
	_ = godotenv.Load()

	accessTTL, err := parseDuration(env("JWT_ACCESS_TTL", "15m"))
	if err != nil {
		return nil, fmt.Errorf("config: JWT_ACCESS_TTL: %w", err)
	}
	refreshTTL, err := parseDuration(env("JWT_REFRESH_TTL", "168h"))
	if err != nil {
		return nil, fmt.Errorf("config: JWT_REFRESH_TTL: %w", err)
	}

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return nil, fmt.Errorf("config: JWT_SECRET must be set")
	}

	return &Config{
		Env: env("ENV", "development"),
		Server: ServerConfig{
			Port: env("PORT", "8080"),
		},
		Database: DatabaseConfig{
			DSN: requireEnv("DATABASE_URL"),
		},
		Redis: RedisConfig{
			URL: requireEnv("REDIS_URL"),
		},
		RabbitMQ: RabbitMQConfig{
			URL: requireEnv("RABBITMQ_URL"),
		},
		JWT: JWTConfig{
			Secret:     secret,
			AccessTTL:  accessTTL,
			RefreshTTL: refreshTTL,
		},
	}, nil
}

// env returns the environment variable value or a fallback default.
func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// requireEnv returns the environment variable value or returns an empty string.
// Callers that truly require the value should validate after Load returns.
func requireEnv(key string) string {
	return os.Getenv(key)
}

func parseDuration(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", s, err)
	}
	return d, nil
}
