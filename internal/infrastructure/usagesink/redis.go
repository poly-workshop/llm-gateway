package usagesink

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/poly-workshop/llm-gateway/internal/domain/llm"
	"github.com/redis/go-redis/v9"
)

// RedisStreamSink publishes usage events to a Redis Stream with best-effort semantics.
// If Redis is unavailable or append fails, the sink logs an error but does not block.
type RedisStreamSink struct {
	client     *redis.Client
	streamKey  string
	maxLen     int64
	timeout    time.Duration
	approxTrim bool // Use MAXLEN ~ (approximate) for better performance
}

// RedisStreamConfig configures the Redis Stream usage sink.
type RedisStreamConfig struct {
	// Addr is the Redis server address (host:port).
	Addr string
	// Password is the Redis auth password (optional).
	Password string
	// DB is the Redis database number (default: 0).
	DB int
	// StreamKey is the Redis Stream key name (e.g., "llmgw:usage:v1").
	StreamKey string
	// MaxLen is the maximum stream length (0 = unlimited). Uses MAXLEN to trim old entries.
	MaxLen int64
	// Timeout is the maximum time to wait for Redis operations (default: 500ms).
	Timeout time.Duration
	// ApproxTrim uses MAXLEN ~ for approximate trimming (better performance, default: true).
	ApproxTrim bool
}

// NewRedisStreamSink creates a new Redis Stream usage sink.
func NewRedisStreamSink(cfg RedisStreamConfig) (*RedisStreamSink, error) {
	if cfg.Addr == "" {
		return nil, fmt.Errorf("redis addr is required")
	}
	if cfg.StreamKey == "" {
		return nil, fmt.Errorf("redis stream key is required")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 500 * time.Millisecond
	}

	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	// Test connection - fail fast if Redis is configured but unavailable
	// This ensures we catch configuration errors at startup rather than runtime
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}

	return &RedisStreamSink{
		client:     client,
		streamKey:  cfg.StreamKey,
		maxLen:     cfg.MaxLen,
		timeout:    cfg.Timeout,
		approxTrim: cfg.ApproxTrim,
	}, nil
}

// Publish publishes a usage event to the Redis Stream.
// This is best-effort and non-blocking: if Redis is unavailable or the operation fails,
// it logs an error but does not return an error to the caller.
func (s *RedisStreamSink) Publish(ctx context.Context, event llm.UsageEvent) error {
	if s == nil || s.client == nil {
		return nil
	}

	// Serialize event to JSON
	eventJSON, err := json.Marshal(event)
	if err != nil {
		slog.Warn("failed to marshal usage event", "error", err)
		return nil // Best-effort: don't propagate error
	}

	// Create a timeout context for the Redis operation
	timeoutCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	// Build XADD args
	args := &redis.XAddArgs{
		Stream: s.streamKey,
		Values: map[string]interface{}{
			"event": string(eventJSON),
		},
	}

	// Apply MAXLEN trimming if configured
	if s.maxLen > 0 {
		args.MaxLen = s.maxLen
		args.Approx = s.approxTrim
	}

	// Publish to Redis Stream
	if err := s.client.XAdd(timeoutCtx, args).Err(); err != nil {
		slog.Warn("failed to publish usage event to redis stream",
			"stream", s.streamKey,
			"error", err,
		)
		return nil // Best-effort: don't propagate error
	}

	return nil
}

// Close closes the Redis client connection.
func (s *RedisStreamSink) Close(ctx context.Context) error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}
