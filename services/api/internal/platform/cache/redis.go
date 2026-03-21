// Package cache provides a Redis client.
package cache

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"
)

// NewClient parses the given URL and returns a connected *redis.Client.
func NewClient(url string, log *slog.Logger) (*redis.Client, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("cache: parse url: %w", err)
	}

	client := redis.NewClient(opts)
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("cache: ping: %w", err)
	}

	log.Info("redis connected")
	return client, nil
}
