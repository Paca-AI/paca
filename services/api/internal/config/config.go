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
	Env      string // development | production
}

type ServerConfig struct {
	Port string
}

type DatabaseConfig struct {
	DSN string
}

type RedisConfig struct {
	URL string
}

type RabbitMQConfig struct {
	URL string
}

type JWTConfig struct {
	Secret     string
	AccessTTL  time.Duration
	RefreshTTL time.Duration
}
