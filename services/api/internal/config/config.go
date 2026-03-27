// Package config defines the typed configuration model for the API service.
package config

import "time"

// Config holds all runtime configuration for the service.
type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Redis    RedisConfig
	RabbitMQ RabbitMQConfig
	JWT      JWTConfig
	Admin    AdminConfig
	Env      string // development | production
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port         string
	CookieSecure bool // set Secure flag on auth cookies; enable when behind an SSL-terminating proxy
}

// AdminConfig holds the default administrator credentials seeded on first startup.
type AdminConfig struct {
	Username string
	Password string
}

// DatabaseConfig holds the primary database connection settings.
type DatabaseConfig struct {
	DSN string
}

// RedisConfig holds Redis connection settings.
type RedisConfig struct {
	URL string
}

// RabbitMQConfig holds RabbitMQ connection settings.
type RabbitMQConfig struct {
	URL string
}

// JWTConfig holds JWT signing and expiry settings.
type JWTConfig struct {
	Secret            string
	AccessTTL         time.Duration
	RefreshTTL        time.Duration // persistent session (remember me = true)
	RefreshSessionTTL time.Duration // ephemeral session (remember me = false)
}
