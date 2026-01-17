package usagesink

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/poly-workshop/llm-gateway/internal/domain/llm"
	"github.com/redis/go-redis/v9"
)

func TestRedisStreamSink_Publish(t *testing.T) {
	// This test requires a local Redis instance running on localhost:6379
	// Skip if Redis is not available
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   15, // Use a separate DB for testing
	})
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Check if Redis is available
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis not available: %v", err)
	}

	// Clean up test stream before and after test
	streamKey := "test:llmgw:usage:v1"
	defer client.Del(context.Background(), streamKey)
	client.Del(context.Background(), streamKey)

	// Create sink
	sink, err := NewRedisStreamSink(RedisStreamConfig{
		Addr:       "localhost:6379",
		Password:   "",
		DB:         15,
		StreamKey:  streamKey,
		MaxLen:     100,
		Timeout:    500 * time.Millisecond,
		ApproxTrim: true,
	})
	if err != nil {
		t.Fatalf("NewRedisStreamSink failed: %v", err)
	}
	defer sink.Close(ctx)

	// Publish test event
	event := llm.UsageEvent{
		Timestamp:    time.Now().UTC(),
		RequestID:    "test-request-123",
		Subject:      "user@example.com",
		JTI:          "jwt-token-id",
		Model:        "dashscope/qwen-turbo",
		Provider:     "dashscope",
		UsageTokens:  llm.TokenUsage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
		LatencyMs:    150,
		Status:       "success",
		ErrorType:    "",
		ErrorMessage: "",
	}

	if err := sink.Publish(ctx, event); err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	// Verify event was written to stream
	result, err := client.XRead(ctx, &redis.XReadArgs{
		Streams: []string{streamKey, "0"},
		Count:   1,
		Block:   time.Second,
	}).Result()
	if err != nil {
		t.Fatalf("XRead failed: %v", err)
	}

	if len(result) == 0 || len(result[0].Messages) == 0 {
		t.Fatal("No messages in stream")
	}

	msg := result[0].Messages[0]
	eventJSON, ok := msg.Values["event"].(string)
	if !ok {
		t.Fatal("event field not found or not a string")
	}

	var gotEvent llm.UsageEvent
	if err := json.Unmarshal([]byte(eventJSON), &gotEvent); err != nil {
		t.Fatalf("Failed to unmarshal event: %v", err)
	}

	// Verify key fields
	if gotEvent.RequestID != event.RequestID {
		t.Errorf("RequestID = %q, want %q", gotEvent.RequestID, event.RequestID)
	}
	if gotEvent.Subject != event.Subject {
		t.Errorf("Subject = %q, want %q", gotEvent.Subject, event.Subject)
	}
	if gotEvent.Model != event.Model {
		t.Errorf("Model = %q, want %q", gotEvent.Model, event.Model)
	}
	if gotEvent.Status != event.Status {
		t.Errorf("Status = %q, want %q", gotEvent.Status, event.Status)
	}
	if gotEvent.UsageTokens.TotalTokens != event.UsageTokens.TotalTokens {
		t.Errorf("TotalTokens = %d, want %d", gotEvent.UsageTokens.TotalTokens, event.UsageTokens.TotalTokens)
	}
}

func TestRedisStreamSink_Config(t *testing.T) {
	tests := []struct {
		name    string
		cfg     RedisStreamConfig
		wantErr bool
		skipPing bool
	}{
		{
			name: "valid config",
			cfg: RedisStreamConfig{
				Addr:      "localhost:6379",
				StreamKey: "test:stream",
			},
			wantErr: false,
			skipPing: false,
		},
		{
			name: "missing addr",
			cfg: RedisStreamConfig{
				StreamKey: "test:stream",
			},
			wantErr: true,
			skipPing: true,
		},
		{
			name: "missing stream key",
			cfg: RedisStreamConfig{
				Addr: "localhost:6379",
			},
			wantErr: true,
			skipPing: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Skip tests that require Redis connection if Redis is not available
			if !tt.skipPing && !tt.wantErr {
				client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
				defer client.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
				defer cancel()
				if err := client.Ping(ctx).Err(); err != nil {
					t.Skipf("Redis not available: %v", err)
				}
			}

			sink, err := NewRedisStreamSink(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewRedisStreamSink() error = %v, wantErr %v", err, tt.wantErr)
			}
			if sink != nil {
				sink.Close(context.Background())
			}
		})
	}
}
