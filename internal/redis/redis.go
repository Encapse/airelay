package redis

import (
	"fmt"

	"github.com/redis/go-redis/v9"
)

// Connect creates a new Redis client from a URL.
func Connect(redisURL string) (*redis.Client, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis URL: %w", err)
	}
	return redis.NewClient(opts), nil
}
